#!/bin/bash
set -e

# Configuration
OLLAMA_URL="http://localhost:11434/api/chat"
SCRAPER_PORT=8081
HDN_PORT=8080

echo "🚀 Starting Smart Scrape Demo Environment"

# 1. Ensure Dependencies (Redis) are running
echo "📦 Checking Redis..."
if ! docker ps | grep -q "agi-redis"; then
    echo "   Starting Redis via docker-compose..."
    docker-compose up -d redis
    sleep 2
else
    echo "   Redis is running."
fi

# 2. Build Scraper
echo "🔨 Building Scraper..."
cd services/playwright_scraper
go build -o scraper main.go
cd ../..

# 3. Start Scraper
echo "🚗 Starting Scraper on port $SCRAPER_PORT..."
PLAYWRIGHT_EXECUTABLE_PATH="" ./services/playwright_scraper/scraper &
SCRAPER_PID=$!
echo "   PID: $SCRAPER_PID"

# 4. Endure HDN Build
echo "🔨 Building HDN..."
cd hdn
go build -o hdn-server main.go
cd ..

# 5. Start HDN
echo "🧠 Starting HDN on port $HDN_PORT..."
# Note: Using localhost for everything as we are running natively on host
export LLM_PROVIDER=ollama
export OLLAMA_URL=$OLLAMA_URL
export REDIS_URL=redis://localhost:6379
export PLAYWRIGHT_SCRAPER_URL=http://localhost:$SCRAPER_PORT
export HDN_PORT=$HDN_PORT
export LOG_LEVEL=debug

./hdn/hdn-server &
HDN_PID=$!
echo "   PID: $HDN_PID"

# Cleanup function
cleanup() {
    echo ""
    echo "🛑 Shutting down services..."
    kill $SCRAPER_PID 2>/dev/null || true
    kill $HDN_PID 2>/dev/null || true
    echo "✅ Done."
}
trap cleanup EXIT INT

# Wait for services to be ready
echo "⏳ Waiting for services to start..."
sleep 5

# 6. Run Python Test Script
echo "🧪 Running Demo Script..."
# Default goal: Find title
# Or custom Goal
URL="https://example.com"
GOAL="Find the page title and domain info"

# Use python environment if needed, defaulting to system python3
python3 test/smart_scrape_demo.py "$URL" "$GOAL"

echo "🎉 Demo Complete"
# Keep running for a bit if user wants to inspect logs, or exit immediately?
# Exit immediately to cleanup.
