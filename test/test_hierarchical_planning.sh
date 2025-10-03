#!/bin/bash

# Test script for hierarchical planning capabilities
# This script demonstrates the multi-step hierarchical planning system

echo "🧠 Testing Hierarchical Planning System"
echo "========================================"

# Start the servers
echo "🚀 Starting servers..."
./start_servers.sh &
SERVER_PID=$!

# Wait for servers to start
echo "⏳ Waiting for servers to start..."
sleep 10

# Test 1: List available workflow templates
echo ""
echo "📋 Test 1: Listing workflow templates"
echo "------------------------------------"
curl -s -X GET "http://localhost:8081/api/v1/hierarchical/templates" | jq '.'

# Test 2: Execute a hierarchical data analysis workflow
echo ""
echo "📊 Test 2: Executing hierarchical data analysis workflow"
echo "------------------------------------------------------"
curl -s -X POST "http://localhost:8081/api/v1/hierarchical/execute" \
  -H "Content-Type: application/json" \
  -d '{
    "task_name": "data_analysis",
    "description": "Complete data analysis pipeline",
    "user_request": "Analyze sales data and generate a report",
    "context": {
      "data_source": "sales_data.csv",
      "analysis_type": "trend_analysis",
      "output_format": "pdf"
    }
  }' | jq '.'

# Get the workflow ID from the response
WORKFLOW_ID=$(curl -s -X POST "http://localhost:8081/api/v1/hierarchical/execute" \
  -H "Content-Type: application/json" \
  -d '{
    "task_name": "data_analysis",
    "description": "Complete data analysis pipeline",
    "user_request": "Analyze sales data and generate a report",
    "context": {
      "data_source": "sales_data.csv",
      "analysis_type": "trend_analysis",
      "output_format": "pdf"
    }
  }' | jq -r '.workflow_id')

if [ "$WORKFLOW_ID" != "null" ] && [ "$WORKFLOW_ID" != "" ]; then
  echo "✅ Workflow started with ID: $WORKFLOW_ID"
  
  # Test 3: Monitor workflow status
  echo ""
  echo "📈 Test 3: Monitoring workflow status"
  echo "------------------------------------"
  for i in {1..5}; do
    echo "Status check $i:"
    curl -s -X GET "http://localhost:8081/api/v1/hierarchical/workflow/$WORKFLOW_ID/status" | jq '.'
    sleep 2
  done
  
  # Test 4: List active workflows
  echo ""
  echo "📋 Test 4: Listing active workflows"
  echo "----------------------------------"
  curl -s -X GET "http://localhost:8081/api/v1/hierarchical/workflows" | jq '.'
  
  # Test 5: Test workflow control (pause/resume)
  echo ""
  echo "⏸️ Test 5: Testing workflow control"
  echo "----------------------------------"
  echo "Pausing workflow..."
  curl -s -X POST "http://localhost:8081/api/v1/hierarchical/workflow/$WORKFLOW_ID/pause" \
    -H "Content-Type: application/json" \
    -d '{"reason": "Testing pause functionality"}' | jq '.'
  
  sleep 2
  
  echo "Resuming workflow..."
  curl -s -X POST "http://localhost:8081/api/v1/hierarchical/workflow/$WORKFLOW_ID/resume" \
    -H "Content-Type: application/json" \
    -d '{"resume_token": "test_token"}' | jq '.'
  
  # Test 6: Execute a web scraping workflow
  echo ""
  echo "🕷️ Test 6: Executing web scraping workflow"
  echo "----------------------------------------"
  curl -s -X POST "http://localhost:8081/api/v1/hierarchical/execute" \
    -H "Content-Type: application/json" \
    -d '{
      "task_name": "web_scraping",
      "description": "Complete web scraping pipeline",
      "user_request": "Scrape product information from e-commerce site",
      "context": {
        "url": "https://example-store.com/products",
        "selectors": "product-name,price,description",
        "output_format": "json"
      }
    }' | jq '.'
  
  # Test 7: Execute an ML pipeline workflow
  echo ""
  echo "🤖 Test 7: Executing ML pipeline workflow"
  echo "---------------------------------------"
  curl -s -X POST "http://localhost:8081/api/v1/hierarchical/execute" \
    -H "Content-Type: application/json" \
    -d '{
      "task_name": "ml_pipeline",
      "description": "Complete machine learning pipeline",
      "user_request": "Train a model to predict customer churn",
      "context": {
        "dataset": "customer_data.csv",
        "model_type": "random_forest",
        "target_column": "churn"
      }
    }' | jq '.'
  
else
  echo "❌ Failed to start workflow"
fi

# Test 8: Test traditional planning (fallback)
echo ""
echo "🔄 Test 8: Testing traditional planning fallback"
echo "----------------------------------------------"
curl -s -X POST "http://localhost:8081/api/v1/task/execute" \
  -H "Content-Type: application/json" \
  -d '{
    "task_name": "CalculatePrimes",
    "context": {
      "count": "10"
    }
  }' | jq '.'

echo ""
echo "🎉 Hierarchical planning tests completed!"
echo "========================================"

# Clean up
echo "🧹 Cleaning up..."
kill $SERVER_PID 2>/dev/null || true
