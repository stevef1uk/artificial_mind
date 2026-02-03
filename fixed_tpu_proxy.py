#!/usr/bin/env python3
"""
Fixed TPU Proxy for Qwen2.5-1.5B on AX650
Works with Flask server that returns complete responses from /api/generate
"""
import httpx
import json
import time
from fastapi import FastAPI, Request
from fastapi.responses import JSONResponse

app = FastAPI()
LLM_ORIGIN = "http://127.0.0.1:8000"
client = httpx.AsyncClient(timeout=300.0)  # 5 minute timeout for slow TPU

# Define tags to remove
THINK_START = "<think>"
THINK_END = "</think>"

def clean_response(text):
    """Remove think tags from response"""
    return text.replace(THINK_START, "").replace(THINK_END, "")

@app.post("/v1/chat/completions")
async def openai_chat_completions(request: Request):
    """OpenAI-compatible chat completions endpoint"""
    body = await request.json()
    messages = body.get("messages", [])
    stream = body.get("stream", False)
    
    # Format messages into a single prompt
    formatted_prompt = ""
    for msg in messages:
        role = msg["role"]
        content = msg["content"]
        
        if "SetKVCache failed" in content or not content.strip():
            continue
            
        if role == "system":
            formatted_prompt += f"Instructions: {content}\n"
        elif role == "user":
            formatted_prompt += f"User: {content}\n"
        elif role == "assistant":
            formatted_prompt += f"Assistant: {content}\n"
    
    formatted_prompt += "Assistant:"

    print("\n" + "📡" + "-"*50)
    print(f"FORWARDING {len(messages)} MESSAGES TO TPU")
    print(f"STREAM MODE: {stream}")
    print(formatted_prompt)
    print("-" * 52 + "\n")

    try:
        # Call the Flask server's /api/generate endpoint
        # This endpoint returns a complete response, not streaming
        response = await client.post(
            f"{LLM_ORIGIN}/api/generate",
            json={
                "prompt": formatted_prompt,
                "stream": False
            },
            timeout=300.0  # 5 minutes for TPU processing
        )
        
        if response.status_code != 200:
            print(f"❌ Flask server error: {response.status_code}")
            print(f"Response: {response.text}")
            return JSONResponse(
                status_code=500,
                content={"error": f"LLM server error: {response.status_code}"}
            )
        
        # Parse the Flask server response
        data = response.json()
        print(f"📦 Received response from Flask server")
        print(f"Response keys: {list(data.keys())}")
        
        # Extract the generated text
        # Flask server should return {"response": "generated text"}
        generated_text = data.get("response", "")
        
        if not generated_text:
            print(f"⚠️  No 'response' field in data: {data}")
            # Try alternative field names
            generated_text = data.get("text", data.get("content", ""))
        
        print(f"✅ Generated text: {len(generated_text)} chars")
        if generated_text:
            print(f"Preview: {generated_text[:200]}...")
        
        # Clean the response
        cleaned_text = clean_response(generated_text)
        
        # Return in OpenAI format
        request_id = f"chatcmpl-{int(time.time())}"
        return {
            "id": request_id,
            "object": "chat.completion",
            "created": int(time.time()),
            "model": "qwen2.5-1.5b-ax650",
            "choices": [{
                "index": 0,
                "message": {
                    "role": "assistant",
                    "content": cleaned_text
                },
                "finish_reason": "stop"
            }],
            "usage": {
                "prompt_tokens": len(formatted_prompt.split()),
                "completion_tokens": len(cleaned_text.split()),
                "total_tokens": len(formatted_prompt.split()) + len(cleaned_text.split())
            }
        }
        
    except httpx.TimeoutException:
        print("❌ Timeout waiting for TPU response")
        return JSONResponse(
            status_code=504,
            content={"error": "TPU processing timeout (5 minutes)"}
        )
    except Exception as e:
        print(f"❌ Error: {type(e).__name__}: {e}")
        return JSONResponse(
            status_code=500,
            content={"error": str(e)}
        )

@app.get("/v1/models")
async def list_models():
    """List available models"""
    return {
        "object": "list",
        "data": [{
            "id": "qwen2.5-1.5b-ax650",
            "object": "model",
            "created": int(time.time()),
            "owned_by": "axera-tech"
        }]
    }

@app.get("/health")
async def health_check():
    """Health check endpoint"""
    try:
        # Check if Flask server is responding
        response = await client.get(f"{LLM_ORIGIN}/health", timeout=5.0)
        return {"status": "healthy", "flask_server": "connected"}
    except:
        return {"status": "degraded", "flask_server": "disconnected"}

if __name__ == "__main__":
    import uvicorn
    print("🚀 Starting TPU Proxy Server")
    print(f"📡 Forwarding to Flask LLM server at: {LLM_ORIGIN}")
    print(f"🌐 Listening on: http://0.0.0.0:11434")
    uvicorn.run(app, host="0.0.0.0", port=11434)
