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
# Support multiple backend instances for concurrency
# The proxy will Load Balance (Round Robin) + Failover between these
LLM_ORIGINS = [
    "http://127.0.0.1:8000",  # Instance 1
    "http://127.0.0.1:8001"   # Instance 2 (Optional)
]
current_origin_idx = 0
client = httpx.AsyncClient(timeout=None)

def get_next_origin():
    """Get next backend URL (Round Robin)"""
    global current_origin_idx
    origin = LLM_ORIGINS[current_origin_idx]
    current_origin_idx = (current_origin_idx + 1) % len(LLM_ORIGINS)
    return origin

async def send_to_tpu(method, endpoint, json_data=None, timeout=None):
    """Send request to TPU with Failover support"""
    global current_origin_idx
    start_idx = current_origin_idx
    retries = len(LLM_ORIGINS)
    
    for _ in range(retries):
        origin = get_next_origin()
        try:
            url = f"{origin}{endpoint}"
            if method == "POST":
                return await client.post(url, json=json_data, timeout=timeout)
            else:
                return await client.get(url, timeout=timeout)
        except Exception as e:
            print(f"⚠️  Backend {origin} failed: {e}. Trying next...")
            continue
            
    raise Exception("All TPU backends are down!")

@app.post("/v1/chat/completions")
async def openai_chat_completions(request: Request):
    """OpenAI-compatible chat completions endpoint optimized for code generation"""
    body = await request.json()
    messages = body.get("messages", [])
    stream = body.get("stream", False)
    
    # ... (prompt formatting logic remains same) ...
    
    # Simple prompt formatting - just concatenate messages
    formatted_prompt = ""
    for msg in messages:
        role = msg["role"]
        content = msg["content"]
        if not content.strip(): continue
        if role == "system": formatted_prompt += f"{content}\n\n"
        elif role == "user": formatted_prompt += f"{content}\n\n"
        elif role == "assistant": formatted_prompt += f"{content}\n\n"
    formatted_prompt += "Assistant:"

    print("\n" + "📡" + "-"*60)
    print(f"🔧 CODE GENERATION REQUEST")
    print(f"📊 Messages: {len(messages)} | Stream: {stream}")
    print("-" * 60 + "\n")

    try:
        # Step 1: Send Generation Request (with Load Balancing)
        # We don't get the response object here for streaming, just trigger generation
        # For non-streaming, we might want to poll specific origin.
        # Implication: The backend MUST be stateless or we stick to one for a session.
        # Current logic: /api/generate triggers, /api/generate_provider polls.
        # CRITICAL: We must poll the SAME origin that we triggered!
        
        # Select origin for THIS request
        origin = get_next_origin() 
        print(f"👉 Selected Backend: {origin}")
        
        try:
            await client.post(f"{origin}/api/generate", json={
                "prompt": formatted_prompt,
                "temperature": body.get("temperature", 0.7),
                "top-k": 40
            })
        except Exception as e:
            # Try Failover
            print(f"⚠️  Backend {origin} failed: {e}. Failover...")
            origin = get_next_origin() # Try next
            print(f"👉 Redirecting to: {origin}")
            await client.post(f"{origin}/api/generate", json={
                "prompt": formatted_prompt,
                "temperature": body.get("temperature", 0.7),
                "top-k": 40
            })

    except Exception as e:
        print(f"❌ TPU Connection Error: {e}")
        return JSONResponse(status_code=500, content={"error": str(e)})

    # ... (rest of logic needs to use 'origin' variable instead of LLM_ORIGIN) ...

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
        retries = 0
        max_retries = 1
        
        while True:
            try:
                resp = await client.get(f"{LLM_ORIGIN}/api/generate_provider")
                data = resp.json()
                chunk = data.get("response", "")
                is_done = data.get("done", False)

                if chunk:
                    # Check for TPU memory error
                    if "SetKVCache failed" in chunk:
                        print(f"⚠️  TPU Error detected in chunk: '{chunk.strip()}'")
                        
                        if retries < max_retries:
                            print(f"‼️ TPU MEMORY ERROR (Resetting... Attempt {retries+1}/{max_retries})")
                            try:
                                # Reset the TPU
                                await client.post(f"{LLM_ORIGIN}/api/reset", json={})
                                print("✅ TPU Reset Successful. Retrying generation...")
                                
                                # Give it a moment to reset
                                await asyncio.sleep(2.0)
                                
                                # Retry the generation command
                                await client.post(f"{LLM_ORIGIN}/api/generate", json={
                                    "prompt": formatted_prompt,
                                    "temperature": body.get("temperature", 0.7),
                                    "top-k": 40
                                })
                                
                                # Clear current response, increment retries, and continue loop
                                full_response = ""
                                start_time = time.time()  # Reset timer
                                retries += 1
                                continue
                                
                            except Exception as e:
                                print(f"❌ Failed to reset TPU: {e}")
                                full_response = f"Error: TPU memory full. Auto-reset failed: {e}"
                                break
                        else:
                            print("❌ Max retries reached. Returning error.")
                            full_response += f"\n[Error: TPU Memory Full - Max retries exceeded]\nOriginal error: {chunk}"
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

@app.post("/api/chat")
async def ollama_chat(request: Request):
    """Ollama-compatible chat endpoint with correct NDJSON formatting"""
    body = await request.json()
    messages = body.get("messages", [])
    stream = body.get("stream", False)
    
    # 1. Format Prompt (Same as OpenAI)
    formatted_prompt = ""
    for msg in messages:
        role = msg["role"]
        content = msg["content"]
        if not content.strip(): continue
        if role == "system": formatted_prompt += f"{content}\n\n"
        elif role == "user": formatted_prompt += f"{content}\n\n"
        elif role == "assistant": formatted_prompt += f"{content}\n\n"
    formatted_prompt += "Assistant:"

    print("\n" + "📡" + "-"*60)
    print(f"🔧 OLLAMA REQUEST")
    print(f"📊 Messages: {len(messages)} | Stream: {stream}")
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
        return JSONResponse(status_code=500, content={"error": str(e)})

    created_at = time.strftime("%Y-%m-%dT%H:%M:%S.%fZ", time.gmtime())

    if stream:
        async def stream_ollama_results():
            while True:
                try:
                    resp = await client.get(f"{LLM_ORIGIN}/api/generate_provider")
                    data = resp.json()
                    chunk = data.get("response", "")
                    is_done = data.get("done", False)

                    if chunk:
                        # Check for auto-reset (Same logic)
                        if "SetKVCache failed" in chunk:
                            print("‼️ TPU MEMORY FULL (Ollama Stream). Resetting...")
                            await client.post(f"{LLM_ORIGIN}/api/reset", json={})
                            await asyncio.sleep(2.0)
                            await client.post(f"{LLM_ORIGIN}/api/generate", json={"prompt": formatted_prompt})
                            continue

                        # OLLAMA FORMAT: JSON object per line
                        # Note: n8n and some clients look for 'response' (legacy) or 'message'
                        payload = {
                            "model": "qwen3-tpu",
                            "created_at": created_at,
                            "message": {"role": "assistant", "content": chunk},
                            "response": chunk, # Legacy/generate format support
                            "done": False
                        }
                        yield json.dumps(payload) + "\n"

                    if is_done:
                        # Final done message
                        final_payload = {
                            "model": "qwen3-tpu",
                            "created_at": time.strftime("%Y-%m-%dT%H:%M:%S.%fZ", time.gmtime()),
                            "done": True,
                            "total_duration": 0,
                            "load_duration": 0,
                            "prompt_eval_count": 0,
                            "eval_count": 0,
                            "context": [], # Empty context to prevent errors
                            "response": "", # Ensure field exists
                            "message": {"role": "assistant", "content": ""} # Ensure field exists
                        }
                        yield json.dumps(final_payload) + "\n"
                        break
                    
                    await asyncio.sleep(0.04)
                except Exception as e:
                    print(f"❌ Stream Error: {e}")
                    break

        return StreamingResponse(stream_ollama_results(), media_type="application/x-ndjson")
    
    else:
        # Non-streaming Ollama response
        full_response = ""
        start_time = time.time()
        retries = 0
        max_retries = 1
        
        while True:
            try:
                resp = await client.get(f"{LLM_ORIGIN}/api/generate_provider")
                data = resp.json()
                chunk = data.get("response", "")
                is_done = data.get("done", False)

                if chunk:
                    if "SetKVCache failed" in chunk:
                        # Auto-reset logic (same as OpenAI)
                        if retries < max_retries:
                            print(f"‼️ TPU Resetting (Attempt {retries+1})")
                            await client.post(f"{LLM_ORIGIN}/api/reset", json={})
                            await asyncio.sleep(2.0)
                            await client.post(f"{LLM_ORIGIN}/api/generate", json={"prompt": formatted_prompt})
                            full_response = ""
                            retries += 1
                            continue
                        else:
                            full_response += f"\n[Error: TPU Memory Full]"
                            break
                    
                    full_response += chunk

                if is_done:
                    break
                await asyncio.sleep(0.04)
            except Exception as e:
                break
        
        print(f"✅ Ollama Response: {len(full_response)} chars")
        
        return {
            "model": "qwen3-tpu",
            "created_at": created_at,
            "message": {
                "role": "assistant",
                "content": full_response
            },
            "done": True,
            "total_duration": int((time.time() - start_time) * 1e9), # nanoseconds
            "load_duration": 0,
            "prompt_eval_count": 0,
            "eval_count": 0
        }

@app.get("/api/tags")
async def list_ollama_tags():
    """Ollama-compatible endpoint to list models"""
    return {
        "models": [{
            "name": "qwen3-tpu",
            "model": "qwen3-tpu",
            "modified_at": time.strftime("%Y-%m-%dT%H:%M:%S.%fZ", time.gmtime()),
            "size": 1700000000,
            "digest": "sha256:qwen3-tpu",
            "details": {
                "parent_model": "",
                "format": "gguf",
                "family": "qwen2",
                "families": ["qwen2"],
                "parameter_size": "1.7B",
                "quantization_level": "Q8_0"
            }
        }]
    }

if __name__ == "__main__":
    import uvicorn
    print("🚀 Starting Code Generation TPU Proxy")
    print(f"📡 TPU Server: {LLM_ORIGIN}")
    print(f"🌐 Listening on: http://0.0.0.0:11434")
    print(f"🔧 Optimized for: Code generation (stateless, no chat history)")
    uvicorn.run(app, host="0.0.0.0", port=11434)
