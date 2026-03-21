# Local n8n Webhook Setup

For local testing of agents that use n8n webhooks, you need to set the `N8N_WEBHOOK_URL` environment variable.

## Importing Exported Workflows

If you have a fresh local n8n instance, you can easily load the pre-configured workflows included in this project:

1. Locate the `.json` workspace files inside the `n8n/` directory (e.g., `KnowledgeAgent.json`, `WebHookKnowledgeAgent.json`).
2. In your local n8n UI, click on the **...** menu in the top-right corner.
3. Select **Import from File** and upload the JSON files to load the integrations.
4. Remember to replace configuration nodes such as credentials with your local setup.


## Option 1: Port-forward from Kubernetes (Recommended)

If you have n8n running in Kubernetes:

```bash
# In one terminal, port-forward n8n
kubectl port-forward -n n8n svc/n8n 5678:5678

# Add to your .env file:
N8N_WEBHOOK_URL=http://localhost:5678/webhook/google-workspace
```

## Option 2: Use Cluster-Internal URL

If you're running the HDN server locally but can access the cluster:

```bash
# Add to your .env file:
N8N_WEBHOOK_URL=http://n8n.n8n.svc.cluster.local:5678/webhook/google-workspace
```

## Option 3: Local n8n Instance

If you're running n8n locally:

```bash
# Add to your .env file:
N8N_WEBHOOK_URL=http://localhost:5678/webhook/google-workspace
```

## Setting Up .env

Add the following to your `.env` file:

```bash
# n8n Webhook Configuration
N8N_WEBHOOK_URL=http://localhost:5678/webhook/google-workspace
N8N_WEBHOOK_SECRET=your-secret-here  # Optional, if your webhook requires authentication
```

**Note:** Replace `google-workspace` with your actual n8n webhook path if different.

## Verifying Setup

After adding to `.env` and restarting the server, check the logs:

```bash
grep "CONFIG-SKILLS\|SKILL-REGISTRY" /tmp/hdn_server.log | tail -5
```

You should see:
- `✅ [CONFIG-SKILLS] Loaded 1 skill(s) from configuration`
- `✅ [SKILL-REGISTRY] Registered skill: read_google_data`
- `✅ [AGENT-REGISTRY] Skill registry wired up (1 skills available)`

## Testing

Test the agent:

```bash
curl -X POST http://localhost:8081/api/v1/agents/email_monitor_agent/execute \
  -H "Content-Type: application/json" \
  -d '{"input": "Check for unread emails"}' | jq
```

The agent should now successfully call the n8n webhook instead of failing with "unknown tool".

