# Chat Timeout Fix

## Issue
Chain of thought chat requests were timing out at 120 seconds (2 minutes).

## Root Cause
The chat endpoint makes multiple LLM calls in sequence:
1. Intent parsing (LLM call)
2. Action determination (may use LLM)
3. Action execution (may use tools/LLM)
4. NLG response generation (LLM call)
5. Thought expression (may use LLM)

With LLM throttling (max 2 concurrent requests), these steps can queue up, requiring more time than the 2-minute timeout.

## Solution Applied

### 1. Server-Side Timeout (HDN Conversational API)
**File**: `hdn/conversational/api.go`
- Added 3-minute timeout context for chat processing
- Added proper timeout error handling

### 2. Monitor Proxy Timeout
**File**: `monitor/main.go`
- Increased HTTP client timeout: `120s` → `180s` (3 minutes)

### 3. Frontend Timeout
**File**: `monitor/templates/dashboard_tabs.html`
- Increased axios timeout: `120000ms` → `180000ms` (3 minutes)

## To Apply the Fix

You need to **rebuild and restart** the services:

```bash
# Rebuild the monitor service (includes the HTML template)
cd monitor
go build -o ../bin/monitor-ui

# Rebuild the HDN server (includes conversational API changes)
cd ../hdn
go build -o ../bin/hdn-server

# Restart services
# (Use your normal restart process - e.g., systemd, docker-compose, k8s, etc.)
```

### For Kubernetes:
```bash
# Rebuild and push images, then restart deployments
kubectl rollout restart deployment/monitor-ui -n agi
kubectl rollout restart deployment/hdn-server-rpi58 -n agi
```

## Verification

After rebuilding, the chat should:
- Have a 3-minute timeout instead of 2 minutes
- Show better error messages if it does timeout
- Handle multiple LLM calls more reliably

## If Still Timing Out

If you still see timeouts after rebuilding:

1. **Check GPU load**: The LLM throttling (max 2 concurrent) might be causing queuing
   - Monitor GPU usage
   - Consider temporarily increasing `LLM_MAX_CONCURRENT_REQUESTS` to 3

2. **Simplify the question**: Complex questions requiring many LLM calls may still timeout
   - Try breaking into simpler questions
   - Use the Tools tab for complex tasks instead

3. **Increase timeout further**: If needed, you can increase to 4-5 minutes:
   - `hdn/conversational/api.go`: Change `3*time.Minute` to `4*time.Minute` or `5*time.Minute`
   - `monitor/main.go`: Change `180 * time.Second` to `240 * time.Second` or `300 * time.Second`
   - `monitor/templates/dashboard_tabs.html`: Change `180000` to `240000` or `300000`

## Files Changed

1. `hdn/conversational/api.go` - Added 3-minute timeout context
2. `monitor/main.go` - Increased proxy timeout to 3 minutes
3. `monitor/templates/dashboard_tabs.html` - Increased frontend timeout to 3 minutes

