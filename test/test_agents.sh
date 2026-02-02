#!/bin/bash
# Test script for agent registry

set -e

echo "🧪 Testing Agent Registry..."

# Test 1: List all agents
echo ""
echo "Test 1: List all agents"
echo "GET http://localhost:8081/api/v1/agents"
curl -s http://localhost:8081/api/v1/agents | jq '.' || echo "Failed to get agents"

# Test 2: Get specific agent
echo ""
echo "Test 2: Get email_monitor_agent"
echo "GET http://localhost:8081/api/v1/agents/email_monitor_agent"
curl -s http://localhost:8081/api/v1/agents/email_monitor_agent | jq '.' || echo "Failed to get agent"

# Test 3: List crews
echo ""
echo "Test 3: List all crews"
echo "GET http://localhost:8081/api/v1/crews"
curl -s http://localhost:8081/api/v1/crews | jq '.' || echo "Failed to get crews"

echo ""
echo "✅ Agent registry tests completed"

