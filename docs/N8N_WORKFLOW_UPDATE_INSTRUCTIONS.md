# n8n Workflow Update Instructions

## Fixed Workflow

The fixed workflow JSON is in `n8n_workflow_fixed.json`. Here's what changed:

### Changes Made

1. **Added "Parse Request" Code Node**
   - Extracts `query`, `type`, and `limit` from webhook body
   - Clamps limit between 1-50 to prevent timeouts
   - Builds filters based on query parameter (unread/read)

2. **Updated "Get many messages" Gmail Node**
   - Now uses `maxResults` option set to the `limit` parameter
   - Uses dynamic filter from parsed request
   - Will fetch up to the specified limit of emails

3. **Added "Format as Array" Code Node**
   - Ensures response is always an array, even if only 1 email
   - Handles different data structures from Gmail node
   - Extracts email data from nested structures if needed

4. **Updated Connections**
   - Webhook → Parse Request → Get many messages → Format as Array → Respond to Webhook

## How to Update

### Option 1: Import Fixed Workflow (Recommended)

1. Open n8n UI
2. Go to Workflows
3. Click "Import from File" or "Import from URL"
4. Select `n8n_workflow_fixed.json`
5. The workflow will be imported with the same ID and credentials

### Option 2: Manual Update

1. Open your existing workflow in n8n
2. Add a new "Code" node after the Webhook node
3. Name it "Parse Request"
4. Paste this code:
```javascript
// Extract query, type, and limit from webhook body
const body = $input.first().json.body || $input.first().json;
const query = body.query || '';
const dataType = body.type || 'email';
const limit = Math.min(Math.max(parseInt(body.limit) || 10, 1), 50); // Clamp between 1-50

// Build filters based on query
const filters = {};
if (query.toLowerCase() === 'unread') {
  filters.readStatus = 'unread';
} else if (query.toLowerCase() === 'read') {
  filters.readStatus = 'read';
}

return {
  json: {
    query: query,
    type: dataType,
    limit: limit,
    filters: filters
  }
};
```

5. Update "Get many messages" Gmail node:
   - Add `maxResults` in Options: `={{ $json.limit || 10 }}`
   - Update filters to use: `={{ $json.filters.readStatus || 'unread' }}`

6. Add another "Code" node after "Get many messages"
7. Name it "Format as Array"
8. Paste this code:
```javascript
// Ensure we always return an array of emails
const items = $input.all();

// If no items, return empty array
if (!items || items.length === 0) {
  return [{ json: [] }];
}

// Extract email data from items
const emails = items.map(item => {
  // If item already has email structure, use it
  if (item.json.id && (item.json.subject || item.json.Subject)) {
    return item.json;
  }
  // If item has nested structure, extract it
  if (item.json.json) {
    return item.json.json;
  }
  // Otherwise use the item as-is
  return item.json;
});

// Return as array
return [{ json: emails }];
```

9. Update connections:
   - Webhook → Parse Request
   - Parse Request → Get many messages
   - Get many messages → Format as Array
   - Format as Array → Respond to Webhook

10. Save and activate the workflow

## Testing

After updating, test with:

```bash
# Test with limit 10
kubectl exec -n agi <pod-name> -- /tmp/test_n8n_email_webhook 10 "unread"

# Should show multiple emails if available
✅ Found 10 email(s)  # or actual count
```

## Expected Response Format

The workflow will now return:
```json
[
  {
    "id": "email-id-1",
    "subject": "Email 1 Subject",
    "from": {...},
    "labelIds": ["UNREAD", "INBOX"],
    ...
  },
  {
    "id": "email-id-2",
    "subject": "Email 2 Subject",
    "from": {...},
    "labelIds": ["UNREAD", "INBOX"],
    ...
  }
]
```

Even if there's only 1 email, it will be: `[{email1}]` not `{email1}`

