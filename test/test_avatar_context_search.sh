#!/bin/bash

# Test script for the new search_avatar_context MCP tool

set -e

echo "🧪 Testing search_avatar_context MCP tool..."
echo ""

# Test 1: Search for Accenture
echo "Test 1: Searching for 'Accenture'..."
curl -s -X POST http://localhost:8080/api/v1/mcp \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "id": 1,
    "method": "tools/call",
    "params": {
      "name": "search_avatar_context",
      "arguments": {
        "query": "Accenture",
        "limit": 3
      }
    }
  }' | jq '.'

echo ""
echo "---"
echo ""

# Test 2: Search for skills
echo "Test 2: Searching for 'Go Python skills'..."
curl -s -X POST http://localhost:8080/api/v1/mcp \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "id": 2,
    "method": "tools/call",
    "params": {
      "name": "search_avatar_context",
      "arguments": {
        "query": "Go Python",
        "limit": 3
      }
    }
  }' | jq '.'

echo ""
echo "---"
echo ""

# Test 3: List all tools to verify the new tool is registered
echo "Test 3: Listing all MCP tools..."
curl -s -X POST http://localhost:8080/api/v1/mcp \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "id": 3,
    "method": "tools/list"
  }' | jq '.result.tools[] | select(.name == "search_avatar_context")'

echo ""
echo "✅ Tests completed!"
