# Smart Scrape LLM Enhancements for Modern Web Scraping

## Overview

Enhanced the `smart_scrape` tool's LLM prompt to better handle modern websites that use:
- **Custom HTML tags** (e.g., `<fin-streamer>`, `<price-display>`, web components)
- **Data attributes** for storing values (not just tag content)
- **Multiple value storage patterns** (content vs. attributes)

## Changes Made

### 1. Enhanced LLM System Prompt (Generic Patterns)

**File**: `hdn/mcp_knowledge_server.go` (lines 3962-3999)

Added new generic instruction sections:

#### MODERN WEB PATTERNS Section
```
9. Custom tags (e.g. <fin-streamer>, <price-display>): Match ANY tag name you see in HTML
   - Value in content: "<custom-tag[^>]*attribute='value'[^>]*?>\s*([0-9,.]+)"
   - Value in data-value attribute: "<custom-tag[^>]*data-value='([^']+)'"
   - Value in value attribute: "<custom-tag[^>]*value='([^']+)'"
10. Try MULTIPLE patterns for the same field if unsure where value is stored:
    - First try: content between tags
    - Then try: data-value, value, data-field, or other data-* attributes
11. For data attributes, use: "<tag[^>]*data-attribute-name='([^']+)'"
12. Match partial attribute values: "data-field='[^']*price[^']*'"
```

#### STRATEGY Section
```
- Look for custom HTML tags (anything not standard like div/span/p)
- Check both tag CONTENT and tag ATTRIBUTES for values
- Use flexible patterns that work across similar elements
- If you see data-* attributes, they often contain the actual values
```

**Key Benefits**:
- ✅ No site-specific hardcoding
- ✅ Works for Yahoo Finance, Google Finance, and any modern site
- ✅ Teaches LLM to look in multiple places for data
- ✅ Encourages flexible, robust patterns

### 2. Improved HTML Cleaning

**File**: `hdn/mcp_knowledge_server.go` (lines 4147-4160)

**Before**: Removed many data attributes indiscriminately
**After**: Selectively preserve important data attributes

```go
// Preserve: data-symbol, data-field, data-value, value
// Remove only: data-tracking, data-analytics, data-reactid, etc.
```

**Preserved Attributes** (commonly contain actual values):
- `data-symbol` - Yahoo Finance stock symbols
- `data-field` - Field identifiers
- `data-value` - Explicit value storage
- `value` - Standard value attribute
- `data-test` - Often used for important elements

**Still Removed** (tracking/analytics only):
- `data-tracking`, `data-analytics`, `data-ylk`
- `data-reactid`, `data-rapid-context`
- Event handlers (`onclick`, `onmouseover`, etc.)
- Style attributes

### 3. Debug Logging

**File**: `hdn/mcp_knowledge_server.go` (lines 3955-3960)

Added logging to show first 2000 chars of cleaned HTML:
- **Robust JSON Parsing**: Added defensive logic for robust parsing of LLM JSON responses (handling both scalar strings and arrays). The goal is to enable robust JSON Parsing to handle variations in LLM JSON output (e.g., arrays vs strings for extractions), ensuring the scraper doesn't crash if the LLM deviates from the strict schema.
```go
log.Printf("🔍 [MCP-SMART-SCRAPE] Sample of cleaned HTML for LLM (first %d chars):\n%s\n...end sample", 
    sampleLen, html[:sampleLen])
```

**Benefits**:
- Helps troubleshoot when LLM can't find expected tags
- Verify that important attributes are preserved
- See what the LLM actually receives

### Limitations
- The underlying scraper uses Go's `regexp` engine, which does **not** support lookarounds (`(?=...)`, `(?<=...)`).
- The LLM prompt has been updated to explicitly forbid these, but manual hints must also adhere to standard Go regex syntax.

## How It Works

### Example: Yahoo Finance Stock Price

**Original HTML**:
```html
<fin-streamer data-symbol="AAPL" data-field="regularMarketPrice" value="150.25">150.25</fin-streamer>
```

**What the LLM Now Knows**:
1. Look for custom tags like `<fin-streamer>`
2. Check BOTH content AND attributes
3. Try multiple patterns:
   - Content: `<fin-streamer[^>]*>([0-9,.]+)</fin-streamer>`
   - Attribute: `<fin-streamer[^>]*value='([^']+)'`
   - Field-based: `<fin-streamer[^>]*data-field='regularMarketPrice'[^>]*>([0-9,.]+)`

**What Gets Preserved in Cleaning**:
- ✅ `data-symbol="AAPL"` - preserved
- ✅ `data-field="regularMarketPrice"` - preserved
- ✅ `value="150.25"` - preserved
- ❌ `data-tracking="xyz"` - removed (if present)

## Testing

### Build
```bash
cd hdn && go build -o ../bin/hdn .
```

### Test Locally
```bash
# Test with Yahoo Finance
./test/run_smart_scrape_local.sh "https://finance.yahoo.com/quote/AAPL" "Find the current stock price"

# Check logs for:
# 1. "🔍 [MCP-SMART-SCRAPE] Sample of cleaned HTML" - verify tags are preserved
# 2. "🤖 [MCP-SMART-SCRAPE] Raw LLM planning response" - see what patterns LLM generated
```

### Test in Kubernetes
```bash
# Deploy updated HDN
kubectl rollout restart deployment/hdn -n agi

# Run test
./test/test_smart_scrape_k8s.sh agi "https://finance.yahoo.com/quote/AAPL" "Find the current stock price"
```

## Expected Improvements

### Before
- LLM only looked for values in tag content
- Custom tags might be ignored
- Data attributes were stripped during cleaning
- Yahoo Finance-specific hardcoded pattern

### After
- LLM checks BOTH content and attributes
- Custom tags explicitly recognized
- Important data attributes preserved
- Generic patterns work across many sites

## Sites That Will Benefit

1. **Yahoo Finance** - `<fin-streamer>` tags with data attributes
2. **Google Finance** - Custom web components
3. **Modern SPAs** - React/Vue apps with data attributes
4. **E-commerce** - Price displays in custom tags
5. **Any site** using web components or data attributes

## Future Enhancements

If needed, could add:
1. **Shadow DOM handling** - for web components with shadow roots
2. **JSON-LD extraction** - structured data in `<script type="application/ld+json">`
3. **Meta tag extraction** - Open Graph, Twitter Cards
4. **Microdata/Schema.org** - structured markup

## Notes

- ✅ **No site-specific code** - all patterns are generic
- ✅ **Backward compatible** - old patterns still work
- ✅ **Debuggable** - logs show what LLM sees
- ✅ **Flexible** - LLM can adapt to different value storage methods
