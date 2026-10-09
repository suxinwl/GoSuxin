/* Embedded home carousel: centered 16:9 slides with adjacent artwork visible. */
const ENTITIES = {amp: '&', lt: '<', gt: '>', quot: '"', apos: "'", nbsp: ' ', ensp: ' ', emsp: ' ', hellip: '…', mdash: '—', ndash: '–', middot: '·', bull: '•', copy: '©', reg: '®', trade: '™', laquo: '«', raquo: '»', lsquo: '‘', rsquo: '’', ldquo: '“', rdquo: '”'};

export function heroPlainText(value) {
  let text = String(value ?? '');
  for (let pass = 0; pass < 3; pass++) text = text.replace(/&(#x[\da-f]+|#\d+|[a-z]+);/gi, (entity, name) => {
    if (name[0] !== '#') return ENTITIES[name.toLowerCase()] ?? entity;
    const number = /^#x/i.test(name) ? parseInt(name.slice(2), 16) : parseInt(name.slice(1), 10);
    return number > 0 && number <= 0x10ffff && !(number >= 0xd800 && number <= 0xdfff) ? String.fromCodePoint(number) : '';
  });
  return text.replace(/<(script|style)\b[^>]*>[\s\S]*?<\/\1\s*>/gi, ' ').replace(/<(?:br\b[^>]*|\/(?:p|div|li|h[1-6])\s*)>/gi, ' ').replace(/<[^>]*>/g, '').replace(/\s+/g, ' ').trim();
}

function artwork(value) { const text = typeof value === 'string' ? value.trim() : ''; return /^(https?:\/\/|\/[^/])/i.test(text) ? text : ''; }

export function selectHeroItems(dramas, limit = 6) {
  const items = new Map();
  for (const drama of Array.isArray(dramas) ? dramas : []) {
    if (!drama?.id) continue;
    const portrait = artwork(drama.cover || drama.coverUrl || drama.cover_url || drama.poster || drama.image);
    const backdrop = artwork(drama.backdrop || drama.backdropUrl || drama.backdrop_url || drama.bannerUrl || drama.landscapeCover);
    if (!portrait && !backdrop) continue;
    const score = Number.parseFloat(drama.rating || drama.score);
    const id = String(drama.id);
    items.set(id, {id, title: heroPlainText(drama.title || drama.name || '未命名影片'), description: heroPlainText(drama.description || drama.plot || drama.desc || drama.intro || ''), image: backdrop || portrait, landscape: Boolean(backdrop), meta: [heroPlainText(drama.year || drama.releaseYear || ''), heroPlainText(drama.categoryPath?.[0] || drama.mediaType || drama.categoryName || ''), Number.isFinite(score) && score > 0 ? score.toFixed(1) + ' 分' : ''].filter(Boolean).join(' · ')});
  }
  return [...items.values()].sort((left, right) => left.id < right.id ? -1 : left.id > right.id ? 1 : 0).slice(0, limit);
}

export function createHeroBanner(app, environment = {}) {
  const doc = environment.document || document, win = environment.window || window, ImageClass = environment.Image || win.Image;
  const later = environment.setTimeout || win.setTimeout.bind(win), cancel = environment.clearTimeout || win.clearTimeout.bind(win);
  const banner = doc.getElementById('heroBanner4kvm');
  if (!banner) return {render() {}, destroy() {}};
  banner.heroController?.destroy();
  let items = [], fingerprint = '', active = null, activeIndex = -1, physicalIndex = 0, timer = null, transitionTimer = null, pending = false, generation = 0, destroyed = false, inView = !win.IntersectionObserver, hovered = false, focused = false;
  const images = new Map(), reducedMotion = win.matchMedia?.('(prefers-reduced-motion: reduce)');
  const node = (tag, className, text = '') => {const result = doc.createElement(tag); result.className = className; result.textContent = text; return result;};
  const root = node('section', 'reference-hero'); root.setAttribute('aria-label', '精选影视'); root.setAttribute('aria-roledescription', '轮播');
  const viewport = node('div', 'reference-hero__viewport'), track = node('div', 'reference-hero__track');
  const previous = node('button', 'reference-hero__arrow reference-hero__arrow--previous', '‹'), next = node('button', 'reference-hero__arrow reference-hero__arrow--next', '›'), pagination = node('div', 'reference-hero__pagination');
  previous.type = next.type = 'button'; previous.dataset.heroAction = 'previous'; next.dataset.heroAction = 'next'; previous.setAttribute('aria-label', '上一部精选'); next.setAttribute('aria-label', '下一部精选'); pagination.setAttribute('aria-label', '选择精选影片');
  viewport.append(track); root.append(viewport, previous, next, pagination); banner.replaceChildren(root);

  function stopTimer() {if (timer !== null) cancel(timer); timer = null;}
  function visible() {return !destroyed && inView && !doc.hidden && !banner.hidden && !banner.closest('[hidden]');}
  function schedule() {stopTimer(); if (!visible() || hovered || focused || pending || !active || items.length < 2 || reducedMotion?.matches) return; timer = later(() => {timer = null; void advance(1, true);}, 15000);}
  function preload(item) {
    if (images.has(item.image)) return images.get(item.image);
    const promise = new Promise(resolve => {const image = new ImageClass(); image.decoding = 'async'; let settled = false; const finish = result => {if (settled) return; settled = true; image.onload = null; image.onerror = null; resolve(result);}; image.onerror = () => finish(null); image.onload = async () => {if (!image.naturalWidth || !image.naturalHeight) {finish(null); return;} try {if (image.decode) await image.decode();} catch (_) {} finish(image);}; image.src = item.image;});
    images.set(item.image, promise); return promise;
  }
  function position(animate = false) {
    const slide = track.children[physicalIndex];
    if (!slide) return;
    track.style.transitionDuration = animate && !reducedMotion?.matches ? '500ms' : '0ms';
    const left = viewport.clientWidth / 2 - slide.offsetLeft - slide.offsetWidth / 2;
    track.style.transform = 'translate3d(' + left + 'px,0,0)';
  }
  function updateSlides() {
    [...track.children].forEach((slide, index) => {
      const selected = index === physicalIndex;
      slide.classList.toggle('is-active', selected);
      slide.setAttribute('aria-hidden', String(!selected));
      const link = slide.querySelector('a'); if (link) link.tabIndex = selected ? 0 : -1;
    });
    [...pagination.children].forEach((dot, index) => {dot.classList.toggle('is-active', index === activeIndex); dot.setAttribute('aria-pressed', String(index === activeIndex));});
  }
  function normalizePosition() {
    if (transitionTimer !== null) cancel(transitionTimer); transitionTimer = null;
    physicalIndex = items.length > 1 ? activeIndex + 1 : 0;
    updateSlides(); position(false);
  }
  function buildSlides() {
    track.replaceChildren(); pagination.replaceChildren();
    const copies = items.length > 1 ? [items[items.length - 1], ...items, items[0]] : items;
    copies.forEach((item, index) => {
      const slide = node('article', 'reference-hero__slide'); slide.setAttribute('aria-roledescription', '幻灯片');
      const actualIndex = items.length > 1 ? (index - 1 + items.length) % items.length : 0;
      slide.setAttribute('aria-label', (actualIndex + 1) + ' / ' + items.length);
      const link = node('a', 'reference-hero__link'); link.href = '/play/' + encodeURIComponent(item.id); link.dataset.heroAction = 'play'; link.dataset.heroId = item.id;
      const image = node('img', 'reference-hero__image'); image.src = item.image; image.alt = item.title; image.decoding = 'async'; image.draggable = false;
      image.addEventListener('error', () => slide.classList.add('image-unavailable'), {once: true});
      const shade = node('div', 'reference-hero__shade'), content = node('div', 'reference-hero__content'), title = node('h2', 'reference-hero__title', item.title), description = node('p', 'reference-hero__description', item.description), meta = node('p', 'reference-hero__meta', item.meta);
      description.hidden = !item.description; meta.hidden = !item.meta;
      content.append(title, description, meta); link.append(image, shade, content); slide.append(link); track.append(slide);
    });
    items.forEach((item, index) => {const dot = node('button', 'reference-hero__dot'); dot.type = 'button'; dot.dataset.heroAction = 'select'; dot.dataset.heroIndex = String(index); dot.setAttribute('aria-label', '播放精选 ' + (index + 1) + '：' + item.title); pagination.append(dot);});
    previous.hidden = next.hidden = pagination.hidden = items.length < 2;
  }
  function show(item, animate = true, direction = 0) {
    const priorIndex = activeIndex; active = item; activeIndex = items.indexOf(item); root.dataset.workId = item.id;
    physicalIndex = items.length > 1 ? activeIndex + 1 : 0;
    if (animate && direction === 1 && priorIndex === items.length - 1 && activeIndex === 0) physicalIndex = items.length + 1;
    if (animate && direction === -1 && priorIndex === 0 && activeIndex === items.length - 1) physicalIndex = 0;
    updateSlides(); position(animate);
    if (transitionTimer !== null) cancel(transitionTimer);
    transitionTimer = later(normalizePosition, reducedMotion?.matches ? 0 : 520);
  }
  async function advance(direction = 1, automatic = false, preferred = '') {
    if (destroyed || !items.length || pending) return; stopTimer(); pending = true; normalizePosition(); const version = generation, currentIndex = items.findIndex(item => item.id === active?.id), preferredIndex = items.findIndex(item => item.id === preferred), start = preferredIndex >= 0 ? preferredIndex : currentIndex < 0 ? 0 : (currentIndex + direction + items.length) % items.length;
    for (let offset = 0; offset < items.length; offset++) {const item = items[(start + direction * offset + items.length) % items.length], image = await preload(item); if (destroyed || version !== generation) return; if (automatic && !visible()) break; if (!image) continue; show(item, currentIndex >= 0, direction); break;}
    pending = false; if (!active && items.length) show(items[start], false); schedule();
  }
  const onClick = event => {const button = event.target.closest('[data-hero-action]'); if (!button || !root.contains(button)) return; event.preventDefault(); if (button.dataset.heroAction === 'play') {const item = items.find(item => item.id === button.dataset.heroId); if (item) app.play?.(item.id, item.title);} else if (button.dataset.heroAction === 'select') {const item = items[Number(button.dataset.heroIndex)]; if (item) void advance(1, false, item.id);} else void advance(button.dataset.heroAction === 'previous' ? -1 : 1);};
  const onKey = event => {if (event.key !== 'ArrowLeft' && event.key !== 'ArrowRight') return; event.preventDefault(); void advance(event.key === 'ArrowLeft' ? -1 : 1);};
  const onEnter = () => {hovered = true; stopTimer();}, onLeave = () => {hovered = false; schedule();}, onFocus = () => {focused = true; stopTimer();}, onBlur = event => {focused = root.contains(event.relatedTarget); schedule();}, onVisibility = () => schedule();
  const onResize = () => position(false);
  root.addEventListener('click', onClick); root.addEventListener('keydown', onKey); root.addEventListener('pointerenter', onEnter); root.addEventListener('pointerleave', onLeave); root.addEventListener('focusin', onFocus); root.addEventListener('focusout', onBlur); doc.addEventListener('visibilitychange', onVisibility); reducedMotion?.addEventListener?.('change', onVisibility); win.addEventListener?.('resize', onResize);
  const resize = win.ResizeObserver ? new win.ResizeObserver(onResize) : null; resize?.observe(viewport);
  const intersection = win.IntersectionObserver ? new win.IntersectionObserver(entries => {inView = entries.some(entry => entry.isIntersecting && entry.intersectionRatio > 0); schedule();}, {threshold: 0.05}) : null; intersection?.observe(banner);
  const mutations = win.MutationObserver ? new win.MutationObserver(onVisibility) : null; mutations?.observe(banner, {attributes: true, attributeFilter: ['hidden']});
  function render(dramas) {if (destroyed) return; if (!root.isConnected) banner.replaceChildren(root); const selected = selectHeroItems(dramas), nextFingerprint = JSON.stringify(selected); if (nextFingerprint === fingerprint) {root.hidden = !items.length; position(false); schedule(); return;} const preferred = active?.id; fingerprint = nextFingerprint; items = selected; generation++; pending = false; stopTimer(); if (transitionTimer !== null) cancel(transitionTimer); transitionTimer = null; active = null; activeIndex = -1; root.hidden = !items.length; buildSlides(); if (!items.length) return; activeIndex = Math.max(0, items.findIndex(item => item.id === preferred)); physicalIndex = items.length > 1 ? activeIndex + 1 : 0; updateSlides(); position(false); return advance(1, false, preferred || items[0].id);}
  function destroy() {if (destroyed) return; destroyed = true; generation++; stopTimer(); if (transitionTimer !== null) cancel(transitionTimer); intersection?.disconnect(); mutations?.disconnect(); resize?.disconnect(); root.removeEventListener('click', onClick); root.removeEventListener('keydown', onKey); root.removeEventListener('pointerenter', onEnter); root.removeEventListener('pointerleave', onLeave); root.removeEventListener('focusin', onFocus); root.removeEventListener('focusout', onBlur); doc.removeEventListener('visibilitychange', onVisibility); reducedMotion?.removeEventListener?.('change', onVisibility); win.removeEventListener?.('resize', onResize); images.clear(); if (banner.heroController === controller) delete banner.heroController;}
  const controller = {render, destroy}; banner.heroController = controller; return controller;
}
