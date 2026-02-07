#!/bin/bash
# Automated local test for Playwright scraper service

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

PROJECT_ROOT="/home/stevef/dev/artificial_mind"
SCRAPER_PORT=8080
CONTAINER_NAME="scraper-test"

echo -e "${BLUE}🧪 Playwright Scraper - Local Test${NC}"
echo "======================================"
echo ""

# Cleanup function
cleanup() {
    echo -e "\n${YELLOW}🧹 Cleaning up...${NC}"
    docker stop $CONTAINER_NAME 2>/dev/null || true
    docker rm $CONTAINER_NAME 2>/dev/null || true
}

# Set trap to cleanup on exit
trap cleanup EXIT

# Check prerequisites
echo -e "${BLUE}1️⃣  Checking prerequisites...${NC}"

if ! command -v docker &> /dev/null; then
    echo -e "${RED}❌ Docker not found. Please install Docker.${NC}"
    exit 1
fi

if ! command -v jq &> /dev/null; then
    echo -e "${RED}❌ jq not found. Please install jq.${NC}"
    exit 1
fi

# Check if port is available
if lsof -Pi :$SCRAPER_PORT -sTCP:LISTEN -t >/dev/null 2>&1; then
    echo -e "${RED}❌ Port $SCRAPER_PORT is already in use${NC}"
    echo "   Please stop the service using this port first."
    exit 1
fi

echo -e "${GREEN}✅ Prerequisites OK${NC}"
echo ""

# Build scraper image
echo -e "${BLUE}2️⃣  Building scraper service...${NC}"
cd $PROJECT_ROOT

docker build -t playwright-scraper:test \
    -f services/playwright_scraper/Dockerfile \
    services/playwright_scraper/ > /tmp/build.log 2>&1

if [ $? -eq 0 ]; then
    echo -e "${GREEN}✅ Build successful${NC}"
    
    # Show image size
    SIZE=$(docker images playwright-scraper:test --format "{{.Size}}")
    echo "   Image size: $SIZE"
else
    echo -e "${RED}❌ Build failed${NC}"
    echo "   Check /tmp/build.log for details"
    tail -20 /tmp/build.log
    exit 1
fi
echo ""

# Start scraper container
echo -e "${BLUE}3️⃣  Starting scraper service...${NC}"

docker run -d \
    --name $CONTAINER_NAME \
    -p $SCRAPER_PORT:8080 \
    playwright-scraper:test > /dev/null

# Wait for service to be ready
echo -e "${YELLOW}   Waiting for service to start...${NC}"
MAX_WAIT=30
WAITED=0

while [ $WAITED -lt $MAX_WAIT ]; do
    if curl -sf http://localhost:$SCRAPER_PORT/health > /dev/null 2>&1; then
        echo -e "${GREEN}✅ Service started (${WAITED}s)${NC}"
        break
    fi
    sleep 1
    WAITED=$((WAITED + 1))
    echo -n "."
done

if [ $WAITED -ge $MAX_WAIT ]; then
    echo -e "\n${RED}❌ Service failed to start within ${MAX_WAIT}s${NC}"
    echo -e "\n${YELLOW}Container logs:${NC}"
    docker logs $CONTAINER_NAME
    exit 1
fi
echo ""

# Show service info
echo -e "${BLUE}📊 Service Info:${NC}"
docker logs $CONTAINER_NAME 2>&1 | grep -E "(Chromium|Worker|Configuration)" || true
echo ""

# Test health endpoint
echo -e "${BLUE}4️⃣  Testing health endpoint...${NC}"

HEALTH=$(curl -s http://localhost:$SCRAPER_PORT/health)
if echo "$HEALTH" | jq -e '.status == "healthy"' > /dev/null 2>&1; then
    echo -e "${GREEN}✅ Health check passed${NC}"
    echo "$HEALTH" | jq '.'
else
    echo -e "${RED}❌ Health check failed${NC}"
    echo "$HEALTH"
    exit 1
fi
echo ""

# Test scraping job
echo -e "${BLUE}5️⃣  Testing scraping job (EcoTree Car: Portsmouth → London)...${NC}"

TS_CONFIG="await page.locator('#geosuggest__input').first().fill('Portsmouth'); 
    await page.waitForTimeout(3000); 
    await page.getByText('Portsmouth').first().click(); 
    await page.waitForTimeout(1000); 
    await page.locator('#geosuggest__input').nth(1).fill('London'); 
    await page.waitForTimeout(3000); 
    await page.getByText('London').first().click(); 
    await page.waitForTimeout(1000); 
    await page.locator('#return').click(); 
    await page.waitForTimeout(500); 
    await page.getByRole('link', { name: ' Calculate my emissions ' }).click(); 
    await page.waitForTimeout(5000);"

EXTRACTIONS='{"co2_kg": "Your footprint[\\s\\S]*?Carbon[\\s\\S]*?(\\d+(?:[.,]\\d+)?)\\s*kg", "distance_km": "Your footprint[\\s\\S]*?Kilometers[\\s\\S]*?(\\d+(?:[.,]\\d+)?)\\s*km"}'

START_RESP=$(curl -s -X POST http://localhost:$SCRAPER_PORT/scrape/start \
    -H 'Content-Type: application/json' \
    -d "{\"url\": \"https://ecotree.green/en/calculate-car-co2\", \"typescript_config\": $(echo "$TS_CONFIG" | jq -Rs .), \"extractions\": $EXTRACTIONS}")

JOB_ID=$(echo "$START_RESP" | jq -r '.job_id')
if [ -z "$JOB_ID" ] || [ "$JOB_ID" = "null" ]; then
    echo -e "${RED}❌ Failed to start job${NC}"
    echo "$START_RESP" | jq '.'
    exit 1
fi

echo -e "${GREEN}✅ Job started: $JOB_ID${NC}"
echo ""

# Poll for results
echo -e "${BLUE}6️⃣  Polling for results...${NC}"
TIMEOUT=90
ELAPSED=0
STATUS="pending"
START_TIME=$(date +%s)

while [ "$STATUS" != "completed" ] && [ "$STATUS" != "failed" ] && [ $ELAPSED -lt $TIMEOUT ]; do
    sleep 2
    ELAPSED=$(($(date +%s) - START_TIME))
    
    JOB_RESP=$(curl -s http://localhost:$SCRAPER_PORT/scrape/job?job_id=$JOB_ID)
    STATUS=$(echo "$JOB_RESP" | jq -r '.status')
    
    echo -e "${YELLOW}   [${ELAPSED}s] Status: $STATUS${NC}"
    
    if [ "$STATUS" = "completed" ]; then
        echo ""
        echo -e "${GREEN}✅ Job completed in ${ELAPSED}s!${NC}"
        echo ""
        echo -e "${BLUE}📊 Results:${NC}"
        echo "$JOB_RESP" | jq '.result'
        
        # Extract and display CO2 and distance
        CO2=$(echo "$JOB_RESP" | jq -r '.result.co2_kg // "N/A"')
        DISTANCE=$(echo "$JOB_RESP" | jq -r '.result.distance_km // "N/A"')
        
        echo ""
        echo -e "${GREEN}🚗 CO2 Emissions: $CO2 kg${NC}"
        echo -e "${GREEN}📏 Distance: $DISTANCE km${NC}"
        echo ""
        
        # Show container logs summary
        echo -e "${BLUE}📝 Service Logs (last 10 lines):${NC}"
        docker logs $CONTAINER_NAME 2>&1 | tail -10
        echo ""
        
        echo -e "${GREEN}✅ ALL TESTS PASSED!${NC}"
        echo ""
        echo -e "${BLUE}Next steps:${NC}"
        echo "   1. Review test/LOCAL_TEST_GUIDE.md for more tests"
        echo "   2. Deploy to Kubernetes: DEPLOYMENT_GUIDE_SCRAPER.md"
        echo "   3. Test with n8n workflows"
        
        exit 0
    elif [ "$STATUS" = "failed" ]; then
        echo ""
        echo -e "${RED}❌ Job failed${NC}"
        echo "$JOB_RESP" | jq '.error'
        
        echo -e "\n${YELLOW}Container logs:${NC}"
        docker logs $CONTAINER_NAME
        
        exit 1
    fi
done

if [ $ELAPSED -ge $TIMEOUT ]; then
    echo ""
    echo -e "${RED}❌ Job timed out after ${TIMEOUT}s${NC}"
    echo -e "\n${YELLOW}Last job status:${NC}"
    echo "$JOB_RESP" | jq '.'
    
    echo -e "\n${YELLOW}Container logs:${NC}"
    docker logs $CONTAINER_NAME
    
    exit 1
fi

