// Integration for Smart Scrape Studio to use Go Playwright scraper
// Add this to app.js in the Smart Scrape interface

// Configuration
const SCRAPER_CONFIG = {
  baseUrl: 'http://localhost:8087',
  endpoints: {
    generic: '/api/scraper/generic',
    workflow: '/api/scraper/workflow',
    deploy: '/api/scraper/agent/deploy'
  }
};

// ============ GENERIC SCRAPER INTEGRATION ============

/**
 * Execute a generic scrape with natural language instructions
 */
async function executeGenericScrape(url, instructions, extractions = {}) {
  try {
    const payload = {
      url: url,
      instructions: instructions,
      extractions: extractions,
      wait_time: 2000,
      get_html: false
    };

    const response = await fetch(SCRAPER_CONFIG.baseUrl + SCRAPER_CONFIG.endpoints.generic, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json'
      },
      body: JSON.stringify(payload)
    });

    if (!response.ok) {
      throw new Error(`Server error: ${response.status}`);
    }

    const result = await response.json();
    return result;

  } catch (error) {
    console.error('Scrape error:', error);
    throw error;
  }
}

/**
 * Deploy a scraper as an Agent
 */
async function deployScraperAgent(name, url, instructions, extractions = {}, frequency = 'daily') {
  try {
    const payload = {
      name: name,
      url: url,
      instructions: instructions,
      extractions: extractions,
      frequency: frequency
    };

    const response = await fetch(SCRAPER_CONFIG.baseUrl + SCRAPER_CONFIG.endpoints.deploy, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json'
      },
      body: JSON.stringify(payload)
    });

    if (!response.ok) {
      throw new Error(`Deployment failed: ${response.status}`);
    }

    const result = await response.json();
    return result;

  } catch (error) {
    console.error('Deployment error:', error);
    throw error;
  }
}

/**
 * Execute a workflow with parameters
 */
async function executeWorkflow(workflowName, params = {}) {
  try {
    const payload = {
      workflow_name: workflowName,
      params: params
    };

    const response = await fetch(SCRAPER_CONFIG.baseUrl + SCRAPER_CONFIG.endpoints.workflow, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json'
      },
      body: JSON.stringify(payload)
    });

    if (!response.ok) {
      throw new Error(`Workflow execution failed: ${response.status}`);
    }

    const result = await response.json();
    return result;

  } catch (error) {
    console.error('Workflow error:', error);
    throw error;
  }
}

// ============ UI INTEGRATION HOOKS ============

/**
 * Hook into the Smart Scrape Create Agent button
 * Replace existing handler with this
 */
async function onCreateAgentClicked() {
  const agentName = document.getElementById('agent-name').value || 'My_Scraper';
  const url = document.getElementById('url-input').value;
  const instructions = document.getElementById('goal-input').value;
  const frequency = document.getElementById('schedule-select').value;
  
  if (!url) {
    alert('Please enter a URL');
    return;
  }

  if (!instructions) {
    alert('Please enter extraction instructions');
    return;
  }

  try {
    showStatus('🚀 Deploying agent... This will be scheduled for ' + frequency + ' execution.');
    
    const result = await deployScraperAgent(
      agentName,
      url,
      instructions,
      {},
      frequency
    );

    showStatus('✅ Agent deployed: ' + agentName);
    console.log('Deployment result:', result);

  } catch (error) {
    showStatus('❌ Deployment failed: ' + error.message);
    console.error('Error:', error);
  }
}

/**
 * Hook into the analyze button to use Go scraper instead
 */
async function onAnalyzePage() {
  const url = document.getElementById('url-input').value;
  
  if (!url) {
    alert('Please enter a URL');
    return;
  }

  try {
    showStatus('🔍 Analyzing with Go Playwright scraper...');
    
    const instructions = document.getElementById('goal-input').value || 'Extract all relevant information';
    
    const result = await executeGenericScrape(
      url,
      instructions
    );

    showStatus('✅ Analysis complete');
    console.log('Scrape result:', result);
    
    // Display results in the UI
    displayScrapeResults(result);

  } catch (error) {
    showStatus('❌ Analysis failed: ' + error.message);
    console.error('Error:', error);
  }
}

/**
 * Display scrape results in the UI
 */
function displayScrapeResults(result) {
  const goalInput = document.getElementById('goal-input');
  
  if (result.status === 'success') {
    // Show extracted data
    const dataStr = JSON.stringify(result.data, null, 2);
    const preview = `
✅ Status: ${result.status}
📄 Title: ${result.title}
⏱️  Execution: ${result.execution_time_ms}ms

📋 Extracted Data:
${dataStr}
    `;
    
    alert(preview);
    
    // If screenshot available, show it
    if (result.screenshot) {
      const img = new Image();
      img.src = result.screenshot;
      // Add to preview area if available
    }
  } else {
    alert('❌ Scrape failed: ' + (result.error || 'Unknown error'));
  }
}

/**
 * Helper to show status messages
 */
function showStatus(message) {
  const statusEl = document.getElementById('status-message');
  if (statusEl) {
    statusEl.textContent = message;
    statusEl.classList.remove('hidden');
    statusEl.classList.add('visible');
  } else {
    console.log(message);
  }
}

// ============ EXAMPLE USAGE ============

/*
// Example 1: Scrape Hacker News
executeGenericScrape(
  'https://news.ycombinator.com',
  'Extract story titles and scores'
).then(result => {
  console.log('Stories extracted:', result.data);
});

// Example 2: Deploy daily scraper
deployScraperAgent(
  'Daily_HN_Top_Stories',
  'https://news.ycombinator.com',
  'Extract top 10 stories with scores',
  {},
  'daily'
).then(result => {
  console.log('Agent deployed:', result);
});

// Example 3: Execute MyClimate workflow
executeWorkflow('myclimate_flight', {
  from: 'CDG',
  to: 'NYC'
}).then(result => {
  console.log('Flight emissions:', result.data);
});
*/

export { 
  executeGenericScrape, 
  deployScraperAgent, 
  executeWorkflow,
  onAnalyzePage,
  onCreateAgentClicked,
  displayScrapeResults
};
