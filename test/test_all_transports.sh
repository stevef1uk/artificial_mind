#!/bin/bash
# Test all three transport types sequentially

# set -e (disabled so all tests run even if one fails)

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

SCRAPER_URL="${PLAYWRIGHT_SCRAPER_URL:-http://localhost:8085}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

echo -e "${BLUE}🧪 Testing All Transport Types${NC}"
echo "=================================="
echo ""

# Check if scraper service is running
echo -e "${BLUE}🔍 Checking scraper service...${NC}"
if ! curl -sf "$SCRAPER_URL/health" > /dev/null 2>&1; then
    echo -e "${RED}❌ Scraper service not reachable at $SCRAPER_URL${NC}"
    echo "   Start it with: docker run -d --name scraper-dev -p 8085:8080 playwright-scraper:dev"
    exit 1
fi
echo -e "${GREEN}✅ Scraper service is running${NC}"
echo ""

# Track results
PASSED=0
FAILED=0
declare -a RESULTS

# Test Plane
echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo -e "${BLUE}Test 1/3: Plane${NC}"
echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
if bash "$SCRIPT_DIR/test_scraper_plane.sh"; then
    RESULTS+=("${GREEN}✅ Plane - PASSED${NC}")
    ((PASSED++))
else
    RESULTS+=("${RED}❌ Plane - FAILED${NC}")
    ((FAILED++))
fi
echo ""

# Test Train
echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo -e "${BLUE}Test 2/3: Train${NC}"
echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
if bash "$SCRIPT_DIR/test_scraper_train.sh"; then
    RESULTS+=("${GREEN}✅ Train - PASSED${NC}")
    ((PASSED++))
else
    RESULTS+=("${RED}❌ Train - FAILED${NC}")
    ((FAILED++))
fi
echo ""

# Test Car
echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo -e "${BLUE}Test 3/3: Car${NC}"
echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
if bash "$SCRIPT_DIR/test_scraper_car.sh"; then
    RESULTS+=("${GREEN}✅ Car - PASSED${NC}")
    ((PASSED++))
else
    RESULTS+=("${RED}❌ Car - FAILED${NC}")
    ((FAILED++))
fi
echo ""

# Summary
echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo -e "${BLUE}Summary${NC}"
echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
for result in "${RESULTS[@]}"; do
    echo -e "  $result"
done
echo ""
echo -e "Total: ${GREEN}$PASSED passed${NC}, ${RED}$FAILED failed${NC}"
echo ""

if [ $FAILED -eq 0 ]; then
    echo -e "${GREEN}✅ ALL TESTS PASSED!${NC}"
    echo ""
    echo -e "${BLUE}Next steps:${NC}"
    echo "  1. Deploy to Kubernetes (see DEPLOYMENT_GUIDE_SCRAPER.md)"
    echo "  2. Update n8n workflows with transport-specific configs"
    echo "  3. Test end-to-end in production"
    exit 0
else
    echo -e "${RED}❌ SOME TESTS FAILED${NC}"
    echo ""
    echo "Check the logs above for details."
    exit 1
fi

