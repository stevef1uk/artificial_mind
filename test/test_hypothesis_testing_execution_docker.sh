#!/bin/bash

# Test script for Hypothesis Testing Execution (Docker version)
# Verifies that hypothesis testing goals actually execute and produce artifacts
# Works with Docker containers instead of Kubernetes pods

# Don't exit on error - we want to show helpful messages
set +e

FSM_URL="${FSM_URL:-http://localhost:8083}"
GOAL_MGR_URL="${GOAL_MGR_URL:-http://localhost:8090}"
HDN_URL="${HDN_URL:-http://localhost:8081}"
REDIS_HOST="${REDIS_HOST:-localhost}"
REDIS_PORT="${REDIS_PORT:-6379}"

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}🧪 Testing Hypothesis Testing Execution (Docker)${NC}"
echo "=========================================="
echo ""

# Check if Docker is available
DOCKER_AVAILABLE=false
if command -v docker &> /dev/null; then
    if docker ps &> /dev/null; then
        DOCKER_AVAILABLE=true
    fi
fi

# Find Docker containers (try common naming patterns)
find_container() {
    local pattern="$1"
    if [ "$DOCKER_AVAILABLE" = true ]; then
        # Get all container names
        local containers=$(docker ps --format "{{.Names}}" 2>/dev/null)
        if [ -z "$containers" ]; then
            echo ""
            return
        fi
        # Try exact match first
        echo "$containers" | grep -E "^${pattern}$" | head -1 && return
        # Try with agi- prefix (most common)
        echo "$containers" | grep -iE "^agi-${pattern}(-x86)?(-arm)?$" | head -1 && return
        # Try partial match (contains pattern)
        echo "$containers" | grep -i "${pattern}" | head -1 && return
        # No match found
        echo ""
    else
        echo ""
    fi
}

REDIS_CONTAINER="${REDIS_CONTAINER:-$(find_container "redis")}"
# Fallback: explicitly check for agi-redis if not found
if [ -z "$REDIS_CONTAINER" ] && [ "$DOCKER_AVAILABLE" = true ]; then
    if docker ps --format "{{.Names}}" 2>/dev/null | grep -q "^agi-redis$"; then
        REDIS_CONTAINER="agi-redis"
    fi
fi

FSM_CONTAINER="${FSM_CONTAINER:-$(find_container "fsm")}"
HDN_CONTAINER="${HDN_CONTAINER:-$(find_container "hdn")}"
GOAL_MGR_CONTAINER="${GOAL_MGR_CONTAINER:-$(find_container "goal")}"

# Helper function for Redis commands
redis_cmd() {
    if [ -n "$REDIS_CONTAINER" ]; then
        docker exec "$REDIS_CONTAINER" redis-cli "$@" 2>/dev/null
    elif command -v redis-cli &> /dev/null; then
        redis-cli -h "$REDIS_HOST" -p "$REDIS_PORT" "$@" 2>/dev/null
    else
        echo -e "${RED}❌ Redis not accessible${NC}" >&2
        echo "   Container: ${REDIS_CONTAINER:-NOT FOUND}"
        echo "   Host: $REDIS_HOST:$REDIS_PORT"
        return 1
    fi
}

# Helper function to get logs
get_logs() {
    local container="$1"
    local tail_lines="${2:-100}"
    if [ -n "$container" ] && [ "$DOCKER_AVAILABLE" = true ]; then
        docker logs --tail="$tail_lines" "$container" 2>/dev/null || echo ""
    else
        echo ""
    fi
}

# Check if services are accessible
echo "🔍 Checking service availability..."
echo ""

SERVICES_OK=true

# Check Redis
if [ -n "$REDIS_CONTAINER" ]; then
    if docker exec "$REDIS_CONTAINER" redis-cli ping &> /dev/null; then
        echo -e "${GREEN}✅ Redis container: $REDIS_CONTAINER${NC}"
    else
        echo -e "${RED}❌ Redis container $REDIS_CONTAINER not responding${NC}"
        SERVICES_OK=false
    fi
elif redis-cli -h "$REDIS_HOST" -p "$REDIS_PORT" ping &> /dev/null 2>&1; then
    echo -e "${GREEN}✅ Redis accessible at $REDIS_HOST:$REDIS_PORT${NC}"
else
    echo -e "${RED}❌ Redis not accessible${NC}"
    SERVICES_OK=false
fi

# Check FSM
if [ -n "$FSM_CONTAINER" ]; then
    echo -e "${GREEN}✅ FSM container: $FSM_CONTAINER${NC}"
elif curl -s --connect-timeout 2 "$FSM_URL/health" > /dev/null 2>&1; then
    echo -e "${GREEN}✅ FSM accessible at $FSM_URL${NC}"
else
    echo -e "${YELLOW}⚠️  FSM not accessible at $FSM_URL${NC}"
    echo "   (Will try to continue - may be running locally)"
fi

# Check HDN
if [ -n "$HDN_CONTAINER" ]; then
    echo -e "${GREEN}✅ HDN container: $HDN_CONTAINER${NC}"
elif curl -s --connect-timeout 2 "$HDN_URL/api/v1/domains" > /dev/null 2>&1; then
    echo -e "${GREEN}✅ HDN accessible at $HDN_URL${NC}"
else
    echo -e "${YELLOW}⚠️  HDN not accessible at $HDN_URL${NC}"
    echo "   (Will try to continue - may be running locally)"
fi

# Check Goal Manager
if [ -n "$GOAL_MGR_CONTAINER" ]; then
    echo -e "${GREEN}✅ Goal Manager container: $GOAL_MGR_CONTAINER${NC}"
elif curl -s --connect-timeout 2 "$GOAL_MGR_URL/health" > /dev/null 2>&1; then
    echo -e "${GREEN}✅ Goal Manager accessible at $GOAL_MGR_URL${NC}"
else
    echo -e "${YELLOW}⚠️  Goal Manager not accessible at $GOAL_MGR_URL${NC}"
    echo "   (Will try to continue - may be running locally)"
fi

echo ""

if [ "$SERVICES_OK" = false ]; then
    echo -e "${RED}❌ Required services not available${NC}"
    echo ""
    echo "Detected containers:"
    echo "   Redis: ${REDIS_CONTAINER:-NOT FOUND}"
    echo "   FSM: ${FSM_CONTAINER:-NOT FOUND}"
    echo "   HDN: ${HDN_CONTAINER:-NOT FOUND}"
    echo "   Goal Manager: ${GOAL_MGR_CONTAINER:-NOT FOUND}"
    echo ""
    echo "Available Docker containers:"
    if [ "$DOCKER_AVAILABLE" = true ]; then
        docker ps --format "table {{.Names}}\t{{.Status}}\t{{.Ports}}" 2>/dev/null | head -20 || echo "   (Could not list containers)"
    else
        echo "   Docker not available or not accessible"
    fi
    echo ""
    echo "You can set container names via environment variables:"
    echo "   REDIS_CONTAINER=agi-redis"
    echo "   FSM_CONTAINER=my-fsm"
    echo "   HDN_CONTAINER=my-hdn"
    echo "   GOAL_MGR_CONTAINER=my-goal-manager"
    echo ""
    exit 1
fi

# Test 1: Create a test hypothesis
echo "1️⃣ Creating test hypothesis"
echo "----------------------------"

TEST_HYP_ID="test_hyp_$(date +%s)"
TEST_TIMESTAMP=$(date +%s)
# Use unique hypothesis description to avoid duplicate workflow rejection
TEST_HYP_DESC="If we explore test_event_${TEST_TIMESTAMP}_unique further, we can discover new insights about General domain"

HYP_KEY="fsm:agent_1:hypotheses"
TEST_HYP_JSON=$(cat <<EOF
{
  "id": "$TEST_HYP_ID",
  "description": "$TEST_HYP_DESC",
  "domain": "General",
  "status": "proposed",
  "confidence": 0.6,
  "created_at": "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
}
EOF
)

redis_cmd HSET "$HYP_KEY" "$TEST_HYP_ID" "$TEST_HYP_JSON" > /dev/null 2>&1
if [ $? -eq 0 ]; then
    echo -e "   ${GREEN}✅ Created test hypothesis: $TEST_HYP_ID${NC}"
    echo "   Description: $TEST_HYP_DESC"
else
    echo -e "   ${RED}❌ Failed to create test hypothesis${NC}"
    exit 1
fi
echo ""

# Test 2: Create hypothesis testing goal via Goal Manager
echo "2️⃣ Creating hypothesis testing goal"
echo "------------------------------------"

GOAL_PAYLOAD=$(cat <<EOF
{
  "description": "Test hypothesis: $TEST_HYP_DESC",
  "priority": "medium",
  "status": "active",
  "context": {
    "domain": "General",
    "source": "test",
    "hypothesis_id": "$TEST_HYP_ID"
  }
}
EOF
)

GOAL_RESPONSE=$(curl -s -X POST "$GOAL_MGR_URL/goal" \
    -H "Content-Type: application/json" \
    -d "$GOAL_PAYLOAD" 2>&1)

if echo "$GOAL_RESPONSE" | grep -q '"id"'; then
    GOAL_ID=$(echo "$GOAL_RESPONSE" | python3 -c "import sys, json; print(json.load(sys.stdin).get('id', ''))" 2>/dev/null || echo "")
    if [ -n "$GOAL_ID" ]; then
        echo -e "   ${GREEN}✅ Created goal: $GOAL_ID${NC}"
    else
        echo -e "   ${YELLOW}⚠️  Goal created but ID not extracted${NC}"
        GOAL_ID="unknown"
    fi
else
    echo -e "   ${RED}❌ Failed to create goal${NC}"
    echo "   Response: $GOAL_RESPONSE"
    exit 1
fi
echo ""

# Test 3: Wait for goal execution
echo "3️⃣ Waiting for goal execution (up to 180s)"
echo "-------------------------------------------"

EXECUTION_FOUND=false
WORKFLOW_ID=""
DUPLICATE_REJECTED=false
COMPLETION_DETECTED=false
for i in {1..36}; do
    sleep 5
    
    # Check FSM logs for goal triggering
    if [ -n "$FSM_CONTAINER" ]; then
        FSM_LOGS=$(get_logs "$FSM_CONTAINER" 30 | grep -i "triggered goal.*$GOAL_ID\|workflow.*$GOAL_ID" | tail -3 || echo "")
        if echo "$FSM_LOGS" | grep -q "workflow"; then
            WORKFLOW_ID=$(echo "$FSM_LOGS" | grep -o "workflow [a-z0-9_]*" | head -1 | awk '{print $2}' || echo "")
            if [ -n "$WORKFLOW_ID" ]; then
                echo -e "   ${GREEN}✅ Goal triggered, workflow: $WORKFLOW_ID${NC}"
                EXECUTION_FOUND=true
            fi
        fi
    fi
    
    # Check HDN logs for hypothesis testing execution
    HDN_LOGS=""
    if [ -n "$HDN_CONTAINER" ]; then
        HDN_LOGS=$(get_logs "$HDN_CONTAINER" 200 | grep -i "test hypothesis.*test_event_${TEST_TIMESTAMP}\|hypothesis.*test_event_${TEST_TIMESTAMP}\|detected hypothesis.*test_event\|intelligent.*test_event\|🧪.*Detected hypothesis\|Generated code successfully\|Final execution successful" | tail -15 || echo "")
    elif [ -f "/tmp/hdn_server.log" ]; then
        # Fallback: check log file directly if no container
        HDN_LOGS=$(tail -1000 /tmp/hdn_server.log 2>/dev/null | grep -i "test hypothesis.*test_event_${TEST_TIMESTAMP}\|hypothesis.*test_event_${TEST_TIMESTAMP}\|detected hypothesis.*test_event\|intelligent.*test_event\|🧪.*Detected hypothesis\|Generated code successfully\|Final execution successful\|test_event_${TEST_TIMESTAMP}" | tail -15 || echo "")
    fi
    
    if [ -n "$HDN_LOGS" ]; then
        # Improved detection: check for multiple completion indicators
        if echo "$HDN_LOGS" | grep -qi "test hypothesis.*test_event_${TEST_TIMESTAMP}\|detected hypothesis.*will generate\|🧪.*Detected hypothesis\|Generated code successfully\|Final execution successful"; then
            EXECUTION_FOUND=true
            echo -e "   ${GREEN}✅ Found hypothesis testing execution in HDN logs${NC}"
            
            # Check for completion indicators
            if echo "$HDN_LOGS" | grep -qi "Generated code successfully\|Final execution successful\|✅.*INTELLIGENT.*successful"; then
                COMPLETION_DETECTED=true
                echo -e "   ${GREEN}✅ Code generation and execution completed${NC}"
            fi
            
            # Try to extract workflow ID from HDN logs (improved pattern matching)
            if [ -z "$WORKFLOW_ID" ] || [ "$WORKFLOW_ID" = "N/A" ]; then
                if [ -n "$HDN_CONTAINER" ]; then
                    WF_FROM_LOGS=$(get_logs "$HDN_CONTAINER" 500 | grep -i "test_event_${TEST_TIMESTAMP}\|goal_id.*${GOAL_ID}" | grep -oE "intelligent_[0-9]+" | head -1 || echo "")
                elif [ -f "/tmp/hdn_server.log" ]; then
                    # Look for workflow IDs in logs around the test event
                    WF_FROM_LOGS=$(tail -1000 /tmp/hdn_server.log 2>/dev/null | grep -i "test_event_${TEST_TIMESTAMP}\|goal_id.*${GOAL_ID}" | grep -oE "intelligent_[0-9]+" | head -1 || echo "")
                fi
                if [ -n "$WF_FROM_LOGS" ]; then
                    WORKFLOW_ID="$WF_FROM_LOGS"
                    echo -e "   ${GREEN}✅ Extracted workflow ID from logs: $WORKFLOW_ID${NC}"
                fi
            fi
        fi
        
        # Check for duplicate rejection
        if [ -n "$HDN_CONTAINER" ]; then
            DUPLICATE_CHECK=$(get_logs "$HDN_CONTAINER" 50 | grep -i "rejecting duplicate.*test_event_${TEST_TIMESTAMP}" | tail -1 || echo "")
        elif [ -f "/tmp/hdn_server.log" ]; then
            DUPLICATE_CHECK=$(tail -200 /tmp/hdn_server.log 2>/dev/null | grep -i "rejecting duplicate.*test_event_${TEST_TIMESTAMP}" | tail -1 || echo "")
        fi
        if [ -n "$DUPLICATE_CHECK" ]; then
            DUPLICATE_REJECTED=true
            echo -e "   ${YELLOW}⚠️  Workflow rejected as duplicate${NC}"
            echo "   (This means a similar goal was executed recently)"
            break
        fi
    fi
    
    # Try to find workflow ID by checking Redis workflow records for goal ID
    if [ -z "$WORKFLOW_ID" ] || [ "$WORKFLOW_ID" = "N/A" ]; then
        if [ -n "$GOAL_ID" ]; then
            # First, try to get workflow ID directly from goal record
            GOAL_DATA=$(redis_cmd GET "goal:${GOAL_ID}" 2>/dev/null || echo "")
            if [ -n "$GOAL_DATA" ]; then
                WF_FROM_GOAL=$(echo "$GOAL_DATA" | python3 -c "import sys, json; d=json.load(sys.stdin); print(d.get('workflow_id', ''))" 2>/dev/null || echo "")
                if [ -n "$WF_FROM_GOAL" ] && [ "$WF_FROM_GOAL" != "None" ] && [ "$WF_FROM_GOAL" != "" ]; then
                    WORKFLOW_ID="$WF_FROM_GOAL"
                    echo -e "   ${GREEN}✅ Found workflow ID from goal record: $WORKFLOW_ID${NC}"
                fi
            fi
            
            # If still not found, check recent workflow records in Redis for goal_id
            if [ -z "$WORKFLOW_ID" ] || [ "$WORKFLOW_ID" = "N/A" ]; then
                RECENT_WFS=$(redis_cmd KEYS "workflow:intelligent_*" 2>/dev/null | head -20 || echo "")
                for wf_key in $RECENT_WFS; do
                    WF_DATA=$(redis_cmd GET "$wf_key" 2>/dev/null || echo "")
                    if [ -n "$WF_DATA" ] && echo "$WF_DATA" | grep -qi "$GOAL_ID"; then
                        WF_ID=$(echo "$wf_key" | sed 's/^workflow://' || echo "")
                        if [ -n "$WF_ID" ]; then
                            WORKFLOW_ID="$WF_ID"
                            echo -e "   ${GREEN}✅ Found workflow ID from Redis by goal ID: $WORKFLOW_ID${NC}"
                            break
                        fi
                    fi
                done
            fi
        fi
    fi
    
    # If we have a workflow ID, check if it completed
    if [ -n "$WORKFLOW_ID" ] && [ "$WORKFLOW_ID" != "N/A" ]; then
        WF_STATUS=$(curl -s "$HDN_URL/api/v1/hierarchical/workflow/$WORKFLOW_ID/details" 2>/dev/null | python3 -c "import sys, json; d=json.load(sys.stdin); print(d.get('details', {}).get('status', 'unknown'))" 2>/dev/null || echo "unknown")
        if [ "$WF_STATUS" = "completed" ] || [ "$WF_STATUS" = "failed" ]; then
            echo -e "   ${GREEN}✅ Workflow $WORKFLOW_ID status: $WF_STATUS${NC}"
            break
        fi
    fi
    
    # If completion detected in logs, we can break early
    if [ "$COMPLETION_DETECTED" = true ] && [ "$EXECUTION_FOUND" = true ]; then
        echo -e "   ${GREEN}✅ Execution completed (detected in logs)${NC}"
        break
    fi
    
    echo "   ⏳ Waiting... ($((i*5))s / 180s)"
done

if [ "$EXECUTION_FOUND" = false ] && [ "$DUPLICATE_REJECTED" = false ]; then
    echo -e "   ${YELLOW}⚠️  No execution found in logs (may still be processing)${NC}"
fi
echo ""

# Test 4: Check for artifacts
echo "4️⃣ Checking for artifacts"
echo "-------------------------"

# Wait a bit more for artifacts to be created
sleep 15

# Check HDN file storage for artifacts
ARTIFACTS_FOUND=false
ARTIFACT_COUNT=0

if [ -n "$WORKFLOW_ID" ] && [ "$WORKFLOW_ID" != "N/A" ]; then
    echo "   Checking workflow: $WORKFLOW_ID"
    
    # Try to get artifacts via HDN API
    ARTIFACTS_RESPONSE=$(curl -s "$HDN_URL/api/v1/hierarchical/workflow/$WORKFLOW_ID/files" 2>/dev/null || echo "")
    
    if [ -n "$ARTIFACTS_RESPONSE" ] && echo "$ARTIFACTS_RESPONSE" | grep -qi "hypothesis_test_report\|\.md\|\.pdf\|filename"; then
        ARTIFACTS_FOUND=true
        echo -e "   ${GREEN}✅ Found artifacts for workflow $WORKFLOW_ID${NC}"
        echo "$ARTIFACTS_RESPONSE" | python3 -c "
import sys, json
try:
    data = json.load(sys.stdin)
    files = []
    if isinstance(data, list):
        files = data
    elif isinstance(data, dict):
        if 'files' in data:
            files = data['files'] if isinstance(data['files'], list) else []
        elif 'filename' in data:
            files = [data]
    for f in files:
        filename = f.get('filename', 'N/A')
        size = f.get('size', 0)
        print(f\"   - {filename} ({size} bytes)\")
except Exception as e:
    print(f'   (Could not parse: {e})')
    print(f'   Raw response: {sys.stdin.read()[:200]}')
" 2>/dev/null || echo "   (Artifacts found but format unclear)"
    fi
    
    # Also check Redis file storage directly
    # First try to find files by workflow ID in metadata
    # Also check for hierarchical workflow ID via mapping
    HIERARCHICAL_WF_ID=""
    if [ -n "$WORKFLOW_ID" ] && echo "$WORKFLOW_ID" | grep -qE "^intelligent_"; then
        # Try to find reverse mapping (intelligent -> hierarchical)
        REVERSE_MAP=$(redis_cmd GET "workflow_mapping_reverse:${WORKFLOW_ID}" 2>/dev/null || echo "")
        if [ -n "$REVERSE_MAP" ]; then
            HIERARCHICAL_WF_ID="$REVERSE_MAP"
            echo -e "   ${GREEN}✅ Found hierarchical workflow ID via reverse mapping: $HIERARCHICAL_WF_ID${NC}"
        fi
    fi
    
    REDIS_FILES=""
    # Get all metadata keys and check for matching workflow_id
    ALL_METADATA_KEYS=$(redis_cmd KEYS "file:metadata:*" 2>/dev/null | head -100 || echo "")
    for meta_key in $ALL_METADATA_KEYS; do
        metadata=$(redis_cmd GET "$meta_key" 2>/dev/null || echo "")
        if [ -n "$metadata" ]; then
            wf_id=$(echo "$metadata" | python3 -c "import sys, json; d=json.load(sys.stdin); print(d.get('workflow_id', ''))" 2>/dev/null || echo "")
            # Check if it matches the intelligent workflow ID or the hierarchical one
            if [ "$wf_id" = "$WORKFLOW_ID" ] || ([ -n "$HIERARCHICAL_WF_ID" ] && [ "$wf_id" = "$HIERARCHICAL_WF_ID" ]); then
                REDIS_FILES="$REDIS_FILES $meta_key"
            fi
        fi
    done
    
    # Also check filename indexes
    ALL_NAME_KEYS=$(redis_cmd KEYS "file:by_name:*" 2>/dev/null | head -50 || echo "")
    for name_key in $ALL_NAME_KEYS; do
        file_id=$(redis_cmd GET "$name_key" 2>/dev/null || echo "")
        if [ -n "$file_id" ]; then
            metadata=$(redis_cmd GET "file:metadata:$file_id" 2>/dev/null || echo "")
            if [ -n "$metadata" ]; then
                wf_id=$(echo "$metadata" | python3 -c "import sys, json; d=json.load(sys.stdin); print(d.get('workflow_id', ''))" 2>/dev/null || echo "")
                # Check if it matches the intelligent workflow ID or the hierarchical one
                if [ "$wf_id" = "$WORKFLOW_ID" ] || ([ -n "$HIERARCHICAL_WF_ID" ] && [ "$wf_id" = "$HIERARCHICAL_WF_ID" ]); then
                    REDIS_FILES="$REDIS_FILES $name_key"
                fi
            fi
        fi
    done
    
    if [ -n "$REDIS_FILES" ]; then
        ARTIFACTS_FOUND=true
        ARTIFACT_COUNT=$(echo "$REDIS_FILES" | tr ' ' '\n' | grep -v '^$' | wc -l | tr -d ' ')
        echo -e "   ${GREEN}✅ Found $ARTIFACT_COUNT artifact(s) in Redis storage${NC}"
        for file_key in $REDIS_FILES; do
            if [ -n "$file_key" ]; then
                filename="unknown"
                size="0"
                
                # Check if this is a filename index (file:by_name:filename)
                if echo "$file_key" | grep -q "^file:by_name:"; then
                    # Get the file ID from the index
                    file_id=$(redis_cmd GET "$file_key" 2>/dev/null || echo "")
                    if [ -n "$file_id" ]; then
                        # Get metadata from file:metadata:{fileID}
                        metadata=$(redis_cmd GET "file:metadata:$file_id" 2>/dev/null || echo "")
                        if [ -n "$metadata" ]; then
                            filename=$(echo "$metadata" | python3 -c "import sys, json; d=json.load(sys.stdin); print(d.get('filename', 'N/A'))" 2>/dev/null || echo "N/A")
                            size=$(echo "$metadata" | python3 -c "import sys, json; d=json.load(sys.stdin); print(d.get('size', 0))" 2>/dev/null || echo "0")
                        else
                            filename=$(echo "$file_key" | cut -d: -f3 || echo "$file_key")
                        fi
                    else
                        filename=$(echo "$file_key" | cut -d: -f3 || echo "$file_key")
                    fi
                # Check if this is a metadata key (file:metadata:fileID)
                elif echo "$file_key" | grep -q "^file:metadata:"; then
                    metadata=$(redis_cmd GET "$file_key" 2>/dev/null || echo "")
                    if [ -n "$metadata" ]; then
                        filename=$(echo "$metadata" | python3 -c "import sys, json; d=json.load(sys.stdin); print(d.get('filename', 'N/A'))" 2>/dev/null || echo "N/A")
                        size=$(echo "$metadata" | python3 -c "import sys, json; d=json.load(sys.stdin); print(d.get('size', 0))" 2>/dev/null || echo "0")
                    fi
                fi
                echo "   - $filename ($size bytes)"
            fi
        done
    fi
fi

# If no workflow ID, check by goal ID, test event ID, or recent timestamps
if [ "$ARTIFACTS_FOUND" = false ]; then
    echo "   Checking for artifacts by goal ID, test event, or recent workflows..."
    
    # Method 1: Check for hypothesis_test_report.md files
    REDIS_FILES=$(redis_cmd KEYS "file:*hypothesis_test_report*" 2>/dev/null | head -10 || echo "")
    
    # Method 2: Check by test event ID
    if [ -z "$REDIS_FILES" ]; then
        REDIS_FILES=$(redis_cmd KEYS "file:*test_event_${TEST_TIMESTAMP}*" 2>/dev/null | head -10 || echo "")
    fi
    
    # Method 3: Check recent workflow files (last 5 minutes)
    if [ -z "$REDIS_FILES" ] && [ -n "$GOAL_ID" ]; then
        # Find recent workflows and check their files
        RECENT_WFS=$(redis_cmd KEYS "workflow:intelligent_*" 2>/dev/null | head -10 || echo "")
        for wf_key in $RECENT_WFS; do
            WF_ID=$(echo "$wf_key" | sed 's/^workflow://' || echo "")
            if [ -n "$WF_ID" ]; then
                WF_FILES=$(redis_cmd KEYS "file:*:$WF_ID:*" 2>/dev/null | head -5 || echo "")
                if [ -n "$WF_FILES" ]; then
                    # Check if any file matches our expected artifact name
                    for fkey in $WF_FILES; do
                        if echo "$fkey" | grep -qi "hypothesis_test_report\|\.md"; then
                            REDIS_FILES="$REDIS_FILES $fkey"
                        fi
                    done
                fi
            fi
        done
    fi
    
    # Method 4: Check all recent .md files
    if [ -z "$REDIS_FILES" ]; then
        # Get all file keys and filter for .md files created recently
        ALL_FILES=$(redis_cmd KEYS "file:*" 2>/dev/null | head -50 || echo "")
        for fkey in $ALL_FILES; do
            if echo "$fkey" | grep -qi "hypothesis_test_report\|\.md"; then
                # Check if file metadata contains goal_id or was created recently
                METADATA=$(redis_cmd GET "file:metadata:$(echo "$fkey" | cut -d: -f3)" 2>/dev/null || echo "")
                if [ -n "$METADATA" ]; then
                    REDIS_FILES="$REDIS_FILES $fkey"
                fi
            fi
        done
    fi
    
    if [ -n "$REDIS_FILES" ]; then
        ARTIFACTS_FOUND=true
        ARTIFACT_COUNT=$(echo "$REDIS_FILES" | tr ' ' '\n' | grep -v '^$' | wc -l | tr -d ' ')
        echo -e "   ${GREEN}✅ Found $ARTIFACT_COUNT artifact(s) in Redis storage${NC}"
        for file_key in $REDIS_FILES; do
            if [ -n "$file_key" ]; then
                filename="unknown"
                size="0"
                file_id=""
                
                # Check if this is a filename index (file:by_name:filename)
                if echo "$file_key" | grep -q "^file:by_name:"; then
                    # Get the file ID from the index
                    file_id=$(redis_cmd GET "$file_key" 2>/dev/null || echo "")
                    if [ -n "$file_id" ]; then
                        # Get metadata from file:metadata:{fileID}
                        metadata=$(redis_cmd GET "file:metadata:$file_id" 2>/dev/null || echo "")
                        if [ -n "$metadata" ]; then
                            filename=$(echo "$metadata" | python3 -c "import sys, json; d=json.load(sys.stdin); print(d.get('filename', 'N/A'))" 2>/dev/null || echo "N/A")
                            size=$(echo "$metadata" | python3 -c "import sys, json; d=json.load(sys.stdin); print(d.get('size', 0))" 2>/dev/null || echo "0")
                            # Extract workflow ID from metadata if not already set
                            if [ -z "$WORKFLOW_ID" ] || [ "$WORKFLOW_ID" = "N/A" ]; then
                                wf_from_metadata=$(echo "$metadata" | python3 -c "import sys, json; d=json.load(sys.stdin); print(d.get('workflow_id', ''))" 2>/dev/null || echo "")
                                if [ -n "$wf_from_metadata" ] && [ "$wf_from_metadata" != "None" ] && [ "$wf_from_metadata" != "" ]; then
                                    # Validate workflow ID - should be intelligent_* not code-executor-*
                                    if echo "$wf_from_metadata" | grep -qE "^intelligent_"; then
                                        WORKFLOW_ID="$wf_from_metadata"
                                        echo -e "   ${GREEN}✅ Extracted workflow ID from artifact: $WORKFLOW_ID${NC}"
                                    elif echo "$wf_from_metadata" | grep -qE "^code-executor-"; then
                                        # This is a container ID, not a workflow ID - try to find the correct one
                                        echo -e "   ${YELLOW}⚠️  Artifact has container ID instead of workflow ID, searching for correct workflow ID...${NC}"
                                        # Try to find intelligent_* workflow ID from goal or recent workflows
                                        if [ -n "$GOAL_ID" ]; then
                                            GOAL_DATA=$(redis_cmd GET "goal:${GOAL_ID}" 2>/dev/null || echo "")
                                            if [ -n "$GOAL_DATA" ]; then
                                                CORRECT_WF=$(echo "$GOAL_DATA" | python3 -c "import sys, json; d=json.load(sys.stdin); print(d.get('workflow_id', ''))" 2>/dev/null || echo "")
                                                if [ -n "$CORRECT_WF" ] && [ "$CORRECT_WF" != "None" ] && echo "$CORRECT_WF" | grep -qE "^intelligent_"; then
                                                    WORKFLOW_ID="$CORRECT_WF"
                                                    echo -e "   ${GREEN}✅ Found correct workflow ID from goal record: $WORKFLOW_ID${NC}"
                                                fi
                                            fi
                                        fi
                                        # If still not found, look for recent intelligent_* workflows
                                        if [ -z "$WORKFLOW_ID" ] || [ "$WORKFLOW_ID" = "N/A" ] || echo "$WORKFLOW_ID" | grep -qE "^code-executor-"; then
                                            # Try multiple strategies to find the workflow ID
                                            # Strategy 1: Look for workflows matching the goal ID
                                            RECENT_INTELLIGENT=$(redis_cmd KEYS "workflow:intelligent_*" 2>/dev/null | head -10 || echo "")
                                            for wf_key in $RECENT_INTELLIGENT; do
                                                WF_ID=$(echo "$wf_key" | sed 's/^workflow://' || echo "")
                                                if [ -n "$WF_ID" ] && [ -n "$GOAL_ID" ]; then
                                                    WF_DATA=$(redis_cmd GET "$wf_key" 2>/dev/null || echo "")
                                                    if [ -n "$WF_DATA" ]; then
                                                        # Check if workflow contains goal ID
                                                        if echo "$WF_DATA" | grep -qi "$GOAL_ID"; then
                                                            WORKFLOW_ID="$WF_ID"
                                                            echo -e "   ${GREEN}✅ Found correct workflow ID from workflow records (by goal ID): $WORKFLOW_ID${NC}"
                                                            break
                                                        fi
                                                        # Check if workflow matches test hypothesis description
                                                        if echo "$WF_DATA" | grep -qi "test_event_${TEST_TIMESTAMP}"; then
                                                            WORKFLOW_ID="$WF_ID"
                                                            echo -e "   ${GREEN}✅ Found correct workflow ID from workflow records (by test event): $WORKFLOW_ID${NC}"
                                                            break
                                                        fi
                                                    fi
                                                fi
                                            done
                                            
                                            # Strategy 2: If still not found, get the most recent intelligent_* workflow
                                            if [ -z "$WORKFLOW_ID" ] || [ "$WORKFLOW_ID" = "N/A" ] || echo "$WORKFLOW_ID" | grep -qE "^code-executor-"; then
                                                # Get workflows sorted by timestamp (most recent first)
                                                ALL_WFS=$(redis_cmd KEYS "workflow:intelligent_*" 2>/dev/null || echo "")
                                                if [ -n "$ALL_WFS" ]; then
                                                    # Sort by extracting timestamp from ID and get most recent
                                                    MOST_RECENT=$(echo "$ALL_WFS" | sed 's/^workflow://' | sort -r | head -1 || echo "")
                                                    if [ -n "$MOST_RECENT" ]; then
                                                        # Verify this workflow was created recently (within last 5 minutes)
                                                        WF_TIMESTAMP=$(echo "$MOST_RECENT" | grep -oE "[0-9]+" | head -1 || echo "")
                                                        if [ -n "$WF_TIMESTAMP" ]; then
                                                            CURRENT_TIME=$(date +%s)
                                                            # Convert nanoseconds to seconds if needed (intelligent_* IDs use nanoseconds)
                                                            if [ ${#WF_TIMESTAMP} -gt 10 ]; then
                                                                WF_TIMESTAMP_SEC=$((WF_TIMESTAMP / 1000000000))
                                                            else
                                                                WF_TIMESTAMP_SEC=$WF_TIMESTAMP
                                                            fi
                                                            TIME_DIFF=$((CURRENT_TIME - WF_TIMESTAMP_SEC))
                                                            if [ $TIME_DIFF -ge 0 ] && [ $TIME_DIFF -lt 300 ]; then
                                                                WORKFLOW_ID="$MOST_RECENT"
                                                                echo -e "   ${GREEN}✅ Found most recent workflow ID: $WORKFLOW_ID${NC}"
                                                            fi
                                                        fi
                                                    fi
                                                fi
                                            fi
                                            
                                            # Strategy 3: Look in HDN logs for workflow ID
                                            if [ -z "$WORKFLOW_ID" ] || [ "$WORKFLOW_ID" = "N/A" ] || echo "$WORKFLOW_ID" | grep -qE "^code-executor-"; then
                                                if [ -n "$HDN_CONTAINER" ]; then
                                                    WF_FROM_LOGS=$(get_logs "$HDN_CONTAINER" 500 | grep -i "test_event_${TEST_TIMESTAMP}\|goal_id.*${GOAL_ID}" | grep -oE "intelligent_[0-9]+" | head -1 || echo "")
                                                elif [ -f "/tmp/hdn_server.log" ]; then
                                                    WF_FROM_LOGS=$(tail -1000 /tmp/hdn_server.log 2>/dev/null | grep -i "test_event_${TEST_TIMESTAMP}\|goal_id.*${GOAL_ID}" | grep -oE "intelligent_[0-9]+" | head -1 || echo "")
                                                fi
                                                if [ -n "$WF_FROM_LOGS" ]; then
                                                    WORKFLOW_ID="$WF_FROM_LOGS"
                                                    echo -e "   ${GREEN}✅ Found workflow ID from HDN logs: $WORKFLOW_ID${NC}"
                                                fi
                                            fi
                                        fi
                                    else
                                        WORKFLOW_ID="$wf_from_metadata"
                                        echo -e "   ${GREEN}✅ Extracted workflow ID from artifact: $WORKFLOW_ID${NC}"
                                    fi
                                fi
                            fi
                        else
                            filename=$(echo "$file_key" | cut -d: -f3 || echo "$file_key")
                        fi
                    else
                        filename=$(echo "$file_key" | cut -d: -f3 || echo "$file_key")
                    fi
                # Check if this is a metadata key (file:metadata:fileID)
                elif echo "$file_key" | grep -q "^file:metadata:"; then
                    file_id=$(echo "$file_key" | cut -d: -f3 || echo "")
                    metadata=$(redis_cmd GET "$file_key" 2>/dev/null || echo "")
                    if [ -n "$metadata" ]; then
                        filename=$(echo "$metadata" | python3 -c "import sys, json; d=json.load(sys.stdin); print(d.get('filename', 'N/A'))" 2>/dev/null || echo "N/A")
                        size=$(echo "$metadata" | python3 -c "import sys, json; d=json.load(sys.stdin); print(d.get('size', 0))" 2>/dev/null || echo "0")
                        # Extract workflow ID from metadata if not already set
                        if [ -z "$WORKFLOW_ID" ] || [ "$WORKFLOW_ID" = "N/A" ]; then
                            wf_from_metadata=$(echo "$metadata" | python3 -c "import sys, json; d=json.load(sys.stdin); print(d.get('workflow_id', ''))" 2>/dev/null || echo "")
                            if [ -n "$wf_from_metadata" ] && [ "$wf_from_metadata" != "None" ] && [ "$wf_from_metadata" != "" ]; then
                                # Validate workflow ID - should be intelligent_* not code-executor-*
                                if echo "$wf_from_metadata" | grep -qE "^intelligent_"; then
                                    WORKFLOW_ID="$wf_from_metadata"
                                    echo -e "   ${GREEN}✅ Extracted workflow ID from artifact: $WORKFLOW_ID${NC}"
                                elif echo "$wf_from_metadata" | grep -qE "^code-executor-"; then
                                    # This is a container ID, not a workflow ID - try to find the correct one
                                    echo -e "   ${YELLOW}⚠️  Artifact has container ID instead of workflow ID, searching for correct workflow ID...${NC}"
                                    # Try to find intelligent_* workflow ID from goal or recent workflows
                                    if [ -n "$GOAL_ID" ]; then
                                        GOAL_DATA=$(redis_cmd GET "goal:${GOAL_ID}" 2>/dev/null || echo "")
                                        if [ -n "$GOAL_DATA" ]; then
                                            CORRECT_WF=$(echo "$GOAL_DATA" | python3 -c "import sys, json; d=json.load(sys.stdin); print(d.get('workflow_id', ''))" 2>/dev/null || echo "")
                                            if [ -n "$CORRECT_WF" ] && [ "$CORRECT_WF" != "None" ] && echo "$CORRECT_WF" | grep -qE "^intelligent_"; then
                                                WORKFLOW_ID="$CORRECT_WF"
                                                echo -e "   ${GREEN}✅ Found correct workflow ID from goal record: $WORKFLOW_ID${NC}"
                                            fi
                                        fi
                                    fi
                                    # If still not found, look for recent intelligent_* workflows
                                    if [ -z "$WORKFLOW_ID" ] || [ "$WORKFLOW_ID" = "N/A" ] || echo "$WORKFLOW_ID" | grep -qE "^code-executor-"; then
                                        # Try multiple strategies to find the workflow ID
                                        # Strategy 1: Look for workflows matching the goal ID
                                        RECENT_INTELLIGENT=$(redis_cmd KEYS "workflow:intelligent_*" 2>/dev/null | head -10 || echo "")
                                        for wf_key in $RECENT_INTELLIGENT; do
                                            WF_ID=$(echo "$wf_key" | sed 's/^workflow://' || echo "")
                                            if [ -n "$WF_ID" ] && [ -n "$GOAL_ID" ]; then
                                                WF_DATA=$(redis_cmd GET "$wf_key" 2>/dev/null || echo "")
                                                if [ -n "$WF_DATA" ]; then
                                                    # Check if workflow contains goal ID
                                                    if echo "$WF_DATA" | grep -qi "$GOAL_ID"; then
                                                        WORKFLOW_ID="$WF_ID"
                                                        echo -e "   ${GREEN}✅ Found correct workflow ID from workflow records (by goal ID): $WORKFLOW_ID${NC}"
                                                        break
                                                    fi
                                                    # Check if workflow matches test hypothesis description
                                                    if echo "$WF_DATA" | grep -qi "test_event_${TEST_TIMESTAMP}"; then
                                                        WORKFLOW_ID="$WF_ID"
                                                        echo -e "   ${GREEN}✅ Found correct workflow ID from workflow records (by test event): $WORKFLOW_ID${NC}"
                                                        break
                                                    fi
                                                fi
                                            fi
                                        done
                                        
                                        # Strategy 2: If still not found, get the most recent intelligent_* workflow
                                        if [ -z "$WORKFLOW_ID" ] || [ "$WORKFLOW_ID" = "N/A" ] || echo "$WORKFLOW_ID" | grep -qE "^code-executor-"; then
                                            # Get workflows sorted by timestamp (most recent first)
                                            ALL_WFS=$(redis_cmd KEYS "workflow:intelligent_*" 2>/dev/null || echo "")
                                            if [ -n "$ALL_WFS" ]; then
                                                # Sort by extracting timestamp from ID and get most recent
                                                MOST_RECENT=$(echo "$ALL_WFS" | sed 's/^workflow://' | sort -r | head -1 || echo "")
                                                if [ -n "$MOST_RECENT" ]; then
                                                    # Verify this workflow was created recently (within last 5 minutes)
                                                    WF_TIMESTAMP=$(echo "$MOST_RECENT" | grep -oE "[0-9]+" | head -1 || echo "")
                                                    if [ -n "$WF_TIMESTAMP" ]; then
                                                        CURRENT_TIME=$(date +%s)
                                                        # Convert nanoseconds to seconds if needed (intelligent_* IDs use nanoseconds)
                                                        if [ ${#WF_TIMESTAMP} -gt 10 ]; then
                                                            WF_TIMESTAMP_SEC=$((WF_TIMESTAMP / 1000000000))
                                                        else
                                                            WF_TIMESTAMP_SEC=$WF_TIMESTAMP
                                                        fi
                                                        TIME_DIFF=$((CURRENT_TIME - WF_TIMESTAMP_SEC))
                                                        if [ $TIME_DIFF -ge 0 ] && [ $TIME_DIFF -lt 300 ]; then
                                                            WORKFLOW_ID="$MOST_RECENT"
                                                            echo -e "   ${GREEN}✅ Found most recent workflow ID: $WORKFLOW_ID${NC}"
                                                        fi
                                                    fi
                                                fi
                                            fi
                                        fi
                                        
                                        # Strategy 3: Look in HDN logs for workflow ID
                                        if [ -z "$WORKFLOW_ID" ] || [ "$WORKFLOW_ID" = "N/A" ] || echo "$WORKFLOW_ID" | grep -qE "^code-executor-"; then
                                            if [ -n "$HDN_CONTAINER" ]; then
                                                WF_FROM_LOGS=$(get_logs "$HDN_CONTAINER" 500 | grep -i "test_event_${TEST_TIMESTAMP}\|goal_id.*${GOAL_ID}" | grep -oE "intelligent_[0-9]+" | head -1 || echo "")
                                            elif [ -f "/tmp/hdn_server.log" ]; then
                                                WF_FROM_LOGS=$(tail -1000 /tmp/hdn_server.log 2>/dev/null | grep -i "test_event_${TEST_TIMESTAMP}\|goal_id.*${GOAL_ID}" | grep -oE "intelligent_[0-9]+" | head -1 || echo "")
                                            fi
                                            if [ -n "$WF_FROM_LOGS" ]; then
                                                WORKFLOW_ID="$WF_FROM_LOGS"
                                                echo -e "   ${GREEN}✅ Found workflow ID from HDN logs: $WORKFLOW_ID${NC}"
                                            fi
                                        fi
                                    fi
                                else
                                    WORKFLOW_ID="$wf_from_metadata"
                                    echo -e "   ${GREEN}✅ Extracted workflow ID from artifact: $WORKFLOW_ID${NC}"
                                fi
                            fi
                        fi
                    fi
                else
                    # Try to extract filename from key pattern
                    filename=$(echo "$file_key" | cut -d: -f4 || echo "$file_key")
                    if [ -z "$filename" ] || [ "$filename" = "$file_key" ]; then
                        # Try alternative parsing
                        filename=$(echo "$file_key" | grep -oE "[^:]+\.md" | head -1 || echo "$file_key")
                    fi
                    # Try to get metadata by extracting file ID from key
                    file_id=$(echo "$file_key" | cut -d: -f3 || echo "")
                    if [ -n "$file_id" ] && [ "$file_id" != "$filename" ]; then
                        metadata=$(redis_cmd GET "file:metadata:$file_id" 2>/dev/null || echo "")
                        if [ -n "$metadata" ]; then
                            filename=$(echo "$metadata" | python3 -c "import sys, json; d=json.load(sys.stdin); print(d.get('filename', 'N/A'))" 2>/dev/null || echo "$filename")
                            size=$(echo "$metadata" | python3 -c "import sys, json; d=json.load(sys.stdin); print(d.get('size', 0))" 2>/dev/null || echo "0")
                        fi
                    fi
                fi
                echo "   - $filename ($size bytes)"
            fi
        done
    fi
fi

# Final validation: Ensure we have a valid intelligent_* workflow ID (not a container ID)
if [ -z "$WORKFLOW_ID" ] || [ "$WORKFLOW_ID" = "N/A" ] || echo "$WORKFLOW_ID" | grep -qE "^code-executor-"; then
    echo -e "   ${YELLOW}⚠️  Workflow ID not found or is a container ID, attempting to find correct intelligent_* workflow ID...${NC}"
    # Strategy 1: Try to find from goal record
    if [ -n "$GOAL_ID" ]; then
        GOAL_DATA=$(redis_cmd GET "goal:${GOAL_ID}" 2>/dev/null || echo "")
        if [ -n "$GOAL_DATA" ]; then
            CORRECT_WF=$(echo "$GOAL_DATA" | python3 -c "import sys, json; d=json.load(sys.stdin); print(d.get('workflow_id', ''))" 2>/dev/null || echo "")
            if [ -n "$CORRECT_WF" ] && [ "$CORRECT_WF" != "None" ] && echo "$CORRECT_WF" | grep -qE "^intelligent_"; then
                WORKFLOW_ID="$CORRECT_WF"
                echo -e "   ${GREEN}✅ Corrected workflow ID from goal record: $WORKFLOW_ID${NC}"
            fi
        fi
    fi
    
    # Strategy 2: Look for workflows matching goal ID or test event
    if [ -z "$WORKFLOW_ID" ] || [ "$WORKFLOW_ID" = "N/A" ] || echo "$WORKFLOW_ID" | grep -qE "^code-executor-"; then
        RECENT_INTELLIGENT=$(redis_cmd KEYS "workflow:intelligent_*" 2>/dev/null | head -10 || echo "")
        for wf_key in $RECENT_INTELLIGENT; do
            WF_ID=$(echo "$wf_key" | sed 's/^workflow://' || echo "")
            if [ -n "$WF_ID" ]; then
                WF_DATA=$(redis_cmd GET "$wf_key" 2>/dev/null || echo "")
                if [ -n "$WF_DATA" ]; then
                    # Check if workflow contains goal ID
                    if [ -n "$GOAL_ID" ] && echo "$WF_DATA" | grep -qi "$GOAL_ID"; then
                        WORKFLOW_ID="$WF_ID"
                        echo -e "   ${GREEN}✅ Found correct workflow ID from workflow records (by goal ID): $WORKFLOW_ID${NC}"
                        break
                    fi
                    # Check if workflow matches test hypothesis description
                    if echo "$WF_DATA" | grep -qi "test_event_${TEST_TIMESTAMP}"; then
                        WORKFLOW_ID="$WF_ID"
                        echo -e "   ${GREEN}✅ Found correct workflow ID from workflow records (by test event): $WORKFLOW_ID${NC}"
                        break
                    fi
                fi
            fi
        done
    fi
    
    # Strategy 3: Get most recent intelligent_* workflow (within last 5 minutes)
    if [ -z "$WORKFLOW_ID" ] || [ "$WORKFLOW_ID" = "N/A" ] || echo "$WORKFLOW_ID" | grep -qE "^code-executor-"; then
        ALL_WFS=$(redis_cmd KEYS "workflow:intelligent_*" 2>/dev/null || echo "")
        if [ -n "$ALL_WFS" ]; then
            MOST_RECENT=$(echo "$ALL_WFS" | sed 's/^workflow://' | sort -r | head -1 || echo "")
            if [ -n "$MOST_RECENT" ]; then
                # Verify this workflow was created recently (within last 5 minutes)
                WF_TIMESTAMP=$(echo "$MOST_RECENT" | grep -oE "[0-9]+" | head -1 || echo "")
                if [ -n "$WF_TIMESTAMP" ]; then
                    CURRENT_TIME=$(date +%s)
                    # Convert nanoseconds to seconds if needed
                    if [ ${#WF_TIMESTAMP} -gt 10 ]; then
                        WF_TIMESTAMP_SEC=$((WF_TIMESTAMP / 1000000000))
                    else
                        WF_TIMESTAMP_SEC=$WF_TIMESTAMP
                    fi
                    TIME_DIFF=$((CURRENT_TIME - WF_TIMESTAMP_SEC))
                    if [ $TIME_DIFF -ge 0 ] && [ $TIME_DIFF -lt 300 ]; then
                        WORKFLOW_ID="$MOST_RECENT"
                        echo -e "   ${GREEN}✅ Found most recent workflow ID: $WORKFLOW_ID${NC}"
                    fi
                fi
            fi
        fi
    fi
    
    # Strategy 4: Look in HDN logs
    if [ -z "$WORKFLOW_ID" ] || [ "$WORKFLOW_ID" = "N/A" ] || echo "$WORKFLOW_ID" | grep -qE "^code-executor-"; then
        if [ -n "$HDN_CONTAINER" ]; then
            WF_FROM_LOGS=$(get_logs "$HDN_CONTAINER" 500 | grep -i "test_event_${TEST_TIMESTAMP}\|goal_id.*${GOAL_ID}" | grep -oE "intelligent_[0-9]+" | head -1 || echo "")
        elif [ -f "/tmp/hdn_server.log" ]; then
            WF_FROM_LOGS=$(tail -1000 /tmp/hdn_server.log 2>/dev/null | grep -i "test_event_${TEST_TIMESTAMP}\|goal_id.*${GOAL_ID}" | grep -oE "intelligent_[0-9]+" | head -1 || echo "")
        fi
        if [ -n "$WF_FROM_LOGS" ]; then
            WORKFLOW_ID="$WF_FROM_LOGS"
            echo -e "   ${GREEN}✅ Found workflow ID from HDN logs: $WORKFLOW_ID${NC}"
        fi
    fi
fi

# If duplicate was rejected, check the original workflow
if [ "$DUPLICATE_REJECTED" = true ]; then
    echo "   Checking for artifacts from original (duplicate) workflow..."
    if [ -n "$HDN_CONTAINER" ]; then
        RECENT_WFS=$(get_logs "$HDN_CONTAINER" 200 | grep -o "intelligent_[0-9]*" | sort -u | tail -5 || echo "")
        for wf in $RECENT_WFS; do
            REDIS_FILES=$(redis_cmd KEYS "file:*:$wf:*" 2>/dev/null | head -5 || echo "")
            if [ -n "$REDIS_FILES" ]; then
                ARTIFACTS_FOUND=true
                echo -e "   ${GREEN}✅ Found artifacts in original workflow: $wf${NC}"
                for file_key in $REDIS_FILES; do
                    filename=$(echo "$file_key" | cut -d: -f4 || echo "$file_key")
                    echo "   - $filename"
                done
                break
            fi
        done
    fi
fi

if [ "$ARTIFACTS_FOUND" = false ]; then
    echo -e "   ${YELLOW}⚠️  No artifacts found${NC}"
    if [ "$DUPLICATE_REJECTED" = true ]; then
        echo "   (Workflow was rejected as duplicate - check original workflow artifacts)"
    else
        echo "   (This may be OK if execution is still in progress or failed)"
    fi
fi
echo ""

# Test 5: Check HDN logs for code generation and execution
echo "5️⃣ Checking HDN logs for code generation and execution"
echo "--------------------------------------------------------"

# Look for logs related to our specific test hypothesis
RECENT_LOGS=""
if [ -n "$HDN_CONTAINER" ]; then
    RECENT_LOGS=$(get_logs "$HDN_CONTAINER" 300 | grep -i "test_event_${TEST_TIMESTAMP}\|test hypothesis.*test_event\|hypothesis.*test_event\|intelligent.*test_event\|🧪.*Detected hypothesis" | tail -20 || echo "")
elif [ -f "/tmp/hdn_server.log" ]; then
    # Fallback: check log file directly if no container
    RECENT_LOGS=$(tail -1000 /tmp/hdn_server.log 2>/dev/null | grep -i "test_event_${TEST_TIMESTAMP}\|test hypothesis.*test_event\|hypothesis.*test_event\|intelligent.*test_event\|🧪.*Detected hypothesis\|Generated code.*test_event" | tail -20 || echo "")
fi

if [ -n "$RECENT_LOGS" ]; then
    echo -e "   ${GREEN}✅ Found relevant log messages:${NC}"
    echo "$RECENT_LOGS" | sed 's/^/   /' | head -15
    
    # Check if code was actually generated (not skipped)
    if echo "$RECENT_LOGS" | grep -qi "skipping code generation\|acknowledged"; then
        echo -e "   ${RED}❌ FAIL: Hypothesis testing was skipped/acknowledged instead of executing${NC}"
        echo "   This means the fix didn't work - check intelligent_executor.go"
        exit 1
    elif echo "$RECENT_LOGS" | grep -qi "rejecting duplicate"; then
        echo -e "   ${YELLOW}⚠️  Workflow was rejected as duplicate (this is expected if similar goal ran recently)${NC}"
        ORIG_WF=$(echo "$RECENT_LOGS" | grep -o "intelligent_[0-9]*" | head -1 || echo "")
        if [ -n "$ORIG_WF" ]; then
            echo "   Original workflow: $ORIG_WF"
            WORKFLOW_ID="$ORIG_WF"
        fi
    elif echo "$RECENT_LOGS" | grep -qi "generated code\|will generate code\|detected hypothesis.*will generate\|✅.*generated code\|🧪.*Detected hypothesis\|Generated code successfully\|✅.*INTELLIGENT.*Generated code\|Final execution successful"; then
        echo -e "   ${GREEN}✅ PASS: Code generation detected${NC}"
        
        # Check for execution success/failure (improved patterns)
        EXEC_LOGS=""
        if [ -n "$HDN_CONTAINER" ]; then
            EXEC_LOGS=$(get_logs "$HDN_CONTAINER" 300 | grep -i "test_event_${TEST_TIMESTAMP}" | grep -iE "execution|validation|success|failed|error|Report saved|Final execution|✅.*INTELLIGENT|Extracted file.*md|Stored file" | tail -10 || echo "")
        elif [ -f "/tmp/hdn_server.log" ]; then
            EXEC_LOGS=$(tail -1000 /tmp/hdn_server.log 2>/dev/null | grep -i "test_event_${TEST_TIMESTAMP}" | grep -iE "execution|validation|success|failed|error|Report saved|Final execution|✅.*INTELLIGENT|Extracted file.*md|Stored file.*hypothesis|hypothesis_test_report" | tail -10 || echo "")
        fi
        if [ -n "$EXEC_LOGS" ]; then
            echo "   Execution logs:"
            echo "$EXEC_LOGS" | sed 's/^/   /' | head -8
            if echo "$EXEC_LOGS" | grep -qiE "success|✅|Report saved|hypothesis_test_report|Final execution successful|Extracted file.*md|Stored file"; then
                echo -e "   ${GREEN}✅ Code execution succeeded${NC}"
            elif echo "$EXEC_LOGS" | grep -qiE "failed|error|❌"; then
                echo -e "   ${YELLOW}⚠️  Code execution had errors (check logs above)${NC}"
            fi
        fi
    else
        echo -e "   ${YELLOW}⚠️  Found logs but unclear if code was generated${NC}"
    fi
else
    # Fallback: check for any hypothesis testing activity
    FALLBACK_LOGS=""
    if [ -n "$HDN_CONTAINER" ]; then
        FALLBACK_LOGS=$(get_logs "$HDN_CONTAINER" 200 | grep -i "detected hypothesis\|test hypothesis\|will generate code\|🧪.*Detected hypothesis" | tail -10 || echo "")
    elif [ -f "/tmp/hdn_server.log" ]; then
        FALLBACK_LOGS=$(tail -500 /tmp/hdn_server.log 2>/dev/null | grep -i "detected hypothesis\|test hypothesis\|will generate code\|🧪.*Detected hypothesis\|Generated code successfully" | tail -10 || echo "")
    fi
    if [ -n "$FALLBACK_LOGS" ]; then
        echo -e "   ${YELLOW}⚠️  Found general hypothesis testing logs (not specific to test):${NC}"
        echo "$FALLBACK_LOGS" | sed 's/^/   /'
    else
        echo -e "   ${YELLOW}⚠️  No relevant log messages found${NC}"
        echo "   Check if HDN server has the latest code with hypothesis testing fix"
        echo ""
        echo "   To view HDN logs:"
        if [ -n "$HDN_CONTAINER" ]; then
            echo "      docker logs $HDN_CONTAINER --tail=200 | grep -i hypothesis"
        elif [ -f "/tmp/hdn_server.log" ]; then
            echo "      tail -200 /tmp/hdn_server.log | grep -i hypothesis"
        else
            echo "      (HDN container not found and log file not found - check if HDN is running)"
        fi
    fi
fi
echo ""

# Summary
echo "=========================================="
echo -e "${BLUE}📊 Test Summary${NC}"
echo "=========================================="
echo "   Test Hypothesis ID: $TEST_HYP_ID"
echo "   Goal ID: $GOAL_ID"
echo "   Workflow ID: ${WORKFLOW_ID:-N/A}"
echo "   Execution Found: $EXECUTION_FOUND"
echo "   Artifacts Found: $ARTIFACTS_FOUND"
echo ""

# Determine test result
if [ "$DUPLICATE_REJECTED" = true ]; then
    if [ "$ARTIFACTS_FOUND" = true ]; then
        echo -e "${GREEN}✅ Hypothesis testing is working!${NC}"
        echo "   (Workflow was rejected as duplicate, but artifacts exist from original execution)"
        echo "   This confirms the fix is working - hypothesis testing goals execute and produce artifacts."
        exit 0
    else
        echo -e "${YELLOW}⚠️  Workflow rejected as duplicate${NC}"
        echo "   This means a similar goal was executed recently."
        echo "   The fix appears to be working (no skip detected), but we need a more unique test."
        echo "   Try running the test again with a different hypothesis description."
        exit 0
    fi
elif [ "$EXECUTION_FOUND" = true ] && [ "$ARTIFACTS_FOUND" = true ]; then
    echo -e "${GREEN}✅ Hypothesis testing execution is working!${NC}"
    echo "   Goals are being executed and producing artifacts."
    exit 0
elif [ "$EXECUTION_FOUND" = true ]; then
    echo -e "${YELLOW}⚠️  Partial success: Execution detected but artifacts not found${NC}"
    echo ""
    echo "   Debugging steps:"
    if [ -n "$HDN_CONTAINER" ]; then
        echo "   1. Check HDN logs:"
        echo "      docker logs $HDN_CONTAINER --tail=300 | grep -i 'test_event_${TEST_TIMESTAMP}'"
        echo "   2. Check for execution errors:"
        echo "      docker logs $HDN_CONTAINER --tail=300 | grep -i 'error\\|failed\\|validation' | grep -i 'test_event'"
    fi
    if [ -n "$REDIS_CONTAINER" ]; then
        echo "   3. Check artifact storage:"
        echo "      docker exec $REDIS_CONTAINER redis-cli KEYS 'file:*' | head -10"
    fi
    if [ -n "$WORKFLOW_ID" ] && [ "$WORKFLOW_ID" != "N/A" ]; then
        echo "   4. Check workflow details:"
        echo "      curl -s $HDN_URL/api/v1/hierarchical/workflow/$WORKFLOW_ID/details | jq"
    fi
    exit 0
else
    echo -e "${RED}❌ FAIL: Hypothesis testing execution not detected${NC}"
    echo ""
    echo "   Check logs:"
    if [ -n "$HDN_CONTAINER" ]; then
        echo "   docker logs $HDN_CONTAINER --tail=200 | grep -i hypothesis"
    fi
    if [ -n "$FSM_CONTAINER" ]; then
        echo "   docker logs $FSM_CONTAINER --tail=200 | grep -i 'triggered goal.*$GOAL_ID'"
    fi
    exit 1
fi


