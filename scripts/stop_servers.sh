#!/bin/bash

# Platform-aware AGI System Stop Script
# This script detects the platform and stops services appropriately

set -e

# Detect platform
OS=$(uname -s)

echo "🛑 Stopping AGI System"
echo "====================="
echo "ℹ️  Platform: $OS"
echo ""

# Function to stop a service by PID file
stop_service() {
    local pid_file=$1
    local service_name=$2
    
    if [ -f "$pid_file" ]; then
        local pid=$(cat "$pid_file")
        if ps -p "$pid" > /dev/null 2>&1; then
            echo "🔄 Stopping $service_name (PID: $pid)..."
            kill -TERM "$pid" 2>/dev/null || true
            sleep 2
            
            # Force kill if still running
            if ps -p "$pid" > /dev/null 2>&1; then
                echo "⚡ Force stopping $service_name..."
                kill -9 "$pid" 2>/dev/null || true
            fi
            
            echo "✅ $service_name stopped"
        else
            echo "ℹ️  $service_name was not running"
        fi
        rm -f "$pid_file"
    else
        echo "ℹ️  No PID file found for $service_name"
    fi
}

# Platform-specific Monitor UI stopping
if [ "$OS" = "Darwin" ]; then
    echo "🍎 Mac detected - stopping Monitor UI Docker container..."
    # Stop Monitor UI Docker container
    if docker ps -q --filter ancestor=monitor-ui-local | grep -q .; then
        echo "🔄 Stopping Monitor UI Docker container..."
        docker stop $(docker ps -q --filter ancestor=monitor-ui-local) >/dev/null 2>&1 || true
        echo "✅ Monitor UI Docker container stopped"
    else
        echo "ℹ️  Monitor UI Docker container was not running"
    fi
else
    echo "🐧 Linux detected - stopping Monitor UI native process..."
    stop_service "/tmp/monitor_ui.pid" "Monitor UI"
fi

# Stop other services (platform-agnostic)
echo ""
echo "🔄 Stopping Other Services..."
stop_service "/tmp/hdn_server.pid" "HDN Server"
stop_service "/tmp/principles_server.pid" "Principles Server"
stop_service "/tmp/fsm_server.pid" "FSM Server"
stop_service "/tmp/goal_manager.pid" "Goal Manager"

# Stop infrastructure services
echo ""
echo "🏗️  Stopping Infrastructure Services..."
cd "$(dirname "$0")/.."

if [ "$OS" = "Darwin" ]; then
    echo "🍎 Mac detected - stopping Docker services..."
    # Check if Docker is running before trying to stop services
    if docker info >/dev/null 2>&1; then
        docker-compose down
        echo "✅ Docker services stopped"
    else
        echo "ℹ️  Docker is not running - skipping Docker service cleanup"
    fi
else
    echo "🐧 Linux detected - stopping Docker services..."
    docker-compose down
    echo "✅ Docker services stopped"
fi

echo ""
echo "🎉 AGI System stopped successfully!"