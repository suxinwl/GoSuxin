/**
 * sidebar-rankings.js — 4KVM 风格右侧榜单排行小部件
 * 从 app.library.all() 取评分/热度最高的影视展示
 */

export function createSidebarRankings(app) {
  const CONTAINER_ID = 'sidebarRankings4kvm';

  /* 星级 SVG */
  const STAR_SVG = `<svg class="icon" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><path d="M12 17.27L18.18 21l-1.64-7.03L22 9.24l-7.19-.61L12 2 9.19 8.63 2 9.24l5.46 4.73L5.82 21z"/></svg>`;
  const TROPHY_SVG = `<svg class="icon" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><path d="M19 3H5v10c0 3.25 2.6 5.9 5.82 5.99L12 19v2H8v2h8v-2h-4v-2l1.18-.01C16.4 18.9 19 16.25 19 13V3zM7 5h10v8c0 2.21-1.79 4-4 4H11c-2.21 0-4-1.79-4-4V5zm-4 0h2v4H3V5zm18 0v4h-2V5h2z"/></svg>`;

  function rankNumClass(i) {
    if (i === 0) return 'gold';
    if (i === 1) return 'silver';
    if (i === 2) return 'bronze';
    return 'other';
  }

  function escHtml(s) {
    return String(s || '').replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;').replace(/"/g,'&quot;');
  }

  function buildRankItem(drama, index) {
    const title  = escHtml(drama.title || drama.name || '未知');
    const cover  = escHtml(drama.cover || '');
    const rating = drama.rating ? parseFloat(drama.rating).toFixed(1) : '';
    const source = escHtml(drama.source || '');
    const numCls = rankNumClass(index);

    return `
      <a class="rank-item" role="listitem" data-drama-id="${escHtml(drama.id)}" href="javascript:void(0)"
         style="display:flex;align-items:center;gap:1rem;padding:.5rem;border-radius:.5rem;text-decoration:none;cursor:pointer;transition:background .15s;">
        <span class="rank-num ${numCls}"
              style="font-size:1.5rem;font-weight:700;width:2rem;text-align:center;flex-shrink:0;
                     color:${numCls==='other'?'#4b5563':'#ff5991'};">${index + 1}</span>
        <div class="rank-cover" style="width:3rem;height:4rem;border-radius:.25rem;overflow:hidden;flex-shrink:0;background:#1a1e24;">
          ${cover ? `<img src="${cover}" alt="${title}" style="width:100%;height:100%;object-fit:cover;" loading="lazy">` :
            `<div style="width:100%;height:100%;display:flex;align-items:center;justify-content:center;color:#4b5563;font-size:.75rem;">${title.slice(0,2)}</div>`}
        </div>
        <div class="rank-info" style="flex:1;min-width:0;">
          <h4 class="rank-title" style="color:#fff;font-weight:500;font-size:.875rem;white-space:nowrap;overflow:hidden;text-overflow:ellipsis;margin:0 0 .25rem;transition:color .2s;">${title}</h4>
          <div style="display:flex;align-items:center;gap:.5rem;font-size:.75rem;color:#6b7280;">
            ${source ? `<span>${source}</span>` : ''}
          </div>
        </div>
        ${rating ? `<div class="rank-score" style="color:#eab308;font-weight:700;font-size:.875rem;display:flex;align-items:center;gap:.25rem;flex-shrink:0;">
          ${STAR_SVG}${rating}
        </div>` : ''}
      </a>`;
  }

  function pickTopDramas(dramas, count = 10) {
    if (!dramas || !dramas.length) return [];
    const tie = (a, b) => String(a.id || '').localeCompare(String(b.id || ''));
    // 优先有 rating 的，再按 heat/views 排
    const withRating = dramas
      .filter(d => d.rating && parseFloat(d.rating) > 0)
      .sort((a, b) => parseFloat(b.rating) - parseFloat(a.rating) || tie(a, b));
    const withHeat = dramas
      .filter(d => !(d.rating && parseFloat(d.rating) > 0) && d.heat)
      .sort((a, b) => (b.heat || 0) - (a.heat || 0) || tie(a, b));
    return [...withRating, ...withHeat].slice(0, count);
  }

  function render(dramas) {
    const container = document.getElementById(CONTAINER_ID);
    if (!container) return;

    const top = pickTopDramas(dramas, 10);
    if (!top.length) { container.style.display = 'none'; return; }
    container.style.display = '';

    container.innerHTML = `
      <div class="sidebar-rankings" style="background:#1a1e24;border-radius:.75rem;padding:1.5rem;">
        <h2 style="font-size:1.25rem;font-weight:700;color:#fff;margin:0 0 1.5rem;display:flex;align-items:center;gap:.5rem;">
          <span style="color:#eab308;font-size:1.25rem;">${TROPHY_SVG}</span>
          榜单排行
        </h2>
        <div class="rank-list" role="list" style="display:flex;flex-direction:column;gap:.25rem;">
          ${top.map((d, i) => buildRankItem(d, i)).join('')}
        </div>
      </div>`;

    // 点击榜单项 → 打开详情
    container.addEventListener('click', e => {
      const item = e.target.closest('.rank-item');
      if (!item) return;
      const id = item.dataset.dramaId;
      if (id && app.details) app.details.open(id);
    });
    // 悬停高亮
    container.querySelectorAll('.rank-item').forEach(item => {
      item.addEventListener('mouseenter', () => {
        item.style.background = '#23282f';
        const title = item.querySelector('.rank-title');
        if (title) title.style.color = '#ff5991';
      });
      item.addEventListener('mouseleave', () => {
        item.style.background = '';
        const title = item.querySelector('.rank-title');
        if (title) title.style.color = '#fff';
      });
    });
  }

  return { render };
}
