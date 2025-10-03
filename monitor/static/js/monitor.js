// Extracted monitor UI logic from dashboard_tabs.html
// This file must be served at /static/js/monitor.js

let currentTab = 'overview';
let autoRefreshInterval = null;
let reasoningInterval = null;
let tabDataLoaded = {
    overview: false,
    workflows: false,
    memory: false,
    goals: false,
    projects: false,
    tools: false,
    metrics: false,
    logs: false,
    reasoning: false
};

// The rest of the previously inline JS is intentionally not duplicated here for brevity.
// In practice, move all functions referenced by the template into this file (switchTab,
// loadTabData, updateTimestamp, discoverRecentSessions, refreshReasoningFull, loadLogs, etc.)
// ensuring no dependency on Go template variables.

// Minimal Logs tab implementation so the UI no longer hangs on "Loading"
async function fetchJSON(url, timeoutMs = 5000) {
    const ctrl = new AbortController();
    const id = setTimeout(() => ctrl.abort(), timeoutMs);
    try {
        const res = await fetch(url, { signal: ctrl.signal });
        if (!res.ok) throw new Error(`HTTP ${res.status}`);
        return await res.json();
    } finally {
        clearTimeout(id);
    }
}

function renderLogsCompact(entries) {
    const container = document.getElementById('logs-compact');
    if (!container) return;
    if (!Array.isArray(entries) || entries.length === 0) {
        container.innerHTML = '<div class="loading">No recent logs.</div>';
        return;
    }
    const html = entries.map(e => {
        const ts = e.timestamp || '';
        const lvl = (e.level || 'info').toLowerCase();
        const msg = (e.message || '').toString().replace(/</g, '&lt;').replace(/>/g, '&gt;');
        return `<div class="log-entry log-${lvl}"><span class="log-ts">${ts}</span> <span class="log-level">[${lvl}]</span> <span class="log-msg">${msg}</span></div>`;
    }).join('');
    container.innerHTML = html;
}

// Public functions referenced by the template/buttons
window.loadRecentLogsCompact = async function() {
    try {
        const data = await fetchJSON('/api/logs?limit=200');
        // Expecting { logs: [...] } or an array
        const entries = Array.isArray(data) ? data : (data.logs || []);
        renderLogsCompact(entries);
    } catch (err) {
        const container = document.getElementById('logs-compact');
        if (container) container.innerHTML = `<div class="error">Failed to load logs: ${err.message}</div>`;
    }
}

window.loadLogs = window.loadRecentLogsCompact;

// If user clicks Logs tab, ensure it loads once
document.addEventListener('click', (e) => {
    if (e.target && e.target.closest && e.target.closest('button') && e.target.textContent && e.target.textContent.includes('Logs')) {
        window.loadRecentLogsCompact();
    }
});

// Load compact logs shortly after DOM ready to populate initial view if Logs tab is active
document.addEventListener('DOMContentLoaded', () => {
    // Best-effort initial fetch; harmless if logs tab not visible yet
    setTimeout(() => {
        window.loadRecentLogsCompact();
    }, 300);
});


