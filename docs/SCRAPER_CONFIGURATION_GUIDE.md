# Scraper Configuration Guide: Best Practices & Examples

This guide explains how to configure the Playwright Scraper service for maximum reliability, using our "EcoTree Calculator" implementation as the gold standard.

## 🏗️ The Multi-Step Approach

The scraper is **rules-based**. It doesn't guess where data is; you must provide two things:
1.  **Interaction Logic** (`typescript_config`): How to move through the site (click, type, wait).
2.  **Extraction Logic** (`extractions`): How to find the final data in the text results.

---

## 🌟 Gold Standard Example: EcoTree Car Calculator

We use this as our primary example because it involves dynamic dropdowns, asynchronous calculation, and messy HTML results.

### 1. Robust Interaction Script
When writing the `typescript_config`, follow these rules used in our production tests:

```javascript
// AVOID: import { test } ... (The service handles the test wrapper)
// DO: Use direct chained commands

// 1. Navigate and wait for initial state
await page.goto('https://ecotree.green/en/calculate-car-co2');
await page.waitForTimeout(3000);

// 2. Handle Dynamic Dropdowns
// Use .first() and .nth(1) if IDs are reused or complex
await page.locator('#geosuggest__input').first().fill('Southampton');
await page.waitForTimeout(3000); 

// CRITICAL: Explicitly click the suggestion. 
// Just filling the box often isn't enough for SPAs to "register" the value.
await page.getByText('Southampton').first().click();
await page.waitForTimeout(1000);

// 3. Trigger Calculation
await page.getByRole('link', { name: ' Calculate my emissions ' }).click();

// 4. Safety Wait
// Even though the service waits for NetworkIdle, a small fixed wait 
// helps ensure the final numbers are rendered on the DOM.
await page.waitForTimeout(5000);
```

### 2. Reliable Extraction Regex
The extraction runs against the full page text. Common page layouts put labels and values in different `<div>` or `<span>` tags.

**The "Standard" Regex Pattern:**
`"label_name[\\s\\S]*?(\\d+(?:[.,]\\d+)?)\\s*units"`

*   `[\\s\\S]*?`: This is the secret sauce. It matches **any character including newlines** lazily. It bridges the gap between your label and the value, regardless of how many HTML tags are in between.
*   `(\\d+(?:[.,]\\d+)?)`: Captured group to handle integers and decimals (supporting both `.` and `,` separators).
*   `\\s*units`: Matches the units (e.g., `kg`, `km`) with optional whitespace.

**EcoTree Example:**
```json
"extractions": {
  "co2_result": "carbon emissions[\\s\\S]*?(\\d+(?:[.,]\\d+)?)\\s*kg",
  "distance_result": "travelled distance[\\s\\S]*?(\\d+(?:[.,]\\d+)?)\\s*km"
}
```

---

## 🛠️ Configuration Checklist

### ✅ Selector Robustness
*   **Use `getByRole` or `getByText`**: These are more resilient than fragile CSS classes like `.css-1ab2c3`.
*   **Use Chaining**: `page.locator('.parent').locator('.child').first()` ensures you target the exact item.
*   **Explicit Action**: If filling an input opens a menu, **always** click the menu item. Don't rely on `keyboard.press('Enter')` alone.

### ✅ Wait Strategies
The service automatically waits for `NetworkIdle` + **3 seconds** of rendering time. However, you should add `await page.waitForTimeout(ms)` inside your config for:
*   Menu animations (usually 500-1000ms).
*   Search API latencies (usually 2000-3000ms).

### ✅ Dynamic Extraction Tips
*   **Capture Groups**: Always wrap the part you want in `()`. The scraper returns the FIRST capture group.
*   **Content Cleaning**: The service automatically replaces "CO2" with "Carbon" in the search text. This prevents the "2" in "CO2" from being incorrectly captured as the result.
*   **Case Insensitivity**: All extraction regexes are applied with the `(?i)` flag automatically.

---

## 🚀 Testing Your Config
Before deploying a new calculator to the cluster, test it locally using the `curl` loop:

```bash
# 1. Start the job
JOB_ID=$(curl -s -X POST http://localhost:8080/scrape/start \
  -H 'Content-Type: application/json' \
  -d '{
    "url": "https://example.com",
    "typescript_config": "...",
    "extractions": {"my_val": "result: (\\d+)"}
  }' | jq -r '.job_id')

# 2. Poll for the result
curl "http://localhost:8080/scrape/job?job_id=$JOB_ID"
```
