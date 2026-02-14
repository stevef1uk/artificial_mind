// --- ELEMENTS ---
const analyzeBtn = document.getElementById('analyze-btn');
const urlInput = document.getElementById('url-input');
const iframe = document.getElementById('page-preview');
const loading = document.getElementById('loading-indicator');
const placeholder = document.getElementById('placeholder-message');
const selectedSelector = document.getElementById('selected-selector');
const goalInput = document.getElementById('goal-input');
const createBtn = document.getElementById('create-agent-btn');
const statusMsg = document.getElementById('status-message');

const interactionLoading = document.getElementById('interaction-loading');
const instructionInput = document.getElementById('instruction-input');
const executeBtn = document.getElementById('execute-instruction-btn');
const toggleViewBtn = document.getElementById('toggle-view-btn');
const screenshotPreview = document.getElementById('screenshot-preview');
const stopBtn = document.getElementById('stop-execution-btn');

// --- STATE ---
let currentUrl = '';
let isInteractionView = false;
let lastSeenHtml = '';
let progressInterval = null;

// --- UTILS ---

function extractHtml(data) {
    console.log('[DEBUG] Extracting HTML from response:', data);
    if (!data) return '';

    // Check MCP result structure
    if (data.result && data.result.cleaned_html) return data.result.cleaned_html;
    if (data.result && data.result.html) return data.result.html;
    if (data.html) return data.html;

    // Check MCP content array
    if (data.content && Array.isArray(data.content)) {
        for (const item of data.content) {
            if (item.text && (item.text.includes('<html') || item.text.includes('<body'))) {
                return item.text;
            }
        }
    }
    return '';
}

function renderIframe(html) {
    if (!html) {
        console.warn('[DEBUG] renderIframe called with empty input');
        return;
    }

    console.log('[DEBUG] Rendering iframe, length:', html.length);

    // 1. Decode any escaped HTML
    let decoded = html;
    if (decoded.includes('\\u003c')) {
        decoded = decoded.replace(/\\u003c/g, '<').replace(/\\u003e/g, '>').replace(/\\"/g, '"').replace(/\\n/g, '\n');
    }

    // 1b. STRIP SCRIPTS CRITICAL: Prevents frame-busting and execution errors
    decoded = decoded.replace(/<script\b[^>]*>([\s\S]*?)<\/script>/gim, "<!-- Script Removed -->");

    // 2. Prepare our injection payload
    const injection = `
    <style>
        .agi-highlight {
            outline: 4px solid #ef4444 !important;
            outline-offset: -4px !important;
            background: rgba(239, 68, 68, 0.1) !important;
            cursor: crosshair !important;
        }
        body { cursor: crosshair !important; }
    </style>
    <script>
        (function() {
            console.log('Selection script active in iframe');
            
            function generateSelector(el) {
                if (el.id) return '#' + el.id;
                let path = [];
                while (el && el.nodeType === Node.ELEMENT_NODE) {
                    let sel = el.nodeName.toLowerCase();
                    if (el.id) {
                        sel += '#' + el.id;
                        path.unshift(sel);
                        break;
                    } else {
                        let sib = el, nth = 1;
                        while (sib = sib.previousElementSibling) {
                            if (sib.nodeName.toLowerCase() == sel) nth++;
                        }
                        if (nth != 1) sel += ":nth-of-type("+nth+")";
                    }
                    path.unshift(sel);
                    el = el.parentNode;
                    if (el === document.body) break;
                }
                return path.join(" > ");
            }

            document.addEventListener('click', function(e) {
                e.preventDefault();
                e.stopPropagation();
                
                const selector = generateSelector(e.target);
                console.log('Element selected:', selector);
                
                document.querySelectorAll('.agi-highlight').forEach(el => el.classList.remove('agi-highlight'));
                e.target.classList.add('agi-highlight');
                
                window.parent.postMessage({ type: 'elementSelected', selector: selector }, '*');
            }, true);
        })();
    </script>
    `;

    // 3. Construct final HTML. We inject at the start of the body or just at the beginning.
    let finalHtml = '';
    const bodyIdx = decoded.toLowerCase().indexOf('<body');
    if (bodyIdx !== -1) {
        const insertAt = decoded.indexOf('>', bodyIdx) + 1;
        finalHtml = decoded.substring(0, insertAt) + injection + decoded.substring(insertAt);
    } else {
        finalHtml = injection + decoded;
    }

    // Fix images/css with base tag
    if (currentUrl && !finalHtml.includes('<base')) {
        finalHtml = `<base href="${currentUrl}">` + finalHtml;
    }

    iframe.srcdoc = finalHtml;
}

function updateView() {
    console.log('[DEBUG] Updating view mode. InteractionView:', isInteractionView);
    if (isInteractionView) {
        iframe.classList.add('hidden');
        screenshotPreview.classList.remove('hidden');
        toggleViewBtn.textContent = '🔄 Switch to Interactive Selector';
    } else {
        iframe.classList.remove('hidden');
        screenshotPreview.classList.add('hidden');
        toggleViewBtn.textContent = '🔄 Switch to Static Screenshot';
        // Auto-refresh when switching back
        if (lastSeenHtml) renderIframe(lastSeenHtml);
    }
}

// --- LISTENERS ---

analyzeBtn.addEventListener('click', async () => {
    const url = urlInput.value.trim();
    if (!url) return;

    currentUrl = url;
    loading.classList.remove('hidden');
    placeholder.classList.add('hidden');
    statusMsg.classList.add('hidden');
    selectedSelector.textContent = 'None';

    try {
        const resp = await fetch('/api/tools/mcp_browse_web/invoke', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                url: url,
                get_html: true,
                instructions: 'Scan page content and return HTML.'
            })
        });
        const data = await resp.json();
        const html = extractHtml(data);
        if (html) {
            lastSeenHtml = html;
            renderIframe(html);
            createBtn.disabled = false;
        } else {
            console.error("Failed to extract HTML from:", data);
            alert('Failed to extract HTML. Received: ' + JSON.stringify(data).slice(0, 300));
        }
    } catch (e) {
        console.error('Analysis error:', e);
        alert('Analysis error: ' + e.message);
    } finally {
        loading.classList.add('hidden');
    }
});

executeBtn.addEventListener('click', async () => {
    const instr = instructionInput.value.trim();
    if (!instr || !currentUrl) return;

    executeBtn.disabled = true;
    interactionLoading.classList.remove('hidden');
    interactionLoading.innerHTML = `<span>Starting browser session...</span>`;

    const filename = `${Date.now()}.png`;
    const screenshotPath = `/home/stevef/dev/artificial_mind/monitor/static/smart_scrape/screenshots/${filename}`;

    // Poll progress/screenshot from API (same path backend writes to)
    const progressUrl = `/api/smart_scrape/screenshots/${filename}.progress`;
    const screenshotUrlBase = '/api/smart_scrape/screenshots/';
    let lastStep = 0;
    let lastScreenshot = '';
    let lastProgressTime = Date.now();
    let lastStatus = '';

    if (progressInterval) clearInterval(progressInterval);
    progressInterval = setInterval(async () => {
        try {
            const res = await fetch(progressUrl + '?t=' + Date.now());
            if (res.ok) {
                const text = await res.text();
                let prog;
                try {
                    prog = JSON.parse(text);
                } catch (_) {
                    return;
                }

                // Extract progress info
                const step = prog.step != null ? prog.step : (prog.max_steps != null ? '?' : '');
                const total = prog.total != null ? prog.total : prog.max_steps;
                const status = prog.status || (prog.html ? 'Running...' : '');
                const stepLabel = total != null ? `${step}/${total}` : step;

                // Update status text
                let statusText = `<span>[${stepLabel}] ${status}</span>`;
                if (status !== lastStatus) {
                    lastProgressTime = Date.now();
                    lastStatus = status;
                } else if (Date.now() - lastProgressTime > 5000) {
                    statusText += `<br><small style="color: #666; font-style: italic;">Processing... (this may take a few moments for complex steps)</small>`;
                    // After 15 seconds of same status, emphasize it
                    if (Date.now() - lastProgressTime > 15000) {
                        statusText += `<br><small style="color: #e67e22;">🔍 Analyzing page elements...</small>`;
                    }
                }
                interactionLoading.innerHTML = statusText;

                // Store HTML if available
                if (prog.html) lastSeenHtml = prog.html;

                // Update screenshot only if step changed or new screenshot available
                const currentScreenshot = (prog.screenshot && prog.screenshot.split('/').pop()) || filename;
                if (prog.step !== lastStep || currentScreenshot !== lastScreenshot) {
                    lastStep = prog.step;
                    lastScreenshot = currentScreenshot;
                    screenshotPreview.src = screenshotUrlBase + encodeURIComponent(currentScreenshot) + '?t=' + Date.now();
                    console.log(`Updated screenshot: step ${step}, file: ${currentScreenshot}`);
                }

                // Switch to interaction view if we have progress
                if ((prog.step != null || prog.status) && !isInteractionView) {
                    isInteractionView = true;
                    updateView();
                }
            }
        } catch (e) {
            console.error('Progress polling error:', e);
        }
    }, 1000); // Poll every 1 second for more stable updates

    try {
        const resp = await fetch('/api/tools/mcp_browse_web/invoke', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                url: currentUrl,
                instructions: instr,
                screenshot: screenshotPath,
                page_html: lastSeenHtml
            })
        });
        const data = await resp.json();
        const finalHtml = extractHtml(data);
        if (finalHtml) {
            lastSeenHtml = finalHtml;
            isInteractionView = false;
            updateView();
        }
    } catch (e) {
        console.error('Interaction error:', e);
    } finally {
        clearInterval(progressInterval);
        executeBtn.disabled = false;
        interactionLoading.classList.add('hidden');
    }
});

if (stopBtn) {
    stopBtn.addEventListener('click', () => {
        clearInterval(progressInterval);
        executeBtn.disabled = false;
        interactionLoading.classList.add('hidden');
        if (lastSeenHtml) {
            isInteractionView = false;
            updateView();
        }
    });
}

toggleViewBtn.addEventListener('click', () => {
    isInteractionView = !isInteractionView;
    updateView();
});

window.addEventListener('message', (e) => {
    if (e.data && e.data.type === 'elementSelected') {
        selectedSelector.textContent = e.data.selector;
        selectedSelector.style.color = '#ef4444';
        console.log('[DEBUG] Parent received selector:', e.data.selector);
    }
});

createBtn.addEventListener('click', async () => {
    const name = document.getElementById('agent-name').value.trim() || 'scraper_agent';
    const goal = goalInput.value.trim();
    const selector = selectedSelector.textContent;

    statusMsg.textContent = '📦 Deploying Agent...';
    statusMsg.classList.remove('hidden', 'success', 'error');

    try {
        const resp = await fetch('/api/tools/tool_file_write/invoke', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                path: `/home/stevef/dev/artificial_mind/config/scrapers/${name}.json`,
                content: JSON.stringify({ name, url: currentUrl, goal, selector_hint: selector }),
                overwrite: true
            })
        });
        const data = await resp.json().catch(() => ({}));
        if (resp.ok && !data.error) {
            statusMsg.textContent = '✅ Agent Deployed!';
            statusMsg.classList.add('success');
        } else {
            const err = data.error || data.details || resp.statusText || 'Unknown error';
            statusMsg.textContent = '❌ ' + (typeof err === 'string' ? err : JSON.stringify(err));
            statusMsg.classList.add('error');
        }
    } catch (e) {
        statusMsg.textContent = '❌ Deployment failed: ' + (e.message || String(e));
        statusMsg.classList.add('error');
    }
});
