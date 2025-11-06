# Activity Log - Understanding What the Mind is Doing

## Overview

The Activity Log provides a human-readable, real-time view of what the Artificial Mind system is doing. It logs key events like state transitions, hypothesis generation, knowledge growth, and important actions in plain English.

## How It Works

The FSM engine automatically logs activities to Redis as it processes:
- **State transitions**: When the system moves between states (perceive → learn → hypothesize → plan → act)
- **Hypothesis generation**: When hypotheses are created from facts and domain knowledge
- **Hypothesis testing**: When hypotheses are tested and results are evaluated
- **Knowledge growth**: When the knowledge base grows (concepts discovered, gaps filled)
- **Important actions**: Key actions like planning, principles checks, etc.

## API Endpoint

### Get Recent Activity

```bash
GET http://localhost:8083/activity?limit=50
```

**Query Parameters:**
- `limit` (optional): Number of activities to return (default: 50, max: 200)
- `agent_id` (optional): Agent ID to query (default: uses server's agent ID)

**Response:**
```json
{
  "activities": [
    {
      "timestamp": "2024-01-15T10:30:00Z",
      "message": "Moved from 'idle' to 'perceive': Ingesting and validating new data using domain knowledge",
      "state": "perceive",
      "category": "state_change",
      "details": "Reason: new_input"
    },
    {
      "timestamp": "2024-01-15T10:30:15Z",
      "message": "🧠 Generating hypotheses from facts and domain knowledge",
      "state": "hypothesize",
      "action": "generate_hypotheses",
      "category": "action"
    },
    {
      "timestamp": "2024-01-15T10:30:30Z",
      "message": "Generated 3 hypotheses in domain 'programming'",
      "state": "hypothesize",
      "category": "hypothesis",
      "details": "Domain: programming, Count: 3"
    },
    {
      "timestamp": "2024-01-15T10:31:00Z",
      "message": "🌱 Growing knowledge base: discovering concepts and filling gaps",
      "state": "evaluate",
      "action": "grow_knowledge_base",
      "category": "action"
    }
  ],
  "count": 4,
  "agent_id": "agent_1"
}
```

## Activity Categories

- **`state_change`**: System moved to a new state
- **`action`**: Important action being executed
- **`hypothesis`**: Hypothesis generation or testing
- **`learning`**: Knowledge base growth
- **`decision`**: Decision-making processes

## Console Logging

Activities are also logged to the console with the `📋 [ACTIVITY]` prefix, so you can see them in real-time in the FSM server logs:

```
📋 [ACTIVITY] System started
📋 [ACTIVITY] Moved from 'idle' to 'perceive': Ingesting and validating new data using domain knowledge
📋 [ACTIVITY] 🧠 Generating hypotheses from facts and domain knowledge
📋 [ACTIVITY] Generated 3 hypotheses in domain 'programming'
📋 [ACTIVITY] 🌱 Growing knowledge base: discovering concepts and filling gaps
```

## Usage Examples

### View Last 20 Activities
```bash
curl http://localhost:8083/activity?limit=20
```

### View Activities for Specific Agent
```bash
curl http://localhost:8083/activity?agent_id=agent_2&limit=100
```

### Monitor in Real-Time
```bash
watch -n 2 'curl -s http://localhost:8083/activity?limit=10 | jq ".activities[] | \"\(.timestamp) \(.message)\""'
```

## Integration with Monitor UI

The activity log can be integrated into the Monitor UI dashboard to show a live feed of what the system is doing. This makes it much easier to understand the system's behavior at a glance.

## Benefits

1. **Clear Visibility**: See exactly what the system is doing in plain English
2. **Real-Time Monitoring**: Activities are logged as they happen
3. **Easy Debugging**: Understand why the system made certain decisions
4. **Learning Insights**: See when and how the knowledge base grows
5. **Hypothesis Tracking**: Follow hypothesis generation and testing cycles


