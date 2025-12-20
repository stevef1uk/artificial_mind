// Configuration for log source
// Cache bust: 2025-10-03-06-55
// Default to local logs (better for Mac/development), user can switch to K8s if needed
let useKubernetesLogs = localStorage.getItem('use_k8s_logs') === 'true'; // Check localStorage, default to false
let currentLogService = localStorage.getItem('current_log_service') || 'fsm-server-rpi58'; // Default service
// K8s config with persistence
let k8sNs = localStorage.getItem('k8s_ns') || 'agi';
let k8sSelectorKey = localStorage.getItem('k8s_selector_key') || 'app';
// Local log file config
let localLogFile = localStorage.getItem('local_log_file') || '/tmp/hdn_server.log';

// Log filtering
let allLogs = []; // Store all logs for filtering
let currentLogLevelFilter = 'all';
let hideVerboseLogs = false;

// Track if K8s is unavailable to stop polling
let k8sUnavailable = false;

// Patterns to hide when "Hide verbose" is enabled
const verbosePatterns = [
    /Retrieved \d+ files for workflow/,
    /Failed to get workflow record.*redis: nil/,
    /Found \d+ active workflow IDs in Redis/,
    /Payload Preview:/,
    /\[API\] Retrieved \d+ files for workflow/,
    /⚠️ \[API\] Failed to get workflow record.*redis: nil/,
];

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
            // Include log file path if specified
            const params = new URLSearchParams({ limit: '200' });
            if (localLogFile && localLogFile.trim() !== '') {
                params.append('file', localLogFile.trim());
            }
            response = await axios.get(`/api/logs?${params.toString()}`, { timeout: 5000 });
        }
        allLogs = Array.isArray(response.data) ? response.data : [];
        
        // Apply filters and render
        if (typeof applyLogFilters === 'function') {
            applyLogFilters();
        }
    } catch (error) {
        console.error('Error loading logs:', error);
        const el = getOrCreateLogsContainer();
        if (el) {
            let errorMessage = error.message;
            let errorDetails = '';
            
            // Extract more detailed error message from response if available
            if (error.response && error.response.data) {
                const data = error.response.data;
                if (data.message) {
                    errorMessage = data.message;
                }
                if (data.hint) {
                    errorDetails = `<br><small style="color: #7f8c8d;">💡 ${data.hint}</small>`;
                } else if (data.details) {
                    errorDetails = `<br><small style="color: #7f8c8d;">Details: ${data.details}</small>`;
                }
            }
            
            // Check if kubectl is unavailable (400 error with specific message)
            if (error.response && error.response.status === 400) {
                const data = error.response.data || {};
                if (data.error === 'kubectl not available' || data.message?.includes('kubectl')) {
                    k8sUnavailable = true;
                    // Stop polling for K8s logs
                    if (window._logsPoll) {
                        clearInterval(window._logsPoll);
                        window._logsPoll = null;
                    }
                    el.innerHTML = `<div style="color: #e74c3c; text-align: center; padding: 20px; border: 2px solid #e74c3c; border-radius: 8px; background: #fee;">
                        <strong>❌ Kubernetes logs unavailable</strong><br>
                        ${errorMessage}${errorDetails}<br><br>
                        <button onclick="if(typeof setLogSource==='function')setLogSource('local')" style="padding: 8px 16px; background: #28a745; color: white; border: none; border-radius: 4px; cursor: pointer; font-size: 14px; margin-top: 10px;">
                            Switch to Local Logs
                        </button>
                    </div>`;
                    return; // Don't continue trying
                }
            }
            
            if (error.code === 'ECONNABORTED' || error.message.includes('timeout')) {
                const suggestion = useKubernetesLogs 
                    ? 'Try switching to Local logs or check if kubectl is available.'
                    : 'Try switching to K8s logs or check if the service is running.';
                el.innerHTML = `<div style="color: #f39c12; text-align: center; padding: 20px;">⏱️ Logs endpoint timed out. ${suggestion}</div>`;
            } else if (error.response && error.response.status === 404) {
                el.innerHTML = `<div style="color: #e74c3c; text-align: center; padding: 20px;">❌ Service not found: ${errorMessage}${errorDetails}<br><small>Try selecting a different service or switching to Local logs.</small></div>`;
            } else if (error.response && error.response.status === 400) {
                el.innerHTML = `<div style="color: #e74c3c; text-align: center; padding: 20px;">❌ ${errorMessage}${errorDetails}<br><small>Kubernetes logs are not available. Try switching to Local logs.</small></div>`;
            } else {
                el.innerHTML = `<div style="color: #e74c3c; text-align: center; padding: 20px;">❌ Error loading logs: ${errorMessage}${errorDetails}<br><small>Try switching log source using the buttons above.</small></div>`;
            }
        } else {
            // If container still doesn't exist, try to show error in the tab itself
            const logsTab = document.getElementById('logs-tab');
            if (logsTab) {
                const errorDiv = document.createElement('div');
                errorDiv.style.cssText = 'color: #e74c3c; text-align: center; padding: 20px;';
                errorDiv.innerHTML = `❌ Error loading logs: ${error.message}`;
                logsTab.appendChild(errorDiv);
            }
        }
    }
}

// Note: toggleLogSource and setLogSource are now defined in the IIFE above to ensure they're available immediately

function applyK8sSettings() {
    const nsInput = document.getElementById('k8s-ns-input');
    const keyInput = document.getElementById('k8s-selector-key-input');
    const svcSelect = document.getElementById('k8s-service-select');
    const ns = nsInput ? nsInput.value.trim() : k8sNs;
    const key = keyInput ? keyInput.value.trim() : k8sSelectorKey;
    const svc = svcSelect ? svcSelect.value : currentLogService;
    
    setK8sLogsConfig(ns, key);
    if (svc && typeof svc === 'string' && svc.trim()) {
        changeLogService(svc.trim());
    }
    
    if (useKubernetesLogs) {
        if (typeof loadLogs === 'function') {
            loadLogs();
        }
        if (typeof loadRecentLogsCompact === 'function') {
            loadRecentLogsCompact();
        }
    }
}

// Change Kubernetes service
function changeLogService(serviceName) {
    if (!serviceName || typeof serviceName !== 'string') {
        // Try to get from select element if not provided
        const serviceSelect = document.getElementById('k8s-service-select');
        if (serviceSelect) {
            serviceName = serviceSelect.value;
        } else {
            console.warn('changeLogService: No service name provided and select element not found');
            return;
        }
    }
    
    currentLogService = serviceName.trim();
    localStorage.setItem('current_log_service', currentLogService);
    console.log('Changed log service to:', currentLogService);
    
    // Update the select element to reflect the change
    const serviceSelect = document.getElementById('k8s-service-select');
    if (serviceSelect && serviceSelect.value !== currentLogService) {
        serviceSelect.value = currentLogService;
    }
    
    if (useKubernetesLogs) {
        if (typeof loadLogs === 'function') {
            loadLogs();
        }
        if (typeof loadRecentLogsCompact === 'function') {
            loadRecentLogsCompact();
        }
    }
}

// Apply local log file settings
function applyLocalLogSettings() {
    const fileInput = document.getElementById('local-log-file');
    if (fileInput) {
        localLogFile = fileInput.value.trim() || '/tmp/hdn_server.log';
        localStorage.setItem('local_log_file', localLogFile);
        console.log('Changed local log file to:', localLogFile);
    }
    // Reload logs with the new file path
    if (typeof loadLogs === 'function') {
        loadLogs();
    }
    if (typeof loadRecentLogsCompact === 'function') {
        loadRecentLogsCompact();
    }
}

async function loadRecentLogsCompact() {
    // Skip if K8s is unavailable and we're trying to use K8s logs
    if (useKubernetesLogs && k8sUnavailable) {
        return; // Don't keep trying
    }
    
    try {
        let response;
        if (useKubernetesLogs) {
            // Use Kubernetes logs
            const params = new URLSearchParams({ limit: '200', ns: k8sNs, selector_key: k8sSelectorKey });
            response = await axios.get(`/api/k8s/logs/${encodeURIComponent(currentLogService)}?${params.toString()}`, { timeout: 5000 });
            // Reset unavailable flag on success
            k8sUnavailable = false;
        } else {
            // Use local logs endpoint with timeout
            // Include log file path if specified
            const params = new URLSearchParams({ limit: '200' });
            if (localLogFile && localLogFile.trim() !== '') {
                params.append('file', localLogFile.trim());
            }
            response = await axios.get(`/api/logs?${params.toString()}`, { timeout: 5000 });
        }
        allLogs = Array.isArray(response.data) ? response.data : [];
        
        // Apply filters and render
        if (typeof applyLogFilters === 'function') {
            applyLogFilters();
        }
    } catch (error) {
        // Don't spam errors if K8s is unavailable - only log once
        if (!(useKubernetesLogs && k8sUnavailable)) {
            console.error('Error loading compact logs:', error);
        }
        
        const el = getOrCreateLogsContainer();
        if (el) {
            let errorMessage = error.message;
            let errorDetails = '';
            
            // Extract more detailed error message from response if available
            if (error.response && error.response.data) {
                const data = error.response.data;
                if (data.message) {
                    errorMessage = data.message;
                }
                if (data.hint) {
                    errorDetails = `<br><small style="color: #7f8c8d;">💡 ${data.hint}</small>`;
                } else if (data.details) {
                    errorDetails = `<br><small style="color: #7f8c8d;">Details: ${data.details}</small>`;
                }
            }
            
            // Check if kubectl is unavailable (400 error with specific message)
            if (error.response && error.response.status === 400) {
                const data = error.response.data || {};
                if (data.error === 'kubectl not available' || data.message?.includes('kubectl')) {
                    k8sUnavailable = true;
                    // Stop polling for K8s logs
                    if (window._logsPoll) {
                        clearInterval(window._logsPoll);
                        window._logsPoll = null;
                    }
                    // Only show error if we haven't shown it already (avoid duplicate messages)
                    if (!el.innerHTML.includes('Kubernetes logs unavailable')) {
                        el.innerHTML = `<div style="color: #e74c3c; text-align: center; padding: 20px; border: 2px solid #e74c3c; border-radius: 8px; background: #fee;">
                            <strong>❌ Kubernetes logs unavailable</strong><br>
                            ${errorMessage}${errorDetails}<br><br>
                            <button onclick="if(typeof setLogSource==='function')setLogSource('local')" style="padding: 8px 16px; background: #28a745; color: white; border: none; border-radius: 4px; cursor: pointer; font-size: 14px; margin-top: 10px;">
                                Switch to Local Logs
                            </button>
                        </div>`;
                    }
                    return; // Don't continue trying
                }
            }
            
            if (error.code === 'ECONNABORTED' || error.message.includes('timeout')) {
                const suggestion = useKubernetesLogs 
                    ? 'Try switching to Local logs or check if kubectl is available.'
                    : 'Try switching to K8s logs or check if the service is running.';
                el.innerHTML = `<div style="color: #f39c12; text-align: center; padding: 20px;">⏱️ Logs endpoint timed out. ${suggestion}</div>`;
            } else if (error.response && error.response.status === 404) {
                el.innerHTML = `<div style="color: #e74c3c; text-align: center; padding: 20px;">❌ Service not found: ${errorMessage}${errorDetails}<br><small>Try selecting a different service or switching to Local logs.</small></div>`;
            } else if (error.response && error.response.status === 400) {
                el.innerHTML = `<div style="color: #e74c3c; text-align: center; padding: 20px;">❌ ${errorMessage}${errorDetails}<br><small>Kubernetes logs are not available. Try switching to Local logs.</small></div>`;
            } else {
                el.innerHTML = `<div style="color: #e74c3c; text-align: center; padding: 20px;">❌ Error loading logs: ${errorMessage}${errorDetails}<br><small>Try switching log source using the buttons above.</small></div>`;
            }
        } else {
            // If container still doesn't exist, try to show error in the tab itself
            const logsTab = document.getElementById('logs-tab');
            if (logsTab) {
                const errorDiv = document.createElement('div');
                errorDiv.style.cssText = 'color: #e74c3c; text-align: center; padding: 20px;';
                errorDiv.innerHTML = `❌ Error loading logs: ${error.message}`;
                logsTab.appendChild(errorDiv);
            }
        }
    }
}

// Apply log filters and render filtered logs
function applyLogFilters() {
    const levelFilter = document.getElementById('log-level-filter');
    const hideVerbose = document.getElementById('hide-verbose-logs');
    
    if (levelFilter) {
        currentLogLevelFilter = levelFilter.value;
        localStorage.setItem('log_level_filter', currentLogLevelFilter);
    }
    if (hideVerbose) {
        hideVerboseLogs = hideVerbose.checked;
        localStorage.setItem('hide_verbose_logs', hideVerboseLogs ? 'true' : 'false');
    }
    
    // Filter logs
    let filteredLogs = allLogs.filter(log => {
        const level = (log.level || 'info').toLowerCase();
        const msg = (log.message || '').toString();
        
        // Apply level filter
        if (currentLogLevelFilter !== 'all') {
            const levelOrder = { 'error': 0, 'warning': 1, 'info': 2, 'debug': 3 };
            const filterLevel = levelOrder[currentLogLevelFilter] ?? 3;
            const logLevel = levelOrder[level] ?? 2;
            if (logLevel > filterLevel) {
                return false;
            }
        }
        
        // Apply verbose filter
        if (hideVerboseLogs) {
            for (const pattern of verbosePatterns) {
                if (pattern.test(msg)) {
                    return false;
                }
            }
        }
        
        return true;
    });
    
    // Render filtered logs
    renderFilteredLogs(filteredLogs);
}

// Render filtered logs to the container
function renderFilteredLogs(logs) {
    let logsHtml = '';
    if (logs && logs.length > 0) {
        logs.forEach(log => {
            const time = new Date(log.timestamp || Date.now()).toLocaleTimeString();
            const level = (log.level || 'info').toUpperCase();
            let msg = (log.message || '').toString();
            
            // Truncate very long messages
            if (msg.length > 500) {
                msg = msg.substring(0, 500) + '...';
            }
            
            // Escape HTML but preserve line breaks
            msg = msg.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
            
            const levelClass = level.toLowerCase();
            logsHtml += `
                <div class="log-item log-${levelClass}" style="padding: 4px 8px; margin: 2px 0; border-left: 3px solid ${
                    levelClass === 'error' ? '#e74c3c' : 
                    levelClass === 'warning' ? '#f39c12' : 
                    levelClass === 'debug' ? '#95a5a6' : '#3498db'
                }; font-size: 12px; line-height: 1.4;">
                    <span class="log-time" style="color: #7f8c8d; margin-right: 8px; font-family: monospace;">${time}</span>
                    <span class="log-level" style="color: ${
                        levelClass === 'error' ? '#e74c3c' : 
                        levelClass === 'warning' ? '#f39c12' : 
                        levelClass === 'debug' ? '#95a5a6' : '#3498db'
                    }; font-weight: bold; margin-right: 8px; min-width: 60px; display: inline-block;">${level}</span>
                    <span class="log-message" style="color: #2c3e50;">${msg}</span>
                </div>`;
        });
    } else {
        logsHtml = '<div style="color: #666; text-align: center; padding: 20px;">No logs match the current filters</div>';
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
}

// Update UI to show/hide appropriate controls based on log source
function updateLogSourceUI() {
    const k8sControls = document.getElementById('k8s-controls');
    const localControls = document.getElementById('local-controls');
    
    if (k8sControls) {
        k8sControls.style.display = useKubernetesLogs ? 'flex' : 'none';
        // Ensure flex layout is applied
        if (useKubernetesLogs) {
            k8sControls.style.gap = '6px';
            k8sControls.style.alignItems = 'center';
            k8sControls.style.flexWrap = 'wrap';
        }
    }
    if (localControls) {
        localControls.style.display = useKubernetesLogs ? 'none' : 'flex';
        // Ensure flex layout is applied
        if (!useKubernetesLogs) {
            localControls.style.gap = '6px';
            localControls.style.alignItems = 'center';
            localControls.style.flexWrap = 'wrap';
        }
    }
}

// FIRST: Expose all functions to window immediately (before toggleLogSource uses them)
window.changeLogService = changeLogService;
window.loadLogs = loadLogs;
window.loadRecentLogsCompact = loadRecentLogsCompact;
window.applyLogFilters = applyLogFilters;
window.applyK8sSettings = applyK8sSettings;
window.applyLocalLogSettings = applyLocalLogSettings;
window.updateLogSourceUI = updateLogSourceUI;
window.getOrCreateLogsContainer = getOrCreateLogsContainer;

// THEN: Define toggleLogSource which uses the above functions
window.toggleLogSource = function() {
    useKubernetesLogs = !useKubernetesLogs;
    localStorage.setItem('use_k8s_logs', useKubernetesLogs ? 'true' : 'false');
    console.log('toggleLogSource: switched to', useKubernetesLogs ? 'K8s' : 'Local');
    
    // Reset K8s unavailable flag when switching away from K8s
    if (!useKubernetesLogs) {
        k8sUnavailable = false;
    }
    
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
    
    // Update button text
    const toggleBtn = document.getElementById('log-source-toggle');
    if (toggleBtn) {
        toggleBtn.innerHTML = `Switch to <span id="log-source-indicator">${useKubernetesLogs ? 'Local Logs' : 'K8s Logs'}</span>`;
    }
    
    // Show/hide appropriate controls
    if (typeof window.updateLogSourceUI === 'function') {
        window.updateLogSourceUI();
    } else if (typeof updateLogSourceUI === 'function') {
        updateLogSourceUI();
    }
    
    // Restart polling if needed
    if (!useKubernetesLogs && !window._logsPoll) {
        window._logsPoll = setInterval(() => {
            if (typeof window.loadRecentLogsCompact === 'function') {
                window.loadRecentLogsCompact();
            }
        }, 3000);
    }
    
    // Load logs with new source - use window references
    if (typeof window.loadLogs === 'function') {
        window.loadLogs();
    } else if (typeof window.loadRecentLogsCompact === 'function') {
        window.loadRecentLogsCompact();
    } else {
        console.error('loadLogs function not available');
    }
};

window.setLogSource = function(source) {
    useKubernetesLogs = (source === 'k8s');
    localStorage.setItem('use_k8s_logs', useKubernetesLogs ? 'true' : 'false');
    console.log('setLogSource: set to', useKubernetesLogs ? 'K8s' : 'Local');
    
    const indicator = document.getElementById('log-source-indicator');
    if (indicator) {
        indicator.textContent = useKubernetesLogs ? 'K8s Logs' : 'Local Logs';
        indicator.className = useKubernetesLogs ? 'k8s-indicator' : 'local-indicator';
    }
    
    // Update button text
    const toggleBtn = document.getElementById('log-source-toggle');
    if (toggleBtn) {
        toggleBtn.innerHTML = `Switch to <span id="log-source-indicator">${useKubernetesLogs ? 'Local Logs' : 'K8s Logs'}</span>`;
    }
    
    // Show/hide appropriate controls
    if (typeof window.updateLogSourceUI === 'function') {
        window.updateLogSourceUI();
    } else if (typeof updateLogSourceUI === 'function') {
        updateLogSourceUI();
    }
    
    // Load logs with new source - use window references
    if (typeof window.loadLogs === 'function') {
        window.loadLogs();
    } else if (typeof window.loadRecentLogsCompact === 'function') {
        window.loadRecentLogsCompact();
    }
};

// Debug: Log that functions are available
console.log('logs.js loaded - functions available:', {
    toggleLogSource: typeof window.toggleLogSource,
    loadLogs: typeof window.loadLogs,
    loadRecentLogsCompact: typeof window.loadRecentLogsCompact,
    applyK8sSettings: typeof window.applyK8sSettings,
    applyLocalLogSettings: typeof window.applyLocalLogSettings,
    changeLogService: typeof window.changeLogService
});

document.addEventListener('DOMContentLoaded', function() {
    // Restore filter preferences
    const savedLevelFilter = localStorage.getItem('log_level_filter');
    const savedHideVerbose = localStorage.getItem('hide_verbose_logs') === 'true';
    
    if (savedLevelFilter) {
        const levelFilter = document.getElementById('log-level-filter');
        if (levelFilter) {
            levelFilter.value = savedLevelFilter;
            currentLogLevelFilter = savedLevelFilter;
        }
    }
    
    if (savedHideVerbose) {
        const hideVerbose = document.getElementById('hide-verbose-logs');
        if (hideVerbose) {
            hideVerbose.checked = true;
            hideVerboseLogs = true;
        }
    }
    
    // Restore service selection
    const savedService = localStorage.getItem('current_log_service');
    if (savedService) {
        currentLogService = savedService;
        const serviceSelect = document.getElementById('k8s-service-select');
        if (serviceSelect) {
            serviceSelect.value = currentLogService;
        }
    }
    
    // Restore local log file
    const savedLogFile = localStorage.getItem('local_log_file');
    if (savedLogFile) {
        localLogFile = savedLogFile;
        const fileInput = document.getElementById('local-log-file');
        if (fileInput) {
            fileInput.value = localLogFile;
        }
    }
    
    // Restore K8s settings
    const savedNs = localStorage.getItem('k8s_ns');
    const savedSelector = localStorage.getItem('k8s_selector_key');
    if (savedNs) {
        k8sNs = savedNs;
        const nsInput = document.getElementById('k8s-ns-input');
        if (nsInput) nsInput.value = k8sNs;
    }
    if (savedSelector) {
        k8sSelectorKey = savedSelector;
        const selectorInput = document.getElementById('k8s-selector-key-input');
        if (selectorInput) selectorInput.value = k8sSelectorKey;
    }
    
    // Update UI based on current log source
    updateLogSourceUI();
    
    // Update button text
    const toggleBtn = document.getElementById('log-source-toggle');
    if (toggleBtn) {
        toggleBtn.innerHTML = `Switch to <span id="log-source-indicator">${useKubernetesLogs ? 'Local Logs' : 'K8s Logs'}</span>`;
    }
    
    // Populate K8s service dropdown
    const serviceSelect = document.getElementById('k8s-service-select');
    if (serviceSelect) {
        // Set current service as selected
        if (currentLogService) {
            serviceSelect.value = currentLogService;
        }
        
        // Add onchange handler if not already set
        if (!serviceSelect.onchange) {
            serviceSelect.onchange = function() {
                if (typeof changeLogService === 'function') {
                    changeLogService(this.value);
                }
            };
        }
        
        // Try to fetch services from API to populate dropdown
        if (serviceSelect.options.length <= 4) {
            axios.get('/api/k8s/services', { timeout: 4000 }).then(resp => {
                const items = Array.isArray(resp.data) ? resp.data : [];
                const names = items.map(s => s.name).filter(Boolean);
                if (names.length > 0) {
                    const currentValue = serviceSelect.value || currentLogService;
                    serviceSelect.innerHTML = names.map(v => 
                        `<option value="${v}" ${v===currentValue?'selected':''}>${v}</option>`
                    ).join('');
                    // Re-attach onchange handler after innerHTML update
                    serviceSelect.onchange = function() {
                        if (typeof changeLogService === 'function') {
                            changeLogService(this.value);
                        }
                    };
                }
            }).catch(() => {
                // Keep default options
            });
        }
    }
    
    if (typeof loadRecentLogsCompact === 'function') {
        loadRecentLogsCompact();
    }
    if (window._logsPoll) clearInterval(window._logsPoll);
    window._logsPoll = setInterval(() => {
        if (typeof loadRecentLogsCompact === 'function') {
            loadRecentLogsCompact();
        }
    }, 3000);
});

