# 🧠 AI Thinking Mode - Introspection & Transparency

This document describes the **Thinking Mode** feature that allows you to see inside the AI's reasoning process in real-time. This is a classic cognitive architecture feature that provides transparency, debugging capabilities, and educational value.

## 🎯 Overview

The Thinking Mode feature adds introspection capabilities to your AGI system, allowing you to:

- **See the AI's thoughts** as it processes requests
- **Stream reasoning in real-time** via WebSockets/SSE
- **Debug decision-making** by inspecting reasoning traces
- **Generate human-readable explanations** of AI behavior
- **Monitor performance** and identify bottlenecks

## 🏗️ Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                    THINKING MODE ARCHITECTURE                   │
└─────────────────────────────────────────────────────────────────┘
                                │
                                ▼
┌─────────────────────────────────────────────────────────────────┐
│                    FSM ENGINE                                  │
│                   (Enhanced)                                   │
│                                                                 │
│  • publishThinking()     • publishDecision()                   │
│  • publishAction()       • publishObservation()                │
│  • ThoughtEvent struct   • NATS integration                    │
└─────────────────────────────────────────────────────────────────┘
                                │
                                ▼
┌─────────────────────────────────────────────────────────────────┐
│                THOUGHT EXPRESSION SERVICE                       │
│                                                                 │
│  • Convert traces → Natural Language                           │
│  • Multiple styles (conversational, technical, streaming)      │
│  • Confidence scoring                                          │
│  • Summary generation                                          │
└─────────────────────────────────────────────────────────────────┘
                                │
                                ▼
┌─────────────────────────────────────────────────────────────────┐
│                THOUGHT STREAM SERVICE                           │
│                                                                 │
│  • Real-time NATS streaming                                    │
│  • Redis persistence                                           │
│  • Event handlers                                              │
│  • Cleanup & monitoring                                        │
└─────────────────────────────────────────────────────────────────┘
                                │
                                ▼
┌─────────────────────────────────────────────────────────────────┐
│                CONVERSATIONAL LAYER                            │
│                   (Enhanced)                                   │
│                                                                 │
│  • ShowThinking parameter                                      │
│  • Thought integration                                         │
│  • Response enhancement                                        │
└─────────────────────────────────────────────────────────────────┘
                                │
                                ▼
┌─────────────────────────────────────────────────────────────────┐
│                    API LAYER                                   │
│                                                                 │
│  • /api/v1/chat (with show_thinking)                          │
│  • /api/v1/chat/sessions/{id}/thoughts                        │
│  • /api/v1/chat/sessions/{id}/thoughts/stream                 │
│  • /api/v1/chat/sessions/{id}/thoughts/express                │
└─────────────────────────────────────────────────────────────────┘
                                │
                                ▼
┌─────────────────────────────────────────────────────────────────┐
│                    MONITOR UI                                  │
│                                                                 │
│  • Real-time thought display                                   │
│  • Session management                                          │
│  • Style selection                                             │
│  • Confidence visualization                                    │
└─────────────────────────────────────────────────────────────────┘
```

## 🚀 Quick Start

### 1. Basic Usage

Enable thinking mode in your conversation requests:

```json
{
  "message": "Please learn about black holes and explain them to me",
  "session_id": "session_001",
  "show_thinking": true,
  "context": {
    "domain": "astronomy",
    "level": "beginner"
  }
}
```

### 2. Expected Response

```json
{
  "response": "Black holes are regions of space where gravity is so strong that nothing can escape...",
  "session_id": "session_001",
  "timestamp": "2025-01-01T12:00:00Z",
  "confidence": 0.85,
  "thoughts": [
    {
      "type": "thinking",
      "content": "I need to learn about black holes using Wikipedia scraper",
      "state": "plan",
      "goal": "Learn about black holes and explain them",
      "confidence": 0.8,
      "timestamp": "2025-01-01T12:00:01Z"
    },
    {
      "type": "decision",
      "content": "I'll use the wiki_scraper tool to get comprehensive information",
      "state": "decide",
      "goal": "Learn about black holes and explain them",
      "confidence": 0.9,
      "action": "scrape_wikipedia",
      "timestamp": "2025-01-01T12:00:02Z"
    },
    {
      "type": "action",
      "content": "Executing Wikipedia scrape for black hole articles",
      "state": "act",
      "goal": "Learn about black holes and explain them",
      "confidence": 0.95,
      "tool_used": "wiki_scraper",
      "action": "scrape_wikipedia",
      "result": "Found 3 comprehensive articles",
      "timestamp": "2025-01-01T12:00:03Z"
    }
  ],
  "thinking_summary": "I went through 3 reasoning steps, 1 decision, and 1 action in total.",
  "metadata": {
    "thought_count": 3,
    "thinking_confidence": 0.88
  }
}
```

## 📡 API Endpoints

### Chat with Thinking Mode

```http
POST /api/v1/chat
Content-Type: application/json

{
  "message": "Your question here",
  "show_thinking": true
}
```

### Get Session Thoughts

```http
GET /api/v1/chat/sessions/{sessionId}/thoughts?limit=50
```

### Stream Thoughts (Server-Sent Events)

```http
GET /api/v1/chat/sessions/{sessionId}/thoughts/stream
```

### Express Thoughts (Convert to Natural Language)

```http
POST /api/v1/chat/sessions/{sessionId}/thoughts/express
Content-Type: application/json

{
  "style": "conversational",
  "context": {
    "domain": "astronomy"
  }
}
```

## 🎨 Thought Styles

### Conversational Style
```
"I'm analyzing the user's request about black holes"
"I decided to use Wikipedia scraper because it has comprehensive information"
"I'm executing the Wikipedia scrape now..."
```

### Technical Style
```
"Step plan: Analyzing user request (Duration: 150ms)"
"Decision: Chose wiki_scraper. Reasoning: Comprehensive coverage"
"Executed action: scrape_wikipedia"
```

### Streaming Style
```
"(thinking) Analyzing user request about black holes..."
"(deciding) I'll use Wikipedia scraper for comprehensive information"
"(acting) Executing Wikipedia scrape..."
```

## 🔧 Integration Guide

### 1. Initialize Services

```go
// In your main application
thoughtService := NewThoughtExpressionService(redis, llmClient)
streamService := NewThoughtStreamService(natsConn, redis)

// Start listening for thought events
streamService.StartListening(ctx, &ThoughtStreamConfig{
    SubjectPrefix: "agi.events.fsm.thought",
    BufferSize:    1000,
    TTL:           24 * time.Hour,
})
```

### 2. Register Event Handlers

```go
// Monitor handler
streamService.RegisterHandler("monitor", &MonitorHandler{})

// Logger handler
streamService.RegisterHandler("logger", &LoggerHandler{})
```

### 3. Use in Conversational Layer

The conversational layer automatically integrates thinking mode when `ShowThinking: true` is set in the request.

## 🎯 Use Cases

### 1. Debugging AI Decisions
When the AI makes unexpected decisions, inspect its reasoning:

```bash
curl "http://localhost:8080/api/v1/chat/sessions/session_001/thoughts?limit=20"
```

### 2. Real-time Monitoring
Watch the AI think in real-time during complex tasks:

```bash
curl "http://localhost:8080/api/v1/chat/sessions/session_001/thoughts/stream"
```

### 3. Educational Tool
Show students how AI reasoning works:

```json
{
  "message": "Explain quantum computing",
  "show_thinking": true
}
```

### 4. Performance Analysis
Analyze which reasoning steps take the most time:

```bash
curl "http://localhost:8080/api/v1/chat/sessions/session_001/reasoning"
```

### 5. Transparency Reports
Generate human-readable explanations of AI decisions:

```bash
curl -X POST "http://localhost:8080/api/v1/chat/sessions/session_001/thoughts/express" \
  -H "Content-Type: application/json" \
  -d '{"style": "conversational"}'
```

## 🖥️ Monitor UI

Access the thinking stream monitor at:
```
http://localhost:8080/monitor/templates/thinking_panel.html
```

Features:
- **Real-time thought display** with color-coded types
- **Session management** with dropdown selection
- **Style switching** (conversational, technical, streaming)
- **Confidence visualization** with progress bars
- **Metadata display** showing state, goals, tools used
- **Responsive design** for mobile and desktop

## 📊 Thought Types

| Type | Description | Color | Example |
|------|-------------|-------|---------|
| `thinking` | General reasoning process | Green | "I'm analyzing the user's request" |
| `decision` | Decision-making moments | Blue | "I decided to use Wikipedia scraper" |
| `action` | Tool execution or actions | Orange | "Executing Wikipedia scrape..." |
| `observation` | Learning from results | Purple | "Successfully extracted 12 facts" |

## 🔍 Monitoring & Debugging

### View Recent Thoughts
```bash
# Get last 50 thoughts for a session
curl "http://localhost:8080/api/v1/chat/sessions/session_001/thoughts?limit=50"

# Get thoughts in real-time
curl "http://localhost:8080/api/v1/chat/sessions/session_001/thoughts/stream"
```

### Express Thoughts
```bash
# Convert reasoning traces to natural language
curl -X POST "http://localhost:8080/api/v1/chat/sessions/session_001/thoughts/express" \
  -H "Content-Type: application/json" \
  -d '{
    "style": "conversational",
    "context": {
      "domain": "astronomy",
      "level": "beginner"
    }
  }'
```

### Monitor System Health
```bash
# Check thought stream status
curl "http://localhost:8080/api/v1/chat/health"
```

## 🚨 Troubleshooting

### Common Issues

1. **No thoughts appearing**
   - Check if `show_thinking: true` is set in the request
   - Verify NATS connection is working
   - Check Redis connectivity

2. **Stream not connecting**
   - Ensure Server-Sent Events are supported
   - Check CORS settings
   - Verify session ID exists

3. **Performance issues**
   - Reduce thought limit in requests
   - Check Redis memory usage
   - Monitor NATS message queue

### Debug Commands

```bash
# Check Redis keys
redis-cli keys "thought_events:*"

# Monitor NATS messages
nats sub "agi.events.fsm.thought"

# Check system logs
tail -f /var/log/agi/thinking.log
```

## 🔮 Future Enhancements

- **WebSocket support** for real-time bidirectional communication
- **Thought clustering** to group related thoughts
- **Sentiment analysis** of AI thoughts
- **Performance metrics** and analytics
- **Export capabilities** for thought traces
- **Custom thought templates** for different domains
- **Multi-agent thought sharing** between AI instances

## 📚 Examples

See `/examples/thinking_mode_example.go` for comprehensive usage examples including:
- Basic thinking mode usage
- Streaming thoughts in real-time
- Thought inspection and debugging
- Integration examples
- Usage scenarios

## 🤝 Contributing

To contribute to the thinking mode feature:

1. Add new thought types in `ThoughtEvent` struct
2. Implement new expression styles in `ThoughtExpressionService`
3. Add new API endpoints in `api.go`
4. Update monitor UI in `thinking_panel.html`
5. Add tests in the appropriate test files

## 📄 License

This feature is part of the AGI system and follows the same license terms.
