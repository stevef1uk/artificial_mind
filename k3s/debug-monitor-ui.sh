#!/bin/bash
# Debug script to check Monitor UI deployment

NAMESPACE="agi"
POD_NAME=$(kubectl get pods -n ${NAMESPACE} -l app=monitor-ui -o jsonpath='{.items[0].metadata.name}')

if [ -z "$POD_NAME" ]; then
    echo "❌ Monitor UI pod not found!"
    exit 1
fi

echo "🔍 Checking Monitor UI pod: ${POD_NAME}"
echo ""

echo "📦 Image being used:"
kubectl get pod ${POD_NAME} -n ${NAMESPACE} -o jsonpath='{.spec.containers[0].image}' && echo ""
echo ""

echo "📅 Pod creation time:"
kubectl get pod ${POD_NAME} -n ${NAMESPACE} -o jsonpath='{.metadata.creationTimestamp}' && echo ""
echo ""

echo "🔍 Checking if agents-tab exists in deployed HTML:"
kubectl exec -n ${NAMESPACE} ${POD_NAME} -- sh -c "grep -c 'agents-tab' /tmp/unpack/monitor/templates/dashboard_tabs.html 2>/dev/null || echo 'File not found or grep failed'"
echo ""

echo "🔍 Checking if Agents button exists:"
kubectl exec -n ${NAMESPACE} ${POD_NAME} -- sh -c "grep -c \"switchTab('agents')\" /tmp/unpack/monitor/templates/dashboard_tabs.html 2>/dev/null || echo 'File not found or grep failed'"
echo ""

echo "📋 Pod logs (last 20 lines):"
kubectl logs -n ${NAMESPACE} ${POD_NAME} --tail=20
echo ""

echo "💡 To force pull new image, delete the pod:"
echo "   kubectl delete pod ${POD_NAME} -n ${NAMESPACE}"


