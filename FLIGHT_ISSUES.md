# Flight MCP - Known Issues & Fixes

## Current Status (as of April 16, 2026)

### Issue 1: K8s LLM Model Mismatch
**Symptom:** LLM Miner returns [] (empty), no easyJet flights found
**Root Cause:** K8s uses default model `qwen3:14b`, local uses `qwen2.5-coder:7b`
**Evidence:** 
- K8s logs: `LLM Miner cleaned JSON: []`
- Local test found easyJet flights with qwen2.5-coder:7b

**Fix:** Add `LLM_MODEL` env var to K8s deployment

---

### Issue 2: K8s Missing Code Fixes
**Symptom:** Wrong prices, no debug output, bad date parsing
**Root Cause:** K8s running old image (pre-abccaa4 commit)
**Fix:** Rebuild and redeploy image

---

### Issue 3: LLM Price Hallucinations
**Symptom:** LLM invents unrealistic prices like £20, £65
**Root Cause:** LLM generates prices not in the source data
**Fix Applied:** validate LLM prices against OCR mean (30% threshold)
**Status:** ✅ Fixed in code, needs K8s redeploy

---

### Issue 4: Date Parsing for "tomorrow" + "next Friday"
**Symptom:** User says "tomorrow... returning next Friday" but searches wrong dates
**Root Cause:** NLP prompt doesn't properly differentiate "this Friday" vs "next Friday"
**Fix Applied:** Improved NLP prompt with explicit date mappings
**Status:** ✅ Fixed in code, needs K8s redeploy

---

### Issue 5: EasyJet Flights Missing from Results
**Symptom:** OCR doesn't find easyJet, LLM returns [] 
**Root Cause:** 
- OCR regex fails on easyJet prices in screenshots
- LLM miner fails to extract from HTML (model issue)
**Evidence:** K8s logs show only Air France/Swiss found, no easyJet

---

### Issue 6: Output Format Confusion
**Symptom:** "Found X flight options starting at £Y" is confusing for roundtrips
**Root Cause:** Shows cheapest single leg, not total roundtrip
**Fix Applied:** New format shows cheapest TOTAL with breakdown
**Status:** ✅ Fixed in code, needs K8s redeploy

---

### Issue 7: K8s Build Script Requires Missing PEM Files
**Symptom:** `./scripts/build-and-push-images.sh` fails
**Root Cause:** Missing `secure/customer_public.pem` and `vendor_public.pem`
**Status:** 🔴 Blocking K8s deployment

---

## Action Items

### Immediate (Can Fix Now)
1. [ ] Add LLM_MODEL env var to K8s flight-mcp.yaml
2. [ ] Redeploy to get code fixes

### Blocked by PEM Files
3. [ ] Find or create PEM files for secure build
4. [ ] Rebuild Docker image with all fixes

### Longer Term Improvements
5. [ ] Add more flight results from LLM (improve prompt)
6. [ ] Add direct EasyJet/API fallback (bypass Google)
7. [ ] Improve airport code extraction

---

## Test Results Comparison

| Test | Date | Route | Expected | Got |
|------|------|-------|----------|-----|
| Local | Apr 17-24 | GVA-LGW | £100-200 | £260 (close!) |
| K8s | Apr 17-21 | GVA-LGW | £100-200 | £670 (Air France only!) |

The K8s is missing easyJet entirely because LLM returns [].