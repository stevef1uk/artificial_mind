#!/bin/bash

# Test script to verify BBC news loading with fresh data (Local Linux)
# Clears duplicate tracking and runs a fresh ingestion locally

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

print_status() {
    echo -e "${BLUE}[TEST]${NC} $1"
}

print_success() {
    echo -e "${GREEN}[PASS]${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

print_error() {
    echo -e "${RED}[FAIL]${NC} $1"
}

echo "=========================================="
echo "BBC News Fresh Loading Test (Local)"
echo "=========================================="
echo ""

# Default configuration
NATS_URL="${NATS_URL:-nats://127.0.0.1:4222}"
REDIS_URL="${REDIS_URL:-redis://127.0.0.1:6379}"
WEAVIATE_URL="${WEAVIATE_URL:-http://localhost:8080}"
MAX_STORIES="${MAX_STORIES:-24}"
USE_LLM="${USE_LLM:-true}"
BATCH_SIZE="${BATCH_SIZE:-10}"

# Parse arguments
DRY_RUN=false
DEBUG=false

while [[ $# -gt 0 ]]; do
    case $1 in
        --dry)
            DRY_RUN=true
            shift
            ;;
        --no-llm)
            USE_LLM=false
            shift
            ;;
        --max)
            MAX_STORIES="$2"
            shift 2
            ;;
        --debug)
            DEBUG=true
            shift
            ;;
        --batch-size)
            BATCH_SIZE="$2"
            shift 2
            ;;
        --help)
            echo "Usage: $0 [OPTIONS]"
            echo ""
            echo "Options:"
            echo "  --dry          Dry run (don't publish to NATS)"
            echo "  --no-llm       Use heuristic classification instead of LLM"
            echo "  --max N        Maximum stories to process (default: 24)"
            echo "  --debug        Verbose debug output"
            echo "  --batch-size N LLM batch size (default: 10)"
            echo ""
            echo "Environment variables:"
            echo "  NATS_URL       NATS server URL (default: nats://127.0.0.1:4222)"
            echo "  REDIS_URL      Redis URL (default: redis://127.0.0.1:6379)"
            echo "  WEAVIATE_URL   Weaviate URL (default: http://localhost:8080)"
            echo "  LLM_MODEL      LLM model name (default: llama3.1)"
            echo "  OLLAMA_URL     Ollama API URL (default: http://localhost:11434/api/chat)"
            echo ""
            exit 0
            ;;
        *)
            print_error "Unknown option: $1"
            echo "Use --help for usage"
            exit 1
            ;;
    esac
done

# Step 1: Check prerequisites
print_status "Step 1: Checking prerequisites..."

# Check if binary exists
if [ ! -f "bin/bbc-news-ingestor" ]; then
    print_warning "Binary not found: bin/bbc-news-ingestor"
    print_status "Building it now..."
    if ! make build-news-ingestor 2>/dev/null; then
        print_error "Failed to build. Make sure you're in the project root and Go is installed."
        exit 1
    fi
    print_success "Binary built successfully"
fi

# Check NATS
NATS_HOST=$(echo $NATS_URL | sed 's|nats://||' | cut -d: -f1)
NATS_PORT=$(echo $NATS_URL | sed 's|nats://||' | cut -d: -f2)
if nc -z "$NATS_HOST" "$NATS_PORT" 2>/dev/null; then
    print_success "NATS is accessible at $NATS_URL"
else
    print_warning "NATS may not be running at $NATS_URL"
    if [ "$DRY_RUN" = false ]; then
        print_warning "Events will not be published if NATS is down"
    fi
fi

# Check Redis
REDIS_HOST=$(echo $REDIS_URL | sed 's|redis://||' | cut -d: -f1)
REDIS_PORT=$(echo $REDIS_URL | sed 's|redis://||' | cut -d: -f2)
if nc -z "$REDIS_HOST" "$REDIS_PORT" 2>/dev/null; then
    print_success "Redis is accessible at $REDIS_URL"
    REDIS_AVAILABLE=true
else
    print_warning "Redis not accessible at $REDIS_URL (duplicate detection will be disabled)"
    REDIS_AVAILABLE=false
fi

# Check Weaviate (optional, for verification)
WEAVIATE_HOST=$(echo $WEAVIATE_URL | sed 's|http://||' | sed 's|https://||' | cut -d: -f1)
WEAVIATE_PORT=$(echo $WEAVIATE_URL | sed 's|http://||' | sed 's|https://||' | cut -d: -f2 | cut -d/ -f1)
if nc -z "$WEAVIATE_HOST" "${WEAVIATE_PORT:-8080}" 2>/dev/null; then
    print_success "Weaviate is accessible at $WEAVIATE_URL"
    WEAVIATE_AVAILABLE=true
else
    print_warning "Weaviate not accessible at $WEAVIATE_URL (verification will be skipped)"
    WEAVIATE_AVAILABLE=false
fi

echo ""

# Step 2: Clear duplicate tracking in Redis
if [ "$REDIS_AVAILABLE" = true ]; then
    print_status "Step 2: Clearing duplicate tracking in Redis..."
    
    # Use the clear-news-duplicates binary (preferred method)
    CLEAR_BINARY="bin/clear-news-duplicates"
    DELETED=""
    
    # Build binary if it doesn't exist
    if [ ! -f "$CLEAR_BINARY" ] && command -v go >/dev/null 2>&1; then
        print_status "Building clear-news-duplicates binary..."
        (cd cmd/clear-news-duplicates && go build -o ../../bin/clear-news-duplicates . 2>/dev/null)
    fi
    
    # Try using the binary first
    if [ -f "$CLEAR_BINARY" ]; then
        export REDIS_URL
        OUTPUT=$($CLEAR_BINARY 2>&1)
        if [ $? -eq 0 ]; then
            # Extract number from "✅ Cleared 23 duplicate tracking keys" or "No duplicate keys found"
            if echo "$OUTPUT" | grep -qi "Cleared"; then
                DELETED=$(echo "$OUTPUT" | grep -oP "Cleared \K\d+" | head -1)
                print_success "Cleared $DELETED duplicate tracking keys"
            elif echo "$OUTPUT" | grep -qi "No duplicate keys found"; then
                print_status "No duplicate keys found (fresh start or already cleared)"
            fi
        else
            print_warning "clear-news-duplicates failed, trying fallback methods..."
        fi
    fi
    
    # Fallback: Try redis-cli if binary didn't work
    if [ -z "$DELETED" ] && command -v redis-cli >/dev/null 2>&1; then
        REDIS_HOST=$(echo $REDIS_URL | sed 's|redis://||' | cut -d: -f1)
        REDIS_PORT=$(echo $REDIS_URL | sed 's|redis://||' | cut -d: -f2)
        LUA_SCRIPT='local keys = redis.call("KEYS", "news:duplicates:*"); if #keys > 0 then return redis.call("DEL", unpack(keys)) else return 0 end'
        DELETED=$(redis-cli -h "$REDIS_HOST" -p "$REDIS_PORT" EVAL "$LUA_SCRIPT" 0 2>/dev/null || echo "")
        if [ -n "$DELETED" ] && [ "$DELETED" != "0" ] && [ "$DELETED" != "" ]; then
            print_success "Cleared $DELETED duplicate tracking keys"
        elif [ "$DELETED" = "0" ]; then
            print_status "No duplicate keys found (fresh start or already cleared)"
        fi
    fi
    
    # Final fallback: Docker
    if [ -z "$DELETED" ] && command -v docker >/dev/null 2>&1; then
        REDIS_CONTAINER=$(docker ps --format '{{.Names}}' 2>/dev/null | grep -E '^redis|^redis-server|^redis-stack' | head -1)
        if [ -n "$REDIS_CONTAINER" ]; then
            LUA_SCRIPT='local keys = redis.call("KEYS", "news:duplicates:*"); if #keys > 0 then return redis.call("DEL", unpack(keys)) else return 0 end'
            DELETED=$(docker exec "$REDIS_CONTAINER" redis-cli EVAL "$LUA_SCRIPT" 0 2>/dev/null || echo "")
            if [ -n "$DELETED" ] && [ "$DELETED" != "0" ]; then
                print_success "Cleared $DELETED duplicate tracking keys (via Docker)"
            fi
        fi
    fi
    
    # If still no success, warn but continue
    if [ -z "$DELETED" ] || [ "$DELETED" = "" ]; then
        print_warning "Could not clear duplicates automatically (no tools available)"
        print_status "Continuing anyway - duplicates will be skipped during processing"
    fi
else
    print_status "Step 2: Skipping duplicate clearing (Redis not available)"
fi
echo ""

# Step 3: Run the ingestor
print_status "Step 3: Running news ingestor..."
print_status "Configuration:"
echo "  Max stories: $MAX_STORIES"
echo "  Use LLM: $USE_LLM"
if [ "$USE_LLM" = true ]; then
    echo "  Batch size: $BATCH_SIZE"
    echo "  LLM Model: ${LLM_MODEL:-llama3.1 (default)}"
    echo "  Ollama URL: ${OLLAMA_URL:-http://localhost:11434/api/chat (default)}"
fi
echo "  Dry run: $DRY_RUN"
echo "  Debug: $DEBUG"
echo ""

# Build command
CMD="./bin/bbc-news-ingestor"
CMD="$CMD -max $MAX_STORIES"

if [ "$DRY_RUN" = true ]; then
    CMD="$CMD -dry"
fi

if [ "$USE_LLM" = true ]; then
    CMD="$CMD -llm -batch-size $BATCH_SIZE"
fi

# Always use debug to see discovered count (even if user didn't request it)
# This helps with parsing the output
if [ "$DEBUG" = true ]; then
    CMD="$CMD -debug"
else
    # Add debug anyway to see discovered count, but we'll filter verbose output
    CMD="$CMD -debug"
fi

# Set environment variables (ensure they're exported)
export NATS_URL
export REDIS_URL

# Verify REDIS_URL is set (ingestor defaults to Kubernetes service if not set)
if [ -z "$REDIS_URL" ]; then
    print_error "REDIS_URL is not set! The ingestor will try to use Kubernetes service."
    exit 1
fi
print_status "Using REDIS_URL: $REDIS_URL"

# Run and capture output
print_status "Executing: $CMD"
echo "----------------------------------------"
START_TIME=$(date +%s)
OUTPUT=$($CMD 2>&1)
EXIT_CODE=$?
END_TIME=$(date +%s)
DURATION=$((END_TIME - START_TIME))

if [ $EXIT_CODE -eq 0 ]; then
    print_success "Ingestion completed in ${DURATION}s"
else
    print_error "Ingestion failed with exit code $EXIT_CODE"
    echo "Output:"
    echo "$OUTPUT" | sed 's/^/  /'
    exit $EXIT_CODE
fi

# Show output (filter verbose debug output if not in debug mode)
if [ -n "$OUTPUT" ]; then
    if [ "$DEBUG" = true ]; then
        echo "Ingestor output:"
        echo "$OUTPUT" | sed 's/^/  /'
        echo ""
    else
        # Show only summary lines (not verbose debug)
        echo "Ingestor summary:"
        echo "$OUTPUT" | grep -E "discovered|Processing|no new stories|no stories discovered|\[LLM\]|\[FALLBACK\]" | sed 's/^/  /'
        echo ""
    fi
else
    print_warning "No output from ingestor - this may indicate an issue"
    echo "  Try running with --debug to see detailed output"
    echo ""
fi
echo "----------------------------------------"
echo ""

# Step 4: Analyze results
print_status "Step 4: Analyzing results..."

# Check for common messages
if echo "$OUTPUT" | grep -qi "no stories discovered"; then
    print_warning "Ingestor found no stories - this may indicate:"
    echo "  - BBC website structure changed"
    echo "  - Network connectivity issue"
    echo "  - Need to increase --max value"
    echo "  - Try running with --debug to see detailed scraping output"
    echo ""
elif echo "$OUTPUT" | grep -qi "no new stories to process after duplicate filtering"; then
    print_warning "All stories were duplicates (already processed)"
    print_status "To process fresh stories, clear Redis duplicates or wait 24 hours"
    echo ""
fi

# Count discovered stories (handle empty results)
# Look for "discovered X BBC stories" pattern (only shown with -debug)
DISCOVERED_RAW=$(echo "$OUTPUT" | grep -oP "discovered \K\d+(?= BBC stories)" 2>/dev/null | head -1)
if [ -z "$DISCOVERED_RAW" ] || [ "$DISCOVERED_RAW" = "" ]; then
    DISCOVERED=0
else
    DISCOVERED=$DISCOVERED_RAW
fi

if [ "$DISCOVERED" -gt 0 ] 2>/dev/null; then
    print_success "Discovered $DISCOVERED stories"
else
    # Check if it's because all were duplicates vs actually no stories
    if echo "$OUTPUT" | grep -qi "no new stories to process after duplicate filtering"; then
        # Stories were discovered but all were duplicates - try to extract count from debug output
        # Look for "Processing 0 stories (filtered from X)" pattern
        FILTERED_FROM=$(echo "$OUTPUT" | grep -oP "filtered from \K\d+" 2>/dev/null | head -1)
        if [ -n "$FILTERED_FROM" ] && [ "$FILTERED_FROM" -gt 0 ] 2>/dev/null; then
            print_success "Discovered $FILTERED_FROM stories (all were duplicates)"
            DISCOVERED=$FILTERED_FROM
        else
            print_warning "No stories discovered"
        fi
    else
        print_warning "No stories discovered"
    fi
fi

# Count processed (non-duplicate) stories
# Look for "Processing X stories (filtered from Y)" pattern
PROCESSED_RAW=$(echo "$OUTPUT" | grep -oP "Processing \K\d+(?= stories \(filtered)" 2>/dev/null | head -1)
if [ -z "$PROCESSED_RAW" ] || [ "$PROCESSED_RAW" = "" ]; then
    PROCESSED=0
else
    PROCESSED=$PROCESSED_RAW
fi

if [ "$PROCESSED" -gt 0 ] 2>/dev/null; then
    print_success "Processed $PROCESSED new stories"
elif echo "$OUTPUT" | grep -qi "no new stories to process after duplicate filtering"; then
    print_warning "No new stories processed (all $DISCOVERED were duplicates)"
else
    print_warning "No new stories processed (all duplicates or LLM failed)"
fi

# Count published events (handle empty results properly)
# Use awk to get clean integer counts
ALERT_COUNT=$(echo "$OUTPUT" | grep -E "\[LLM\] ALERT|\[FALLBACK\] ALERT" 2>/dev/null | wc -l | awk '{print $1}')
REL_COUNT=$(echo "$OUTPUT" | grep -E "\[LLM\] REL|\[FALLBACK\] REL" 2>/dev/null | wc -l | awk '{print $1}')
SKIP_COUNT=$(echo "$OUTPUT" | grep -E "\[LLM\] SKIP|\[FALLBACK\] SKIP" 2>/dev/null | wc -l | awk '{print $1}')

# Ensure we have valid integers (default to 0 if empty or non-numeric)
if ! [ "$ALERT_COUNT" -eq "$ALERT_COUNT" ] 2>/dev/null; then
    ALERT_COUNT=0
fi
if ! [ "$REL_COUNT" -eq "$REL_COUNT" ] 2>/dev/null; then
    REL_COUNT=0
fi
if ! [ "$SKIP_COUNT" -eq "$SKIP_COUNT" ] 2>/dev/null; then
    SKIP_COUNT=0
fi

echo ""
print_status "Event classification:"
echo "  Alerts: $ALERT_COUNT"
echo "  Relations: $REL_COUNT"
echo "  Skipped: $SKIP_COUNT"
echo ""

# Check for LLM errors
LLM_ERRORS=$(echo "$OUTPUT" | grep -c "LLM error" 2>/dev/null || echo "0")
LLM_ERRORS=$(echo "$LLM_ERRORS" | awk '{print $1}')
if ! [ "$LLM_ERRORS" -eq "$LLM_ERRORS" ] 2>/dev/null; then
    LLM_ERRORS=0
fi
if [ "$LLM_ERRORS" -gt 0 ] 2>/dev/null; then
    print_warning "Found $LLM_ERRORS LLM errors (timeouts expected if Ollama is slow)"
fi

# Show sample of classifications
if [ "$DEBUG" = false ]; then
    echo "Sample classifications:"
    echo "$OUTPUT" | grep -E "\[LLM\]|\[FALLBACK\]" | head -10 | sed 's/^/  /'
    echo ""
fi

# Step 5: Check if events made it to Weaviate (if available and not dry run)
if [ "$WEAVIATE_AVAILABLE" = true ] && [ "$DRY_RUN" = false ]; then
    print_status "Step 5: Checking Weaviate for new events (waiting 30s for FSM processing)..."
    sleep 30
    
    # Get events from last 5 minutes
    FIVE_MINS_AGO=$(date -u -d '5 minutes ago' +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || date -u -v-5M +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || echo "")
    
    QUERY='{"query":"{Get{WikipediaArticle(limit:20 where:{operator:And operands:[{path:[\"source\"] operator:Equal valueString:\"news:fsm\"},{path:[\"timestamp\"] operator:GreaterThan valueString:\"'$FIVE_MINS_AGO'\"}]} sort:[{path:[\"timestamp\"] order:desc}]){title source timestamp url}}}}"}'
    
    RESULT=$(curl -s -X POST "$WEAVIATE_URL/v1/graphql" \
        -H "Content-Type: application/json" \
        -d "$QUERY" 2>/dev/null || echo "")
    
    if [ -n "$RESULT" ]; then
        RECENT_COUNT=$(echo "$RESULT" | python3 -c '
import sys, json
try:
    data = json.load(sys.stdin)
    articles = data.get("data", {}).get("Get", {}).get("WikipediaArticle", [])
    # Filter out tool.created events
    news_articles = [a for a in articles if "tool.created" not in a.get("title", "")]
    print(len(news_articles))
except:
    print(0)
' 2>/dev/null || echo "0")
        
        if [ "$RECENT_COUNT" -gt 0 ]; then
            print_success "Found $RECENT_COUNT recent news events in Weaviate!"
            echo "Recent events:"
            echo "$RESULT" | python3 <<'PYTHON_SCRIPT'
import sys, json
try:
    data = json.load(sys.stdin)
    articles = data.get("data", {}).get("Get", {}).get("WikipediaArticle", [])
    for a in articles[:5]:
        title = a.get("title", "No title")
        if "tool.created" not in title:
            print("  - " + title[:70])
except:
    print("  Could not parse results")
PYTHON_SCRIPT
            if [ $? -ne 0 ]; then
                echo "  Could not parse results"
            fi
        else
            print_warning "No recent news events in Weaviate yet"
            print_status "FSM may still be processing, or events failed to store"
        fi
    else
        print_warning "Failed to query Weaviate"
    fi
else
    print_status "Step 5: Skipping Weaviate check (not available or dry run)"
fi

echo ""
echo "=========================================="
echo "Test Complete"
echo "=========================================="
echo ""

if [ "$DRY_RUN" = false ]; then
    echo "To check NATS events:"
    echo "  nats sub 'agi.events.news.>' --count=10"
    echo ""
    echo "To check FSM processing (if running locally):"
    echo "  tail -f /path/to/fsm-server.log | grep -E 'news|News|storeNewsEventInWeaviate'"
    echo ""
fi

if [ "$WEAVIATE_AVAILABLE" = true ]; then
    echo "To check Weaviate directly:"
    echo "  curl -X POST '$WEAVIATE_URL/v1/graphql' \\"
    echo "    -H 'Content-Type: application/json' \\"
    echo "    -d '{\"query\": \"{ Get { WikipediaArticle(limit: 10, where: {path: [\\\"source\\\"], operator: Equal, valueString: \\\"news:fsm\\\"}) { title source timestamp } } }\"}'"
    echo ""
fi

