# How to Extract Nationwide Savings Products & Rates

## The Challenge

The Nationwide website:
- Uses JavaScript to render content dynamically
- Embeds product data in JSON within the HTML page
- Has complex styling that makes simple regex extraction difficult

## Solutions (in order of simplicity)

### Solution 1: Use the Inspector Script (EASY)
```bash
python3 test/inspect_nationwide.py
```
This will:
1. Scrape the page via Playwright
2. Save the HTML to a file
3. Show you the actual structure

Then you can:
```bash
# View the raw data
cat /tmp/nationwide_page_*.html | python3 -m json.tool

# Search for product patterns
cat /tmp/nationwide_page_*.html | grep -i "year\|isa\|bond\|saver"
```

### Solution 2: Write Better Extraction Patterns (MEDIUM)
Once you see the HTML structure, update the patterns. Example:

```python
payload = {
    "url": "https://www.nationwide.co.uk/savings/...",
    "extractions": {
        # Match the actual HTML as it appears in the rendered page
        "products": "pattern_for_product_names",
        "rates": "pattern_for_interest_rates",
    }
}
```

### Solution 3: Use Playwright to Extract Structured Data (ADVANCED)
```typescript
// In the typescript_config:
const products = [];

// Use page.evaluate() to run JavaScript in the browser
const data = await page.evaluate(() => {
  // Find elements using CSS selectors
  const productElements = document.querySelectorAll('[data-product]');
  return Array.from(productElements).map(el => ({
    name: el.querySelector('.name')?.textContent,
    rate: el.querySelector('.rate')?.textContent
  }));
});

return { products: data };
```

## Why Basic Regex Doesn't Work

The page structure is something like:
```html
<script>
  window.data = {
    "products": [
      {"name": "1 Year Fixed Rate Cash ISA", "aer": 3.8, ...},
      {"name": "1 Year Fixed Rate Online Bond", "aer": 3.7, ...},
      ...
    ]
  }
</script>
```

Your regex needs to account for:
- JSON escaping (`\"` instead of just `"`)
- The exact nesting and field order
- Whitespace and newline variations
- Dynamic content that's rendered at runtime

## Quick Debug Checklist

✓ Is Playwright actually rendering JavaScript? (use `waitForLoadState('networkidle')`)  
✓ Are you escaping regex characters properly?  
✓ Does your pattern account for JSON structure?  
✓ Have you tested the pattern in isolation?

## Working Example

For Nationwide specifically, you might use:

```python
"extractions": {
    # Extract the full JSON containing products
    "products_json": "\"products\":\\[(.*?)\\]",
    
    # Or extract individual name/rate pairs
    "names_and_rates": "\"name\":\"([^\"]+)\".*?\"aer\":(\\d+\\.?\\d*)"
}
```

Then parse the result as JSON to get structured data.

## Need Help?

1. **See what you're working with**: Run `inspect_nationwide.py`
2. **Test your patterns**: Use Python's `re` module to test regex
3. **Check Playwright docs**: https://playwright.dev/python/ for DOM manipulation
4. **Use browser DevTools**: Inspect the actual rendered HTML with F12 in your browser
