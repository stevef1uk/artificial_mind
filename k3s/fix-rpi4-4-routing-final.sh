#!/bin/bash
# Final fix for rpi4-4 pod routing - ensures pod traffic routes via wlan0
# Run with: sudo ./fix-rpi4-4-routing-final.sh

set -e

if [ "$EUID" -ne 0 ]; then 
    echo "❌ This script must be run as root (use sudo)"
    exit 1
fi

echo "=========================================="
echo "Final Fix for rpi4-4 Pod Internet Routing"
echo "=========================================="
echo ""

# Step 1: Ensure NAT masquerade exists
echo "1. Ensuring NAT masquerade rule exists:"
if iptables -t nat -C POSTROUTING -s 10.42.1.0/24 -o wlan0 -j MASQUERADE 2>/dev/null; then
    echo "   ✅ Masquerade rule already exists"
else
    iptables -t nat -A POSTROUTING -s 10.42.1.0/24 -o wlan0 -j MASQUERADE
    echo "   ✅ Added masquerade rule"
fi
echo ""

# Step 2: Add policy routing for pod network to use wlan0
echo "2. Adding policy routing for pod network:"
WLAN0_GW=$(ip route | grep "default.*wlan0" | awk '{print $3}')
WLAN0_IP=$(ip addr show wlan0 | grep "inet " | awk '{print $2}' | cut -d/ -f1)

if [ -z "$WLAN0_GW" ] || [ -z "$WLAN0_IP" ]; then
    echo "   ❌ Could not determine wlan0 gateway or IP"
    exit 1
fi

echo "   WiFi gateway: $WLAN0_GW"
echo "   WiFi IP: $WLAN0_IP"
echo ""

# Step 3: Add route for pod network subnet to use wlan0
echo "3. Adding route for pod network via wlan0:"
# Remove any existing route for pod network
ip route del 10.42.1.0/24 dev cni0 2>/dev/null || true
# Add route via wlan0 gateway
ip route add 10.42.1.0/24 via $WLAN0_GW dev wlan0 2>/dev/null || echo "   ℹ️  Route may already exist or CNI manages it"
echo "   ✅ Route configured"
echo ""

# Step 4: Add source-based routing rule (more reliable)
echo "4. Adding source-based routing rule:"
# Create custom routing table for pod network
if ! grep -q "200 pod-network" /etc/iproute2/rt_tables 2>/dev/null; then
    echo "200 pod-network" >> /etc/iproute2/rt_tables
    echo "   ✅ Added custom routing table"
fi

# Add route in custom table
ip route add default via $WLAN0_GW dev wlan0 table pod-network 2>/dev/null || true
# Add rule to use custom table for pod network source
ip rule del from 10.42.1.0/24 table pod-network 2>/dev/null || true
ip rule add from 10.42.1.0/24 table pod-network
echo "   ✅ Source-based routing configured"
echo ""

# Step 5: Verify configuration
echo "5. Verifying configuration:"
echo "   NAT masquerade:"
iptables -t nat -L POSTROUTING -n | grep "10.42.1.0/24.*wlan0" || echo "   ⚠️  Not found"
echo ""
echo "   Routing rules:"
ip rule show | grep "10.42.1.0/24" || echo "   ⚠️  Not found"
echo ""
echo "   Routes in pod-network table:"
ip route show table pod-network | grep default || echo "   ⚠️  Not found"
echo ""

# Step 6: Test from node
echo "6. Testing node connectivity:"
if ping -c 2 -I wlan0 8.8.8.8 > /dev/null 2>&1; then
    echo "   ✅ Node can reach internet via wlan0"
else
    echo "   ❌ Node cannot reach internet via wlan0"
fi
echo ""

echo "=========================================="
echo "✅ Fix Applied!"
echo "=========================================="
echo ""
echo "Changes made:"
echo "  1. NAT masquerade: 10.42.1.0/24 -> wlan0"
echo "  2. Source-based routing: pod network traffic routes via wlan0"
echo ""
echo "Next steps:"
echo "  1. Test from a pod on rpi4-4"
echo "  2. If still not working, may need to restart k3s agent:"
echo "     sudo systemctl restart k3s-agent"
echo ""

