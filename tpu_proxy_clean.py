#!/usr/bin/env python3
"""
Optimized TPU Proxy for Code Generation
- No chat history (stateless)
- Simplified prompt formatting
- Optimized for HDN code generation requests
"""
import asyncio
import httpx
import json
import time
from fastapi import FastAPI, Request
from fastapi.responses import StreamingResponse, JSONResponse

app = FastAPI()
LLM_ORIGIN = "http://127.0.0.1:8000"
client = httpx.AsyncClient(timeout=None)

@app.post("/v1/chat/completions")
async def openai_chat_completions(request: Request):
    """OpenAI-compatible chat completions endpoint optimized for code generation"""
    body = await request.json()
    messages = body.get("messages", [])
    stream = body.get("stream", False)
    
    # Simple prompt formatting - just concatenate messages
    # No chat history, no special formatting
    formatted_prompt = ""
    for msg in messages:
        role = msg["role"]
        content = msg["content"]
        
        # Skip empty messages
        if not content.strip():
            continue
            
        # Simple format: just the content with role prefix
        if role == "system":
            formatted_prompt += f"{content}\n\n"
        elif role == "user":
            formatted_prompt += f"{content}\n\n"
        elif role == "assistant":
            formatted_prompt += f"{content}\n\n"
    
    # Add assistant prompt to trigger response
    formatted_prompt += "Assistant:"

    print("\n" + "📡" + "-"*60)
    print(f"🔧 CODE GENERATION REQUEST")
    print(f"📊 Messages: {len(messages)} | Stream: {stream}")
    print(f"📝 Prompt ({len(formatted_prompt)} chars):")
    print("-" * 60)
    print(formatted_prompt[:500] + ("..." if len(formatted_prompt) > 500 else ""))
    print("-" * 60 + "\n")

    try:
        # Send to TPU
        await client.post(f"{LLM_ORIGIN}/api/generate", json={
            "prompt": formatted_prompt,
            "temperature": body.get("temperature", 0.7),
            "top-k": 40
        })
    except Exception as e:
        print(f"❌ TPU Connection Error: {e}")
        return JSONResponse(
            status_code=500,
            content={"error": f"TPU connection failed: {str(e)}"}
        )

    if stream:
        # Streaming response
        async def stream_openai_results():
            request_id = f"chatcmpl-{int(time.time())}"
            
            while True:
                try:
                    resp = await client.get(f"{LLM_ORIGIN}/api/generate_provider")
                    data = resp.json()
                    chunk = data.get("response", "")
                    is_done = data.get("done", False)

                    if chunk:
                        payload = {
                            "id": request_id,
                            "object": "chat.completion.chunk",
                            "created": int(time.time()),
                            "model": "qwen3-tpu",
                            "choices": [{
                                "index": 0,
                                "delta": {"content": chunk},
                                "finish_reason": None
                            }]
                        }
                        yield f"data: {json.dumps(payload)}\n\n"

                    if is_done:
                        yield "data: [DONE]\n\n"
                        break
                    
                    await asyncio.sleep(0.04)
                except Exception as e:
                    print(f"❌ Stream Error: {e}")
                    break

        return StreamingResponse(stream_openai_results(), media_type="text/event-stream")
    
    else:
        # Non-streaming response (better for code generation)
        full_response = ""
        request_id = f"chatcmpl-{int(time.time())}"
        start_time = time.time()
        
        while True:
            try:
                resp = await client.get(f"{LLM_ORIGIN}/api/generate_provider")
                data = resp.json()
                chunk = data.get("response", "")
                is_done = data.get("done", False)

                if chunk:
                    # Check for TPU memory error
                    if "SetKVCache failed" in chunk:
                        print("‼️ TPU MEMORY FULL. Sending Reset Command...")
                        try:
                            # Reset the TPU
                            await client.post(f"{LLM_ORIGIN}/api/reset", json={})
                            print("✅ TPU Reset Successful. Retrying generation...")
                            
                            # Give it a moment to reset
                            await asyncio.sleep(1.0)
                            
                            # Retry the generation command
                            await client.post(f"{LLM_ORIGIN}/api/generate", json={
                                "prompt": formatted_prompt,
                                "temperature": body.get("temperature", 0.7),
                                "top-k": 40
                            })
                            
                            # Clear current response and continue loop to collect new response
                            full_response = ""
                            start_time = time.time()  # Reset timer
                            continue
                            
                        except Exception as e:
                            print(f"❌ Failed to reset TPU: {e}")
                            full_response = "Error: TPU memory full and reset failed."
                            break
                    
                    full_response += chunk

                if is_done:
                    break
                
                await asyncio.sleep(0.04)
            except Exception as e:
                print(f"⚠️  Collection Error: {e}")
                break
        
        elapsed = time.time() - start_time
        print(f"✅ Response collected: {len(full_response)} chars in {elapsed:.1f}s")
        if full_response:
            print(f"📄 Preview: {full_response[:200]}...")
        
        return {
            "id": request_id,
            "object": "chat.completion",
            "created": int(time.time()),
            "model": "qwen3-tpu",
            "choices": [{
                "index": 0,
                "message": {
                    "role": "assistant",
                    "content": full_response
                },
                "finish_reason": "stop"
            }],
            "usage": {
                "prompt_tokens": len(formatted_prompt.split()),
                "completion_tokens": len(full_response.split()),
                "total_tokens": len(formatted_prompt.split()) + len(full_response.split())
            }
        }

@app.get("/v1/models")
async def list_models():
    """List available models"""
    return {
        "object": "list",
        "data": [{
            "id": "qwen3-tpu",
            "object": "model",
            "created": int(time.time()),
            "owned_by": "axera-tech"
        }]
    }

@app.get("/health")
async def health_check():
    """Health check endpoint"""
    try:
        resp = await client.get(f"{LLM_ORIGIN}/health", timeout=5.0)
        return {"status": "healthy", "tpu_server": "connected"}
    except:
        return {"status": "degraded", "tpu_server": "disconnected"}

if __name__ == "__main__":
    import uvicorn
    print("🚀 Starting Code Generation TPU Proxy")
    print(f"📡 TPU Server: {LLM_ORIGIN}")
    print(f"🌐 Listening on: http://0.0.0.0:11434")
    print(f"🔧 Optimized for: Code generation (stateless, no chat history)")
    uvicorn.run(app, host="0.0.0.0", port=11434)
