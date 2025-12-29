#!/bin/bash

# Quick script to check intelligence status on k3s
# Run from k3s directory

NAMESPACE="${K8S_NAMESPACE:-agi}"

echo "🧠 Intelligence Status Check"
echo "==========================="
echo ""

# Get pod names
HDN_POD=$(kubectl get pods -n "$NAMESPACE" --no-headers 2>/dev/null | grep -E "hdn.*Running" | awk '{print $1}' | head -1)
REDIS_POD=$(kubectl get pods -n "$NAMESPACE" --no-headers 2>/dev/null | grep -E "redis.*Running" | awk '{print $1}' | head -1)

if [ -z "$HDN_POD" ]; then
  echo "❌ HDN pod not found"
  exit 1
fi

echo "HDN Pod: $HDN_POD"
[ -n "$REDIS_POD" ] && echo "Redis Pod: $REDIS_POD"
echo ""

# Check Redis learning data
if [ -n "$REDIS_POD" ]; then
  echo "📊 Learning Data in Redis:"
  echo "---------------------------"
  
  PATTERNS=$(kubectl exec -n "$NAMESPACE" "$REDIS_POD" -- redis-cli KEYS "failure_pattern:*" 2>/dev/null | wc -l | tr -d ' ')
  STRATEGIES=$(kubectl exec -n "$NAMESPACE" "$REDIS_POD" -- redis-cli KEYS "codegen_strategy:*" 2>/dev/null | wc -l | tr -d ' ')
  HINTS=$(kubectl exec -n "$NAMESPACE" "$REDIS_POD" -- redis-cli KEYS "prevention_hint:*" 2>/dev/null | wc -l | tr -d ' ')
  
  echo "  Failure patterns: $PATTERNS"
  echo "  Code generation strategies: $STRATEGIES"
  echo "  Prevention hints: $HINTS"
  echo ""
  
  if [ "$PATTERNS" -gt "0" ] || [ "$STRATEGIES" -gt "0" ]; then
    echo "✅ Learning data exists!"
    echo ""
    
    # Show some example patterns
    if [ "$PATTERNS" -gt "0" ]; then
      echo "Example failure patterns:"
      kubectl exec -n "$NAMESPACE" "$REDIS_POD" -- redis-cli KEYS "failure_pattern:*" 2>/dev/null | head -3 | sed 's/^/  - /'
      echo ""
    fi
    
    # Show some example strategies
    if [ "$STRATEGIES" -gt "0" ]; then
      echo "Example strategies:"
      kubectl exec -n "$NAMESPACE" "$REDIS_POD" -- redis-cli KEYS "codegen_strategy:*" 2>/dev/null | head -3 | sed 's/^/  - /'
      echo ""
    fi
  else
    echo "ℹ️  No learning data yet"
  fi
fi

# Check logs for intelligence activity
echo "📝 Recent Intelligence Activity in Logs:"
echo "------------------------------------------"
INTELLIGENCE_LOGS=$(kubectl logs -n "$NAMESPACE" "$HDN_POD" --tail=500 2>/dev/null | grep -i "intelligence\|learned\|prevention" | tail -10)

if [ -n "$INTELLIGENCE_LOGS" ]; then
  echo "$INTELLIGENCE_LOGS" | sed 's/^/  /'
  echo ""
  echo "✅ Intelligence is active!"
else
  echo "  ℹ️  No intelligence messages in recent logs"
  echo "  (This is normal if system hasn't executed many tasks yet)"
  echo ""
  echo "  To see intelligence in action:"
  echo "  1. Run some code generation tasks"
  echo "  2. Watch logs: kubectl logs -n $NAMESPACE -f $HDN_POD | grep -i intelligence"
fi
echo ""

echo "==========================="
echo "Summary:"
echo "  Learning data: ✅ Found ($PATTERNS patterns, $STRATEGIES strategies)"
echo "  Intelligence: $(if [ -n "$INTELLIGENCE_LOGS" ]; then echo "✅ Active"; else echo "⏳ Waiting for activity"; fi)"
echo ""

