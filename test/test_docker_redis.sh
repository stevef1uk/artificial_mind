#!/bin/bash

echo "🧪 Testing Docker + Redis Integration"
echo "====================================="

# Test 1: Direct Docker execution
echo "📦 Test 1: Direct Docker execution"
echo "----------------------------------"

# Resolve project root from env or current dir
AGI_PROJECT_ROOT=${AGI_PROJECT_ROOT:-$(pwd)}

# Create output directory
mkdir -p /tmp/test_output

# Run Python script in Docker
echo "Running Python script in Docker..."
docker run --rm \
  -v "$AGI_PROJECT_ROOT/test_docker_redis.py:/app/test.py" \
  -v /tmp/test_output:/app/output \
  python:3.11-slim \
  python /app/test.py

echo "Checking generated files:"
ls -la /tmp/test_output/

echo ""
echo "File contents:"
echo "CSV file:"
cat /tmp/test_output/sales_data.csv 2>/dev/null || echo "CSV not found"
echo ""
echo "JSON file:"
cat /tmp/test_output/summary.json 2>/dev/null || echo "JSON not found"
echo ""
echo "Report file:"
cat /tmp/test_output/report.txt 2>/dev/null || echo "Report not found"

echo ""
echo "📊 Test 2: HDN Docker API"
echo "------------------------"

# Test HDN Docker API
echo "Testing HDN Docker API..."

# Read the Python script
PYTHON_CODE=$(cat "$AGI_PROJECT_ROOT/test_docker_redis.py")

# Create JSON request
REQUEST=$(cat <<EOF
{
  "language": "python",
  "code": "$(echo "$PYTHON_CODE" | sed 's/"/\\"/g' | tr '\n' '\\n')",
  "timeout": 30,
  "workflow_id": "test-workflow-123",
  "step_id": "test-step-456"
}
EOF
)

echo "Sending request to HDN Docker API..."
curl -X POST http://localhost:8081/api/v1/docker/execute \
  -H "Content-Type: application/json" \
  -d "$REQUEST" | jq .

echo ""
echo "📁 Test 3: Check Redis for stored files"
echo "--------------------------------------"

# Check if files were stored in Redis
echo "Checking Redis for stored files..."
redis-cli KEYS "file:*" | head -10

echo ""
echo "🔍 Test 4: Retrieve files from HDN API"
echo "-------------------------------------"

# Try to retrieve files via HDN API
echo "Trying to retrieve files via HDN API..."

# List files for the test workflow
curl -s http://localhost:8081/api/v1/files/workflow/test-workflow-123 | jq .

echo ""
echo "🧹 Cleanup"
echo "----------"
rm -rf /tmp/test_output
echo "Cleaned up test files"
