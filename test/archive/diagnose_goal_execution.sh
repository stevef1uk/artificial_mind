#!/bin/bash

# Diagnose why coherence goals aren't being executed

NAMESPACE="${K8S_NAMESPACE:-agi}"

echo "🔍 Diagnosing Goal Execution Pipeline"
echo "======================================"
echo ""

# Get pods
GOAL_MGR_POD=$(kubectl get pods -n "$NAMESPACE" -l app=goal-manager -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
FSM_POD=$(kubectl get pods -n "$NAMESPACE" -l app=fsm-server-rpi58 --sort-by=.metadata.creationTimestamp -o jsonpath='{.items[-1].metadata.name}' 2>/dev/null)
REDIS_POD=$(kubectl get pods -n "$NAMESPACE" -l app=redis -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)

echo "1️⃣ Check Goal Manager HTTP API:"
echo "--------------------------------"
if [ -n "$GOAL_MGR_POD" ]; then
    echo "   Testing Goal Manager endpoint..."
    GOAL_MGR_SVC="goal-manager.${NAMESPACE}.svc.cluster.local:8090"
    
    # Try to get active goals
    ACTIVE_GOALS=$(kubectl exec -n "$NAMESPACE" "$GOAL_MGR_POD" -- wget -qO- "http://localhost:8090/goals/agent_1/active" 2>/dev/null)
    if [ $? -eq 0 ] && [ -n "$ACTIVE_GOALS" ]; then
        GOAL_COUNT=$(echo "$ACTIVE_GOALS" | jq 'length' 2>/dev/null || echo "0")
        echo "   ✅ Goal Manager API responding"
        echo "   📊 Active goals: $GOAL_COUNT"
        
        if [ "$GOAL_COUNT" -gt 0 ]; then
            echo ""
            echo "   Active goals:"
            echo "$ACTIVE_GOALS" | jq -r '.[] | "      - \(.id): \(.description[0:60])... (status: \(.status), priority: \(.priority))"' 2>/dev/null | head -10
            
            # Check for coherence goals
            COHERENCE_COUNT=$(echo "$ACTIVE_GOALS" | jq '[.[] | select(.context.domain == "system_coherence")] | length' 2>/dev/null || echo "0")
            echo ""
            echo "   Coherence goals (active): $COHERENCE_COUNT"
            
            if [ "$COHERENCE_COUNT" -gt 0 ]; then
                echo ""
                echo "   Coherence goals details:"
                echo "$ACTIVE_GOALS" | jq -r '.[] | select(.context.domain == "system_coherence") | "      - \(.id): \(.description[0:50])... (context: \(.context))"' 2>/dev/null
            fi
        fi
    else
        echo "   ❌ Goal Manager API not responding or empty response"
        echo "   Checking Goal Manager logs..."
        kubectl logs -n "$NAMESPACE" "$GOAL_MGR_POD" --tail=20 | grep -iE "error|listen|started" | tail -5 | sed 's/^/      /'
    fi
else
    echo "   ⚠️  Goal Manager pod not found"
fi
echo ""

echo "2️⃣ Check FSM Goals Poller:"
echo "--------------------------"
if [ -n "$FSM_POD" ]; then
    echo "   Checking FSM logs for goal polling activity..."
    
    # Check for polling messages
    POLLING_LOGS=$(kubectl logs -n "$NAMESPACE" "$FSM_POD" --tail=500 2>/dev/null | grep -iE "\[FSM\]\[Goals\].*poll|\[FSM\]\[Goals\].*fetch|\[FSM\]\[Goals\].*retrieve" | tail -10)
    if [ -n "$POLLING_LOGS" ]; then
        echo "   ✅ FSM is polling for goals:"
        echo "$POLLING_LOGS" | sed 's/^/      /'
    else
        echo "   ⚠️  No goal polling activity found"
    fi
    echo ""
    
    # Check for goal triggers
    TRIGGER_LOGS=$(kubectl logs -n "$NAMESPACE" "$FSM_POD" --tail=500 2>/dev/null | grep -iE "\[FSM\]\[Goals\].*triggered" | tail -10)
    if [ -n "$TRIGGER_LOGS" ]; then
        echo "   Recent goal triggers:"
        echo "$TRIGGER_LOGS" | sed 's/^/      /'
    else
        echo "   ⚠️  No goal triggers found"
    fi
    echo ""
    
    # Check for workflow completions
    COMPLETION_LOGS=$(kubectl logs -n "$NAMESPACE" "$FSM_POD" --tail=500 2>/dev/null | grep -iE "\[FSM\]\[Goals\].*completed|workflow.*completed" | tail -10)
    if [ -n "$COMPLETION_LOGS" ]; then
        echo "   Recent workflow completions:"
        echo "$COMPLETION_LOGS" | sed 's/^/      /'
    fi
else
    echo "   ⚠️  FSM pod not found"
fi
echo ""

echo "3️⃣ Check Redis for Goal Manager Data:"
echo "-------------------------------------"
if [ -n "$REDIS_POD" ]; then
    # Check active goals set
    ACTIVE_SET=$(kubectl exec -n "$NAMESPACE" "$REDIS_POD" -- redis-cli SMEMBERS "goals:agent_1:active" 2>/dev/null | head -20)
    ACTIVE_COUNT=$(echo "$ACTIVE_SET" | grep -v "^$" | wc -l | tr -d ' ')
    echo "   Active goals in Redis: $ACTIVE_COUNT"
    
    if [ "$ACTIVE_COUNT" -gt 0 ]; then
        echo ""
        echo "   Sample goal IDs:"
        echo "$ACTIVE_SET" | head -5 | sed 's/^/      - /'
        
        # Check one goal's details
        if [ -n "$ACTIVE_SET" ]; then
            SAMPLE_GOAL=$(echo "$ACTIVE_SET" | head -1 | tr -d '\n' | tr -d ' ')
            if [ -n "$SAMPLE_GOAL" ]; then
                GOAL_DATA=$(kubectl exec -n "$NAMESPACE" "$REDIS_POD" -- redis-cli GET "goal:${SAMPLE_GOAL}" 2>/dev/null)
                if [ -n "$GOAL_DATA" ]; then
                    echo ""
                    echo "   Sample goal ($SAMPLE_GOAL) details:"
                    echo "$GOAL_DATA" | jq -r '"      Description: \(.description[0:60])...", "      Status: \(.status)", "      Priority: \(.priority)", "      Context: \(.context // "none")"' 2>/dev/null | sed 's/^/      /'
                fi
            fi
        fi
    else
        echo "   ⚠️  No active goals in Redis"
    fi
    
    # Check history
    HISTORY_COUNT=$(kubectl exec -n "$NAMESPACE" "$REDIS_POD" -- redis-cli SCARD "goals:agent_1:history" 2>/dev/null || echo "0")
    echo ""
    echo "   Archived goals (history): $HISTORY_COUNT"
    
    if [ "$HISTORY_COUNT" -gt 0 ]; then
        # Check recent archived goals for coherence
        RECENT_HISTORY=$(kubectl exec -n "$NAMESPACE" "$REDIS_POD" -- redis-cli SMEMBERS "goals:agent_1:history" 2>/dev/null | head -10)
        COHERENCE_ARCHIVED=0
        for goal_id in $RECENT_HISTORY; do
            goal_data=$(kubectl exec -n "$NAMESPACE" "$REDIS_POD" -- redis-cli GET "goal:${goal_id}" 2>/dev/null)
            if echo "$goal_data" | jq -e '.context.domain == "system_coherence"' >/dev/null 2>&1; then
                COHERENCE_ARCHIVED=$((COHERENCE_ARCHIVED + 1))
            fi
        done
        echo "   Coherence goals in history: $COHERENCE_ARCHIVED"
    fi
else
    echo "   ⚠️  Redis pod not found"
fi
echo ""

echo "4️⃣ Check Monitor Service Conversion:"
echo "-----------------------------------"
MONITOR_POD=$(kubectl get pods -n "$NAMESPACE" -l app=monitor-ui -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
if [ -n "$MONITOR_POD" ]; then
    echo "   Recent conversions (last 10):"
    CONVERSIONS=$(kubectl logs -n "$NAMESPACE" "$MONITOR_POD" --tail=500 2>/dev/null | grep "✅ Converted curiosity goal.*to Goal Manager task" | tail -10)
    if [ -n "$CONVERSIONS" ]; then
        echo "$CONVERSIONS" | sed 's/^/      /'
    else
        echo "      ℹ️  No recent conversions found"
    fi
    echo ""
    
    # Check for errors
    ERRORS=$(kubectl logs -n "$NAMESPACE" "$MONITOR_POD" --tail=500 2>/dev/null | grep -iE "error.*goal|failed.*goal.*manager" | tail -5)
    if [ -n "$ERRORS" ]; then
        echo "   ⚠️  Errors found:"
        echo "$ERRORS" | sed 's/^/      /'
    fi
else
    echo "   ⚠️  Monitor pod not found"
fi
echo ""

echo "💡 Summary & Next Steps:"
echo "----------------------"
echo "   If goals are in Goal Manager but not being triggered:"
echo "      → Check FSM Goals Poller configuration"
echo "      → Verify FSM can reach Goal Manager API"
echo ""
echo "   If goals are being archived immediately:"
echo "      → Check Goal Manager logs for auto-achievement logic"
echo "      → Verify goal status isn't being set to 'achieved' on creation"
echo ""
echo "   If Monitor Service isn't converting:"
echo "      → Check Monitor Service logs for errors"
echo "      → Verify system_coherence domain is in the processing list"
echo ""

