/**
 * 速信影视播放器 SuxinPlayer (内置)
 * 支持直连 m3u8(HLS)/mp4,基于 hls.js;记忆播放位置;自动下一集;投屏
 */
function SuxinPlayer(opts){
  this.opts=Object.assign({container:null,src:'',type:'',poster:'',start:0,nextUrl:'',hasNext:false,vodId:0,episode:1,onProgress:null},opts);
  this.video=null;
  this.hls=null;
  this._destroyed=false;
  this._failed=false;
  this._startupTimer=null;
  this._nativeMSE=null;
  this._nativeAbort=null;
  this._nativeObjectURL=null;
  this._nativeSeekTimer=null;
  this._nativeSwitching=false;
  this._qualityLevels=[];
  this._qualitySelect=null;
  this._nextButton=null;
  this._qualityManifestParsed=false;
  this._keyHandler=null;
  this._controlsCleanup=null;
  this._pageActive=!(document.hidden||document.visibilityState==='hidden');
  this._pageSuspended=false;
  this._resumePending=false;
  this._resumeWasPlaying=false;
  this._resumeGraceUntil=0;
  this._pendingStreamFailure=null;
  this._pageCleanup=null;
  this._watchPlayback=null;
  this._stopPlaybackWatch=null;
  this._init();
}
SuxinPlayer.prototype._isPageActive=function(){
  return this._pageActive!==false&&!document.hidden&&document.visibilityState!=='hidden';
};
// A background tab can suspend decoders and timers without losing its source.
// Keep its media element/session intact, then measure failure only on return.
SuxinPlayer.prototype.setPageActive=function(active){
  if(this._destroyed)return this;
  if(!active){
    if(!this._pageSuspended){this._resumeWasPlaying=Boolean(this.video&&!this.video.paused&&!this.video.ended);this._resumePosition=Number(this.video&&this.video.currentTime)||0;}
    this._pageSuspended=true;this._pageActive=false;
    if(this._stopPlaybackWatch)this._stopPlaybackWatch();
    return this;
  }
  if(document.hidden||document.visibilityState==='hidden')return this;
  this._pageActive=true;
  if(!this._pageSuspended)return this;
  this._pageSuspended=false;this._resumePending=true;this._resumeGraceUntil=Date.now()+20000;
  var v=this.video,self=this;
  // Restart an exhausted loader on the SAME URL; do not reconstruct the player
  // or fetch a new native session simply because the browser became visible.
  if(this._pendingStreamFailure&&this.hls&&typeof this.hls.startLoad==='function'){
    try{this.hls.startLoad(Number(v&&v.currentTime)||this._resumePosition||0);}catch(_){}
  }
  if(this._resumeWasPlaying&&v&&!v.ended){
    if(v.paused){try{var promise=v.play();if(promise&&promise.catch)promise.catch(function(){if(self._stopPlaybackWatch)self._stopPlaybackWatch();});}catch(_){} }
    if(this._watchPlayback)this._watchPlayback();
  }
  return this;
};
SuxinPlayer.prototype._bindPageLifecycle=function(){
  var self=this;
  function visibility(){self.setPageActive(!document.hidden&&document.visibilityState!=='hidden');}
  function hide(){self.setPageActive(false);}
  function show(){self.setPageActive(true);}
  document.addEventListener('visibilitychange',visibility);
  if(window.addEventListener){window.addEventListener('pagehide',hide);window.addEventListener('pageshow',show);}
  this._pageCleanup=function(){document.removeEventListener('visibilitychange',visibility);if(window.removeEventListener){window.removeEventListener('pagehide',hide);window.removeEventListener('pageshow',show);}};
  if(!this._isPageActive())this.setPageActive(false);
};
SuxinPlayer.prototype._fail=function(context){
  if(this._destroyed||this._failed)return;
  context=context||{};
  if(!this._isPageActive()){this._pendingStreamFailure=context;this.setPageActive(false);return;}
  if(this._resumePending&&(Date.now()<this._resumeGraceUntil||this.video&&this.video.paused)){
    this._pendingStreamFailure=context;
    if(this._watchPlayback)this._watchPlayback();
    return;
  }
  if(this._resumePending)context=Object.assign({},context,{phase:'resume',position:Number(this.video&&this.video.currentTime)||this._resumePosition||0,wasPlaying:this._resumeWasPlaying});
  this._failed=true;
  clearTimeout(this._startupTimer);
  var self=this;
  setTimeout(function(){if(self._destroyed)return;if(!self._isPageActive()){self._failed=false;self._pendingStreamFailure=context;self.setPageActive(false);return;}if(self.opts.onStreamError)self.opts.onStreamError(context);},0);
};
SuxinPlayer.prototype._init=function(){
  var o=this.opts,box=typeof o.container==='string'?document.querySelector(o.container):o.container;
  if(!box)return;
  var v=document.createElement('video');
  v.controls=true;v.playsInline=true;v.setAttribute('playsinline','');v.preload='metadata';
  // Native controls remain available until the custom control surface mounts.
  // iPhone's video-only fullscreen also uses its native control surface.
  v.muted=Boolean(o.muted);
  if(Number.isFinite(o.volume))v.volume=Math.max(0,Math.min(1,o.volume));
  if(Number(o.playbackRate)>0){v.defaultPlaybackRate=Number(o.playbackRate);v.playbackRate=Number(o.playbackRate);}
  if(o.poster)v.poster=o.poster;
  box.appendChild(v);
  this.video=v;
  var self=this;
  v.addEventListener('loadedmetadata',function(){
    if(!self._nativeMSE&&o.start>0&&o.start<v.duration-0.1){try{v.currentTime=o.start}catch(e){}}
    self._renderQuality();
  });
  v.addEventListener('ended',function(){ stopPlaybackWatch();var current=self.opts;if(typeof current.onNext==='function')current.onNext(); else if(current.nextUrl)location.href=current.nextUrl; });
  v.addEventListener('error',function(){ self._fail({reason:'media-error'}); });
  var watching=false,lastAdvance=0,lastPlaybackTime=0;
  function stopPlaybackWatch(){
    clearTimeout(self._startupTimer);
    self._startupTimer=null;
    watching=false;
  }
  function recordAdvance(){
    if(!v.seeking&&Math.abs(v.currentTime-lastPlaybackTime)>0.01){
      lastPlaybackTime=v.currentTime;
      lastAdvance=Date.now();
      if(self._isPageActive()){self._resumePending=false;self._pendingStreamFailure=null;}
    }
  }
  function checkPlayback(){
    self._startupTimer=setTimeout(function(){
      self._startupTimer=null;
      if(self._destroyed||self._failed||!self._isPageActive()||v.paused||v.ended){stopPlaybackWatch();return;}
      recordAdvance();
      if(Date.now()-lastAdvance>=20000){stopPlaybackWatch();self._fail();return;}
      checkPlayback();
    },1000);
  }
  function watchPlayback(){
    if(watching||self._destroyed||self._failed||!self._isPageActive()||v.paused||v.ended)return;
    watching=true;
    lastAdvance=Date.now();
    lastPlaybackTime=v.currentTime;
    checkPlayback();
  }
  this._watchPlayback=watchPlayback;
  this._stopPlaybackWatch=stopPlaybackWatch;
  this._bindPageLifecycle();
  // Only an advancing playback clock resets the deadline. Repeated buffering
  // events must not postpone a failed source indefinitely.
  v.addEventListener('play',watchPlayback);
  v.addEventListener('waiting',watchPlayback);
  v.addEventListener('stalled',watchPlayback);
  v.addEventListener('playing',watchPlayback);
  v.addEventListener('pause',stopPlaybackWatch);
  v.addEventListener('seeking',function(){
    lastAdvance=Date.now();lastPlaybackTime=v.currentTime;
    watchPlayback();
  });
  // 进度上报
  var lastSend=0;
  v.addEventListener('timeupdate',function(){
    recordAdvance();
    var now=Date.now();
    if(now-lastSend>15000&&v.currentTime>5){
      lastSend=now;
      if(o.onProgress)o.onProgress(Math.floor(v.currentTime));
    }
  });
  this._load(o.src,o.type);
  if(o.autoplay){
    var p=v.play();
    if(p&&p.catch){ p.catch(function(error){
      // A user pause/source replacement aborts play(); it must not restart it.
      if(self._destroyed||error&&error.name==='AbortError'){if(v.paused)stopPlaybackWatch();return;}
      // 浏览器拦截带声自动播放:静音起播+提示开声
      v.muted=true;
      var p2=v.play();
      if(p2&&p2.catch)p2.catch(function(){if(v.paused)stopPlaybackWatch();});
      self._showUnmute(v,box);
    }); }
  }
  // 键盘快捷键
  this._keyHandler=function(e){
    var active=document.activeElement;
    if(e.defaultPrevented||e.ctrlKey||e.metaKey||e.altKey||active&&(active.isContentEditable||['INPUT','TEXTAREA','SELECT','BUTTON'].indexOf(active.tagName)>=0))return;
    var action={Space:'play',KeyK:'play',KeyF:'fullscreen',KeyM:'mute',ArrowRight:'forward',ArrowLeft:'backward',ArrowUp:'louder',ArrowDown:'quieter'}[e.code];
    if(!action)return;
    e.preventDefault();
    if(self._controlAction){self._controlAction(action);return;}
    if(action==='play'){var promise=v.paused?v.play():v.pause();if(promise&&promise.catch)promise.catch(function(){});}
    if(action==='forward'||action==='backward')v.currentTime=Math.max(0,Math.min(Number.isFinite(v.duration)?v.duration:Infinity,v.currentTime+(action==='forward'?10:-10)));
  };
  document.addEventListener('keydown',this._keyHandler);
  this._enhance(v,box);
};
SuxinPlayer.prototype._load=function(src,type){
  var o=this.opts,v=this.video;
  if(!src)return;
  if(type==='mp4'&&/^\/suxinvideo\/native\/stream\?/.test(src)&&Number(o.duration)>0&&window.MediaSource&&MediaSource.isTypeSupported('video/mp4; codecs="avc1.42C028, mp4a.40.2"')){
    this._loadNativeMSE(src,Number(o.duration));
    return;
  }
  var isM3u8 = type==='m3u8' || type==='hls' || /m3u8/i.test(src);
  var ua = navigator.userAgent;
  var isIOS = /iPad|iPhone|iPod/.test(ua) || (navigator.platform === 'MacIntel' && navigator.maxTouchPoints > 1);
  var isSafari = /Safari/i.test(ua) && !/Chrome|CriOS|FxiOS|EdgiOS|Edg\//i.test(ua);
  // All iOS browsers can use WebKit's native HLS, including Chrome/CriOS.
  // Prefer it when available instead of probing a desktop-style MSE decoder.
  var nativeHLS=isIOS&&typeof v.canPlayType==='function'&&Boolean(v.canPlayType('application/vnd.apple.mpegurl'));
  if(isM3u8 && window.Hls && Hls.isSupported() && !nativeHLS){
    if(this.hls)this.hls.destroy();
    this.hls=new Hls({maxBufferLength:30,maxMaxBufferLength:60,maxBufferSize:60*1000*1000,manifestLoadingTimeOut:12000,manifestLoadingMaxRetry:1,levelLoadingTimeOut:12000,levelLoadingMaxRetry:1,fragLoadingTimeOut:20000,fragLoadingMaxRetry:2,startLevel:-1,abrEwmaDefaultEstimate:2000000});
    var slf=this;
    this.hls.on(Hls.Events.ERROR,function(e,data){
      // A failed segment may be retried or replaced by HLS while buffered
      // media keeps playing. Only an exhausted, fatal error requires a source
      // switch; the playback watchdog still detects a stream that stops moving.
      if(data&&data.fatal)slf._fail({reason:'hls-fatal',errorType:data.type||'',details:data.details||''});
    });
    this.hls.on(Hls.Events.MANIFEST_PARSED,function(){
      slf._qualityManifestParsed=true;
      slf._qualityLevels=(slf.hls&&slf.hls.levels)||[];
      slf._renderQuality();
    });
    this.hls.loadSource(src);
    this.hls.attachMedia(v);
  }else{
    v.src=src;
  }
};
SuxinPlayer.prototype._loadNativeMSE=function(src,duration){
  var self=this,v=this.video;
  this._nativeMSE={src:src,duration:duration,seq:0};
  v.addEventListener('seeking',function(){
    if(self._destroyed||self._nativeSwitching||!self._nativeMSE)return;
    var target=v.currentTime,covered=false;
    for(var i=0;i<v.buffered.length;i++)if(target>=v.buffered.start(i)&&target<v.buffered.end(i)-0.05)covered=true;
    if(covered)return;
    clearTimeout(self._nativeSeekTimer);
    self._nativeSeekTimer=setTimeout(function(){self._startNativeStream(target,!v.paused);},120);
  });
  this._startNativeStream(Math.max(0,Math.min(Number(this.opts.start)||0,duration-0.2)),Boolean(this.opts.autoplay));
};
SuxinPlayer.prototype._startNativeStream=function(start,shouldPlay){
  var self=this,v=this.video,state=this._nativeMSE;
  if(!state||this._destroyed)return;
  var seq=++state.seq;
  if(this._nativeAbort)this._nativeAbort.abort();
  this._nativeAbort=new AbortController();
  var signal=this._nativeAbort.signal;
  if(this._nativeObjectURL){v.removeAttribute('src');v.load();URL.revokeObjectURL(this._nativeObjectURL);}
  var source=new MediaSource();
  this._nativeObjectURL=URL.createObjectURL(source);
  this._nativeSwitching=true;
  v.src=this._nativeObjectURL;
  source.addEventListener('sourceopen',async function(){
    if(signal.aborted||self._destroyed||seq!==state.seq)return;
    try{
      source.duration=state.duration;
      var buffer=source.addSourceBuffer('video/mp4; codecs="avc1.42C028, mp4a.40.2"');
      buffer.timestampOffset=start;
      var endpoint=state.src+(state.src.indexOf('?')<0?'?':'&')+'start='+encodeURIComponent(start.toFixed(3));
      var response=await fetch(endpoint,{signal:signal,credentials:'same-origin',cache:'no-store'});
      if(!response.ok||!response.body)throw Error('视频流加载失败（'+response.status+'）');
      var reader=response.body.getReader(),initialized=false;
      while(!signal.aborted&&seq===state.seq){
        var ahead=v.buffered.length?v.buffered.end(v.buffered.length-1)-v.currentTime:0;
        if(ahead>(v.paused?15:45)){await new Promise(function(resolve){setTimeout(resolve,150)});continue;}
        var chunk=await reader.read();
        if(chunk.done)break;
        await new Promise(function(resolve,reject){
          function clean(){buffer.removeEventListener('updateend',done);buffer.removeEventListener('error',failed);signal.removeEventListener('abort',cancelled)}
          function done(){clean();resolve()}
          function failed(){clean();reject(Error('视频缓冲失败'))}
          function cancelled(){clean();reject(Error('播放已切换'))}
          buffer.addEventListener('updateend',done,{once:true});buffer.addEventListener('error',failed,{once:true});signal.addEventListener('abort',cancelled,{once:true});
          try{buffer.appendBuffer(chunk.value)}catch(error){clean();reject(error)}
        });
        if(!initialized&&buffer.buffered.length){
          initialized=true;
          v.currentTime=Math.min(state.duration-0.1,Math.max(start,buffer.buffered.start(0)+0.03));
          self._nativeSwitching=false;
          if(shouldPlay)v.play().catch(function(){});
        }
        if(buffer.buffered.length&&v.currentTime-buffer.buffered.start(0)>45){
          await new Promise(function(resolve){
            function done(){resolve()}
            buffer.addEventListener('updateend',done,{once:true});
            try{buffer.remove(0,v.currentTime-30)}catch(_){buffer.removeEventListener('updateend',done);resolve()}
          });
        }
      }
      if(!signal.aborted&&seq===state.seq&&source.readyState==='open')source.endOfStream();
    }catch(error){
      if(!signal.aborted&&!self._destroyed&&seq===state.seq)self._fail();
    }
  },{once:true});
};
SuxinPlayer.prototype.destroy=function(){
  this._destroyed=true;
  if(this._nextButton){this._nextButton.hidden=true;this._nextButton.disabled=true;}
  if(this._keyHandler)document.removeEventListener('keydown',this._keyHandler);
  if(this._controlsCleanup)this._controlsCleanup();
  if(this._pageCleanup)this._pageCleanup();
  clearTimeout(this._startupTimer);
  clearTimeout(this._nativeSeekTimer);
  if(this._nativeAbort)this._nativeAbort.abort();
  if(this._nativeObjectURL)URL.revokeObjectURL(this._nativeObjectURL);
  if(this.hls){try{this.hls.destroy()}catch(e){} this.hls=null;}
  if(this.video){try{this.video.pause()}catch(e){} this.video.removeAttribute('src'); this.video.load();}
};
// The page owns episode lookup. Update both the control and options so a
// resolution replacement inherits the latest availability and callback.
SuxinPlayer.prototype.setNextEpisode=function(hasNext,onNext){
  this.opts.hasNext=Boolean(hasNext);
  if(arguments.length>1)this.opts.onNext=typeof onNext==='function'?onNext:null;
  var enabled=!this._destroyed&&this.opts.hasNext&&typeof this.opts.onNext==='function';
  if(this._nextButton){this._nextButton.hidden=!enabled;this._nextButton.disabled=!enabled;}
  return this;
};
SuxinPlayer.prototype._showUnmute=function(v,box){
  if(!box||box.querySelector('.sx-unmute'))return;
  var b=document.createElement('button');
  b.type='button';b.className='sx-unmute sx-plx-btn';
  b.textContent='开声';b.setAttribute('aria-label','点击开启声音');
  b.addEventListener('click',function(e){ e.stopPropagation(); v.muted=false; v.volume=1; if(b.parentNode)b.parentNode.removeChild(b); });
  var toolbar=box.querySelector('.sx-control-row')||box.querySelector('.sx-plx');
  if(toolbar)toolbar.insertBefore(b,toolbar.querySelector('.sx-fullscreen'));
  else box.appendChild(b);
};
// Page expansion is explicitly different from browser fullscreen. Its state
// belongs to the stable frame, so switching resolution/episode keeps it alive.
SuxinPlayer.prototype._pageFullscreen=function(target,on){
  var state=target._sxPageFullscreen;
  if(on&&!state){
    state={overflow:document.documentElement.style.overflow,bodyOverflow:document.body.style.overflow};
    target._sxPageFullscreen=state;
    document.documentElement.style.overflow='hidden';document.body.style.overflow='hidden';
    state.close=function(){SuxinPlayer.prototype._pageFullscreen(target,false);};
    state.key=function(e){if(e.key==='Escape')state.close();};
    document.addEventListener('keydown',state.key);
    window.addEventListener('pagehide',state.close);
    window.addEventListener('popstate',state.close);
    state.detached=new MutationObserver(function(){if(!target.isConnected)state.close();});
    state.detached.observe(document.documentElement,{childList:true,subtree:true});
    target.classList.add('sx-fullscreen-frame','sx-player-expanded','sx-page-fullscreen');
  }else if(!on&&state){
    document.documentElement.style.overflow=state.overflow;document.body.style.overflow=state.bodyOverflow;
    document.removeEventListener('keydown',state.key);
    window.removeEventListener('pagehide',state.close);
    window.removeEventListener('popstate',state.close);
    state.detached.disconnect();
    target.classList.remove('sx-page-fullscreen','sx-player-expanded');
    delete target._sxPageFullscreen;
  }
  document.dispatchEvent(new Event('sxpagefullscreenchange'));
};
// This frame listener outlives individual video instances. A failed source or
// automatic episode replacement must still restore the frame when the browser
// exits fullscreen with its own Back/Escape UI.
SuxinPlayer.prototype._watchFullscreen=function(target){
  if(target._sxFullscreenWatch)return target._sxFullscreenWatch;
  var state={};
  state.refresh=function(){
    var active=document.fullscreenElement===target||document.webkitFullscreenElement===target;
    target.classList.toggle('sx-player-expanded',active||target.classList.contains('sx-page-fullscreen'));
    if(!active){
      document.removeEventListener('fullscreenchange',state.refresh);
      document.removeEventListener('webkitfullscreenchange',state.refresh);
      delete target._sxFullscreenWatch;
    }
  };
  target._sxFullscreenWatch=state;
  document.addEventListener('fullscreenchange',state.refresh);
  document.addEventListener('webkitfullscreenchange',state.refresh);
  return state;
};
/* ===== 浮层播放器控件：不占用视频或全屏画面的高度 ===== */
SuxinPlayer.prototype._enhance=function(v,box){
  if(!box||box.querySelector('.sx-plx'))return;
  var self=this,o=this.opts;
  if(getComputedStyle(box).position==='static')box.style.position='relative';
  box.classList.add('sx-player-controls');
  if(!document.getElementById('sx-plx-style')){
    var st=document.createElement('style');st.id='sx-plx-style';
    st.textContent=`
.sx-player-controls{position:relative;display:block!important;min-width:0!important;min-height:0!important;box-sizing:border-box;background:#000;isolation:isolate;overflow:hidden}
.sx-player-controls>video{display:block!important;position:absolute!important;inset:0!important;width:100%!important;height:100%!important;min-width:0!important;min-height:0!important;max-width:none!important;max-height:none!important;object-fit:contain;background:#000}
.sx-player-controls .sx-plx{position:absolute;left:0;right:0;bottom:0;z-index:8;box-sizing:border-box;min-width:0;width:100%;padding:16px max(10px,env(safe-area-inset-right)) max(6px,env(safe-area-inset-bottom)) max(10px,env(safe-area-inset-left));color:#fff;background:linear-gradient(transparent,rgba(0,0,0,.75));opacity:1;transition:opacity .2s;user-select:none}
.sx-player-controls.sx-controls-hidden .sx-plx{opacity:0;pointer-events:none}
.sx-player-controls.sx-controls-hidden{cursor:none}
.sx-player-controls .sx-control-row{display:flex;align-items:center;gap:7px;min-width:0;height:36px}
.sx-player-controls .sx-plx-btn{display:inline-flex;align-items:center;justify-content:center;flex:0 0 auto;box-sizing:border-box;min-width:32px;min-height:32px;width:32px;height:32px;padding:5px;margin:0;color:#fff;background:transparent;border:0;border-radius:4px;cursor:pointer;font-family:inherit;font-size:12px;line-height:20px;white-space:nowrap}
.sx-player-controls .sx-plx-btn:hover{background:#ffffff20}
.sx-player-controls .sx-plx-btn svg{width:22px;height:22px;fill:currentColor;stroke:none;pointer-events:none}
.sx-player-controls .sx-plx-btn[hidden],.sx-player-controls .sx-setting[hidden],.sx-player-controls .sx-control-status[hidden]{display:none!important}
.sx-player-controls .sx-plx-btn:focus-visible,.sx-player-controls select:focus-visible,.sx-player-controls input:focus-visible{outline:2px solid #39b9ff;outline-offset:2px}
.sx-player-controls .sx-control-spacer{flex:1 1 auto;min-width:0}
.sx-player-controls .sx-time{flex:0 0 auto;min-width:84px;font-size:12px;line-height:20px;white-space:nowrap;font-variant-numeric:tabular-nums;text-shadow:0 1px 2px #000}
.sx-player-controls .sx-volume{flex:0 1 65px;width:65px;min-width:30px;height:20px;margin:0;accent-color:#fff;cursor:pointer}
.sx-player-controls .sx-timeline{height:20px;position:relative;margin:0 4px;cursor:pointer}
.sx-player-controls .sx-track{position:absolute;top:9px;left:0;right:0;height:3px;background:#ffffff45;border-radius:2px;overflow:hidden;pointer-events:none}
.sx-player-controls .sx-buffer,.sx-player-controls .sx-played{position:absolute;inset:0 auto 0 0;width:0;height:100%;background:#ffffff70}
.sx-player-controls .sx-played{background:#e83b73}
.sx-player-controls .sx-seek{position:absolute;inset:0;width:100%;height:20px;margin:0;cursor:pointer;appearance:none;-webkit-appearance:none;background:transparent;border:0;outline:none;touch-action:pan-y}
.sx-player-controls .sx-seek::-webkit-slider-runnable-track{height:3px;background:transparent}
.sx-player-controls .sx-seek::-webkit-slider-thumb{appearance:none;-webkit-appearance:none;width:11px;height:11px;margin-top:-4px;border:0;border-radius:50%;background:#e83b73;box-shadow:0 0 3px #000;opacity:0}
.sx-player-controls .sx-seek::-moz-range-track{height:3px;background:transparent}
.sx-player-controls .sx-seek::-moz-range-thumb{width:11px;height:11px;border:0;border-radius:50%;background:#e83b73;opacity:0}
.sx-player-controls .sx-timeline:hover .sx-seek::-webkit-slider-thumb,.sx-player-controls .sx-seek:focus-visible::-webkit-slider-thumb{opacity:1}
.sx-player-controls .sx-timeline:hover .sx-seek::-moz-range-thumb,.sx-player-controls .sx-seek:focus-visible::-moz-range-thumb{opacity:1}
.sx-player-controls .sx-seek:disabled{cursor:default}
.sx-player-controls .sx-settings-panel{display:flex;align-items:center;gap:7px}
.sx-player-controls .sx-settings-toggle{display:none}
.sx-player-controls .sx-setting{display:flex;align-items:center;gap:6px;margin:0}
.sx-player-controls .sx-setting>span{display:none}
.sx-player-controls .sx-quality,.sx-player-controls .sx-rate,.sx-player-controls .sx-aspect,.sx-player-controls .sx-subtitle-offset{min-width:0;max-width:155px;height:30px;margin:0;padding:3px 5px;border:0;border-radius:4px;background:#171717aa;color:#fff;font-family:inherit;font-size:12px;cursor:pointer;text-overflow:ellipsis}
.sx-player-controls .sx-rate{max-width:70px}.sx-player-controls .sx-aspect{max-width:90px}.sx-player-controls .sx-subtitle-offset{width:55px}
.sx-player-controls select option{background:#202020;color:#fff}
.sx-player-controls select:disabled{opacity:.8;cursor:default}
.sx-player-controls .sx-unmute{position:static;left:auto;top:auto;right:auto;transform:none;backdrop-filter:none;z-index:auto;width:auto;min-width:40px;padding:4px 6px;background:#0009}
.sx-player-controls .sx-control-status{position:absolute;left:10px;right:10px;bottom:80px;z-index:9;box-sizing:border-box;margin:0;padding:7px 10px;border-radius:5px;background:#000c;color:#fff;font-size:12px;line-height:1.5;overflow-wrap:anywhere;text-align:center;pointer-events:none}
.sx-player-controls .sx-center-play{position:absolute;z-index:7;left:50%;top:50%;width:56px;height:56px;transform:translate(-50%,-50%);background:#0008;border:1px solid #ffffff70;border-radius:50%;display:none}
.sx-player-controls .sx-center-play svg{width:30px;height:30px}
.sx-player-controls.sx-paused .sx-center-play{display:flex}
.sx-fullscreen-frame.sx-player-expanded,.sx-fullscreen-frame:fullscreen,.sx-fullscreen-frame:-webkit-full-screen{box-sizing:border-box;width:100vw!important;height:100vh!important;height:100dvh!important;min-width:0!important;min-height:0!important;max-width:none!important;max-height:none!important;margin:0!important;padding:0!important;border:0!important;border-radius:0!important;aspect-ratio:auto!important;box-shadow:none!important;background:#000}
.sx-fullscreen-frame.sx-player-expanded>#suxinplayer{position:absolute!important;inset:0!important;width:100%!important;height:100%!important;min-height:0!important;max-height:none!important;margin:0!important;padding:0!important;aspect-ratio:auto!important}
.sx-fullscreen-frame.sx-page-fullscreen{position:fixed!important;inset:0!important;z-index:2147483647!important;transform:none!important}
.sx-player-controls.sx-player-compact .sx-control-row{gap:4px}.sx-player-controls.sx-player-compact .sx-volume{display:none}.sx-player-controls.sx-player-compact .sx-settings-toggle{display:inline-flex}.sx-player-controls.sx-player-compact .sx-settings-panel{position:absolute;right:max(8px,env(safe-area-inset-right));bottom:58px;display:none;flex-direction:column;align-items:stretch;width:210px;max-width:calc(100% - 16px);max-height:var(--sx-settings-height,calc(100dvh - 85px));overflow:auto;box-sizing:border-box;padding:10px;border:1px solid #ffffff25;border-radius:7px;background:#171717f5;box-shadow:0 2px 15px #0008}.sx-player-controls.sx-player-compact.sx-settings-open .sx-settings-panel{display:flex}.sx-player-controls.sx-player-compact .sx-setting{justify-content:space-between;min-height:35px}.sx-player-controls.sx-player-compact .sx-setting>span{display:block;font-size:12px}.sx-player-controls.sx-player-compact .sx-setting select{max-width:145px}.sx-player-controls.sx-player-compact .sx-settings-panel>.sx-plx-btn{width:100%;justify-content:flex-start;gap:10px}.sx-player-controls.sx-player-compact .sx-settings-panel>.sx-plx-btn:after{content:attr(data-label);font-size:12px}.sx-player-controls.sx-player-compact .sx-time{min-width:80px;font-size:11px}.sx-player-controls.sx-player-compact .sx-plx{padding-left:max(7px,env(safe-area-inset-left));padding-right:max(7px,env(safe-area-inset-right))}
@media(prefers-reduced-motion:reduce){.sx-player-controls .sx-plx{transition:none}}
`;
    document.head.appendChild(st);
  }
  var icons={
    play:'<path d="M7 4v16l13-8z"/>',pause:'<path d="M6 4h4v16H6zm8 0h4v16h-4z"/>',
    next:'<path d="M5 4v16l12-8zM18 4h3v16h-3z"/>',
    sound:'<path d="M3 9v6h4l5 5V4L7 9zM16 7v3a3 3 0 0 1 0 4v3a6 6 0 0 0 0-10zM19 4v3a7 7 0 0 1 0 10v3a11 11 0 0 0 0-16z"/>',
    muted:'<path d="M3 9v6h4l5 5V4L7 9zM15.5 7.5l-1.4 1.4 3.1 3.1-3.1 3.1 1.4 1.4 3.1-3.1 3.1 3.1 1.4-1.4-3.1-3.1 3.1-3.1-1.4-1.4-3.1 3.1z"/>',
    cast:'<path d="M3 3h18v14h-8v-2h6V5H5v3H3zM2 10v2a10 10 0 0 1 10 10h2A12 12 0 0 0 2 10zm0 4v2a6 6 0 0 1 6 6h2a8 8 0 0 0-8-8zm0 4v4h4a4 4 0 0 0-4-4z"/>',
    full:'<path d="M3 3h7v2H5v5H3zm11 0h7v7h-2V5h-5zM3 14h2v5h5v2H3zm16 0h2v7h-7v-2h5z"/>',
    restore:'<path d="M8 3h2v7H3V8h5zm6 0h2v5h5v2h-7zM3 14h7v7H8v-5H3zm11 0h7v2h-5v5h-2z"/>',
    web:'<path d="M3 4h18v16H3zm2 2v12h14V6zM7 8h10v2H7z"/>',
    pip:'<path d="M3 4h18v16H3zm2 2v12h14V6zm6 5h6v5h-6z"/>',
    settings:'<path d="M10 2h4l.5 3 2 .9L19 4.5l2 3.5-2.2 1.9.2 2.1 2.2 1.9-2 3.5-2.5-1.4-2 .9-.5 3h-4l-.5-3-2-.9L5 17.4 3 14l2.2-2-.2-2-2.2-2L5 4.5l2.5 1.4 2-.9zM12 8a4 4 0 1 0 0 8 4 4 0 0 0 0-8z"/>'
  };
  function icon(button,name,label){button.innerHTML='<svg viewBox="0 0 24 24" aria-hidden="true">'+icons[name]+'</svg>';button.title=label;button.setAttribute('aria-label',label);button.setAttribute('data-label',label);}
  function button(name,className,label){var node=document.createElement('button');node.type='button';node.className='sx-plx-btn '+(className||'');icon(node,name,label);return node;}
  function label(text,select){var node=document.createElement('label'),name=document.createElement('span');node.className='sx-setting';name.textContent=text;node.append(name,select);return node;}
  var wrap=document.createElement('div');wrap.className='sx-plx';wrap.setAttribute('role','group');wrap.setAttribute('aria-label','播放器控制栏');
  var timeline=document.createElement('div');timeline.className='sx-timeline';
  var track=document.createElement('div');track.className='sx-track';
  var buffered=document.createElement('div');buffered.className='sx-buffer';
  var played=document.createElement('div');played.className='sx-played';track.append(buffered,played);
  var seek=document.createElement('input');seek.type='range';seek.className='sx-seek';seek.min='0';seek.max='1000';seek.step='1';seek.value='0';seek.setAttribute('aria-label','播放进度');
  timeline.append(track,seek);
  var row=document.createElement('div');row.className='sx-control-row';
  var play=button('play','sx-play-toggle','播放'),center=button('play','sx-center-play','播放');
  var next=button('next','sx-next-episode','播放下一集');
  var mute=button('sound','sx-mute','静音');
  var volume=document.createElement('input');volume.type='range';volume.className='sx-volume';volume.min='0';volume.max='1';volume.step='.05';volume.setAttribute('aria-label','音量');
  var time=document.createElement('span');time.className='sx-time';
  var spacer=document.createElement('span');spacer.className='sx-control-spacer';
  var settingsToggle=button('settings','sx-settings-toggle','播放设置');settingsToggle.setAttribute('aria-expanded','false');
  var settings=document.createElement('div');settings.className='sx-settings-panel';settings.setAttribute('role','group');settings.setAttribute('aria-label','播放设置');
  var rate=document.createElement('select');rate.className='sx-rate';rate.setAttribute('aria-label','播放速度');rate.title='播放速度';
  [.5,.75,1,1.25,1.5,1.75,2].forEach(function(value){var option=document.createElement('option');option.value=String(value);option.textContent=value===1?'正常':value+'倍速';rate.appendChild(option);});
  var quality=document.createElement('select');quality.className='sx-quality';quality.setAttribute('aria-label','播放清晰度');quality.title='播放清晰度';
  var aspect=document.createElement('select');aspect.className='sx-aspect';aspect.setAttribute('aria-label','画面比例');aspect.title='画面比例';
  [['original','原始比例'],['16:9','16:9'],['4:3','4:3'],['fill','铺满画面']].forEach(function(item){var option=document.createElement('option');option.value=item[0];option.textContent=item[1];aspect.appendChild(option);});
  var offset=document.createElement('input');offset.type='number';offset.className='sx-subtitle-offset';offset.min='-10';offset.max='10';offset.step='.5';offset.value=String(Number(o.subtitleOffset)||0);offset.setAttribute('aria-label','字幕偏移秒数');
  var offsetLabel=label('字幕偏移（秒）',offset);offsetLabel.hidden=true;
  var cast=button('cast','sx-cast','投屏'),pip=button('pip','sx-picture-in-picture','画中画'),web=button('web','sx-web-fullscreen','网页铺满'),full=button('full','sx-fullscreen','全屏');
  pip.hidden=Boolean(/Android|iPhone|iPad|iPod/i.test(navigator.userAgent)||navigator.platform==='MacIntel'&&navigator.maxTouchPoints>1)||!(document.pictureInPictureEnabled&&v.requestPictureInPicture||typeof v.webkitSetPresentationMode==='function'&&v.webkitSupportsPresentationMode&&v.webkitSupportsPresentationMode('picture-in-picture'));
  this._qualityLabel=label('清晰度',quality);
  settings.append(label('倍速',rate),this._qualityLabel,label('画面比例',aspect),offsetLabel,cast,pip,web);
  row.append(play,next,mute,volume,time,spacer,settingsToggle,settings,full);wrap.append(timeline,row);
  var status=document.createElement('p');status.className='sx-control-status';status.setAttribute('role','status');status.hidden=true;
  box.append(center,wrap,status);
  this._qualityWrap=settings;
  this._qualitySelect=quality;
  this._nextButton=next;
  this.setNextEpisode(o.hasNext,o.onNext);
  this._renderQuality();
  v.controls=false;
  var events=[],hideTimer=null,statusTimer=null,scrubTimer=null,toolbarHover=false,userScrubbing=false,keyboardFocus=false,selecting=false,settingsOpen=false,cueTimes=new WeakMap(),resizeWatch=null;
  function listen(node,name,handler,options){node.addEventListener(name,handler,options);events.push([node,name,handler,options]);}
  function mobilePointer(){return window.matchMedia&&window.matchMedia('(pointer:coarse)').matches;}
  function menu(on){settingsOpen=Boolean(on);box.classList.toggle('sx-settings-open',settingsOpen);settingsToggle.setAttribute('aria-expanded',String(settingsOpen));reveal();}
  function focused(){return keyboardFocus&&document.activeElement&&box.contains(document.activeElement);}
  function hide(){if(self._destroyed||v.paused||v.ended||settingsOpen||toolbarHover||userScrubbing||selecting||focused())return;box.classList.add('sx-controls-hidden');}
  function reveal(delay){if(self._destroyed)return;clearTimeout(hideTimer);box.classList.remove('sx-controls-hidden');hideTimer=setTimeout(hide,delay||2600);}
  function message(text){status.textContent=text;status.hidden=!text;clearTimeout(statusTimer);if(text)statusTimer=setTimeout(function(){status.hidden=true;status.textContent='';},7000);reveal();}
  function togglePlay(){if(v.paused||v.ended){var promise=v.play();if(promise&&promise.catch)promise.catch(function(error){if(!self._destroyed&&error&&error.name!=='AbortError')message('浏览器未允许播放，请再点击播放按钮。');});}else v.pause();reveal();}
  function duration(){return Number.isFinite(v.duration)&&v.duration>0?v.duration:Number(o.duration)>0?Number(o.duration):0;}
  function clock(seconds){seconds=Math.max(0,Math.floor(Number(seconds)||0));var h=Math.floor(seconds/3600),m=Math.floor(seconds/60)%60,s=seconds%60;return(h?h+':'+String(m).padStart(2,'0'):m)+':'+String(s).padStart(2,'0');}
  function subtitleOffset(){var any=false,shift=Number(o.subtitleOffset)||0;try{for(var i=0;i<v.textTracks.length;i++){var cues=v.textTracks[i].cues;if(!cues||!cues.length)continue;any=true;for(var j=0;j<cues.length;j++){var cue=cues[j],original=cueTimes.get(cue);if(!original){original=[cue.startTime,cue.endTime];cueTimes.set(cue,original);}var start=Math.max(0,original[0]+shift),end=Math.max(start+.001,original[1]+shift);if(cue.startTime!==start)cue.startTime=start;if(cue.endTime!==end)cue.endTime=end;}}}catch(_){}offsetLabel.hidden=!any;}
  function progress(preview){var total=duration(),position=preview===undefined?Number(v.currentTime)||0:preview,percent=total?Math.min(100,position/total*100):0;seek.disabled=!total;time.textContent=clock(position)+' / '+(total?clock(total):v.duration===Infinity?'直播':'--:--');played.style.width=percent+'%';if(!userScrubbing)seek.value=String(percent*10);seek.setAttribute('aria-valuetext',clock(position)+(total?'，总时长 '+clock(total):''));var end=0;try{for(var i=0;i<v.buffered.length;i++)end=Math.max(end,v.buffered.end(i));}catch(_){}buffered.style.width=(total?Math.min(100,end/total*100):0)+'%';subtitleOffset();}
  function updatePlayback(){box.classList.toggle('sx-paused',v.paused||v.ended);icon(play,v.paused||v.ended?'play':'pause',v.paused||v.ended?'播放':'暂停');reveal();}
  function updateVolume(){var silent=v.muted||v.volume===0;icon(mute,silent?'muted':'sound',silent?'开启声音':'静音');mute.setAttribute('aria-pressed',String(silent));volume.value=String(v.muted?0:v.volume);var unmute=box.querySelector('.sx-unmute');if(unmute&&!silent)unmute.remove();}
  function updateRate(){rate.value=String(v.playbackRate);}
  function updateAspect(){box.style.setProperty('--sx-settings-height',Math.max(0,box.clientHeight-75)+'px');box.classList.toggle('sx-player-compact',box.clientWidth<=800||/Android|iPhone|iPad|iPod/i.test(navigator.userAgent)||navigator.platform==='MacIntel'&&navigator.maxTouchPoints>1);var value=o.aspectRatio||'original';aspect.value=value;if(value==='16:9'||value==='4:3'){var parts=value.split(':'),ratio=Number(parts[0])/Number(parts[1]),width=Math.min(box.clientWidth,box.clientHeight*ratio),height=width/ratio;v.style.setProperty('inset','auto','important');v.style.setProperty('left','50%','important');v.style.setProperty('top','50%','important');v.style.setProperty('width',width+'px','important');v.style.setProperty('height',height+'px','important');v.style.setProperty('transform','translate(-50%,-50%)','important');v.style.setProperty('object-fit','fill','important');}else{['inset','left','top','width','height','transform','object-fit'].forEach(function(name){v.style.removeProperty(name);});v.style.setProperty('object-fit',value==='fill'?'cover':'contain','important');}}
  function move(seconds){var end=duration();try{v.currentTime=Math.max(0,Math.min(end||Infinity,(Number(v.currentTime)||0)+seconds));}catch(_){}progress();reveal();}
  function setVolume(value){v.volume=Math.max(0,Math.min(1,value));v.muted=v.volume===0;updateVolume();reveal();}
  listen(play,'click',togglePlay);listen(center,'click',togglePlay);
  listen(next,'click',function(e){e.preventDefault();e.stopPropagation();if(!self._destroyed&&self.opts.hasNext&&typeof self.opts.onNext==='function')self.opts.onNext();});
  listen(mute,'click',function(){v.muted=!v.muted;if(!v.muted&&v.volume===0)v.volume=.5;updateVolume();reveal();});
  listen(volume,'input',function(){setVolume(Number(volume.value));});
  listen(rate,'change',function(){v.playbackRate=Number(rate.value)||1;selecting=false;reveal();});
  listen(aspect,'change',function(){o.aspectRatio=aspect.value;updateAspect();selecting=false;reveal();});
  listen(offset,'change',function(){o.subtitleOffset=Math.max(-10,Math.min(10,Number(offset.value)||0));offset.value=String(o.subtitleOffset);subtitleOffset();reveal();});
  listen(seek,'pointerdown',function(){userScrubbing=true;reveal();});
  listen(seek,'input',function(){userScrubbing=true;progress(Number(seek.value)/1000*duration());reveal();});
  listen(seek,'change',function(){var position=Number(seek.value)/1000*duration();try{v.currentTime=position;}catch(_){}userScrubbing=false;progress();reveal();});
  listen(seek,'blur',function(){userScrubbing=false;progress();reveal();});
  listen(seek,'pointercancel',function(){userScrubbing=false;progress();reveal();});
  listen(document,'pointerup',function(){if(userScrubbing){clearTimeout(scrubTimer);scrubTimer=setTimeout(function(){userScrubbing=false;progress();reveal();},0);}});
  listen(settingsToggle,'click',function(){menu(!settingsOpen);});
  [quality,rate,aspect,offset].forEach(function(select){listen(select,'focus',function(){selecting=true;reveal();});listen(select,'blur',function(){selecting=false;reveal();});listen(select,'change',function(){selecting=false;reveal();});});
  // Touch focus/compatibility mouse events can reveal the overlay before click.
  // A background tap only reveals it; it must not immediately hide it again.
  listen(v,'click',function(e){if(e.pointerType==='touch'||mobilePointer())reveal();else togglePlay();});
  listen(v,'dblclick',function(e){if(mobilePointer())return;e.preventDefault();toggleFullscreen();});
  listen(box,'pointermove',function(e){if(e.pointerType!=='touch')reveal();});
  listen(box,'pointerdown',function(){keyboardFocus=false;});
  listen(box,'keydown',function(e){if(e.key==='Tab'){keyboardFocus=true;reveal();}if(e.key==='Escape'&&settingsOpen){e.preventDefault();menu(false);settingsToggle.focus();}});
  listen(box,'focusin',function(){reveal();});listen(box,'focusout',function(){reveal();});
  listen(document,'keydown',function(e){if(e.key==='Tab')keyboardFocus=true;});
  listen(box,'pointerleave',function(){toolbarHover=false;reveal(800);});
  listen(wrap,'pointerenter',function(e){if(e.pointerType!=='touch'){toolbarHover=true;reveal();}});
  listen(wrap,'pointerleave',function(){toolbarHover=false;reveal();});
  listen(document,'pointerdown',function(e){keyboardFocus=false;var active=document.activeElement;if(selecting&&active&&active!==e.target&&box.contains(active)){selecting=false;if(active.blur)active.blur();}if(settingsOpen&&!settings.contains(e.target)&&e.target!==settingsToggle&&!settingsToggle.contains(e.target))menu(false);});
  ['timeupdate','durationchange','loadedmetadata','progress','seeking','seeked'].forEach(function(name){listen(v,name,function(){if(!userScrubbing)progress();});});
  ['play','playing','pause','ended'].forEach(function(name){listen(v,name,updatePlayback);});
  listen(v,'volumechange',updateVolume);listen(v,'ratechange',updateRate);
  if(window.ResizeObserver){resizeWatch=new ResizeObserver(updateAspect);resizeWatch.observe(box);}else listen(window,'resize',updateAspect);
  if(v.textTracks&&v.textTracks.addEventListener){listen(v.textTracks,'addtrack',subtitleOffset);listen(v.textTracks,'change',subtitleOffset);}
  function fullscreenTarget(){return box.closest('#playerBox')||box;}
  function fullscreenRequest(target){if(target.requestFullscreen&&document.fullscreenEnabled!==false)return target.requestFullscreen;if(target.webkitRequestFullscreen&&document.webkitFullscreenEnabled!==false)return target.webkitRequestFullscreen;return null;}
  var target=fullscreenTarget(),usePageFullscreen=!(fullscreenRequest(target)||v.webkitEnterFullscreen);
  function nativeFullscreen(){var target=fullscreenTarget();return document.fullscreenElement===target||document.webkitFullscreenElement===target||Boolean(v.webkitDisplayingFullscreen);}
  function refreshFullscreen(){var target=fullscreenTarget(),page=target.classList.contains('sx-page-fullscreen'),active=nativeFullscreen();target.classList.toggle('sx-player-expanded',page||active);icon(full,page&&usePageFullscreen||active?'restore':'full',active?'退出全屏':page&&usePageFullscreen?'退出铺满':usePageFullscreen?'网页铺满':'全屏');full.setAttribute('aria-pressed',String(active||page&&usePageFullscreen));icon(web,page?'restore':'web',page?'退出网页铺满':'网页铺满');web.setAttribute('aria-pressed',String(page));updateAspect();reveal();}
  function pageFullscreen(on){self._pageFullscreen(fullscreenTarget(),on);refreshFullscreen();}
  function fullscreenDenied(){var target=fullscreenTarget();if(target._sxFullscreenWatch)target._sxFullscreenWatch.refresh();if(self._destroyed)return;usePageFullscreen=true;pageFullscreen(true);message('浏览器未允许系统全屏，已切换网页铺满；浏览器地址栏可能保留。');}
  function exitFullscreen(after){try{var exiting;if(v.webkitDisplayingFullscreen&&v.webkitExitFullscreen){v.webkitExitFullscreen();if(after)after();return;}var exit=document.exitFullscreen||document.webkitExitFullscreen;if(exit){exiting=exit.call(document);if(exiting&&exiting.then)exiting.then(function(){if(after)after();}).catch(function(){message('未能退出全屏，请使用浏览器返回键或 Esc。');});else if(after)after();}else if(after)after();}catch(_){message('未能退出全屏，请使用浏览器返回键或 Esc。');}}
  function toggleFullscreen(){menu(false);var target=fullscreenTarget();if(nativeFullscreen()){exitFullscreen();return;}if(usePageFullscreen){pageFullscreen(!target.classList.contains('sx-page-fullscreen'));return;}if(target.classList.contains('sx-page-fullscreen'))pageFullscreen(false);
    // Request fullscreen before awaiting play() or URL resolution can consume
    // the trusted click/keydown gesture.
    try{var request=fullscreenRequest(target);if(request){target.classList.add('sx-fullscreen-frame');self._watchFullscreen(target);var entering=request.call(target,{navigationUI:'hide'});if(entering&&entering.catch)entering.catch(fullscreenDenied);}else if(v.webkitEnterFullscreen){if(v.readyState<1){message('视频加载完成后可进入全屏，请稍后再点。');return;}v.controls=true;v.webkitEnterFullscreen();}else fullscreenDenied();}catch(_){fullscreenDenied();}}
  listen(full,'click',toggleFullscreen);
  listen(web,'click',function(){menu(false);var on=!fullscreenTarget().classList.contains('sx-page-fullscreen');if(nativeFullscreen())exitFullscreen(function(){pageFullscreen(on);});else pageFullscreen(on);});
  ['fullscreenchange','webkitfullscreenchange','sxpagefullscreenchange'].forEach(function(name){listen(document,name,refreshFullscreen);});
  listen(v,'webkitbeginfullscreen',function(){v.controls=true;refreshFullscreen();});
  listen(v,'webkitendfullscreen',function(){v.controls=false;refreshFullscreen();});
  function refreshPip(){icon(pip,document.pictureInPictureElement===v||v.webkitPresentationMode==='picture-in-picture'?'restore':'pip',document.pictureInPictureElement===v||v.webkitPresentationMode==='picture-in-picture'?'退出画中画':'画中画');reveal();}
  listen(pip,'click',function(){menu(false);try{var promise;if(document.pictureInPictureElement===v)promise=document.exitPictureInPicture();else if(document.pictureInPictureEnabled&&v.requestPictureInPicture)promise=v.requestPictureInPicture();else if(typeof v.webkitSetPresentationMode==='function')v.webkitSetPresentationMode(v.webkitPresentationMode==='picture-in-picture'?'inline':'picture-in-picture');if(promise&&promise.catch)promise.catch(function(){message('当前视频或浏览器暂不支持画中画。');});}catch(_){message('当前视频或浏览器暂不支持画中画。');}});
  ['enterpictureinpicture','leavepictureinpicture','webkitpresentationmodechanged'].forEach(function(name){listen(v,name,refreshPip);});
  listen(cast,'click',function(e){e.stopPropagation();menu(false);var src=v.currentSrc||o.src||'',isMobile=/Android|iPhone|iPad|iPod/i.test(navigator.userAgent);try{if(v.remote&&typeof v.remote.prompt==='function'){v.remote.prompt().then(function(){},function(){try{if(typeof v.webkitShowPlaybackTargetPicker==='function'){v.webkitShowPlaybackTargetPicker();return;}}catch(_){}fallback(src,isMobile);});return;}}catch(_){}try{if(typeof v.webkitShowPlaybackTargetPicker==='function'){v.webkitShowPlaybackTargetPicker();return;}}catch(_){}fallback(src,isMobile);});
  function fallback(src,isMobile){var tip=isMobile?'未找到可投屏设备。\n\n请确认手机与电视连接同一 WiFi，电视支持 Chromecast / AirPlay / DLNA。\n\n可复制播放地址，在电视浏览器中打开：':'当前浏览器不支持一键投屏。\n\n可复制播放地址，在智能电视浏览器中打开：';prompt(tip,src);}
  this._controlAction=function(action){if(self._destroyed)return;if(action==='play')togglePlay();else if(action==='fullscreen')toggleFullscreen();else if(action==='mute')mute.click();else if(action==='forward')move(10);else if(action==='backward')move(-10);else if(action==='louder')setVolume((v.muted?0:v.volume)+.05);else if(action==='quieter')setVolume((v.muted?0:v.volume)-.05);reveal();};
  this._controlsCleanup=function(){clearTimeout(hideTimer);clearTimeout(statusTimer);clearTimeout(scrubTimer);if(resizeWatch)resizeWatch.disconnect();events.forEach(function(event){event[0].removeEventListener(event[1],event[2],event[3]);});self._controlAction=null;box.classList.remove('sx-controls-hidden','sx-settings-open','sx-paused');
    // Fullscreen belongs to the stable frame and survives quality/episode/source
    // replacement; its existing frame watcher restores browser Back/Escape.
  };
  progress();updatePlayback();updateVolume();updateRate();updateAspect();refreshFullscreen();
};

SuxinPlayer.prototype._renderQuality=function(){
  var self=this,wrap=this._qualityWrap;
  if(!wrap||this._destroyed)return;
  var choices=Array.isArray(this.opts.qualities)?this.opts.qualities:[];
  var explicit=choices.length>1;
  var levels=this._qualityLevels||[];
  var select=this._qualitySelect;
  if(!select){
    select=document.createElement('select');
    select.className='sx-quality';
    select.setAttribute('aria-label','播放清晰度');
    select.title='播放清晰度';
    wrap.insertBefore(select,wrap.firstChild);
    this._qualitySelect=select;
  }
  select.replaceChildren();
  function option(value,label){var item=document.createElement('option');item.value=value;item.textContent=label;select.appendChild(item);}
  select.disabled=!explicit&&levels.length<2;
  if(this._qualityLabel)this._qualityLabel.hidden=select.disabled;
  select.title=select.disabled?'当前片源没有提供其他清晰度':'切换播放清晰度';
  if(select.disabled){
    var only=levels[0];
    var width=only&&only.width||this.video&&this.video.videoWidth,height=only&&only.height||this.video&&this.video.videoHeight;
    var detecting=Boolean(this.hls&&!this._qualityManifestParsed);
    var label=width&&height?width+'×'+height+' · 单档':detecting?'检测清晰度…':'源站单档';
    option('single',label);
    return;
  }
  if(explicit){
    choices.forEach(function(item,index){option('source:'+index,item.label||('清晰度 '+(index+1)));});
    var current=Number(this.opts.qualityIndex);
    if(!Number.isInteger(current)||current<0||current>=choices.length){current=choices.findIndex(function(item){return item.selected;});}
    select.value='source:'+(current<0?0:current);
  }else{
    option('hls:-1','自动');
    levels.forEach(function(level,index){
      var label=level.height?level.height+'P':(level.bitrate?Math.round(level.bitrate/1000)+' kbps':'档位 '+(index+1));
      if(levels.some(function(other,i){return i!==index&&other.height===level.height;}))label+=' · '+Math.round((level.bitrate||0)/1000)+' kbps';
      option('hls:'+index,label);
    });
    select.value='hls:'+(this.hls&&this.hls.autoLevelEnabled===false?this.hls.currentLevel:-1);
  }
  select.onchange=function(){
    var parts=select.value.split(':'),index=Number(parts[1]);
    if(parts[0]==='hls'){
      if(self.hls)self.hls.currentLevel=index;
      return;
    }
    var chosen=choices[index];
    if(!chosen||index===Number(self.opts.qualityIndex))return;
    var box=self.video&&self.video.parentElement;
    if(!box)return;
    var position=Number(self.video.currentTime)||0;
    var shouldPlay=!self.video.paused;
    var next=Object.assign({},self.opts,{src:chosen.url,type:chosen.type||'mp4',duration:Number(chosen.duration)||0,start:position,autoplay:shouldPlay,muted:self.video.muted,volume:self.video.volume,playbackRate:self.video.playbackRate,qualityIndex:index});
    self.destroy();
    box.replaceChildren();
    var replacement=new SuxinPlayer(next);
    if(next.onPlayerReplace)next.onPlayerReplace(replacement);
  };
};
