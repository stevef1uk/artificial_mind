#!/bin/bash
# Verify rpi4-4 routing fix
# Run on rpi4-4

echo "Verifying rpi4-4 routing configuration..."
echo ""

echo "1. NAT Masquerade rules:"
sudo iptables -t nat -L POSTROUTING -n -v | grep -A 2 -B 2 "10.42.1"
echo ""

echo "2. Routing rules (ip rule):"
ip rule show | grep "10.42.1"
echo ""

echo "3. Routes in pod-network table:"
ip route show table pod-network
echo ""

echo "4. Testing from node (should work):"
ping -c 2 -I wlan0 8.8.8.8 2>&1 | head -3
echo ""

echo "5. Testing if pod network gateway can route:"
ping -c 2 10.42.1.1 2>&1 | head -3
echo ""

echo "If NAT rule is missing, add it with:"
echo "  sudo iptables -t nat -A POSTROUTING -s 10.42.1.0/24 -o wlan0 -j MASQUERADE"

