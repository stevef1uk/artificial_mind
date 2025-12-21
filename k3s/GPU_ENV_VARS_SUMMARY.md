# GPU Limiting Environment Variables - Kubernetes Deployments

## Summary
All GPU limiting environment variables have been added to Kubernetes deployment files to prevent GPU overload and ensure consistent behavior across containerized and local environments.

## Environment Variables Added

### Core LLM Throttling
- `LLM_MAX_CONCURRENT_REQUESTS=2` - Limits concurrent LLM API calls globally
- `LLM_TIMEOUT=120s` - LLM request timeout (string format)
- `LLM_TIMEOUT_SECONDS=120` - LLM request timeout (numeric format)

### Service-Specific Limits

#### HDN Server (`hdn-server-rpi58.yaml`)
- ✅ `LLM_MAX_CONCURRENT_REQUESTS=2`
- ✅ `LLM_TIMEOUT_SECONDS=120`
- ✅ `LLM_TIMEOUT=120s`
- ✅ `HDN_MAX_CONCURRENT_EXECUTIONS=3`

#### FSM Server (`fsm-server-rpi58.yaml`)
- ✅ `LLM_MAX_CONCURRENT_REQUESTS=2` (NEW)
- ✅ `LLM_TIMEOUT=120s`
- ✅ `LLM_TIMEOUT_SECONDS=120` (NEW)
- ✅ `FSM_MAX_ACTIVE_GOALS=2`
- ✅ `FSM_MAX_CONCURRENT_HYP_TESTS=1`
- ✅ `FSM_BOOTSTRAP_RPM=30`
- ✅ `FSM_BOOTSTRAP_SEED_BATCH=2`

#### Monitor UI (`monitor-ui.yaml`)
- ✅ `LLM_MAX_CONCURRENT_REQUESTS=2` (NEW)
- ✅ `LLM_TIMEOUT=120s` (NEW)
- ✅ `LLM_TIMEOUT_SECONDS=120` (NEW)
- ✅ `MONITOR_MAX_CONCURRENT_GOALS=2`
- ✅ `MONITOR_LLM_WORKER_TIMEOUT_SECONDS=120`

#### News Ingestor CronJob (`news-ingestor-cronjob.yaml`)
- ✅ `LLM_TIMEOUT=120s`
- ✅ `LLM_TIMEOUT_SECONDS=120` (NEW)
- ✅ `LLM_MAX_CONCURRENT_REQUESTS=2` (NEW)

#### Wiki Summarizer CronJob (`wiki-summarizer-cronjob.yaml`)
- ✅ `LLM_TIMEOUT=120s`
- ✅ `LLM_TIMEOUT_SECONDS=120` (NEW)
- ✅ `LLM_MAX_CONCURRENT_REQUESTS=2` (NEW)

## Priority Queue
The priority queue system (user requests vs background tasks) is **automatic** and does not require additional environment variables. It's implemented in the code and will:
- Give HIGH priority to user chat/conversational requests
- Give LOW priority to background tasks (FSM, learning, etc.)

## Benefits

1. **Consistent Behavior**: Same GPU limits in Kubernetes and local environments
2. **GPU Protection**: Prevents GPU overload with conservative concurrent request limits
3. **Timeout Protection**: Prevents long-running requests from monopolizing resources
4. **Priority Queue**: User requests automatically get priority over background tasks

## Deployment

After updating the deployment files, apply them with:
```bash
kubectl apply -f k3s/hdn-server-rpi58.yaml
kubectl apply -f k3s/fsm-server-rpi58.yaml
kubectl apply -f k3s/monitor-ui.yaml
kubectl apply -f k3s/news-ingestor-cronjob.yaml
kubectl apply -f k3s/wiki-summarizer-cronjob.yaml
```

The pods will automatically restart with the new environment variables.

