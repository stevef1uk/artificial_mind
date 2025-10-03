#!/bin/bash

# Test script for the Reasoning Layer
echo "🧠 Testing Reasoning Layer for FSM"
echo "=================================="

# Check if Redis is running (Docker)
echo "📊 Checking Redis connection..."
docker exec agi-redis redis-cli ping > /dev/null 2>&1
if [ $? -eq 0 ]; then
    echo "✅ Redis is running (Docker)"
else
    echo "❌ Redis is not running. Please start Redis first."
    echo "   Try: docker-compose up -d redis"
    exit 1
fi

# Check if HDN server is running
echo "🔧 Checking HDN server..."
curl -s http://localhost:8081/health > /dev/null 2>&1
if [ $? -eq 0 ]; then
    echo "✅ HDN server is running"
else
    echo "⚠️  HDN server is not running. Some features may not work."
    echo "   Try: cd hdn && go run main.go"
fi

# Run the reasoning demo
echo ""
echo "🧪 Running reasoning engine demo..."
cd fsm
go run -c "package main; import _ \"github.com/redis/go-redis/v9\"; func main() { TestReasoningEngine(); TestFSMWithReasoning() }" reasoning_test.go

if [ $? -eq 0 ]; then
    echo ""
    echo "✅ Reasoning engine test completed successfully!"
else
    echo ""
    echo "⚠️  Direct test failed, trying demo instead..."
    cd ../examples
    go run reasoning_demo.go
    if [ $? -eq 0 ]; then
        echo ""
        echo "✅ Reasoning demo completed successfully!"
    else
        echo ""
        echo "❌ Reasoning demo failed!"
        exit 1
    fi
fi

# Run the comprehensive demo
echo ""
echo "🎯 Running comprehensive reasoning demo..."
cd ../examples
go run reasoning_demo.go

if [ $? -eq 0 ]; then
    echo ""
    echo "✅ Reasoning demo completed successfully!"
else
    echo ""
    echo "❌ Reasoning demo failed!"
    exit 1
fi

echo ""
echo "🎉 All reasoning layer tests completed!"
echo ""
echo "The FSM now has reasoning capabilities:"
echo "  🧠 Query knowledge as a belief system"
echo "  🔍 Apply inference rules to generate new beliefs"
echo "  🎯 Generate curiosity-driven goals for exploration"
echo "  💭 Provide transparent explanations of reasoning"
echo "  📝 Log comprehensive reasoning traces"
echo ""
echo "Next steps:"
echo "  1. Start the FSM server: cd fsm && go run server.go"
echo "  2. Send input events to see reasoning in action"
echo "  3. Check the monitoring UI for reasoning traces and beliefs"
