async function loadDailySummaryLatest() {
    try {
        const res = await axios.get('/api/daily_summary/latest');
        const obj = res.data;
        const text = (obj.summary || '').toString();
        const el = document.getElementById('daily-summary');
        if (el) el.textContent = `Date: ${obj.date || ''}\nGenerated: ${obj.generated_at || ''}\n\n${text}`;
    } catch (e) {
        const el = document.getElementById('daily-summary');
        if (el) el.textContent = 'No daily summary available yet.';
    }
    loadDailySummaryHistory();
}

async function loadDailySummary() {
    const dateInput = document.getElementById('ds-date');
    const date = dateInput ? dateInput.value : '';
    if (!date) { loadDailySummaryLatest(); return; }
    try {
        const res = await axios.get(`/api/daily_summary/${encodeURIComponent(date)}`);
        const obj = res.data;
        const el = document.getElementById('daily-summary');
        if (el) el.textContent = `Date: ${obj.date || ''}\nGenerated: ${obj.generated_at || ''}\n\n${(obj.summary||'').toString()}`;
    } catch (e) {
        const el = document.getElementById('daily-summary');
        if (el) el.textContent = 'Not found for that date.';
    }
}

async function loadDailySummaryHistory() {
    try {
        const res = await axios.get('/api/daily_summary/history');
        const items = (res.data && res.data.history) || [];
        const html = items.map(it => {
            const date = it.date || '';
            const summary = (it.summary||'').toString();
            const preview = summary.length > 200 ? summary.slice(0, 200) + '...' : summary;
            return `<div style="padding:8px; border-bottom:1px solid #eee;">
                <div style="font-weight:bold;">${date}</div>
                <div style="color:#555; white-space:pre-wrap;">${preview}</div>
            </div>`
        }).join('');
        const el = document.getElementById('daily-summary-history');
        if (el) el.innerHTML = html || '<div>No history</div>';
    } catch (e) {
        const el = document.getElementById('daily-summary-history');
        if (el) el.innerHTML = '<div>Error loading history</div>';
    }
}

document.addEventListener('DOMContentLoaded', () => {
    try {
        const today = new Date();
        const yyyy = today.getUTCFullYear();
        const mm = String(today.getUTCMonth()+1).padStart(2,'0');
        const dd = String(today.getUTCDate()).padStart(2,'0');
        const el = document.getElementById('ds-date');
        if (el) el.value = `${yyyy}-${mm}-${dd}`;
    } catch {}
    if (typeof loadDailySummaryLatest === 'function') {
        loadDailySummaryLatest();
    }
});

