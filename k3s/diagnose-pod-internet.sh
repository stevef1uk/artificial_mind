#!/bin/bash
# Diagnose pod internet connectivity issues

echo "🔍 Pod Internet Connectivity Diagnostic"
echo "========================================"
echo ""

# Check if we're in the right namespace
NAMESPACE="${NAMESPACE:-agi}"

echo "1. Testing DNS Resolution:"
echo "---------------------------"
POD=$(kubectl -n $NAMESPACE get pods -l job-name --field-selector status.phase=Running -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
if [ -z "$POD" ]; then
    echo "   ⚠️  No running pods found. Creating test pod..."
    kubectl run -it --rm test-internet --image=busybox --restart=Never -n $NAMESPACE -- sh -c "
        echo 'Testing DNS...'
        nslookup www.bbc.com || echo 'DNS failed'
        echo ''
        echo 'Testing connectivity...'
        ping -c 2 8.8.8.8 || echo 'Ping failed'
        echo ''
        echo 'Testing HTTP...'
        wget -O- --timeout=5 https://www.bbc.com/news 2>&1 | head -5 || echo 'HTTP failed'
    " 2>/dev/null
else
    echo "   Using pod: $POD"
    echo ""
    echo "   DNS Test (www.bbc.com):"
    kubectl -n $NAMESPACE exec $POD -- nslookup www.bbc.com 2>&1 | head -5 || echo "   ❌ DNS failed"
    echo ""
    echo "   Connectivity Test (8.8.8.8):"
    kubectl -n $NAMESPACE exec $POD -- ping -c 2 8.8.8.8 2>&1 || echo "   ❌ Ping failed"
    echo ""
    echo "   HTTP Test (www.bbc.com):"
    kubectl -n $NAMESPACE exec $POD -- wget -O- --timeout=5 https://www.bbc.com/news 2>&1 | head -5 || echo "   ❌ HTTP failed"
fi

echo ""
echo "2. Pod Network Configuration:"
echo "-----------------------------"
if [ -n "$POD" ]; then
    echo "   Pod IP:"
    kubectl -n $NAMESPACE get pod $POD -o jsonpath='{.status.podIP}' 2>/dev/null
    echo ""
    echo ""
    echo "   Network Interfaces:"
    kubectl -n $NAMESPACE exec $POD -- ip addr show 2>/dev/null | grep -E '^[0-9]+:|inet ' | head -10 || echo "   ⚠️  Cannot access pod"
    echo ""
    echo "   Routing Table:"
    kubectl -n $NAMESPACE exec $POD -- ip route show 2>/dev/null | head -10 || echo "   ⚠️  Cannot access pod"
    echo ""
    echo "   DNS Configuration:"
    kubectl -n $NAMESPACE exec $POD -- cat /etc/resolv.conf 2>/dev/null || echo "   ⚠️  Cannot access pod"
fi

echo ""
echo "3. Node Network Configuration:"
echo "-------------------------------"
NODES=$(kubectl get nodes -o jsonpath='{.items[*].metadata.name}' 2>/dev/null)
for NODE in $NODES; do
    echo "   Node: $NODE"
    NODE_IP=$(kubectl get node $NODE -o jsonpath='{.status.addresses[?(@.type=="InternalIP")].address}' 2>/dev/null)
    echo "   IP: $NODE_IP"
    
    # Try to get network info from node
    echo "   Testing internet from node..."
    kubectl debug node/$NODE -it --image=busybox -- sh -c "ping -c 2 8.8.8.8" 2>/dev/null | grep -E 'packets|time=' || echo "   ⚠️  Cannot test from node"
    echo ""
done

echo ""
echo "4. CNI Configuration:"
echo "---------------------"
echo "   Checking Flannel config..."
kubectl get configmap kube-flannel-cfg -n kube-system -o yaml 2>/dev/null | grep -A 5 "Network\|Backend" || echo "   ⚠️  Flannel config not found or not accessible"

echo ""
echo "   Checking Flannel pods:"
kubectl get pods -n kube-system -l app=flannel 2>/dev/null || echo "   ⚠️  Flannel pods not found"

echo ""
echo "5. Recommendations:"
echo "------------------"
echo "   If DNS fails:"
echo "   - Check dnsPolicy and dnsConfig in pod specs"
echo "   - Verify CoreDNS is running: kubectl get pods -n kube-system -l k8s-app=kube-dns"
echo ""
echo "   If connectivity fails:"
echo "   - Check CNI routing configuration"
echo "   - Verify nodes can reach internet (they should be able to)"
echo "   - Check if high-performance network is interfering with pod routing"
echo "   - See k3s/POD_INTERNET_ACCESS_FIX.md for solutions"
echo ""
echo "========================================"

