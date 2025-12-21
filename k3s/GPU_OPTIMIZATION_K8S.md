# Kubernetes GPU Optimization Configuration

This document describes the GPU optimization settings applied to Kubernetes deployments to prevent GPU overload and reduce timeouts.

## Changes Applied

### 1. HDN Server (`hdn-server-rpi58.yaml`)

**Concurrency Limits:**
- `HDN_MAX_CONCURRENT_EXECUTIONS`: Reduced from `8` → `3`
- `LLM_MAX_CONCURRENT_REQUESTS`: Added `2` (new - limits concurrent LLM API calls)

**Timeouts:**
- `LLM_TIMEOUT_SECONDS`: Reduced from `600` → `120`
- `LLM_TIMEOUT`: Added `120s`

**ConfigMap (`hdn-config.yaml`):**
- `llm_timeout_seconds`: Reduced from `600` → `120`

### 2. FSM Server (`fsm-server-rpi58.yaml`)

**Concurrency Limits:**
- `FSM_MAX_ACTIVE_GOALS`: Added `2` (new - limits concurrent active goals)
- `FSM_MAX_CONCURRENT_HYP_TESTS`: Added `1` (new - limits concurrent hypothesis tests)

**Bootstrap Rates (Reduced to prevent GPU overload):**
- `FSM_BOOTSTRAP_RPM`: Reduced from `60` → `30`
- `FSM_BOOTSTRAP_SEED_BATCH`: Reduced from `20` → `2`

**Timeouts:**
- `LLM_TIMEOUT`: Added `120s`

### 3. Monitor UI (`monitor-ui.yaml`)

**Concurrency Limits:**
- `MONITOR_MAX_CONCURRENT_GOALS`: Added `2` (new - limits concurrent goal processing)

**Timeouts:**
- `MONITOR_LLM_WORKER_TIMEOUT_SECONDS`: Added `120`

### 4. CronJobs

**Wiki Summarizer (`wiki-summarizer-cronjob.yaml`):**
- `LLM_TIMEOUT`: Added `120s`

**News Ingestor (`news-ingestor-cronjob.yaml`):**
- `LLM_TIMEOUT`: Added `120s`

## Impact

These changes ensure that:

1. **LLM Request Throttling**: Maximum 2 concurrent LLM requests across all services (via `LLM_MAX_CONCURRENT_REQUESTS`)
2. **Execution Concurrency**: Maximum 3 concurrent executions in HDN server
3. **FSM Activity**: Limited to 2 active goals and 1 concurrent hypothesis test
4. **Reduced Timeouts**: All LLM timeouts reduced from 10 minutes to 2 minutes
5. **Lower Background Load**: Bootstrap rates reduced to prevent GPU saturation

## Applying Changes

To apply these changes to your Kubernetes cluster:

```bash
# Apply updated configurations
kubectl apply -f k3s/hdn-server-rpi58.yaml
kubectl apply -f k3s/fsm-server-rpi58.yaml
kubectl apply -f k3s/monitor-ui.yaml
kubectl apply -f k3s/hdn-config.yaml
kubectl apply -f k3s/wiki-summarizer-cronjob.yaml
kubectl apply -f k3s/news-ingestor-cronjob.yaml

# Restart pods to pick up new environment variables
kubectl rollout restart deployment/hdn-server-rpi58 -n agi
kubectl rollout restart deployment/fsm-server-rpi58 -n agi
kubectl rollout restart deployment/monitor-ui -n agi
```

## Monitoring

After applying changes, monitor:

1. **GPU Utilization**: Should stay below 90% sustained
2. **Pod Logs**: Check for "Server busy" messages (indicates throttling working)
3. **Request Timeouts**: Should be reduced significantly
4. **Response Times**: May be slightly slower but more stable

## Tuning for Your Hardware

If you have more GPU capacity, you can gradually increase:

```yaml
# In hdn-server-rpi58.yaml
- name: LLM_MAX_CONCURRENT_REQUESTS
  value: "3"  # or 4 if GPU can handle it
- name: HDN_MAX_CONCURRENT_EXECUTIONS
  value: "4"  # or 5

# In fsm-server-rpi58.yaml
- name: FSM_MAX_ACTIVE_GOALS
  value: "3"  # or 4
- name: FSM_BOOTSTRAP_RPM
  value: "45"  # or 60
```

**Important**: Monitor GPU usage and increase gradually. If timeouts return, reduce again.

## Files Modified

1. `k3s/hdn-server-rpi58.yaml` - Added LLM throttling and reduced concurrency
2. `k3s/fsm-server-rpi58.yaml` - Added FSM limits and reduced bootstrap rates
3. `k3s/monitor-ui.yaml` - Added monitor concurrency limits
4. `k3s/hdn-config.yaml` - Reduced LLM timeout in ConfigMap
5. `k3s/wiki-summarizer-cronjob.yaml` - Added LLM timeout
6. `k3s/news-ingestor-cronjob.yaml` - Added LLM timeout

