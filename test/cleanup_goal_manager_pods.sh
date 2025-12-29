#!/bin/bash

# Clean up stuck Goal Manager pods

NAMESPACE="${K8S_NAMESPACE:-agi}"

echo "🧹 Cleaning Up Goal Manager Pods"
echo "================================="
echo ""

echo "📦 Current Goal Manager Pods:"
echo "---------------------------"
kubectl get pods -n "$NAMESPACE" -l app=goal-manager -o wide
echo ""

# Get all Goal Manager pods
PODS=$(kubectl get pods -n "$NAMESPACE" -l app=goal-manager -o jsonpath='{.items[*].metadata.name}' 2>/dev/null)

if [ -z "$PODS" ]; then
    echo "ℹ️  No Goal Manager pods found"
    exit 0
fi

echo "🔍 Checking Pod Status:"
echo "----------------------"
STUCK_PODS=()
TERMINATING_PODS=()
HEALTHY_PODS=()

for pod in $PODS; do
    STATUS=$(kubectl get pod -n "$NAMESPACE" "$pod" -o jsonpath='{.status.phase}' 2>/dev/null)
    READY=$(kubectl get pod -n "$NAMESPACE" "$pod" -o jsonpath='{.status.containerStatuses[0].ready}' 2>/dev/null)
    RESTARTS=$(kubectl get pod -n "$NAMESPACE" "$pod" -o jsonpath='{.status.containerStatuses[0].restartCount}' 2>/dev/null)
    AGE=$(kubectl get pod -n "$NAMESPACE" "$pod" -o jsonpath='{.metadata.creationTimestamp}' 2>/dev/null)
    
    echo "   $pod:"
    echo "      Status: $STATUS"
    echo "      Ready: $READY"
    echo "      Restarts: $RESTARTS"
    echo "      Age: $AGE"
    
    # Check if pod is stuck
    if [ "$STATUS" = "Terminating" ]; then
        TERMINATING_PODS+=("$pod")
        echo "      ⚠️  Pod is stuck in Terminating state"
    elif [ "$STATUS" != "Running" ] || [ "$READY" != "true" ]; then
        STUCK_PODS+=("$pod")
        echo "      ⚠️  Pod appears stuck (Status: $STATUS, Ready: $READY)"
    else
        HEALTHY_PODS+=("$pod")
        echo "      ✅ Pod is healthy"
    fi
    echo ""
done

# Count pods
TOTAL_PODS=$(echo $PODS | wc -w | tr -d ' ')
STUCK_COUNT=${#STUCK_PODS[@]}
TERMINATING_COUNT=${#TERMINATING_PODS[@]}
HEALTHY_COUNT=${#HEALTHY_PODS[@]}

echo "📊 Summary:"
echo "----------"
echo "   Total pods: $TOTAL_PODS"
echo "   Healthy: $HEALTHY_COUNT"
echo "   Stuck: $STUCK_COUNT"
echo "   Terminating: $TERMINATING_COUNT"
echo ""

# Clean up stuck pods
if [ $STUCK_COUNT -gt 0 ] || [ $TERMINATING_COUNT -gt 0 ]; then
    echo "🧹 Cleaning up stuck pods..."
    echo "---------------------------"
    
    # Force delete terminating pods
    if [ $TERMINATING_COUNT -gt 0 ]; then
        for pod in "${TERMINATING_PODS[@]}"; do
            echo "   Force deleting terminating pod: $pod"
            kubectl delete pod -n "$NAMESPACE" "$pod" --force --grace-period=0 2>/dev/null || true
        done
    fi
    
    # Delete stuck pods
    if [ $STUCK_COUNT -gt 0 ]; then
        for pod in "${STUCK_PODS[@]}"; do
            echo "   Deleting stuck pod: $pod"
            kubectl delete pod -n "$NAMESPACE" "$pod" --grace-period=30 2>/dev/null || true
        done
    fi
    
    echo ""
    echo "⏳ Waiting for new pods to start..."
    sleep 5
    
    # Wait for deployment to be ready
    echo "   Waiting for deployment rollout..."
    kubectl rollout status deployment/goal-manager -n "$NAMESPACE" --timeout=60s 2>/dev/null || echo "   ⚠️  Rollout check timed out"
    
    echo ""
    echo "📦 Updated Pod Status:"
    echo "---------------------"
    kubectl get pods -n "$NAMESPACE" -l app=goal-manager -o wide
    echo ""
    
    # Verify at least one healthy pod
    NEW_PODS=$(kubectl get pods -n "$NAMESPACE" -l app=goal-manager -o jsonpath='{.items[*].metadata.name}' 2>/dev/null)
    HEALTHY_NEW=0
    for pod in $NEW_PODS; do
        STATUS=$(kubectl get pod -n "$NAMESPACE" "$pod" -o jsonpath='{.status.phase}' 2>/dev/null)
        READY=$(kubectl get pod -n "$NAMESPACE" "$pod" -o jsonpath='{.status.containerStatuses[0].ready}' 2>/dev/null)
        if [ "$STATUS" = "Running" ] && [ "$READY" = "true" ]; then
            HEALTHY_NEW=$((HEALTHY_NEW + 1))
        fi
    done
    
    if [ $HEALTHY_NEW -gt 0 ]; then
        echo "✅ Cleanup complete! $HEALTHY_NEW healthy pod(s) running"
    else
        echo "⚠️  Cleanup complete, but no healthy pods found yet"
        echo "   Check: kubectl describe deployment goal-manager -n $NAMESPACE"
    fi
else
    echo "✅ No stuck pods found - all pods are healthy!"
fi

echo ""
echo "💡 To check Goal Manager logs:"
echo "   kubectl logs -f -n $NAMESPACE -l app=goal-manager"
echo ""

