#!/bin/bash
# Example: Generate Playwright code for MyClimate using LLM

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
GENERATOR="$REPO_ROOT/cmd/llm_playwright_generator"

if [ ! -d "$GENERATOR" ]; then
  echo "❌ Generator not found at $GENERATOR"
    exit 1
fi

if [ -z "$PLAYWRIGHT_GEN_OPENAI_API_KEY" ]; then
  echo "❌ PLAYWRIGHT_GEN_OPENAI_API_KEY not set"
  echo "Set it in .env or export PLAYWRIGHT_GEN_OPENAI_API_KEY=sk-..."
  exit 1
fi

echo "🕸️ Generating Playwright code for MyClimate..."
echo ""

go run "$GENERATOR" \
  --provider openai \
    "https://co2.myclimate.org/en/flight_calculators/new" \
    "Calculate CO2 emissions for a one-way flight from CDG to LHR for 1 passenger in economy. Fill the form with: From=CDG, To=LHR, Aircraft=Boeing 737, 1 passenger, Economy class. Submit and extract the flight distance and CO2 emissions." \
    --output /tmp/myclimate_playwright.ts

echo ""
echo "📄 Generated code saved to /tmp/myclimate_playwright.ts"
echo ""
echo "To use this code with the scraper:"
echo "1. Read the generated TypeScript code"
echo "2. Send it to the scraper via /scrape/start endpoint"
echo "3. Or convert it to operations format for the scraper"
echo ""
echo "Example usage in scraper request:"
echo '{
  "url": "https://co2.myclimate.org/en/flight_calculators/new",
  "typeScriptConfig": "$(cat /tmp/myclimate_playwright.ts)",
  "extractions": {
    "distance": "ca\\. (\\d+)\\s*km",
    "emissions": "(\\d+\\.\\d+)\\s*(t|kg)"
  }
}'
