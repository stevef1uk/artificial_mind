#!/bin/bash
# Simple diagnostic script for rpi4-4 pod routing issues
# Run this ON rpi4-4 node

set -e

echo "=========================================="
echo "rpi4-4 Pod Routing Diagnostic"
echo "=========================================="
echo ""

echo "1. Node routing table:"
echo "----------------------"
ip route show
echo ""

echo "2. Default routes:"
echo "------------------"
ip route | grep default
echo ""

echo "3. Network interfaces:"
echo "---------------------"
ip addr show | grep -E '^[0-9]+:|inet ' | head -20
echo ""

echo "4. Testing internet connectivity from node:"
echo "-------------------------------------------"
echo "Testing via eth0 (high-performance network):"
if ping -c 2 -I eth0 8.8.8.8 2>&1 | grep -q "64 bytes"; then
    echo "  ✅ eth0 CAN reach internet"
else
    echo "  ❌ eth0 CANNOT reach internet"
fi
echo ""

echo "Testing via wlan0 (WiFi):"
if ping -c 2 -I wlan0 8.8.8.8 2>&1 | grep -q "64 bytes"; then
    echo "  ✅ wlan0 CAN reach internet"
else
    echo "  ❌ wlan0 CANNOT reach internet"
fi
echo ""

echo "5. CNI interface (cni0):"
echo "-----------------------"
if ip addr show cni0 > /dev/null 2>&1; then
    ip addr show cni0
    echo ""
    echo "Pod network gateway (should be 10.42.1.1):"
    ip route | grep "10.42.1.0/24"
else
    echo "  ⚠️  cni0 interface not found"
fi
echo ""

echo "6. NAT/Masquerade rules:"
echo "-----------------------"
if command -v iptables > /dev/null 2>&1; then
    echo "POSTROUTING rules for pod network:"
    iptables -t nat -L POSTROUTING -n -v | grep -E "MASQUERADE|10.42.1" || echo "  ⚠️  No masquerade rules found for pod network"
    echo ""
    echo "All POSTROUTING rules:"
    iptables -t nat -L POSTROUTING -n -v | head -10
else
    echo "  ⚠️  iptables not found (may be using nftables)"
    if command -v nft > /dev/null 2>&1; then
        echo "  Checking nftables:"
        nft list ruleset | grep -i masquerade | head -5
    fi
fi
echo ""

echo "7. Testing pod network gateway:"
echo "-----------------------------"
POD_GW="10.42.1.1"
echo "Gateway: $POD_GW"
if ping -c 2 $POD_GW 2>&1 | grep -q "64 bytes"; then
    echo "  ✅ Gateway is reachable"
else
    echo "  ❌ Gateway is NOT reachable"
fi
echo ""

echo "8. Testing external IP from node:"
echo "---------------------------------"
if ping -c 2 151.101.0.81 2>&1 | grep -q "64 bytes"; then
    echo "  ✅ Node CAN reach external IP (151.101.0.81)"
else
    echo "  ❌ Node CANNOT reach external IP"
fi
echo ""

echo "=========================================="
echo "Summary:"
echo "=========================================="
echo ""

# Determine which interface has internet
HAS_ETH0_INTERNET=false
HAS_WLAN0_INTERNET=false

if ping -c 1 -I eth0 8.8.8.8 > /dev/null 2>&1; then
    HAS_ETH0_INTERNET=true
fi

if ping -c 1 -I wlan0 8.8.8.8 > /dev/null 2>&1; then
    HAS_WLAN0_INTERNET=true
fi

if [ "$HAS_ETH0_INTERNET" = true ]; then
    echo "✅ eth0 (high-performance network) HAS internet"
    echo "   Pods should route through this interface"
elif [ "$HAS_WLAN0_INTERNET" = true ]; then
    echo "⚠️  eth0 (high-performance network) does NOT have internet"
    echo "✅ wlan0 (WiFi) HAS internet"
    echo ""
    echo "RECOMMENDATION: Configure CNI to route pod traffic via wlan0"
    echo "   Or ensure high-performance network gateway has internet"
else
    echo "❌ Neither interface has internet connectivity"
fi

echo ""
echo "If pods still can't reach internet:"
echo "  1. Check NAT masquerade rules (step 6 above)"
echo "  2. Verify pod network gateway can route to external IPs"
echo "  3. Check if firewall is blocking outbound traffic"
echo ""

