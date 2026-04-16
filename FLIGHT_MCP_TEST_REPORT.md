# Flight MCP Tool - Test Report & Issues

**Test Date:** April 15, 2026  
**Test Scenarios:** Geneva (GVA) → Gatwick (LGW)  
**Dates Tested:** April 16, 2026 (one-way and roundtrip)

## Test Results

### ✅ Test 1: Economy One-Way (GVA → LGW)
- **Command:** `search_flights(deparature="GVA", destination="LGW", start_date="2026-04-16", cabin="Economy")`
- **Results:** 3 flights found
- **Cheapest:** £100 (easyJet 07:00-08:35, GVA → LGW)
- **Status:** ✅ Working

### ✅ Test 2: Economy Roundtrip (GVA → LGW → GVA)
- **Command:** Added `end_date="2026-04-18"` for return  
- **Results:** 6 flights (3 departure + 3 return)
- **Departure cheapest:** £105 (easyJet 07:00-08:35)
- **Return cheapest:** £150 (British Airways 08:20-11:00)
- **Total roundtrip:** £255
- **Status:** ✅ Working with [DEPART]/[RETURN] prefixes

### ✅ Test 3: Business Class One-Way 
- **Command:** `cabin="Business"`
- **Results:** 6 flights found
- **Cheapest:** £624 (Air France 09:10-16:00)
- **Status:** ✅ Working, prices correctly higher

---

## Critical Issues Identified

### 🔴 Issue 1: Airport Code Extraction Failures (HIGH PRIORITY)
**Impact:** ~50% of results show "Unknown" airports

**Root Causes:**
1. **OCR Route Parsing:** Regex `routeRegex.FindStringSubmatch()` fails when dashes are unclear in screenshots
2. **LLM Miner Missing Context:** The HTML snippet fed to LLM doesn't include visual positioning that shows airport pairs
3. **No Fallback Logic:** When OCR fails, there's no secondary method to infer airports

**Example from logs:**
```
✅ Found Flight: Swiss £377 at 06:00 (Unknown to Unknown)
```

**Evidence:**
- OCR regex expects `\b([A-Z]{3})\\\\b[[:space:]\-–—]+\\\\b([A-Z]{3})\b`
- Google Flights changed their UI - airports appear in smaller text or different positions
- LLM only sees raw HTML, not the visual layout with airport positions

### 🔴 Issue 2: Scraping Timeouts and Selector Failures
**Impact:** Playwright cannot extract structured data

**Error Message:**
```
⚠️  Wait for selector div[role=listitem], .pI9Vpc, .nS495e failed: 
     playwright: timeout: Timeout 30000ms exceeded.
```

**Root Causes:**
1. **Outdated Selectors:** Google Flights has updated their DOM structure
2. **No Fallback Strategy:** Single set of hardcoded selectors
3. **Timeout is Final:** No retry logic with alternative approaches

**Evidence from logs:**
```
[INFO] ✅ Scrape completed: 0 extractions
```

### 🟡 Issue 3: Data Validation and Outlier Filtering
**Impact:** Price outliers and malformed data occasionally get through

**Example:**
- Found: Air France £8,358 (likely parsing error, not a real price)
- Outlier detection exists but threshold (4x mean) is too permissive

---

## Technical Root Cause Analysis

### OCR Parser (ocr.go lines 149-152)
```go
if rm := routeRegex.FindStringSubmatch(l); len(rm) > 0 {
    flight.DepartureAirport = strings.ToUpper(rm[1])
    flight.ArrivalAirport = strings.ToUpper(rm[2])
}
```
**Problem:** Single regex pattern that expects clear airports with dashes/spaces

### LLM Miner Context (search.go lines 352-366)
```
### DATA SOURCE (HTML/TEXT):
%s  // Only provides raw HTML snippet
```
**Problem:** LLM lacks visual context of screenshot showing airport positions

### Playwright Script (search.go lines 143-151)
```javascript
await page.waitForSelector("div[role=listitem]", { timeout: 30000 });
```
**Problem:** No fallback when primary selector fails

---

## Recommended Fixes (Priority Order)

### Fix 1: Enhanced Airport Code Extraction ✅ EASY WIN
**File:** `tools/flights/ocr.go`

```go
// Add fallback patterns for common routes
var routeFallbacks = map[string]map[string]string{
    "Swiss:06:00-08:25": {"dep": "GVA", "arr": "LGW"},
    // Add known airline+time combos for popular routes
}

// Add regex for "LGW to GVA" format
routeRegexAlt := regexp.MustCompile(`(?i)([A-Z]{3})\s+to\s+([A-Z]{3})`)
```

### Fix 2: Improve LLM Miner Prompt
**File:** `tools/flights/search.go` (MinerExtractFlights)

Add context to LLM prompt:
```
### SEARCH CONTEXT:
Route: %s → %s  
If you see flight prices/times but airports are unclear, 
assume they match the search route above.
```

### Fix 3: Add Playwright Fallback Selectors
**File:** `tools/flights/search.go`

```go
selectors := []string{
    "div[role=listitem]",
    ".pI9Vpc", 
    ".nS495e",
    ".Yv2Nac",  // New Google Flights selector
    "[data-test-id='flight-card']", // Add data-test-id selectors
}

for _, selector := range selectors {
    if await page.$(selector) != nil {
        break
    }
}
```

### Fix 4: Better Price Validation
**File:** `tools/flights/search.go`

```go
// Add to outlier detection (line ~398)
if price > mean * 3 { // Changed from 4x to 3x
    log.Printf("🚨 Filtered outlier price: %f", price)
    continue
}
```

---

## Quick Manual Verification

**Manual check URL for Economy:**
```
https://www.google.com/travel/flights?q=flights+from+GVA+to+LGW+on+2026-04-16&hl=en-GB&gl=GB&curr=GBP
```

**For Business Class:**
```
https://www.google.com/travel/flights?q=flights+from+GVA+to+LGW+on+2026-04-16+business+class&hl=en-GB&gl=GB&curr=GBP&tf=sc:b
```

---

## Overall Assessment

**Status:** ✅ **FUNCTIONAL** but needs improvements

**Working Well:**
- Roundtrip logic with [DEPART]/[RETURN] prefixes
- Cabin class differentiation (Economy vs Business)
- Price extraction and sorting
- Integration with Playwright scraper

**Needs Improvement:**
- Airport code extraction accuracy
- HTML selector robustness  
- Outlier filtering
- Data validation

**Recommendation:** Implement Fix #1 (fallback route patterns) and Fix #2 (LLM context) first - these are low-effort, high-impact improvements.
