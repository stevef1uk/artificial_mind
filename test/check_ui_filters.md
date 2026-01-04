# UI Tool Display Issue - Diagnostic Guide

## Current Status
- **14 tools registered** in HDN
- **2 tools have metrics**: `tool_http_get` (4 calls), `tool_ssh_executor` (143 calls)
- **12 tools have 0 metrics** (should show 0 in UI)

## Expected Behavior
The UI should show ALL 14 tools with their counts:
- Tools with metrics: Show actual counts
- Tools without metrics: Show 0 for all counts

## Possible Issues

### 1. Filter Settings in UI
Check the Tools tab in Monitor UI:
- **Origin Filter**: Should be set to "All" (not "System" or "Agent")
- **Hide Ephemeral**: If checked, it hides these tools:
  - `tool_ls`
  - `tool_exec`
  - `tool_file_read`
  - `tool_file_write`
  - `tool_docker_list`
  - `tool_codegen`

### 2. Browser Console Check
Open browser console (F12) and look for:
- `[Tools UI]` log messages
- Any JavaScript errors
- Filter settings being applied

### 3. Tools That Should Be Visible
Even with "Hide Ephemeral" checked, these should still show:
- `tool_http_get` ✅ (has metrics)
- `tool_ssh_executor` ✅ (has metrics)
- `tool_html_scraper` (0 metrics)
- `tool_json_parse` (0 metrics)
- `tool_text_search` (0 metrics)
- `tool_wiki_bootstrapper` (0 metrics)
- `tool_register` (0 metrics)
- `tool_docker_build` (0 metrics, might be filtered if execution_method=docker)

## Quick Fix
1. Open Monitor UI Tools tab
2. Set Origin Filter to "All"
3. Uncheck "Hide Ephemeral"
4. Refresh the page
5. All 14 tools should now be visible

## If Still Not Working
Check browser console for:
- `[Tools UI] Before filtering: X tools`
- `[Tools UI] After filtering: X tools`
- Any error messages









