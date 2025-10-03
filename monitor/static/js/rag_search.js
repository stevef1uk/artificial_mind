(function(){
  async function search(){
    const q = document.getElementById('rag-q').value.trim();
    const limit = document.getElementById('rag-limit').value || '10';
    const collection = document.getElementById('rag-collection').value || 'WikipediaArticle';
    if(!q){ render({error:'Enter a query'}); return; }
    try{
      const url = `/api/rag/search?q=${encodeURIComponent(q)}&limit=${encodeURIComponent(limit)}&collection=${encodeURIComponent(collection)}`;
      const res = await fetch(url);
      if(!res.ok) throw new Error('HTTP '+res.status);
      const data = await res.json();
      render(data);
    }catch(e){ render({error:String(e)}); }
  }
  function render(data){
    const root = document.getElementById('rag-results');
    if(!root) return;
    console.log('RAG Search Debug - Raw data:', data);
    if(data && data.error){ root.innerHTML = `<div class="alert error">${escapeHtml(data.error)}</div>`; return; }
    const items = (data && data.result && data.result.points) || [];
    console.log('RAG Search Debug - Items:', items, 'Length:', items.length);
    if(!Array.isArray(items) || items.length===0){ root.innerHTML = '<div class="loading">No results</div>'; return; }
    root.innerHTML = items.map(it => {
      const payload = (it.payload||{});
      const score = (typeof it.score==='number')? it.score.toFixed(3): '';
      const title = payload.title || payload.Title || payload.name || '(no title)';
      const text = payload.text || payload.Text || payload.summary || '';
      const url = payload.url || '';
      return `<div class="metric"><span class="metric-label">${escapeHtml(title)} (${score})</span><span class="metric-value">${escapeHtml(text.slice(0,200))} ${url?('['+escapeHtml(url)+']'):''}</span></div>`;
    }).join('\n');
  }
  function escapeHtml(s){ return String(s).replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;').replace(/"/g,'&quot;').replace(/'/g,'&#039;'); }
  window.runRagSearch = search;
})();


