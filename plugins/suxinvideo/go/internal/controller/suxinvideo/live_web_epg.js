(function () {
  'use strict';
  const root = document.getElementById('sxLive'); if (!root) return;
  const byId = id => document.getElementById(id);
  const date = byId('liveEpgDate'), status = byId('liveEpgStatus'), list = byId('liveEpgList');
  let channel = null, controller = null, generation = 0;
  const localDate = () => new Intl.DateTimeFormat('en-CA', {timeZone:'Asia/Shanghai',year:'numeric',month:'2-digit',day:'2-digit'}).format(new Date());
  const clock = seconds => new Intl.DateTimeFormat('zh-CN',{timeZone:'Asia/Shanghai',hour:'2-digit',minute:'2-digit',hour12:false}).format(new Date(seconds*1000));
  date.value = localDate();
  async function load() {
    controller?.abort(); const request = new AbortController(); controller = request; const current = ++generation;
    list.replaceChildren(); status.textContent = channel ? '正在读取节目单…' : '选择频道后查看节目单';
    if (!channel) return;
    const timer = setTimeout(() => request.abort(),12000);
    try {
      const response = await fetch('/suxinvideo/app/v1/live/epg?channel_id='+encodeURIComponent(channel.id)+'&date='+encodeURIComponent(date.value),{credentials:'same-origin',signal:request.signal});
      const envelope = await response.json(); if (!response.ok || envelope.code) throw Error(envelope.message || '节目单读取失败');
      if (current !== generation) return;
      const data = envelope.data || envelope;
      byId('liveEpgNow').textContent = data.now ? '正在播出：'+data.now.title : '暂无当前节目';
      byId('liveEpgNext').textContent = data.next ? '下一节目：'+clock(data.next.start)+' '+data.next.title : '暂无下一节目';
      const items = data.items || []; status.textContent = items.length ? '北京时间 · 回看仅显示平台实际提供的节目' : '暂无节目单';
      for (const programme of items) {
        const row = document.createElement('li'), time = document.createElement('time'), title = document.createElement('span');
        time.textContent = clock(programme.start)+'–'+clock(programme.stop); title.textContent = programme.title; row.append(time,title);
        if (programme.start <= Date.now()/1000 && programme.stop > Date.now()/1000) { const badge = document.createElement('b'); badge.textContent = '直播中'; row.append(badge); row.classList.add('is-current'); }
        if (programme.can_replay && programme.stop <= Date.now()/1000) { const button = document.createElement('button'); button.type = 'button'; button.textContent = '回看'; button.addEventListener('click',()=>{root.dispatchEvent(new CustomEvent('sx-live-replay',{detail:programme}));byId('liveStage').scrollIntoView({behavior:'smooth',block:'center'});}); row.append(button); }
        list.append(row);
      }
    } catch (error) { if (current===generation) { status.textContent = error.name==='AbortError' ? '节目单请求超时，请刷新重试' : error.message; byId('liveEpgNow').textContent = '暂无节目单'; byId('liveEpgNext').textContent = ''; } }
    finally { clearTimeout(timer); }
  }
  root.addEventListener('sx-live-channel', event=>{channel=event.detail;date.value=localDate();load();});
  date.addEventListener('change',load);byId('liveEpgRefresh').addEventListener('click',load);
  addEventListener('pagehide',()=>{generation++;controller?.abort();});
})();
