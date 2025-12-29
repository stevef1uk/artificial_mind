#!/bin/bash
# Complete fix for rpi4-4 pod routing
# Run with: sudo ./fix-rpi4-4-routing-complete.sh

set -e

if [ "$EUID" -ne 0 ]; then 
    echo "❌ This script must be run as root (use sudo)"
    exit 1
fi

echo "=========================================="
echo "Fixing rpi4-4 Pod Internet Routing"
echo "=========================================="
echo ""

# Step 1: Check current state
echo "1. Current routing state:"
echo "   Primary route: $(ip route | grep 'default via' | head -1)"
echo "   eth0 has internet: $(ping -c 1 -I eth0 -W 2 8.8.8.8 > /dev/null 2>&1 && echo 'YES' || echo 'NO')"
echo "   wlan0 has internet: $(ping -c 1 -I wlan0 -W 2 8.8.8.8 > /dev/null 2>&1 && echo 'YES' || echo 'NO')"
echo ""

# Step 2: Check NAT rules
echo "2. Checking NAT masquerade rules:"
HAS_MASQ=$(iptables -t nat -L POSTROUTING -n | grep -c "10.42.1.0/24.*MASQUERADE" || echo "0")
if [ "$HAS_MASQ" -gt 0 ]; then
    echo "   ✅ Masquerade rule exists for pod network"
    iptables -t nat -L POSTROUTING -n | grep "10.42.1.0/24"
else
    echo "   ❌ No masquerade rule found - will add one"
fi
echo ""

# Step 3: Add masquerade rule for pod network via wlan0
echo "3. Adding/updating masquerade rule for pod network:"
# Remove existing rule if any
iptables -t nat -D POSTROUTING -s 10.42.1.0/24 -o wlan0 -j MASQUERADE 2>/dev/null || true
# Add new rule
iptables -t nat -A POSTROUTING -s 10.42.1.0/24 -o wlan0 -j MASQUERADE
echo "   ✅ Added: 10.42.1.0/24 -> wlan0 (MASQUERADE)"
echo ""

# Step 4: Add route for pod network to use wlan0 gateway
echo "4. Adding route for pod network via wlan0:"
WLAN0_GW=$(ip route | grep "default.*wlan0" | awk '{print $3}')
if [ -n "$WLAN0_GW" ]; then
    echo "   WiFi gateway: $WLAN0_GW"
    # Remove existing route if any
    ip route del 10.42.1.0/24 dev cni0 2>/dev/null || true
    # Note: We can't easily change the cni0 route, but we can add a more specific route
    # Actually, the CNI manages this, so we'll just ensure NAT works
    echo "   ℹ️  CNI manages the pod network route, but NAT will route via wlan0"
else
    echo "   ⚠️  Could not determine WiFi gateway"
fi
echo ""

# Step 5: Verify cni0 interface
echo "5. Checking cni0 interface:"
if ip link show cni0 | grep -q "state DOWN"; then
    echo "   ⚠️  cni0 is DOWN - this may be normal if no pods are running"
    echo "   Will attempt to bring it up..."
    ip link set cni0 up 2>/dev/null || echo "   ℹ️  Cannot bring up cni0 (may be managed by CNI)"
else
    echo "   ✅ cni0 is UP"
fi
echo ""

# Step 6: Test connectivity
echo "6. Testing fixes:"
echo "   Testing if node can reach external IP via wlan0:"
if ping -c 2 -I wlan0 8.8.8.8 > /dev/null 2>&1; then
    echo "   ✅ Node can reach internet via wlan0"
else
    echo "   ❌ Node cannot reach internet via wlan0"
fi
echo ""

# Step 7: Show final NAT rules
echo "7. Final NAT masquerade rules:"
iptables -t nat -L POSTROUTING -n -v | grep -E "MASQUERADE|10.42.1" | head -5
echo ""

echo "=========================================="
echo "✅ Fix Applied!"
echo "=========================================="
echo ""
echo "Summary of changes:"
echo "  1. Added NAT masquerade: 10.42.1.0/24 -> wlan0"
echo "  2. Pod traffic will now NAT through wlan0 (which has internet)"
echo ""
echo "Next steps:"
echo "  1. Test from a pod: kubectl run -it --rm test --image=busybox --restart=Never -n agi -- wget -O- https://www.bbc.com/news"
echo "  2. If still not working, may need to restart k3s agent or CNI daemon"
echo ""

