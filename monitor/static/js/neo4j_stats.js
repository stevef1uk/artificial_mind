(function(){
  async function fetchNeo4jStats(){
    try{
      const res = await fetch('/api/neo4j/stats', {headers:{'Accept':'application/json'}});
      if(!res.ok){ throw new Error('HTTP '+res.status); }
      return await res.json();
    }catch(err){
      return { error: String(err) };
    }
  }

  function renderNeo4jStats(data){
    const root = document.getElementById('neo4j-stats');
    if(!root) return;
    if(data && data.error){
      root.innerHTML = '<div class="alert error">'+escapeHtml(data.error)+'</div>';
      return;
    }
    const nodes = data && typeof data.nodes === 'number' ? data.nodes : 0;
    const rels  = data && typeof data.relationships === 'number' ? data.relationships : 0;
    const ts    = data && data.checked_at ? data.checked_at : '';
    const gc    = data && data.graph_counts ? data.graph_counts : null;
    const labels = Array.isArray(data.labels) ? data.labels : [];
    const relTypes = Array.isArray(data.rel_types) ? data.rel_types : [];
    const domains = Array.isArray(data.domains) ? data.domains : [];
    const concepts = Array.isArray(data.concepts) ? data.concepts : [];

    let extra = '';
    if(gc){
      try{
        extra = '<pre class="code-block" style="white-space:pre-wrap; max-height:180px; overflow:auto;">'+escapeHtml(JSON.stringify(gc, null, 2))+'</pre>';
      }catch(e){ /* ignore */ }
    }

    const lbl = labels.map(x => '<div class="metric"><span class="metric-label">'+escapeHtml(String(x.label))+'</span><span class="metric-value">'+x.count+'</span></div>').join('\n');
    const relb = relTypes.map(x => '<div class="metric"><span class="metric-label">'+escapeHtml(String(x.type))+'</span><span class="metric-value">'+x.count+'</span></div>').join('\n');
    const domb = domains.map(x => '<div class="metric"><span class="metric-label">'+escapeHtml(String(x.domain))+'</span><span class="metric-value">'+x.count+'</span></div>').join('\n');
    const conb = concepts.map(x => '<div class="metric"><span class="metric-label">'+escapeHtml(String(x.name))+' ('+escapeHtml(String(x.domain))+')</span><span class="metric-value">'+escapeHtml(String(x.definition||''))+'</span></div>').join('\n');

    root.innerHTML = [
      '<div class="metric"><span class="metric-label">Nodes</span><span class="metric-value">'+nodes+'</span></div>',
      '<div class="metric"><span class="metric-label">Relationships</span><span class="metric-value">'+rels+'</span></div>',
      '<h4 style="margin-top:8px;">Labels</h4>',
      lbl || '<div class="metric"><span class="metric-label">(none)</span><span class="metric-value">0</span></div>',
      '<h4 style="margin-top:8px;">Relationship Types</h4>',
      relb || '<div class="metric"><span class="metric-label">(none)</span><span class="metric-value">0</span></div>',
      '<h4 style="margin-top:8px;">Concepts by Domain</h4>',
      domb || '<div class="metric"><span class="metric-label">(none)</span><span class="metric-value">0</span></div>',
      '<h4 style="margin-top:8px;">Sample Concepts</h4>',
      conb || '<div class="metric"><span class="metric-label">(none)</span><span class="metric-value">0</span></div>',
      extra
    ].join('\n');

    const stamp = document.getElementById('neo4j-stats-timestamp');
    if(stamp){ stamp.textContent = 'Last checked: ' + (ts || new Date().toISOString()); }
  }

  function escapeHtml(s){
    return String(s)
      .replace(/&/g,'&amp;')
      .replace(/</g,'&lt;')
      .replace(/>/g,'&gt;')
      .replace(/"/g,'&quot;')
      .replace(/'/g,'&#039;');
  }

  async function refresh(){
    const data = await fetchNeo4jStats();
    renderNeo4jStats(data);
  }

  // Expose to global for button hook
  window.refreshNeo4jStats = refresh;

  // Auto-refresh every 5 minutes
  document.addEventListener('DOMContentLoaded', function(){
    refresh();
    setInterval(refresh, 5*60*1000);
  });
})();


