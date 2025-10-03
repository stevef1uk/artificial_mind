#!/bin/bash

# HDN + Principles Server Stop Script
# This script cleanly stops both servers

echo "🛑 Stopping AGI System (HDN + Principles + Infrastructure)"
echo "========================================================="

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

# Stop application services
stop_service "/tmp/principles_server.pid" "Principles Server"
stop_service "/tmp/hdn_server.pid" "HDN Server"
stop_service "/tmp/monitor_ui.pid" "Monitor UI"
stop_service "/tmp/fsm_server.pid" "FSM Server"
stop_service "/tmp/goal_manager.pid" "Goal Manager"

# Resolve project root from env or current dir
AGI_PROJECT_ROOT=${AGI_PROJECT_ROOT:-$(pwd)}

# Stop infrastructure services
echo ""
echo "🏗️  Stopping Infrastructure Services..."
cd "$AGI_PROJECT_ROOT"
docker-compose down

# Clean up any remaining processes on the ports
echo ""
echo "🧹 Cleaning up any remaining processes..."
lsof -ti:8080 | xargs kill -9 2>/dev/null || true
lsof -ti:8081 | xargs kill -9 2>/dev/null || true
lsof -ti:8082 | xargs kill -9 2>/dev/null || true
lsof -ti:8083 | xargs kill -9 2>/dev/null || true
lsof -ti:8090 | xargs kill -9 2>/dev/null || true
lsof -ti:7474 | xargs kill -9 2>/dev/null || true
lsof -ti:7687 | xargs kill -9 2>/dev/null || true
lsof -ti:6333 | xargs kill -9 2>/dev/null || true
lsof -ti:6379 | xargs kill -9 2>/dev/null || true

echo ""
echo "✅ All services stopped!"
echo "📄 Logs are preserved in /tmp/ for debugging"
echo "🗄️  Infrastructure data is preserved in ./data/"
