/* Background source lookup. Media playback remains owned by the play page. */
(function(root){
  'use strict';
  function episodeKey(item){return item&&(item.key||('label:'+(item.name||'').trim()))||'';}
  function playable(item){return Boolean(item&&(item.psrc||item.iframe));}
  function bufferedOnly(item){return {name:item.name,key:item.key,number:item.number,url:'',psrc:'',iframe:'',activeOnly:true};}
  function catalogueKey(sources){return JSON.stringify(sources.map(function(src){return [src.code,src.name,src.episodes.map(function(ep){return [episodeKey(ep),ep.url||ep.psrc,ep.iframe||''];})];}));}
  function reconcile(current,incoming,state){
    var next=incoming.filter(function(src){return src&&typeof src.code==='string'&&Array.isArray(src.episodes);}).map(function(src){return Object.assign({},src,{episodes:src.episodes.slice()});});
    var old=current.find(function(src){return src.code===state.code;}),oldItem=old&&old.episodes.find(function(item){return episodeKey(item)===state.key;});
    if(state.preserve&&old){
      var found=next.find(function(src){return src.code===state.code;});
      if(!found){found={code:old.code,name:old.name,activeOnly:true,episodes:old.episodes.map(bufferedOnly)};next.push(found);}
      if(oldItem&&!found.episodes.some(function(item){return episodeKey(item)===state.key;}))found.episodes.push(bufferedOnly(oldItem));
    }
    var sourceIndex=-1,episodeIndex=-1;
    function match(index){
      if(index<0)return false;
      var ep=next[index].episodes.findIndex(function(item){return episodeKey(item)===state.key&&(state.preserve||playable(item));});
      if(ep<0)return false;sourceIndex=index;episodeIndex=ep;return true;
    }
    match(next.findIndex(function(src){return src.code===(state.preserve?state.code:state.manualCode||state.code);}));
    if(sourceIndex<0&&!state.manualCode)for(var i=0;i<next.length;i++)if(match(i))break;
    return {sources:next,source:sourceIndex,episode:episodeIndex,matched:sourceIndex>=0,changed:catalogueKey(current)!==catalogueKey(next)};
  }
  function statusText(state){
    var message=typeof state.message==='string'?state.message.trim():'';
    switch(state.status){
      case 'queued':case 'running':return '后台查找可用片源…检测 '+(Number(state.checked)||0)+'/'+(Number(state.total)||0);
      case 'done':return message?'后台查找：'+message:Number(state.added)>0?'后台查找完成，新增 '+state.added+' 条片源':Number(state.updated)>0?'后台查找完成，已更新 '+state.updated+' 条片源':'后台查找完成，暂无新增可用片源';
      case 'busy':return state.retrying?'后台片源查找繁忙，稍后重试':message?'后台片源查找：'+message:'后台片源查找繁忙，请稍后刷新';
      case 'timeout':return '后台查找仍在继续，可稍后刷新查看结果';
      case 'error':return message?'后台查找未完成：'+message:'后台片源查找暂不可用，请稍后重试';
      case 'disabled':return '';
      default:return '后台查找可用片源…';
    }
  }
  function start(options){
    options=options||{};
    var urls=options.urls||{},stopped=false,timer=null,deadline=null,requestTimer=null,controller=null,retriedBusy=false,revision='';
    var request=options.fetch||root.fetch.bind(root),maxMs=Math.max(1,Number(options.maxMs)||600000),interval=Math.max(1,Number(options.intervalMs)||2000),queuedInterval=Math.max(1,Number(options.queuedIntervalMs)||5000);
    function stop(){if(stopped)return;stopped=true;clearTimeout(timer);clearTimeout(deadline);clearTimeout(requestTimer);if(controller)controller.abort();controller=null;}
    function notify(state){if(!stopped&&options.onState)options.onState(state,statusText(state));}
    function sources(state){
      if(stopped||!Array.isArray(state.sources))return;
      if(!state.source_revision||state.source_revision!==revision){if(options.onSources)options.onSources(state.sources,state.source_revision||'');}
      if(state.source_revision)revision=state.source_revision;
    }
    function schedule(fn,delay){if(!stopped)timer=setTimeout(fn,delay);}
    async function read(url,method){
      controller=new AbortController();
      var active=controller;
      requestTimer=setTimeout(function(){active.abort();},12000);
      try{
        var response=await request(url,{method:method,credentials:'same-origin',cache:'no-store',signal:active.signal});
        var body=await response.json();
        if(!response.ok||body.code!==undefined&&body.code!==0&&body.code!==1||!body.data)throw Error('discovery request failed');
        return body.data;
      }finally{clearTimeout(requestTimer);if(controller===active)controller=null;}
    }
    function accept(state,fromStart){
      if(stopped)return;
      notify(state);sources(state);
      if(state.status==='disabled'){stop();return;}
      if(state.status==='queued'||state.status==='running'){schedule(poll,state.status==='queued'?queuedInterval:interval);return;}
      if(state.status==='busy'&&!retriedBusy){retriedBusy=true;notify(Object.assign({},state,{retrying:true}));schedule(begin,5000);return;}
      // POST returns counters only; one GET also refreshes a cached/completed
      // lookup's source inventory without starting another collection task.
      if(fromStart){schedule(poll,0);return;}
      stop();
    }
    function failed(){if(!stopped){notify({status:'error'});stop();}}
    async function begin(){try{accept(await read(urls.start,'POST'),true);}catch(_){failed();}}
    async function poll(){
      var url=urls.status+(revision?(urls.status.indexOf('?')>=0?'&':'?')+'revision='+encodeURIComponent(revision):'');
      try{accept(await read(url,'GET'),false);}catch(_){failed();}
    }
    if(!urls.start||!urls.status)return {cancel:stop};
    deadline=setTimeout(function(){notify({status:'timeout'});stop();},maxMs);
    notify({status:'queued'});begin();
    return {cancel:stop};
  }
  root.SuxinSourceDiscovery={start:start,reconcile:reconcile,statusText:statusText};
  if(typeof module!=='undefined'&&module.exports)module.exports=root.SuxinSourceDiscovery;
})(typeof window!=='undefined'?window:globalThis);
