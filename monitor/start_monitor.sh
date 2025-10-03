#!/bin/bash

# HDN Monitor UI Startup Script
# This script starts the monitoring dashboard

set -e  # Exit on any error

echo "🚀 Starting HDN Monitor UI"
echo "=========================="

# Check if Go is installed
if ! command -v go &> /dev/null; then
    echo "❌ Go is not installed. Please install Go first."
    exit 1
fi

# Check if we're in the monitor directory
if [ ! -f "main.go" ]; then
    echo "❌ Please run this script from the monitor directory"
    exit 1
fi

# Function to check if a port is in use
check_port() {
    local port=$1
    if lsof -Pi :$port -sTCP:LISTEN -t >/dev/null 2>&1; then
        return 0  # Port is in use
    else
        return 1  # Port is free
    fi
}

# Function to kill processes on a port
kill_port() {
    local port=$1
    local service_name=$2
    if check_port $port; then
        echo "🔄 Stopping existing $service_name on port $port..."
        lsof -ti:$port | xargs kill -9 2>/dev/null || true
        sleep 2
    fi
}

# Clean up any existing monitor process
echo "🧹 Cleaning up existing monitor process..."
kill_port 8082 "Monitor UI"

# Install dependencies
echo "📦 Installing dependencies..."
go mod tidy

# Resolve project root from env or current dir
AGI_PROJECT_ROOT=${AGI_PROJECT_ROOT:-$(pwd)}

# Start the monitor service
echo ""
echo "🖥️  Starting Monitor UI..."
cd "$AGI_PROJECT_ROOT/monitor"
# Run the entire package so all files in package main are included (not just main.go)
nohup go run . > /tmp/monitor_ui.log 2>&1 &
MONITOR_PID=$!
echo "📝 Monitor UI PID: $MONITOR_PID"
echo "📄 Logs: /tmp/monitor_ui.log"

# Wait for monitor to be ready
echo "⏳ Waiting for Monitor UI to be ready..."
sleep 3

if check_port 8082; then
    echo "✅ Monitor UI is ready!"
    echo ""
    echo "🎉 Monitor UI is running!"
    echo "=========================="
    echo "🖥️  Monitor UI: http://localhost:8082"
    echo "🔧 API: http://localhost:8082/api/status"
    echo ""
    echo "📊 Dashboard Features:"
    echo "  - Real-time system status"
    echo "  - Service health monitoring"
    echo "  - Active workflow tracking"
    echo "  - Execution metrics"
    echo "  - System logs"
    echo "  - Auto-refresh capability"
    echo ""
    echo "🛑 To stop: kill $MONITOR_PID or ./stop_monitor.sh"
    echo "📄 View logs: tail -f /tmp/monitor_ui.log"
    echo ""
    echo "✅ Ready to monitor your HDN system!"
    
    # Save PID for cleanup
    echo "$MONITOR_PID" > /tmp/monitor_ui.pid
else
    echo "❌ Failed to start Monitor UI"
    echo "📄 Check logs: cat /tmp/monitor_ui.log"
    exit 1
fi
