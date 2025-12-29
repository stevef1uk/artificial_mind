#!/bin/bash

# Check when coherence monitor should run next and verify it's working

NAMESPACE="${K8S_NAMESPACE:-agi}"

echo "⏰ Coherence Monitor Timing Check"
echo "=================================="
echo ""

# Get FSM pod
FSM_POD=$(kubectl get pods -n "$NAMESPACE" -l app=fsm-server-rpi58 -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
if [ -z "$FSM_POD" ]; then
  echo "❌ FSM pod not found"
  exit 1
fi

echo "📦 FSM Pod: $FSM_POD"
echo ""

# Get when monitor started
STARTUP_TIME=$(kubectl logs -n "$NAMESPACE" "$FSM_POD" 2>/dev/null | grep "Coherence monitoring loop started" | tail -1 | awk '{print $1, $2}')
if [ -z "$STARTUP_TIME" ]; then
  echo "❌ Could not find monitor startup time"
  exit 1
fi

echo "🕐 Monitor Started: $STARTUP_TIME"
echo ""

# Calculate when first check should happen (5 minutes after startup)
# Note: Go tickers fire AFTER the first interval
echo "📊 Expected Check Times:"
echo "   First check: ~5 minutes after startup"
echo "   Subsequent checks: Every 5 minutes"
echo ""

# Get all coherence logs
echo "📋 All Coherence Log Entries:"
ALL_LOGS=$(kubectl logs -n "$NAMESPACE" "$FSM_POD" 2>/dev/null | grep -i "\[Coherence\]")
if [ -n "$ALL_LOGS" ]; then
  echo "$ALL_LOGS" | sed 's/^/   /'
  CHECK_COUNT=$(echo "$ALL_LOGS" | grep -c "Starting cross-system coherence check" || echo "0")
  echo ""
  echo "   Total checks run: $CHECK_COUNT"
else
  echo "   ℹ️  No coherence logs found"
fi
echo ""

# Check current time vs expected
echo "💡 Analysis:"
CURRENT_TIME=$(date +%s)
# Try to parse the startup time (format: 2025/12/28 22:36:09)
# This is approximate - we'll just show what we found
if [ "$CHECK_COUNT" = "0" ]; then
  echo "   ⚠️  No coherence checks have run yet"
  echo "   The first check should happen 5 minutes after startup"
  echo "   If it's been more than 5 minutes, there may be an issue"
  echo ""
  echo "   Try checking logs in real-time:"
  echo "     kubectl logs -n $NAMESPACE $FSM_POD -f | grep -i coherence"
else
  echo "   ✅ Coherence checks are running ($CHECK_COUNT check(s) found)"
  LAST_CHECK=$(echo "$ALL_LOGS" | grep "Starting cross-system coherence check" | tail -1)
  echo "   Last check: $LAST_CHECK"
fi
echo ""

# Show recent full logs to see if there are any errors
echo "🔍 Recent FSM Logs (last 20 lines, filtering for errors/warnings):"
RECENT=$(kubectl logs -n "$NAMESPACE" "$FSM_POD" --tail=50 2>/dev/null | grep -iE "error|warning|panic|fatal|coherence" | tail -20)
if [ -n "$RECENT" ]; then
  echo "$RECENT" | sed 's/^/   /'
else
  echo "   ℹ️  No relevant logs found"
fi
echo ""

echo "💡 To manually trigger a check, you would need to restart the pod,"
echo "   or wait for the next 5-minute interval."
echo ""

