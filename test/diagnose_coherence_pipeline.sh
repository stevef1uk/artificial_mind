#!/bin/bash

# Full diagnostic of coherence monitor → Monitor Service → Goal Manager pipeline

NAMESPACE="${K8S_NAMESPACE:-agi}"

echo "🔍 Coherence Monitor Pipeline Diagnostic"
echo "========================================"
echo ""

FSM_POD=$(kubectl get pods -n "$NAMESPACE" -l app=fsm-server-rpi58 --sort-by=.metadata.creationTimestamp -o jsonpath='{.items[-1].metadata.name}' 2>/dev/null)
REDIS_POD=$(kubectl get pods -n "$NAMESPACE" -l app=redis -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)

echo "1️⃣ Check Coherence Monitor Activity:"
echo "------------------------------------"
if [ -n "$FSM_POD" ]; then
    echo "   FSM Pod: $FSM_POD"
    echo ""
    
    # Check if monitor started
    STARTED=$(kubectl logs -n "$NAMESPACE" "$FSM_POD" --tail=1000 | grep -i "Coherence monitoring loop started" | tail -1)
    if [ -n "$STARTED" ]; then
        echo "   ✅ Coherence monitor started"
        echo "   $STARTED" | sed 's/^/      /'
    else
        echo "   ❌ Coherence monitor not started!"
    fi
    echo ""
    
    # Check for coherence checks
    CHECK_COUNT=$(kubectl logs -n "$NAMESPACE" "$FSM_POD" --tail=1000 2>/dev/null | grep -c "Starting cross-system coherence check" 2>/dev/null || echo "0")
    CHECK_COUNT=$(echo "$CHECK_COUNT" | tr -d '\n' | tr -d ' ')
    echo "   Coherence checks run: $CHECK_COUNT"
    
    # Check for detected inconsistencies
    INC_COUNT=$(kubectl logs -n "$NAMESPACE" "$FSM_POD" --tail=1000 2>/dev/null | grep -c "Detected.*inconsistencies" 2>/dev/null || echo "0")
    INC_COUNT=$(echo "$INC_COUNT" | tr -d '\n' | tr -d ' ')
    echo "   Inconsistencies detected: $INC_COUNT"
    
    # Check for goal generation
    GOAL_GEN=$(kubectl logs -n "$NAMESPACE" "$FSM_POD" --tail=1000 2>/dev/null | grep -c "Generated resolution task for inconsistency" 2>/dev/null || echo "0")
    GOAL_GEN=$(echo "$GOAL_GEN" | tr -d '\n' | tr -d ' ')
    echo "   Resolution goals generated: $GOAL_GEN"
    echo ""
    
    # Show recent coherence activity
    echo "   Recent coherence activity (last 10):"
    RECENT=$(kubectl logs -n "$NAMESPACE" "$FSM_POD" --tail=500 | grep -iE "\[Coherence\]" | tail -10)
    if [ -n "$RECENT" ]; then
        echo "$RECENT" | sed 's/^/      /'
    else
        echo "      ℹ️  No recent coherence activity"
    fi
else
    echo "   ❌ FSM pod not found"
fi
echo ""

echo "2️⃣ Check Redis for Coherence Data:"
echo "----------------------------------"
if [ -n "$REDIS_POD" ]; then
    # Check inconsistencies
    INC_KEY="coherence:inconsistencies:agent_1"
    INC_COUNT=$(kubectl exec -n "$NAMESPACE" "$REDIS_POD" -- redis-cli LLEN "$INC_KEY" 2>/dev/null || echo "0")
    echo "   Inconsistencies stored: $INC_COUNT"
    
    # Check coherence goals
    COHERENCE_KEY="reasoning:curiosity_goals:system_coherence"
    COHERENCE_COUNT=$(kubectl exec -n "$NAMESPACE" "$REDIS_POD" -- redis-cli LLEN "$COHERENCE_KEY" 2>/dev/null || echo "0")
    echo "   Coherence goals in Redis: $COHERENCE_COUNT"
    
    if [ "$COHERENCE_COUNT" -gt 0 ]; then
        echo ""
        echo "   Sample coherence goals:"
        kubectl exec -n "$NAMESPACE" "$REDIS_POD" -- redis-cli LRANGE "$COHERENCE_KEY" 0 2 2>/dev/null | \
            while read -r line; do
                if [ -n "$line" ] && [ "$line" != "(nil)" ]; then
                    echo "$line" | jq -r '"      - ID: \(.id), Status: \(.status), Description: \(.description[0:60])..."' 2>/dev/null || echo "      - $line"
                fi
            done | head -5
    fi
    
    # Check goal mappings
    MAPPING_COUNT=$(kubectl exec -n "$NAMESPACE" "$REDIS_POD" -- redis-cli KEYS "coherence:goal_mapping:*" 2>/dev/null | wc -l | tr -d ' ')
    echo ""
    echo "   Goal mappings: $MAPPING_COUNT"
    
    # Check reflection tasks
    REFLECTION_KEY="coherence:reflection_tasks:agent_1"
    REFLECTION_COUNT=$(kubectl exec -n "$NAMESPACE" "$REDIS_POD" -- redis-cli LLEN "$REFLECTION_KEY" 2>/dev/null || echo "0")
    echo "   Reflection tasks: $REFLECTION_COUNT"
else
    echo "   ❌ Redis pod not found"
fi
echo ""

echo "3️⃣ Check Monitor Service Processing:"
echo "------------------------------------"
MONITOR_POD=$(kubectl get pods -n "$NAMESPACE" -l app=monitor-ui -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
if [ -n "$MONITOR_POD" ]; then
    # Check if curiosity goal consumer started
    CONSUMER_STARTED=$(kubectl logs -n "$NAMESPACE" "$MONITOR_POD" --tail=1000 | grep -i "curiosity goal consumer\|Checking.*system_coherence" | head -1)
    if [ -n "$CONSUMER_STARTED" ]; then
        echo "   ✅ Curiosity goal consumer started"
    else
        echo "   ⚠️  Curiosity goal consumer may not be running"
    fi
    
    # Check for system_coherence domain processing
    SYSTEM_COHERENCE_CHECKS=$(kubectl logs -n "$NAMESPACE" "$MONITOR_POD" --tail=500 | grep -c "Checking system_coherence" || echo "0")
    echo "   system_coherence checks: $SYSTEM_COHERENCE_CHECKS"
    
    # Check for conversions
    CONVERSIONS=$(kubectl logs -n "$NAMESPACE" "$MONITOR_POD" --tail=500 | grep "✅ Converted curiosity goal.*system_coherence" | wc -l | tr -d ' ')
    echo "   Coherence goal conversions: $CONVERSIONS"
    
    # Show recent system_coherence activity
    echo ""
    echo "   Recent system_coherence activity:"
    RECENT=$(kubectl logs -n "$NAMESPACE" "$MONITOR_POD" --tail=200 | grep -iE "system_coherence|coherence.*goal" | tail -10)
    if [ -n "$RECENT" ]; then
        echo "$RECENT" | sed 's/^/      /'
    else
        echo "      ℹ️  No recent system_coherence activity"
    fi
else
    echo "   ❌ Monitor pod not found"
fi
echo ""

echo "4️⃣ Check Goal Manager for Coherence Goals:"
echo "------------------------------------------"
GOAL_MGR_POD=$(kubectl get pods -n "$NAMESPACE" -l app=goal-manager -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
if [ -n "$GOAL_MGR_POD" ]; then
    # Get all active goals and filter for coherence
    ALL_GOALS=$(kubectl exec -n "$NAMESPACE" "$GOAL_MGR_POD" -- wget -qO- "http://localhost:8090/goals/agent_1/active" 2>/dev/null)
    if [ -n "$ALL_GOALS" ]; then
        COHERENCE_ACTIVE=$(echo "$ALL_GOALS" | jq '[.[] | select(.context.domain == "system_coherence")] | length' 2>/dev/null || echo "0")
        echo "   Active coherence goals in Goal Manager: $COHERENCE_ACTIVE"
        
        if [ "$COHERENCE_ACTIVE" -gt 0 ]; then
            echo ""
            echo "   Sample coherence goals:"
            echo "$ALL_GOALS" | jq -r '.[] | select(.context.domain == "system_coherence") | "      - \(.id): \(.description[0:60])... (context: \(.context))"' 2>/dev/null | head -5
        fi
    fi
    
    # Check for debug messages
    echo ""
    echo "   Recent Goal Manager debug messages:"
    DEBUG_MSGS=$(kubectl logs -n "$NAMESPACE" "$GOAL_MGR_POD" --tail=100 | grep -E "📥|✅|🐛|DEBUG.*goal" | tail -5)
    if [ -n "$DEBUG_MSGS" ]; then
        echo "$DEBUG_MSGS" | sed 's/^/      /'
    else
        echo "      ℹ️  No debug messages (may be old code or no goals received)"
    fi
else
    echo "   ❌ Goal Manager pod not found"
fi
echo ""

echo "💡 Diagnosis Summary:"
echo "-------------------"
if [ "$COHERENCE_COUNT" -eq 0 ] && [ "$INC_COUNT" -gt 0 ]; then
    echo "   ⚠️  Inconsistencies detected but no coherence goals in Redis"
    echo "      → Coherence monitor may not be generating goals"
    echo "      → Or goals are being consumed immediately"
fi

if [ "$COHERENCE_COUNT" -gt 0 ] && [ "$CONVERSIONS" -eq 0 ]; then
    echo "   ⚠️  Coherence goals exist but Monitor Service isn't converting them"
    echo "      → Monitor Service may not be processing system_coherence domain"
    echo "      → Or Monitor Service needs to be rebuilt"
fi

if [ "$CHECK_COUNT" -eq 0 ]; then
    echo "   ⚠️  Coherence monitor hasn't run any checks"
    echo "      → Monitor may not have started"
    echo "      → Or checks are taking a very long time"
fi

if [ "$SYSTEM_COHERENCE_CHECKS" -eq 0 ]; then
    echo "   ⚠️  Monitor Service hasn't checked system_coherence domain"
    echo "      → Consumer may not be running"
    echo "      → Or domain list doesn't include system_coherence"
fi
echo ""

echo "🔧 Recommended Actions:"
echo "----------------------"
echo "   1. If coherence monitor not running: Check FSM logs for errors"
echo "   2. If no goals generated: Wait for next coherence check (runs every 5 min)"
echo "   3. If Monitor Service not converting: Rebuild Monitor Service"
echo "   4. If Goal Manager not receiving: Check network connectivity"
echo ""

