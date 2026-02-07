#!/usr/bin/env python3
"""Debug script to see the raw LLM response from smart_scrape."""
import requests
import json
import os

HDN_URL = os.environ.get("HDN_URL", "http://localhost:8081")

payload = {
    "jsonrpc": "2.0",
    "id": 1,
    "method": "tools/call",
    "params": {
        "name": "smart_scrape",
        "arguments": {
            "url": "https://www.example.com",
            "goal": "Find the page title",
            "typescript_config": "",
            "extractions": {}
        }
    }
}

print("Sending request to smart_scrape...")
resp = requests.post(f"{HDN_URL}/mcp", json=payload, timeout=120)
print(f"Status: {resp.status_code}")
print(f"\nRaw Response:")
print(resp.text)
print(f"\nParsed JSON:")
try:
    data = resp.json()
    print(json.dumps(data, indent=2))
except Exception as e:
    print(f"Failed to parse: {e}")
