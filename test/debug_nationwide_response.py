#!/usr/bin/env python3
"""
Debug test to see what the smart_scrape tool is actually returning
"""
import requests
import json
import sys
import os

HDN_URL = os.environ.get("HDN_URL", "http://localhost:8081")

payload = {
    "jsonrpc": "2.0",
    "id": 1,
    "method": "tools/call",
    "params": {
        "name": "smart_scrape",
        "arguments": {
            "url": "https://www.nationwide.co.uk/savings/cash-isas/",
            "goal": "Find the product names and interest rates from the savings table"
        }
    }
}

print("🧪 Calling smart_scrape with Nationwide URL...")
print(f"   POST {HDN_URL}/mcp")

resp = requests.post(f"{HDN_URL}/mcp", json=payload, timeout=60)
print(f"\n   Status: {resp.status_code}")

result = resp.json()

print(f"\n📋 Full JSON Response:")
print(json.dumps(result, indent=2))

if "result" in result:
    content = result["result"].get("content", [])
    if content:
        for item in content:
            if "text" in item:
                print(f"\n📊 Content Text:")
                print(item["text"])
