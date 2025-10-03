#!/bin/bash

# Test script for hierarchical planning capabilities with dummy data
# This script creates the required data files and then tests the hierarchical planning system

echo "🧠 Testing Hierarchical Planning System with Data"
echo "================================================"

# Create dummy sales data file
echo "📊 Creating dummy sales data..."
cat > sales_data.csv << 'EOF'
Date,Sales,Region,Product
2024-01-01,1000,North,Widget A
2024-01-02,1200,North,Widget A
2024-01-03,1100,North,Widget B
2024-01-04,1300,South,Widget A
2024-01-05,1400,South,Widget B
2024-01-06,1150,North,Widget A
2024-01-07,1250,South,Widget A
2024-01-08,1350,North,Widget B
2024-01-09,1450,South,Widget B
2024-01-10,1200,North,Widget A
2024-01-11,1300,South,Widget A
2024-01-12,1400,North,Widget B
2024-01-13,1500,South,Widget B
2024-01-14,1250,North,Widget A
2024-01-15,1350,South,Widget A
EOF

echo "✅ Created sales_data.csv with sample data"

# Test 1: List available workflow templates
echo ""
echo "📋 Test 1: Listing workflow templates"
echo "------------------------------------"
curl -s -X GET "http://localhost:8081/api/v1/hierarchical/templates" | jq '.'

# Test 2: Execute a hierarchical data analysis workflow
echo ""
echo "📊 Test 2: Executing hierarchical data analysis workflow"
echo "------------------------------------------------------"
WORKFLOW_RESPONSE=$(curl -s -X POST "http://localhost:8081/api/v1/hierarchical/execute" \
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
  }')

echo "$WORKFLOW_RESPONSE" | jq '.'

# Get the workflow ID from the response
WORKFLOW_ID=$(echo "$WORKFLOW_RESPONSE" | jq -r '.workflow_id')

if [ "$WORKFLOW_ID" != "null" ] && [ "$WORKFLOW_ID" != "" ]; then
  echo "✅ Workflow started with ID: $WORKFLOW_ID"
  
  # Test 3: Monitor workflow status
  echo ""
  echo "📈 Test 3: Monitoring workflow status"
  echo "------------------------------------"
  for i in {1..3}; do
    echo "Status check $i:"
    curl -s -X GET "http://localhost:8081/api/v1/hierarchical/workflow/$WORKFLOW_ID/status" | jq '.'
    sleep 3
  done
  
  # Test 4: List active workflows
  echo ""
  echo "📋 Test 4: Listing active workflows"
  echo "----------------------------------"
  curl -s -X GET "http://localhost:8081/api/v1/hierarchical/workflows" | jq '.'
  
else
  echo "❌ Failed to start workflow"
fi

echo ""
echo "🎉 Hierarchical planning test with data completed!"
echo "================================================="

# Clean up dummy data
echo "🧹 Cleaning up dummy data..."
rm -f sales_data.csv
echo "✅ Cleanup completed"
