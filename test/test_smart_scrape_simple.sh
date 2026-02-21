#!/bin/bash

# Test smart scraper on a simple website
HDN_URL="http://localhost:8081/api/v1/tools/mcp_smart_scrape/invoke"

echo "🧠 Testing Smart Scraper - Books.toscrape.com"
echo "=============================================="
echo "Goal: Scrape book information" 

curl -s -X POST "$HDN_URL" \
  -H "Content-Type: application/json" \
  -d '{
    "url": "http://books.toscrape.com/",
    "goal": "List the first 3 books on the page. For each book extract: (1) the title, (2) the price in pounds (£), (3) the availability status. Return as a simple list."
  }' | jq '.'

echo -e "\n✅ Test Triggered. Check /tmp/hdn_server.log for execution details."
