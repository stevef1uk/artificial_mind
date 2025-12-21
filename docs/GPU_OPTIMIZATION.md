# GPU Optimization Guide

## Problem
The system was experiencing GPU overload and timeouts due to:
- Too many concurrent LLM requests overwhelming the GPU
- High concurrent execution limits allowing multiple simultaneous operations
- Very long timeout values (600 seconds) causing requests to hang

## Solutions Implemented

### 1. LLM Request Throttling
Added a global semaphore to limit concurrent LLM API calls:
- **Default**: 2 concurrent LLM requests maximum
- **Configurable**: Set `LLM_MAX_CONCURRENT_REQUESTS` environment variable
- **Location**: `hdn/llm_client.go`

This prevents the GPU from being overwhelmed by too many simultaneous model inference requests.

### 2. Reduced Concurrent Execution Limits
Updated defaults to be more conservative:
- `HDN_MAX_CONCURRENT_EXECUTIONS`: Reduced from 12 → **3** (default in code)
- `FSM_MAX_ACTIVE_GOALS`: Reduced from 8 → **2**
- `FSM_MAX_CONCURRENT_HYP_TESTS`: Reduced from 4 → **1**
- `MONITOR_MAX_CONCURRENT_GOALS`: Reduced from 8 → **2**
- `AUTO_EXECUTOR_MAX_CONCURRENT`: Reduced from 4 → **2**

### 3. Reduced Timeout Values
Shortened timeouts to prevent long-running requests:
- LLM timeout: Reduced from 600s → **120s** (2 minutes)
- Execution timeout: Reduced from 600s → **120s** (2 minutes)
- Monitor LLM worker timeout: Reduced from 600s → **120s**

### 4. Reduced Bootstrap Rates
Lowered background processing rates:
- `FSM_BOOTSTRAP_RPM`: Reduced from 120 → **30**
- `FSM_BOOTSTRAP_SEED_BATCH`: Reduced from 6 → **2**

## Configuration

### Environment Variables

Add these to your `.env` file or export them:

```bash
# LLM Request Throttling (most important for GPU)
LLM_MAX_CONCURRENT_REQUESTS=2

# Concurrent Execution Limits
HDN_MAX_CONCURRENT_EXECUTIONS=3
FSM_MAX_ACTIVE_GOALS=2
FSM_MAX_CONCURRENT_HYP_TESTS=1
MONITOR_MAX_CONCURRENT_GOALS=2
AUTO_EXECUTOR_MAX_CONCURRENT=2

# Timeouts (in seconds)
LLM_TIMEOUT=120s
MONITOR_LLM_WORKER_TIMEOUT_SECONDS=120

# Bootstrap Rates
FSM_BOOTSTRAP_RPM=30
FSM_BOOTSTRAP_SEED_BATCH=2
```

### Configuration Files

Updated files:
- `config/config.json`: LLM timeout reduced to 120s
- `hdn/config.json`: LLM timeout reduced to 120s
- `env.example`: All conservative defaults documented

## Tuning for Your Hardware

### If GPU is Still Overloaded
1. **Reduce LLM concurrency further**:
   ```bash
   LLM_MAX_CONCURRENT_REQUESTS=1
   ```

2. **Reduce execution concurrency**:
   ```bash
   HDN_MAX_CONCURRENT_EXECUTIONS=1
   FSM_MAX_ACTIVE_GOALS=1
   ```

3. **Disable background processing**:
   ```bash
   FSM_AUTONOMY=false
   DISABLE_NEWS_POLLER=true
   ```

### If You Have More GPU Capacity
You can gradually increase limits:
```bash
LLM_MAX_CONCURRENT_REQUESTS=3  # or 4 if GPU can handle it
HDN_MAX_CONCURRENT_EXECUTIONS=4
FSM_MAX_ACTIVE_GOALS=3
```

**Important**: Monitor GPU usage and increase gradually. If timeouts return, reduce again.

## Monitoring

Watch for these indicators:
- GPU utilization (should stay below 90% sustained)
- Request timeouts in logs
- "Server busy - too many concurrent executions" errors
- LLM request queue buildup

## Files Modified

1. `hdn/llm_client.go` - Added LLM request semaphore
2. `hdn/api.go` - Reduced default concurrent executions and timeouts
3. `hdn/intelligent_executor.go` - Reduced timeout defaults
4. `config/config.json` - Reduced LLM timeout
5. `hdn/config.json` - Reduced LLM timeout
6. `env.example` - Updated with conservative defaults
7. `monitor/templates/dashboard.html` - Reduced UI timeout

## Next Steps

1. **Restart services** to apply changes
2. **Monitor GPU usage** - should see immediate reduction
3. **Adjust limits** based on your hardware capacity
4. **Check logs** for timeout errors - should be reduced

If issues persist, consider:
- Using a smaller/faster model
- Offloading some LLM calls to a remote API
- Adding more GPU memory
- Using model quantization

