# HDN System Monitor

A real-time monitoring dashboard for the Hierarchical Decision Network (HDN) system built with Gin and modern web technologies.

## Features

### 🖥️ Real-time Dashboard
- **System Status Overview**: Live health monitoring of all services
- **Service Health Checks**: Individual status for HDN Server, Principles Server, and Redis
- **Active Workflow Tracking**: Real-time monitoring of running workflows
- **Execution Metrics**: Success rates, execution times, and performance statistics
- **System Logs**: Recent log entries with filtering by level
- **Auto-refresh**: Configurable automatic data refresh

### 📊 Monitoring Capabilities
- **Service Health**: HTTP health checks with response time monitoring
- **Redis Monitoring**: Connection status and basic metrics
- **Workflow Status**: Real-time workflow execution tracking
- **Execution Statistics**: Performance metrics and trends
- **Alert System**: Visual alerts for service failures
- **Log Streaming**: Real-time log viewing with level filtering

### 🎨 Modern UI
- **Responsive Design**: Works on desktop and mobile devices
- **Interactive Charts**: Real-time execution trend visualization
- **Status Indicators**: Color-coded health status indicators
- **Progress Bars**: Visual workflow progress tracking
- **Modern Styling**: Clean, professional interface with smooth animations

## Quick Start

### Prerequisites
- Go 1.21 or later
- Redis server running on localhost:6379
- HDN Server running on localhost:8081
- Principles Server running on localhost:8080

### Installation

1. **Navigate to the monitor directory:**
   ```bash
   cd monitor
   ```

2. **Install dependencies:**
   ```bash
   go mod tidy
   ```

3. **Start the monitor:**
   ```bash
   ./start_monitor.sh
   ```

4. **Open the dashboard:**
   ```
   http://localhost:8082
   ```

### Using the Main Startup Script

The monitor is automatically started with the main system:

```bash
# Start all services including monitor
cd .
./start_servers.sh

# Stop all services
./stop_servers.sh
```

## API Endpoints

The monitor provides a REST API for programmatic access:

### System Status
- `GET /api/status` - Overall system health and service status
- `GET /api/workflows` - Active workflow information
- `GET /api/metrics` - Execution metrics and statistics
- `GET /api/redis` - Redis connection and performance info
- `GET /api/docker` - Docker container information
- `GET /api/logs` - Recent system logs with filtering

### Query Parameters
- `level` - Filter logs by level (info, warning, error, critical)
- `limit` - Limit number of log entries returned

## Configuration

### Service URLs
The monitor connects to these services by default:
- **HDN Server**: `http://localhost:8081`
- **Principles Server**: `http://localhost:8080`
- **Redis**: `localhost:6379`

To modify these URLs, edit the `NewMonitorService()` function in `main.go`.

### Port Configuration
The monitor runs on port 8082 by default. To change this, modify the `r.Run(":8082")` call in `main.go`.

## Dashboard Features

### System Status Card
- Overall system health indicator
- Individual service status with response times
- Alert notifications for service failures

### Services Card
- Real-time health checks for all services
- Response time monitoring
- Error message display for failed services

### Metrics Card
- Active workflow count
- Total execution statistics
- Success rate percentage
- Average execution time

### Active Workflows Card
- Real-time workflow status
- Progress tracking with visual progress bars
- Workflow details and timestamps
- Error information for failed workflows

### Execution Trends Chart
- Real-time execution count visualization
- Historical trend tracking
- Interactive chart with Chart.js

### System Logs
- Recent log entries with timestamps
- Color-coded log levels
- Real-time log streaming
- Filtering by log level

### Reasoning Layer Monitoring
- **Reasoning Traces**: Stream of reasoning steps and evidence
- **Beliefs and Inferences**: Table of beliefs and inferred facts
- **Curiosity Goals**: List of generated exploration goals with status tracking
- **Reasoning Explanations**: Human-readable explanations of reasoning processes
- **News-Driven Goals**: Goals generated from news events and alerts

## Monitoring Data Sources

### HDN Server Integration
- Health check endpoint: `/health`
- Workflow status: `/api/v1/hierarchical/workflows`
- Execution metrics from Redis

### Principles Server Integration
- Health check endpoint: `/action`
- Service status monitoring

### Redis Integration
- Connection health monitoring
- Metrics storage and retrieval
- Workflow state tracking

## Troubleshooting

### Common Issues

1. **Monitor UI not starting:**
   ```bash
   # Check logs
   tail -f /tmp/monitor_ui.log
   
   # Check if port 8082 is available
   lsof -i :8082
   ```

2. **Services not showing as healthy:**
   - Ensure HDN Server is running on port 8081
   - Ensure Principles Server is running on port 8080
   - Check Redis connection on port 6379

3. **No workflow data:**
   - Verify HDN Server is running and accessible
   - Check Redis connection for workflow storage

### Log Files
- Monitor UI logs: `/tmp/monitor_ui.log`
- HDN Server logs: `/tmp/hdn_server.log`
- Principles Server logs: `/tmp/principles_server.log`

## Development

### Project Structure
```
monitor/
├── main.go              # Main application and API handlers
├── templates/
│   └── dashboard.html   # Main dashboard template
├── go.mod              # Go module dependencies
├── start_monitor.sh    # Startup script
├── stop_monitor.sh     # Stop script
└── README.md           # This file
```

### Adding New Metrics
1. Add new data structures in `main.go`
2. Implement data collection in service methods
3. Update the dashboard template to display new metrics
4. Add API endpoints if needed

### Customizing the UI
- Modify `templates/dashboard.html` for UI changes
- Update CSS styles in the `<style>` section
- Add new JavaScript functionality as needed

## Security Considerations

- The monitor runs on localhost by default
- No authentication is implemented (suitable for local development)
- For production use, consider adding:
  - Authentication and authorization
  - HTTPS support
  - Input validation and sanitization
  - Rate limiting

## Performance

- Lightweight Gin-based server
- Efficient Redis integration
- Minimal resource usage
- Real-time updates without polling overhead

## Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Test thoroughly
5. Submit a pull request

## License

This project is part of the HDN (Hierarchical Decision Network) system.
