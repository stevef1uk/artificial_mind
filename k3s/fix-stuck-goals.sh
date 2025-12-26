#!/bin/bash
# Fix stuck goals by clearing the triggered set for active goals

echo "🔧 Fixing Stuck Goals"
echo "====================="
echo ""

# Get list of active goal IDs from Goal Manager
echo "1. Fetching active goals from Goal Manager..."
ACTIVE_GOALS=$(kubectl exec -n agi deployment/fsm-server-rpi58 -- wget -qO- --timeout=5 http://goal-manager.agi.svc.cluster.local:8090/goals/agent_1/active 2>/dev/null | jq -r '.[].id' 2>/dev/null)

if [ -z "$ACTIVE_GOALS" ]; then
    echo "   ❌ Could not fetch active goals"
    exit 1
fi

ACTIVE_COUNT=$(echo "$ACTIVE_GOALS" | wc -l | tr -d ' ')
echo "   Found $ACTIVE_COUNT active goals"
echo ""

# Check which ones are marked as triggered
echo "2. Checking which active goals are marked as triggered..."
TRIGGERED_COUNT=0
for goal_id in $ACTIVE_GOALS; do
    IS_TRIGGERED=$(kubectl exec -n agi deployment/redis -- redis-cli SISMEMBER "fsm:agent_1:goals:triggered" "$goal_id" 2>/dev/null)
    if [ "$IS_TRIGGERED" = "1" ]; then
        echo "   - $goal_id is marked as triggered (stuck)"
        TRIGGERED_COUNT=$((TRIGGERED_COUNT + 1))
    fi
done
echo ""

if [ $TRIGGERED_COUNT -eq 0 ]; then
    echo "   ✅ No stuck goals found - all active goals are ready to be processed"
    exit 0
fi

echo "3. Found $TRIGGERED_COUNT stuck goals"
echo ""
read -p "   Do you want to clear the triggered flag for these goals? (y/N): " -n 1 -r
echo ""

if [[ ! $REPLY =~ ^[Yy]$ ]]; then
    echo "   Cancelled"
    exit 0
fi

echo "4. Clearing triggered flags..."
CLEARED=0
for goal_id in $ACTIVE_GOALS; do
    IS_TRIGGERED=$(kubectl exec -n agi deployment/redis -- redis-cli SISMEMBER "fsm:agent_1:goals:triggered" "$goal_id" 2>/dev/null)
    if [ "$IS_TRIGGERED" = "1" ]; then
        kubectl exec -n agi deployment/redis -- redis-cli SREM "fsm:agent_1:goals:triggered" "$goal_id" >/dev/null 2>&1
        echo "   ✅ Cleared triggered flag for $goal_id"
        CLEARED=$((CLEARED + 1))
    fi
done

echo ""
echo "=========================="
echo "✅ Cleared triggered flags for $CLEARED goals"
echo ""
echo "FSM should now pick up these goals on the next polling cycle (every 2 seconds)"
echo ""

