# Kubernetes Deployment Error Diagnosis

**Date:** December 20, 2025  
**Status:** Multiple issues identified, system mostly operational

## Executive Summary

Your Kubernetes deployment is running, but there are several errors occurring:

1. **CRITICAL:** Wiki-bootstrapper cronjob failing due to invalid vendor token signature
2. **MODERATE:** Weaviate GraphQL query errors with invalid `session_id` field
3. **LOW:** Neo4j configuration warnings (harmless but noisy)
4. **INFO:** FSM timeout guard checks (normal operation, not errors)

## Detailed Issues

### 1. CRITICAL: Wiki-Bootstrapper Token Signature Error

**Error Message:**
```
token signature invalid: crypto/rsa: verification error
```

**Affected Pods:**
- `wiki-bootstrapper-cronjob-29437650-4s76m` (Error state)
- `wiki-bootstrapper-cronjob-29437650-qqghn` (Error state)
- `wiki-bootstrapper-cronjob-29437650-q7j49` (Running but will fail)

**Root Cause:**
The vendor token stored in the `secure-vendor` Kubernetes secret is either:
- Expired or invalid
- Signed with a different private key than the one used to build the Docker images
- Corrupted or incorrectly formatted

**Impact:**
- Wiki-bootstrapper cronjob cannot unpack and run the encrypted binary
- Wikipedia knowledge ingestion is not working
- Cronjob runs every 10 minutes and fails each time

**Current Token Preview:**
The token starts with: `MjAyNS0xMi0zMTpTSkZpc2hlcjpzdGV2ZWZAc2pmaXNoZXIuY2...`

**Recommended Fix:**
1. Verify the vendor token is valid and not expired:
   ```bash
   cd ~/dev/artificial_mind/k3s
   ./generate-vendor-token.sh ~/dev/artificial_mind/secure
   ```

2. Update the Kubernetes secret:
   ```bash
   ./update-secrets.sh ~/dev/artificial_mind/secure
   ```

3. Verify the token matches the vendor_public.pem used during Docker image build

4. Check token expiration date (should be valid until 2025-12-31 based on license info)

### 2. MODERATE: Weaviate GraphQL Query Errors

**Error Message:**
```json
{
  "error": "Argument \"where\" has invalid value {session_id: {equal: \"system\"}}.\nIn field \"session_id\": Unknown field.",
  "level": "error",
  "msg": "unexpected error"
}
```

**Occurrences:**
- Seen in Weaviate logs at 2025-12-19T16:18:27Z and 2025-12-19T18:57:51Z

**Root Cause:**
The code is trying to filter Weaviate queries by `session_id`, but this field doesn't exist in the Weaviate schema. The `session_id` is stored in the `metadata` field as a nested property, not as a top-level field.

**Affected Code Locations:**
- `hdn/memory/vector_db_adapter.go` - `buildWhereClause()` function
- `hdn/api.go` - `handleSearchEpisodes()` function (line 3963-3964)
- `monitor/main.go` - Weaviate search queries

**Impact:**
- Episodic memory searches with session_id filters fail
- Monitor UI may show errors when filtering by session
- Some API endpoints return errors instead of filtered results

**Recommended Fix:**
Update the Weaviate query builder to filter on `metadata.session_id` instead of `session_id`:
```go
// Instead of:
filters["session_id"] = sid

// Use:
filters["metadata.session_id"] = sid
```

Or update the `buildWhereClause()` function to handle `session_id` by converting it to a metadata filter.

### 3. LOW: Neo4j Configuration Warnings

**Warning Messages:**
```
WARN  Unrecognized setting. No declared setting with name: PORT.7687.TCP.PORT
WARN  Unrecognized setting. No declared setting with name: PORT.7474.TCP.ADDR
WARN  Unrecognized setting. No declared setting with name: SERVICE.PORT
... (12 more similar warnings)
```

**Root Cause:**
Neo4j is receiving Kubernetes service discovery environment variables that it doesn't recognize. These are automatically injected by Kubernetes and are harmless.

**Impact:**
- No functional impact
- Logs are noisy with warnings
- May indicate Neo4j version compatibility issue (using 4.4-community, newer versions may handle this better)

**Recommended Fix:**
These warnings can be safely ignored. If you want to suppress them:
1. Upgrade to Neo4j 5.x which handles these environment variables better
2. Or filter out these specific warnings in log aggregation

### 4. INFO: FSM Timeout Guard Checks

**Log Messages:**
```
Timeout check: elapsed 2.40828365s, timeout 5m0s
Guard learning_timeout failed for event timer_tick
Guard hypothesis_timeout failed for event timer_tick
Guard reasoning_timeout failed for event timer_tick
```

**Root Cause:**
These are NOT errors. The FSM (Finite State Machine) has timeout guards that check if certain operations are taking too long. When a guard "fails", it means the timeout hasn't been exceeded yet, which is normal operation.

**Impact:**
- None - this is expected behavior
- The guards prevent operations from running too long

**Recommended Action:**
No action needed. These are informational logs showing the FSM is working correctly.

## Service Health Status

### ✅ Healthy Services
- **FSM Server** (`fsm-server-rpi58`) - Running, processing state transitions
- **HDN Server** (`hdn-server-rpi58`) - Running, handling API requests
- **Goal Manager** (`goal-manager`) - Running, processing goals
- **Principles Server** (`principles-server`) - Running
- **Monitor UI** (`monitor-ui`) - Running
- **NATS** (`nats`) - Running, no errors
- **Redis** (`redis`) - Running, no errors
- **Neo4j** (`neo4j`) - Running (warnings only, no errors)
- **Weaviate** (`weaviate`) - Running (GraphQL errors but service is up)

### ❌ Failing Services
- **Wiki-Bootstrapper CronJob** - Failing due to token signature error

## Recommended Actions (Priority Order)

### Immediate (Critical)
1. **Fix Wiki-Bootstrapper Token Issue**
   ```bash
   cd ~/dev/artificial_mind/k3s
   # Regenerate token if needed
   ./generate-vendor-token.sh ~/dev/artificial_mind/secure
   # Update Kubernetes secret
   ./update-secrets.sh ~/dev/artificial_mind/secure
   # Verify the pod can start
   kubectl get pods -n agi -l app=wiki-bootstrapper
   ```

### Short-term (Moderate)
2. **Fix Weaviate session_id Filtering**
   - Update `hdn/memory/vector_db_adapter.go` to handle `session_id` in metadata
   - Update `hdn/api.go` `handleSearchEpisodes()` to use metadata filter
   - Test with: `curl "http://hdn-server-rpi58.agi.svc.cluster.local:8080/api/episodes/search?q=test&session_id=system"`

### Long-term (Low Priority)
3. **Suppress Neo4j Warnings** (optional)
   - Consider upgrading to Neo4j 5.x
   - Or configure log filtering

## Verification Commands

```bash
# Check all pod statuses
kubectl get pods -n agi

# Check wiki-bootstrapper logs
kubectl logs -n agi -l app=wiki-bootstrapper --tail=50

# Check Weaviate errors
kubectl logs -n agi deployment/weaviate --tail=100 | grep -i error

# Check FSM server health
kubectl logs -n agi deployment/fsm-server-rpi58 --tail=50 | grep -i error

# Check HDN server health
kubectl logs -n agi deployment/hdn-server-rpi58 --tail=50 | grep -i error

# Verify vendor token secret exists
kubectl get secret -n agi secure-vendor

# Check recent events
kubectl get events -n agi --sort-by='.lastTimestamp' | tail -20
```

## Additional Notes

- The system is mostly operational despite these errors
- Main services (FSM, HDN, Goal Manager) are functioning correctly
- The wiki-bootstrapper failure is the most critical issue as it prevents knowledge ingestion
- Weaviate errors are intermittent and don't prevent the service from running
- All services are properly configured with correct service URLs and environment variables










