/* One numbered chapter ahead, shared by the button, autoplay and preloading. */
(function(root){
  'use strict';
  function identity(target){
    return target ? JSON.stringify([target.code,target.item.key,target.item.url||target.item.psrc||target.item.iframe,target.quality||'']) : '';
  }
  function findNext(sources,sourceIndex,episodeIndex){
    sources=Array.isArray(sources)?sources:[];
    var currentSource=sources[sourceIndex];
    var current=currentSource&&Array.isArray(currentSource.episodes)&&currentSource.episodes[episodeIndex];
    if(!current||!Number.isInteger(current.number)||current.number<0)return null;
    var key='episode:'+(current.number+1);
    for(var offset=0;offset<sources.length;offset++){
      var index=(sourceIndex+offset)%sources.length,src=sources[index],found=-1;
      if(!src||src.activeOnly||!Array.isArray(src.episodes))continue;
      if(currentSource.version_key&&src.version_key&&currentSource.version_key!==src.version_key)continue;
      src.episodes.forEach(function(item,i){
        if(item.key===key&&!item.activeOnly&&(item.psrc||item.iframe))found=i;
      });
      if(found>=0)return {source:index,episode:found,code:src.code,item:src.episodes[found]};
    }
    return null;
  }

  // The factory uses the same resolver/player as foreground playback. A
  // cancelled/failed prefetch never changes the current chapter or reports
  // a playback failure to the source-health service.
  function createCache(options){
    options=options||{};
    var entry=null,serial=0,failedKey='';
    var timeout=Math.max(1000,Math.min(30000,Number(options.timeoutMs)||20000));
    var ttl=Math.max(1000,Math.min(300000,Number(options.ttlMs)||300000));
    function detach(e){
      root.clearTimeout(e.timer);root.clearTimeout(e.expiry);
      if(e.result&&e.readyListener){
        var video=e.result.player.video;
        video.removeEventListener('canplay',e.readyListener);
        video.removeEventListener('error',e.errorListener);
      }
      if(e.result&&e.result.player.opts)e.result.player.opts.onStreamError=e.originalError;
    }
    function destroy(e){
      if(!e)return;
      if(!e.controller.signal.aborted)e.controller.abort();
      detach(e);
      if(e.result&&!e.disposed){
        e.disposed=true;
        try{e.result.player.destroy();}catch(_){}
        if(e.result.element)e.result.element.remove();
      }
    }
    function cancel(){
      ++serial;var old=entry;entry=null;failedKey='';destroy(old);
    }
    function start(target){
      var key=identity(target);
      if(!key){cancel();return;}
      if(entry&&entry.key===key||failedKey===key)return;
      cancel();
      var e={key:key,target:target,seq:serial,controller:new AbortController(),result:null,ready:false,disposed:false};
      entry=e;
      function fail(){
        if(entry!==e)return;
        failedKey=key;entry=null;destroy(e);
      }
      e.timer=root.setTimeout(fail,timeout);
      Promise.resolve().then(function(){
        if(entry!==e||e.controller.signal.aborted)return null;
        return options.create(target,e.controller.signal);
      }).then(function(result){
        e.result=result;
        if(entry!==e||e.controller.signal.aborted){destroy(e);return;}
        var player=result&&result.player,video=player&&player.video;
        if(!video){fail();return;}
        video.muted=true;video.preload='auto';
        e.originalError=player.opts.onStreamError;
        e.errorListener=fail;
        e.readyListener=function(){
          if(entry!==e||e.ready)return;
          if(video.error||player._failed){fail();return;}
          if(video.readyState>=3){
            e.ready=true;e.preparedAt=Date.now();root.clearTimeout(e.timer);
            if(result.element)result.element.dataset.sxNextPreload='ready';
            e.expiry=root.setTimeout(function(){if(entry===e)cancel();},ttl);
          }
        };
        player.opts.onStreamError=fail;
        video.addEventListener('canplay',e.readyListener);
        video.addEventListener('error',e.errorListener);
        e.readyListener();
      }).catch(function(){if(entry===e)fail();});
    }
    function take(target){
      var e=entry;
      if(!e||!e.ready||e.key!==identity(target)||e.result.player._failed||e.result.player.video.error||Date.now()-e.preparedAt>ttl)return null;
      entry=null;++serial;failedKey='';detach(e);
      if(typeof e.result.resume==='function')e.result.resume();
      return e.result;
    }
    return {start:start,take:take,cancel:cancel,state:function(){return entry?{key:entry.key,ready:entry.ready}:null;}};
  }
  root.SuxinEpisodeNext={findNext:findNext,identity:identity,createCache:createCache};
  if(typeof module!=='undefined'&&module.exports)module.exports=root.SuxinEpisodeNext;
})(typeof window!=='undefined'?window:globalThis);
