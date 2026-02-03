#!/bin/bash
# Quick script to rebuild and restart HDN server

set -e

echo "🔨 Step 1: Building HDN server..."
cd /home/stevef/dev/artificial_mind
make build-hdn 2>&1 | tail -5

echo ""
echo "🛑 Step 2: Stopping existing HDN server..."
if [ -f /tmp/hdn_server.pid ]; then
    PID=$(cat /tmp/hdn_server.pid)
    if ps -p $PID > /dev/null 2>&1; then
        echo "   Killing PID: $PID"
        kill $PID 2>/dev/null || kill -9 $PID 2>/dev/null || true
        sleep 2
    fi
fi
# Also try to kill by port
lsof -ti:8081 | xargs kill -9 2>/dev/null || true
sleep 1

echo ""
echo "🚀 Step 3: Starting HDN server..."
cd /home/stevef/dev/artificial_mind/hdn
export AGI_PROJECT_ROOT=/home/stevef/dev/artificial_mind
# Load environment if .env exists
if [ -f ../.env ]; then
    set -a
    source ../.env
    set +a
fi

# Start in background
nohup go run . -mode=server -port=8081 -config=../hdn/config.json > /tmp/hdn_server.log 2>&1 &
HDN_PID=$!
echo $HDN_PID > /tmp/hdn_server.pid

echo "   HDN server started with PID: $HDN_PID"
echo "   Logs: tail -f /tmp/hdn_server.log"

echo ""
echo "⏳ Step 4: Waiting for server to be ready..."
for i in {1..30}; do
    if curl -s http://localhost:8081/api/v1/domains > /dev/null 2>&1; then
        echo "✅ HDN server is ready!"
        break
    fi
    echo -n "."
    sleep 1
done
echo ""

echo ""
echo "✅ Done! Test with: ./test/test_browse_web_tool.sh"
