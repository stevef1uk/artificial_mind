#!/usr/bin/env bash
# Script to calculate total LLM tokens from Redis in Kubernetes

set -e

NAMESPACE="${NAMESPACE:-agi}"
REDIS_POD=$(kubectl get pods -n $NAMESPACE -l app=redis -o jsonpath='{.items[0].metadata.name}')

if [ -z "$REDIS_POD" ]; then
    echo "❌ Redis pod not found in namespace $NAMESPACE"
    exit 1
fi

echo "✅ Found Redis pod: $REDIS_POD"
echo ""

# Get all token usage keys
echo "Fetching token usage keys..."
KEYS=$(kubectl exec -n $NAMESPACE $REDIS_POD -- redis-cli --raw KEYS "token_usage*")

if [ -z "$KEYS" ]; then
    echo "No token usage keys found in Redis"
    exit 0
fi

KEY_COUNT=$(echo "$KEYS" | wc -l | tr -d ' ')
echo "Found $KEY_COUNT token usage keys"
echo ""

# Initialize totals
TOTAL_TOKENS=0
PROMPT_TOKENS=0
COMPLETION_TOKENS=0
PROCESSED_KEYS=0

# Track dates we've seen to avoid double-counting (using a temp file)
DATES_FILE=$(mktemp)
trap "rm -f $DATES_FILE" EXIT

# First, get all overall daily totals (primary source)
echo "Processing overall daily totals..."
OVERALL_TOTALS=$(kubectl exec -n $NAMESPACE $REDIS_POD -- redis-cli --raw KEYS "token_usage:[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]:total" | grep -v "component" | grep -v "aggregated")
while IFS= read -r key; do
    if [ -z "$key" ]; then
        continue
    fi
    
    # Extract date from key (format: token_usage:YYYY-MM-DD:total)
    DATE=$(echo "$key" | sed 's/token_usage:\([0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]\):total/\1/')
    echo "$DATE" >> "$DATES_FILE"
    
    VALUE=$(kubectl exec -n $NAMESPACE $REDIS_POD -- redis-cli --raw GET "$key" 2>/dev/null || echo "0")
    if [ -z "$VALUE" ] || ! [[ "$VALUE" =~ ^[0-9]+$ ]]; then
        continue
    fi
    
    TOTAL_TOKENS=$((TOTAL_TOKENS + VALUE))
    PROCESSED_KEYS=$((PROCESSED_KEYS + 1))
    echo "  $key: $VALUE"
    
    # Also get prompt and completion for this date
    PROMPT_KEY="token_usage:${DATE}:prompt"
    COMPLETION_KEY="token_usage:${DATE}:completion"
    
    PROMPT_VAL=$(kubectl exec -n $NAMESPACE $REDIS_POD -- redis-cli --raw GET "$PROMPT_KEY" 2>/dev/null || echo "0")
    COMPLETION_VAL=$(kubectl exec -n $NAMESPACE $REDIS_POD -- redis-cli --raw GET "$COMPLETION_KEY" 2>/dev/null || echo "0")
    
    if [[ "$PROMPT_VAL" =~ ^[0-9]+$ ]] && [ "$PROMPT_VAL" -gt 0 ]; then
        PROMPT_TOKENS=$((PROMPT_TOKENS + PROMPT_VAL))
    fi
    if [[ "$COMPLETION_VAL" =~ ^[0-9]+$ ]] && [ "$COMPLETION_VAL" -gt 0 ]; then
        COMPLETION_TOKENS=$((COMPLETION_TOKENS + COMPLETION_VAL))
    fi
done <<< "$OVERALL_TOTALS"

# Check for dates that only have aggregated keys (if daily keys were reset)
echo ""
echo "Checking for aggregated-only dates..."
AGGREGATED_TOTALS=$(kubectl exec -n $NAMESPACE $REDIS_POD -- redis-cli --raw KEYS "token_usage:aggregated:[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]:total" | grep -v "component")
while IFS= read -r key; do
    if [ -z "$key" ]; then
        continue
    fi
    
    # Extract date from key (format: token_usage:aggregated:YYYY-MM-DD:total)
    DATE=$(echo "$key" | sed 's/token_usage:aggregated:\([0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]\):total/\1/')
    
    # Only count if we haven't seen this date in overall totals
    if ! grep -q "^${DATE}$" "$DATES_FILE" 2>/dev/null; then
        VALUE=$(kubectl exec -n $NAMESPACE $REDIS_POD -- redis-cli --raw GET "$key" 2>/dev/null || echo "0")
        if [ -z "$VALUE" ] || ! [[ "$VALUE" =~ ^[0-9]+$ ]]; then
            continue
        fi
        
        TOTAL_TOKENS=$((TOTAL_TOKENS + VALUE))
        PROCESSED_KEYS=$((PROCESSED_KEYS + 1))
        echo "  $key: $VALUE (aggregated, no daily key found)"
        
        # Also get prompt and completion for this date
        PROMPT_KEY="token_usage:aggregated:${DATE}:prompt"
        COMPLETION_KEY="token_usage:aggregated:${DATE}:completion"
        
        PROMPT_VAL=$(kubectl exec -n $NAMESPACE $REDIS_POD -- redis-cli --raw GET "$PROMPT_KEY" 2>/dev/null || echo "0")
        COMPLETION_VAL=$(kubectl exec -n $NAMESPACE $REDIS_POD -- redis-cli --raw GET "$COMPLETION_KEY" 2>/dev/null || echo "0")
        
        if [[ "$PROMPT_VAL" =~ ^[0-9]+$ ]] && [ "$PROMPT_VAL" -gt 0 ]; then
            PROMPT_TOKENS=$((PROMPT_TOKENS + PROMPT_VAL))
        fi
        if [[ "$COMPLETION_VAL" =~ ^[0-9]+$ ]] && [ "$COMPLETION_VAL" -gt 0 ]; then
            COMPLETION_TOKENS=$((COMPLETION_TOKENS + COMPLETION_VAL))
        fi
    fi
done <<< "$AGGREGATED_TOTALS"

echo ""
echo "============================================================"
echo "SUMMARY"
echo "============================================================"
echo "Keys processed: $PROCESSED_KEYS"
echo "Total Prompt Tokens: $PROMPT_TOKENS"
echo "Total Completion Tokens: $COMPLETION_TOKENS"
echo "Total Tokens (from :total keys): $TOTAL_TOKENS"
echo ""

# Calculate total from prompt + completion
CALCULATED_TOTAL=$((PROMPT_TOKENS + COMPLETION_TOKENS))
if [ $TOTAL_TOKENS -ne $CALCULATED_TOTAL ] && [ $TOTAL_TOKENS -gt 0 ]; then
    echo "Note: Calculated total (prompt + completion) = $CALCULATED_TOTAL"
    echo "      This may differ from stored totals if some keys only track totals."
fi

# Use the larger of the two totals
FINAL_TOTAL=$TOTAL_TOKENS
if [ $CALCULATED_TOTAL -gt $TOTAL_TOKENS ]; then
    FINAL_TOTAL=$CALCULATED_TOTAL
    echo ""
    echo "Using calculated total (prompt + completion) as it's higher."
fi

echo ""
echo "============================================================"
echo "FINAL TOTAL LLM TOKENS: $FINAL_TOTAL"
echo "============================================================"

