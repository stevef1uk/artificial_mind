# n8n Email Workflow Configuration

## Current Issue

The n8n webhook is currently returning a **single email object** (map) instead of an **array of emails**, even when `limit=10` is specified. This causes only 1 email to be displayed even when multiple emails are available.

## Expected Behavior

When the webhook receives:
```json
{
  "query": "unread",
  "type": "email",
  "limit": 10
}
```

It should return an **array of email objects**:
```json
[
  {
    "id": "...",
    "subject": "Email 1",
    "from": {...},
    "labelIds": ["UNREAD", "INBOX"],
    ...
  },
  {
    "id": "...",
    "subject": "Email 2",
    "from": {...},
    "labelIds": ["UNREAD", "INBOX"],
    ...
  },
  ...
]
```

## Current Behavior

Currently, n8n returns a **single email object**:
```json
{
  "id": "...",
  "subject": "Email 1",
  "from": {...},
  "labelIds": ["UNREAD", "INBOX"],
  ...
}
```

## How to Fix in n8n

### Option 1: Use "Split In Batches" Node (Recommended)

1. After fetching emails from Gmail API, add a **"Split In Batches"** node
2. Configure it to split by the email array
3. Use **"Return All Items"** mode to return all emails as an array

### Option 2: Use "Aggregate" Node

1. After the Gmail API node, add an **"Aggregate"** node
2. Set operation to **"Append Items"** or **"Return All Items"**
3. This will collect all emails into an array

### Option 3: Use "Code" Node to Format Response

Add a **"Code"** node before the webhook response:

```javascript
// Get all items from previous node
const items = $input.all();

// If items is already an array, return it
if (Array.isArray(items)) {
  return items;
}

// If it's a single item, wrap it in an array
if (items && items.length === 1) {
  return [items[0].json];
}

// Otherwise, return all items as array
return items.map(item => item.json);
```

### Option 4: Modify Gmail API Query

Ensure the Gmail API node is configured to:
1. Use `maxResults` parameter set to the `limit` value from webhook
2. Return results as an array (not a single object)
3. Check the "Return All" option if available

## Testing

Use the test program to verify the fix:

```bash
# Build test program
GOOS=linux GOARCH=arm64 go build -o test_n8n_email_webhook_arm64 test_n8n_email_webhook.go

# Copy to pod
kubectl cp test_n8n_email_webhook_arm64 agi/<pod-name>:/tmp/test_n8n_email_webhook

# Test with limit 10
kubectl exec -n agi <pod-name> -- /tmp/test_n8n_email_webhook 10 "unread"
```

The test should show:
```
✅ Found 10 email(s)  # or however many unread emails exist
```

Instead of:
```
✅ Found 1 email(s)
```

## n8n Workflow Structure (Expected)

```
Webhook (POST)
  ↓
Parse JSON (extract query, type, limit)
  ↓
Gmail API - List Messages (with maxResults=limit)
  ↓
Gmail API - Get Message (for each message ID)
  ↓
Split In Batches / Aggregate (to create array)
  ↓
Format Response (ensure array format)
  ↓
Respond to Webhook (return array)
```

## Key Points

1. **Always return an array**, even if there's only 1 email: `[email1]` not `email1`
2. **Respect the `limit` parameter** - return up to that many emails
3. **Handle the `query` parameter** - filter by "unread", "recent", etc.
4. **Return all email fields** - subject, from, to, labelIds, date, etc.

## Code Compatibility

The Go code in `hdn/mcp_knowledge_server.go` already handles both formats:
- ✅ Array of emails: `[{email1}, {email2}, ...]`
- ✅ Single email wrapped: `{email1}` → automatically wrapped to `[{email1}]`

So once n8n returns an array, the code will work correctly.

