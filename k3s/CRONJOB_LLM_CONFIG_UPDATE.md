# CronJob LLM Configuration Update

## Summary

Updated the news-ingestor and wiki-summarizer cron jobs to use the same LLM configuration approach as HDN server - using the `llm-config` Kubernetes secret instead of hardcoded values.

## Changes Made

### 1. Updated `k3s/llm-config-secret.yaml`

Added new configuration keys for Ollama and async LLM queue:

- `OLLAMA_BASE_URL`: Base URL for Ollama (without API path)
- `OLLAMA_URL`: Full URL with `/api/chat` endpoint (for news-ingestor)
- `LLM_ENDPOINT`: Full URL with `/api/generate` endpoint (for wiki-summarizer)
- `USE_ASYNC_LLM_QUEUE`: Enable async LLM queue (set to "1")
- `ASYNC_LLM_MAX_WORKERS`: Maximum concurrent workers (default: "3")
- `ASYNC_LLM_TIMEOUT_SECONDS`: Timeout for async requests (default: "60")

### 2. Updated `k3s/news-ingestor-cronjob.yaml`

**Before:**
- Hardcoded `OLLAMA_URL: "http://192.168.1.45:11434/api/chat"`
- Hardcoded `OLLAMA_MODEL: "gemma3:latest"`
- Hardcoded timeout and concurrency settings

**After:**
- All LLM settings now reference `llm-config` secret via `secretKeyRef`
- Added async LLM queue configuration
- Environment variables mapped:
  - `LLM_MODEL` ← `llm-config.LLM_MODEL`
  - `OLLAMA_MODEL` ← `llm-config.LLM_MODEL`
  - `OLLAMA_URL` ← `llm-config.OLLAMA_URL`
  - `LLM_ENDPOINT` ← `llm-config.OLLAMA_URL`
  - `LLM_TIMEOUT` ← `llm-config.LLM_TIMEOUT`
  - `LLM_TIMEOUT_SECONDS` ← `llm-config.LLM_TIMEOUT_SECONDS`
  - `LLM_MAX_CONCURRENT_REQUESTS` ← `llm-config.LLM_MAX_CONCURRENT_REQUESTS`
  - `USE_ASYNC_LLM_QUEUE` ← `llm-config.USE_ASYNC_LLM_QUEUE`
  - `ASYNC_LLM_MAX_WORKERS` ← `llm-config.ASYNC_LLM_MAX_WORKERS`
  - `ASYNC_LLM_TIMEOUT_SECONDS` ← `llm-config.ASYNC_LLM_TIMEOUT_SECONDS`

### 3. Updated `k3s/wiki-summarizer-cronjob.yaml`

**Before:**
- Hardcoded `LLM_PROVIDER: "ollama"`
- Hardcoded `LLM_ENDPOINT: "http://192.168.1.45:11434/api/generate"`
- Hardcoded `LLM_MODEL: "gemma3:latest"`
- Hardcoded timeout and concurrency settings

**After:**
- All LLM settings now reference `llm-config` secret via `secretKeyRef`
- Added async LLM queue configuration
- Environment variables mapped:
  - `LLM_PROVIDER` ← `llm-config.LLM_PROVIDER`
  - `LLM_ENDPOINT` ← `llm-config.LLM_ENDPOINT`
  - `LLM_MODEL` ← `llm-config.LLM_MODEL`
  - `LLM_TIMEOUT` ← `llm-config.LLM_TIMEOUT`
  - `LLM_TIMEOUT_SECONDS` ← `llm-config.LLM_TIMEOUT_SECONDS`
  - `LLM_MAX_CONCURRENT_REQUESTS` ← `llm-config.LLM_MAX_CONCURRENT_REQUESTS`
  - `USE_ASYNC_LLM_QUEUE` ← `llm-config.USE_ASYNC_LLM_QUEUE`
  - `ASYNC_LLM_MAX_WORKERS` ← `llm-config.ASYNC_LLM_MAX_WORKERS`
  - `ASYNC_LLM_TIMEOUT_SECONDS` ← `llm-config.ASYNC_LLM_TIMEOUT_SECONDS`

## Benefits

1. **Centralized Configuration**: All LLM settings in one place (`llm-config` secret)
2. **Easy Updates**: Change LLM model/endpoint without modifying deployment YAMLs
3. **Consistency**: Same configuration approach across HDN, FSM, and cron jobs
4. **Async Queue Support**: Both cron jobs now support async LLM queue system
5. **No Redeployment**: Update secret and restart pods, no need to rebuild images

## Usage

### Update LLM Configuration

To change LLM settings, edit the secret:

```bash
kubectl edit secret llm-config -n agi
```

Or update specific values:

```bash
# Change model
kubectl patch secret llm-config -n agi --type='json' \
  -p='[{"op": "replace", "path": "/data/LLM_MODEL", "value": "'$(echo -n "gemma3:latest" | base64)'"}]'

# Change Ollama URL
kubectl patch secret llm-config -n agi --type='json' \
  -p='[{"op": "replace", "path": "/data/OLLAMA_URL", "value": "'$(echo -n "http://192.168.1.45:11434/api/chat" | base64)'"}]'
```

### Apply Changes

After updating the secret, apply the updated cron jobs:

```bash
kubectl apply -f k3s/llm-config-secret.yaml
kubectl apply -f k3s/news-ingestor-cronjob.yaml
kubectl apply -f k3s/wiki-summarizer-cronjob.yaml
```

### Manual Test Run

To test the changes immediately (without waiting for cron schedule):

```bash
# Test news ingestor
kubectl create job --from=cronjob/news-ingestor-cronjob manual-news-ingest -n agi

# Test wiki summarizer
kubectl create job --from=cronjob/wiki-summarizer-cronjob manual-wiki-summarize -n agi
```

## Configuration Reference

### LLM Config Secret Keys

| Key | Description | Example |
|-----|-------------|---------|
| `LLM_PROVIDER` | LLM provider type | `"ollama"` or `"openai"` |
| `LLM_MODEL` | Model name | `"gemma3:latest"` |
| `OLLAMA_BASE_URL` | Ollama base URL | `"http://192.168.1.45:11434"` |
| `OLLAMA_URL` | Ollama chat endpoint | `"http://192.168.1.45:11434/api/chat"` |
| `LLM_ENDPOINT` | LLM endpoint (generate) | `"http://192.168.1.45:11434/api/generate"` |
| `LLM_TIMEOUT` | Timeout string | `"120s"` |
| `LLM_TIMEOUT_SECONDS` | Timeout in seconds | `"120"` |
| `LLM_MAX_CONCURRENT_REQUESTS` | Max concurrent requests | `"2"` |
| `USE_ASYNC_LLM_QUEUE` | Enable async queue | `"1"` |
| `ASYNC_LLM_MAX_WORKERS` | Async workers | `"3"` |
| `ASYNC_LLM_TIMEOUT_SECONDS` | Async timeout | `"60"` |

## Notes

- The `llm-config` secret must exist before applying the cron jobs
- If using Ollama, ensure `LLM_PROVIDER` is set to `"ollama"` in the secret
- Async LLM queue is enabled by default (`USE_ASYNC_LLM_QUEUE=1`)
- All timeout and concurrency settings are now centralized in the secret

