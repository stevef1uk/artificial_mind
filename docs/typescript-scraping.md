# TypeScript/Playwright Config Scraping

## Overview

The `scrape_url` MCP tool supports parsing TypeScript/Playwright test code and executing the equivalent actions using a headless browser. This allows you to capture browser automation workflows and replay them programmatically.

## How It Works

1. **TypeScript Parser**: Parses Playwright test code to extract operations (goto, click, fill, etc.)
2. **Action Converter**: Converts parsed operations to browser-compatible actions
3. **Headless Browser**: Executes actions using Playwright Go
4. **Result Extraction**: Returns page content and extracted data

## Supported Operations

- `page.goto(url)` - Navigate to a URL
- `page.getByRole('link', { name: 'text' }).click()` - Click links by text
- `page.getByText('text').click()` - Click elements containing text  
- `page.locator('selector').click()` - Click by CSS selector
- `page.locator('selector').fill('value')` - Fill inputs by CSS selector

## Example Usage

### 1. Create a TypeScript test file

```typescript
import { test, expect } from '@playwright/test';

test('test', async ({ page }) => {
  await page.goto('https://example.com');
  await page.getByText('Example Domain').click();
});
```

### 2. Call the MCP tool

```bash
curl -X POST http://localhost:8081/mcp \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "id": 1,
    "method": "tools/call",
    "params": {
      "name": "scrape_url",
      "arguments": {
        "url": "https://example.com",
        "typescript_config": "$(cat test.ts)"
      }
    }
  }'
```

### 3. Get results

The tool returns:
- Page title
- Page URL  
- Extracted data
- Success status

## Limitations

### Complex Forms
Forms with dynamic autocomplete or multi-step interactions may not work reliably with simple CSS selectors. For complex scenarios, consider:
- Using direct CSS selectors if you know the page structure
- Breaking down interactions into simpler steps
- Using the `browse_web` tool with natural language instructions instead

### Selector Translation
Playwright's `getByRole` with accessibility names doesn't always translate cleanly to CSS selectors. The parser attempts to use:
- Text-based selectors for links (`text='Link Name'`)
- Generic visible input selectors for forms (`input[type='text']:visible`)
- Direct CSS selectors when available

### Timing Issues
Dynamic pages may require additional wait times. The system automatically adds 2-second waits after link clicks.

## Testing

Run the test suite:

```bash
cd /home/stevef/dev/artificial_mind
./test/test_browse_web_tool.sh
```

Set a custom TypeScript file:

```bash
TS_CONFIG_FILE=/path/to/test.ts ./test/test_browse_web_tool.sh
```

## Architecture

### Parser (`hdn/playwright/parser.go`)
- Shared package for TypeScript parsing
- Extracts operations into structured format
- Reusable by both server and test tools

### Server Integration (`hdn/mcp_knowledge_server.go`)
- `scrapeWithConfig` function handles TypeScript configs
- `executePlaywrightOperations` converts to browser actions
- `browseWebWithActions` executes via headless browser binary

### Headless Browser (`tools/headless_browser/`)
- Standalone binary using Playwright Go
- Supports actions: click, fill, wait, extract, press (keyboard)
- Returns JSON with extracted data

## Future Enhancements

- [ ] Support for `page.waitForSelector()`
- [ ] Better autocomplete handling  
- [ ] Screenshot capture
- [ ] More robust selector generation
- [ ] Support for iframes and shadow DOM
- [ ] Parallel action execution

## Troubleshooting

### Browser times out
Increase timeout in the action or add wait actions between steps.

### Selector not found
Check the actual page HTML - the element might have different attributes than expected. Try using the browser developer tools to find more specific selectors.

### Process killed
The browser may be taking too long. The system has a 60-second timeout per action. Consider breaking complex workflows into smaller chunks.

