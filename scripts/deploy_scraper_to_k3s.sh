#!/bin/bash
set -e

echo "🚀 Deploying Playwright Scraper to K3s"
echo "======================================="

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

# Configuration
SCRAPER_IMAGE="stevef1uk/playwright-scraper:latest"
HDN_IMAGE="stevef1uk/hdn-server:secure"
NAMESPACE="agi"

echo -e "${YELLOW}Step 1: Building Playwright Scraper Docker image${NC}"
cd services/playwright_scraper
docker build -t ${SCRAPER_IMAGE} .
cd ../..

echo -e "${GREEN}✅ Scraper image built${NC}"

echo -e "${YELLOW}Step 2: Pushing scraper image to registry${NC}"
docker push ${SCRAPER_IMAGE}

echo -e "${GREEN}✅ Scraper image pushed${NC}"

echo -e "${YELLOW}Step 3: Rebuilding HDN server with updated code${NC}"
docker build --no-cache -f Dockerfile.hdn.secure \
  --build-arg CUSTOMER_PUBLIC_KEY=secure/customer_public.pem \
  --build-arg VENDOR_PUBLIC_KEY=secure/vendor_public.pem \
  -t ${HDN_IMAGE} .

echo -e "${GREEN}✅ HDN server image built${NC}"

echo -e "${YELLOW}Step 4: Pushing HDN image to registry${NC}"
docker push ${HDN_IMAGE}

echo -e "${GREEN}✅ HDN server image pushed${NC}"

echo -e "${YELLOW}Step 5: Deploying scraper service to K3s${NC}"
kubectl apply -f k3s/playwright-scraper-deployment.yaml

echo -e "${GREEN}✅ Scraper service deployed${NC}"

echo -e "${YELLOW}Step 6: Waiting for scraper to be ready${NC}"
kubectl wait --for=condition=ready pod -l app=playwright-scraper -n ${NAMESPACE} --timeout=180s

echo -e "${GREEN}✅ Scraper is ready${NC}"

echo -e "${YELLOW}Step 7: Updating HDN server deployment with scraper URL${NC}"
kubectl apply -f k3s/hdn-server-rpi58.yaml

echo -e "${YELLOW}Step 8: Restarting HDN server deployment${NC}"
kubectl rollout restart deployment/hdn-server-rpi58 -n ${NAMESPACE}

echo -e "${YELLOW}Step 9: Waiting for HDN server to be ready${NC}"
kubectl wait --for=condition=ready pod -l app=hdn-server-rpi58 -n ${NAMESPACE} --timeout=180s

echo -e "${GREEN}✅ HDN server is ready${NC}"

echo ""
echo -e "${GREEN}🎉 Deployment complete!${NC}"
echo ""
echo "Scraper service URL (from within cluster):"
echo "  http://playwright-scraper.agi.svc.cluster.local:8080"
echo ""
echo "To test:"
echo "  kubectl logs -f deployment/playwright-scraper -n ${NAMESPACE}"
echo "  kubectl logs -f deployment/hdn-server-rpi58 -n ${NAMESPACE}"

