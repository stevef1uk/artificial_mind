#!/bin/bash

# Test script for Enhanced Hypothesis Testing Prompt
# Verifies that the new step-by-step instructions are being used

# Don't exit on error immediately - we want to show helpful messages
set +e

# Try to detect the correct HDN URL
# Check if HDN_URL is already set, otherwise try common options
if [ -z "$HDN_URL" ]; then
    # Try NodePort first (if service is exposed)
    if kubectl get svc -n agi hdn-server-rpi58 >/dev/null 2>&1; then
        NODEPORT=$(kubectl get svc -n agi hdn-server-rpi58 -o jsonpath='{.spec.ports[?(@.name=="http")].nodePort}' 2>/dev/null)
        if [ -n "$NODEPORT" ]; then
            HDN_URL="http://localhost:${NODEPORT}"
            echo "Using NodePort: ${HDN_URL}"
        else
            # Try default NodePort from previous context
            HDN_URL="http://localhost:30257"
            echo "Using default NodePort: ${HDN_URL}"
        fi
    else
        # Fallback to localhost:8081 (for local development or port-forward)
        HDN_URL="http://localhost:8081"
        echo "Using default: ${HDN_URL}"
    fi
fi

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}🧪 Testing Enhanced Hypothesis Testing Prompt${NC}"
echo "=========================================="
echo ""

# Generate unique test hypothesis
TIMESTAMP=$(date +%s)
TEST_EVENT="test_event_${TIMESTAMP}_unique"
HYPOTHESIS="If we explore ${TEST_EVENT} further, we can discover new insights about General domain"

echo -e "${BLUE}1️⃣ Testing Direct API Call${NC}"
echo "-----------------------------------"
echo "Hypothesis: ${HYPOTHESIS}"
echo ""

# Make the API call - use a temp file to avoid JSON escaping issues
TEMP_JSON=$(mktemp)
cat > "$TEMP_JSON" <<EOF
{
  "task_name": "Test hypothesis: ${HYPOTHESIS}",
  "description": "Test hypothesis: ${HYPOTHESIS}",
  "language": "python",
  "context": {
    "hypothesis_testing": "true",
    "artifact_names": "hypothesis_test_report.md"
  },
  "force_regenerate": true,
  "max_retries": 1,
  "timeout": 60
}
EOF

echo "Making API request to ${HDN_URL}/api/v1/intelligent/execute..."
echo "(This may take 30-60 seconds for code generation and execution)"

RESPONSE=$(curl -s --max-time 90 -w "\n%{http_code}" -X POST "${HDN_URL}/api/v1/intelligent/execute" \
  -H "Content-Type: application/json" \
  -d "@${TEMP_JSON}")

rm -f "$TEMP_JSON"

# Check if curl timed out or failed
if [ $? -ne 0 ]; then
    echo -e "${RED}❌ Request failed or timed out${NC}"
    echo "This could mean:"
    echo "  - The server is not responding"
    echo "  - The request is taking too long (>90s)"
    echo "  - Network connectivity issues"
    exit 1
fi

# Extract HTTP status code (last line)
HTTP_CODE=$(echo "$RESPONSE" | tail -n1)
# Extract body (all but last line)
RESPONSE_BODY=$(echo "$RESPONSE" | head -n -1)

# Check HTTP status
if [ "$HTTP_CODE" != "200" ]; then
    echo -e "${RED}❌ HTTP Error: ${HTTP_CODE}${NC}"
    echo "Response:"
    echo "$RESPONSE_BODY"
    exit 1
fi

# Use the body for further processing
RESPONSE="$RESPONSE_BODY"

# Check if request was successful
if [ $? -ne 0 ]; then
    echo -e "${RED}❌ Failed to make API request${NC}"
    exit 1
fi

# Extract workflow ID if available
WORKFLOW_ID=$(echo "$RESPONSE" | jq -r '.workflow_id // empty' 2>/dev/null || echo "")
if [ -n "$WORKFLOW_ID" ]; then
    echo -e "${GREEN}✅ Request successful - Workflow ID: ${WORKFLOW_ID}${NC}"
else
    echo -e "${YELLOW}⚠️  No workflow ID in response${NC}"
fi

echo ""
echo -e "${BLUE}2️⃣ Checking Generated Code${NC}"
echo "-----------------------------------"

# Extract generated code
GENERATED_CODE=$(echo "$RESPONSE" | jq -r '.generated_code.code // empty' 2>/dev/null || echo "")
DESCRIPTION_USED=$(echo "$RESPONSE" | jq -r '.generated_code.description // empty' 2>/dev/null || echo "")

if [ -z "$GENERATED_CODE" ]; then
    echo -e "${RED}❌ No generated code in response${NC}"
    echo "Response:"
    echo "$RESPONSE" | jq '.' 2>/dev/null || echo "$RESPONSE"
    exit 1
fi

echo -e "${GREEN}✅ Generated code received (${#GENERATED_CODE} chars)${NC}"
echo ""

# Check for key improvements in generated code
echo -e "${BLUE}Checking for enhanced prompt features:${NC}"

# Check 1: Term extraction (should use regex, not split words)
if echo "$GENERATED_CODE" | grep -q "re.findall\|re.search"; then
    echo -e "${GREEN}✅ Uses regex for term extraction (not word splitting)${NC}"
else
    echo -e "${YELLOW}⚠️  May not use regex for term extraction${NC}"
fi

# Check 2: Explicit property returns in Cypher
if echo "$GENERATED_CODE" | grep -q "RETURN.*AS name.*AS description"; then
    echo -e "${GREEN}✅ Uses explicit property returns (RETURN ... AS name ... AS description)${NC}"
elif echo "$GENERATED_CODE" | grep -q "RETURN c.name AS name"; then
    echo -e "${GREEN}✅ Uses explicit property returns (RETURN c.name AS name)${NC}"
else
    echo -e "${YELLOW}⚠️  May not use explicit property returns${NC}"
fi

# Check 3: Correct result access (result['name'], not result['c'])
if echo "$GENERATED_CODE" | grep -q "result\['name'\]\|result\[\"name\"\]\|result.get('name'"; then
    echo -e "${GREEN}✅ Accesses results correctly (result['name'])${NC}"
else
    echo -e "${YELLOW}⚠️  May not access results correctly${NC}"
fi

# Check 4: Avoids wrong patterns
if echo "$GENERATED_CODE" | grep -q "result\['c'\]"; then
    echo -e "${RED}❌ Still uses wrong pattern: result['c']${NC}"
else
    echo -e "${GREEN}✅ Does not use wrong pattern result['c']${NC}"
fi

# Check 5: Report structure
if echo "$GENERATED_CODE" | grep -q "## Hypothesis\|## Evidence\|## Conclusion"; then
    echo -e "${GREEN}✅ Includes correct report structure (Hypothesis, Evidence, Conclusion)${NC}"
else
    echo -e "${YELLOW}⚠️  May not include correct report structure${NC}"
fi

# Check 6: Term extraction from hypothesis (not placeholder)
if echo "$GENERATED_CODE" | grep -q "$TEST_EVENT\|test_event"; then
    echo -e "${GREEN}✅ Extracts actual terms from hypothesis (not placeholder)${NC}"
else
    echo -e "${YELLOW}⚠️  May not extract actual terms from hypothesis${NC}"
fi

echo ""
echo -e "${BLUE}3️⃣ Checking Description Used${NC}"
echo "-----------------------------------"

if [ -n "$DESCRIPTION_USED" ]; then
    echo -e "${GREEN}✅ Description received (${#DESCRIPTION_USED} chars)${NC}"
    
    # Check if enhanced description is being used
    if echo "$DESCRIPTION_USED" | grep -q "STEP 5\|CRITICAL REQUIREMENTS"; then
        echo -e "${GREEN}✅ Enhanced description is being used!${NC}"
        echo ""
        echo "Description preview (first 500 chars):"
        echo "$DESCRIPTION_USED" | head -c 500
        echo "..."
    else
        echo -e "${RED}❌ Enhanced description NOT being used (missing STEP 5 or CRITICAL REQUIREMENTS)${NC}"
        echo ""
        echo "Description preview (first 500 chars):"
        echo "$DESCRIPTION_USED" | head -c 500
        echo "..."
    fi
else
    echo -e "${YELLOW}⚠️  No description in response${NC}"
fi

echo ""
echo -e "${BLUE}4️⃣ Generated Code Preview${NC}"
echo "-----------------------------------"
echo "First 50 lines of generated code:"
echo "$GENERATED_CODE" | head -50
echo "..."

echo ""
echo -e "${BLUE}5️⃣ Next Steps${NC}"
echo "-----------------------------------"
echo "1. Check HDN server logs for:"
echo "   - 🧪 [INTELLIGENT] Enhanced description length: X chars"
echo "   - 🧪 [INTELLIGENT] Enhanced description preview"
echo ""
echo "2. If workflow ID was returned, check for artifacts:"
if [ -n "$WORKFLOW_ID" ]; then
    echo "   Workflow ID: ${WORKFLOW_ID}"
    echo "   Check: ${HDN_URL}/api/v1/workflows/${WORKFLOW_ID}/files"
fi
echo ""
echo "3. Verify the generated report has:"
echo "   - Proper term extraction (not individual words)"
echo "   - Evidence with actual descriptions (not 'None')"
echo "   - Complete report structure"

echo ""
echo -e "${GREEN}✅ Test completed!${NC}"

