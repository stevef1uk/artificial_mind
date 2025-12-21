# Changing LLM Model in Kubernetes Deployment

This guide shows all the locations where you need to update the LLM model configuration when deploying to Kubernetes.

## Current Model
Currently configured: `gemma3:latest`

## Files to Update

### 1. Main FSM Configuration
**File:** `config/server-k3s.yaml`
- **Line 5:** Change `model: Qwen2.5-VL-7B-Instruct:latest` to your desired model

### 2. HDN ConfigMap
**File:** `k3s/hdn-config.yaml`
- **Line 13:** In `config.json` → `settings.model`
- **Line 32:** In `domain.json` → `config.settings.model`

### 3. FSM Server Deployment
**File:** `k3s/fsm-server-rpi58.yaml`
- **Line 40:** Environment variable `LLM_MODEL`

### 4. HDN Server Deployment
**File:** `k3s/hdn-server-rpi58.yaml`
- **Line 54:** Environment variable `LLM_MODEL`

### 5. News Ingestor CronJob
**File:** `k3s/news-ingestor-cronjob.yaml`
- **Line 25:** Environment variable `OLLAMA_MODEL`

### 6. Wiki Summarizer CronJob
**File:** `k3s/wiki-summarizer-cronjob.yaml`
- **Line 33:** Environment variable `LLM_MODEL`

## Quick Update Script

After updating the files, you'll need to:

1. **Update the ConfigMap** (for HDN):
   ```bash
   kubectl apply -f k3s/hdn-config.yaml
   ```

2. **Restart the deployments** to pick up environment variable changes:
   ```bash
   kubectl rollout restart deployment/fsm-server-rpi58 -n agi
   kubectl rollout restart deployment/hdn-server-rpi58 -n agi
   ```

3. **For CronJobs**, the changes will take effect on the next scheduled run, or you can manually trigger them:
   ```bash
   kubectl create job --from=cronjob/news-ingestor-cronjob manual-news-ingest -n agi
   kubectl create job --from=cronjob/wiki-summarizer-cronjob manual-wiki-summarize -n agi
   ```

## Example: Changing to a Different Model

If you want to change to `llama3.2:latest`, you would update:

1. `config/server-k3s.yaml`: `model: llama3.2:latest`
2. `k3s/hdn-config.yaml`: Both occurrences of `"model": "llama3.2:latest"`
3. `k3s/fsm-server-rpi58.yaml`: `value: "llama3.2:latest"`
4. `k3s/hdn-server-rpi58.yaml`: `value: "llama3.2:latest"`
5. `k3s/news-ingestor-cronjob.yaml`: `value: "llama3.2:latest"`
6. `k3s/wiki-summarizer-cronjob.yaml`: `value: "llama3.2:latest"`

## Notes

- Make sure the model is available on your Ollama instance at `http://192.168.1.45:11434`
- All services should use the same model for consistency
- After changes, verify the model is being used by checking pod logs:
  ```bash
  kubectl logs -f deployment/fsm-server-rpi58 -n agi | grep -i model
  kubectl logs -f deployment/hdn-server-rpi58 -n agi | grep -i model
  ```

