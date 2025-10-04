# Platform-Aware AGI System Setup

This system now automatically detects your platform and uses the appropriate method to run the Monitor UI.

## 🚀 Quick Start

### Start the System
```bash
./scripts/start_servers.sh
```

### Stop the System
```bash
./scripts/stop_servers.sh
```

## 🖥️ Platform-Specific Behavior

### 🍎 **Mac (Darwin)**
- **Monitor UI**: Runs in Docker container with `host.docker.internal` networking
- **Other Services**: Run natively on the host
- **Infrastructure**: Uses Docker Compose for Redis, Neo4j, Weaviate, NATS
- **Benefits**: Avoids Go template parsing issues, better service isolation

### 🐧 **Linux**
- **Monitor UI**: Runs natively as a Go binary
- **Other Services**: Run natively on the host
- **Infrastructure**: Uses Docker Compose for Redis, Neo4j, Weaviate, NATS
- **Benefits**: Lower resource usage, faster startup

## 🔧 Technical Details

### Mac Configuration
The Monitor UI on Mac uses:
- **Docker Image**: `monitor-ui-local` (built from `Dockerfile.monitor-ui.local`)
- **Network**: `artificial_mind_default` (Docker Compose network)
- **Volume Mounts**: `/tmp:/tmp:ro` (read-only access to host logs)
- **Log Output**: Redirected to `/tmp/monitor_ui.log` (clean terminal output)
- **Kubernetes Logging**: Disabled (`K8S_NAMESPACE=""` - no password prompts)
- **Service URLs**:
  - HDN: `http://host.docker.internal:8081`
  - Goal Manager: `http://host.docker.internal:8090`
  - FSM: `http://host.docker.internal:8083`
  - Principles: `http://host.docker.internal:8084`
  - Redis: `agi-redis:6379`
  - Neo4j: `http://agi-neo4j:7474`
  - Weaviate: `http://agi-weaviate:8080`
  - NATS: `nats://agi-nats:4222`

### Linux Configuration
The Monitor UI on Linux uses:
- **Binary**: `./bin/monitor-ui` (native Go binary)
- **Service URLs**:
  - HDN: `http://localhost:8081`
  - Goal Manager: `http://localhost:8090`
  - FSM: `http://localhost:8083`
  - Principles: `http://localhost:8084`
  - Redis: `redis://localhost:6379`
  - Neo4j: `http://localhost:7474`
  - Weaviate: `http://localhost:8080`
  - NATS: `nats://localhost:4222`

## 📁 File Structure

```
artificial_mind/
├── scripts/
│   ├── start_servers.sh         # Platform-aware startup script (replaces original)
│   ├── stop_servers.sh          # Platform-aware stop script (replaces original)
│   ├── start_servers_old.sh     # Original startup script (backup)
│   └── stop_servers_old.sh      # Original stop script (backup)
├── run-monitor-mac.sh           # Mac-specific Monitor UI script
├── Dockerfile.monitor-ui.local  # Docker image for Mac Monitor UI
└── PLATFORM_SETUP.md            # This file
```

## 🐛 Troubleshooting

### Mac Issues
- **Docker not running**: Start Docker Desktop
- **Port conflicts**: The script automatically frees port 8082
- **Container issues**: Run `docker ps` to check container status
- **Service connectivity**: Check that host services are running on correct ports
- **Password prompts**: Fixed by disabling Kubernetes logging (`K8S_NAMESPACE=""`)
- **Template errors**: Fixed by using Docker with symlink for template paths
- **Log output**: Check `/tmp/monitor_ui.log` for Monitor UI logs

### Linux Issues
- **Go not found**: Install Go or ensure it's in PATH
- **Template parsing errors**: The native version may have Go template issues
- **Port conflicts**: Check for processes using ports 8080-8090

### Common Solutions
- **Force rebuild**: Use `./scripts/start_servers.sh --rebuild-monitor` if image is outdated
- **Check logs**: Monitor UI logs are in `/tmp/monitor_ui.log` (Mac) or terminal (Linux)
- **Service status**: Visit http://localhost:8082/api/status to check all services
- **Clean restart**: Use `./scripts/stop_servers.sh` then `./scripts/start_servers.sh` for fresh start

## 🔄 Manual Override

If you want to force a specific method:

### Force Docker Monitor UI (even on Linux)
```bash
USE_DOCKER_MONITOR=true ./scripts/start_servers.sh
```

### Force Native Monitor UI (even on Mac)
```bash
USE_DOCKER_MONITOR=false ./scripts/start_servers.sh
```

### Force Rebuild Monitor UI Docker Image
```bash
./scripts/start_servers.sh --rebuild-monitor
```

### Skip Infrastructure Services
```bash
./scripts/start_servers.sh --skip-infra
```

## ⚡ Performance Optimizations

### Docker Build Caching
- **Smart Rebuild**: Only rebuilds Monitor UI Docker image when explicitly requested
- **Layer Caching**: Optimized Dockerfile for better build cache utilization
- **Force Rebuild**: Use `--rebuild-monitor` flag when needed
- **Skip by Default**: Reuses existing image to avoid unnecessary rebuilds

### Build Time Improvements
- **Go Mod Caching**: Dependencies cached separately from source code
- **Template Path Fix**: Symlink resolves template loading issues
- **Clean Terminal**: Monitor UI logs redirected to `/tmp/monitor_ui.log`
- **No Password Prompts**: Kubernetes logging disabled for local development

### Troubleshooting Improvements
- **Syntax Error Fixes**: Proper `if/else/fi` structure in shell scripts
- **Platform Detection**: Automatic Mac/Linux differentiation
- **Error Handling**: Graceful fallbacks and clear error messages

## 📊 Service Status

After starting, check service status:
- **Monitor UI**: http://localhost:8082
- **API Status**: http://localhost:8082/api/status
- **FSM Server**: http://localhost:8083
- **HDN Server**: http://localhost:8081
- **Principles Server**: http://localhost:8084

## 🔧 Recent Fixes & Improvements

### ✅ Issues Resolved
- **Blank Screen on Mac**: Fixed by using Docker with proper template paths
- **Password Prompts**: Disabled Kubernetes logging with `K8S_NAMESPACE=""`
- **Template Parsing Errors**: Resolved with Docker symlink (`ln -sf /app/monitor /monitor`)
- **Unnecessary Rebuilds**: Fixed by removing timestamp comparison, using image existence check
- **Syntax Errors**: Fixed shell script `if/else/fi` structure
- **Log Clutter**: Monitor UI output redirected to `/tmp/monitor_ui.log`

### 🚀 New Features
- **Platform-Aware Scripts**: Automatic Mac/Linux detection and appropriate handling
- **Docker Volume Mounts**: Host `/tmp` accessible for log reading
- **Clean Terminal Output**: No more Monitor UI logs cluttering the terminal
- **Smart Rebuild Logic**: Only rebuilds when explicitly requested
- **Comprehensive Error Handling**: Better error messages and fallbacks

## 🎯 Benefits

1. **Seamless Experience**: Users don't need to know about platform differences
2. **Optimal Performance**: Each platform uses the best method for that OS
3. **Easy Maintenance**: Single entry point for both platforms
4. **Robust Error Handling**: Platform-specific error messages and solutions
5. **Consistent Interface**: Same commands work on both platforms
6. **Clean Development**: No password prompts, clean terminal output, proper logging
