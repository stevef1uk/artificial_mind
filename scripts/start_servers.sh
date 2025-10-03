#!/bin/bash

# HDN + Principles Server Startup Script
# This script ensures both servers start in the correct order and directories

set -e  # Exit on any error

# Check for ARM64 architecture and provide helpful message
ARCH=$(uname -m)
if [ "$ARCH" = "aarch64" ]; then
    echo "⚠️  WARNING: Running on ARM64 architecture"
    echo "This script may not work properly on ARM64. Consider using Docker build system instead."
    echo "Continuing anyway..."
else
    echo "ℹ️  Running on $ARCH architecture"
fi

echo "🚀 Starting AGI System (HDN + Principles + Neo4j + Weaviate)"
echo "=========================================================="

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

# Function to wait for a service to be ready
wait_for_service() {
    local url=$1
    local service_name=$2
    local max_attempts=30
    local attempt=0
    
    echo "⏳ Waiting for $service_name to be ready..."
    while [ $attempt -lt $max_attempts ]; do
        if curl -s "$url" >/dev/null 2>&1; then
            echo "✅ $service_name is ready!"
            return 0
        fi
        attempt=$((attempt + 1))
        sleep 1
    done
    
    echo "❌ $service_name failed to start after $max_attempts seconds"
    return 1
}

# Clean up any existing processes
echo "🧹 Cleaning up existing processes..."
kill_port 8080 "Weaviate"
kill_port 8084 "Principles Server"
kill_port 8081 "HDN Server"
kill_port 8082 "Monitor UI"
kill_port 8083 "FSM Server"
kill_port 8090 "Goal Manager"
kill_port 7474 "Neo4j"
kill_port 7687 "Neo4j Bolt"
kill_port 8080 "Weaviate"

# Resolve project root from env or current dir
AGI_PROJECT_ROOT=${AGI_PROJECT_ROOT:-$(pwd)}
export AGI_PROJECT_ROOT

# Load environment from project .env if present
if [ -f "$AGI_PROJECT_ROOT/.env" ]; then
    echo "📦 Loading environment from .env"
    set -a
    # shellcheck disable=SC1091
    . "$AGI_PROJECT_ROOT/.env"
    set +a
    
    # Export key environment variables for services
    export LLM_PROVIDER
    export LLM_MODEL
    export LLM_API_KEY
    export OPENAI_API_KEY
    export ANTHROPIC_API_KEY
    export OLLAMA_URL
    export LLM_TIMEOUT
    echo "🔧 Exported LLM_PROVIDER: $LLM_PROVIDER"
    echo "🔧 Exported LLM_MODEL: $LLM_MODEL"
fi

# Set X86-optimized Docker resource limits for local development
# (These can be overridden by .env file or environment variables)
if [ -z "$DOCKER_MEMORY_LIMIT" ]; then
    export DOCKER_MEMORY_LIMIT="2g"
fi
if [ -z "$DOCKER_CPU_LIMIT" ]; then
    export DOCKER_CPU_LIMIT="2.0"
fi
if [ -z "$DOCKER_PIDS_LIMIT" ]; then
    export DOCKER_PIDS_LIMIT="512"
fi
if [ -z "$DOCKER_TMPFS_SIZE" ]; then
    export DOCKER_TMPFS_SIZE="256m"
fi

echo "🐳 Docker resource limits: Memory=${DOCKER_MEMORY_LIMIT}, CPU=${DOCKER_CPU_LIMIT}, PIDs=${DOCKER_PIDS_LIMIT}, Tmpfs=${DOCKER_TMPFS_SIZE}"

# Add Go to PATH if not already present
if ! command -v go >/dev/null 2>&1; then
    if [ -x "/usr/local/go/bin/go" ]; then
        echo "🔧 Adding Go to PATH"
        export PATH="/usr/local/go/bin:$PATH"
    else
        echo "❌ Go not found and not in /usr/local/go/bin"
        exit 1
    fi
fi

# Start Infrastructure Services (Neo4j + Weaviate + Redis + NATS)
echo ""
echo "🏗️  Starting Infrastructure Services (Neo4j + Weaviate + Redis + NATS)..."
cd "$AGI_PROJECT_ROOT"
docker-compose up -d neo4j weaviate redis nats

# Wait for Neo4j to be ready
if ! wait_for_service "http://localhost:7474" "Neo4j"; then
    echo "❌ Failed to start Neo4j"
    echo "📄 Check logs: docker logs agi-neo4j"
    exit 1
fi

# Wait for Weaviate to be ready
if ! wait_for_service "http://localhost:8080/v1/meta" "Weaviate"; then
    echo "❌ Failed to start Weaviate"
    echo "📄 Check logs: docker logs agi-weaviate"
    exit 1
fi

# Wait for Redis to be ready
echo "⏳ Waiting for Redis to be ready..."
max_attempts=30
attempt=0
while [ $attempt -lt $max_attempts ]; do
    if docker exec agi-redis redis-cli ping >/dev/null 2>&1; then
        echo "✅ Redis is ready!"
        break
    fi
    attempt=$((attempt + 1))
    sleep 1
done

if [ $attempt -eq $max_attempts ]; then
    echo "❌ Redis failed to start after $max_attempts seconds"
    echo "📄 Check logs: docker logs agi-redis"
    exit 1
fi

# Wait for NATS to be ready
if ! wait_for_service "http://localhost:8223/varz" "NATS"; then
    echo "❌ Failed to start NATS"
    echo "📄 Check logs: docker logs agi-nats"
    exit 1
fi

# Helper to run either a local binary or `go run`
run_service() {
    local name="$1"      # friendly name
    local workdir="$2"   # working directory
    local binpath="$3"   # absolute path to binary if built
    shift 3
    local goargs=("$@")  # args for go run/binary

    mkdir -p /tmp
    local logfile="/tmp/${name// /_}.log"

    echo ""
    echo "▶️  Starting $name..."
    cd "$workdir"
    
    # Show relevant environment variables being passed
    echo "🔧 Environment variables being passed:"
    printenv | grep -E '^(LLM_|OPENAI_|ANTHROPIC_|OLLAMA_|EXECUTION_METHOD|ENABLE_ARM64_TOOLS|DOCKER_|REDIS_|NATS_|NEO4J_|WEAVIATE_|PRINCIPLES_|HDN_|FSM_|GOAL_|MONITOR_)' | sed 's/^/  /' || echo "  (none found)"

    if [ -x "$binpath" ]; then
        nohup env $(printenv | grep -E '^(LLM_|OPENAI_|ANTHROPIC_|OLLAMA_|EXECUTION_METHOD|ENABLE_ARM64_TOOLS|DOCKER_|REDIS_|NATS_|NEO4J_|WEAVIATE_|PRINCIPLES_|HDN_|FSM_|GOAL_|MONITOR_)' | tr '\n' ' ') "$binpath" "${goargs[@]}" > "$logfile" 2>&1 &
    else
        if command -v go >/dev/null 2>&1; then
            nohup env $(printenv | grep -E '^(LLM_|OPENAI_|ANTHROPIC_|OLLAMA_|EXECUTION_METHOD|ENABLE_ARM64_TOOLS|DOCKER_|REDIS_|NATS_|NEO4J_|WEAVIATE_|PRINCIPLES_|HDN_|FSM_|GOAL_|MONITOR_)' | tr '\n' ' ') go run . "${goargs[@]}" > "$logfile" 2>&1 &
        else
            echo "❌ Cannot start $name: neither '$binpath' exists nor 'go' is installed" >&2
            echo "ℹ️  Build binaries (make build) or install Go, then retry." >&2
            return 1
        fi
    fi

    local pid=$!
    echo "📝 $name PID: $pid"
    echo "📄 Logs: $logfile"
    echo "$pid"
}

# Start Principles Server
echo "🔨 Building Principles Server..."
cd "$AGI_PROJECT_ROOT"
make build-principles >/dev/null 2>&1 || { echo "❌ Failed to build Principles Server"; exit 1; }

PRINCIPLES_PID=$(run_service "principles_server" \
    "$AGI_PROJECT_ROOT/principles" \
    "$AGI_PROJECT_ROOT/bin/principles-server -port=8084") || { echo "❌ Failed to start Principles Server"; exit 1; }

# Wait for Principles Server to be ready
if ! wait_for_service "http://localhost:8084/action" "Principles Server"; then
    echo "❌ Failed to start Principles Server"
    echo "📄 Check logs: cat /tmp/principles_server.log"
    exit 1
fi

# Start HDN Server
# Ensure HDN binary is built with neo4j tag
echo "🔨 Building HDN server (neo4j) binary..."
cd "$AGI_PROJECT_ROOT"
make build-hdn >/dev/null 2>&1 || { echo "❌ Failed to build HDN"; exit 1; }

HDN_PID=$(run_service "hdn_server" \
    "$AGI_PROJECT_ROOT/hdn" \
    "$AGI_PROJECT_ROOT/bin/hdn-server" \
    -mode=server -port=8081) || { echo "❌ Failed to start HDN Server"; exit 1; }

# Wait for HDN Server to be ready
if ! wait_for_service "http://localhost:8081/api/v1/domains" "HDN Server"; then
    echo "❌ Failed to start HDN Server"
    echo "📄 Check logs: cat /tmp/hdn_server.log"
    exit 1
fi

# Start Monitor UI
echo "🔨 Building Monitor UI..."
cd "$AGI_PROJECT_ROOT/monitor"

# Build the monitor UI and capture the output
BUILD_OUTPUT=$(go build -o ../bin/monitor-ui . 2>&1)
BUILD_EXIT_CODE=$?

if [ $BUILD_EXIT_CODE -eq 0 ]; then
    echo "✅ Monitor UI built successfully"
    cd "$AGI_PROJECT_ROOT"
    
    # Check if binary exists
    if [ -f "$AGI_PROJECT_ROOT/bin/monitor-ui" ]; then
        echo "✅ Monitor UI binary exists"
        
        export WEAVIATE_URL="http://localhost:8080"
        export PRINCIPLES_URL="http://localhost:8084"
        export HDN_URL="http://localhost:8081"
        export GOAL_MANAGER_URL="http://localhost:8090"
        export FSM_URL="http://localhost:8083"
        export NEO4J_URL="http://localhost:7474"
        export NATS_URL="nats://localhost:4222"
        export REDIS_URL="redis://localhost:6379"
        export NEO4J_USER="neo4j"
        export NEO4J_PASS="test1234"
        MONITOR_PID=$(run_service "monitor_ui" \
            "$AGI_PROJECT_ROOT" \
            "$AGI_PROJECT_ROOT/bin/monitor-ui") || {
            echo "⚠️  Monitor UI failed to start, but continuing with main servers"; MONITOR_PID=""; }
    else
        echo "❌ Monitor UI binary not found after build"
        MONITOR_PID=""
    fi
else
    echo "❌ Failed to build Monitor UI:"
    echo "$BUILD_OUTPUT"
    MONITOR_PID=""
    cd "$AGI_PROJECT_ROOT"
fi

# Wait for Monitor UI to be ready (only if it was started)
if [ -n "$MONITOR_PID" ]; then
    echo "⏳ Waiting for Monitor UI to be ready..."
    sleep 5  # Give it a moment to start
    if curl -s "http://localhost:8082/api/status" >/dev/null 2>&1; then
        echo "✅ Monitor UI is ready!"
    else
        echo "⚠️  Monitor UI health check failed, but continuing (it may still work)"
        echo "📄 Check logs: cat /tmp/monitor_ui.log"
        MONITOR_PID=""
    fi
else
    echo "⚠️  Monitor UI not started - skipping health check"
fi

# Start FSM Server
echo "sleep for a bit"
sleep 4
echo "🔨 Building FSM Server..."
cd "$AGI_PROJECT_ROOT"
make build-fsm || { echo "❌ Failed to build FSM Server"; exit 1; }

echo "🧠 Starting FSM Server..."
FSM_PID=$(run_service "fsm" \
    "$AGI_PROJECT_ROOT/fsm" \
    "$AGI_PROJECT_ROOT/bin/fsm-server" \
    -config "config/artificial_mind.yaml") || {
    echo "❌ Failed to start FSM Server"
    exit 1
}

# Optionally flush FSM state in Redis for a clean start (set FSM_FLUSH_STATE=true)
if [ "${FSM_FLUSH_STATE:-false}" = "true" ]; then
    echo "🧹 Flushing FSM state in Redis (fsm:agent_1:state)..."
    docker exec agi-redis redis-cli del fsm:agent_1:state >/dev/null 2>&1 || true
fi
# Start Goal Manager
echo "🔨 Building Goal Manager..."
cd "$AGI_PROJECT_ROOT"
make build-goal >/dev/null 2>&1 || { echo "❌ Failed to build Goal Manager"; GOAL_PID=""; }

GOAL_PID=$(run_service "goal_manager" \
    "$AGI_PROJECT_ROOT" \
    "$AGI_PROJECT_ROOT/bin/goal-manager" \
    -agent=agent_1 -nats=nats://localhost:4222 -redis=redis://localhost:6379 -debug) || {
    echo "⚠️  Goal Manager failed to start, but continuing"; GOAL_PID=""; }

# (Optional) Wait a moment for Goal Manager to warm up
sleep 1


# Save PIDs for cleanup
echo "$PRINCIPLES_PID" > /tmp/principles_server.pid
echo "$HDN_PID" > /tmp/hdn_server.pid
if [ ! -z "$MONITOR_PID" ]; then
    echo "$MONITOR_PID" > /tmp/monitor_ui.pid
fi
if [ ! -z "$FSM_PID" ]; then
    echo "$FSM_PID" > /tmp/fsm_server.pid
fi
if [ ! -z "$GOAL_PID" ]; then
    echo "$GOAL_PID" > /tmp/goal_manager.pid
fi

echo ""
echo "🎉 All services are running!"
echo "=========================="
echo "🏗️  Infrastructure Services:"
echo "  🗄️  Neo4j (Domain Knowledge): http://localhost:7474"
echo "  🔍 Weaviate (Episodic Memory): http://localhost:8080"
echo "  📦 Redis (Working Memory): http://localhost:6379"
echo "  📡 NATS (Event Bus): http://localhost:8223"
echo ""
echo "🧠 Application Services:"
echo "  🔒 Principles Server: http://localhost:8084"
echo "  🧠 HDN Server: http://localhost:8081/api/v1"
if [ ! -z "$MONITOR_PID" ]; then
    echo "  🖥️  Monitor UI: http://localhost:8082"
fi
if [ ! -z "$FSM_PID" ]; then
    echo "  🧠 FSM Server: http://localhost:8083"
fi
if [ ! -z "$GOAL_PID" ]; then
    echo "  🧭 Goal Manager: NATS=nats://localhost:4222, Redis=redis://localhost:6379"
fi
echo ""
echo "📊 Service Status:"
echo "  Neo4j: $(curl -s -o /dev/null -w "%{http_code}" http://localhost:7474)"
echo "  Weaviate: $(curl -s -o /dev/null -w "%{http_code}" http://localhost:8080/v1/meta)"
echo "  Redis: $(docker exec agi-redis redis-cli ping 2>/dev/null | grep -q PONG && echo "200" || echo "000")"
echo "  Principles: $(curl -s -o /dev/null -w "%{http_code}" http://localhost:8084/action)"
echo "  HDN: $(curl -s -o /dev/null -w "%{http_code}" http://localhost:8081/api/v1/domains)"
if [ ! -z "$MONITOR_PID" ]; then
    echo "  Monitor: $(curl -s -o /dev/null -w "%{http_code}" http://localhost:8082/api/status)"
fi
if [ ! -z "$FSM_PID" ]; then
    echo "  FSM: $(curl -s -o /dev/null -w "%{http_code}" http://localhost:8083/health)"
fi
echo ""
echo "🛑 To stop servers: ./stop_servers.sh"
echo "📄 View logs: tail -f /tmp/principles_server.log /tmp/hdn_server.log"
if [ ! -z "$MONITOR_PID" ]; then
    echo "📄 Monitor logs: tail -f /tmp/monitor_ui.log"
fi
if [ ! -z "$FSM_PID" ]; then
    echo "📄 FSM logs: tail -f /tmp/fsm_server.log"
fi
echo ""
echo "✅ Ready to run demos!"
if [ ! -z "$MONITOR_PID" ]; then
    echo "📊 Open Monitor UI: http://localhost:8082"
fi
if [ ! -z "$FSM_PID" ]; then
    echo "🧠 Open FSM Monitor: http://localhost:8083"
fi
