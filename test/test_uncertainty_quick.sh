#!/bin/bash

# Quick test for uncertainty modeling - just checks if data exists

REDIS_HOST="${REDIS_HOST:-localhost}"
REDIS_PORT="${REDIS_PORT:-6379}"

echo "🔍 Quick Uncertainty Model Check"
echo "=================================="
echo ""

# Check hypotheses
HYP_COUNT=$(redis-cli -h "$REDIS_HOST" -p "$REDIS_PORT" HLEN "fsm:agent_1:hypotheses" 2>/dev/null || echo "0")
if [ "$HYP_COUNT" -gt 0 ]; then
  HYP_SAMPLE=$(redis-cli -h "$REDIS_HOST" -p "$REDIS_PORT" HGETALL "fsm:agent_1:hypotheses" 2>/dev/null | head -2 | tail -1)
  if echo "$HYP_SAMPLE" | grep -q "uncertainty"; then
    echo "✅ Hypotheses: $HYP_COUNT found, uncertainty models present"
  else
    echo "⚠️  Hypotheses: $HYP_COUNT found, but no uncertainty models"
  fi
else
  echo "ℹ️  Hypotheses: None found (trigger hypothesis generation)"
fi

# Check goals
GOAL_COUNT=$(redis-cli -h "$REDIS_HOST" -p "$REDIS_PORT" LLEN "reasoning:curiosity_goals:General" 2>/dev/null || echo "0")
if [ "$GOAL_COUNT" -gt 0 ]; then
  GOAL_SAMPLE=$(redis-cli -h "$REDIS_HOST" -p "$REDIS_PORT" LRANGE "reasoning:curiosity_goals:General" 0 0 2>/dev/null)
  if echo "$GOAL_SAMPLE" | grep -q "uncertainty"; then
    echo "✅ Goals: $GOAL_COUNT found, uncertainty models present"
  else
    echo "⚠️  Goals: $GOAL_COUNT found, but no uncertainty models"
  fi
else
  echo "ℹ️  Goals: None found (trigger goal generation)"
fi

# Check beliefs
BELIEF_COUNT=$(redis-cli -h "$REDIS_HOST" -p "$REDIS_PORT" LLEN "reasoning:beliefs:General" 2>/dev/null || echo "0")
if [ "$BELIEF_COUNT" -gt 0 ]; then
  BELIEF_SAMPLE=$(redis-cli -h "$REDIS_HOST" -p "$REDIS_PORT" LRANGE "reasoning:beliefs:General" 0 0 2>/dev/null)
  if echo "$BELIEF_SAMPLE" | grep -q "uncertainty"; then
    echo "✅ Beliefs: $BELIEF_COUNT found, uncertainty models present"
  else
    echo "⚠️  Beliefs: $BELIEF_COUNT found, but no uncertainty models"
  fi
else
  echo "ℹ️  Beliefs: None found (trigger inference)"
fi

echo ""
echo "For detailed testing, run: ./test/test_uncertainty_modeling.sh"

