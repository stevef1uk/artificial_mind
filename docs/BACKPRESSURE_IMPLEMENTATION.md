# LLM Queue Backpressure Implementation

## Problem
The async LLM queue was backing up with 800+ low-priority requests, causing:
- Chain-of-thought chat requests to timeout
- System overload
- Poor user experience

## Solution: Backpressure

Added queue size limits with automatic rejection when queues are full.

### Configuration

**Environment Variables:**
- `LLM_MAX_HIGH_PRIORITY_QUEUE` (default: 100) - Maximum high-priority requests in queue
- `LLM_MAX_LOW_PRIORITY_QUEUE` (default: 50) - Maximum low-priority requests in queue

**Defaults:**
- High-priority: 100 requests (user chat, chain-of-thought)
- Low-priority: 50 requests (background FSM tasks, autonomy)

### How It Works

1. **Enqueue Check**: Before adding a request to the queue, check if the queue is full
2. **Rejection**: If queue is full, reject the request immediately with an error
3. **Priority Handling**: 
   - High-priority requests get a higher limit (100)
   - Low-priority requests get a lower limit (50) to prevent backlog
4. **Monitoring**: Queue health is logged every 30 seconds
5. **Warnings**: Log warnings when queues exceed 80% capacity

### Benefits

- **Prevents Backlog**: Low-priority requests are rejected when queue is full
- **Protects User Requests**: High-priority requests have higher limits
- **Automatic Throttling**: System automatically throttles background tasks
- **Visibility**: Queue health is monitored and logged

### Behavior

**When Low-Priority Queue is Full:**
- New low-priority requests are rejected immediately
- Error message: "low-priority queue full (X/50 requests) - backpressure applied"
- Background tasks (FSM autonomy, goal generation) will fail gracefully
- User requests (high-priority) are still accepted

**When High-Priority Queue is Full:**
- New high-priority requests are rejected
- This should rarely happen, but protects against extreme load
- Error message: "high-priority queue full (X/100 requests)"

### Monitoring

Queue health is logged every 30 seconds:
```
📊 [ASYNC-LLM] Queue health: high=5/100 (5.0%) low=12/50 (24.0%) active_workers=2/2
```

Warnings when queues are >80% full:
```
⚠️ [ASYNC-LLM] Low-priority queue is 85% full (43/50) - backpressure may be applied
```

### Tuning

If you need to adjust limits:

```bash
# Increase low-priority limit (allows more background tasks)
LLM_MAX_LOW_PRIORITY_QUEUE=100

# Decrease high-priority limit (stricter user request limit)
LLM_MAX_HIGH_PRIORITY_QUEUE=50

# Increase both (if GPU can handle more)
LLM_MAX_HIGH_PRIORITY_QUEUE=200
LLM_MAX_LOW_PRIORITY_QUEUE=100
```

### Integration with DISABLE_BACKGROUND_LLM

The backpressure works alongside `DISABLE_BACKGROUND_LLM`:
- `DISABLE_BACKGROUND_LLM=1`: Stops processing low-priority requests entirely
- Backpressure: Rejects new low-priority requests when queue is full

Both can be used together for maximum control.

