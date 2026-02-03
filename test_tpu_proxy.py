#!/usr/bin/env python3
"""Test the TPU proxy on the correct port and endpoint"""

import requests
import json

# CORRECTED: Your proxy is on port 8000, not 11434!
BASE_URL = "http://192.168.1.60:8000"
MODEL = "qwen2.5-1.5b-instruct"

# Simple coding task
PROMPT = """Write a Python function to calculate factorial using recursion. Include type hints."""

print("="*70)
print("🧪 Testing TPU Proxy - Code Generation")
print("="*70)
print(f"📡 URL: {BASE_URL}/api/generate")
print(f"🤖 Model: {MODEL}")
print(f"📝 Prompt: {PROMPT}\n")

# Use the /api/generate endpoint that your proxy supports
payload = {
    "model": MODEL,
    "prompt": PROMPT,
    "stream": False
}

print("🚀 Sending request...")
print(f"📦 Payload: {json.dumps(payload, indent=2)}\n")

try:
    response = requests.post(
        f"{BASE_URL}/api/generate",
        json=payload,
        headers={"Content-Type": "application/json"},
        timeout=120
    )
    
    print(f"📊 Status Code: {response.status_code}")
    print(f"📊 Headers: {dict(response.headers)}\n")
    
    if response.status_code == 200:
        print("✅ SUCCESS - Raw Response:")
        print("="*70)
        print(response.text)
        print("="*70)
        
        try:
            data = response.json()
            print("\n📋 Parsed JSON:")
            print(json.dumps(data, indent=2))
            
            # Try to extract the response text
            if "response" in data:
                print("\n🎯 Generated Code:")
                print("="*70)
                print(data["response"])
                print("="*70)
            elif "text" in data:
                print("\n🎯 Generated Code:")
                print("="*70)
                print(data["text"])
                print("="*70)
            else:
                print(f"\n⚠️  Response keys: {list(data.keys())}")
        except json.JSONDecodeError as e:
            print(f"\n⚠️  JSON decode error: {e}")
            print("Raw response might not be JSON")
    else:
        print(f"❌ FAILED - Status: {response.status_code}")
        print(f"Response: {response.text}")
        
except requests.exceptions.Timeout:
    print("❌ TIMEOUT - Request took longer than 120 seconds")
except requests.exceptions.ConnectionError as e:
    print(f"❌ CONNECTION ERROR: {e}")
except Exception as e:
    print(f"❌ ERROR: {type(e).__name__}: {e}")

print("\n" + "="*70)
