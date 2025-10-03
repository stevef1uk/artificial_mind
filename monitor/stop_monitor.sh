#!/bin/bash

# HDN Monitor UI Stop Script
# This script cleanly stops the monitoring dashboard

echo "🛑 Stopping HDN Monitor UI"
echo "=========================="

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

# Stop monitor service
stop_service "/tmp/monitor_ui.pid" "Monitor UI"

# Clean up any remaining processes on the port
echo ""
echo "🧹 Cleaning up any remaining processes..."
lsof -ti:8082 | xargs kill -9 2>/dev/null || true

echo ""
echo "✅ Monitor UI stopped!"
echo "📄 Logs are preserved in /tmp/ for debugging"
