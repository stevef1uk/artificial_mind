#!/bin/bash
# Fix routing on rpi4-4 to enable internet access
# This script should be run ON rpi4-4 node

echo "🔧 Fixing rpi4-4 Internet Routing"
echo "=================================="
echo ""

# Check current routing
echo "Current routing table:"
ip route show
echo ""

# Check network interfaces
echo "Network interfaces:"
ip addr show
echo ""

# Find the default gateway (usually .1 on the subnet)
GATEWAY=$(ip route | grep default | awk '{print $3}' | head -1)
if [ -z "$GATEWAY" ]; then
    # Try to determine gateway from network
    INTERFACE=$(ip route | grep default | awk '{print $5}' | head -1)
    if [ -n "$INTERFACE" ]; then
        NETWORK=$(ip -4 addr show $INTERFACE | grep inet | awk '{print $2}' | cut -d/ -f1 | cut -d. -f1-3)
        GATEWAY="${NETWORK}.1"
        echo "⚠️  No default gateway found. Assuming ${GATEWAY} based on network ${NETWORK}.0/24"
    else
        echo "❌ Cannot determine gateway. Please set GATEWAY manually:"
        echo "   export GATEWAY=192.168.10.1  # or your actual gateway IP"
        exit 1
    fi
else
    echo "Found gateway: $GATEWAY"
fi

# Find the interface that should have internet (usually the one with the node IP)
INTERFACE=$(ip route | grep default | awk '{print $5}' | head -1)
if [ -z "$INTERFACE" ]; then
    # Try to find interface with 192.168.10.x
    INTERFACE=$(ip -4 addr show | grep "192.168.10" | grep -v "127.0.0.1" | awk '{print $NF}' | head -1)
    echo "⚠️  No default route found. Using interface: $INTERFACE"
fi

if [ -z "$INTERFACE" ]; then
    echo "❌ Cannot determine interface. Please check network configuration."
    exit 1
fi

echo "Using interface: $INTERFACE"
echo ""

# Check if default route exists
if ip route | grep -q "^default"; then
    echo "Default route exists. Checking if it's correct..."
    CURRENT_GW=$(ip route | grep "^default" | awk '{print $3}' | head -1)
    if [ "$CURRENT_GW" != "$GATEWAY" ]; then
        echo "⚠️  Current gateway ($CURRENT_GW) differs from expected ($GATEWAY)"
        echo "Removing old default route..."
        sudo ip route del default 2>/dev/null || true
    fi
fi

# Add/update default route
if ! ip route | grep -q "^default"; then
    echo "Adding default route via $GATEWAY on $INTERFACE..."
    sudo ip route add default via $GATEWAY dev $INTERFACE
    if [ $? -eq 0 ]; then
        echo "✅ Default route added"
    else
        echo "❌ Failed to add default route"
        exit 1
    fi
else
    echo "✅ Default route already exists"
fi

# Test connectivity
echo ""
echo "Testing internet connectivity..."
if ping -c 2 8.8.8.8 > /dev/null 2>&1; then
    echo "✅ Internet connectivity restored!"
else
    echo "❌ Still cannot reach internet. Check:"
    echo "   1. Gateway IP is correct: $GATEWAY"
    echo "   2. Interface is correct: $INTERFACE"
    echo "   3. Gateway is reachable: ping $GATEWAY"
    echo "   4. Firewall rules allow outbound traffic"
fi

# Make it persistent (for systemd-based systems)
if command -v systemctl > /dev/null 2>&1; then
    echo ""
    echo "To make this persistent, you may need to:"
    echo "1. Edit /etc/systemd/network/ files, or"
    echo "2. Add to /etc/rc.local, or"
    echo "3. Use NetworkManager or netplan configuration"
fi

