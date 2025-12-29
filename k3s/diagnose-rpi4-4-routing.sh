#!/bin/bash
# Diagnose routing issues on rpi4-4 that prevent pods from reaching external IPs

echo "🔍 Diagnosing rpi4-4 Pod Routing Issues"
echo "========================================"
echo ""

echo "1. Node-level connectivity (from rpi4-4):"
echo "-------------------------------------------"
echo "Testing from node itself..."
kubectl debug node/rpi4-4 -it --image=busybox -- sh -c "
  echo 'Node can ping external IP:'
  ping -c 2 151.101.0.81 2>&1 | head -3
  echo ''
  echo 'Node can reach HTTPS:'
  wget -O- --timeout=3 https://www.bbc.com/news 2>&1 | head -2 || echo 'HTTPS failed'
" 2>/dev/null || echo "⚠️  Cannot test from node directly"

echo ""
echo "2. Pod network routing (from pod on rpi4-4):"
echo "----------------------------------------------"
POD=$(kubectl get pods -n agi --field-selector spec.nodeName=rpi4-4 -o jsonpath='{.items[0].metadata.name}' 2>/dev/null | head -1)
if [ -n "$POD" ]; then
    echo "Using pod: $POD"
    echo ""
    echo "Pod routing table:"
    kubectl exec -n agi $POD -- ip route show 2>/dev/null
    echo ""
    echo "Pod default gateway:"
    kubectl exec -n agi $POD -- ip route | grep default 2>/dev/null
    echo ""
    echo "Pod can ping gateway:"
    GATEWAY=$(kubectl exec -n agi $POD -- ip route | grep default | awk '{print $3}' 2>/dev/null)
    if [ -n "$GATEWAY" ]; then
        echo "  Gateway: $GATEWAY"
        kubectl exec -n agi $POD -- ping -c 2 $GATEWAY 2>&1 | head -3
    fi
    echo ""
    echo "Pod can ping external IP:"
    kubectl exec -n agi $POD -- ping -c 2 151.101.0.81 2>&1 | head -3
    echo ""
    echo "Pod can reach external HTTPS:"
    kubectl exec -n agi $POD -- wget -O- --timeout=3 https://www.bbc.com/news 2>&1 | head -2 || echo "  ❌ HTTPS failed"
else
    echo "⚠️  No pod found on rpi4-4"
fi

echo ""
echo "3. Comparing with working node (rpi58):"
echo "----------------------------------------"
POD_RPI58=$(kubectl get pods -n agi --field-selector spec.nodeName=rpi58 -o jsonpath='{.items[0].metadata.name}' 2>/dev/null | head -1)
if [ -n "$POD_RPI58" ]; then
    echo "Using pod on rpi58: $POD_RPI58"
    echo ""
    echo "rpi58 pod routing table:"
    kubectl exec -n agi $POD_RPI58 -- ip route show 2>/dev/null
    echo ""
    echo "rpi58 pod default gateway:"
    kubectl exec -n agi $POD_RPI58 -- ip route | grep default 2>/dev/null
    echo ""
    echo "rpi58 pod can ping external IP:"
    kubectl exec -n agi $POD_RPI58 -- ping -c 2 151.101.0.81 2>&1 | head -3
else
    echo "⚠️  No pod found on rpi58"
fi

echo ""
echo "4. Checking CNI configuration:"
echo "-------------------------------"
echo "Checking Flannel config:"
kubectl get configmap kube-flannel-cfg -n kube-system -o yaml 2>/dev/null | grep -A 10 "Network\|Backend" | head -15 || echo "⚠️  Flannel config not found"

echo ""
echo "Checking node annotations:"
kubectl get node rpi4-4 -o jsonpath='{.metadata.annotations}' 2>/dev/null | grep -i flannel || echo "  No Flannel annotations"

echo ""
echo "5. Checking iptables/NAT rules (from pod):"
echo "-------------------------------------------"
if [ -n "$POD" ]; then
    echo "Pod can see iptables rules:"
    kubectl exec -n agi $POD -- iptables -t nat -L -n 2>/dev/null | head -10 || echo "  ⚠️  Cannot check iptables (may need privileged access)"
fi

echo ""
echo "========================================"
echo "📊 Summary:"
echo ""
echo "If node can reach internet but pods cannot:"
echo "  - Check pod network gateway routing"
echo "  - Check CNI masquerade/NAT rules"
echo "  - Check if high-performance network interface is interfering"
echo "  - Compare with working node (rpi58) configuration"
echo ""

