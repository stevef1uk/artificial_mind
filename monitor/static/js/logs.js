// Configuration for log source
// Cache bust: 2025-10-03-06-55
let useKubernetesLogs = true; // Default to Kubernetes logs
let currentLogService = 'fsm-server-rpi58'; // Default service
// K8s config with persistence
let k8sNs = localStorage.getItem('k8s_ns') || 'agi';
let k8sSelectorKey = localStorage.getItem('k8s_selector_key') || 'app';

function setK8sLogsConfig(ns, selectorKey) {
    if (ns && typeof ns === 'string') {
        k8sNs = ns.trim();
        localStorage.setItem('k8s_ns', k8sNs);
    }
    if (selectorKey && typeof selectorKey === 'string') {
        k8sSelectorKey = selectorKey.trim();
        localStorage.setItem('k8s_selector_key', k8sSelectorKey);
    }
    // console.log('K8s logs config set:', { ns: k8sNs, selectorKey: k8sSelectorKey });
}

// Ensure a logs container exists in the currently active logs tab.
function getOrCreateLogsContainer() {
    let el = document.getElementById('logs-compact');
    if (el) return el;

    const logsTab = document.getElementById('logs-tab');
    if (!logsTab) return null;

    const wrapper = document.createElement('div');
    wrapper.className = 'card full-width';
    wrapper.innerHTML = `
        <div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 15px; flex-wrap: wrap;">
            <h3>🧾 Recent Logs</h3>
            <div style="display: flex; gap: 10px; align-items: center; flex-wrap: wrap;">
                <span id="log-source-indicator" class="${useKubernetesLogs ? 'k8s-indicator' : 'local-indicator'}" style="min-width:90px;">${useKubernetesLogs ? 'K8s Logs' : 'Local Logs'}</span>
                <button onclick="setLogSource('k8s')" style="padding: 6px 10px; background: #0ea5e9; color: white; border: none; border-radius: 4px; cursor: pointer; font-size: 13px;">Use K8s</button>
                <button onclick="setLogSource('local')" style="padding: 6px 10px; background: #16a34a; color: white; border: none; border-radius: 4px; cursor: pointer; font-size: 13px;">Use Local</button>
                <div style="display:flex; gap:6px; align-items:center;">
                    <label for="k8s-ns-input" style="font-size:12px; color:#374151;">Namespace</label>
                    <input id="k8s-ns-input" value="${k8sNs}" placeholder="e.g. agi" style="width:120px; padding:4px 6px; font-size:12px;" />
                    <label for="k8s-selector-key-input" style="font-size:12px; color:#374151;">Selector key</label>
                    <input id="k8s-selector-key-input" value="${k8sSelectorKey}" placeholder="e.g. app or app.kubernetes.io/name" title="Label KEY to match" style="width:190px; padding:4px 6px; font-size:12px;" />
                    <label for="k8s-service-select" style="font-size:12px; color:#374151;">Label value</label>
                    <select id="k8s-service-select" style="min-width:160px; padding:4px 6px; font-size:12px;"></select>
                    <button onclick="applyK8sSettings()" title="Apply K8s settings" style="padding: 6px 10px; background: #6b7280; color: white; border: none; border-radius: 4px; cursor: pointer; font-size: 12px;">Apply</button>
                </div>
                <button onclick="loadLogs(); loadRecentLogsCompact();" style="padding: 8px 12px; background: #28a745; color: white; border: none; border-radius: 4px; cursor: pointer; font-size: 14px;">
                    🔄 Refresh
                </button>
            </div>
        </div>
        <div id="logs-compact" class="logs-container" style="max-height: 400px; overflow-y: auto; overflow-x: hidden;"></div>
    `;

    logsTab.innerHTML = '';
    logsTab.appendChild(wrapper);
    // Populate services select from backend; fallback to known label values
    const svcSelect = wrapper.querySelector('#k8s-service-select');
    if (svcSelect) {
        const fallback = ['nats', 'neo4j', 'redis', 'weaviate', 'weaviate-health-proxy', 'hdn-server-rpi58', 'fsm-server-rpi58', 'goal-manager', 'principles-server'];
        try {
            axios.get('/api/k8s/services', { timeout: 4000 }).then(resp => {
                const items = Array.isArray(resp.data) ? resp.data : [];
                const names = items.map(s => s.name).filter(Boolean);
                const values = names.length ? names : fallback;
                svcSelect.innerHTML = values.map(v => `<option value="${v}" ${v===currentLogService?'selected':''}>${v}</option>`).join('');
            }).catch(() => {
                svcSelect.innerHTML = fallback.map(v => `<option value="${v}" ${v===currentLogService?'selected':''}>${v}</option>`).join('');
            });
        } catch (e) {
            svcSelect.innerHTML = fallback.map(v => `<option value="${v}" ${v===currentLogService?'selected':''}>${v}</option>`).join('');
        }
    }
    return document.getElementById('logs-compact');
}

async function loadLogs() {
    try {
        let response;
        if (useKubernetesLogs) {
            // Use Kubernetes logs
            const params = new URLSearchParams({ limit: '200', ns: k8sNs, selector_key: k8sSelectorKey });
            response = await axios.get(`/api/k8s/logs/${encodeURIComponent(currentLogService)}?${params.toString()}`, { timeout: 5000 });
        } else {
            // Use local logs endpoint with timeout
            response = await axios.get('/api/logs?limit=200', { timeout: 5000 });
        }
        const logs = response.data;
        
        let logsHtml = '';
        if (logs && logs.length > 0) {
            logs.forEach(log => {
                const time = new Date(log.timestamp || Date.now()).toLocaleTimeString();
                const level = (log.level || '').toUpperCase();
                const msg = (log.message || '').toString();
                logsHtml += `
                    <div class="log-item">
                        <span class="log-time">${time}</span>
                        <span class="log-level ${level}">${level}</span>
                        <span class="log-message">${msg}</span>
                    </div>`;
            });
        } else {
            logsHtml = '<div style="color: #666; text-align: center; padding: 20px;">No logs available</div>';
        }
        
        const el = getOrCreateLogsContainer();
        if (el) {
            el.innerHTML = logsHtml;
        } else {
            console.error('Element logs-compact not found');
            // Try again after a short delay
            setTimeout(() => {
                const retryEl = getOrCreateLogsContainer();
                if (retryEl) {
                    retryEl.innerHTML = logsHtml;
                }
            }, 200);
        }
    } catch (error) {
        console.error('Error loading logs:', error);
        const el = getOrCreateLogsContainer();
        if (el) {
            if (error.code === 'ECONNABORTED' || error.message.includes('timeout')) {
                el.innerHTML = '<div style="color: #f39c12; text-align: center; padding: 20px;">⏱️ Logs endpoint timed out. Try switching to K8s logs or check if the service is running.</div>';
            } else {
                el.innerHTML = `<div style="color: #e74c3c; text-align: center; padding: 20px;">❌ Error loading logs: ${error.message}</div>`;
            }
        }
    }
}

// Toggle between Kubernetes and local logs
function toggleLogSource() {
    useKubernetesLogs = !useKubernetesLogs;
    // console.log('Switched to:', useKubernetesLogs ? 'Kubernetes logs' : 'Local logs');
    
    // Update both UI indicators
    const indicators = [
        document.getElementById('log-source-indicator'),
        document.getElementById('log-source-indicator-tab')
    ];
    
    indicators.forEach(indicator => {
        if (indicator) {
            indicator.textContent = useKubernetesLogs ? 'K8s Logs' : 'Local Logs';
            indicator.className = useKubernetesLogs ? 'k8s-indicator' : 'local-indicator';
        }
    });
    
    // Reload logs with new source
    loadLogs();
    loadRecentLogsCompact();
}

// Explicitly set source from UI buttons
function setLogSource(source) {
    useKubernetesLogs = (source === 'k8s');
    const indicator = document.getElementById('log-source-indicator');
    if (indicator) {
        indicator.textContent = useKubernetesLogs ? 'K8s Logs' : 'Local Logs';
        indicator.className = useKubernetesLogs ? 'k8s-indicator' : 'local-indicator';
    }
    loadLogs();
    loadRecentLogsCompact();
}

function applyK8sSettings() {
    const nsInput = document.getElementById('k8s-ns-input');
    const keyInput = document.getElementById('k8s-selector-key-input');
    const svcSelect = document.getElementById('k8s-service-select');
    const ns = nsInput ? nsInput.value : k8sNs;
    const key = keyInput ? keyInput.value : k8sSelectorKey;
    const svc = svcSelect ? svcSelect.value : currentLogService;
    setK8sLogsConfig(ns, key);
    if (svc && typeof svc === 'string' && svc.trim()) {
        changeLogService(svc.trim());
    }
    if (useKubernetesLogs) {
        loadLogs();
        loadRecentLogsCompact();
    }
}

// Change Kubernetes service
function changeLogService(serviceName) {
    currentLogService = serviceName;
    // console.log('Changed log service to:', serviceName);
    
    if (useKubernetesLogs) {
        loadLogs();
        loadRecentLogsCompact();
    }
}

async function loadRecentLogsCompact() {
    try {
        let response;
        if (useKubernetesLogs) {
            // Use Kubernetes logs
            const params = new URLSearchParams({ limit: '200', ns: k8sNs, selector_key: k8sSelectorKey });
            response = await axios.get(`/api/k8s/logs/${encodeURIComponent(currentLogService)}?${params.toString()}`, { timeout: 5000 });
        } else {
            // Use local logs endpoint with timeout
            response = await axios.get('/api/logs?limit=200', { timeout: 5000 });
        }
        const logs = response.data;
        
        let logsHtml = '';
        if (logs && logs.length > 0) {
            logs.forEach(log => {
                const time = new Date(log.timestamp || Date.now()).toLocaleTimeString();
                const level = (log.level || '').toUpperCase();
                const msg = (log.message || '').toString();
                logsHtml += `
                    <div class="log-item" style="font-size:12px;">
                        <span class="log-time">${time}</span>
                        <span class="log-level ${level}">${level}</span>
                        <span class="log-message">${msg}</span>
                    </div>`;
            });
        } else {
            logsHtml = '<div style="color: #666; text-align: center; padding: 20px;">No logs available</div>';
        }
        
        const el = getOrCreateLogsContainer();
        if (el) {
            el.innerHTML = logsHtml;
        } else {
            console.error('Element logs-compact not found for compact view');
            // Try again after a short delay
            setTimeout(() => {
                const retryEl = getOrCreateLogsContainer();
                if (retryEl) {
                    retryEl.innerHTML = logsHtml;
                    // console.log('Compact logs HTML set on retry');
                }
            }, 200);
        }
    } catch (error) {
        console.error('Error loading compact logs:', error);
        const el = getOrCreateLogsContainer();
        if (el) {
            if (error.code === 'ECONNABORTED' || error.message.includes('timeout')) {
                el.innerHTML = '<div style="color: #f39c12; text-align: center; padding: 20px;">⏱️ Logs endpoint timed out. Try switching to K8s logs or check if the service is running.</div>';
            } else {
                el.innerHTML = `<div style="color: #e74c3c; text-align: center; padding: 20px;">❌ Error loading logs: ${error.message}</div>`;
            }
        }
    }
}

// Make functions globally available
window.toggleLogSource = toggleLogSource;
window.changeLogService = changeLogService;
window.loadLogs = loadLogs;
window.loadRecentLogsCompact = loadRecentLogsCompact;

document.addEventListener('DOMContentLoaded', function() {
    loadRecentLogsCompact();
    if (window._logsPoll) clearInterval(window._logsPoll);
    window._logsPoll = setInterval(loadRecentLogsCompact, 3000);
});

