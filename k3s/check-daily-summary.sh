#!/bin/bash

# Diagnostic script to check daily summary system status

set -e

NAMESPACE="agi"
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

print_status() {
    echo -e "${BLUE}[CHECK]${NC} $1"
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
echo "Daily Summary System Diagnostic"
echo "=========================================="
echo ""

# Step 1: Check Redis for daily summary data
print_status "Step 1: Checking Redis for daily summary data..."
REDIS_POD=$(kubectl get pods -n $NAMESPACE -l app=redis -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || echo "")

if [ -z "$REDIS_POD" ]; then
    print_error "Redis pod not found"
    exit 1
fi

# Check for latest summary
LATEST=$(kubectl exec -n $NAMESPACE "$REDIS_POD" -- redis-cli GET "daily_summary:latest" 2>/dev/null || echo "")
if [ -n "$LATEST" ] && [ "$LATEST" != "(nil)" ]; then
    print_success "Found daily_summary:latest in Redis"
    echo "$LATEST" | python3 -c '
import sys, json
try:
    data = json.load(sys.stdin)
    print(f"  Date: {data.get(\"date\", \"unknown\")}")
    print(f"  Generated: {data.get(\"generated_at\", \"unknown\")}")
    summary = data.get("summary", "")
    preview = summary[:200] + "..." if len(summary) > 200 else summary
    print(f"  Summary preview: {preview}")
except:
    print("  (Could not parse JSON)")
' 2>/dev/null || echo "  (Raw data exists but could not parse)"
else
    print_warning "No daily_summary:latest found in Redis"
fi

# Check for today's summary
TODAY=$(date -u +%Y-%m-%d)
TODAY_KEY="daily_summary:$TODAY"
TODAY_SUMMARY=$(kubectl exec -n $NAMESPACE "$REDIS_POD" -- redis-cli GET "$TODAY_KEY" 2>/dev/null || echo "")
if [ -n "$TODAY_SUMMARY" ] && [ "$TODAY_SUMMARY" != "(nil)" ]; then
    print_success "Found today's summary ($TODAY_KEY)"
else
    print_warning "No summary found for today ($TODAY)"
fi

# Check history
HISTORY_COUNT=$(kubectl exec -n $NAMESPACE "$REDIS_POD" -- redis-cli LLEN "daily_summary:history" 2>/dev/null || echo "0")
if [ "$HISTORY_COUNT" -gt 0 ]; then
    print_success "Found $HISTORY_COUNT entries in daily_summary:history"
else
    print_warning "No entries in daily_summary:history"
fi

echo ""

# Step 2: Check FSM logs for scheduler activity
print_status "Step 2: Checking FSM logs for scheduler activity..."
FSM_POD=$(kubectl get pods -n $NAMESPACE -l app=fsm-server-rpi58 -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || echo "")

if [ -n "$FSM_POD" ]; then
    # Check for scheduler initialization
    SCHEDULER_INIT=$(kubectl logs -n $NAMESPACE "$FSM_POD" --tail=1000 2>&1 | grep -c "sleep_cron.*Next daily_summary" || echo "0")
    if [ "$SCHEDULER_INIT" -gt 0 ]; then
        print_success "Found scheduler initialization in FSM logs"
        echo "  Last scheduler message:"
        kubectl logs -n $NAMESPACE "$FSM_POD" --tail=1000 2>&1 | grep "sleep_cron.*Next daily_summary" | tail -1 | sed 's/^/    /'
    else
        print_warning "No scheduler initialization found in recent FSM logs"
    fi
    
    # Check for daily_summary trigger attempts
    TRIGGER_ATTEMPTS=$(kubectl logs -n $NAMESPACE "$FSM_POD" --tail=1000 2>&1 | grep -c "sleep_cron.*daily_summary triggered" || echo "0")
    if [ "$TRIGGER_ATTEMPTS" -gt 0 ]; then
        print_success "Found $TRIGGER_ATTEMPTS daily_summary trigger(s) in logs"
        echo "  Recent triggers:"
        kubectl logs -n $NAMESPACE "$FSM_POD" --tail=1000 2>&1 | grep "sleep_cron.*daily_summary triggered" | tail -3 | sed 's/^/    /'
    else
        print_warning "No daily_summary triggers found in recent FSM logs"
    fi
    
    # Check for trigger failures
    TRIGGER_FAILURES=$(kubectl logs -n $NAMESPACE "$FSM_POD" --tail=1000 2>&1 | grep -c "sleep_cron.*daily_summary trigger failed" || echo "0")
    if [ "$TRIGGER_FAILURES" -gt 0 ]; then
        print_error "Found $TRIGGER_FAILURES daily_summary trigger failure(s)"
        echo "  Recent failures:"
        kubectl logs -n $NAMESPACE "$FSM_POD" --tail=1000 2>&1 | grep "sleep_cron.*daily_summary trigger failed" | tail -3 | sed 's/^/    /'
    fi
else
    print_error "FSM pod not found"
fi

echo ""

# Step 3: Check HDN logs for daily_summary processing
print_status "Step 3: Checking HDN logs for daily_summary processing..."
HDN_POD=$(kubectl get pods -n $NAMESPACE -l app=hdn-server-rpi58 -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || echo "")

if [ -n "$HDN_POD" ]; then
    # Check for daily_summary task detection
    TASK_DETECTED=$(kubectl logs -n $NAMESPACE "$HDN_POD" --tail=1000 2>&1 | grep -c "daily_summary.*wrote latest" || echo "0")
    if [ "$TASK_DETECTED" -gt 0 ]; then
        print_success "Found $TASK_DETECTED daily_summary generation(s) in HDN logs"
        echo "  Recent generations:"
        kubectl logs -n $NAMESPACE "$HDN_POD" --tail=1000 2>&1 | grep "daily_summary.*wrote latest" | tail -3 | sed 's/^/    /'
    else
        print_warning "No daily_summary generation found in recent HDN logs"
    fi
    
    # Check for errors
    SUMMARY_ERRORS=$(kubectl logs -n $NAMESPACE "$HDN_POD" --tail=1000 2>&1 | grep -c "daily_summary.*failed" || echo "0")
    if [ "$SUMMARY_ERRORS" -gt 0 ]; then
        print_error "Found $SUMMARY_ERRORS daily_summary error(s) in HDN logs"
        echo "  Recent errors:"
        kubectl logs -n $NAMESPACE "$HDN_POD" --tail=1000 2>&1 | grep "daily_summary.*failed" | tail -3 | sed 's/^/    /'
    fi
else
    print_error "HDN pod not found"
fi

echo ""

# Step 4: Check current time vs next scheduled time
print_status "Step 4: Checking scheduler timing..."
CURRENT_UTC=$(date -u +%H:%M)
CURRENT_HOUR=$(date -u +%H | sed 's/^0//')
CURRENT_MIN=$(date -u +%M | sed 's/^0//')

# Next scheduled time is 02:30 UTC
if [ "$CURRENT_HOUR" -lt 2 ] || ([ "$CURRENT_HOUR" -eq 2 ] && [ "$CURRENT_MIN" -lt 30 ]); then
    print_status "Current UTC time: $CURRENT_UTC"
    print_status "Next scheduled run: 02:30 UTC (today)"
elif [ "$CURRENT_HOUR" -eq 2 ] && [ "$CURRENT_MIN" -ge 30 ]; then
    print_status "Current UTC time: $CURRENT_UTC"
    print_status "Next scheduled run: 02:30 UTC (tomorrow)"
    print_warning "Scheduler should have run today at 02:30 UTC"
else
    print_status "Current UTC time: $CURRENT_UTC"
    print_status "Next scheduled run: 02:30 UTC (tomorrow)"
    print_warning "Scheduler should have run today at 02:30 UTC"
fi

echo ""

# Step 5: Test Monitor API endpoint
print_status "Step 5: Testing Monitor API endpoint..."
MONITOR_POD=$(kubectl get pods -n $NAMESPACE -l app=monitor-ui -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || echo "")

if [ -n "$MONITOR_POD" ]; then
    # Try to access the API via port-forward or direct pod exec
    API_RESPONSE=$(kubectl exec -n $NAMESPACE "$MONITOR_POD" -- wget -q -O- 'http://localhost:8082/api/daily_summary/latest' 2>/dev/null || echo "")
    if [ -n "$API_RESPONSE" ]; then
        print_success "Monitor API endpoint is accessible"
        echo "$API_RESPONSE" | python3 -c '
import sys, json
try:
    data = json.load(sys.stdin)
    if "error" in data:
        print(f"  API returned error: {data[\"error\"]}")
    else:
        print(f"  Date: {data.get(\"date\", \"unknown\")}")
        print(f"  Generated: {data.get(\"generated_at\", \"unknown\")}")
except:
    print("  (Could not parse API response)")
' 2>/dev/null || echo "  (Could not parse API response)"
    else
        print_warning "Could not access Monitor API endpoint"
    fi
else
    print_warning "Monitor pod not found"
fi

echo ""
echo "=========================================="
echo "Diagnostic Complete"
echo "=========================================="
echo ""
echo "To manually trigger a daily summary:"
echo "  kubectl exec -n $NAMESPACE deployment/hdn-server-rpi58 -- wget -qO- \\"
echo "    --post-data '{\"task_name\":\"daily_summary\",\"description\":\"Summarize the day\",\"language\":\"python\"}' \\"
echo "    --header='Content-Type: application/json' \\"
echo "    http://localhost:8080/api/v1/intelligent/execute"
echo ""

