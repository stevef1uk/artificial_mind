#!/bin/bash

# Monitor queue stats in real-time
# Useful for watching queue behavior during load testing

HDN_URL="${HDN_URL:-http://localhost:8080}"
INTERVAL="${INTERVAL:-2}"

echo "📊 Monitoring LLM Queue Stats"
echo "=============================="
echo "HDN URL: $HDN_URL"
echo "Update interval: ${INTERVAL}s"
echo "Press Ctrl+C to stop"
echo ""

# Check if HDN is accessible
if ! curl -s --max-time 5 "$HDN_URL/health" > /dev/null 2>&1; then
    echo "❌ HDN server not accessible at $HDN_URL"
    exit 1
fi

# Function to get and display queue stats
show_stats() {
    local stats=$(curl -s "$HDN_URL/api/v1/llm/queue/stats" 2>/dev/null)
    if [ $? -eq 0 ] && [ -n "$stats" ]; then
        local timestamp=$(echo "$stats" | jq -r '.timestamp // "N/A"' 2>/dev/null)
        local high_size=$(echo "$stats" | jq -r '.high_priority_queue_size // 0' 2>/dev/null)
        local high_max=$(echo "$stats" | jq -r '.max_high_priority_queue // 0' 2>/dev/null)
        local high_pct=$(echo "$stats" | jq -r '.high_priority_percent // 0' 2>/dev/null)
        local low_size=$(echo "$stats" | jq -r '.low_priority_queue_size // 0' 2>/dev/null)
        local low_max=$(echo "$stats" | jq -r '.max_low_priority_queue // 0' 2>/dev/null)
        local low_pct=$(echo "$stats" | jq -r '.low_priority_percent // 0' 2>/dev/null)
        local active=$(echo "$stats" | jq -r '.active_workers // 0' 2>/dev/null)
        local max=$(echo "$stats" | jq -r '.max_workers // 0' 2>/dev/null)
        local disabled=$(echo "$stats" | jq -r '.background_llm_disabled // false' 2>/dev/null)
        local auto_disabled=$(echo "$stats" | jq -r '.auto_disabled // false' 2>/dev/null)
        
        # Color coding
        local high_color=""
        local low_color=""
        if (( $(echo "$high_pct >= 70" | bc -l) )); then
            high_color="\033[33m" # Yellow
        elif (( $(echo "$high_pct >= 90" | bc -l) )); then
            high_color="\033[31m" # Red
        else
            high_color="\033[32m" # Green
        fi
        
        if (( $(echo "$low_pct >= 70" | bc -l) )); then
            low_color="\033[33m" # Yellow
        elif (( $(echo "$low_pct >= 90" | bc -l) )); then
            low_color="\033[31m" # Red
        else
            low_color="\033[32m" # Green
        fi
        
        printf "\r\033[K[%s] High: ${high_color}%3d/%3d (%5.1f%%)${high_color}\033[0m | Low: ${low_color}%3d/%3d (%5.1f%%)${low_color}\033[0m | Workers: %d/%d" \
            "$(date +%H:%M:%S)" \
            "$high_size" "$high_max" "$high_pct" \
            "$low_size" "$low_max" "$low_pct" \
            "$active" "$max"
        
        if [ "$disabled" = "true" ] || [ "$auto_disabled" = "true" ]; then
            printf " | \033[31mDISABLED\033[0m"
        fi
    else
        printf "\r\033[K[%s] \033[31mError fetching stats\033[0m" "$(date +%H:%M:%S)"
    fi
}

# Main loop
trap 'echo ""; echo "Stopped."; exit 0' INT TERM

while true; do
    show_stats
    sleep "$INTERVAL"
done

