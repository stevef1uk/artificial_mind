#!/bin/bash

# Test Drone Executor via HDN API using curl
# This script tests if a simple program can be created and run using Drone

echo "🧪 Testing Drone Executor via HDN API"
echo "====================================="

# Configuration
HDN_SERVER="http://localhost:8081"
TOOL_ENDPOINT="$HDN_SERVER/api/v1/tools/tool_drone_executor/invoke"
TOOLS_ENDPOINT="$HDN_SERVER/api/v1/tools"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Function to print colored output
print_status() {
    local status=$1
    local message=$2
    case $status in
        "INFO") echo -e "${BLUE}ℹ️  $message${NC}" ;;
        "SUCCESS") echo -e "${GREEN}✅ $message${NC}" ;;
        "WARNING") echo -e "${YELLOW}⚠️  $message${NC}" ;;
        "ERROR") echo -e "${RED}❌ $message${NC}" ;;
    esac
}

# Function to test HDN server connectivity
test_hdn_connectivity() {
    print_status "INFO" "Testing HDN server connectivity..."
    
    if curl -s --connect-timeout 5 "$TOOLS_ENDPOINT" > /dev/null; then
        print_status "SUCCESS" "HDN server is reachable"
        return 0
    else
        print_status "ERROR" "HDN server is not reachable at $HDN_SERVER"
        return 1
    fi
}

# Function to get available tools
get_available_tools() {
    print_status "INFO" "Fetching available tools..."
    
    local response=$(curl -s "$TOOLS_ENDPOINT")
    if [ $? -eq 0 ]; then
        print_status "SUCCESS" "Tools endpoint responded"
        echo "Available tools:"
        echo "$response" | jq -r '.[] | "  - \(.id): \(.name)"' 2>/dev/null || echo "$response"
    else
        print_status "ERROR" "Failed to fetch tools"
        return 1
    fi
}

# Function to test drone executor with a specific test case
test_drone_executor() {
    local test_name="$1"
    local code="$2"
    local language="$3"
    local image="$4"
    
    print_status "INFO" "Testing: $test_name"
    print_status "INFO" "Language: $language, Image: $image"
    
    # Create JSON payload
    local payload=$(cat <<EOF
{
    "code": "$code",
    "language": "$language",
    "image": "$image",
    "environment": {},
    "timeout": 30
}
EOF
)
    
    echo "Payload:"
    echo "$payload" | jq . 2>/dev/null || echo "$payload"
    echo
    
    # Make the request
    print_status "INFO" "Sending request to $TOOL_ENDPOINT"
    
    local response=$(curl -s -X POST \
        -H "Content-Type: application/json" \
        -d "$payload" \
        "$TOOL_ENDPOINT")
    
    local curl_exit_code=$?
    
    if [ $curl_exit_code -eq 0 ]; then
        print_status "SUCCESS" "Request sent successfully"
        echo "Response:"
        echo "$response" | jq . 2>/dev/null || echo "$response"
        
        # Check if the response indicates success
        local success=$(echo "$response" | jq -r '.success' 2>/dev/null)
        if [ "$success" = "true" ]; then
            print_status "SUCCESS" "Code execution successful!"
            local output=$(echo "$response" | jq -r '.output' 2>/dev/null)
            echo "Output: $output"
        else
            print_status "ERROR" "Code execution failed"
            local error=$(echo "$response" | jq -r '.error' 2>/dev/null)
            echo "Error: $error"
        fi
    else
        print_status "ERROR" "Failed to send request (curl exit code: $curl_exit_code)"
    fi
    
    echo "----------------------------------------"
}

# Main execution
main() {
    echo
    
    # Test 1: Check HDN server connectivity
    if ! test_hdn_connectivity; then
        print_status "ERROR" "Cannot proceed without HDN server"
        exit 1
    fi
    
    echo
    
    # Test 2: Get available tools
    get_available_tools
    
    echo
    
    # Test 3: Test simple Go program
    test_drone_executor \
        "Simple Go Hello World" \
        "package main
import \"fmt\"
func main() {
    fmt.Println(\"Hello from Go via Drone!\")
    fmt.Println(\"This is a test of the Drone executor\")
}" \
        "go" \
        "golang:1.21-alpine"
    
    # Test 4: Test simple Python program
    test_drone_executor \
        "Simple Python Hello World" \
        "print('Hello from Python via Drone!')
print('This is a test of the Drone executor')
import sys
print(f'Python version: {sys.version}')" \
        "python" \
        "python:3.11-alpine"
    
    # Test 5: Test simple Bash program
    test_drone_executor \
        "Simple Bash Hello World" \
        "echo 'Hello from Bash via Drone!'
echo 'This is a test of the Drone executor'
echo 'Current date:' \$(date)
echo 'Architecture:' \$(uname -m)" \
        "bash" \
        "alpine:latest"
    
    # Test 6: Test Go with more complex code
    test_drone_executor \
        "Go with file operations" \
        "package main
import (
    \"fmt\"
    \"os\"
    \"time\"
)
func main() {
    fmt.Println(\"Testing file operations in Drone\")
    
    // Create a test file
    content := \"Hello from Drone executor!\\n\"
    content += \"Timestamp: \" + time.Now().Format(\"2006-01-02 15:04:05\") + \"\\n\"
    
    err := os.WriteFile(\"/tmp/drone_test.txt\", []byte(content), 0644)
    if err != nil {
        fmt.Printf(\"Error writing file: %v\\n\", err)
        return
    }
    
    // Read the file back
    data, err := os.ReadFile(\"/tmp/drone_test.txt\")
    if err != nil {
        fmt.Printf(\"Error reading file: %v\\n\", err)
        return
    }
    
    fmt.Printf(\"File content:\\n%s\", string(data))
    fmt.Println(\"File operations test completed!\")
}" \
        "go" \
        "golang:1.21-alpine"
    
    print_status "SUCCESS" "All Drone executor tests completed!"
}

# Check if jq is available for JSON formatting
if ! command -v jq &> /dev/null; then
    print_status "WARNING" "jq is not installed. JSON output will not be formatted."
fi

# Run the main function
main
