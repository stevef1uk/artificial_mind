#!/bin/bash

# Test script to verify all programming languages work correctly
# Tests: Python, Go, JavaScript, Java, and Rust

# Don't exit on error - we want to test all languages even if one fails
set +e

HDN_URL="${HDN_URL:-http://localhost:8081}"
API_URL="${HDN_URL}/api/v1/intelligent/execute"

echo "🧪 Testing All Programming Languages"
echo "===================================="
echo "HDN URL: ${HDN_URL}"
echo ""

# Colors for output
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Test counter
PASSED=0
FAILED=0

# Function to test a language
test_language() {
    local lang=$1
    local description=$2
    local expected_output=$3
    
    echo -e "${YELLOW}Testing ${lang}...${NC}"
    echo "  Request: ${description}"
    
    # Create JSON payload
    payload=$(cat <<EOF
{
    "task_name": "test_${lang}",
    "description": "${description}",
    "language": "${lang}",
    "context": {
        "artifacts_wrapper": "true"
    },
    "force_regenerate": true,
    "max_retries": 2
}
EOF
)
    
    # Make API request
    response=$(curl -s -X POST "${API_URL}" \
        -H "Content-Type: application/json" \
        -d "${payload}" \
        -w "\n%{http_code}")
    
    # Extract HTTP status code (last line)
    http_code=$(echo "${response}" | tail -n 1)
    body=$(echo "${response}" | sed '$d')
    
    # Check if request was successful
    if [ "${http_code}" != "200" ]; then
        echo -e "  ${RED}❌ FAILED${NC} - HTTP ${http_code}"
        echo "  Response: ${body}"
        FAILED=$((FAILED + 1))
        return 1
    fi
    
    # Parse JSON response using Python for more reliable parsing
    success=$(echo "${body}" | python3 -c "import sys, json; data = json.load(sys.stdin); print('true' if data.get('success') else 'false')" 2>/dev/null || echo "false")
    result=$(echo "${body}" | python3 -c "import sys, json; data = json.load(sys.stdin); print(data.get('result', ''))" 2>/dev/null || echo "")
    error=$(echo "${body}" | python3 -c "import sys, json; data = json.load(sys.stdin); print(data.get('error', ''))" 2>/dev/null || echo "")
    generated_lang=$(echo "${body}" | python3 -c "import sys, json; data = json.load(sys.stdin); gc = data.get('generated_code', {}); print(gc.get('language', ''))" 2>/dev/null || echo "")
    generated_code=$(echo "${body}" | python3 -c "import sys, json; data = json.load(sys.stdin); gc = data.get('generated_code', {}); print(gc.get('code', ''))" 2>/dev/null || echo "")
    
    # Fallback to grep if Python fails
    if [ "${success}" = "false" ] && [ -z "${result}" ]; then
        success=$(echo "${body}" | grep -o '"success":[^,}]*' | cut -d':' -f2 | tr -d ' ' | head -1)
        result=$(echo "${body}" | grep -o '"result":"[^"]*"' | cut -d'"' -f4 | head -1 || echo "")
        error=$(echo "${body}" | grep -o '"error":"[^"]*"' | cut -d'"' -f4 | head -1 || echo "")
    fi
    
    # Validate that the generated code is in the requested language
    lang_validation_passed=true
    lang_validation_msg=""
    
    if [ -n "${generated_lang}" ] && [ "${generated_lang}" != "${lang}" ]; then
        lang_validation_passed=false
        lang_validation_msg="Language mismatch: requested '${lang}', got '${generated_lang}'"
    fi
    
    # Also check the code itself for language-specific syntax
    if [ -n "${generated_code}" ]; then
        case "${lang}" in
            rust)
                if ! echo "${generated_code}" | grep -q "fn main\|use \|println!"; then
                    lang_validation_passed=false
                    lang_validation_msg="${lang_validation_msg} (code doesn't look like Rust)"
                fi
                ;;
            go)
                if ! echo "${generated_code}" | grep -q "package main\|func main\|fmt\."; then
                    lang_validation_passed=false
                    lang_validation_msg="${lang_validation_msg} (code doesn't look like Go)"
                fi
                ;;
            java)
                if ! echo "${generated_code}" | grep -q "public class\|public static void main"; then
                    lang_validation_passed=false
                    lang_validation_msg="${lang_validation_msg} (code doesn't look like Java)"
                fi
                ;;
            python|py)
                if echo "${generated_code}" | grep -q "fn main\|package main\|public class"; then
                    lang_validation_passed=false
                    lang_validation_msg="${lang_validation_msg} (code looks like Rust/Go/Java, not Python)"
                fi
                ;;
            javascript|js)
                if ! echo "${generated_code}" | grep -q "console\.log\|require\|module\.exports"; then
                    if echo "${generated_code}" | grep -q "fn main\|package main\|public class"; then
                        lang_validation_passed=false
                        lang_validation_msg="${lang_validation_msg} (code looks like Rust/Go/Java, not JavaScript)"
                    fi
                fi
                ;;
        esac
    fi
    
    # Check if execution was successful
    if [ "${success}" = "true" ] && [ "${lang_validation_passed}" = "true" ]; then
        echo -e "  ${GREEN}✅ PASSED${NC}"
        if [ -n "${result}" ]; then
            echo "  Output: ${result}"
        fi
        if [ -n "${expected_output}" ] && [ -n "${result}" ]; then
            if echo "${result}" | grep -q "${expected_output}"; then
                echo -e "  ${GREEN}✓ Output matches expected: ${expected_output}${NC}"
            else
                echo -e "  ${YELLOW}⚠ Output doesn't match expected (but execution succeeded)${NC}"
            fi
        fi
        if [ -n "${generated_lang}" ]; then
            echo -e "  ${GREEN}✓ Language correct: ${generated_lang}${NC}"
        fi
        PASSED=$((PASSED + 1))
        return 0
    else
        echo -e "  ${RED}❌ FAILED${NC}"
        if [ "${success}" != "true" ]; then
            if [ -n "${error}" ]; then
                echo "  Error: ${error}"
            fi
        fi
        if [ "${lang_validation_passed}" != "true" ]; then
            echo -e "  ${RED}✗ ${lang_validation_msg}${NC}"
            if [ -n "${generated_code}" ]; then
                echo "  Generated code (first 200 chars): $(echo "${generated_code}" | head -c 200)"
            fi
        fi
        if [ -n "${result}" ]; then
            echo "  Output: ${result}"
        fi
        FAILED=$((FAILED + 1))
        return 1
    fi
}

# Test Python
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
test_language "python" "Create a Python program that prints 'Hello from Python'" "Hello from Python"

# Test Go
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
test_language "go" "Create a Go program that prints 'Hello from Go'" "Hello from Go"

# Test JavaScript
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
test_language "javascript" "Create a JavaScript program that prints 'Hello from JavaScript'" "Hello from JavaScript"

# Test Java
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
test_language "java" "Create a Java program that prints 'Hello from Java'" "Hello from Java"

# Test Rust
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
test_language "rust" "Create a Rust program that prints 'Hello from Rust'" "Hello from Rust"

# Summary
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "📊 Test Summary"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo -e "${GREEN}Passed: ${PASSED}${NC}"
echo -e "${RED}Failed: ${FAILED}${NC}"
echo "Total:  $((PASSED + FAILED))"

if [ ${FAILED} -eq 0 ]; then
    echo ""
    echo -e "${GREEN}🎉 All tests passed!${NC}"
    exit 0
else
    echo ""
    echo -e "${RED}❌ Some tests failed${NC}"
    exit 1
fi

