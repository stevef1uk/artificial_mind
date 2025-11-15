#!/bin/bash

# Test script for chained program execution
set -e

HDN_URL=${HDN_URL:-http://localhost:8081}

echo "🧪 Testing Chained Program Execution"
echo "===================================="
echo ""

# Test 1: Simple chained programs (Python -> Go with JSON)
echo "Test 1: Python generates JSON, Go reads it"
echo "-------------------------------------------"

REQUEST='{
  "task_name": "chained_programs",
  "description": "Create TWO programs executed sequentially. Program 1 (Python) must PRINT EXACTLY one line with the JSON string {\"number\": 21} and no other output. Program 2 (Go) must READ the previous JSON and PRINT EXACTLY the number 42 (no extra text, no labels, no JSON). Do NOT print any extra whitespace, labels, prompts, or commentary in either program.",
  "context": {
    "artifacts_wrapper": "true",
    "artifact_names": "prog1.py,prog2.go"
  },
  "language": "python"
}'

echo "Sending request..."
RESPONSE=$(curl -s -X POST "$HDN_URL/api/v1/intelligent/execute" \
  -H 'Content-Type: application/json' \
  -d "$REQUEST")

echo "Response:"
echo "$RESPONSE" | jq '.' || echo "$RESPONSE"

SUCCESS=$(echo "$RESPONSE" | jq -r '.success // false')
WORKFLOW_ID=$(echo "$RESPONSE" | jq -r '.workflow_id // ""')

if [ "$SUCCESS" = "true" ] && [ -n "$WORKFLOW_ID" ]; then
  echo ""
  echo "✅ Request successful! Workflow ID: $WORKFLOW_ID"
  echo ""
  echo "Checking generated files..."
  
  FILES_RESPONSE=$(curl -s "$HDN_URL/api/v1/files/workflow/$WORKFLOW_ID")
  echo "$FILES_RESPONSE" | jq '.[] | {filename: .filename, size: .size}' || echo "$FILES_RESPONSE"
  
  # Check if both files exist (API returns array directly, not wrapped in "files")
  HAS_PROG1=$(echo "$FILES_RESPONSE" | jq -r '.[]? | select(.filename == "prog1.py") | .filename' || echo "")
  HAS_PROG2=$(echo "$FILES_RESPONSE" | jq -r '.[]? | select(.filename == "prog2.go") | .filename' || echo "")
  
  if [ -n "$HAS_PROG1" ] && [ -n "$HAS_PROG2" ]; then
    echo ""
    echo "✅ Both files generated!"
    echo ""
    echo "prog1.py content:"
    curl -s "$HDN_URL/api/v1/workflow/$WORKFLOW_ID/files/prog1.py" | head -30
    echo ""
    echo "prog2.go content:"
    curl -s "$HDN_URL/api/v1/workflow/$WORKFLOW_ID/files/prog2.go" | head -30
  else
    echo "⚠️  Missing files:"
    [ -z "$HAS_PROG1" ] && echo "  - prog1.py not found"
    [ -z "$HAS_PROG2" ] && echo "  - prog2.go not found"
  fi
else
  echo ""
  echo "❌ Request failed or no workflow ID"
  ERROR=$(echo "$RESPONSE" | jq -r '.error // "Unknown error"')
  echo "Error: $ERROR"
fi

