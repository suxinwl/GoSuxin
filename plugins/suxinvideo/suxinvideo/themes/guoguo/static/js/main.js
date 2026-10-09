/* Guoguo presentation connected to the SuxinVideo CMS routes. */
(function () {
  'use strict';
  const q = (selector) => document.querySelector(selector);
  const qa = (selector) => Array.from(document.querySelectorAll(selector));
  const imageFallback = '/suxinvideo/asset?theme=guoguo&file=nopic.svg';
  const reducedMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
  const toast = function (message, ok) {
    const node = document.createElement('div');
    node.setAttribute('role', 'status');
    node.style.cssText = 'position:fixed;top:72px;left:50%;transform:translateX(-50%);z-index:100001;max-width:calc(100vw - 28px);padding:12px 20px;border-radius:9px;color:#fff;box-shadow:0 10px 35px #0005;overflow-wrap:anywhere;background:' + (ok === false ? '#c33347' : '#238955');
    node.textContent = String(message || '');
    document.body.appendChild(node);
    setTimeout(() => node.remove(), 3000);
  };
  window.sxToast = toast;
  window.sxPost = (address, data) => fetch(address, {method: 'POST', body: data, credentials: 'same-origin', headers: {'X-Requested-With': 'XMLHttpRequest'}}).then((response) => response.json());

  let preference = 'dark';
  try { preference = localStorage.getItem('sx_guoguo_theme') || 'dark'; } catch (_) {}
  if (!['dark', 'light', 'system'].includes(preference)) preference = 'dark';
  const systemTheme = window.matchMedia('(prefers-color-scheme: dark)');
  function applyTheme() {
    const theme = preference === 'system' ? (systemTheme.matches ? 'dark' : 'light') : preference;
    document.documentElement.dataset.theme = theme;
    const color = q('meta[name="theme-color"]');
    if (color) color.content = theme === 'light' ? '#f5f6f8' : '#12171b';
    qa('[data-theme-toggle]').forEach((button) => {
      button.setAttribute('aria-label', theme === 'dark' ? '切换浅色外观' : '切换深色外观');
      button.setAttribute('title', theme === 'dark' ? '切换浅色外观' : '切换深色外观');
      button.setAttribute('aria-pressed', String(theme === 'light'));
    });
    document.dispatchEvent(new CustomEvent('themechange', {detail: preference}));
  }
  qa('[data-theme-toggle]').forEach((button) => button.addEventListener('click', () => {
    preference = document.documentElement.dataset.theme === 'dark' ? 'light' : 'dark';
    try { localStorage.setItem('sx_guoguo_theme', preference); } catch (_) {}
    applyTheme();
  }));
  if (systemTheme.addEventListener) systemTheme.addEventListener('change', applyTheme);
  applyTheme();

  const menuToggle = q('#mobileMenuToggle,.nav-mobile-toggle');
  const menu = q('#mobileMenu,.mobile-menu-drawer');
  const nav = q('.nav-links-desktop');
  const navInner = q('.navbar-inner');
  const navLogo = q('.navbar-logo.brand');
  const navActions = q('.navbar-actions');
  // A backdrop-filter header establishes the containing block for fixed
  // descendants. Place full-height mobile panels outside that header.
  if (menu) document.body.appendChild(menu);
  const accountButton = q('#openAccountBtn');
  if (accountButton) accountButton.setAttribute('aria-label', q('#accountLabel')?.textContent.trim() || '我的账号');
  let mask = q('.guoguo-mobile-mask');
  if (menu && !mask) {
    mask = document.createElement('div');
    mask.className = 'guoguo-mobile-mask';
    mask.setAttribute('aria-hidden', 'true');
    document.body.appendChild(mask);
  }
  function setMenu(open, restoreFocus) {
    document.body.classList.toggle('guoguo-mobile-open', open);
    if (menuToggle) menuToggle.setAttribute('aria-expanded', String(open));
    if (menu) {
      menu.setAttribute('aria-hidden', String(!open));
      menu.inert = !open;
    }
    if (open) menu?.querySelector('a,button')?.focus({preventScroll: true});
    else if (restoreFocus) menuToggle?.focus({preventScroll: true});
  }
  if (menuToggle && menu) {
    setMenu(false, false);
    menuToggle.addEventListener('click', () => setMenu(!document.body.classList.contains('guoguo-mobile-open'), true));
    mask?.addEventListener('click', () => setMenu(false, true));
    menu.addEventListener('click', (event) => { if (event.target.closest('a')) setMenu(false, false); });
  }
  // Measure the complete menu even while the compact layout hides it. The
  // measurements run synchronously, with the navigation outside document flow.
  // Using the previous hidden width would alternate between both layouts.
  function updateNavLayout() {
    if (!nav || !navInner || !navLogo || !navActions || !menuToggle) return;
    if (innerWidth < 1024) {
      document.body.classList.add('guoguo-compact-nav');
      return;
    }
    document.body.classList.add('guoguo-compact-nav');
    const previous = nav.getAttribute('style');
    const set = (key, value) => nav.style.setProperty(key, value, 'important');
    set('position', 'absolute'); set('visibility', 'hidden'); set('display', 'flex');
    set('width', 'max-content'); set('max-width', 'none'); set('overflow', 'visible');
    set('left', '0'); set('top', '0');
    const number = (value) => Number.parseFloat(value) || 0;
    const navStyle = getComputedStyle(nav);
    const contentWidth = nav.getBoundingClientRect().width + number(navStyle.marginLeft) + number(navStyle.marginRight);
    if (previous === null) nav.removeAttribute('style'); else nav.setAttribute('style', previous);
    const innerStyle = getComputedStyle(navInner);
    const leftStyle = getComputedStyle(navLogo.parentElement);
    const actionStyle = getComputedStyle(navActions);
    const actionWidth = navActions.getBoundingClientRect().width - menuToggle.getBoundingClientRect().width - number(actionStyle.columnGap);
    const available = navInner.clientWidth - number(innerStyle.paddingLeft) - number(innerStyle.paddingRight)
      - navLogo.getBoundingClientRect().width - number(leftStyle.columnGap) - number(innerStyle.columnGap) - actionWidth;
    const compact = contentWidth > available + 1;
    document.body.classList.toggle('guoguo-compact-nav', compact);
    if (!compact) setMenu(false, false);
  }
  let navFrame = 0;
  const scheduleNavLayout = () => {
    cancelAnimationFrame(navFrame);
    navFrame = requestAnimationFrame(updateNavLayout);
  };
  updateNavLayout();
  window.addEventListener('resize', scheduleNavLayout);
  navLogo?.querySelector('img')?.addEventListener('load', scheduleNavLayout);
  document.fonts?.ready.then(scheduleNavLayout);
  document.addEventListener('keydown', (event) => {
    if (event.key === 'Escape') {
      setMenu(false, true);
      closeHistory();
    }
    if (event.key === 'Tab' && document.body.classList.contains('guoguo-mobile-open')) {
      const nodes = [menuToggle, ...Array.from(menu.querySelectorAll('a[href],button:not([disabled])'))].filter(Boolean);
      const index = nodes.indexOf(document.activeElement);
      if (!event.shiftKey && index === nodes.length - 1) {event.preventDefault(); nodes[0].focus();}
      else if (event.shiftKey && index <= 0) {event.preventDefault(); nodes[nodes.length - 1].focus();}
    }
  });

  const historyButton = q('#navHistoryBtn');
  const historyPopover = q('#navHistoryPopover');
  const historyMobile = q('#navHistoryMobile');
  if (historyMobile) document.body.appendChild(historyMobile);
  const historyLists = qa('#navHistoryList,#navHistoryMobileList');
  function safeImage(raw) {
    const text = String(raw || '').trim();
    return /^(https?:\/\/|\/[^/])/i.test(text) ? text : imageFallback;
  }
  function readHistory() {
    try {
      const items = JSON.parse(localStorage.getItem('sx_watch_hist') || '[]');
      return Array.isArray(items) ? items.filter((item) => /^\d+$/.test(String(item.id))).slice(0, 20) : [];
    } catch (_) { return []; }
  }
  function renderHistory() {
    const records = readHistory();
    historyLists.forEach((list) => {
      list.replaceChildren();
      if (!records.length) {
        const empty = document.createElement('div');
        empty.className = 'history-empty'; empty.textContent = '暂无观看记录';
        list.appendChild(empty); return;
      }
      records.forEach((item) => {
        const link = document.createElement('a');
        link.className = 'history-item';
        link.href = '/suxinvideo/play?id=' + encodeURIComponent(item.id) + '&episode=' + Math.max(0, Number(item.ep || 1) - 1);
        const cover = document.createElement('div'); cover.className = 'history-item-cover';
        const image = document.createElement('img'); image.src = safeImage(item.pic); image.alt = ''; image.loading = 'lazy';
        cover.appendChild(image);
        const info = document.createElement('div'); info.className = 'history-item-info';
        const title = document.createElement('div'); title.className = 'history-item-title'; title.textContent = String(item.name || '未命名影片');
        const ep = document.createElement('div'); ep.className = 'history-item-ep'; ep.textContent = '看到第 ' + Math.max(1, Number(item.ep) || 1) + ' 集';
        const time = document.createElement('div'); time.className = 'history-item-time';
        const date = new Date(Number(item.time)); time.textContent = Number.isFinite(date.getTime()) ? date.toLocaleDateString('zh-CN') : '';
        info.append(title, ep, time); link.append(cover, info); list.appendChild(link);
      });
      const all = document.createElement('a'); all.className = 'history-item'; all.href = '/suxinvideo/history'; all.textContent = '查看账户观看记录'; list.appendChild(all);
    });
  }
  function closeHistory() {
    historyPopover?.classList.remove('open');
    historyMobile?.classList.remove('open');
    historyButton?.setAttribute('aria-expanded', 'false');
  }
  historyButton?.addEventListener('click', () => {
    const open = historyButton.getAttribute('aria-expanded') !== 'true';
    renderHistory(); closeHistory();
    if (!open) return;
    const target = innerWidth <= 640 && historyMobile ? historyMobile : historyPopover || historyMobile;
    target?.classList.add('open'); historyButton.setAttribute('aria-expanded', 'true');
  });
  qa('[data-history-close]').forEach((button) => button.addEventListener('click', closeHistory));
  qa('#navHistoryClearBtn,#navHistoryMobileClearBtn').forEach((button) => button.addEventListener('click', () => {
    try {localStorage.removeItem('sx_watch_hist');} catch (_) {}
    renderHistory(); toast('已清空此浏览器的观看记录');
  }));
  document.addEventListener('click', (event) => {
    if (!historyButton?.contains(event.target) && !historyPopover?.contains(event.target) && !historyMobile?.contains(event.target)) closeHistory();
  });
  window.addEventListener('storage', (event) => {if (event.key === 'sx_watch_hist') renderHistory();});

  qa('[data-follow-id]').forEach((button) => button.addEventListener('click', async (event) => {
    event.preventDefault(); event.stopPropagation();
    const id = button.dataset.followId;
    if (!/^\d+$/.test(id || '') || button.disabled) return;
    const data = new FormData(); data.set('vod_id', id); button.disabled = true;
    try {
      const response = await fetch('/suxinvideo/favorite', {method: 'POST', body: data, credentials: 'same-origin'});
      const raw = await response.text(); let result;
      try {result = JSON.parse(raw);} catch (_) {throw new Error(raw.slice(0, 150) || '收藏失败');}
      if (!response.ok || !result.ok) throw new Error(result.message || result.msg || '请先登录后收藏');
      button.setAttribute('aria-pressed', String(result.fav));
      toast(result.fav ? '已加入收藏' : '已取消收藏');
    } catch (error) {toast(error.message, false);} finally {button.disabled = false;}
  }));

  const episodeJump = q('#guoguoEpisodeJump');
  episodeJump?.addEventListener('submit', (event) => {
    event.preventDefault();
    const value = Number(episodeJump.elements.episode?.value);
    const buttons = qa('#epGrid .ep-btn');
    if (!Number.isInteger(value) || value < 1 || value > buttons.length) {
      toast(buttons.length ? '请输入 1 至 ' + buttons.length + ' 之间的集数' : '当前线路没有可用分集', false);
      return;
    }
    const target = buttons[value - 1];
    if (target.disabled) {toast('当前分集暂不可播放，请切换线路', false); return;}
    target.click();
    target.scrollIntoView({block: 'nearest', behavior: reducedMotion ? 'instant' : 'smooth'});
  });

  if (window.matchMedia('(hover: hover) and (pointer: fine)').matches) {
    qa('.card').forEach((card) => {
      const cover = card.querySelector('.movie-poster img');
      const title = card.querySelector('.movie-card-title');
      // Ranking wrappers also carry .card but their preview belongs to the
      // inner article. Only its own article should set preview alignment.
      if (card.querySelector('.movie-card-preview') && !card.querySelector(':scope > .movie-card-preview')) return;
      const alignPreview = () => {
        const box = card.getBoundingClientRect();
        const preview = card.querySelector(':scope > .movie-card-preview');
        const width = preview?.offsetWidth || Math.min(360, box.width * 2);
        if (box.left + box.width / 2 - width / 2 < 12) card.dataset.previewEdge = 'left';
        else if (box.left + box.width / 2 + width / 2 > innerWidth - 12) card.dataset.previewEdge = 'right';
        else delete card.dataset.previewEdge;
      };
      card.addEventListener('mouseenter', alignPreview);
      card.addEventListener('focusin', alignPreview);
      if (!cover || !title || card.querySelector('.movie-card-preview')) return;
      const preview = document.createElement('div'); preview.className = 'movie-card-preview'; preview.setAttribute('aria-hidden', 'true');
      const picture = document.createElement('div'); picture.className = 'preview-cover';
      const image = document.createElement('img'); image.src = cover.currentSrc || cover.src; image.alt = ''; image.loading = 'lazy'; picture.appendChild(image);
      const info = document.createElement('div'); info.className = 'preview-info';
      const label = document.createElement('div'); label.className = 'preview-title'; label.textContent = title.textContent;
      info.appendChild(label);
      const meta = card.querySelector('.movie-card-meta');
      if (meta) {const text = document.createElement('div'); text.className = 'preview-meta'; text.textContent = meta.textContent; info.appendChild(text);}
      if (card.dataset.description) {const description = document.createElement('p'); description.className = 'preview-desc'; description.textContent = card.dataset.description; info.appendChild(description);}
      preview.append(picture, info); card.appendChild(preview);
    });
  }

  const input = q('.nav-search-input');
  const searchForm = input?.closest('form');
  if (input && searchForm) {
    const panel = document.createElement('div'); panel.className = 'guoguo-suggestions'; panel.hidden = true; searchForm.appendChild(panel);
    let timer = null, controller = null;
    input.addEventListener('input', () => {
      clearTimeout(timer); controller?.abort(); const word = input.value.trim();
      if (!word) {panel.hidden = true; panel.replaceChildren(); return;}
      timer = setTimeout(async () => {
        const request = new AbortController(); controller = request;
        try {
          const response = await fetch('/suxinvideo/suggest?wd=' + encodeURIComponent(word), {signal: request.signal, credentials: 'same-origin'});
          const result = await response.json();
          if (request.signal.aborted || input.value.trim() !== word || !response.ok) return;
          const records = Array.isArray(result.data) ? result.data.slice(0, 8) : [];
          panel.replaceChildren();
          records.forEach((item) => {
            if (!/^\d+$/.test(String(item.id))) return;
            const link = document.createElement('a'); link.href = '/suxinvideo/detail?id=' + encodeURIComponent(item.id); link.textContent = String(item.name || '未命名影片'); panel.appendChild(link);
          });
          panel.hidden = !panel.childElementCount;
        } catch (_) {if (!request.signal.aborted) panel.hidden = true;}
      }, 250);
    });
    document.addEventListener('click', (event) => {if (!searchForm.contains(event.target)) panel.hidden = true;});
    searchForm.addEventListener('submit', () => {panel.hidden = true;});
    window.addEventListener('pagehide', () => {clearTimeout(timer); controller?.abort();});
  }

  document.addEventListener('error', (event) => {
    const image = event.target;
    if (image instanceof HTMLImageElement && !image.dataset.guoguoFallback && !image.src.includes('nopic.svg')) {
      image.dataset.guoguoFallback = '1'; image.src = imageFallback;
      image.closest('.reference-hero__slide,.xiaoqi-hero')?.classList.add('image-unavailable');
    }
  }, true);
  qa('[data-scroll-top]').forEach((button) => button.addEventListener('click', () => window.scrollTo({top: 0, behavior: reducedMotion ? 'instant' : 'smooth'})));
})();
