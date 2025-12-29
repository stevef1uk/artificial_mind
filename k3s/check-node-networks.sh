#!/bin/bash
# Check which network interfaces Kubernetes nodes are using

echo "🔍 Kubernetes Node Network Analysis"
echo "===================================="
echo ""

echo "1. Node IP Addresses:"
kubectl get nodes -o wide
echo ""

echo "2. Network Interface Details:"
echo ""

# Check each node's network interfaces
for node in rpi58 rpi4-4 rpi5b; do
    echo "--- $node ---"
    echo "IP Address:"
    kubectl get node $node -o jsonpath='{.status.addresses[?(@.type=="InternalIP")].address}' 2>/dev/null
    echo ""
    
    echo "Network Interfaces (from node):"
    kubectl debug node/$node -it --image=busybox -- sh -c "ip addr show 2>/dev/null | grep -E '^[0-9]+:|inet ' | head -20" 2>/dev/null || \
    echo "   (Cannot access node directly - checking via pod)"
    echo ""
    
    # Try to get network info from a pod on that node
    POD=$(kubectl -n agi get pods -o wide --field-selector spec.nodeName=$node 2>/dev/null | tail -n +2 | head -1 | awk '{print $1}')
    if [ -n "$POD" ]; then
        echo "Network info from pod $POD on $node:"
        kubectl -n agi exec $POD -- ip addr show 2>/dev/null | grep -E '^[0-9]+:|inet ' | head -10 || echo "   (Cannot access)"
    fi
    echo ""
done

echo "3. Network Subnet Analysis:"
echo ""
echo "   rpi58:    192.168.10.3  (Subnet: 192.168.10.0/24)"
echo "   rpi4-4:   192.168.1.80  (Subnet: 192.168.1.0/24)"
echo "   rpi5b:    192.168.1.63  (Subnet: 192.168.1.0/24)"
echo ""
echo "⚠️  WARNING: rpi58 is on a DIFFERENT subnet than the other nodes!"
echo "   This suggests:"
echo "   - rpi58 might be on wired network (192.168.10.x)"
echo "   - rpi4-4 and rpi5b might be on WiFi (192.168.1.x)"
echo "   - Cross-subnet communication may be slower"
echo ""

echo "4. Testing Node-to-Node Connectivity:"
echo ""

# Test connectivity between nodes
test_connectivity() {
    local from=$1
    local to=$2
    local to_ip=$3
    
    echo "   Testing $from -> $to ($to_ip):"
    POD=$(kubectl -n agi get pods -o wide --field-selector spec.nodeName=$from 2>/dev/null | tail -n +2 | head -1 | awk '{print $1}')
    if [ -n "$POD" ]; then
        START=$(date +%s%N)
        kubectl -n agi exec $POD -- timeout 2 ping -c 1 $to_ip 2>/dev/null > /dev/null
        END=$(date +%s%N)
        DURATION=$(( (END - START) / 1000000 ))
        if [ $? -eq 0 ]; then
            echo "      ✅ Connected (${DURATION}ms)"
        else
            echo "      ❌ Failed or timeout"
        fi
    else
        echo "      ⚠️  No pod on $from to test from"
    fi
}

# Get node IPs
RPI58_IP=$(kubectl get node rpi58 -o jsonpath='{.status.addresses[?(@.type=="InternalIP")].address}' 2>/dev/null)
RPI4_IP=$(kubectl get node rpi4-4 -o jsonpath='{.status.addresses[?(@.type=="InternalIP")].address}' 2>/dev/null)
RPI5B_IP=$(kubectl get node rpi5b -o jsonpath='{.status.addresses[?(@.type=="InternalIP")].address}' 2>/dev/null)

if [ -n "$RPI58_IP" ] && [ -n "$RPI4_IP" ]; then
    test_connectivity rpi58 rpi4-4 $RPI4_IP
fi
if [ -n "$RPI58_IP" ] && [ -n "$RPI5B_IP" ]; then
    test_connectivity rpi58 rpi5b $RPI5B_IP
fi
if [ -n "$RPI4_IP" ] && [ -n "$RPI5B_IP" ]; then
    test_connectivity rpi4-4 rpi5b $RPI5B_IP
fi

echo ""
echo "5. Service-to-Service Communication Test:"
echo ""

# Test service communication latency
test_service() {
    local service=$1
    local url=$2
    
    echo "   Testing $service:"
    POD=$(kubectl -n agi get pods -l app=monitor-ui -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
    if [ -n "$POD" ]; then
        START=$(date +%s%N)
        kubectl -n agi exec $POD -- timeout 3 wget -qO- --timeout=3 "$url" > /dev/null 2>&1
        END=$(date +%s%N)
        DURATION=$(( (END - START) / 1000000 ))
        if [ $? -eq 0 ]; then
            echo "      ✅ Responded (${DURATION}ms)"
        else
            echo "      ❌ Timeout or failed"
        fi
    fi
}

test_service "HDN" "http://hdn-server-rpi58.agi.svc.cluster.local:8080/health"
test_service "FSM" "http://fsm-server-rpi58.agi.svc.cluster.local:8083/health"
test_service "Redis" "redis://redis.agi.svc.cluster.local:6379"

echo ""
echo "===================================="
echo "📊 Summary:"
echo ""
echo "If rpi58 is on a different subnet (192.168.10.x vs 192.168.1.x):"
echo "  - This could cause slower inter-node communication"
echo "  - Traffic may route through a gateway/router"
echo "  - WiFi nodes (192.168.1.x) may have higher latency"
echo ""
echo "Recommendations:"
echo "  1. Check if rpi58 is on wired network (faster)"
echo "  2. Consider moving all nodes to same subnet for better performance"
echo "  3. If using WiFi, ensure 5GHz band for better speed"
echo "  4. Check router/gateway configuration for inter-subnet routing"
echo ""

