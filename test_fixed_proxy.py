#!/usr/bin/env python3
"""
Test the FIXED proxy on port 11434 with /v1/chat/completions endpoint
This mimics how HDN would call the LLM for code generation
"""
import requests
import json

# Test the FIXED proxy (should be running on port 11434)
PROXY_URL = "http://192.168.1.60:11434/v1/chat/completions"
MODEL = "qwen2.5-1.5b-ax650"

# Code generation prompt (like HDN's IntelligentExecutor would send)
PROMPT = """Write a Python function to calculate factorial using recursion. 
Include type hints and error handling for negative numbers.
Return ONLY the code, no explanations."""

print("="*70)
print("🧪 Testing FIXED TPU Proxy - Code Generation")
print("="*70)
print(f"📡 URL: {PROXY_URL}")
print(f"🤖 Model: {MODEL}")
print(f"📝 Prompt: {PROMPT}\n")

# OpenAI-compatible format (as HDN uses)
payload = {
    "model": MODEL,
    "messages": [
        {
            "role": "user",
            "content": PROMPT
        }
    ],
    "temperature": 0.7,
    "max_tokens": 1000,
    "stream": False
}

print("🚀 Sending request...")
print(f"📦 Payload: {json.dumps(payload, indent=2)}\n")

try:
    response = requests.post(
        PROXY_URL,
        json=payload,
        headers={"Content-Type": "application/json"},
        timeout=300  # 5 minutes for TPU
    )
    
    print(f"📊 Status Code: {response.status_code}\n")
    
    if response.status_code == 200:
        data = response.json()
        print("✅ SUCCESS - Response received!")
        print("="*70)
        print(json.dumps(data, indent=2))
        print("="*70)
        
        # Extract generated code
        if "choices" in data and len(data["choices"]) > 0:
            content = data["choices"][0]["message"]["content"]
            print("\n🎯 Generated Code:")
            print("="*70)
            print(content)
            print("="*70)
        else:
            print("\n⚠️  Unexpected response format")
    else:
        print(f"❌ FAILED - Status: {response.status_code}")
        print(f"Response: {response.text}")
        
except requests.exceptions.Timeout:
    print("❌ TIMEOUT - Request took longer than 5 minutes")
except requests.exceptions.ConnectionError as e:
    print(f"❌ CONNECTION ERROR: {e}")
    print("\n💡 Make sure the fixed proxy is running:")
    print("   python3 fixed_tpu_proxy.py")
except Exception as e:
    print(f"❌ ERROR: {type(e).__name__}: {e}")

print("\n" + "="*70)
