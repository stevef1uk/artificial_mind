#!/usr/bin/env python3
"""
Test script to send a code generation request to the LLM proxy,
mimicking how HDN would request code generation.
"""

import requests
import json
import sys

# Configuration - adjust these to match your setup
PROXY_BASE = "http://192.168.1.60:11434"  # From your llm-config-secret.yaml
MODEL_NAME = "qwen2.5-1.5b-instruct"  # Adjust to your actual model name

# Try both Ollama API endpoints
ENDPOINTS = {
    "chat": f"{PROXY_BASE}/api/chat",
    "generate": f"{PROXY_BASE}/api/generate",
    "openai": f"{PROXY_BASE}/v1/chat/completions"
}

def test_code_generation():
    """
    Send a code generation request similar to how HDN's IntelligentExecutor would.
    This mimics the request format from intelligent_executor.go
    """
    
    # Craft a prompt similar to what HDN would send for code generation
    prompt = """You are an expert programmer. Generate Python code to solve the following task:

Task: Write a function that calculates the factorial of a number using recursion.

Requirements:
- Function name: calculate_factorial
- Input: integer n (n >= 0)
- Output: factorial of n
- Include error handling for negative numbers
- Add docstring and type hints

Respond with ONLY the Python code, no explanations."""

    # Ollama API format (as used by HDN when provider is "ollama")
    request_payload = {
        "model": MODEL_NAME,
        "messages": [
            {
                "role": "user",
                "content": prompt
            }
        ],
        "stream": False
    }
    
    print("=" * 80)
    print("🧪 Testing LLM Proxy - Code Generation Request")
    print("=" * 80)
    print(f"📡 Endpoint: {PROXY_URL}")
    print(f"🤖 Model: {MODEL_NAME}")
    print(f"📝 Prompt:\n{prompt}\n")
    print("=" * 80)
    print("🚀 Sending request...\n")
    
    try:
        response = requests.post(
            PROXY_URL,
            json=request_payload,
            headers={"Content-Type": "application/json"},
            timeout=120  # 2 minute timeout like HDN uses
        )
        
        print(f"📊 Status Code: {response.status_code}")
        print(f"📊 Response Headers: {dict(response.headers)}\n")
        
        if response.status_code == 200:
            response_data = response.json()
            print("✅ SUCCESS - Response received:")
            print("=" * 80)
            print(json.dumps(response_data, indent=2))
            print("=" * 80)
            
            # Extract the generated code (Ollama format)
            if "message" in response_data and "content" in response_data["message"]:
                generated_code = response_data["message"]["content"]
                print("\n🎯 Generated Code:")
                print("=" * 80)
                print(generated_code)
                print("=" * 80)
                return True
            else:
                print("⚠️  Unexpected response format - no message.content found")
                return False
        else:
            print(f"❌ FAILED - Status: {response.status_code}")
            print(f"Response: {response.text}")
            return False
            
    except requests.exceptions.Timeout:
        print("❌ TIMEOUT - Request took longer than 120 seconds")
        return False
    except requests.exceptions.ConnectionError as e:
        print(f"❌ CONNECTION ERROR - Could not connect to {PROXY_URL}")
        print(f"Error: {e}")
        return False
    except Exception as e:
        print(f"❌ ERROR - {type(e).__name__}: {e}")
        return False

def test_simple_chat():
    """
    Send a simple chat request to verify basic connectivity.
    """
    
    request_payload = {
        "model": MODEL_NAME,
        "messages": [
            {
                "role": "user",
                "content": "Hello! Please respond with 'LLM proxy is working correctly.'"
            }
        ],
        "stream": False
    }
    
    print("\n" + "=" * 80)
    print("🧪 Testing LLM Proxy - Simple Chat Request")
    print("=" * 80)
    print(f"📡 Endpoint: {PROXY_URL}")
    print(f"🤖 Model: {MODEL_NAME}")
    print("🚀 Sending request...\n")
    
    try:
        response = requests.post(
            PROXY_URL,
            json=request_payload,
            headers={"Content-Type": "application/json"},
            timeout=60
        )
        
        if response.status_code == 200:
            response_data = response.json()
            if "message" in response_data and "content" in response_data["message"]:
                print("✅ Response:", response_data["message"]["content"])
                return True
        else:
            print(f"❌ Failed with status {response.status_code}")
            return False
            
    except Exception as e:
        print(f"❌ Error: {e}")
        return False

if __name__ == "__main__":
    print("\n🔬 LLM Proxy Test Suite")
    print("This script mimics how HDN would request code generation from the LLM\n")
    
    # Test 1: Simple chat
    if not test_simple_chat():
        print("\n⚠️  Simple chat test failed. Check your proxy configuration.")
        sys.exit(1)
    
    # Test 2: Code generation
    if test_code_generation():
        print("\n✅ All tests passed! Your LLM proxy is working correctly for code generation.")
        sys.exit(0)
    else:
        print("\n❌ Code generation test failed.")
        sys.exit(1)
