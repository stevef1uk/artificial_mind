# Testing n8n Email Webhook

This test program calls the n8n webhook directly to see the email response format.

## Running inside the cluster

```bash
# Build the test program
go build -o test_n8n_email_webhook test_n8n_email_webhook.go

# Copy to a pod that can reach n8n (like hdn-server pod)
kubectl cp test_n8n_email_webhook agi/$(kubectl get pod -n agi -l app=hdn-server-rpi58 -o jsonpath='{.items[0].metadata.name}'):/tmp/

# Run inside the pod
kubectl exec -n agi -it $(kubectl get pod -n agi -l app=hdn-server-rpi58 -o jsonpath='{.items[0].metadata.name}') -- /tmp/test_n8n_email_webhook 10 ""

# Or with unread query
kubectl exec -n agi -it $(kubectl get pod -n agi -l app=hdn-server-rpi58 -o jsonpath='{.items[0].metadata.name}') -- /tmp/test_n8n_email_webhook 10 "unread"
```

## Running locally with port-forwarding

```bash
# In one terminal, forward n8n port
kubectl port-forward -n n8n svc/n8n 5678:5678

# In another terminal, run the test
export N8N_WEBHOOK_URL=http://localhost:5678/webhook/google-workspace
go run test_n8n_email_webhook.go 10 ""

# Or with unread query
go run test_n8n_email_webhook.go 10 "unread"
```

## Usage

```bash
go run test_n8n_email_webhook.go [limit] [query]

Examples:
  go run test_n8n_email_webhook.go 10 ""           # Get 10 emails, no query filter
  go run test_n8n_email_webhook.go 10 "unread"      # Get 10 unread emails
  go run test_n8n_email_webhook.go 50 "recent"      # Get 50 recent emails
```

## Environment Variables

- `N8N_WEBHOOK_URL`: Override the n8n webhook URL (default: cluster-internal URL)
- `N8N_WEBHOOK_SECRET`: Webhook secret for authentication (if required)
