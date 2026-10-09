import { createCardViewport } from './card-viewport.js';
import { createCoverRepair } from './cover-repair.js';
import { createDramaRefresh } from './drama-refresh.js';
import { createRecommendations } from './recommendations.js';
import { icon, initial, withFocus, readPreference, savePreference, matchesSource, matchesChannel } from './ui-core.js';
import { $, element, empty, button, setMessage, api, post, valueText, firstNonEmpty, dramaTitle, dramaOperationID, watchLabel, historySuffix, normalizeSource, sourceKey, sourceLabel, categoryName, episodeCount, coverURL, tagsText, dramaSearchText, rebuildOptions, number, formatBytes, formatTime, statusText, releaseText, phaseText, progressText, progressBar, episodeLabel, groupStats } from './ui-core.js';

export function createLibrary(app) {
  const dramas = [], selected = new Set(), byID = new Map(), cards = $('cards');
  let visibleIDs = [], libraryRevision = 0, libraryRequest = false, libraryReload = false;
  let catalogGeneration = 0, catalogLoading = false, catalogLoaded = 0, catalogTotal = 0, catalogError = '';
  let libraryUpdateRequest = false, libraryQueuedUpdate = false;
  let libraryTimer, libraryBusyText = '', libraryIsLoading = false, libraryMetadataRemaining = {};
  let sortMessage = '', onlineSearchMessage = '', onlineSearchQuery = '', onlineSearchIDs = new Set();
  let searchController, searchSequence = 0, batch = false, filterTimer;
  let pendingCategory = null, sourceWarning = '', readError = false;
  const recommendations = createRecommendations({allowed: () => app.viewer?.sources?.includes('hongguo') !== false && (!app.viewer?.account || app.viewer.account.admin || app.viewer?.contentPreference === 'short_drama'), merge: mergeRecommendations, changed: renderDramas, switched: () => {resetOnlineSearch(); toggleBatch(false); app.shell.resetScroll();}});
  const viewport = createCardViewport({container: cards, scroller: $('workspaceMain'), create: renderCard, key: dramaCardKey, rendered: refreshFollowing, update: (card, drama) => {
    card.classList.toggle('selected', selected.has(drama.id));
    card.querySelector('input[type=checkbox]').checked = selected.has(drama.id);
  }});
  const searchText = new WeakMap();
  const coverRepair = createCoverRepair({get: id => byID.get(id), post, apply: (id, address, previous) => {
    const drama = byID.get(id);
    if (!drama) return;
    drama.cover = address;
    drama.coverUrl = address;
    if (address === previous) {
      for (const card of cards.querySelectorAll('.card')) {if (card.dataset.dramaId === id) card.retryCover?.();}
    } else viewport.refresh();
    app.details.refreshCover(id, address === previous);
    app.following.refreshCover(id);
  }});
  const dramaRefresh = createDramaRefresh({post, apply: applyDramaRefresh, failure: (id) => {
    app.details.metadataStatus(id, '资料暂未更新，已保留原有信息；稍后重新打开可重试。');
    coverRepair.touch(id);
  }});
  function reconcileDrama(drama) {return coverRepair.reconcile(dramaRefresh.reconcile(drama));}
  function applyDramaRefresh(drama, warning) {
    const index = dramas.findIndex(item => item.id === drama.id || dramaOperationID(item) === drama.id);
    const previous = index >= 0 ? dramas[index] : null;
    if (previous?.sources?.length) drama = Object.assign({}, previous, drama, {id: previous.id, sources: previous.sources, primaryDramaId: previous.primaryDramaId, sourceCount: previous.sourceCount, primarySource: previous.primarySource});
    if (index >= 0) dramas[index] = drama; else dramas.push(drama);
    byID.set(drama.id, drama);
    libraryRevision = 0;
    coverRepair.updated(drama.id);
    app.details.metadataStatus(drama.id, warning);
    rebuildChannels(false);
    renderDramas();
    app.following.render();
    app.details.refresh();
    if (previous && coverURL(previous) === coverURL(drama)) {
      for (const card of cards.querySelectorAll('.card')) {if (card.dataset.dramaId === drama.id) card.retryCover?.();}
      app.details.refreshCover(drama.id, true);
      app.following.refreshCover(drama.id);
    }
    window.dispatchEvent(new Event('xiaoqilibrarychange'));
  }
function refreshDrama(id) {
    const drama = byID.get(id);
    const operationID = dramaOperationID(drama) || id;
    if (!['hongguo'].includes(String(operationID).split(':')[0])) return;
    void dramaRefresh.refresh(operationID);
  }
  const renderTasks = () => app.downloads.render();
  const pollTasks = () => app.downloads.refresh();
function searchableText(drama) {if (!searchText.has(drama)) searchText.set(drama, dramaSearchText(drama)); return searchText.get(drama);}
function filteredDramas(){ const keyword=$('searchInput').value.trim().toLowerCase();const source=$('sourceSelect').value;const channel=$('channelSelect').value;return dramas.filter(dr=>{if(!matchesSource(dr,source)||!matchesChannel(dr,channel))return false;return !keyword||searchableText(dr).includes(keyword)||onlineSearchQuery===keyword&&onlineSearchIDs.has(dr.id);}); }

function rebuildSources(){ const sources=[...new Set((app.viewer?.sources || ['hongguo']).map(normalizeSource).filter(Boolean))];rebuildOptions($('sourceSelect'),sources,'全部站源',sourceLabel,false); }

function rebuildChannels(reset){
  const source=$('sourceSelect').value;
  const top=['电影','电视剧','动漫','儿童动画','综艺','纪录片','体育','短剧','其他'];
  const sourceItems = dramas.filter(dr => matchesSource(dr, source));
  const values = Array.from(new Set(top.concat(sourceItems.flatMap(dr => dr.categoryPath || [categoryName(dr)]), pendingCategory ? [pendingCategory] : [])))
    .filter(Boolean)
    .sort((left, right) => left.localeCompare(right, 'zh-Hans-CN'));
  rebuildOptions($('channelSelect'),values,'全部分类',value=>value,reset);
  if (pendingCategory && values.includes(pendingCategory) && !reset) $('channelSelect').value=pendingCategory;
  updateRefreshLabel();
}

function updateLibraryButton() {
  const source = recommendations.enabled ? 'hongguo' : $('sourceSelect').value, hongguo = !source || source === 'hongguo';
  const button = $('refreshBtn'), label = $('refreshBtnLabel');
  const busy = libraryUpdateRequest || libraryIsLoading;
  const text = busy ? '更新中…' : '更新剧库';
  if (label.textContent !== text) label.textContent = text;
  if (button.disabled !== busy) button.disabled = busy;
  if (button.getAttribute('aria-busy') !== String(busy)) button.setAttribute('aria-busy', String(busy));
  button.setAttribute('aria-label', (busy ? '正在更新' : '更新') + (source ? sourceLabel(source) : '全部站源') + '剧库');
  button.title = '检查新内容' + (hongguo ? '并继续加载历史目录' : '') + '，后台分批补充缺失资料；优先当前筛选结果' + (libraryMetadataRemaining[source] ? '，待检查 ' + libraryMetadataRemaining[source] + ' 部' : '');
}

function metadataPriorityIDs(){const pending=new Set(dramas.filter(drama=>!drama.sortMetadata||drama.sortMetadata.version!==1||!coverURL(drama)&&!drama.sortMetadata.coverChecked).map(drama=>dramaOperationID(drama)));return visibleIDs.map(id=>dramaOperationID(byID.get(id))).filter(id=>pending.has(id)).slice(0,40);}

function updateRefreshLabel(){ const source=$('sourceSelect').value;updateLibraryButton();$('onlineSearchBtn').hidden=false;$('searchInput').placeholder='搜索剧名、简介或标签';$('onlineSearchBtn').title='联网搜索' + (source ? sourceLabel(source) : '全部站源') + '，也可按回车'; }

function placeholder(text){ return element('div','cover placeholder',text||'暂无封面'); }

function dramaMetaText(drama) {
  const history = window.XiaoqiHistory.progressText(dramaOperationID(drama) || drama.id);
  const count = episodeCount(drama);
  const status = drama.releaseStatus === 'finished' ? '已完结' : drama.releaseStatus === 'ongoing' ? '连载中' : '';
  return [history || (count ? count + ' 集' : '集数未知'), history ? '' : status].filter(Boolean).join(' · ');
}

function dramaCardKey(drama) {
  return JSON.stringify([dramaTitle(drama), sourceKey(drama), categoryName(drama), episodeCount(drama), coverURL(drama), drama.releaseStatus]);
}

function renderCard(drama) {
  const title  = dramaTitle(drama);
  const cover  = coverURL(drama);
  const cat    = categoryName(drama);
  const src    = sourceKey(drama);
  const srcLbl = sourceLabel(src);
  const sourceCount = Number(drama.sourceCount || drama.sources?.length || 0);
  const meta   = dramaMetaText(drama);
  const desc   = drama.description || drama.plot || '';
  const rating = drama.rating ? parseFloat(drama.rating).toFixed(1) : '';
  const year   = drama.year || drama.releaseYear || (drama.airedDate ? drama.airedDate.slice(0,4) : '');
  const hasProgress = Boolean(window.XiaoqiHistory?.get(dramaOperationID(drama) || drama.id));

  /* 外层 article */
  const card = element('article', 'card movie-card-4kvm' + (selected.has(drama.id) ? ' selected' : ''));
  card.dataset.dramaId = drama.id;
  card.renderKey = dramaCardKey(drama);

  /* 批量勾选（原有功能，视觉隐藏） */
  const label = element('label', 'card-select');
  label.dataset.downloadOnly = '';
  const checkbox = element('input');
  checkbox.type = 'checkbox';
  checkbox.checked = selected.has(drama.id);
  checkbox.dataset.focusKey = 'select-' + drama.id;
  checkbox.setAttribute('aria-label', '选择 ' + title);
  checkbox.addEventListener('change', () => {
    if (checkbox.checked) selected.add(drama.id); else selected.delete(drama.id);
    card.classList.toggle('selected', checkbox.checked);
    updateDramaSelection();
  });
  label.appendChild(checkbox);

  /* ── 默认卡片视图（竖向海报） ── */
  const defaultView = element('div', 'movie-card-default');

  /* 海报容器 */
  const poster = button('', () => app.play(drama.id, title), false,
    'poster watch-button' + (hasProgress ? ' has-progress' : ''));
  poster.setAttribute('aria-label', (hasProgress ? '继续观看 ' : '播放 ') + title);
  poster.dataset.focusKey = 'play-' + drama.id;

  const posterInner = element('div', 'movie-poster');
  /* 封面图 */
  if (cover) {
    const image = element('img', 'cover');
    image.alt = '';
    image.loading = 'lazy';
    image.decoding = 'async';
    const fallback = element('span', 'cover placeholder');
    fallback.setAttribute('aria-hidden', 'true');
    fallback.append(element('span', '', initial(title)), element('small', '', '海报加载失败'));
    image.addEventListener('error', () => { image.replaceWith(fallback); coverRepair.failed(drama.id, cover); });
    image.addEventListener('load', () => coverRepair.loaded(drama.id, cover));
    image.src = cover;
    card.retryCover = () => { if (fallback.isConnected) { image.src = cover; fallback.replaceWith(image); } };
    posterInner.appendChild(image);
  } else {
    const fallback = element('span', 'cover placeholder');
    fallback.setAttribute('aria-hidden', 'true');
    fallback.append(element('span', '', initial(title)), element('small', '', '暂无海报'));
    posterInner.appendChild(fallback);
  }

  /* 4K 角标 */
  const badge4k = element('span', 'badge-4k');
  badge4k.textContent = sourceCount > 1 ? (srcLbl || '红果') + ' · ' + sourceCount + '源' : (srcLbl || '4K');
  badge4k.setAttribute('aria-hidden', 'true');
  posterInner.appendChild(badge4k);

  /* 底部集数 / 观看进度浮层 */
  const epBadge = element('span', 'episode-badge');
  epBadge.setAttribute('aria-hidden', 'true');
  epBadge.textContent = meta;
  posterInner.appendChild(epBadge);

  /* 进度条（保留原有） */
  const progress = element('span', 'poster-progress');
  progress.setAttribute('aria-hidden', 'true');
  progress.appendChild(element('span'));
  progress.hidden = !hasProgress;
  posterInner.appendChild(progress);

  poster.appendChild(posterInner);
  defaultView.appendChild(poster);

  /* 卡片标题（封面下方） */
  const cardTitle = element('h3', 'movie-card-title kvm-card-title');
  cardTitle.textContent = title;
  cardTitle.setAttribute('aria-hidden', 'true');
  defaultView.appendChild(cardTitle);

  /* ── 悬停快速预览卡（桌面 md+ 才显示） ── */
  const preview = element('div', 'movie-card-preview');
  preview.setAttribute('aria-hidden', 'true');

  /* 预览封面区 (16:9) */
  const previewCover = element('a', 'preview-cover');
  previewCover.href = 'javascript:void(0)';
  previewCover.setAttribute('tabindex', '-1');
  previewCover.addEventListener('click', () => app.play(drama.id, title));

  if (cover) {
    const previewImg = element('img');
    previewImg.src = cover;
    previewImg.alt = '';
    previewImg.loading = 'lazy';
    previewCover.appendChild(previewImg);
  }

  /* 播放按钮 */
  const playWrap = element('div', 'preview-play-btn-wrap');
  const playBtn  = element('button', 'preview-play-btn');
  playBtn.type   = 'button';
  // 播放三角 SVG
  const playSvg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
  playSvg.setAttribute('viewBox', '0 0 24 24');
  playSvg.setAttribute('fill', 'currentColor');
  playSvg.setAttribute('aria-hidden', 'true');
  const playPath = document.createElementNS('http://www.w3.org/2000/svg', 'path');
  playPath.setAttribute('d', 'M8 5v14l11-7z');
  playSvg.appendChild(playPath);
  playBtn.appendChild(playSvg);
  playBtn.addEventListener('click', () => app.play(drama.id, title));
  playWrap.appendChild(playBtn);
  previewCover.appendChild(playWrap);

  /* 评分角标 */
  if (rating) {
    const ratingBadge = element('div', 'preview-rating-badge');
    const starSvg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
    starSvg.setAttribute('viewBox', '0 0 24 24');
    starSvg.setAttribute('fill', 'currentColor');
    starSvg.setAttribute('aria-hidden', 'true');
    const starPath = document.createElementNS('http://www.w3.org/2000/svg', 'path');
    starPath.setAttribute('d', 'M12 17.27L18.18 21l-1.64-7.03L22 9.24l-7.19-.61L12 2 9.19 8.63 2 9.24l5.46 4.73L5.82 21z');
    starSvg.appendChild(starPath);
    ratingBadge.append(starSvg, rating);
    previewCover.appendChild(ratingBadge);
  }

  preview.appendChild(previewCover);

  /* 预览信息区 */
  const previewInfo = element('div', 'preview-info');

  /* 标题 */
  const previewTitle = element('a', 'preview-title');
  previewTitle.textContent = title;
  previewTitle.href = 'javascript:void(0)';
  previewTitle.addEventListener('click', () => app.details.open(drama.id));
  previewInfo.appendChild(previewTitle);

  /* meta 行 */
  const previewMeta = element('div', 'preview-meta');
  if (rating) {
    const scoreSpan = element('span', 'score');
    scoreSpan.textContent = rating;
    previewMeta.appendChild(scoreSpan);
  }
  if (year) {
    const yearSpan = element('span', 'year');
    yearSpan.textContent = year;
    previewMeta.appendChild(yearSpan);
  }
  const qualityTag = element('span', 'quality-tag');
  qualityTag.textContent = srcLbl || '4K';
  previewMeta.appendChild(qualityTag);
  if (meta) {
    const epInfo = element('span', 'episode-info');
    epInfo.textContent = meta;
    previewMeta.appendChild(epInfo);
  }
  previewInfo.appendChild(previewMeta);

  /* 简介 */
  if (desc) {
    const descEl = element('p', 'preview-desc');
    descEl.textContent = desc;
    previewInfo.appendChild(descEl);
  }

  /* 动作按钮 */
  const previewActions = element('div', 'preview-actions');

  /* 收藏 */
  const favBtn = app.following.saveButton(drama.id, title);
  favBtn.className = 'preview-action-btn';
  favBtn.title = '收藏';
  previewActions.appendChild(favBtn);

  /* 下载 */
  if (!app.viewer?.onlineOnly) {
    const dlBtn = element('button', 'preview-action-btn ml-auto');
    dlBtn.type = 'button';
    dlBtn.title = '下载';
    dlBtn.setAttribute('aria-label', '下载 ' + title);
    dlBtn.appendChild(icon('download'));
    dlBtn.addEventListener('click', async () => {
      if (dlBtn.disabled) return;
      dlBtn.disabled = true;
      try { await app.downloads.enqueue([dramaOperationID(drama)]); } finally { dlBtn.disabled = false; }
    });
    previewActions.appendChild(dlBtn);
  }

  previewInfo.appendChild(previewActions);
  preview.appendChild(previewInfo);

  /* 组装 */
  card.append(label, defaultView, preview);
  return card;
}

function updateDramaSelection() {
  $('dramaSelection').textContent = '已选 ' + selected.size + ' 部';
  $('enqueueBtn').disabled = !selected.size;
}

function renderDramas() {
  clearTimeout(filterTimer);
  const filtered = filteredDramas();
  const mode = $('sortSelect').value;
  const list = recommendations.enabled ? recommendations.items().map(drama => byID.get(drama.id) || drama) : window.XiaoqiLibrarySort.sortDramas(filtered, mode);
  sortMessage = recommendations.enabled ? '' : window.XiaoqiLibrarySort.summary(filtered, mode);
  $('sortSelect').title = '缺少数据的剧集排在后面。' + sortMessage;
  visibleIDs = list.map(drama => drama.id);
  $('dramaCount').textContent = recommendations.enabled ? list.length + ' 部' : list.length === dramas.length ? list.length + ' 部' : list.length + ' / ' + dramas.length + ' 部';
  updateDramaSelection();
  updateLibraryStatus();
  refreshFilterSummary();
  let state = null;
  if (!list.length && recommendations.enabled) {
    state = element('div', 'empty', '红果分类推荐会显示在这里。');
  } else if (!list.length) {
    state = element('div', 'empty');
    state.append(element('strong', '', dramas.length ? '没有找到匹配的剧集' : '剧库还没有内容'), element('p', '', dramas.length ? '换个关键词，或清除当前筛选。' : '更新剧库即可获取可用站源的内容。'));
    state.appendChild(button(dramas.length ? '清除筛选' : '更新剧库', () => dramas.length ? resetFilters() : loadDramas(true), false, 'secondary'));
  }
  viewport.setItems(list, state);
  window.dispatchEvent(new Event('xiaoqilibraryviewchange'));
}

function mergeWorks(items, replace = false) {
  if (replace) { dramas.length = 0; byID.clear(); }
  for (const raw of Array.isArray(items) ? items : []) {
    const drama = reconcileDrama(raw);
    if (!drama || (!sourceKey(drama) && !drama.sources?.length)) continue;
    const previous = byID.get(drama.id);
    if (previous) {
      const index = dramas.indexOf(previous);
      if (index >= 0) dramas[index] = Object.assign({}, previous, drama);
    } else dramas.push(drama);
    byID.set(drama.id, drama);
  }
}

async function loadRemainingWorks(total, revision, generation) {
  let offset = Math.max(0, catalogLoaded);
  try {
    while (offset < total && generation === catalogGeneration) {
      const result = await api('/api/ui/works?offset=' + offset + '&limit=500&revision=' + encodeURIComponent(String(revision || 0)));
      if (generation !== catalogGeneration) return;
      if (revision && result.revision && result.revision !== revision) {catalogError = '剧库在分页加载期间发生变化，已停止继续追加；请重试'; readError = true; break;}
      const rows = Array.isArray(result.data) ? result.data : [];
      if (!rows.length) break;
      mergeWorks(rows);
      offset += rows.length;
      catalogLoaded = offset;
      if (rows.length < 500) break;
      await new Promise(resolve => setTimeout(resolve, 0));
    }
  } catch (error) {
    if (generation === catalogGeneration) {catalogError = '剧库分页读取失败：' + error.message; readError = true;}
  } finally {
    if (generation === catalogGeneration) {
      catalogLoading = false;
      renderDramas();
      app.following.render();
      app.details.refresh();
      window.dispatchEvent(new Event('xiaoqilibrarychange'));
      if (catalogError) libraryBusyText = catalogError;
      updateLibraryStatus();
      updateLibraryButton();
    }
  }
}

async function loadDramas(update) {
  if (catalogLoading && !update) return;
  if (libraryRequest) {
    libraryReload = true;
    if (update) {libraryQueuedUpdate = true; libraryUpdateRequest = true; updateLibraryButton();}
    return;
  }
  libraryRequest = true;
  const generation = ++catalogGeneration;
  catalogError = '';
  catalogLoading = false;
  catalogLoaded = 0;
  catalogTotal = 0;
  libraryUpdateRequest = Boolean(update);
  clearTimeout(libraryTimer);
  updateLibraryButton();
  const source = recommendations.enabled ? 'hongguo' : $('sourceSelect').value;
  const legacyQuery = update ? '?update=1&source=' + encodeURIComponent(source) + '&priority=' + encodeURIComponent(metadataPriorityIDs().join(',')) : '?revision=' + libraryRevision;
  // Load the permitted unified catalog once; the source selector filters this
  // in memory so the home hero and multi-source cards can see every allowed
  // source while red-fruit remains the default selected filter.
  const query = update ? '/api/ui/dramas' + legacyQuery : '/api/ui/works?offset=0&limit=500';
  try {
    let result = await api(query);
    // The unified catalog is the normal read path. An empty catalog can mean
    // that the first provider sync has not been started yet; ask the legacy
    // loader to warm the cache, then render its unified `works` projection.
    if (!update && (!Array.isArray(result.data) || result.total === 0)) {
      result = await api('/api/ui/dramas' + legacyQuery);
    }
    readError = false;
    libraryRevision = result.revision || 0;
    libraryIsLoading = Boolean(result.loading || result.metadata?.running);
    libraryMetadataRemaining = result.metadataRemaining || {};
    app.shell.sourceStates(result.sources);
    sourceWarning = result.error ? '站源更新尚未完成，可在“更多”查看状态' : '';
    if (Array.isArray(result.data)) {
      const incoming = Array.isArray(result.works) && result.works.length ? result.works : result.data;
      mergeWorks(incoming, true);
      catalogLoaded = incoming.length;
      catalogTotal = Number(result.total) || incoming.length;
      catalogLoading = !update && catalogLoaded < catalogTotal;
      for (const drama of recommendations.all()) {if (!byID.has(drama.id)) {dramas.push(drama); byID.set(drama.id, drama);}}
      rebuildSources();
      rebuildChannels(false);
      if (pendingCategory !== null) {
        const found = Array.from($('channelSelect').options).some(option => option.value === pendingCategory);
        if (found) $('channelSelect').value = pendingCategory;
        if (found || !result.loading) pendingCategory = null;
      }
      renderDramas();
      app.following.render();
      app.details.refresh();
      window.dispatchEvent(new Event('xiaoqilibrarychange'));
      if (catalogLoading) void loadRemainingWorks(catalogTotal, libraryRevision, generation);
    }
    const loaded = result.loadedAt && !result.loadedAt.startsWith('0001') ? new Date(result.loadedAt).toLocaleString() : '';
    $('libraryUpdatedAt').textContent = loaded ? '更新于 ' + loaded : '';
    $('libraryUpdatedAt').title = $('libraryUpdatedAt').textContent;
    $('libraryMenuUpdatedAt').textContent = $('libraryUpdatedAt').textContent;
    const metadata = result.metadata || {};
    libraryBusyText = result.loading ? '正在更新剧库，已有内容可继续使用' : metadata.running ? '后台补充资料 ' + metadata.checked + ' / ' + metadata.total : catalogLoading ? '已加载 ' + catalogLoaded + ' / ' + catalogTotal + ' 部，正在继续读取' : catalogError || '';
    if (catalogError) sourceWarning = catalogError;
    updateLibraryStatus();
    if (result.loading || metadata.running) libraryTimer = setTimeout(() => loadDramas(false), result.loading ? 1200 : 5000);
  } catch (error) {
    libraryIsLoading = false;
    readError = true;
    libraryBusyText = '剧库读取失败：' + error.message;
    if (!dramas.length) renderDramas();
    updateLibraryStatus();
  } finally {
    libraryRequest = false;
    if (libraryReload) {
      const queuedUpdate = libraryQueuedUpdate;
      libraryReload = false;
      libraryQueuedUpdate = false;
      loadDramas(queuedUpdate);
    } else {
      libraryUpdateRequest = false;
      updateLibraryButton();
    }
  }
}

function mergeRecommendations(items) {
  const positions = new Map(dramas.map((drama, index) => [drama.id, index]));
  for (let drama of items) {
    drama = reconcileDrama(drama);
    const index = positions.get(drama.id);
    if (index === undefined) {positions.set(drama.id, dramas.length); dramas.push(drama);} else dramas[index] = drama;
    byID.set(drama.id, drama);
  }
  libraryRevision = 0;
  rebuildSources();
  rebuildChannels(false);
  app.following.render();
  app.details.refresh();
}

function updateLibraryStatus() {
  const text = [libraryBusyText, onlineSearchMessage, sourceWarning, sortMessage].filter(Boolean).join(' · ');
  $('libraryStatus').textContent = text;
  $('libraryFeedback').hidden = !text;
  $('retryLibraryBtn').hidden = !readError;
}

function resetOnlineSearch(){searchSequence++;if(searchController)searchController.abort();searchController=null;onlineSearchQuery='';onlineSearchIDs.clear();onlineSearchMessage='';$('onlineSearchBtn').disabled=false;$('onlineSearchBtn').textContent='联网搜索';updateLibraryStatus();}

async function searchOnline(){
    rememberSearch($('searchInput').value);
    const selSource=$('sourceSelect').value;
    if(selSource && app.viewer?.sources?.includes(selSource)===false)return;
    const keyword=$('searchInput').value.trim();if(!keyword){setMessage('请先输入搜索词',true);return;}if(searchController)return;
    resetOnlineSearch();const sequence=searchSequence;const controller=new AbortController();searchController=controller;
    $('onlineSearchBtn').disabled=true;$('onlineSearchBtn').textContent='搜索中';onlineSearchMessage='正在联网搜索'+(selSource?sourceLabel(selSource):'全部站源');updateLibraryStatus();
    try{
      const srcQuery = selSource ? '&source=' + encodeURIComponent(selSource) : '&source=all';
      const result=await api('/api/ui/search?q='+encodeURIComponent(keyword)+srcQuery,{signal:controller.signal});
      if(sequence!==searchSequence||keyword!==$('searchInput').value.trim())return;
      const matches=(Array.isArray(result.data)?result.data:[]).filter(drama=>Boolean(sourceKey(drama)));
      onlineSearchQuery=keyword.toLowerCase();onlineSearchIDs=new Set(matches.map(drama=>drama.id));

      const positions=new Map(dramas.map((drama,index)=>[drama.id,index]));for(let drama of matches){drama=reconcileDrama(drama);const position=positions.get(drama.id);if(position===undefined){positions.set(drama.id,dramas.length);dramas.push(drama);}else dramas[position]=drama;}
      onlineSearchMessage=matches.length?'联网返回 '+matches.length+' 部匹配':'联网暂无匹配';
      if(matches.length&&result.saved===false)onlineSearchMessage+='，缓存未保存';
      for(const drama of dramas)byID.set(drama.id,drama);rebuildSources();rebuildChannels(false);renderDramas();app.following.render();updateLibraryStatus();libraryRevision=0;await loadDramas(false);
    }catch(error){if(sequence===searchSequence&&!controller.signal.aborted){onlineSearchMessage='联网搜索暂不可用：'+error.message+'；可重试，本地筛选仍可使用';updateLibraryStatus();}}
    finally{if(sequence===searchSequence){searchController=null;$('onlineSearchBtn').disabled=false;$('onlineSearchBtn').textContent='联网搜索';}}
  }

async function enqueueSelected() {
  if (!selected.size) {setMessage('请先选择剧集', true); return;}
  $('enqueueBtn').disabled = true;
  const ok = await app.downloads.enqueue(Array.from(selected).map(id => dramaOperationID(byID.get(id)) || id));
  if (ok) toggleBatch(false);
  updateDramaSelection();
}



function refreshFilterSummary() {
  for (const id of ['sourceSelect', 'channelSelect']) $(id).title = $(id).selectedOptions[0]?.textContent || '';
  $('resetFiltersBtn').hidden = recommendations.enabled || !$('searchInput').value && !$('channelSelect').value && $('sortSelect').value === 'default' && ['', 'hongguo'].includes($('sourceSelect').value);
  $('clearSearchBtn').hidden = !$('searchInput').value;
}

function persistFilters() {
  savePreference('libraryFilters', {source: $('sourceSelect').value, category: $('channelSelect').value, sort: $('sortSelect').value, search: $('searchInput').value});
}

function resetFilters() {
  resetOnlineSearch();
  $('sourceSelect').value = '';
  $('searchInput').value = '';
  $('sortSelect').value = 'default';
  pendingCategory = null;
  rebuildChannels(true);
  renderDramas();
  persistFilters();
  app.shell.resetScroll();
}

function setCategory(value) {
  let category = String(value || '').trim();
  try { category = decodeURIComponent(category); } catch (_) {}
  // Retain the explicit source filter across channel navigation. Rebuilding
  // the permitted choices also drops preferences the viewer can no longer use.
  rebuildSources();
  if (recommendations.enabled) $('libraryModeBtn').click();
  pendingCategory = category;
  rebuildChannels(false);
  const select = $('channelSelect');
  let option = Array.from(select.options).find(item => item.value === category || encodeURIComponent(item.value) === value);
  if (!option && category) {
    option = element('option', '', category);
    option.value = category;
    select.appendChild(option);
  }
  if (option) select.value = option.value;
  pendingCategory = category || null;
  persistFilters();
  renderDramas();
  app.shell.resetScroll();
}

function toggleBatch(value) {
  batch = value;
  if (!batch) selected.clear();
  document.body.classList.toggle('library-batch', batch);
  $('libraryBatchBar').hidden = !batch;
  $('batchSelectBtn').setAttribute('aria-pressed', String(batch));
  $('batchSelectLabel').textContent = batch ? '退出批量' : '批量选择';
  $('batchSelectBtn').setAttribute('aria-label', batch ? '退出批量选择' : '批量选择');
  $('batchSelectBtn').title = batch ? '退出批量选择' : '批量选择';
  renderDramas();
}

function refreshFollowing(nodes = document.querySelectorAll('#cards .card, #replicaCatalog .card')) {
  for (const card of nodes) {
    const id = card.dataset.dramaId, drama = byID.get(id);
    if (!drama) continue;
    const save = card.querySelector('[data-save-id]');
    if (save) {
      const saved = Boolean(app.following.get(id)?.saved);
      save.setAttribute('aria-pressed', String(saved));
      save.setAttribute('aria-label', (saved ? '取消收藏 ' : '收藏 ') + dramaTitle(drama));
      save.title = saved ? '取消收藏' : '加入想看';
      save.disabled = app.following.busy(id);
    }
    const history = window.XiaoqiHistory.get(id);
    const watch = card.querySelector('.watch-button');
    if (!watch) continue;
    const watchLabel = watch.querySelector('.watch-label');
    if (watchLabel) watchLabel.textContent = history ? '续播' : '播放';
    watch.classList.toggle('has-progress', Boolean(history));
    watch.setAttribute('aria-label', (history ? '继续观看 ' : '播放 ') + dramaTitle(drama));
    const progress = watch.querySelector('.poster-progress');
    if (progress) {
      progress.hidden = !(history?.duration > 0);
      if (progress.firstElementChild) progress.firstElementChild.style.width = history?.duration > 0 ? Math.min(100, Math.max(0, history.position / history.duration * 100)) + '%' : '0%';
    }
    const meta = card.querySelector('.meta');
    if (meta) {
      meta.textContent = dramaMetaText(drama);
      meta.title = dramaMetaText(drama);
    }
  }
}

function searchHistory() {
  const items = readPreference('recentSearches', []);
  return Array.isArray(items) ? items.filter(item => typeof item === 'string' && item.length <= 80).slice(0, 10) : [];
}

function rememberSearch(raw) {
  const query = raw.trim().slice(0, 80);
  if (query) savePreference('recentSearches', [query, ...searchHistory().filter(item => item !== query)].slice(0, 10));
  $('recentSearches').hidden = true;
}

function renderSearchHistory() {
  const list = searchHistory(), target = $('recentSearches');
  target.replaceChildren();
  target.hidden = !list.length;
  if (!list.length) return;
  target.appendChild(element('span', 'small', '最近搜索'));
  list.forEach(query => target.appendChild(button(query, () => {
    $('searchInput').value = query;
    resetOnlineSearch();
    renderDramas();
    persistFilters();
    target.hidden = true;
  }, false, 'secondary')));
  target.appendChild(button('清除记录', () => {savePreference('recentSearches', []); target.hidden = true;}, false, 'quiet'));
}

function init() {
  recommendations.init();
  rebuildSources();
  const stored = readPreference('libraryFilters', {});
  const saved = stored && typeof stored === 'object' ? stored : {};
  const allowedSources = (app.viewer?.sources || ['hongguo']).map(normalizeSource);
  const savedSource = normalizeSource(saved.source);
  $('sourceSelect').value = allowedSources.includes(savedSource) ? savedSource : '';
  $('sortSelect').value = window.XiaoqiLibrarySort.modes.includes(saved.sort) ? saved.sort : 'default';
  $('searchInput').value = typeof saved.search === 'string' ? saved.search.slice(0, 80) : '';
  const initialRoute = window.XiaoqiDialogs?.route?.();
  pendingCategory = initialRoute?.kind === 'category' ? initialRoute.value : typeof saved.category === 'string' ? saved.category : null;
  rebuildChannels(false);
  refreshFilterSummary();
  const filters = () => {persistFilters(); renderDramas(); app.shell.resetScroll();};
  $('refreshBtn').addEventListener('click', () => loadDramas(true));
  $('retryLibraryBtn').addEventListener('click', () => loadDramas(false));
  $('searchInput').addEventListener('input', () => {
    resetOnlineSearch();
    persistFilters();
    refreshFilterSummary();
    clearTimeout(filterTimer);
    filterTimer = setTimeout(() => {renderDramas(); app.shell.resetScroll();}, 90);
  });
  $('searchInput').addEventListener('focus', renderSearchHistory);
  $('searchInput').addEventListener('blur', () => setTimeout(() => {
    if (!$('recentSearches').contains(document.activeElement)) $('recentSearches').hidden = true;
  }, 150));
  $('searchInput').addEventListener('keydown', event => {
    if (event.key === 'Enter' && !event.isComposing) {event.preventDefault(); searchOnline();}
  });
  $('clearSearchBtn').addEventListener('click', () => {$('searchInput').value = ''; resetOnlineSearch(); filters(); $('searchInput').focus();});
  $('onlineSearchBtn').addEventListener('click', searchOnline);
  $('sourceSelect').addEventListener('change', () => {
    const route = window.XiaoqiDialogs?.route?.();
    pendingCategory = route?.kind === 'category' ? route.value : $('channelSelect').value || null;
    resetOnlineSearch();
    rebuildChannels(false);
    filters();
  });
  $('channelSelect').addEventListener('change', () => {
    filters();
    const value = $('channelSelect').value;
    if (value) window.XiaoqiDialogs?.navigateCategory?.(value);
    else if (window.XiaoqiDialogs?.route?.().kind === 'category') window.XiaoqiDialogs.navigate('library');
  });
  $('sortSelect').addEventListener('change', filters);
  $('resetFiltersBtn').addEventListener('click', resetFilters);
  $('batchSelectBtn').addEventListener('click', () => toggleBatch(!batch));
  $('finishBatchBtn').addEventListener('click', () => toggleBatch(false));
  $('selectVisibleBtn').addEventListener('click', () => {visibleIDs.forEach(id => selected.add(id)); renderDramas();});
  $('invertVisibleBtn').addEventListener('click', () => {visibleIDs.forEach(id => selected.has(id) ? selected.delete(id) : selected.add(id)); renderDramas();});
  $('deselectDramasBtn').addEventListener('click', () => {selected.clear(); renderDramas();});
  $('enqueueBtn').addEventListener('click', enqueueSelected);
  for (let index = 0; index < 12; index++) {const item = element('div', 'skeleton-card'); item.setAttribute('aria-hidden', 'true'); cards.appendChild(item);}
  return loadDramas(false);
}

function snapshot() {
  return {
    source: $('sourceSelect').value,
    category: $('channelSelect').value,
    sort: $('sortSelect').value,
    search: $('searchInput').value,
    scrollTop: $('workspaceMain').scrollTop
  };
}

function restoreSnapshot(value) {
  if (!value || typeof value !== 'object') return;
  if (typeof value.source === 'string' && Array.from($('sourceSelect').options).some(option => option.value === value.source)) $('sourceSelect').value = value.source;
  if (typeof value.sort === 'string' && Array.from($('sortSelect').options).some(option => option.value === value.sort)) $('sortSelect').value = value.sort;
  if (typeof value.search === 'string') $('searchInput').value = value.search.slice(0, 80);
  rebuildChannels(false);
  if (typeof value.category === 'string' && value.category) {
    let option = Array.from($('channelSelect').options).find(item => item.value === value.category);
    if (!option) {option = element('option', '', value.category); option.value = value.category; $('channelSelect').appendChild(option);}
    $('channelSelect').value = option.value;
    pendingCategory = value.category;
  } else pendingCategory = null;
  resetOnlineSearch();
  persistFilters();
  renderDramas();
  requestAnimationFrame(() => { $('workspaceMain').scrollTop = Number.isFinite(Number(value.scrollTop)) ? Math.max(0, Number(value.scrollTop)) : 0; });
}

function get(id) {
  const value = String(id || '');
  return byID.get(value) || dramas.find(drama => drama.id === value || drama.primaryDramaId === value || drama.rawIds?.includes(value) || drama.sources?.some(source => source.dramaId === value));
}

async function ensure(id) {
  const value = String(id || '').trim();
  if (!value) return null;
  const known = get(value);
  if (known) return known;
  try {
    const result = await api('/api/ui/works/' + encodeURIComponent(value));
    if (!result || !result.id) return null;
    mergeWorks([result]);
    rebuildSources();
    rebuildChannels(false);
    renderDramas();
    app.following.render();
    app.details.refresh();
    window.dispatchEvent(new Event('xiaoqilibrarychange'));
    return get(value) || result;
  } catch (_) {
    return null;
  }
}

return {init, refreshDrama, layout: viewport.refresh, get, ensure, all: () => dramas, isCatalogLoading: () => catalogLoading, createCard: renderCard, render: renderDramas, setCategory, snapshot, restoreSnapshot, refreshFollowing, repairCover: coverRepair.touch, coverFailed: coverRepair.failed, coverLoaded: coverRepair.loaded, refresh: () => {libraryRevision = 0; return loadDramas(false);}, retryCovers: () => {document.querySelectorAll('#cards .card, #replicaCatalog .card').forEach(card => card.retryCover?.()); app.details.retryCover(); app.following.retryCovers();}};

}
