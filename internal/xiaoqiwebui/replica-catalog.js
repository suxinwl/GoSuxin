import { matchesSource, matchesChannel, matchesContentPreference } from './ui-core.js';

// Content sections matching the reference catalogue. The data always comes
// from the unified library; this module only groups and orders existing cards.
export function createReplicaCatalog(app) {
  let lastSignature = '';
  let expanded = false;
  let previousCategory = null;
  const rootID = 'replicaCatalog';
  const labels = {
    newest: '最新上架',
    weekly: '本周热门播放',
    popular: '热门电影',
    score: '高分电影'
  };
  const categoryLabels = {
    newest: '最近更新',
    weekly: '热门播放',
    score: '高分'
  };
  const metric = (value) => {
    if (typeof value === 'number') return Number.isFinite(value) ? value : 0;
    const match = String(value ?? '').replace(/[,，\s]/g, '').match(/([\d.]+)(亿|万|千|w|k|m)?/i);
    if (!match) return 0;
    return Number(match[1]) * ({亿: 1e8, 万: 1e4, 千: 1e3, w: 1e4, k: 1e3, m: 1e6}[match[2]?.toLowerCase()] || 1);
  };
  const date = drama => Date.parse(String(drama.onlineDate || drama.releaseDate || drama.airedDate || '')) || 0;
  const score = drama => metric(drama.rating ?? drama.score ?? drama.doubanScore);
  const isCategory = (drama, value) => {
    if (!value) return true;
    return matchesChannel(drama, value);
  };
  const base = (category, source) => (app.library?.all?.() || []).filter(drama => isCategory(drama, category) && matchesSource(drama, source));
  const titleKey = drama => String(drama.title || drama.name || drama.id || '').trim().toLocaleLowerCase('zh-Hans-CN');
  const stableTie = (a, b) => {
    const title = titleKey(a).localeCompare(titleKey(b), 'zh-Hans-CN');
    return title || String(a.id || '').localeCompare(String(b.id || ''));
  };
  const descending = (value, fallback) => (a, b) => {
    const left = value(a), right = value(b);
    if (left !== right) return right - left;
    const leftDate = date(a), rightDate = date(b);
    if (leftDate !== rightDate) return rightDate - leftDate;
    return stableTie(a, b) || fallback(a, b);
  };
  function ordered(items, kind) {
    const copy = items.slice();
    if (kind === 'newest' || kind === 'all') return copy.sort(descending(date, stableTie));
    if (kind === 'weekly') return copy.sort(descending(a => metric(a.weeklyViews ?? a.views ?? a.heat), stableTie));
    if (kind === 'popular') return copy.sort(descending(a => metric(a.heat ?? a.views), stableTie));
    if (kind === 'personal') return copy.sort((a, b) => {
      const hot = metric(b.weeklyViews ?? b.views ?? b.heat) - metric(a.weeklyViews ?? a.views ?? a.heat);
      if (hot) return hot;
      const rated = score(b) - score(a);
      if (rated) return rated;
      const fresh = date(b) - date(a);
      return fresh || stableTie(a, b);
    });
    return copy.sort(descending(score, stableTie));
  }
  function pickDistinct(items, kind, used, limit) {
    const picked = [];
    for (const drama of ordered(items, kind)) {
      const key = String(drama.id || drama.title || drama.name || '');
      if (used.has(key)) continue;
      used.add(key);
      picked.push(drama);
      if (picked.length >= limit) break;
    }
    return picked;
  }
  function section(kind, items, limit = 16, headingText = labels[kind]) {
    const node = document.createElement('section');
    node.className = 'replica-section replica-section-' + kind;
    const heading = document.createElement('div');
    heading.className = 'replica-section-heading';
    const title = document.createElement('h2');
    title.textContent = headingText;
    const link = document.createElement('button');
    link.type = 'button';
    link.className = 'replica-section-link';
    link.textContent = '查看更多';
    link.addEventListener('click', () => {
      expanded = true;
      const sort = kind === 'newest' ? 'newest' : kind === 'score' ? 'heat' : 'views';
      const select = document.getElementById('sortSelect');
      if (select) { select.value = sort; select.dispatchEvent(new Event('change', {bubbles: true})); }
      document.getElementById('cards')?.scrollIntoView({behavior: 'smooth', block: 'start'});
    });
    if (kind === 'personal') link.hidden = true;
    heading.append(title, link);
    const grid = document.createElement('div');
    grid.className = 'replica-card-grid';
    if (!items.length) {
      const empty = document.createElement('p');
      empty.className = 'empty';
      empty.textContent = '当前筛选下暂无影片';
      empty.setAttribute('role', 'status');
      grid.appendChild(empty);
      link.hidden = true;
    }
    for (const drama of ordered(items, kind).slice(0, limit)) {
      const card = app.library?.createCard?.(drama);
      if (card) {
        if (kind === 'weekly') {
          const rank = document.createElement('span');
          rank.className = 'replica-rank';
          rank.textContent = String(grid.childElementCount + 1);
          rank.setAttribute('aria-hidden', 'true');
          card.prepend(rank);
        }
        grid.appendChild(card);
      }
    }
    node.append(heading, grid);
    return node;
  }
  function render() {
    const catalog = document.querySelector('.catalog-content');
    const cards = document.getElementById('cards');
    if (!catalog || !cards || !app.library?.createCard) return;
    const route = window.XiaoqiDialogs?.route?.() || {kind: 'page'};
    const page = window.XiaoqiDialogs?.page?.() || document.body.dataset.page || 'library';
    const category = route.kind === 'category' ? route.value : '';
    if (category !== previousCategory) {expanded = false; previousCategory = category;}
    const search = document.getElementById('searchInput')?.value.trim() || '';
    const active = page === 'library' && !search;
    const categoryPage = page === 'library' && Boolean(category);
    const title = document.getElementById('categoryTitle');
    if (title) {title.hidden = !categoryPage; title.textContent = category;}
    let root = document.getElementById(rootID);
    document.body.classList.toggle('replica-catalog-active', active && !expanded);
    document.body.classList.toggle('replica-category-active', categoryPage);
    if (!active) expanded = false;
    if (!active) {
      if (root) root.hidden = true;
      cards.hidden = false;
      return;
    }
    if (!root) { root = document.createElement('div'); root.id = rootID; catalog.insertBefore(root, cards.parentElement); }
    // Home keeps its curated all-source layout; channel sections and their
    // expanded results share the explicit source filter.
    const source = category ? document.getElementById('sourceSelect')?.value || '' : '';
    const items = base(category, source);
    const preference = !category && app.viewer?.account && !app.viewer.account.admin ? app.viewer?.contentPreference || '' : '';
    const signature = category + '|' + source + '|' + preference + '|' + items.map(item => item.id).join(',');
    if (signature === lastSignature) { root.hidden = expanded; cards.hidden = !expanded; return; }
    lastSignature = signature;
    const sidebar = document.getElementById('sidebarRankings4kvm');
    if (sidebar && root.contains(sidebar)) document.getElementById('contentGrid4kvm')?.append(sidebar);
    const newest = category ? null : section('newest', items, 12, labels.newest);
    if (!category) {
      const top = document.createElement('div');
      top.className = 'replica-home-top';
      top.append(newest);
      if (sidebar) top.append(sidebar);
      const children = [top];
      if (preference) children.unshift(section('personal', items.filter(item => matchesContentPreference(item, preference)), 12, '为你推荐'));
      children.push(section('weekly', items, 12), section('popular', items, 12), section('score', items, 12));
      root.replaceChildren(...children);
    } else {
      // Some channel records have no rating/view metadata. Without a
      // fallback, every sorter produces the same order. Keep each channel
      // section visually useful by allocating non-overlapping groups in the
      // requested order while preserving metric-based sorting where present.
      const used = new Set();
      const newestItems = pickDistinct(items, 'newest', used, 12);
      const weeklyItems = pickDistinct(items, 'weekly', used, 12);
      const scoreItems = pickDistinct(items, 'score', used, 12);
      root.replaceChildren(
        section('newest', newestItems, 12, categoryLabels.newest),
        section('weekly', weeklyItems, 12, categoryLabels.weekly),
        section('score', scoreItems, 12, categoryLabels.score)
      );
    }
    root.hidden = expanded;
    cards.hidden = !expanded;
  }
  return {render};
}
