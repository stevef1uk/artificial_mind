# LLM Configuration Secret

This document explains how to configure LLM settings using Kubernetes secrets, allowing you to change the LLM provider and model without modifying deployment YAML files.

## Setup

### 1. Create the Secret

Apply the secret configuration:

```bash
kubectl apply -f k3s/llm-config-secret.yaml
```

### 2. Update the Secret

To change LLM settings, edit the secret directly:

```bash
# Edit the secret
kubectl edit secret llm-config -n agi
```

Or update specific values:

```bash
# Change LLM provider
kubectl patch secret llm-config -n agi --type='json' \
  -p='[{"op": "replace", "path": "/data/LLM_PROVIDER", "value": "'$(echo -n "openai" | base64)'"}]'

# Change model
kubectl patch secret llm-config -n agi --type='json' \
  -p='[{"op": "replace", "path": "/data/LLM_MODEL", "value": "'$(echo -n "gemma-3-1b-it-q4_k_m.gguf" | base64)'"}]'

# Change base URL
kubectl patch secret llm-config -n agi --type='json' \
  -p='[{"op": "replace", "path": "/data/OPENAI_BASE_URL", "value": "'$(echo -n "http://llama-server.agi.svc.cluster.local:8085" | base64)'"}]'
```

### 3. Restart Pods

After updating the secret, restart the pods to pick up new values:

```bash
# Restart HDN server
kubectl rollout restart deployment hdn-server-rpi58 -n agi

# Restart FSM server
kubectl rollout restart deployment fsm-server-rpi58 -n agi
```

## Configuration Options

### LLM_PROVIDER
- `"openai"` - For OpenAI API or OpenAI-compatible servers (e.g., llama.cpp)
- `"anthropic"` - For Anthropic Claude API
- `"local"` or `"ollama"` - For Ollama servers

### LLM_MODEL
The model name as it appears in your LLM server. Examples:
- `"gemma-3-1b-it-q4_k_m.gguf"` (llama.cpp)
- `"gemma3:latest"` (Ollama)
- `"gpt-4"` (OpenAI)

### OPENAI_BASE_URL
Base URL for OpenAI-compatible servers:
- `"http://llama-server.agi.svc.cluster.local:8085"` (llama.cpp in cluster)
- `"https://api.openai.com"` (OpenAI API)
- `"http://192.168.1.45:8085"` (External llama.cpp server)

### OLLAMA_BASE_URL
Base URL for Ollama servers:
- `"http://ollama.agi.svc.cluster.local:11434"` (Ollama in cluster)
- `"http://192.168.1.45:11434"` (External Ollama server)

## Example: Switching from Ollama to llama.cpp

1. Update the secret:
```bash
kubectl patch secret llm-config -n agi --type='json' \
  -p='[
    {"op": "replace", "path": "/data/LLM_PROVIDER", "value": "'$(echo -n "openai" | base64)'"},
    {"op": "replace", "path": "/data/LLM_MODEL", "value": "'$(echo -n "gemma-3-1b-it-q4_k_m.gguf" | base64)'"},
    {"op": "replace", "path": "/data/OPENAI_BASE_URL", "value": "'$(echo -n "http://llama-server.agi.svc.cluster.local:8085" | base64)'"}
  ]'
```

2. Restart pods:
```bash
kubectl rollout restart deployment hdn-server-rpi58 -n agi
kubectl rollout restart deployment fsm-server-rpi58 -n agi
```

## Example: Switching Back to Ollama

1. Update the secret:
```bash
kubectl patch secret llm-config -n agi --type='json' \
  -p='[
    {"op": "replace", "path": "/data/LLM_PROVIDER", "value": "'$(echo -n "local" | base64)'"},
    {"op": "replace", "path": "/data/LLM_MODEL", "value": "'$(echo -n "gemma3:latest" | base64)'"},
    {"op": "replace", "path": "/data/OLLAMA_BASE_URL", "value": "'$(echo -n "http://ollama.agi.svc.cluster.local:11434" | base64)'"}
  ]'
```

2. Restart pods:
```bash
kubectl rollout restart deployment hdn-server-rpi58 -n agi
kubectl rollout restart deployment fsm-server-rpi58 -n agi
```

## Verifying Configuration

Check what values are currently set:

```bash
# View all secret values (base64 encoded)
kubectl get secret llm-config -n agi -o yaml

# Decode and view specific values
kubectl get secret llm-config -n agi -o jsonpath='{.data.LLM_PROVIDER}' | base64 -d && echo
kubectl get secret llm-config -n agi -o jsonpath='{.data.LLM_MODEL}' | base64 -d && echo
kubectl get secret llm-config -n agi -o jsonpath='{.data.OPENAI_BASE_URL}' | base64 -d && echo
```

## Notes

- The `hdn-config.yaml` ConfigMap still contains default values, but environment variables from the secret will override them
- Both `OPENAI_BASE_URL` and `OLLAMA_BASE_URL` are marked as `optional: true` in the deployments, so you only need to set the one you're using
- The llama-server service (`llama-server-service.yaml`) needs to be updated with the correct IP address where your llama.cpp server is running

