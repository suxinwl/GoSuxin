/**
 * 4kvm-ui.js — 4KVM 前端 UI 主控模块
 * 负责：侧栏榜单渲染、
 *       导航栏交互（汉堡菜单、历史浮层、移动端搜索同步）、
 *       分类胶囊选择、导航按钮页面切换联动
 */

import { createSidebarRankings } from '/assets/sidebar-rankings.js';
import { createReplicaCatalog } from '/assets/replica-catalog.js';

function getLibraryDramas() {
  try {
    // 优先使用 library.all()（新版 createLibrary 返回的接口）
    if (typeof window.app?.library?.all === 'function') {
      const all = window.app.library.all();
      if (all?.length > 0) return all;
    }
    // 兜底：旧版字段名
    if (window.app?.library?.items?.length) return window.app.library.items;
    if (window.app?.library?.dramas?.length) return window.app.library.dramas;
    if (window.dramas?.length) return window.dramas;
  } catch {}
  return [];
}

/* ─── 构造全局 app 代理，供侧栏榜单使用 ─── */
const appProxy = {
  get library() { return window.app?.library; },
  get following() { return window.app?.following; },
  get downloads() { return window.app?.downloads; },
  get viewer() { return window.app?.viewer; },
  play(id, title) {
    if (window.app?.play) { window.app.play(id, title); return; }
    const btn = document.querySelector(`[data-drama-id="${id}"] .poster`);
    if (btn) { btn.click(); return; }
    if (window.app?.details?.open) window.app.details.open(id);
  },
  details: {
    open(id) {
      if (window.app?.details?.open) { window.app.details.open(id); return; }
      document.dispatchEvent(new CustomEvent('drama:open', { detail: { id } }));
    }
  }
};

/* ─── 主初始化 ─── */
function init() {
  // Navigation must be usable before the library request finishes, including
  // on login pages and installations whose catalogue is still empty.
  initNavbar();
  initCategoryPills();
  initSearchSync();

  const sidebarRankings = createSidebarRankings(appProxy);
  const replicaCatalog = createReplicaCatalog(appProxy);
  let needsRender = true, rendering = false, sidebarAvailable = false;

  function currentPresentation() {
    const route = window.XiaoqiDialogs?.route?.() || {kind: 'page'};
    const page = window.XiaoqiDialogs?.page?.() || document.body.dataset.page || 'library';
    const login = location.pathname === '/login' || document.body.classList.contains('account-page');
    const home = !login && page === 'library' && route.kind === 'page';
    const category = !login && page === 'library' && route.kind === 'category';
    return {route, page, home, category};
  }

  function applyRoutePresentation() {
    const {route, page, home, category} = currentPresentation();
    const sidebar = document.getElementById('sidebarRankings4kvm');
    const grid = document.getElementById('contentGrid4kvm');
    const showSidebar = home && sidebarAvailable && window.innerWidth >= 1280;
    for (const [node, visible] of [[sidebar, showSidebar]]) {
      if (!node) continue;
      node.style.removeProperty('display');
      node.hidden = !visible;
    }
    if (grid) {
      grid.style.removeProperty('grid-template-columns');
      grid.classList.toggle('with-home-sidebar', showSidebar);
    }
    replicaCatalog.render();
    document.querySelectorAll('.nav-item-4kvm[data-page]').forEach(item => {
      const selected = item.dataset.page === 'library' ? home : !category && item.dataset.page === page;
      item.classList.toggle('active', selected);
      if (selected) item.setAttribute('aria-current', 'page');
      else item.removeAttribute('aria-current');
    });
    document.querySelectorAll('.nav-category[data-filter-channel], .tab-pill[data-filter-channel]').forEach(item => {
      const selected = category && item.dataset.filterChannel === route.value;
      item.classList.toggle('active', selected);
      item.setAttribute('aria-pressed', String(selected));
    });
  }

  async function renderHome() {
    if (rendering || !needsRender || !currentPresentation().home) return;
    // The first server page is already a usable snapshot. Render the home
    // sections immediately and let the library publish one final change when
    // the remaining pages finish loading.
    const dramas = getLibraryDramas();
    if (!dramas.length) return;
    rendering = true;
    needsRender = false;
    try {
      // Clear stale home-only widgets before a refresh. Their renderers may
      // intentionally return early for an empty result and leave old nodes.
      document.getElementById('sidebarRankings4kvm')?.replaceChildren();
      sidebarRankings.render(dramas);
      const sidebar = document.getElementById('sidebarRankings4kvm');
      sidebarAvailable = Boolean(sidebar?.childElementCount);
      applyRoutePresentation();
    } catch (error) {
      console.error(error);
    } finally {
      rendering = false;
      // A render may finish after the viewer navigates to playback. Always
      // re-check the current route instead of revealing home-only content.
      applyRoutePresentation();
      if (needsRender) void renderHome();
    }
  }

  const updateRoute = () => {applyRoutePresentation(); void renderHome();};
  window.XiaoqiDialogs?.listenRoute?.(updateRoute);
  window.addEventListener('resize', applyRoutePresentation);
  window.addEventListener('xiaoqilibrarychange', () => {
    needsRender = true;
    updateRoute();
  });
  window.addEventListener('xiaoqilibraryviewchange', () => replicaCatalog.render());
  updateRoute();
}

/* ── 导航栏交互 ── */
function initNavbar() {
  /* 汉堡菜单 */
  const toggle  = document.getElementById('mobileMenuToggle');
  const drawer  = document.getElementById('mobileMenuDrawer');
  const icon    = document.getElementById('mobileMenuIcon');
  const iconX   = document.getElementById('mobileMenuCloseIcon');
  function closeDrawer() {
    drawer?.classList.remove('open');
    drawer?.setAttribute('aria-hidden', 'true');
    toggle?.setAttribute('aria-expanded', 'false');
    if (icon) icon.hidden = false;
    if (iconX) iconX.hidden = true;
  }
  for (const item of [icon, iconX]) item?.style.removeProperty('display');
  closeDrawer();

  if (toggle && drawer) {
    toggle.addEventListener('click', () => {
      const isOpen = drawer.classList.toggle('open');
      toggle.setAttribute('aria-expanded', String(isOpen));
      drawer.setAttribute('aria-hidden', String(!isOpen));
      if (icon) icon.hidden = isOpen;
      if (iconX) iconX.hidden = !isOpen;
    });
    // 点击外部关闭
    document.addEventListener('click', e => {
      if (!drawer.contains(e.target) && !toggle.contains(e.target)) {
        closeDrawer();
      }
    });
  }

  /* 观看历史：导航入口打开与追剧页相同的完整历史弹窗，
     这样单条删除和清空都使用同一个服务端记录源。 */
  const historyBtn = document.getElementById('navHistoryBtn');
  const historyClearBtn = document.getElementById('navHistoryClearBtn');
  const historyMobileClearBtn = document.getElementById('navHistoryMobileClearBtn');
  const openFullHistory = () => {
    closeAllHistoryPanels();
    if (window.XiaoqiHistory?.open) window.XiaoqiHistory.open();
    else document.getElementById('openHistoryBtn')?.click();
  };
  function closeAllHistoryPanels() {
    document.getElementById('navHistoryPopover')?.classList.remove('open');
    document.getElementById('navHistoryMobile')?.classList.remove('open');
    historyBtn?.setAttribute('aria-expanded', 'false');
  }
  historyBtn?.addEventListener('click', e => {
    e.stopPropagation();
    openFullHistory();
  });
  for (const clear of [historyClearBtn, historyMobileClearBtn]) {
    clear?.addEventListener('click', e => {
      e.stopPropagation();
      window.XiaoqiHistory?.remove?.('');
    });
  }
  document.addEventListener('click', e => {
    if (!e.target.closest('#navHistoryWrap')) closeAllHistoryPanels();
  });

  /* 导航按钮只向路由器提交一次目标，active/可见性由当前路由计算。 */
  const navItems4kvm = document.querySelectorAll('.nav-item-4kvm[data-page]');
  const categoryNavItems = document.querySelectorAll('.nav-category[data-filter-channel]');
  navItems4kvm.forEach(btn => {
    btn.addEventListener('click', () => {
      window.XiaoqiDialogs?.navigate(btn.dataset.page);
      closeDrawer();
    });
  });

  categoryNavItems.forEach(btn => {
    btn.addEventListener('click', () => {
      window.XiaoqiDialogs?.navigateCategory(btn.dataset.filterChannel || '');
      closeDrawer();
    });
  });
}

/* ── 分类胶囊联动 ── */
function initCategoryPills() {
  const pills      = document.querySelectorAll('.tab-pill[data-filter-channel]');
  pills.forEach(pill => {
    pill.addEventListener('click', () => {
      window.XiaoqiDialogs?.navigateCategory(pill.dataset.filterChannel || '');
    });
  });
}

/* ── 搜索框同步 ── */
function initSearchSync() {
  const navSearch    = document.getElementById('navSearchInput');
  const mobileSearch = document.getElementById('mobileSearchInput');
  const mainSearch   = document.getElementById('searchInput');

  function syncToMain(val) {
    if (!mainSearch) return;
    mainSearch.value = val;
    mainSearch.dispatchEvent(new Event('input', { bubbles: true }));
  }

  navSearch?.addEventListener('input', e => {
    syncToMain(e.target.value);
    if (mobileSearch) mobileSearch.value = e.target.value;
  });
  mobileSearch?.addEventListener('input', e => {
    syncToMain(e.target.value);
    if (navSearch) navSearch.value = e.target.value;
  });

  // 按 Enter 提交搜索
  [navSearch, mobileSearch].forEach(input => {
    input?.addEventListener('keydown', e => {
      if (e.key === 'Enter') {
        e.preventDefault();
        syncToMain(input.value);
        // 关闭汉堡
        document.getElementById('mobileMenuDrawer')?.classList.remove('open');
      }
    });
  });

  // 主搜索框变化时反向同步（比如点了重置）
  mainSearch?.addEventListener('input', e => {
    if (navSearch && navSearch.value !== e.target.value) navSearch.value = e.target.value;
    if (mobileSearch && mobileSearch.value !== e.target.value) mobileSearch.value = e.target.value;
  });
}

function escHtml(s) {
  return String(s || '').replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;').replace(/"/g,'&quot;');
}

/* ─── 启动 ─── */
init();
