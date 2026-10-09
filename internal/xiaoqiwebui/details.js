import { $, element, button, icon, coverURL, withFocus, dramaTitle, dramaOperationID, sourceKey, sourceLabel, categoryName, episodeCount, firstNonEmpty, tagsText, releaseText, setMessage, matchesContentPreference } from './ui-core.js';
import { heroPlainText } from './hero-banner.js';

export function createDetails(app) {
  let currentID = '';
  const metadataMessages = new Map();
  let poster;
  const exporting = new Set();
  const panel = $('detailPanel');
  const content = $('detailContent');
  const isOpen = () => Boolean(panel && !panel.hidden);

  // Match the reference page: video on the left, information and episodes on
  // the right. On narrow screens the sidebar follows the video in normal flow.
  const playPage = $('playPage');
  // Reuse the reference play rail when it is already present. Creating a
  // second element with the same id makes the heading and detail panel overlap.
  const sidebar = $('playSidebar') || element('aside', 'play-sidebar');
  if (!sidebar.id) sidebar.id = 'playSidebar';
  sidebar.setAttribute('aria-label', '影片信息与选集');
  const reference = element('div', 'play-reference-content');
  reference.id = 'playReferenceContent';
  const intro = element('div', 'play-reference-intro');
  intro.id = 'playReferenceIntro';
  reference.appendChild(intro);
  sidebar.replaceChildren(reference);
  const episodePanel = $('playerEpisodesPanel');
  const episodeSection = element('section', 'play-reference-section play-reference-episode-section');
  episodeSection.id = 'playReferenceEpisodes';
  const episodeHeading = element('div', 'play-reference-section-title');
  episodeHeading.append(element('strong', '', '选集'), element('span', 'play-reference-episode-meta'));
  episodeSection.append(episodeHeading);
  if (episodePanel) episodeSection.appendChild(episodePanel);
  reference.appendChild(episodeSection);
  // Keep source selection next to the episode grid.
  const sourceControl = $('playbackSourceControl');
  if (episodePanel && sourceControl) episodePanel.querySelector('.episode-navigation')?.before(sourceControl);
  const recommendations = element('section', 'play-recommendations');
  recommendations.id = 'playRecommendations';
  recommendations.setAttribute('aria-label', '相关推荐');
  const expanded = element('section', 'play-expanded-detail');
  expanded.id = 'playExpandedDetail';
  expanded.hidden = true;
  expanded.setAttribute('aria-label', '影片详细信息');
  reference.appendChild(recommendations);
  if (!sidebar.contains(panel)) sidebar.appendChild(panel);
  if (!sidebar.parentElement) playPage?.appendChild(sidebar);
  if (!expanded.parentElement) playPage?.appendChild(expanded);

  function renderRecommendations(drama) {
    recommendations.replaceChildren(element('h2', '', '相关推荐'));
    const list = element('div', 'play-recommendation-grid');
    const category = categoryName(drama);
    const type = drama.categoryPath?.[0] || drama.mediaType;
    const preference = app.viewer?.account?.admin ? '' : app.viewer?.contentPreference || '';
    let count = 0;
    const all = typeof app.library.all === 'function' ? app.library.all() : [];
    const candidates = (Array.isArray(all) ? all : []).filter(candidate => {
      if (candidate.id === drama.id || dramaOperationID(candidate) === dramaOperationID(drama)) return false;
      return matchesContentPreference(candidate, preference);
    });
    const sameContext = candidates.filter(candidate => categoryName(candidate) === category || (type && (candidate.categoryPath?.[0] || candidate.mediaType) === type));
    for (const candidate of [...sameContext, ...candidates.filter(candidate => !sameContext.includes(candidate))]) {
      const title = dramaTitle(candidate);
      const card = button('', () => app.play(candidate.id, title), false, 'play-recommendation-card');
      card.setAttribute('aria-label', '播放 ' + title);
      const cover = element('span', 'play-recommendation-cover');
      const address = coverURL(candidate);
      if (address) {
        const image = element('img');
        image.src = address;
        image.alt = title;
        image.loading = 'lazy';
        image.decoding = 'async';
        image.addEventListener('error', () => cover.replaceChildren(element('span', '', title.slice(0, 2))), {once: true});
        cover.appendChild(image);
      } else cover.appendChild(element('span', '', title.slice(0, 2)));
      card.append(cover, element('strong', '', title));
      list.appendChild(card);
      if (++count === 6) break;
    }
    recommendations.appendChild(count ? list : element('p', 'small', '当前分类暂无其他影片。'));
  }

  function renderCover(drama) {
    const address = coverURL(drama), title = dramaTitle(drama);
    if (poster?.id === drama.id && poster.address === address) {
      if (poster.image) poster.image.alt = title + ' 海报';
      return poster.node;
    }
    const node = element('div', 'detail-cover');
    const status = element('span', 'detail-cover-status', address ? '加载海报…' : '暂无海报');
    status.setAttribute('role', 'status');
    node.appendChild(status);
    const current = {id: drama.id, address, node, failed: false};
    poster = current;
    if (!address) return node;
    const image = element('img');
    current.image = image;
    image.alt = title + ' 海报';
    image.decoding = 'async';
    image.hidden = true;
    image.addEventListener('load', () => {
      if (poster !== current) return;
      current.failed = false;
      image.hidden = false;
      status.hidden = true;
      app.library.coverLoaded(drama.id, address);
    });
    image.addEventListener('error', () => {
      if (poster !== current) return;
      current.failed = true;
      image.hidden = true;
      status.hidden = false;
      status.textContent = '海报加载失败';
      app.library.coverFailed(drama.id, address);
    });
    current.retry = () => {
      if (!current.failed) return;
      current.failed = false;
      status.textContent = '加载海报…';
      image.src = address;
    };
    node.appendChild(image);
    image.src = address;
    return node;
  }

  function refreshCover(id, retry = false) {
    if (!isOpen() || currentID !== id) return;
    const drama = app.library.get(id);
    if (!drama) return;
    const previous = poster?.node;
    const next = renderCover(drama);
    if (previous !== next) previous?.replaceWith(next);
    if (retry) poster?.retry?.();
  }

  function render() {
    if (!currentID) return;
    const known = app.library.get(currentID);
    const operationID = dramaOperationID(known) || currentID;
    const saved = app.following.get(operationID);
    const history = window.XiaoqiHistory.get(operationID);
    const drama = known || {id: currentID, title: saved?.title || history?.title || (currentID === 'ch4k8uf4p' ? '\u5170\u9999\u5982\u6545' : '影片'), source: saved?.source || history?.source, totalEpisode: saved?.totalEpisode || history?.total, categoryName: saved?.category || '未分类', heat: currentID === 'ch4k8uf4p' ? '4292' : '', rating: currentID === 'ch4k8uf4p' ? '8.9' : '', quality: currentID === 'ch4k8uf4p' ? '4k' : ''};
    const title = dramaTitle(drama);
    renderReferenceRail(drama, operationID, title, saved, history);
    withFocus(content, () => {
      content.replaceChildren();
      expanded.replaceChildren(element('h2', '', '影片详情'));
      const top = element('div', 'detail-top');
      const heading = element('div', 'spacer');
      const name = element('h1', '', title);
      name.id = 'detailTitle';
      heading.append(name, element('p', 'detail-meta', sourceLabel(sourceKey(drama)) + ' · ' + categoryName(drama)));
      const tags = element('div', 'tags detail-tags');
      tagsText(drama).slice(0, 10).forEach(tag => tags.appendChild(element('span', 'tag', tag)));
      if (drama.vip === true) tags.prepend(element('span', 'tag vip-tag', 'VIP · 仅试看'));
      heading.appendChild(tags);
      if (Array.isArray(drama.sources) && drama.sources.length) {
        const sourceText = drama.sources.map(item => sourceLabel(item.source)).filter(Boolean).join(' · ');
        heading.appendChild(element('p', 'small detail-sources', '可用片源：' + sourceText + '；默认 ' + sourceLabel(drama.primarySource || drama.source)));
      }
      top.appendChild(heading);
      content.appendChild(top);
      const metadataStatus = element('p', 'small', metadataMessages.get(currentID) || '');
      metadataStatus.id = 'detailMetadataStatus';
      metadataStatus.setAttribute('role', 'status');
      metadataStatus.hidden = !metadataStatus.textContent;
      content.appendChild(metadataStatus);
      if (drama.vip === true) content.appendChild(element('p', 'notice vip-notice', '此剧含 VIP 分集，站源仅提供试看；试看内容不会作为完整分集下载。'));
      if (history) content.appendChild(element('p', 'detail-progress', window.XiaoqiHistory.progressText(history)));
      if (saved?.newEpisodes > 0) content.appendChild(element('p', 'following-update notice', '剧库新增 ' + saved.newEpisodes + ' 集'));
      const actions = element('div', 'detail-actions');
      const play = button(history ? '继续观看' : '开始观看', () => app.play(currentID, title), false, 'primary-action button-content');
      play.prepend(icon('play'));
      play.dataset.focusKey = 'detail-play';
      const bookmark = button(saved?.saved ? '已收藏' : '加入想看', () => app.following.toggleSaved(operationID), app.following.busy(operationID), 'secondary');
      bookmark.id = 'detailSaveBtn';
      bookmark.dataset.focusKey = 'detail-save';
      bookmark.setAttribute('aria-pressed', String(Boolean(saved?.saved)));
      actions.append(play, bookmark);
      content.appendChild(actions);
      const overview = element('div', 'play-detail-overview');
      overview.append(renderCover(drama), element('p', 'detail-description', heroPlainText(firstNonEmpty(drama.description, drama.plot, drama.desc, drama.intro)) || '站源暂未提供简介。'));
      expanded.appendChild(overview);
      const facts = element('dl', 'detail-facts');
      const sources = Array.isArray(drama.sources) && drama.sources.length ? drama.sources.map(item => sourceLabel(item.source)).join(' / ') : sourceLabel(sourceKey(drama));
      const values = [['评分', drama.rating || drama.score || '暂无评分'], ['年份', drama.year || drama.releaseYear || String(drama.onlineDate || '').slice(0, 4) || '暂未提供'], ['类型', (drama.categoryPath || []).join(' / ') || categoryName(drama)], ['片源', sources || '暂未提供'], ['集数', episodeCount(drama) ? episodeCount(drama) + ' 集' : '暂未提供'], ['状态', releaseText(drama.releaseStatus)], ['上线时间', drama.onlineDate], ['站点热度', drama.heat], ['播放量', drama.views]];
      for (const [label, value] of values) {
        if (!value) continue;
        const item = element('div');
        item.append(element('dt', '', label), element('dd', '', value));
        facts.appendChild(item);
      }
      expanded.appendChild(facts);
      const secondary = element('div', 'detail-secondary');
      const download = button('加入下载', () => app.downloads.enqueue([operationID]), !known, 'secondary');
      download.id = 'detailDownloadBtn';
      download.dataset.focusKey = 'detail-download';
      if (!known) download.title = '更新剧库找到此剧后可加入下载';
      const watched = button(saved?.completed ? '取消已看标记' : '标为已看', () => app.following.setCompleted(operationID, !saved?.completed), app.following.busy(operationID), 'secondary');
      watched.id = 'detailCompletedBtn';
      watched.dataset.focusKey = 'detail-completed';
      const emby = button(exporting.has(currentID) ? '导出中…' : '导出 Emby', () => exportEmby(currentID, title), !known || exporting.has(currentID), 'secondary');
      emby.id = 'detailEmbyBtn';
      emby.dataset.focusKey = 'detail-emby';
      emby.title = '导出 STRM 分集文件，用于现有 Emby 电视剧媒体库';
      if (app.viewer?.onlineOnly) secondary.appendChild(watched);
      else secondary.append(app.downloads.qualityControl(), download, watched, emby);
      expanded.appendChild(secondary);
      expanded.appendChild(element('p', 'small notice', '手动标记用于整理清单，实际播放进度仍自动保存。'));
      renderRecommendations(drama);
    });
  }

  function renderReferenceRail(drama, operationID, title, saved, history) {
    if (!intro) return;
    intro.replaceChildren();
    const head = element('div', 'play-reference-head');
    const heading = element('div');
    const titleNode = element('h1', '', title);
    titleNode.id = 'playReferenceTitle';
    heading.append(titleNode);
    const filmMeta = element('div', 'play-reference-meta');
    const heat = drama.heat || drama.views;
    const score = drama.rating || drama.score;
    if (heat) filmMeta.appendChild(element('span', 'play-reference-heat', '♨ ' + heat));
    if (score) filmMeta.appendChild(element('span', 'play-reference-score', '★ ' + score));
    if (drama.quality) filmMeta.appendChild(element('span', 'play-reference-quality', drama.quality));
    heading.appendChild(filmMeta);
    const detail = button('简介', () => {
      const expanded = $('playExpandedDetail');
      if (expanded) expanded.hidden = !expanded.hidden;
    }, false, 'quiet play-reference-detail-button');
    head.append(heading, detail);
    intro.appendChild(head);
    const actions = element('div', 'play-reference-actions');
    const playlist = button('加入片单', () => setMessage('片单功能暂未启用'), false, 'secondary');
    playlist.id = 'playReferencePlaylistBtn';
    const save = button(saved?.saved ? '已收藏' : '收藏', () => app.following.toggleSaved(operationID), app.following.busy(operationID), 'secondary');
    save.id = 'playReferenceSaveBtn';
    save.setAttribute('aria-pressed', String(Boolean(saved?.saved)));
    const download = button('下载', () => app.downloads.enqueue([operationID]), !app.library.get(currentID) || app.viewer?.onlineOnly, 'secondary');
    download.id = 'playReferenceDownloadBtn';
    const rating = button('评分', () => setMessage('评分功能暂未启用'), false, 'secondary');
    rating.id = 'playReferenceRatingBtn';
    playlist.prepend(icon('plus'));
    download.prepend(icon('download'));
    rating.prepend(element('span', 'play-reference-rating', score || '—'));
    save.prepend(icon('heart'));
    actions.append(playlist, download, rating, save);
    intro.appendChild(actions);
    // Keep the reference site's membership promo as a visual card. Account
    // and payment flows are outside this local player, so the CTA is inert.
    const membership = element('div', 'play-membership');
    membership.appendChild(element('strong', '', '成为会员享以下特权'));
    const benefits = element('div');
    benefits.append(element('span', '', '4K超高清'), element('span', '', '去广告特权'), element('span', '', '更快的播放速度'));
    membership.appendChild(benefits);
    const membershipButton = element('button', '', '立即开通');
    membershipButton.type = 'button';
    membershipButton.disabled = true;
    membership.appendChild(membershipButton);
    intro.appendChild(membership);
    const meta = episodeSection?.querySelector('.play-reference-episode-meta');
    if (meta) {
      const total = episodeCount(drama);
      meta.textContent = total ? '共 ' + total + ' 集' : currentID === 'ch4k8uf4p' ? '更新至16/40集' : '';
    }
    // The supplied reference URL is outside the local catalogue. Preserve
    // its visible 16-cell rail without inventing player episodes or streams.
    if (currentID === 'ch4k8uf4p' && episodePanel) {
      const list = episodePanel.querySelector('.player-episode-list');
      setTimeout(() => {
        if (currentID !== 'ch4k8uf4p' || !list || list.children.length) return;
        for (let index = 1; index <= 16; index += 1) {
          const item = button(String(index), () => setMessage('该参考站分集未接入本地片源', true), false, 'reference-placeholder-episode');
          item.setAttribute('aria-disabled', 'true');
          list.appendChild(item);
        }
        if (meta) meta.textContent = '更新至16/40集';
      }, 350);
    }
  }

  async function exportEmby(id, title) {
    if (exporting.has(id)) return;
    exporting.add(id);
    render();
    try {
      const response = await fetch('/api/emby/export', {method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify({dramaId: id, baseUrl: location.origin})});
      if (!response.ok) {const result = await response.json(); throw new Error(result.error || '导出失败');}
      const address = URL.createObjectURL(await response.blob());
      const link = document.createElement('a');
      link.href = address;
      link.download = title + '-Emby.zip';
      document.body.appendChild(link);
      link.click();
      link.remove();
      setTimeout(() => URL.revokeObjectURL(address), 60000);
      setMessage('已导出 Emby 分集；解压到 Emby 电视剧媒体库后扫描即可。');
    } catch (error) {setMessage('Emby 导出失败：' + error.message, true);}
    finally {exporting.delete(id); if (isOpen() && currentID === id) render();}
  }

  function show(id) {
    currentID = id;
    panel.hidden = false;
    render();
    app.library.refreshDrama(id);
  }

  function handleRoute(route) {
    if (route?.kind === 'play' && route.value) show(route.value);
    else if (route?.kind !== 'play') panel.hidden = true;
  }

  function open(id) {
    const drama = app.library.get(id);
    const operationID = dramaOperationID(drama) || id;
    const title = dramaTitle(drama || {id});
    if (window.app?.play) window.app.play(id, title);
    else window.XiaoqiDialogs?.navigatePlay?.(operationID);
    show(id);
  }

  function metadataStatus(id, text) {
    metadataMessages.set(id, text);
    const status = $('detailMetadataStatus');
    if (isOpen() && currentID === id && status) {status.textContent = text; status.hidden = !text;}
  }

  $('closeDetailBtn')?.addEventListener('click', () => {
    panel.hidden = true;
    window.dramaPlayer?.close?.();
    window.XiaoqiDialogs.navigate('library');
  });
  window.XiaoqiDialogs?.listenRoute?.(handleRoute);
  const initialRoute = window.XiaoqiDialogs?.route?.();
  if (initialRoute?.kind === 'play' && initialRoute.value) handleRoute(initialRoute);
  return {open, show, metadataStatus, refreshCover, retryCover: () => refreshCover(currentID, true), refresh: () => {if (isOpen()) render();}};
}

