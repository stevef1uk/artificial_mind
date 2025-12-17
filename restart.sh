#!/bin/bash

# Quick restart script for Artificial Mind
# This script stops and restarts all services

set -e

echo "🔄 Restarting Artificial Mind..."
echo "================================"
echo ""

# Stop all services
echo "🛑 Stopping services..."
./scripts/stop_servers.sh

# Wait a moment
sleep 2

# Restart infrastructure
echo ""
echo "🏗️  Restarting infrastructure..."
make compose-restart || docker-compose restart

# Wait for infrastructure to be ready
echo ""
echo "⏳ Waiting for infrastructure to be ready..."
sleep 5

# Start all services
echo ""
echo "🚀 Starting services..."
./scripts/start_servers.sh

echo ""
echo "✅ Artificial Mind restarted!"
echo ""
echo "📊 Check status:"
echo "  - Monitor UI: http://localhost:8082"
echo "  - HDN API: http://localhost:8081/health"
echo "  - FSM API: http://localhost:8083/health"
echo ""

