# 🕸️ Smart Scrape Studio - Quick Start

## Finding the Scraper in the Monitor UI

The **Smart Scrape Studio** is now prominently featured in your monitor dashboard:

### Location:
1. Open the **Monitor UI** at `http://localhost:3003` (or your configured port)
2. Look for the **🕸️ SMART SCRAPE STUDIO** button in the tab navigation
   - It's styled with a **purple gradient** to stand out
   - Located on the second row of tabs if screen is narrow

### What It Does:
- 🌐 **Scrape any website** with natural language instructions
- 🎯 **Extract specific data** without coding
- 🤖 **Deploy as Agent** - (Note: Feature prioritized for API usage)
- 📋 **Extract API Commands** - Get curl commands for any configured scrape
- 📊 **Visualize results** in real-time

---

## Quick Test: Scrape Hacker News

1. **Click** 🕸️ **SMART SCRAPE STUDIO** tab
2. **Enter URL**: `https://news.ycombinator.com`
3. **Enter Goal**: `Extract the top 5 story titles`
4. **Click** 🔍 **Analyze Page**
5. **View results** - stories extracted in ~8 seconds

---

## Use Cases

### 1. **Real-time Monitoring**
- Track news headlines hourly
- Monitor product prices
- Watch competitor changes

### 2. **Data Collection**
- Gather research data
- Extract structured information
- Build datasets

### 3. **Automated Tasks**
- Daily reports via Agent deployment
- Scheduled price checking
- Content monitoring

---

## How It Works

| Step | Action | Backend |
|------|--------|---------|
| 1 | User enters URL | Browser → Monitor |
| 2 | User describes what to extract | Smart Scrape UI |
| 3 | Click "Analyze" | Monitor calls Go Scraper |
| 4 | Go Scraper launches browser | Playwright automation |
| 5 | Smart extraction happens | GenericScraper logic |
| 6 | Results returned | JSON response |
| 7 | Displayed in UI | Real-time preview |

---

## Integration with Go Scraper Service

The Smart Scrape Studio is connected to the **Go Playwright Scraper** running on port 8087:

- **Endpoint**: `http://localhost:8087/api/scraper/generic`
- **Time**: 8-20 seconds per scrape
- **Accuracy**: 85-99% extraction success
- **Scalability**: 3-5 concurrent pages

---

## Troubleshooting

### "Loading Smart Scrape Studio..." stays visible
- **Issue**: iframe not loading
- **Fix**: Ensure static files exist at `/monitor/static/smart_scrape/`
- **Try**: Hard refresh (Ctrl+F5)

### "Cannot reach scraper service"
- **Issue**: Port 8087 not accessible
- **Fix**: Start scraper: `cd services/playwright_scraper && ./playwright_scraper`

### Extraction results are empty
- **Issue**: Website structure differs from expectations
- **Try**: Use CSS selectors instead of natural language
- **Example**: `.product-title` instead of "product names"

---

## Next Steps

1. **Try it now**: Click the 🕸️ SMART SCRAPE STUDIO button
2. **Automate**: Click 📋 **Show API Commands** to get the curl commands for your CI/CD or scripts
3. **Integrate with HDN**: Use results in your workflows

---

**Version**: 2.0
**Status**: ✅ Production Ready
**Last Updated**: 2026-02-15
