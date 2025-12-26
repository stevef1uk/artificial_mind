#!/bin/bash
# Summary of current system activity

echo "📊 AGI System Activity Summary"
echo "=============================="
echo ""

# Recent activity count
ACTIVITY_COUNT=$(kubectl exec -n agi deployment/redis -- redis-cli LLEN "fsm:agent_1:activity_log" 2>/dev/null || echo "0")
echo "📋 FSM Activity Log Entries: $ACTIVITY_COUNT"
echo ""

# Recent activities
echo "🔄 Recent FSM Activities (last 10):"
kubectl exec -n agi deployment/redis -- redis-cli LRANGE "fsm:agent_1:activity_log" 0 9 2>/dev/null | \
  jq -r '.message' 2>/dev/null | head -10 | sed 's/^/   /'
echo ""

# Tool executions in last 30 minutes
echo "🔧 Tool Executions (last 30 min):"
TOOL_EXECS=$(kubectl -n agi logs deployment/hdn-server-rpi58 --since=30m | grep -c "\[API\] Tool call executed" || echo "0")
echo "   $TOOL_EXECS tool executions"
echo ""

# Recent tool executions
echo "   Recent tools executed:"
kubectl -n agi logs deployment/hdn-server-rpi58 --since=30m | \
  grep "\[API\] Tool call executed" | \
  tail -10 | \
  sed 's/.*Tool call executed: /   - /' | \
  sort | uniq
echo ""

# FSM state transitions
echo "🔄 FSM State Activity (last 30 min):"
FSM_ACTIVITY=$(kubectl -n agi logs deployment/fsm-server-rpi58 --since=30m | grep -c "📋\|💭\|🔍\|✅\|❌" || echo "0")
echo "   $FSM_ACTIVITY activity events"
echo ""

# Goals status
echo "🎯 Goals Status:"
ACTIVE=$(kubectl exec -n agi deployment/fsm-server-rpi58 -- wget -qO- --timeout=5 http://goal-manager.agi.svc.cluster.local:8090/goals/agent_1/active 2>/dev/null | jq '. | length' || echo "0")
TRIGGERED=$(kubectl exec -n agi deployment/redis -- redis-cli SCARD "fsm:agent_1:goals:triggered" 2>/dev/null || echo "0")
echo "   Active goals: $ACTIVE"
echo "   Triggered goals: $TRIGGERED"
echo ""

# Cronjob status
echo "⏰ Scheduled Jobs:"
NEWS_JOBS=$(kubectl -n agi get pods -l app=news-ingestor-cronjob --field-selector=status.phase=Succeeded 2>/dev/null | tail -n +2 | wc -l | tr -d ' ')
WIKI_BOOTSTRAP=$(kubectl -n agi get pods -l app=wiki-bootstrapper-cronjob --field-selector=status.phase=Running 2>/dev/null | tail -n +2 | wc -l | tr -d ' ')
WIKI_SUMMARIZE=$(kubectl -n agi get pods -l app=wiki-summarizer-cronjob --field-selector=status.phase=Succeeded 2>/dev/null | tail -n +2 | wc -l | tr -d ' ')
echo "   News ingestor jobs completed: $NEWS_JOBS"
echo "   Wiki bootstrapper running: $WIKI_BOOTSTRAP"
echo "   Wiki summarizer jobs completed: $WIKI_SUMMARIZE"
echo ""

# System health
echo "💚 System Health:"
PODS_READY=$(kubectl -n agi get pods --field-selector=status.phase=Running 2>/dev/null | grep -c "1/1" || echo "0")
PODS_TOTAL=$(kubectl -n agi get pods --field-selector=status.phase=Running 2>/dev/null | tail -n +2 | wc -l | tr -d ' ')
echo "   Pods running: $PODS_READY/$PODS_TOTAL"
echo ""

# Recent errors
echo "⚠️  Recent Errors (last 30 min):"
ERRORS=$(kubectl -n agi logs deployment/fsm-server-rpi58 --since=30m 2>/dev/null | grep -ci "error\|Error\|ERROR\|failed\|Failed\|FAILED" || echo "0")
echo "   FSM errors: $ERRORS"
HDN_ERRORS=$(kubectl -n agi logs deployment/hdn-server-rpi58 --since=30m 2>/dev/null | grep -ci "error\|Error\|ERROR\|failed\|Failed\|FAILED" || echo "0")
echo "   HDN errors: $HDN_ERRORS"
echo ""

echo "=============================="
echo "✅ Summary complete"
echo ""
echo "💡 The system is actively:"
echo "   - Running FSM autonomy cycles (perceive -> learn -> hypothesize -> reason -> plan)"
echo "   - Executing tools and storing results"
echo "   - Generating hypotheses and reasoning about them"
echo "   - Processing scheduled jobs (news, wiki)"
echo ""
echo "⚠️  Note: Goals are still stuck (marked as triggered but not completing)"
echo "   Run ./fix-stuck-goals.sh to clear them if needed"

