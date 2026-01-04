# Tool System Analysis for Kubernetes

## Overview

The AGI system uses a tool registry system where tools are registered in Redis and can be invoked via the HDN API. This document analyzes why tools might not be working in your Kubernetes deployment.

## Tool Registration Flow

### 1. Bootstrap on Startup
- **Location**: `hdn/server.go:302`
- **Function**: `BootstrapSeedTools()`
- **When**: Called during HDN server startup
- **What it does**:
  1. Checks if `tools_bootstrap.json` exists (in working dir or `config/` dir)
  2. If found, registers all tools from that file
  3. If not found, registers default minimal set:
     - `tool_http_get` (HTTP GET tool)
     - `tool_html_scraper` (HTML scraper)
  4. Registers ARM64-specific tools if conditions are met:
     - `tool_ssh_executor` (if `EXECUTION_METHOD=ssh` or `ENABLE_ARM64_TOOLS=true` or running on ARM64)

### 2. Tool Discovery Endpoint
- **Endpoint**: `POST /api/v1/tools/discover`
- **Location**: `hdn/tools.go:325`
- **What it does**:
  - Registers 3 tools:
    1. `tool_http_get`
    2. `tool_wiki_bootstrapper`
    3. `tool_docker_exec` (if Docker available) OR `tool_ssh_executor` (if ARM64/SSH enabled)

### 3. Manual Registration
- **Scripts**: `bootstrap-tools.sh` and `register-all-tools.sh`
- **What they do**:
  - `bootstrap-tools.sh`: Calls `/api/v1/tools/discover` endpoint
  - `register-all-tools.sh`: Registers 11 default tools via `/api/v1/tools` endpoint

## Tool Storage

- **Redis Key**: `tools:registry` (SET containing tool IDs)
- **Tool Metadata**: `tool:{id}` (String containing JSON tool definition)
- **Verification**: `kubectl exec -n agi deployment/redis -- redis-cli SMEMBERS tools:registry`

## Tool Invocation Flow

1. **Request**: `POST /api/v1/tools/{id}/invoke`
2. **Location**: `hdn/tools.go:537` (`handleInvokeTool`)
3. **Steps**:
   - Load tool metadata from Redis
   - Check principles gate (`CheckActionWithPrinciples`)
   - Check sandbox permissions
   - Execute tool based on ID (switch statement)
   - Log tool call result

## Common Issues

### Issue 1: Tools Not Registered
**Symptoms**:
- `kubectl exec -n agi deployment/redis -- redis-cli SMEMBERS tools:registry` returns empty
- No tools visible in Monitor UI

**Causes**:
1. `BootstrapSeedTools()` failed silently
2. Redis connection failed during bootstrap
3. Principles gate blocked tool registration
4. `tools_bootstrap.json` not found and defaults not registered

**Diagnosis**:
```bash
# Check HDN logs for bootstrap messages
kubectl logs -n agi deployment/hdn-server-rpi58 | grep -i bootstrap

# Check for tool registration
kubectl logs -n agi deployment/hdn-server-rpi58 | grep -i "register.*tool"
```

**Fix**:
```bash
# Manually bootstrap tools
cd k3s
./bootstrap-tools.sh

# Or register all tools
./register-all-tools.sh
```

### Issue 2: Tools Registered But Not Invoked
**Symptoms**:
- Tools exist in Redis registry
- No tool invocations in logs
- Tasks fail with "no tools available"

**Causes**:
1. Tool invocation blocked by principles
2. Execution method misconfigured (SSH vs Docker)
3. Tool executor not properly initialized
4. HDN URL misconfigured (tools call back to HDN)

**Diagnosis**:
```bash
# Check tool invocation logs
kubectl logs -n agi deployment/hdn-server-rpi58 | grep -i "invoke.*tool"

# Check execution method
kubectl exec -n agi deployment/hdn-server-rpi58 -- sh -c 'echo $EXECUTION_METHOD'

# Check HDN URL
kubectl exec -n agi deployment/hdn-server-rpi58 -- sh -c 'echo $HDN_URL'
```

**Fix**:
- Ensure `EXECUTION_METHOD` is set correctly (`ssh` or `docker`)
- Ensure `HDN_URL` points to correct service (should be `http://hdn-server-rpi58.agi.svc.cluster.local:8080` or `http://localhost:8080`)

### Issue 3: SSH Execution Method Issues
**Symptoms**:
- `EXECUTION_METHOD=ssh` but tools fail
- SSH connection errors

**Causes**:
1. SSH keys not mounted in pod
2. `RPI_HOST` not set or incorrect
3. SSH keys not authorized on target host

**Diagnosis**:
```bash
# Check SSH keys in pod
kubectl exec -n agi deployment/hdn-server-rpi58 -- ls -la /root/.ssh/

# Check RPI_HOST
kubectl exec -n agi deployment/hdn-server-rpi58 -- sh -c 'echo $RPI_HOST'
```

**Fix**:
- Ensure `ssh-keys` secret is mounted (see `hdn-server-rpi58.yaml:186-188`)
- Ensure `RPI_HOST` is set correctly
- Ensure SSH public key is in `~/.ssh/authorized_keys` on target host

### Issue 4: Docker Execution Method Issues
**Symptoms**:
- `EXECUTION_METHOD=docker` but tools fail
- Docker socket errors

**Causes**:
1. Docker socket not mounted
2. Docker not available in pod
3. Permission issues

**Diagnosis**:
```bash
# Check Docker socket
kubectl exec -n agi deployment/hdn-server-rpi58 -- test -S /var/run/docker.sock && echo "Socket exists" || echo "Socket missing"
```

**Fix**:
- Mount Docker socket in deployment (not currently done in `hdn-server-rpi58.yaml`)
- Or use SSH execution method instead

### Issue 5: Principles Blocking Tools
**Symptoms**:
- Tools registered but invocations return 403
- "blocked by principles" errors

**Causes**:
1. Principles server not accessible
2. Principles gate too restrictive
3. Tool safety level too high

**Diagnosis**:
```bash
# Check principles server
kubectl get pods -n agi -l app=principles-server

# Check principles URL
kubectl exec -n agi deployment/hdn-server-rpi58 -- sh -c 'echo $PRINCIPLES_URL'
```

**Fix**:
- Ensure principles server is running
- Check principles configuration
- Review tool safety levels

## Configuration Checklist

### HDN Deployment (`hdn-server-rpi58.yaml`)
- [ ] `EXECUTION_METHOD` set correctly (`ssh` or `docker`)
- [ ] `ENABLE_ARM64_TOOLS=true` if using SSH executor
- [ ] `RPI_HOST` set if using SSH
- [ ] `HDN_URL` set correctly
- [ ] `REDIS_URL` points to Redis service
- [ ] `PRINCIPLES_URL` points to principles service
- [ ] SSH keys secret mounted (`ssh-keys`)
- [ ] ConfigMap with `domain.json` and `config.json` mounted

### Redis
- [ ] Redis pod running
- [ ] Redis service accessible from HDN
- [ ] Tools registered in `tools:registry` set

### Principles Server
- [ ] Principles server pod running
- [ ] Principles service accessible from HDN
- [ ] Principles not blocking tool registration/invocation

## Diagnostic Commands

### Quick Check
```bash
# Run comprehensive diagnosis
cd k3s
./diagnose-tools.sh
```

### Manual Checks
```bash
# Check tools in Redis
kubectl exec -n agi deployment/redis -- redis-cli SMEMBERS tools:registry

# Check HDN logs
kubectl logs -n agi deployment/hdn-server-rpi58 --tail=100

# Test tool API
kubectl port-forward -n agi svc/hdn-server-rpi58 8080:8080
curl http://localhost:8080/api/v1/tools
```

### Test Tool Invocation
```bash
# Port forward HDN
kubectl port-forward -n agi svc/hdn-server-rpi58 8080:8080

# Test HTTP GET tool
curl -X POST http://localhost:8080/api/v1/tools/tool_http_get/invoke \
  -H "Content-Type: application/json" \
  -d '{"url":"https://example.com"}'
```

## Recommendations

1. **Always run bootstrap after deployment**:
   ```bash
   cd k3s
   ./register-all-tools.sh
   ```

2. **Monitor tool registration**:
   - Check HDN logs for bootstrap messages
   - Verify tools in Redis after startup

3. **Use SSH execution method on ARM64**:
   - Set `EXECUTION_METHOD=ssh`
   - Set `ENABLE_ARM64_TOOLS=true`
   - Configure SSH keys properly

4. **Verify tool invocation**:
   - Test with simple tools first (`tool_http_get`)
   - Check logs for invocation errors
   - Verify principles are not blocking

5. **Check service connectivity**:
   - HDN → Redis
   - HDN → Principles
   - HDN → Self (for tool callbacks)

## Next Steps

1. Run `./diagnose-tools.sh` to get full system status
2. Check HDN logs for bootstrap/registration messages
3. Verify tools in Redis registry
4. Test tool invocation manually
5. Check execution method configuration
6. Verify SSH/Docker setup based on execution method





