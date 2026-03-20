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

# Function to kill processes on a port (safely)
# Avoid killing Docker Desktop/vpnkit/lima backend processes which publish container ports on macOS
kill_port() {
    local port=$1
    local service_name=$2
    if lsof -Pi :$port -sTCP:LISTEN -t >/dev/null 2>&1; then
        echo "🔄 Stopping $service_name on port $port..."
        # Get listening PIDs on the port, exclude Docker Desktop related proxies
        local pids
        pids=$(lsof -nP -iTCP:$port -sTCP:LISTEN -t 2>/dev/null | xargs -I{} sh -c 'ps -o pid=,comm= -p {}' | awk 'BEGIN{ok=0} !/com\.docker|Docker|vpnkit|lima|qemu|docker-proxy/ {print $1; ok=1} END{ if (ok==0) exit 0 }')
        if [ -n "$pids" ]; then
            echo "$pids" | xargs kill -TERM 2>/dev/null || true
            sleep 2
            # Force kill if still running
            for pid in $pids; do
                if ps -p "$pid" > /dev/null 2>&1; then
                    kill -9 "$pid" 2>/dev/null || true
                fi
            done
            echo "✅ $service_name on port $port stopped"
        else
            echo "ℹ️  Listener appears to be managed by Docker Desktop or is already gone; skipping kill"
        fi
    else
        echo "ℹ️  No process found on port $port for $service_name"
    fi
}

# Function to kill processes by name pattern
kill_by_name() {
    local pattern=$1
    local service_name=$2
    local pids=$(pgrep -f "$pattern" 2>/dev/null || true)
    if [ -n "$pids" ]; then
        echo "🔄 Stopping $service_name processes (pattern: $pattern)..."
        echo "$pids" | xargs kill -TERM 2>/dev/null || true
        sleep 2
        # Force kill if still running
        for pid in $pids; do
            if ps -p "$pid" > /dev/null 2>&1; then
                kill -9 "$pid" 2>/dev/null || true
            fi
        done
        echo "✅ $service_name processes stopped"
    fi
}

# Function to stop a service by PID file
stop_service() {
    local pid_file=$1
    local service_name=$2
    local port=$3  # Optional port for fallback
    local name_pattern=$4  # Optional name pattern for fallback
    
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
            echo "ℹ️  $service_name was not running (PID file exists but process not found)"
        fi
        rm -f "$pid_file"
    else
        echo "ℹ️  No PID file found for $service_name"
    fi
    
    # Fallback: kill by port if provided
    if [ -n "$port" ]; then
        kill_port "$port" "$service_name"
    fi
    
    # Fallback: kill by name pattern if provided
    if [ -n "$name_pattern" ]; then
        kill_by_name "$name_pattern" "$service_name"
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
    # Also kill any processes on port 8082 as fallback
    kill_port 8082 "Monitor UI"
else
    echo "🐧 Linux detected - stopping Monitor UI native process..."
    stop_service "/tmp/monitor_ui.pid" "Monitor UI" "8082" "monitor-ui"
fi

# Stop other services (platform-agnostic)
echo ""
echo "🔄 Stopping Other Services..."
stop_service "/tmp/hdn_server.pid" "HDN Server" "8081" "hdn-server"
stop_service "/tmp/principles_server.pid" "Principles Server" "8084" "principles-server"
stop_service "/tmp/fsm_server.pid" "FSM Server" "8083" "fsm-server"
stop_service "/tmp/goal_manager.pid" "Goal Manager" "" "goal-manager"
stop_service "/tmp/playwright_scraper.pid" "Playwright Scraper" "8085" "playwright-scraper"
# Also stop docker version if it was left running
docker stop playwright-scraper 2>/dev/null || true

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

# Final cleanup: ensure all service ports are free
echo ""
echo "🧹 Final cleanup: checking for any remaining processes..."
kill_port 8081 "HDN Server"
kill_port 8082 "Monitor UI"
kill_port 8083 "FSM Server"
kill_port 8084 "Principles Server"
kill_port 8085 "Playwright Scraper"

# Clean up any remaining PID files
rm -f /tmp/principles_server.pid
rm -f /tmp/hdn_server.pid
rm -f /tmp/monitor_ui.pid
rm -f /tmp/fsm_server.pid
rm -f /tmp/goal_manager.pid
rm -f /tmp/playwright_scraper.pid

echo ""
echo "🎉 AGI System stopped successfully!"