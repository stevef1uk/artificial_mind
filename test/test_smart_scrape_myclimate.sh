#!/bin/bash

# Configuration
HDN_URL="http://localhost:8081/api/v1/tools/mcp_smart_scrape/invoke"
LOG_FILE="/tmp/hdn_server.log"

echo "🧠 Testing Smart Scraper - MyClimate"
echo "===================================="
echo "Goal: Calculate emissions for a one-way flight from CDG to LHR for 1 passenger in a Boeing 737."
echo "Extract: (1) flight distance in km, (2) CO2 emissions per passenger with unit (kg or t)"

# Trigger the smart scrape
# This specific flight on MyClimate requires filling multiple fields and clicking submit.
# It usually triggers the 'Two-Step' scraping logic.
curl -s -X POST "$HDN_URL" \
  -H "Content-Type: application/json" \
  -d '{
    "url": "https://co2.myclimate.org/en/flight_calculators/new",
    "goal": "Fill form: From=CDG, To=LHR, Aircraft=BOEING_737, 1 passenger, Economy. Submit Calculate button and wait for results. After navigation to portfolio page, extract: (1) flight distance from div id=flight_calc_details (looks like: \"ca. 700 km\" or similar), capture the number and km unit; (2) CO2 emissions from strong id=co2_amount (looks like: \"0.314 t\" or similar), capture the number and unit (t or kg)."
  }' | jq .

echo -e "\n✅ Test Triggered. Check $LOG_FILE for execution details."
