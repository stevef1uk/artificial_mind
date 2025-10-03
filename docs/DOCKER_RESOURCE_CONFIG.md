# Docker Resource Configuration for Artificial Mind Services

This document describes the configurable Docker resource limits for Artificial Mind services running on different hardware platforms.

## Environment Variables

The following environment variables control Docker container resource limits when services execute tools:

| Variable | Default (Raspberry Pi) | Description |
|----------|------------------------|-------------|
| `DOCKER_MEMORY_LIMIT` | `512m` | Maximum memory per container |
| `DOCKER_CPU_LIMIT` | `1.0` | Maximum CPU cores per container |
| `DOCKER_PIDS_LIMIT` | `256` | Maximum number of processes per container |
| `DOCKER_TMPFS_SIZE` | `128m` | Size of writable tmpfs for temporary files |

## Platform-Specific Recommendations

### Raspberry Pi (ARM64)
```bash
DOCKER_MEMORY_LIMIT=256m
DOCKER_CPU_LIMIT=0.5
DOCKER_PIDS_LIMIT=128
DOCKER_TMPFS_SIZE=64m
```

### Desktop/Laptop (x86_64)
```bash
DOCKER_MEMORY_LIMIT=2g
DOCKER_CPU_LIMIT=2.0
DOCKER_PIDS_LIMIT=512
DOCKER_TMPFS_SIZE=256m
```

### Server (High-end)
```bash
DOCKER_MEMORY_LIMIT=4g
DOCKER_CPU_LIMIT=4.0
DOCKER_PIDS_LIMIT=1024
DOCKER_TMPFS_SIZE=512m
```

## Kubernetes Configuration

These environment variables are automatically set in the Kubernetes deployments:

- `k3s/hdn-server.yaml` - HDN server (executes tools via Docker)
- `k3s/fsm-server.yaml` - FSM server (executes tools via Docker)

## Local Development

### X86 Development (Default)
The `start_servers.sh` script automatically sets X86-optimized defaults for local development:

```bash
DOCKER_MEMORY_LIMIT=2g
DOCKER_CPU_LIMIT=2.0
DOCKER_PIDS_LIMIT=512
DOCKER_TMPFS_SIZE=256m
```

### Custom Configuration
You can override these settings by:

1. **Environment Variables** (temporary):
```bash
export DOCKER_MEMORY_LIMIT=4g
export DOCKER_CPU_LIMIT=4.0
./start_servers.sh
```

2. **`.env` file** (persistent):
```bash
cp env.example .env
# Edit .env with your preferred settings
./start_servers.sh
```

3. **Direct export** before running:
```bash
export DOCKER_MEMORY_LIMIT=256m
export DOCKER_CPU_LIMIT=0.5
export DOCKER_PIDS_LIMIT=128
export DOCKER_TMPFS_SIZE=64m
./start_servers.sh
```

## Impact

These settings affect:
- Tool execution performance
- Memory usage on the host system
- Number of concurrent tool executions
- Stability on resource-constrained systems

Lower limits improve stability on Raspberry Pi but may slow down complex tool executions.
