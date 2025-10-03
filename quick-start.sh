#!/bin/bash

# =============================================================================
# Artifical Mind  PROJECT QUICK START SCRIPT
# =============================================================================
# This script helps you get the AGI project up and running quickly
# =============================================================================

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Function to print colored output
print_status() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

print_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Function to check if command exists
command_exists() {
    command -v "$1" >/dev/null 2>&1
}

# Function to check if port is in use
port_in_use() {
    lsof -i :$1 >/dev/null 2>&1
}

# Function to wait for service to be ready
wait_for_service() {
    local url=$1
    local service_name=$2
    local max_attempts=30
    local attempt=1

    print_status "Waiting for $service_name to be ready..."
    
    while [ $attempt -le $max_attempts ]; do
        if curl -s "$url" >/dev/null 2>&1; then
            print_success "$service_name is ready!"
            return 0
        fi
        
        echo -n "."
        sleep 2
        attempt=$((attempt + 1))
    done
    
    print_error "$service_name failed to start after $max_attempts attempts"
    return 1
}

# Main setup function
main() {
    echo "🧠 AGI Project Quick Start"
    echo "========================="
    echo ""

    # Check prerequisites
    print_status "Checking prerequisites..."
    
    if ! command_exists docker; then
        print_error "Docker is not installed. Please install Docker first."
        print_status "Visit: https://www.docker.com/get-started"
        exit 1
    fi
    
    if ! command_exists docker-compose; then
        print_error "Docker Compose is not installed. Please install Docker Compose first."
        print_status "Visit: https://docs.docker.com/compose/install/"
        exit 1
    fi
    
    if ! command_exists curl; then
        print_error "curl is not installed. Please install curl first."
        exit 1
    fi
    
    print_success "All prerequisites are installed!"

    # Check if .env file exists
    if [ ! -f .env ]; then
        print_status "Creating .env file template..."
        cat > .env << 'EOF'
# AGI Project Environment Configuration
# REQUIRED: Set LLM_PROVIDER to one of: openai, anthropic, ollama, local, mock
# LLM_PROVIDER=

# OpenAI Configuration (if using OpenAI)
# OPENAI_API_KEY=your-openai-api-key-here
# OPENAI_MODEL=gpt-4
# OPENAI_BASE_URL=https://api.openai.com/v1

# Anthropic Configuration (if using Anthropic)
# ANTHROPIC_API_KEY=your-anthropic-api-key-here
# ANTHROPIC_MODEL=claude-3-sonnet-20240229
# ANTHROPIC_MAX_TOKENS=4000

# Ollama Configuration (if using Ollama or local)
# OLLAMA_BASE_URL=http://localhost:11434
# LLM_MODEL=ingu627/Qwen2.5-VL-7B-Instruct-Q5_K_M:latest
# OLLAMA_MAX_TOKENS=4000

# Execution Method (docker for x86, drone for ARM)
EXECUTION_METHOD=docker
ENABLE_ARM64_TOOLS=false

# Service URLs
REDIS_URL=redis://localhost:6379
NATS_URL=nats://localhost:4222
WEAVIATE_URL=http://localhost:8080
NEO4J_URI=bolt://localhost:7687
NEO4J_USER=neo4j
NEO4J_PASS=test1234
PRINCIPLES_URL=http://localhost:8084
HDN_URL=http://localhost:8081
FSM_URL=http://localhost:8083
GOAL_MANAGER_URL=http://localhost:8090
MONITOR_URL=http://localhost:8082

# Docker Configuration
DOCKER_MEMORY_LIMIT=512m
DOCKER_CPU_LIMIT=0.5
DOCKER_PIDS_LIMIT=128
DOCKER_TMPFS_SIZE=64m
HDN_MAX_CONCURRENT_EXECUTIONS=2
DEBUG=false
LOG_LEVEL=info
EOF
        print_error "Created .env file template - CONFIGURATION REQUIRED!"
        print_status "You MUST edit .env file and set:"
        print_status "  - LLM_PROVIDER (openai, anthropic, ollama, local, or mock)"
        print_status "  - OPENAI_API_KEY (if using OpenAI)"
        print_status "  - ANTHROPIC_API_KEY (if using Anthropic)"
        print_status "  - OLLAMA_BASE_URL (if using Ollama or local)"
        echo ""
        print_status "Example configurations:"
        print_status "  For OpenAI: LLM_PROVIDER=openai + OPENAI_API_KEY=sk-..."
        print_status "  For Anthropic: LLM_PROVIDER=anthropic + ANTHROPIC_API_KEY=sk-ant-..."
        print_status "  For Ollama: LLM_PROVIDER=ollama + OLLAMA_BASE_URL=http://localhost:11434"
        print_status "  For local: LLM_PROVIDER=local + OLLAMA_BASE_URL=http://localhost:11434"
        print_status "  For testing: LLM_PROVIDER=mock"
        echo ""
        read -p "Press Enter after editing .env file with your LLM configuration..."
    fi

    # Load environment variables
    if [ -f .env ]; then
        set -a
        source .env
        set +a
        print_success "Environment variables loaded from .env file"
        
        # Check for required LLM configuration
        if [ -z "$LLM_PROVIDER" ]; then
            print_error "LLM_PROVIDER not set in .env file!"
        print_status "Please edit .env file and set LLM_PROVIDER to one of:"
        print_status "  - openai (requires OPENAI_API_KEY)"
        print_status "  - anthropic (requires ANTHROPIC_API_KEY)"
        print_status "  - ollama (for local LLM)"
        print_status "  - local (for local LLM)"
        print_status "  - mock (for testing only)"
            exit 1
        fi
        
        # Validate LLM provider specific configuration
        case "$LLM_PROVIDER" in
            "openai")
                if [ -z "$OPENAI_API_KEY" ]; then
                    print_error "OPENAI_API_KEY not set for OpenAI provider!"
                    print_status "Please set OPENAI_API_KEY in .env file"
                    exit 1
                fi
                ;;
            "anthropic")
                if [ -z "$ANTHROPIC_API_KEY" ]; then
                    print_error "ANTHROPIC_API_KEY not set for Anthropic provider!"
                    print_status "Please set ANTHROPIC_API_KEY in .env file"
                    exit 1
                fi
                ;;
            "ollama")
                if [ -z "$OLLAMA_BASE_URL" ]; then
                    print_warning "OLLAMA_BASE_URL not set, using default: http://localhost:11434"
                    export OLLAMA_BASE_URL="http://localhost:11434"
                fi
                ;;
            "local")
                if [ -z "$OLLAMA_BASE_URL" ]; then
                    print_warning "OLLAMA_BASE_URL not set, using default: http://localhost:11434"
                    export OLLAMA_BASE_URL="http://localhost:11434"
                fi
                if [ -z "$LLM_MODEL" ]; then
                    print_warning "LLM_MODEL not set, using default: ingu627/Qwen2.5-VL-7B-Instruct-Q5_K_M:latest"
                    export LLM_MODEL="ingu627/Qwen2.5-VL-7B-Instruct-Q5_K_M:latest"
                fi
                ;;
            "mock")
                print_warning "Using mock LLM provider - this is for testing only!"
                ;;
            *)
                print_error "Invalid LLM_PROVIDER: $LLM_PROVIDER"
                print_status "Valid options: openai, anthropic, ollama, local, mock"
                exit 1
                ;;
        esac
        
        # Set execution method for x86 systems
        if [ -z "$EXECUTION_METHOD" ]; then
            print_status "Setting EXECUTION_METHOD=docker for x86 system"
            export EXECUTION_METHOD="docker"
        fi
        
        print_status "Using LLM_PROVIDER: $LLM_PROVIDER"
        print_status "Using EXECUTION_METHOD: $EXECUTION_METHOD"
    fi

    # Check for port conflicts
    print_status "Checking for port conflicts..."
    
    ports=(8080 8081 8082 8083 6379 4222)
    for port in "${ports[@]}"; do
        if port_in_use $port; then
            print_warning "Port $port is already in use. This might cause conflicts."
        fi
    done

    # Start infrastructure services with Docker Compose
    print_status "Starting infrastructure services (Redis, Neo4j, Weaviate, NATS)..."
    docker-compose up -d
    
    # Wait for infrastructure to be ready
    print_status "Waiting for infrastructure services to be ready..."
    sleep 10
    
    # Check if Go is available for building services
    if ! command_exists go; then
        print_error "Go is not installed. Please install Go to build the AGI services."
        print_status "Visit: https://golang.org/doc/install"
        exit 1
    fi
    
    # Build and start AGI services using the start_servers.sh script
    print_status "Building and starting AGI services..."
    
    # Use the existing start_servers.sh script for the AGI services
    if [ -f "scripts/start_servers.sh" ]; then
        print_status "Using start_servers.sh to build and start AGI services..."
        print_status "Environment variables will be passed to all services..."
        ./scripts/start_servers.sh
    else
        print_error "start_servers.sh not found. Please ensure you're in the AGI project root."
        exit 1
    fi

    # Wait for services to be ready
    print_status "Waiting for services to start..."
    sleep 10

    # Check service health
    print_status "Checking service health..."
    
    if wait_for_service "http://localhost:8080/health" "Principles Server"; then
        print_success "✅ Principles Server is running on port 8080"
    else
        print_error "❌ Principles Server failed to start"
    fi

    if wait_for_service "http://localhost:8081/health" "HDN Server"; then
        print_success "✅ HDN Server is running on port 8081"
    else
        print_error "❌ HDN Server failed to start"
    fi

    if wait_for_service "http://localhost:8082/health" "Monitor UI"; then
        print_success "✅ Monitor UI is running on port 8082"
    else
        print_error "❌ Monitor UI failed to start"
    fi

    if wait_for_service "http://localhost:8083/health" "FSM Server"; then
        print_success "✅ FSM Server is running on port 8083"
    else
        print_error "❌ FSM Server failed to start"
    fi

    # Test the system
    print_status "Testing the system..."
    
    # Test basic chat
    print_status "Testing basic chat functionality..."
    if curl -s -X POST http://localhost:8081/api/v1/chat/text \
        -H "Content-Type: application/json" \
        -d '{"message": "Hello, can you help me test this setup?", "session_id": "quick_start_test"}' \
        >/dev/null 2>&1; then
        print_success "✅ Chat functionality is working"
    else
        print_warning "⚠️  Chat functionality test failed (this might be normal if LLM is not configured)"
    fi

    # Test thinking mode
    print_status "Testing thinking mode..."
    if curl -s -X POST http://localhost:8081/api/v1/chat \
        -H "Content-Type: application/json" \
        -d '{"message": "Please think out loud about what you can do", "show_thinking": true, "session_id": "thinking_test"}' \
        >/dev/null 2>&1; then
        print_success "✅ Thinking mode is working"
    else
        print_warning "⚠️  Thinking mode test failed (this might be normal if LLM is not configured)"
    fi

    # Display access information
    echo ""
    echo "🎉 AGI Project is now running!"
    echo "==============================="
    echo ""
    echo "📊 Monitor UI:     http://localhost:8082"
    echo "🔧 HDN API:        http://localhost:8081"
    echo "⚖️  Principles API: http://localhost:8080"
    echo "🧠 FSM API:        http://localhost:8083"
    echo ""
    echo "📚 Documentation:"
    echo "  - Setup Guide:     docs/SETUP_GUIDE.md"
    echo "  - Configuration:   docs/CONFIGURATION_GUIDE.md"
    echo "  - API Reference:   docs/API_REFERENCE.md"
    echo "  - Thinking Mode:   docs/THINKING_MODE_README.md"
    echo ""
    echo "🧪 Quick Tests:"
    echo "  # Test basic chat"
    echo "  curl -X POST http://localhost:8081/api/v1/chat/text \\"
    echo "    -H 'Content-Type: application/json' \\"
    echo "    -d '{\"message\": \"Hello!\", \"session_id\": \"test\"}'"
    echo ""
    echo "  # Test thinking mode"
    echo "  curl -X POST http://localhost:8081/api/v1/chat \\"
    echo "    -H 'Content-Type: application/json' \\"
    echo "    -d '{\"message\": \"Think out loud\", \"show_thinking\": true}'"
    echo ""
    echo "🛑 To stop the services:"
    echo "  docker-compose down"
    echo ""
    echo "📝 To view logs:"
    echo "  docker-compose logs -f"
    echo ""
    print_success "Setup complete! Enjoy exploring the Artifical Mind  project! 🚀"
}

# Help function
show_help() {
    echo "AGI Project Quick Start Script"
    echo ""
    echo "Usage: $0 [OPTIONS]"
    echo ""
    echo "Options:"
    echo "  --build    Force rebuild of Go services (same as default behavior)"
    echo "  --help     Show this help message"
    echo ""
    echo "Examples:"
    echo "  $0              # Quick start (builds Go services locally)"
    echo "  $0 --build      # Quick start (same as above)"
    echo ""
}

# Parse command line arguments
case "${1:-}" in
    --help|-h)
        show_help
        exit 0
        ;;
    --build)
        main --build
        ;;
    "")
        main
        ;;
    *)
        print_error "Unknown option: $1"
        show_help
        exit 1
        ;;
esac
