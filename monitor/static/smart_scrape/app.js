// --- ELEMENTS ---
const urlInput = document.getElementById('url-input');
const goalInput = document.getElementById('goal-input');
const extractBtn = document.getElementById('extract-btn');
const loadingIndicator = document.getElementById('loading-indicator');
const mainResults = document.getElementById('main-results');
const resultsContainer = document.getElementById('results-container');
const resultsContent = document.getElementById('results-content');
const resultsCopyBtn = document.getElementById('results-copy-btn');
const resultsClearBtn = document.getElementById('results-clear-btn');

const codegenStartBtn = document.getElementById('codegen-start-btn');
const codegenStatusBtn = document.getElementById('codegen-status-btn');
const codegenLoadBtn = document.getElementById('codegen-load-btn');
const codegenStatus = document.getElementById('codegen-status');

const scriptInput = document.getElementById('script-input');
const scriptTestBtn = document.getElementById('script-test-btn');
const extractionsInput = document.getElementById('extractions-input');
const variablesInput = document.getElementById('variables-input');
const scriptPreview = document.getElementById('script-preview');
const scriptPreviewContent = document.getElementById('script-preview-content');

const agentName = document.getElementById('agent-name');
const scheduleSelect = document.getElementById('schedule-select');
const createBtn = document.getElementById('create-agent-btn');
const statusMsg = document.getElementById('status-message');

// --- STATE ---
const scraperBaseUrl = 'http://localhost:8085';
let codegenSessionId = localStorage.getItem('smart_scrape_codegen_id') || null;
let codegenNovncUrl = localStorage.getItem('smart_scrape_codegen_url') || null;
let lastResult = null;

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

function renderSimpleResult(title, data) {
    resultsContainer.classList.remove('hidden');
    resultsContent.innerHTML = `<pre>${JSON.stringify(data, null, 2)}</pre>`;

    mainResults.innerHTML = `
        <div class="result-card">
            <h3>✅ ${title}</h3>
            <div class="result-data">
                <pre>${JSON.stringify(data, null, 2)}</pre>
            </div>
        </div>
    `;
}

function parseJsonField(inputElement, fieldName) {
    const raw = inputElement.value.trim();
    if (!raw) return null;
    try {
        const parsed = JSON.parse(raw);
        if (typeof parsed !== 'object' || Array.isArray(parsed)) {
            showStatus(`${fieldName} must be a JSON object`, 'error');
            return null;
        }
        return parsed;
    } catch (err) {
        showStatus(`${fieldName} is invalid JSON`, 'error');
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

    codegenStatus.textContent = `Session: ${id || 'None'} (${status})`;
    codegenStatus.className = 'selector-display ' + status;

    if (status === 'running' && codegenNovncUrl) {
        codegenStatus.innerHTML = `Running: <a href="${codegenNovncUrl}" target="_blank">Open Recorder</a>`;
    }
}

async function pollScrapeJob(jobId) {
    while (true) {
        const resp = await fetch(`${scraperBaseUrl}/scrape/job?job_id=${jobId}`);
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
        const resp = await fetch(`${scraperBaseUrl}/api/codegen/status?id=${id}`);
        if (!resp.ok) {
            if (resp.status === 404) {
                updateCodegenUI('expired', id);
                localStorage.removeItem('smart_scrape_codegen_id');
                return;
            }
            throw new Error(`Status check failed: ${resp.status}`);
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

    // Simple regex to find ${VARIABLE_NAME}
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

extractBtn.addEventListener('click', async () => {
    const url = urlInput.value.trim();
    const instructions = goalInput.value.trim();

    if (!url || !instructions) {
        showStatus('URL and instructions are required', 'error');
        return;
    }

    extractBtn.disabled = true;
    loadingIndicator.classList.remove('hidden');
    showStatus('Starting smart extraction...', 'info');

    try {
        const resp = await fetch(`${scraperBaseUrl}/scrape/start`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ url, instructions, get_html: true })
        });

        if (!resp.ok) throw new Error(`Server returned ${resp.status}`);
        const data = await resp.json();

        const job = await pollScrapeJob(data.job_id);
        if (job.status === 'failed') {
            throw new Error(job.error || 'Extraction failed');
        }

        lastResult = job.result;
        renderSimpleResult('Smart Scrape Results', job.result);
        showStatus('Extraction successful!', 'success');
    } catch (err) {
        showStatus(err.message, 'error');
        mainResults.innerHTML = `<div class="error-box"><h3>❌ Error</h3><p>${err.message}</p></div>`;
    } finally {
        extractBtn.disabled = false;
        loadingIndicator.classList.add('hidden');
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
        const resp = await fetch(`${scraperBaseUrl}/api/codegen/start`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ url })
        });

        if (!resp.ok) throw new Error(`Codegen start failed: ${resp.status}`);
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
    if (!codegenSessionId) {
        showStatus('No active codegen session', 'info');
        return;
    }

    showStatus('Loading recorded script...', 'info');
    try {
        const resp = await fetch(`${scraperBaseUrl}/api/codegen/result?id=${codegenSessionId}`);
        if (!resp.ok) throw new Error(`Failed to load script: ${resp.status}`);

        const script = await resp.text();
        scriptInput.value = script;
        showStatus('Script loaded into editor', 'success');
    } catch (err) {
        showStatus(err.message, 'error');
    }
});

scriptTestBtn.addEventListener('click', async () => {
    const url = urlInput.value.trim();
    const script = scriptInput.value.trim();

    if (!url || !script) {
        showStatus('URL and script are required', 'error');
        return;
    }

    const variables = parseJsonField(variablesInput, 'Variables') || {};
    const extractions = parseJsonField(extractionsInput, 'Extractions') || {};

    const validation = validateVariablesAgainstScript(script, variables);
    if (validation.missing.length > 0) {
        showStatus(`Missing variables: ${validation.missing.join(', ')}`, 'error');
        return;
    }

    scriptTestBtn.disabled = true;
    showStatus('Running test script...', 'info');

    try {
        const resp = await fetch(`${scraperBaseUrl}/scrape/start`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                url,
                typescript_config: script,
                variables,
                extractions,
                get_html: true
            })
        });

        if (!resp.ok) throw new Error(`Script test start failed: ${resp.status}`);
        const data = await resp.json();

        const job = await pollScrapeJob(data.job_id);
        if (job.status === 'failed') {
            throw new Error(job.error || 'Script execution failed');
        }

        lastResult = job.result;
        renderSimpleResult('Script Test Results', job.result);
        showStatus('Script test passed!', 'success');
    } catch (err) {
        showStatus(err.message, 'error');
    } finally {
        scriptTestBtn.disabled = false;
    }
});

createBtn.addEventListener('click', async () => {
    const name = agentName.value.trim();
    if (!name) {
        showStatus('Agent name is required', 'error');
        return;
    }

    const url = urlInput.value.trim();
    const instructions = goalInput.value.trim();
    const script = scriptInput.value.trim();
    const variables = parseJsonField(variablesInput, 'Variables');
    const extractions = parseJsonField(extractionsInput, 'Extractions');

    createBtn.disabled = true;
    showStatus('Deploying agent...', 'info');

    try {
        const resp = await fetch(`${scraperBaseUrl}/api/scraper/agent/deploy`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                name,
                url,
                instructions,
                typescript_config: script || undefined,
                variables: variables || undefined,
                extractions: extractions || undefined,
                frequency: scheduleSelect.value
            })
        });

        if (!resp.ok) {
            const errData = await resp.json().catch(() => ({}));
            throw new Error(errData.error || `Deployment failed: ${resp.status}`);
        }

        showStatus(`Agent "${name}" deployed successfully!`, 'success');
        agentName.value = '';
    } catch (err) {
        showStatus(err.message, 'error');
    } finally {
        createBtn.disabled = false;
    }
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
});

console.log('🕸️ Smart Scrape Studio v2.0 initialized');
