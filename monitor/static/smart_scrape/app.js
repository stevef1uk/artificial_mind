// --- ELEMENTS ---
const urlInput = document.getElementById('url-input');
const goalInput = document.getElementById('goal-input'); // may be null after UI redesign

const extractBtn = document.getElementById('extract-btn');
const mainResults = document.getElementById('main-results');
const resultsContainer = document.getElementById('results-container');
const resultsContent = document.getElementById('results-content');
const resultsCopyBtn = document.getElementById('results-copy-btn');
const resultsClearBtn = document.getElementById('results-clear-btn');

const codegenStartBtn = document.getElementById('codegen-start-btn');
const codegenStatusBtn = document.getElementById('codegen-status-btn');
const codegenLoadBtn = document.getElementById('codegen-load-btn');
const codegenStatus = document.getElementById('codegen-status');
const previewContainer = document.getElementById('preview-container');
const pagePreview = document.getElementById('page-preview');

const viewTabs = document.getElementById('view-tabs');
const tabInteractive = document.getElementById('tab-interactive');
const tabRaw = document.getElementById('tab-raw');
const closePreviewBtn = document.getElementById('close-preview-btn');

const scriptInput = document.getElementById('script-input');
const scriptTestBtn = document.getElementById('script-test-btn');
const extractionsInput = document.getElementById('extractions-input');
const variablesInput = document.getElementById('variables-input');
const scriptPreview = document.getElementById('script-preview');
const scriptPreviewContent = document.getElementById('script-preview-content');

const statusMsg = document.getElementById('status-message');
const showApiBtn = document.getElementById('show-api-btn');
const apiCommandsPanel = document.getElementById('api-commands-panel');
const apiCmdStart = document.getElementById('api-cmd-start');
const apiCmdPoll = document.getElementById('api-cmd-poll');

// --- STATE ---
const scraperBaseUrl = 'http://localhost:8085';
let codegenSessionId = localStorage.getItem('smart_scrape_codegen_id') || null;
let codegenNovncUrl = localStorage.getItem('smart_scrape_codegen_url') || null;
let lastResult = null;
let currentView = 'interactive'; // 'raw' or 'interactive'

// --- INITIALIZATION ---
if (codegenSessionId) {
    updateCodegenUI('restored', codegenSessionId);
    checkCodegenStatus(codegenSessionId);
}

// --- UTILS ---
function showStatus(message, type = 'info') {
    statusMsg.textContent = message;
    statusMsg.className = 'status-message ' + type;
    statusMsg.classList.remove('hidden');
    setTimeout(() => {
        statusMsg.classList.add('hidden');
    }, 5000);
}

function resetPreviewArea() {
    mainResults.innerHTML = `
        <div class="placeholder">
            <div>🕵️</div>
            <p>Enter a URL and instructions to start scraping...</p>
        </div>
    `;
    mainResults.classList.remove('hidden');
    previewContainer.classList.add('hidden');
    viewTabs.classList.add('hidden');
    resultsContainer.classList.add('hidden');
}

function renderSimpleResult(title, data) {
    lastResult = data;

    // Separate extracted fields from metadata/html noise
    const SKIP_KEYS = new Set(['cleaned_html', 'html', 'full_html', 'cookies', 'page_url', 'page_title']);
    const extracted = {};
    const meta = {};
    for (const [k, v] of Object.entries(data)) {
        if (SKIP_KEYS.has(k)) meta[k] = v;
        else extracted[k] = v;
    }

    const hasExtracted = Object.keys(extracted).length > 0;
    const html = data.cleaned_html || data.html || data.full_html;

    // Build result display
    let html_out = `<div class="result-card">`;
    html_out += `<h3>✅ ${title}</h3>`;

    if (hasExtracted) {
        html_out += `<div class="extracted-fields">`;
        for (const [k, v] of Object.entries(extracted)) {
            html_out += `<div class="extracted-row"><span class="field-name">${k}</span><span class="field-value">${String(v)}</span></div>`;
        }
        html_out += `</div>`;
    } else {
        html_out += `<p style="color:var(--text-muted);font-size:0.85rem;">⚠️ No fields extracted — check your CSS selectors match the page structure.</p>`;
    }

    // Page metadata (collapsed)
    html_out += `<details style="margin-top:12px;"><summary style="cursor:pointer;color:var(--text-muted);font-size:0.8rem;">📄 Page info & raw data</summary>`;
    html_out += `<pre style="font-size:0.75rem;max-height:200px;overflow:auto;">${JSON.stringify({ page_url: data.page_url, page_title: data.page_title, ...extracted }, null, 2)}</pre>`;
    html_out += `</details>`;
    html_out += `</div>`;

    mainResults.innerHTML = html_out;
    mainResults.classList.remove('hidden');

    // Update sidebar results
    resultsContainer.classList.remove('hidden');
    resultsContent.innerHTML = `<pre>${JSON.stringify(hasExtracted ? extracted : data, null, 2)}</pre>`;

    // Show iframe preview if HTML available
    if (html) {
        viewTabs.classList.remove('hidden');
        renderIframe(html, data.page_url || urlInput.value.trim());
        switchView('interactive');
    } else {
        viewTabs.classList.add('hidden');
    }
}


function switchView(view) {
    currentView = view;
    if (view === 'raw') {
        mainResults.classList.remove('hidden');
        previewContainer.classList.add('hidden');
        tabRaw.classList.add('active');
        tabInteractive.classList.remove('active');
    } else {
        mainResults.classList.add('hidden');
        previewContainer.classList.remove('hidden');
        tabRaw.classList.remove('active');
        tabInteractive.classList.add('active');
    }
}

function showLoadingState(title) {
    previewContainer.classList.add('hidden');
    mainResults.classList.remove('hidden');
    viewTabs.classList.add('hidden');
    mainResults.innerHTML = '<div class="loading-full"><div class="spinner large"></div><h3>' + title + '</h3><p>Wait while we navigate and process the page content...</p></div>';
}

function renderIframe(html, baseUrl) {
    const injection = `
    <style>
        body { 
            overflow: auto !important; 
            height: auto !important; 
            min-height: 100vh !important; 
            background: #fff !important; 
            color: #000 !important;
            margin: 0;
            padding: 20px;
            cursor: default !important;
        }
        .agi-highlight {
            outline: 3px solid #4caf50 !important;
            outline-offset: -3px !important;
            background: rgba(76, 175, 80, 0.1) !important;
            cursor: cell !important;
        }
    </style>
    <script>
        document.addEventListener('mouseover', (e) => {
            if (e.target.classList) e.target.classList.add('agi-highlight');
        });
        document.addEventListener('mouseout', (e) => {
            if (e.target.classList) e.target.classList.remove('agi-highlight');
        });
        document.addEventListener('click', (e) => {
            e.preventDefault();
            e.stopPropagation();
            
            let el = e.target;
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
            const selector = path.join(" > ");
            
            window.parent.postMessage({ 
                type: 'elementSelected', 
                selector: selector,
                text: e.target.innerText.substring(0, 50).trim()
            }, '*');
        }, true);
    </script>
    `;

    let content = html;
    if (baseUrl) {
        content = '<base href="' + baseUrl + '">' + content;
    }

    if (content.indexOf('<html') === -1) {
        content = '<html><head>' + injection + '</head><body>' + content + '</body></html>';
    } else {
        if (content.indexOf('</head>') !== -1) {
            content = content.replace('</head>', injection + '</head>');
        } else if (content.indexOf('<body>') !== -1) {
            content = content.replace('<body>', '<body>' + injection);
        } else {
            content = injection + content;
        }
    }

    pagePreview.srcdoc = content;
}

function parseJsonField(inputElement, fieldName) {
    const raw = inputElement.value.trim();
    if (!raw) return null;
    try {
        const parsed = JSON.parse(raw);
        if (typeof parsed !== 'object' || Array.isArray(parsed)) {
            showStatus(fieldName + ' must be a JSON object', 'error');
            return null;
        }
        return parsed;
    } catch (err) {
        showStatus(fieldName + ' is invalid JSON', 'error');
        return null;
    }
}

function updateCodegenUI(status, id, url = null) {
    if (id) {
        codegenSessionId = id;
        localStorage.setItem('smart_scrape_codegen_id', id);
    }
    if (url) {
        codegenNovncUrl = url;
        localStorage.setItem('smart_scrape_codegen_url', url);
    }

    codegenStatus.textContent = 'Session: ' + (id || 'None') + ' (' + status + ')';
    codegenStatus.className = 'selector-display ' + status;

    if (status === 'running' && codegenNovncUrl) {
        codegenStatus.innerHTML = 'Running: <a href="' + codegenNovncUrl + '" target="_blank">Open Recorder</a>';
    }
}

async function pollScrapeJob(jobId) {
    while (true) {
        const resp = await fetch(scraperBaseUrl + '/scrape/job?job_id=' + jobId);
        if (!resp.ok) throw new Error('Failed to poll job status');
        const data = await resp.json();

        if (data.status === 'completed' || data.status === 'failed') {
            return data;
        }
        await new Promise(r => setTimeout(r, 2000));
    }
}

async function checkCodegenStatus(id) {
    try {
        const resp = await fetch(scraperBaseUrl + '/api/codegen/status?id=' + id);
        if (!resp.ok) {
            if (resp.status === 404) {
                updateCodegenUI('expired', id);
                localStorage.removeItem('smart_scrape_codegen_id');
                return;
            }
            throw new Error('Status check failed: ' + resp.status);
        }
        const data = await resp.json();
        updateCodegenUI(data.status, data.id, data.novnc_url);
        return data;
    } catch (err) {
        console.error('Codegen status error:', err);
    }
}

function validateVariablesAgainstScript(script, vars) {
    const missing = [];
    const extra = [];
    const used = new Set();
    const regex = /\${([A-Z0-9_]+)}/g;
    let match;
    while ((match = regex.exec(script)) !== null) {
        const varName = match[1];
        used.add(varName);
        if (!vars[varName]) {
            missing.push(varName);
        }
    }
    Object.keys(vars).forEach(v => {
        if (!used.has(v)) extra.push(v);
    });
    return { missing, extra };
}

// --- HANDLERS ---

tabInteractive.addEventListener('click', () => switchView('interactive'));
tabRaw.addEventListener('click', () => switchView('raw'));
closePreviewBtn.addEventListener('click', () => resetPreviewArea());

extractBtn.addEventListener('click', async () => {
    const url = urlInput.value.trim();
    if (!url) {
        alert('Please enter a target URL first.');
        showStatus('URL is required', 'error');
        return;
    }
    const extractions = parseJsonField(extractionsInput, 'Extractions') || {};
    if (Object.keys(extractions).length === 0) {
        showStatus('Add at least one extraction rule (CSS selector or regex)', 'error');
        return;
    }
    extractBtn.disabled = true;
    const spinner = extractBtn.querySelector('.spinner');
    if (spinner) spinner.classList.remove('hidden');
    showLoadingState('🎯 Extracting data...');
    try {
        const resp = await fetch(scraperBaseUrl + '/scrape/start', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ url, extractions, get_html: true })
        });
        if (!resp.ok) throw new Error('Server returned ' + resp.status);
        const data = await resp.json();
        const job = await pollScrapeJob(data.job_id);
        if (job.status === 'failed') {
            throw new Error(job.error || 'Extraction failed');
        }
        renderSimpleResult('Extraction Results', job.result);
        showStatus('Extraction successful!', 'success');
    } catch (err) {
        showStatus(err.message, 'error');
        mainResults.innerHTML = '<div class="error-box"><h3>❌ Error</h3><p>' + err.message + '</p></div>';
    } finally {
        extractBtn.disabled = false;
        const spinner = extractBtn.querySelector('.spinner');
        if (spinner) spinner.classList.add('hidden');
    }
});

codegenStartBtn.addEventListener('click', async () => {
    const url = urlInput.value.trim();
    if (!url) {
        showStatus('URL is required for codegen', 'error');
        return;
    }
    codegenStartBtn.disabled = true;
    showStatus('Launching codegen container...', 'info');
    try {
        const resp = await fetch(scraperBaseUrl + '/api/codegen/start', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ url })
        });
        if (!resp.ok) throw new Error('Codegen start failed: ' + resp.status);
        const data = await resp.json();
        updateCodegenUI('running', data.id, data.novnc_url);
        showStatus('Codegen started. Open the recorder link below.', 'success');
        if (data.novnc_url) {
            window.open(data.novnc_url, '_blank');
        }
    } catch (err) {
        showStatus(err.message, 'error');
    } finally {
        codegenStartBtn.disabled = false;
    }
});

codegenStatusBtn.addEventListener('click', () => {
    if (!codegenSessionId) {
        showStatus('No active codegen session', 'info');
        return;
    }
    checkCodegenStatus(codegenSessionId);
});

codegenLoadBtn.addEventListener('click', async () => {
    showStatus('Loading recorded script...', 'info');
    try {
        let script = null;

        // 1. Try loading by session ID first
        if (codegenSessionId) {
            const resp = await fetch(scraperBaseUrl + '/api/codegen/result?id=' + codegenSessionId);
            if (resp.ok) {
                script = await resp.text();
            }
        }

        // 2. Fallback: load the most recently recorded script
        if (!script) {
            const resp = await fetch(scraperBaseUrl + '/api/codegen/latest');
            if (resp.ok) {
                script = await resp.text();
                const modified = resp.headers.get('X-Script-Modified') || '';
                const filename = resp.headers.get('X-Script-File') || 'latest script';
                showStatus(`Loaded latest script: ${filename} (${modified ? new Date(modified).toLocaleTimeString() : ''})`, 'success');
            } else {
                throw new Error('No recorded scripts found. Please record a session first.');
            }
        }

        if (script && script.trim()) {
            scriptInput.value = script;
            showStatus('✅ Script loaded into editor', 'success');
        } else {
            throw new Error('Script is empty — recording may not have completed.');
        }
    } catch (err) {
        showStatus(err.message, 'error');
    }
});

scriptTestBtn.addEventListener('click', async () => {
    const url = urlInput.value.trim();
    const script = scriptInput.value.trim(); // optional
    if (!url) {
        alert('Please enter a target URL first.');
        showStatus('URL is required', 'error');
        return;
    }
    const variables = parseJsonField(variablesInput, 'Variables') || {};
    const extractions = parseJsonField(extractionsInput, 'Extractions') || {};
    if (script) {
        const validation = validateVariablesAgainstScript(script, variables);
        if (validation.missing.length > 0) {
            showStatus('Missing variables: ' + validation.missing.join(', '), 'error');
            return;
        }
    }
    scriptTestBtn.disabled = true;
    const spinner = scriptTestBtn.querySelector('.spinner');
    if (spinner) spinner.classList.remove('hidden');
    showLoadingState('🧪 Testing Custom Script...');
    try {
        const resp = await fetch(scraperBaseUrl + '/scrape/start', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                url,
                instructions: goalInput ? goalInput.value.trim() : '',

                typescript_config: script,
                variables,
                extractions,
                get_html: true
            })
        });
        if (!resp.ok) throw new Error('Script test start failed: ' + resp.status);
        const data = await resp.json();
        const job = await pollScrapeJob(data.job_id);
        if (job.status === 'failed') {
            throw new Error(job.error || 'Script execution failed');
        }
        renderSimpleResult('Script Test Results', job.result);
        showStatus('Script test passed!', 'success');
    } catch (err) {
        showStatus(err.message, 'error');
        mainResults.innerHTML = '<div class="error-box"><h3>❌ Error</h3><p>' + err.message + '</p></div>';
    } finally {
        scriptTestBtn.disabled = false;
        const spinner = scriptTestBtn.querySelector('.spinner');
        if (spinner) spinner.classList.add('hidden');
    }
});

showApiBtn.addEventListener('click', () => {
    const url = urlInput.value.trim() || 'https://your-target-url.com';
    const extractions = extractionsInput.value.trim();
    const script = scriptInput.value.trim();
    const variables = variablesInput.value.trim();

    const payload = { url };
    if (extractions) {
        try { payload.extractions = JSON.parse(extractions); } catch (e) { }
    }
    if (script) payload.typescript_config = script;
    if (variables) {
        try { payload.variables = JSON.parse(variables); } catch (e) { }
    }
    payload.get_html = false;

    // Use file-based approach to safely handle scripts with special chars/quotes
    const payloadStr = JSON.stringify(payload, null, 2);
    const serverHost = (window.location.hostname !== 'localhost' && window.location.hostname !== '')
        ? window.location.hostname
        : 'YOUR_SERVER_IP';

    const startCmd = [
        '# 1. Save payload to file (handles special characters safely):',
        "cat > /tmp/scrape_payload.json << 'ENDJSON'",
        payloadStr,
        'ENDJSON',
        '',
        '# 2. Start the scrape (note: run on the Linux server, not Mac):',
        `curl -s -X POST http://${serverHost}:8085/scrape/start \\`,
        '  -H "Content-Type: application/json" \\',
        '  -d @/tmp/scrape_payload.json'
    ].join('\n');

    const pollCmd = [
        '# 3. Poll for result — replace JOB_ID with the id from step 2:',
        'JOB_ID="paste-job-id-here"',
        `curl -s "http://${serverHost}:8085/scrape/job?job_id=$JOB_ID" | python3 -m json.tool`
    ].join('\n');

    apiCmdStart.textContent = startCmd;
    apiCmdPoll.textContent = pollCmd;
    apiCommandsPanel.classList.toggle('hidden');
});


document.getElementById('copy-cmd-start').addEventListener('click', () => {
    navigator.clipboard.writeText(apiCmdStart.textContent)
        .then(() => showStatus('Start command copied!', 'success'));
});

document.getElementById('copy-cmd-poll').addEventListener('click', () => {
    navigator.clipboard.writeText(apiCmdPoll.textContent)
        .then(() => showStatus('Poll command copied!', 'success'));
});

resultsCopyBtn.addEventListener('click', () => {
    const text = resultsContent.textContent;
    if (!text) return;
    navigator.clipboard.writeText(text).then(() => {
        showStatus('Results copied to clipboard', 'success');
    });
});

resultsClearBtn.addEventListener('click', () => {
    resultsContent.innerHTML = '';
    resultsContainer.classList.add('hidden');
    resetPreviewArea();
});

window.addEventListener('message', (e) => {
    if (e.data && e.data.type === 'elementSelected') {
        const { selector, text } = e.data;
        let extractions = {};
        try {
            const val = extractionsInput.value.trim();
            if (val) extractions = JSON.parse(val);
        } catch (err) { }
        const safeText = text.replace(/\W+/g, '_').toLowerCase().substring(0, 20);
        const propName = prompt("Name this extraction field:", safeText) || "field_" + Date.now();
        extractions[propName] = selector;
        extractionsInput.value = JSON.stringify(extractions, null, 2);
        showStatus('Added extraction rule for "' + propName + '"', 'success');
    }
});

console.log('🕸️ Smart Scrape Studio v2.0 initialized');
