(function () {
  'use strict';
  const root = document.getElementById('sxLive');
  if (!root) return;
  const byId = id => document.getElementById(id);
  const video = byId('liveVideo'), stage = byId('liveStage'), controls = byId('liveControls');
  const state = { channel: null, description: null, hls: null, generation: 0, controller: null, listController: null, listGeneration: 0, page: 1, total: 0, size: 30, group: 0, query: '', excluded: [], retried: new Set(), recovering: false, renew: 0, stall: 0, hide: 0, lastTime: 0, lastAdvance: Date.now(), brightness: 1, autoplayMuted: false, pausedInBackground: false, disposed: false };
  const recentKey = 'sx_live_recent_channel';
	state.programme = null; state.resumePosition = 0; state.ended = false; state.eventReplay = false; state.eventId = '';
	const isReplay = () => Boolean(state.programme || state.eventReplay);
  const policyError = error => [401, 403, 404, 410, 451].includes(Number(error?.status || 0));
  const retryable = error => !Number(error?.status) || [408, 429].includes(Number(error.status)) || Number(error.status) >= 500;
  function localGet(key) { try { return localStorage.getItem(key); } catch (_) { return null; } }
  function localSet(key, value) { try { localStorage.setItem(key, value); } catch (_) {} }
  function message(text, retry = false, start = false) {
    byId('liveMessage').hidden = !text; byId('liveMessageText').textContent = text;
    byId('liveRetry').hidden = !retry; byId('liveStart').hidden = !start;
    const busy = Boolean(text && !retry && !start && !/不支持|尚未|未允许|需要|已结束/.test(text));
    byId('liveSpinner').hidden = !busy;
    byId('liveMessageChannels').hidden = !retry;
    stage.classList.toggle('is-loading', busy);
  }
  function mutedHint() {
    byId('liveMute').textContent = video.muted ? '开启声音' : '声音';
    byId('liveMute').setAttribute('aria-label', video.muted ? '开启声音' : '静音');
    byId('liveHint').hidden = !state.autoplayMuted;
    byId('liveHint').textContent = state.autoplayMuted ? '已静音播放，点按“开启声音”收听' : '';
  }
  async function api(path, options = {}) {
    const request = new AbortController(), upstream = options.signal;
    const abort = () => request.abort(); upstream?.addEventListener('abort', abort, { once: true });
    if (upstream?.aborted) request.abort();
    let timedOut = false;
    const timer = setTimeout(() => { timedOut = true; request.abort(); }, /(?:^|\/)resolve$/.test(path) ? 23000 : 12000);
    try {
      const response = await fetch('/suxinvideo/app/v1/live/' + path, { credentials: 'same-origin', ...options, signal: request.signal });
      let result;
      try { result = await response.json(); } catch (_) { throw new Error('直播服务暂时不可用，请稍后重试'); }
      if (!response.ok || (result.code !== undefined && result.code !== 0)) {
        const error = new Error(result.message || result.error?.message || '直播请求失败');
        error.data = result.data; error.status = response.status; throw error;
      }
      return result.data || result;
    } catch (error) {
      if (timedOut) throw new Error('直播服务响应较慢，请重新连接或切换频道');
      if (error.name === 'TypeError') throw new Error('网络连接失败，请检查网络后重试');
      throw error;
    } finally {
      clearTimeout(timer); upstream?.removeEventListener('abort', abort);
    }
  }
  const post = (path, data, signal) => api(path, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(data), signal });
  function showControls() {
    stage.classList.remove('controls-hidden'); clearTimeout(state.hide);
    if (!video.paused) state.hide = setTimeout(() => { if (!controls.contains(document.activeElement) && byId('liveSettings').hidden) stage.classList.add('controls-hidden'); }, 4500);
  }
  function stopMedia() {
    clearTimeout(state.renew); clearTimeout(state.stall);
    if (state.hls) { state.hls.destroy(); state.hls = null; }
    video.pause(); video.removeAttribute('src'); video.load();
  }
  function edge() {
	if (isReplay()) return;
    if (state.hls && Number.isFinite(state.hls.liveSyncPosition)) video.currentTime = state.hls.liveSyncPosition;
    else if (video.seekable.length) video.currentTime = Math.max(video.seekable.start(video.seekable.length - 1), video.seekable.end(video.seekable.length - 1) - 1);
  }
  function renderSources(channel, description) {
    const select = byId('liveStreams'); select.replaceChildren();
    const sources = channel?.streams?.length ? channel.streams : description?.streams || [];
    const auto = new Option('自动选择', '0'); select.add(auto);
    sources.forEach((stream, index) => {
      const health = stream.health === 'unavailable' || stream.health === 'unhealthy' ? ' · 暂不可用' : '';
      select.add(new Option('线路 ' + (index + 1) + ' · ' + (stream.name || '直播源') + health, String(stream.id)));
    });
    if (description && !sources.some(stream => Number(stream.id) === Number(description.stream_id))) select.add(new Option(description.stream_name || '当前线路', String(description.stream_id)));
    select.value = String(description?.stream_id || 0); select.disabled = sources.length === 0;
  }
  function renderQualities() {
    const select = byId('liveQuality'); select.replaceChildren();
    const levels = state.hls?.levels || [];
    if (levels.length > 1) {
      select.add(new Option('自动', '-1'));
      levels.forEach((level, index) => { const dimensions = level.height ? level.height + 'p' : level.width ? level.width + 'px' : ''; select.add(new Option(dimensions || Math.round(level.bitrate / 1000) + ' kbps', String(index))); });
      select.value = String(state.hls.currentLevel < 0 ? -1 : state.hls.currentLevel); select.dataset.mode = 'hls';
    } else {
      const qualities = state.description?.qualities || [];
      qualities.filter(item => item.url).forEach((item, index) => select.add(new Option(item.label || (item.height ? item.height + 'p' : '清晰度 ' + (index + 1)), String(index))));
      select.dataset.mode = 'urls';
    }
    byId('liveQualityWrap').hidden = select.options.length < 2;
  }
  async function playVideo() {
    try { await video.play(); } catch (error) {
      if (error.name === 'NotAllowedError' && !video.muted) {
        video.muted = true; state.autoplayMuted = true; mutedHint();
        try { await video.play(); } catch (_) { message('点按下方按钮开始直播', false, true); }
      } else if (error.name === 'NotAllowedError') message('点按下方按钮开始直播', false, true);
      else if (error.name !== 'AbortError') await recover();
    }
  }
  function loadMedia(url, generation) {
    stopMedia(); state.lastTime = 0; state.lastAdvance = Date.now();
	state.ended = false;
	byId('liveModeLabel').textContent = isReplay() ? '节目回看' : '● 直播中';
	byId('liveReplayProgress').hidden = !isReplay();
	if (state.description?.mime_type === 'video/mp4') { video.src = url; renderQualities(); playVideo(); armStall(generation); scheduleRenew(generation); return; }
    const nativeHls = video.canPlayType('application/vnd.apple.mpegurl');
    const appleBrowser = /iPad|iPhone|iPod|Macintosh/.test(navigator.userAgent) && !/Chrome|Chromium|Android/.test(navigator.userAgent);
    if (nativeHls && appleBrowser) {
      video.src = url; renderQualities(); playVideo();
    } else if (window.Hls?.isSupported()) {
      const hls = new window.Hls({ liveSyncDurationCount: 3, liveMaxLatencyDurationCount: 8, manifestLoadingTimeOut: 12000, levelLoadingTimeOut: 12000, fragLoadingTimeOut: 35000, maxBufferLength: 30, backBufferLength: 30, autoStartLoad: true });
      state.hls = hls;
      hls.on(window.Hls.Events.MANIFEST_PARSED, () => { if (generation !== state.generation) return; renderQualities(); playVideo(); });
      hls.on(window.Hls.Events.ERROR, (_, data) => {
        if (generation !== state.generation) return;
        const unsupported = /unsupported.*(?:codec|hevc)|no demux matching/i.test(data.error?.message || data.reason || '');
        if (policyError({ status: data.response?.code || data.networkDetails?.status })) { stopMedia(); state.recovering = false; message('当前媒体授权已失效或无权播放，请重新登录或返回直播', true); return; }
        if (data.fatal || unsupported) recover();
      });
      hls.attachMedia(video); hls.loadSource(url);
    } else if (nativeHls) {
      video.src = url; renderQualities(); playVideo();
    } else { message('此浏览器不支持 HLS 直播，请使用最新版 Chrome、Edge 或 Safari', false); return; }
    armStall(generation); scheduleRenew(generation);
  }
  function armStall(generation) {
    clearTimeout(state.stall);
    state.stall = setTimeout(() => {
      if (generation !== state.generation || state.disposed) return;
      if (!document.hidden && !video.paused && Date.now() - state.lastAdvance > 40000) recover();
      else if (!document.hidden && video.readyState < 2 && Date.now() - state.lastAdvance > 40000 && byId('liveStart').hidden) recover();
      else armStall(generation);
    }, 4000);
  }
  function scheduleRenew(generation) {
    clearTimeout(state.renew);
    if (!state.description?.session_id) return;
    const expires = Number(state.description.expires_at || 0) * 1000;
    const delay = Math.min(60000, Math.max(10000, expires - Date.now() - 60000));
    state.renew = setTimeout(async () => {
      if (generation !== state.generation || state.disposed) return;
      try {
        const renewed = await post('renew', { session_id: state.description.session_id }, state.controller?.signal);
        if (generation !== state.generation) return;
        state.description.expires_at = renewed.expires_at; scheduleRenew(generation);
      } catch (error) { if (generation === state.generation && error.name !== 'AbortError') { if (policyError(error)) { stopMedia(); state.recovering = false; message(error.message || '播放授权已失效，请重新登录或返回直播', true); } else recover(); } }
    }, delay);
  }
  async function resolve(streamId = 0, isRecovery = false) {
    if (!state.channel) return;
    const generation = ++state.generation;
    state.controller?.abort(); state.controller = new AbortController(); stopMedia();
    if (!isRecovery) { state.excluded = []; state.retried.clear(); }
    message(isRecovery ? '当前线路中断，正在切换可用线路…' : '正在连接 ' + state.channel.name + '…');
    byId('liveName').textContent = state.channel.name; renderSources(state.channel, null);
    try {
      const description = await post(state.programme ? 'replay/resolve' : 'resolve', state.programme ? { channel_id: state.channel.id, programme_id: state.programme.id, stream_id: streamId || state.programme.stream_id } : { channel_id: state.channel.id, stream_id: streamId, exclude_stream_ids: state.excluded }, state.controller.signal);
      if (generation !== state.generation || state.disposed) return;
      const expected = state.programme ? Number(description.programme_id) === Number(state.programme.id) && description.is_live === false && ['catchup', 'event_replay'].includes(description.mode)
        : description.is_live !== false || description.mode === 'event_replay' && Number(description.programme_id || 0) >= 0;
      if (Number(description.channel_id) !== Number(state.channel.id) || !expected || state.eventId && description.event_id !== state.eventId) {
        const error = new Error('播放信息与当前频道或节目不一致，请重新选择'); error.status = 409; throw error;
      }
      state.eventReplay = !state.programme && description.is_live === false && description.mode === 'event_replay'; state.eventId = description.event_id || '';
      state.description = description; state.recovering = false;
      byId('liveName').textContent = state.programme?.title || description.programme_title || description.name || state.channel.name;
      byId('liveLine').textContent = description.stream_name || '当前直播线路';
      renderSources(state.channel, description); loadMedia(description.url, generation);
      const address = new URL(location.href); address.searchParams.set('channel', String(state.channel.id)); history.replaceState(null, '', address);
    } catch (error) {
      if (generation !== state.generation || error.name === 'AbortError') return;
      const failed = Number(error.data?.stream_id || streamId || 0);
	  if (policyError(error) || !retryable(error)) { state.recovering = false; state.description = null; message(error.message || '当前频道或节目暂不可访问，请重新登录或返回直播', true); return; }
	  if (isReplay()) {
        const replayLine = failed || Number(state.programme?.stream_id || 0);
        if (!state.retried.has(replayLine)) { state.retried.add(replayLine); await resolve(replayLine, true); return; }
        state.recovering = false; state.description = null; message(error.message || '此节目回看暂时不可用，请重试或返回直播', true); return;
      }
      if (error.data?.streams?.length) state.channel.streams = error.data.streams;
      if (failed && !state.retried.has(failed)) {
        state.retried.add(failed); await resolve(failed, true); return;
      }
      if (failed && !state.excluded.includes(failed)) {
        state.excluded.push(failed); await resolve(0, true); return;
      }
      state.recovering = false; state.description = null; byId('liveLine').textContent = '暂无可用线路'; message(error.message || '当前频道暂无可用线路，请稍后重试', true);
    }
  }
  async function recover() {
    if (state.recovering || !state.channel || state.disposed || state.ended || document.hidden) return;
    state.recovering = true;
    const current = Number(state.description?.stream_id || 0);
	if (isReplay()) { if (current && !state.retried.has(current)) { state.retried.add(current); state.resumePosition = video.currentTime; await resolve(current, true); } else { state.recovering = false; message('节目回看连接中断，请重试或返回直播', true); } return; }
    if (current && !state.retried.has(current)) { state.retried.add(current); await resolve(current, true); }
    else { if (current && !state.excluded.includes(current)) state.excluded.push(current); await resolve(0, true); }
  }
  async function choose(channel, requestedStream = 0) {
	state.programme = null; state.resumePosition = 0; state.ended = false; state.eventReplay = false; state.eventId = '';
	root.dispatchEvent(new CustomEvent('sx-live-channel', { detail: channel }));
    state.channel = channel; state.description = null; state.recovering = false;
    document.querySelectorAll('.sx-live-channels button').forEach(button => button.classList.toggle('on', Number(button.dataset.id) === Number(channel.id)));
    resolve(requestedStream);
    const selectedId = channel.id;
    try {
      const detail = await api('channels?channel_id=' + encodeURIComponent(channel.id) + '&size=1');
      if (state.channel?.id !== selectedId) return;
      const detailed = detail.items?.find(item => Number(item.id) === Number(selectedId));
      if (detailed) { state.channel = detailed; renderSources(state.channel, state.description); }
    } catch (_) { /* Playback can start independently of optional channel metadata. */ }
  }
  function channelCard(channel) {
    const button = document.createElement('button'); button.type = 'button'; button.dataset.id = String(channel.id); button.title = channel.name;
    button.classList.toggle('on', Number(state.channel?.id) === Number(channel.id));
    button.setAttribute('aria-label', channel.name + (channel.group_name ? '，' + channel.group_name : ''));
    const logo = document.createElement('span'); logo.className = 'sx-live-logo';
    const fallback = document.createElement('span'); fallback.className = 'sx-live-logo-fallback'; fallback.textContent = channel.name.slice(0, 3); logo.append(fallback);
    if (channel.logo) {
      const image = document.createElement('img'); image.src = channel.logo; image.loading = 'lazy'; image.alt = '';
      image.addEventListener('load', () => { fallback.hidden = true; image.classList.add('loaded'); }, { once: true });
      image.addEventListener('error', () => image.remove(), { once: true }); logo.append(image);
    }
    button.append(logo);
    const text = document.createElement('span'); text.className = 'sx-live-channel-text'; const title = document.createElement('b'); title.textContent = channel.name;
    const group = document.createElement('small'); group.textContent = channel.group_name || '直播频道'; text.append(title, group); button.append(text);
    button.addEventListener('click', () => choose(channel)); return button;
  }
  async function loadChannels(first = false) {
    const generation = ++state.listGeneration; state.listController?.abort(); state.listController = new AbortController();
    byId('liveListStatus').textContent = '正在加载频道…';
    try {
      const data = await api('channels?' + new URLSearchParams({ group_id: String(state.group), q: state.query, page: String(state.page), size: String(state.size) }), { signal: state.listController.signal });
      if (generation !== state.listGeneration) return;
      const channels = data.items || []; state.total = Number(data.total || 0); const pages = Math.max(1, Math.ceil(state.total / state.size));
      byId('liveChannels').replaceChildren(...channels.map(channelCard)); byId('liveChannels').scrollTop = 0;
      byId('liveListStatus').textContent = channels.length ? '共 ' + state.total + ' 个频道' : '暂无匹配频道';
      byId('livePage').textContent = state.page + ' / ' + pages; byId('livePrev').disabled = state.page <= 1; byId('liveNext').disabled = state.page >= pages;
      if (first && !state.channel) {
        let stored; try { stored = JSON.parse(localGet(recentKey) || 'null'); } catch (_) {}
        const wanted = Number(new URL(location.href).searchParams.get('channel') || stored?.id || 0);
        let chosen = channels.find(channel => Number(channel.id) === wanted);
        if (!chosen && wanted) { const found = await api('channels?channel_id=' + wanted + '&size=1', { signal: state.listController.signal }); chosen = found.items?.find(channel => Number(channel.id) === wanted); }
        if (generation !== state.listGeneration || state.channel) return;
        chosen ||= channels[0]; if (chosen) choose(chosen, stored?.id === chosen.id ? Number(stored.stream_id || 0) : 0);
        else message('管理员尚未启用直播频道');
      }
    } catch (error) { if (generation === state.listGeneration && error.name !== 'AbortError') byId('liveListStatus').textContent = error.message || '频道加载失败，请重新搜索'; }
  }
  async function loadGroups() {
    const data = await api('groups'); const groups = [{ id: 0, name: '全部' }, ...(data.items || [])];
    byId('liveGroups').replaceChildren(...groups.map(group => { const button = document.createElement('button'); button.type = 'button'; button.textContent = group.name; button.title = group.name; button.classList.toggle('on', group.id === state.group); button.addEventListener('click', () => { state.group = Number(group.id); state.page = 1; byId('liveGroups').querySelectorAll('button').forEach(item => item.classList.toggle('on', item === button)); loadChannels(); }); return button; }));
  }
  byId('liveSearch').addEventListener('submit', event => { event.preventDefault(); state.query = byId('liveSearchInput').value.trim(); state.page = 1; loadChannels(); });
  byId('livePrev').addEventListener('click', () => { if (state.page > 1) { state.page--; loadChannels(); } });
  byId('liveNext').addEventListener('click', () => { if (state.page * state.size < state.total) { state.page++; loadChannels(); } });
  byId('liveStreams').addEventListener('change', event => { if (isReplay()) state.resumePosition = video.currentTime || state.resumePosition; resolve(Number(event.target.value)); });
  byId('liveQuality').addEventListener('change', event => {
    if (event.target.dataset.mode === 'hls' && state.hls) state.hls.currentLevel = Number(event.target.value);
    else { const quality = state.description?.qualities?.filter(item => item.url)[Number(event.target.value)]; if (quality) { if (isReplay()) state.resumePosition = video.currentTime; loadMedia(quality.url, state.generation); } }
    showControls();
  });
  byId('liveRetry').addEventListener('click', () => { if (isReplay() && state.description) state.resumePosition = video.currentTime || state.resumePosition; resolve(isReplay() ? Number(state.description?.stream_id || state.programme?.stream_id || 0) : 0); }); byId('liveStart').addEventListener('click', () => { message(''); playVideo(); });
  byId('liveToggle').addEventListener('click', () => { if (isReplay() && state.ended) { state.ended = false; video.currentTime = 0; message(''); armStall(state.generation); playVideo(); } else if (video.paused) playVideo(); else video.pause(); });
  byId('liveMute').addEventListener('click', () => { video.muted = !video.muted; state.autoplayMuted = false; mutedHint(); if (video.paused) playVideo(); });
  byId('liveEdge').addEventListener('click', () => { if (state.eventReplay && !state.programme) chooseFromCatalog(); else if (isReplay()) { state.programme = null; state.eventReplay = false; state.eventId = ''; state.resumePosition = 0; state.ended = false; byId('liveReplayProgress').hidden = true; resolve(); } else { edge(); playVideo(); } showControls(); });
  async function leaveFullscreen() {
    if (document.fullscreenElement || document.webkitFullscreenElement) await (document.exitFullscreen?.() || document.webkitExitFullscreen?.());
    else if (video.webkitDisplayingFullscreen) video.webkitExitFullscreen?.();
  }
  async function chooseFromCatalog() {
    try { await leaveFullscreen(); } catch (_) {}
    byId('liveCatalog').scrollIntoView({ behavior: 'smooth', block: 'start' });
    if (!matchMedia('(pointer: coarse)').matches) byId('liveSearchInput').focus({ preventScroll: true });
  }
  byId('liveChoose').addEventListener('click', chooseFromCatalog);
  byId('liveMessageChannels').addEventListener('click', chooseFromCatalog);
  byId('liveSettingsChannels').addEventListener('click', chooseFromCatalog);
  function closeSettings() {
    byId('liveSettings').hidden = true; byId('liveSettingsToggle').setAttribute('aria-expanded', 'false'); showControls();
  }
  function fullscreenChanged() {
    const fullscreen = document.fullscreenElement === stage || document.webkitFullscreenElement === stage;
    (fullscreen ? byId('liveSettings') : byId('liveOptionsDock')).append(byId('liveOptions'));
    byId('liveSettingsToggle').hidden = !fullscreen;
    byId('liveFull').textContent = fullscreen ? '退出' : '全屏';
    closeSettings();
  }
  byId('liveSettingsToggle').addEventListener('click', () => {
    const panel = byId('liveSettings'); panel.hidden = !panel.hidden;
    byId('liveSettingsToggle').setAttribute('aria-expanded', String(!panel.hidden)); showControls();
  });
  byId('liveSettingsClose').addEventListener('click', closeSettings);
  byId('liveSettings').addEventListener('click', event => event.stopPropagation());
  document.addEventListener('fullscreenchange', fullscreenChanged);
  document.addEventListener('webkitfullscreenchange', fullscreenChanged);
  byId('liveFull').addEventListener('click', async () => {
    try {
      if (document.fullscreenElement || document.webkitFullscreenElement || video.webkitDisplayingFullscreen) await leaveFullscreen();
      else if (stage.requestFullscreen) await stage.requestFullscreen(); else if (stage.webkitRequestFullscreen) stage.webkitRequestFullscreen(); else if (video.webkitEnterFullscreen) video.webkitEnterFullscreen();
    } catch (_) { message('浏览器未允许全屏，请从浏览器设置启用', false); setTimeout(() => message(''), 2500); }
    showControls();
  });
  controls.addEventListener('click', event => { event.stopPropagation(); showControls(); }); controls.addEventListener('focusin', showControls);
  stage.addEventListener('pointermove', event => { if (event.pointerType === 'mouse') showControls(); }); stage.addEventListener('click', () => { if (stage.classList.contains('controls-hidden')) showControls(); else if (!video.paused) stage.classList.add('controls-hidden'); });
  stage.addEventListener('keydown', event => { if (event.target !== stage) return; if (event.key === ' ' || event.key === 'Enter') { event.preventDefault(); byId('liveToggle').click(); } if (event.key.toLowerCase() === 'f') byId('liveFull').click(); });
  video.addEventListener('playing', () => { message(''); mutedHint(); state.recovering = false; state.lastAdvance = Date.now(); byId('liveToggle').textContent = '暂停'; if (!isReplay() && state.description && state.channel) localSet(recentKey, JSON.stringify({ id: state.channel.id, name: state.channel.name, stream_id: state.description.stream_id })); showControls(); });
  video.addEventListener('pause', () => { byId('liveToggle').textContent = '播放'; showControls(); });
  video.addEventListener('timeupdate', () => { if (video.currentTime !== state.lastTime) { state.lastTime = video.currentTime; state.lastAdvance = Date.now(); } });
	video.addEventListener('timeupdate', () => { if (isReplay() && Number.isFinite(video.duration) && video.duration > 0) { byId('liveReplaySeek').value = String(video.currentTime / video.duration * 1000); byId('liveReplayTime').textContent = Math.floor(video.currentTime/60)+':'+String(Math.floor(video.currentTime%60)).padStart(2,'0')+' / '+Math.floor(video.duration/60)+':'+String(Math.floor(video.duration%60)).padStart(2,'0'); } });
	video.addEventListener('loadedmetadata', () => { if (isReplay() && state.resumePosition > 0) { video.currentTime = Number.isFinite(video.duration) ? Math.min(state.resumePosition, Math.max(0,video.duration - 1)) : state.resumePosition; state.resumePosition = 0; } });
	video.addEventListener('ended', () => { if (isReplay()) { state.ended = true; clearTimeout(state.stall); byId('liveToggle').textContent = '重播'; message('节目回看已结束，可选择其他节目或返回直播', false); showControls(); } });
	byId('liveReplaySeek').addEventListener('input', event => { if (isReplay() && Number.isFinite(video.duration)) video.currentTime = Number(event.target.value)/1000*video.duration; });
	root.addEventListener('sx-live-replay', event => { const programme = event.detail; if (!state.channel || Number(programme.channel_id) !== Number(state.channel.id) || !programme.can_replay || Number(programme.stop) > Date.now()/1000) return; state.programme = programme; state.eventReplay = false; state.eventId = ''; state.resumePosition = 0; state.ended = false; state.description = null; resolve(programme.stream_id); });
  video.addEventListener('error', () => { if (video.getAttribute('src') || state.hls) recover(); });
  video.addEventListener('waiting', () => { if (!state.recovering && !document.hidden && byId('liveStart').hidden && !state.ended) message(isReplay() ? '正在缓冲节目回看…' : '正在缓冲直播画面…'); });
  document.addEventListener('visibilitychange', () => {
    if (document.hidden) { state.pausedInBackground = !video.paused; video.pause(); }
    else if (state.pausedInBackground) { state.pausedInBackground = false; state.lastAdvance = Date.now(); edge(); playVideo(); }
  });
  let touch = null, gestureTimer;
  stage.addEventListener('touchstart', event => {
    if (event.touches.length !== 1 || event.target.closest('button,select,label')) return;
    const point = event.touches[0], rect = stage.getBoundingClientRect(); touch = { x: point.clientX, y: point.clientY, left: point.clientX < rect.left + rect.width / 2, value: point.clientX < rect.left + rect.width / 2 ? state.brightness : video.volume, height: rect.height, moving: false };
  }, { passive: true });
  stage.addEventListener('touchmove', event => {
    if (!touch || event.touches.length !== 1) return;
    const point = event.touches[0], dy = touch.y - point.clientY;
    if (!touch.moving && (Math.abs(dy) < 16 || Math.abs(dy) < Math.abs(point.clientX - touch.x))) return;
    touch.moving = true; event.preventDefault(); const value = Math.max(touch.left ? .1 : 0, Math.min(1, touch.value + dy / Math.max(120, touch.height)));
    if (touch.left) { state.brightness = value; byId('liveShade').style.opacity = String(1 - value); }
    else { video.volume = value; video.muted = value === 0; }
    byId('liveGesture').hidden = false; byId('liveGesture').textContent = (touch.left ? '亮度 ' : '音量 ') + Math.round(value * 100) + '%';
  }, { passive: false });
  function endGesture() { touch = null; clearTimeout(gestureTimer); gestureTimer = setTimeout(() => { byId('liveGesture').hidden = true; }, 700); }
  stage.addEventListener('touchend', endGesture); stage.addEventListener('touchcancel', endGesture);
  addEventListener('pagehide', () => { state.disposed = true; state.generation++; state.controller?.abort(); state.listController?.abort(); stopMedia(); clearTimeout(state.hide); clearTimeout(gestureTimer); });
  loadGroups().catch(error => { byId('liveListStatus').textContent = error.message; }); loadChannels(true);
})();
