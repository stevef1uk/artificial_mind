// Configuration for log source
// Cache bust: 2025-10-03-06-55
console.log('logs.js script loaded');
let useKubernetesLogs = true; // Default to Kubernetes logs
let currentLogService = 'fsm-server-rpi58'; // Default service
// K8s config with persistence
let k8sNs = localStorage.getItem('k8s_ns') || 'agi';
let k8sSelectorKey = localStorage.getItem('k8s_selector_key') || 'app';
// Local log file config
let localLogFile = localStorage.getItem('local_log_file') || '/tmp/hdn_server.log';

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
    console.log('loadLogs() called, useKubernetesLogs:', useKubernetesLogs, 'currentLogService:', currentLogService, 'localLogFile:', localLogFile);
    try {
        // Wait a bit to ensure DOM is ready
        await new Promise(resolve => setTimeout(resolve, 100));
        
        const el = getOrCreateLogsContainer();
        if (!el) {
            console.error('loadLogs: Could not get or create logs container');
            return;
        }
        
        // Show loading state
        el.innerHTML = '<div style="color: #666; text-align: center; padding: 20px;">Loading logs...</div>';
        
        let response;
        if (useKubernetesLogs) {
            // Use Kubernetes logs
            console.log('loadLogs: Loading K8s logs for service:', currentLogService, 'ns:', k8sNs, 'selector_key:', k8sSelectorKey);
            const params = new URLSearchParams({ limit: '200', ns: k8sNs, selector_key: k8sSelectorKey });
            const url = `/api/k8s/logs/${encodeURIComponent(currentLogService)}?${params.toString()}`;
            console.log('loadLogs: K8s URL:', url);
            response = await axios.get(url, { timeout: 5000 });
        } else {
            // Use local logs endpoint with timeout
            const fileToLoad = localLogFile || '/tmp/hdn_server.log';
            console.log('loadLogs: Loading local logs from file:', fileToLoad, 'useKubernetesLogs:', useKubernetesLogs);
            const params = new URLSearchParams({ limit: '200' });
            if (fileToLoad && fileToLoad.trim() !== '') {
                params.append('file', fileToLoad.trim());
            }
            const url = `/api/logs?${params.toString()}`;
            console.log('loadLogs: Local URL:', url);
            response = await axios.get(url, { timeout: 5000 });
        }
        const logs = response.data;
        console.log('loadLogs: Response received, logs count:', Array.isArray(logs) ? logs.length : 0, 'logs type:', typeof logs);
        
        let logsHtml = '';
        if (logs && Array.isArray(logs) && logs.length > 0) {
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
        
        // Get the element again to ensure we have the latest reference
        const finalEl = document.getElementById('logs-compact');
        if (finalEl) {
            finalEl.innerHTML = logsHtml;
            console.log('Logs rendered successfully');
        } else {
            console.error('loadLogs: logs-compact element not found after loading');
            // Try getOrCreateLogsContainer as fallback
            const fallbackEl = getOrCreateLogsContainer();
            if (fallbackEl) {
                fallbackEl.innerHTML = logsHtml;
            }
        }
    } catch (error) {
        console.error('loadLogs: Error loading logs:', error);
        console.error('loadLogs: Error details:', {
            message: error.message,
            code: error.code,
            response: error.response?.data,
            status: error.response?.status,
            stack: error.stack
        });
        const el = getOrCreateLogsContainer();
        if (el) {
            if (error.code === 'ECONNABORTED' || error.message.includes('timeout')) {
                el.innerHTML = '<div style="color: #f39c12; text-align: center; padding: 20px;">⏱️ Logs endpoint timed out. Try switching to K8s logs or check if the service is running.</div>';
            } else {
                el.innerHTML = `<div style="color: #e74c3c; text-align: center; padding: 20px;">❌ Error loading logs: ${error.message}<br><small>Check console for details</small></div>`;
            }
        }
    }
}

// Toggle between Kubernetes and local logs
function toggleLogSource() {
    useKubernetesLogs = !useKubernetesLogs;
    localStorage.setItem('use_k8s_logs', useKubernetesLogs ? 'true' : 'false');
    console.log('toggleLogSource: Switched to:', useKubernetesLogs ? 'Kubernetes logs' : 'Local logs');
    
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
    
    // Show/hide controls based on source
    const k8sControls = document.getElementById('k8s-service-controls');
    const localControls = document.getElementById('local-log-controls');
    if (k8sControls) {
        k8sControls.style.display = useKubernetesLogs ? 'flex' : 'none';
    }
    if (localControls) {
        localControls.style.display = useKubernetesLogs ? 'none' : 'flex';
        // Update the select dropdown to show current file
        const selectEl = document.getElementById('local-log-file-select');
        if (selectEl) {
            selectEl.value = localLogFile || '/tmp/hdn_server.log';
        }
    }
    console.log('toggleLogSource: k8s-controls display:', k8sControls?.style.display, 'local-controls display:', localControls?.style.display);
    
    // Reload logs with new source
    loadLogs();
    loadRecentLogsCompact();
}

// Change local log file (similar to changeLogService for K8s)
function changeLocalLogFile(filePath) {
    if (filePath && filePath.trim() !== '') {
        localLogFile = filePath.trim();
        localStorage.setItem('local_log_file', localLogFile);
        console.log('Changed local log file to:', localLogFile);
        
        // Update the select dropdown to show the selected file
        const selectEl = document.getElementById('local-log-file-select');
        if (selectEl) {
            selectEl.value = localLogFile;
        }
        
        // Reload logs with the new file path
        if (!useKubernetesLogs) {
            loadLogs();
            loadRecentLogsCompact();
        }
    }
}

// Apply local log file settings (legacy function for backward compatibility)
function applyLocalLogFile() {
    const fileInput = document.getElementById('local-log-file');
    if (fileInput) {
        changeLocalLogFile(fileInput.value);
    }
}

// Explicitly set source from UI buttons
function setLogSource(source) {
    // Force console output - use multiple methods
    console.log('=== setLogSource FUNCTION START ===');
    console.error('setLogSource ERROR TEST - called with:', source); // Use error to ensure it shows
    console.warn('setLogSource WARN TEST - source:', source);
    alert('setLogSource called with: ' + source); // Force visible output
    
    try {
        useKubernetesLogs = (source === 'k8s');
        localStorage.setItem('use_k8s_logs', useKubernetesLogs ? 'true' : 'false');
        console.log('setLogSource: switched to', source, 'useKubernetesLogs:', useKubernetesLogs);
    
    const indicator = document.getElementById('log-source-indicator');
    if (indicator) {
        indicator.textContent = useKubernetesLogs ? 'K8s Logs' : 'Local Logs';
        indicator.className = useKubernetesLogs ? 'k8s-indicator' : 'local-indicator';
    }
    
    // Show/hide controls based on source
    const k8sControls = document.getElementById('k8s-service-controls');
    const localControls = document.getElementById('local-log-controls');
    console.log('setLogSource: Found elements:', {
        k8sControls: !!k8sControls,
        localControls: !!localControls,
        useKubernetesLogs: useKubernetesLogs
    });
    
    if (k8sControls) {
        const newDisplay = useKubernetesLogs ? 'flex' : 'none';
        k8sControls.style.display = newDisplay;
        console.log('setLogSource: K8s controls display set to', newDisplay, 'actual:', k8sControls.style.display);
    } else {
        console.warn('setLogSource: k8s-service-controls element not found!');
    }
    
    if (localControls) {
        const newDisplay = useKubernetesLogs ? 'none' : 'flex';
        localControls.style.display = newDisplay;
        console.log('setLogSource: Local controls display set to', newDisplay, 'actual:', localControls.style.display);
        // Update the select dropdown to show current file
        const selectEl = document.getElementById('local-log-file-select');
        if (selectEl) {
            selectEl.value = localLogFile || '/tmp/hdn_server.log';
            console.log('setLogSource: Local log file selector set to', selectEl.value);
        } else {
            console.warn('setLogSource: local-log-file-select element not found!');
        }
    } else {
        console.warn('setLogSource: local-log-controls element not found!');
    }
    
    // Ensure K8s service selector is populated when switching to K8s
    if (useKubernetesLogs) {
        const svcSelect = document.getElementById('k8s-service-select');
        if (svcSelect) {
            const fallback = ['nats', 'neo4j', 'redis', 'weaviate', 'weaviate-health-proxy', 'hdn-server-rpi58', 'fsm-server-rpi58', 'goal-manager', 'principles-server'];
            // Always populate, even if it already has options
            console.log('setLogSource: Populating K8s service selector, current options:', svcSelect.options.length);
            try {
                axios.get('/api/k8s/services', { timeout: 4000 }).then(resp => {
                    const items = Array.isArray(resp.data) ? resp.data : [];
                    const names = items.map(s => s.name).filter(Boolean);
                    const values = names.length ? names : fallback;
                    svcSelect.innerHTML = values.map(v => `<option value="${v}" ${v===currentLogService?'selected':''}>${v}</option>`).join('');
                    console.log('setLogSource: K8s service selector populated with', values.length, 'services:', values);
                }).catch((err) => {
                    console.warn('setLogSource: Failed to load K8s services, using fallback:', err);
                    svcSelect.innerHTML = fallback.map(v => `<option value="${v}" ${v===currentLogService?'selected':''}>${v}</option>`).join('');
                    console.log('setLogSource: K8s service selector populated with fallback');
                });
            } catch (e) {
                console.warn('setLogSource: Error loading K8s services, using fallback:', e);
                svcSelect.innerHTML = fallback.map(v => `<option value="${v}" ${v===currentLogService?'selected':''}>${v}</option>`).join('');
                console.log('setLogSource: K8s service selector populated with fallback (catch)');
            }
        } else {
            console.warn('setLogSource: k8s-service-select element not found!');
        }
    }
    
    console.error('setLogSource: About to call loadLogs and loadRecentLogsCompact');
    loadLogs();
    loadRecentLogsCompact();
    console.error('setLogSource: EXIT - completed');
    } catch (error) {
        console.error('setLogSource: ERROR:', error);
        console.error('setLogSource: Error stack:', error.stack);
        alert('setLogSource ERROR: ' + error.message);
        throw error;
    }
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
            const fileToLoad = localLogFile || '/tmp/hdn_server.log';
            console.log('loadRecentLogsCompact: Loading local logs from file:', fileToLoad, 'useKubernetesLogs:', useKubernetesLogs);
            const params = new URLSearchParams({ limit: '200' });
            if (fileToLoad && fileToLoad.trim() !== '') {
                params.append('file', fileToLoad.trim());
            }
            const url = `/api/logs?${params.toString()}`;
            console.log('loadRecentLogsCompact: Requesting URL:', url);
            response = await axios.get(url, { timeout: 5000 });
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

// Make functions globally available IMMEDIATELY (before DOMContentLoaded)
// Store original function reference before assigning to window
const originalSetLogSource = setLogSource;
window.toggleLogSource = toggleLogSource;
window.changeLogService = changeLogService;
window.changeLocalLogFile = changeLocalLogFile;
window.loadLogs = loadLogs;
window.loadRecentLogsCompact = loadRecentLogsCompact;
window.applyLocalLogFile = applyLocalLogFile;
// Direct assignment - no wrapper to avoid any issues
// But add a test to verify it's our function
console.log('Assigning setLogSource to window, function body length:', setLogSource.toString().length);
window.setLogSource = function(source) {
    // Make a visible change to the page to prove this is executing
    const testDiv = document.createElement('div');
    testDiv.id = 'setLogSource-test';
    testDiv.style.cssText = 'position: fixed; top: 10px; right: 10px; background: red; color: white; padding: 20px; z-index: 99999; font-size: 20px;';
    testDiv.textContent = 'setLogSource CALLED WITH: ' + source;
    document.body.appendChild(testDiv);
    setTimeout(() => testDiv.remove(), 3000);
    
    console.error('=== WINDOW.SETLOGSOURCE CALLED ===', source);
    console.error('=== CALLING ORIGINAL FUNCTION ===');
    try {
        const result = setLogSource(source);
        console.error('=== ORIGINAL FUNCTION RETURNED ===', result);
        return result;
    } catch (e) {
        console.error('=== ERROR IN ORIGINAL FUNCTION ===', e);
        throw e;
    }
};
console.log('window.setLogSource assigned, type:', typeof window.setLogSource);
window.getOrCreateLogsContainer = getOrCreateLogsContainer;

// Debug: Verify functions are exposed
console.log('=== logs.js: All functions exposed ===');
console.log('logs.js: Functions exposed:', {
    setLogSource: typeof window.setLogSource,
    toggleLogSource: typeof window.toggleLogSource,
    changeLocalLogFile: typeof window.changeLocalLogFile,
    loadLogs: typeof window.loadLogs,
    originalSetLogSource: typeof originalSetLogSource
});
console.log('logs.js: window.setLogSource === originalSetLogSource?', window.setLogSource === originalSetLogSource);
console.log('logs.js: Script execution completed at', new Date().toISOString());

// Debug: Log that functions are available
console.log('logs.js: Functions exposed to window:', {
    toggleLogSource: typeof window.toggleLogSource,
    loadLogs: typeof window.loadLogs,
    loadRecentLogsCompact: typeof window.loadRecentLogsCompact,
    applyLocalLogFile: typeof window.applyLocalLogFile,
    getOrCreateLogsContainer: typeof window.getOrCreateLogsContainer
});

document.addEventListener('DOMContentLoaded', function() {
    // Initialize local log file from localStorage
    const savedLogFile = localStorage.getItem('local_log_file');
    if (savedLogFile) {
        localLogFile = savedLogFile;
        const fileInput = document.getElementById('local-log-file');
        if (fileInput) {
            fileInput.value = localLogFile;
        }
    }
    
    // Update indicator based on saved preference
    const savedSource = localStorage.getItem('use_k8s_logs');
    if (savedSource === 'false') {
        useKubernetesLogs = false;
    }
    
    // Show/hide controls based on current source
    const k8sControls = document.getElementById('k8s-service-controls');
    const localControls = document.getElementById('local-log-controls');
    if (k8sControls) {
        k8sControls.style.display = useKubernetesLogs ? 'flex' : 'none';
    }
    if (localControls) {
        localControls.style.display = useKubernetesLogs ? 'none' : 'flex';
        // Update the select dropdown to show current file
        const selectEl = document.getElementById('local-log-file-select');
        if (selectEl) {
            selectEl.value = localLogFile || '/tmp/hdn_server.log';
        }
    }
    console.log('DOMContentLoaded: k8s-controls display:', k8sControls?.style.display, 'local-controls display:', localControls?.style.display, 'useKubernetesLogs:', useKubernetesLogs);
    
    // Update indicator text
    const indicator = document.getElementById('log-source-indicator');
    if (indicator) {
        indicator.textContent = useKubernetesLogs ? 'K8s Logs' : 'Local Logs';
        indicator.className = useKubernetesLogs ? 'k8s-indicator' : 'local-indicator';
    }
    
    // Always populate K8s service selector so it's ready when switching to K8s mode
    const svcSelect = document.getElementById('k8s-service-select');
    if (svcSelect) {
        const fallback = ['nats', 'neo4j', 'redis', 'weaviate', 'weaviate-health-proxy', 'hdn-server-rpi58', 'fsm-server-rpi58', 'goal-manager', 'principles-server'];
        console.log('DOMContentLoaded: Populating K8s service selector...');
        console.log('DOMContentLoaded: svcSelect current options:', svcSelect.options.length);
        // Always populate, even if it already has options (to replace "Loading...")
        try {
            axios.get('/api/k8s/services', { timeout: 4000 }).then(resp => {
                const items = Array.isArray(resp.data) ? resp.data : [];
                const names = items.map(s => s.name).filter(Boolean);
                const values = names.length ? names : fallback;
                svcSelect.innerHTML = values.map(v => `<option value="${v}" ${v===currentLogService?'selected':''}>${v}</option>`).join('');
                console.log('DOMContentLoaded: K8s service selector populated with', values.length, 'services:', values);
            }).catch((err) => {
                console.warn('DOMContentLoaded: Failed to load K8s services, using fallback:', err);
                svcSelect.innerHTML = fallback.map(v => `<option value="${v}" ${v===currentLogService?'selected':''}>${v}</option>`).join('');
                console.log('DOMContentLoaded: K8s service selector populated with fallback:', fallback.length, 'services');
            });
        } catch (e) {
            console.warn('DOMContentLoaded: Error loading K8s services, using fallback:', e);
            svcSelect.innerHTML = fallback.map(v => `<option value="${v}" ${v===currentLogService?'selected':''}>${v}</option>`).join('');
            console.log('DOMContentLoaded: K8s service selector populated with fallback (catch):', fallback.length, 'services');
        }
    } else {
        console.warn('DOMContentLoaded: k8s-service-select element not found!');
    }
    loadRecentLogsCompact();
    if (window._logsPoll) clearInterval(window._logsPoll);
    window._logsPoll = setInterval(loadRecentLogsCompact, 3000);
});

