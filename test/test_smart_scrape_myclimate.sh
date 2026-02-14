#!/bin/bash

# Configuration
HDN_URL="http://localhost:8081/api/v1/mcp/tool/mcp_smart_scrape"
LOG_FILE="/tmp/hdn.log"

echo "🧠 Testing Smart Scraper - MyClimate"
echo "===================================="
echo "Goal: Calculate emissions for a one-way flight from CDG to LHR for 1 passenger in a Boeing 737. Extract the CO2 emissions amount and the unit (t or kg)."

# Trigger the smart scrape
# This specific flight on MyClimate requires filling multiple fields and clicking submit.
# It usually triggers the 'Two-Step' scraping logic.
curl -s -X POST "$HDN_URL" \
  -H "Content-Type: application/json" \
  -d '{
    "arguments": {
      "url": "https://co2.myclimate.org/en/flight_calculators/new",
      "goal": "Calculate co2 emissions for a flight from Paris (CDG) to London (LHR) one way, 1 passenger, aircraft Boeing 737. Extract the CO2 emissions value and the unit."
    }
  }' | jq .

echo -e "\n✅ Test Triggered. Check $LOG_FILE for execution details."
