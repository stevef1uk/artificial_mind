This guide explains how to configure the Playwright Scraper service for maximum reliability. 

> 🚀 **New to scraping?** Check out the [Scraper Quick Start](SCRAPER_QUICK_START.md) for our "Easy Mode" AI generation tool.

## 🏗️ The Multi-Step Approach

The scraper is **rules-based**. It doesn't guess where data is; you must provide two things:
1.  **Interaction Logic** (`typescript_config`): How to move through the site (click, type, wait).
2.  **Extraction Logic** (`extractions`): How to find the final data in the text results.

## 🛠️ Tools for Generation
The fastest and most reliable way to create your `typescript_config` is using the **Playwright Codegen** tool. It records your browser actions and converts them into the precise code the scraper needs.

### 1. Run Codegen
On your local machine (with Node.js installed), run:
```bash
npx playwright codegen https://ecotree.green/en/calculate-car-co2
```

### 2. Record Your Actions
1.  Perform the clicks and typing just like a user would.
2.  **Crucial**: When selecting from a dropdown/geosuggest, make sure you actually click the result with your mouse.
3.  Stop recording once you reach the results page.

### 3. Clean the Output
Copy the generated code into your tool configuration, but **remove** the boilerplate:
*   ❌ REMOVE: `import { test, expect } from '@playwright/test';`
*   ❌ REMOVE: `test('test', async ({ page }) => { ... });`
*   ✅ KEEP: Only the lines starting with `await page...`

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

**EcoTree Example (with Block Anchor):**
The EcoTree results section is contained within a "Your footprint" block. In visible text flow, the values `292 kg` actually appear **above** their labels. To ensure we match the right numbers and avoid example data at the bottom of the page, we use a broad anchor:

```json
"extractions": {
  "co2_result": "Your footprint[\\s\\S]*?(?:CO|emissions)[\\s\\S]*?(\\d+(?:[.,]\\d+)?)\\s*kg",
  "distance_result": "Your footprint[\\s\\S]*?Kilometers[\\s\\S]*?(\\d+(?:[.,]\\d+)?)\\s*km"
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
*   **Literal Matching**: Match the literal text exactly as it appears on the page. For common variations like "CO2" vs "CO₂", use a non-capturing group to handle both: `(?:CO|emissions)`.
*   **Block Anchors**: If a page has multiple sections (e.g., a "Results" section and an "Examples" section), use a unique header from the results section as your starting anchor.
*   **Visual Order (InnerText)**: Remember that the scraper uses visible text. If the website design puts the number physically above the label, your regex should reflect that order (e.g., `"ParentHeader[\\s\\S]*?Value[\\s\\S]*?Label"`).
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
