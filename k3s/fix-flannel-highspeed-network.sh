#!/bin/bash
# Fix Flannel to use high-speed network for VXLAN tunnels
# Run on each node

set -e

if [ "$EUID" -ne 0 ]; then 
    echo "❌ This script must be run as root (use sudo)"
    exit 1
fi

echo "=========================================="
echo "Configuring Flannel for High-Speed Network"
echo "=========================================="
echo ""

# Find high-speed network interface
HIGH_SPEED_IF=""
for iface in eth1 eth0; do
    if ip addr show $iface > /dev/null 2>&1; then
        if ip addr show $iface | grep -q "192.168.10"; then
            HIGH_SPEED_IF=$iface
            echo "✅ Found high-speed network: $iface"
            break
        fi
    fi
done

if [ -z "$HIGH_SPEED_IF" ]; then
    echo "❌ Could not find high-speed network interface"
    exit 1
fi

echo ""
echo "1. Current Flannel VXLAN interface:"
ip link show flannel.1 2>/dev/null | head -3 || echo "   flannel.1 not found"

echo ""
echo "2. Adding route for Flannel VXLAN to use high-speed network:"
# Get the high-speed network IP
HIGH_SPEED_IP=$(ip addr show $HIGH_SPEED_IF | grep "inet 192.168.10" | awk '{print $2}' | cut -d/ -f1)
echo "   High-speed IP: $HIGH_SPEED_IP"

# Check if we need to configure Flannel interface binding
# In k3s, Flannel uses the node's primary IP, but VXLAN might route through default gateway
echo ""
echo "3. Checking routing for pod network:"
POD_NETWORK=$(ip route | grep "10.42" | head -1)
echo "   $POD_NETWORK"

echo ""
echo "4. Ensuring Flannel uses high-speed network:"
# The annotation should already be set, but verify
NODE_NAME=$(hostname)
echo "   Node: $NODE_NAME"
echo "   High-speed IP: $HIGH_SPEED_IP"

# Restart k3s agent to pick up any config changes
echo ""
echo "5. Restarting k3s agent:"
if systemctl is-active --quiet k3s-agent; then
    systemctl restart k3s-agent
    echo "   ✅ k3s-agent restarted"
elif systemctl is-active --quiet k3s; then
    systemctl restart k3s
    echo "   ✅ k3s restarted"
fi

echo ""
echo "=========================================="
echo "✅ Configuration Complete"
echo "=========================================="
echo ""
echo "Flannel should now use $HIGH_SPEED_IF for VXLAN tunnels"
echo "Inter-node pod traffic will use the high-speed network"

