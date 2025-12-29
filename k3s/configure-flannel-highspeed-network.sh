#!/bin/bash
# Configure Flannel/k3s to use high-speed network for inter-node communication
# This should be run on each node

set -e

if [ "$EUID" -ne 0 ]; then 
    echo "❌ This script must be run as root (use sudo)"
    exit 1
fi

echo "=========================================="
echo "Configuring k3s/Flannel for High-Speed Network"
echo "=========================================="
echo ""

# Detect high-speed network interface
echo "1. Detecting network interfaces:"
HIGH_SPEED_IF=""
HIGH_SPEED_IP=""

# Check for 192.168.10.x network (high-speed network)
for iface in eth1 eth0 enp1s0; do
    if ip addr show $iface > /dev/null 2>&1; then
        IP=$(ip addr show $iface | grep "inet 192.168.10" | awk '{print $2}' | cut -d/ -f1)
        if [ -n "$IP" ]; then
            HIGH_SPEED_IF=$iface
            HIGH_SPEED_IP=$IP
            echo "   ✅ Found high-speed network: $iface ($IP)"
            break
        fi
    fi
done

if [ -z "$HIGH_SPEED_IF" ]; then
    echo "   ⚠️  Could not detect high-speed network interface"
    echo "   Please specify manually:"
    echo "   export HIGH_SPEED_IF=eth1"
    echo "   export HIGH_SPEED_IP=192.168.10.X"
    exit 1
fi

echo ""
echo "2. Current k3s configuration:"
if [ -f /etc/rancher/k3s/config.yaml ]; then
    echo "   Config file exists:"
    cat /etc/rancher/k3s/config.yaml | grep -E "node-ip|flannel" || echo "   (no node-ip or flannel config found)"
else
    echo "   No config file found"
fi

echo ""
echo "3. Configuring k3s to use high-speed network:"
mkdir -p /etc/rancher/k3s

# Create or update config
if [ -f /etc/rancher/k3s/config.yaml ]; then
    # Backup existing config
    cp /etc/rancher/k3s/config.yaml /etc/rancher/k3s/config.yaml.bak
    echo "   ✅ Backed up existing config"
fi

# Add node-ip to use high-speed network
cat > /etc/rancher/k3s/config.yaml <<EOF
node-ip: $HIGH_SPEED_IP
flannel-backend: vxlan
EOF

echo "   ✅ Created config:"
cat /etc/rancher/k3s/config.yaml

echo ""
echo "4. Restarting k3s agent to apply changes:"
if systemctl is-active --quiet k3s-agent; then
    echo "   Restarting k3s-agent..."
    systemctl restart k3s-agent
    echo "   ✅ k3s-agent restarted"
elif systemctl is-active --quiet k3s; then
    echo "   Restarting k3s (master)..."
    systemctl restart k3s
    echo "   ✅ k3s restarted"
else
    echo "   ⚠️  k3s service not found or not running"
fi

echo ""
echo "=========================================="
echo "✅ Configuration Applied!"
echo "=========================================="
echo ""
echo "k3s will now use $HIGH_SPEED_IF ($HIGH_SPEED_IP) for node communication"
echo "Inter-node pod traffic should now use the high-speed network"
echo ""
echo "Note: You need to run this on ALL nodes (rpi58, rpi5b, rpi4-4)"

