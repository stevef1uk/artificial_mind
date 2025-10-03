(function(){
  async function fetchWeaviate(){
    try{
      const res = await fetch('/api/weaviate/stats', {headers:{'Accept':'application/json'}});
      if(!res.ok) throw new Error('HTTP '+res.status);
      return await res.json();
    }catch(e){ return { error: String(e) }; }
  }
  function renderWeaviate(data){
    const root = document.getElementById('qdrant-stats');
    if(!root) return;
    if(data && data.error){ root.innerHTML = '<div class="alert error">'+escapeHtml(data.error)+'</div>'; return; }
    const cols = (data && data.collections) || [];
    const parts = cols.map(c => {
      const name = c.name || 'Unknown';
      const points = c.points_count || 'N/A';
      return '<div class="metric"><span class="metric-label">'+escapeHtml(name)+'</span><span class="metric-value">Class: '+escapeHtml(name)+' | Points: '+escapeHtml(String(points))+'</span></div>';
    });
    root.innerHTML = parts.join('\n') || '<div class="loading">No classes found</div>';
    const stamp = document.getElementById('qdrant-stats-timestamp');
    if(stamp && data && data.checked_at){ stamp.textContent = 'Last checked: '+data.checked_at; }
  }
  function escapeHtml(s){ return String(s).replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;').replace(/"/g,'&quot;').replace(/'/g,'&#039;'); }
  async function refresh(){ renderWeaviate(await fetchWeaviate()); }
  window.refreshQdrantStats = refresh;
  console.log('refreshQdrantStats function defined:', typeof window.refreshQdrantStats);
  document.addEventListener('DOMContentLoaded', function(){ 
    console.log('DOM loaded, calling refresh');
    refresh(); 
    setInterval(refresh, 5*60*1000); 
  });
})();


