#!/bin/bash
# Fix pod routing on rpi4-4
# This script should be run ON rpi4-4 node

echo "🔧 Fixing rpi4-4 Pod Internet Routing"
echo "======================================"
echo ""

# Check current routing
echo "1. Current routing table:"
ip route show
echo ""

# Check which interface has internet
echo "2. Testing internet connectivity:"
echo "   Testing via eth0 (high-performance network):"
ping -c 2 -I eth0 8.8.8.8 2>&1 | head -3 || echo "   ❌ eth0 cannot reach internet"
echo ""
echo "   Testing via wlan0 (WiFi):"
ping -c 2 -I wlan0 8.8.8.8 2>&1 | head -3 || echo "   ❌ wlan0 cannot reach internet"
echo ""

# Check CNI interface
echo "3. CNI interface (cni0) configuration:"
ip addr show cni0 2>/dev/null || echo "   ⚠️  cni0 not found"
echo ""

# Check if pod network gateway can route
echo "4. Pod network gateway:"
POD_GW=$(ip route | grep "10.42.1.0/24" | awk '{print $1}' | cut -d/ -f1 | sed 's/0$/1/')
if [ -z "$POD_GW" ]; then
    POD_GW="10.42.1.1"
fi
echo "   Gateway: $POD_GW"
ping -c 2 $POD_GW 2>&1 | head -3
echo ""

# Check NAT rules
echo "5. Checking NAT/Masquerade rules:"
if command -v iptables > /dev/null 2>&1; then
    echo "   POSTROUTING rules:"
    iptables -t nat -L POSTROUTING -n -v | grep -E "MASQUERADE|10.42.1" | head -5
    echo ""
    echo "   Checking if masquerade exists for pod network:"
    if iptables -t nat -L POSTROUTING -n | grep -q "10.42.1.0/24"; then
        echo "   ✅ Masquerade rule exists for pod network"
    else
        echo "   ❌ No masquerade rule for pod network!"
        echo ""
        echo "   Adding masquerade rule..."
        # Find the interface that has internet
        if ping -c 1 -I wlan0 8.8.8.8 > /dev/null 2>&1; then
            INTERFACE="wlan0"
            echo "   Using wlan0 (has internet)"
        elif ping -c 1 -I eth0 8.8.8.8 > /dev/null 2>&1; then
            INTERFACE="eth0"
            echo "   Using eth0 (has internet)"
        else
            echo "   ⚠️  Cannot determine which interface has internet"
            INTERFACE="eth0"  # Default
        fi
        
        # Add masquerade rule
        iptables -t nat -A POSTROUTING -s 10.42.1.0/24 -o $INTERFACE -j MASQUERADE
        echo "   ✅ Added masquerade rule: 10.42.1.0/24 -> $INTERFACE"
    fi
else
    echo "   ⚠️  iptables not found (may be using nftables)"
fi
echo ""

# Check if default route is correct
echo "6. Default route analysis:"
PRIMARY_GW=$(ip route | grep "default via" | head -1 | awk '{print $3}')
PRIMARY_IF=$(ip route | grep "default via" | head -1 | awk '{print $5}')
echo "   Primary default route: via $PRIMARY_GW dev $PRIMARY_IF"
echo "   Testing if primary gateway has internet:"
ping -c 2 $PRIMARY_GW 2>&1 | head -3
echo ""

# If high-performance network doesn't have internet, suggest fix
if [ "$PRIMARY_IF" = "eth0" ] || [ "$PRIMARY_IF" = "eth1" ]; then
    echo "7. ⚠️  WARNING: Primary route uses high-performance network ($PRIMARY_IF)"
    echo "   If this network doesn't have internet, pods won't work."
    echo ""
    echo "   Options:"
    echo "   a) Ensure high-performance network gateway (192.168.10.1) has internet"
    echo "   b) Change CNI to use WiFi interface (wlan0) for egress"
    echo "   c) Add route for pod network to use WiFi interface"
    echo ""
    echo "   To route pod traffic via WiFi:"
    echo "   sudo ip route add 10.42.1.0/24 via <wifi-gateway> dev wlan0"
    echo ""
fi

echo "======================================"
echo "📊 Summary:"
echo ""
echo "If pods still can't reach internet after this:"
echo "  1. Check if high-performance network (192.168.10.x) has internet gateway"
echo "  2. Consider configuring CNI to use WiFi interface for egress"
echo "  3. Check firewall rules blocking outbound traffic"
echo ""

