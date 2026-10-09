/* First usable media in the preferred tier wins; failed tiers fall back. */
(function(root){
  'use strict';
  function start(options){
    options=options||{};
    var candidates=Array.isArray(options.candidates)?options.candidates.slice():[];
    var concurrency=Math.max(1,Math.min(4,Math.floor(Number(options.concurrency)||4)));
    var candidateTimeout=Math.max(1,Math.min(20000,Number(options.candidateTimeoutMs)||12000));
    // Preserve source/episode indices. Only the probe queue is ordered, and a
    // faster unrelated provider cannot beat a usable preferred provider.
    var ordered=candidates.map(function(candidate,index){
      var priority=0;
      if(typeof options.priority==='function'){
        try{priority=Number(options.priority(candidate));}catch(_){}
      }
      if(!Number.isFinite(priority))priority=0;
      return {candidate:candidate,index:index,priority:Math.max(0,Math.floor(priority))};
    }).sort(function(a,b){return a.priority-b.priority||a.index-b.index;});
    var groups=[];
    ordered.forEach(function(item,index){
      var group=groups[groups.length-1];
      if(!group||group.priority!==item.priority){group={priority:item.priority,start:index,end:index};groups.push(group);}
      group.end=index+1;
    });
    candidates=ordered.map(function(item){return item.candidate;});
    // Every queued batch gets the same resolver + media readiness window.
    // An explicit shorter budget is useful for callers that intentionally
    // perform a quick probe; the normal path must reach later usable lines.
    var completeBudget=groups.reduce(function(total,group){return total+Math.ceil((group.end-group.start)/concurrency)*candidateTimeout;},1000);
    var requestedBudget=Number(options.timeoutMs);
    var timeout=Number.isFinite(requestedBudget)&&requestedBudget>0?Math.max(1,Math.min(completeBudget,requestedBudget)):completeBudget;
    var entries=[],cursor=0,phase=0,active=0,completed=0,settled=false,timer=null,resolveResult;
    var now=function(){return root.performance&&root.performance.now?root.performance.now():Date.now();};
    var began=now();
    var promise=new Promise(function(resolve){resolveResult=resolve;});

    function notify(){
      if(typeof options.onProgress==='function'){
        try{options.onProgress({completed:completed,total:candidates.length,active:active,started:cursor,priority:groups[phase]?groups[phase].priority:null});}catch(_){}
      }
    }
    function detach(entry){
      var result=entry.result,video=result&&result.player&&result.player.video;
      if(video&&entry.ready){
        video.removeEventListener('canplay',entry.ready);
        video.removeEventListener('error',entry.failed);
      }
      if(result&&result.player&&result.player.opts&&result.player.opts.onStreamError===entry.failed){
        result.player.opts.onStreamError=entry.originalError;
      }
    }
    function dispose(entry){
      entry.stopped=true;
      if(entry.timer!==undefined)root.clearTimeout(entry.timer);
      if(!entry.controller.signal.aborted)entry.controller.abort();
      detach(entry);
      // A resolver may finish after cancellation. Clean its eventual result too.
      if(entry.result&&!entry.cleaned){
        entry.cleaned=true;
        try{entry.result.player.destroy();}catch(_){}
        if(entry.result.element&&entry.result.element.remove)entry.result.element.remove();
      }
    }
    function finish(winner){
      if(settled)return;
      settled=true;
      if(timer!==null)root.clearTimeout(timer);
      entries.forEach(function(entry){
        if(entry===winner){root.clearTimeout(entry.timer);detach(entry);}
        else dispose(entry);
      });
      if(winner){
        winner.result.player.video.muted=winner.originalMuted;
        resolveResult({candidate:winner.candidate,player:winner.result.player,element:winner.result.element,elapsedMs:Math.max(0,now()-began)});
      }else resolveResult(null);
    }
    function rejectEntry(entry,reason){
      if(settled||entry.finished||entry.stopped)return;
      entry.finished=true;
      completed++;
      active--;
      // A per-item deadline is a local attempt result, not a health report.
      // Manual/global cancellation and losing probes remain silent.
      if((!reason||reason==='error'||reason==='timeout')&&typeof options.onCandidateFailure==='function'){
        try{options.onCandidateFailure(entry.candidate,reason==='timeout'?'timeout':'error');}catch(_){}
      }
      dispose(entry);
      notify();
      launch();
    }
    function launch(){
      if(settled)return;
      while(active===0&&phase<groups.length&&cursor>=groups[phase].end)phase++;
      var group=groups[phase];
      while(group&&active<concurrency&&cursor<group.end){
        var entry={candidate:candidates[cursor++],controller:new AbortController(),result:null,finished:false,stopped:false,cleaned:false};
        entries.push(entry);
        active++;
        prepare(entry);
      }
      notify();
      if(active===0&&cursor>=candidates.length)finish(null);
    }
    function prepare(entry){
      // A stalled initial batch must release slots for later candidates.
      entry.timer=root.setTimeout(function(){rejectEntry(entry,'timeout');},candidateTimeout);
      Promise.resolve().then(function(){
        if(settled||entry.stopped)return null;
        return options.create(entry.candidate,entry.controller.signal);
      }).then(function(result){
        if(!result){rejectEntry(entry,'cancelled');return;}
        entry.result=result;
        if(settled||entry.stopped){dispose(entry);return;}
        var player=result.player,video=player&&player.video;
        if(!video||typeof video.addEventListener!=='function'){rejectEntry(entry);return;}
        entry.originalMuted=Boolean(video.muted);
        video.muted=true;
        video.preload='auto';
        entry.originalError=player.opts&&player.opts.onStreamError;
        entry.failed=function(){rejectEntry(entry);};
        entry.ready=function(){
          if(settled||entry.finished||entry.stopped)return;
          if(video.error||player._failed){rejectEntry(entry);return;}
          if(video.readyState>=3){entry.finished=true;finish(entry);}
        };
        if(player.opts)player.opts.onStreamError=entry.failed;
        video.addEventListener('canplay',entry.ready);
        video.addEventListener('error',entry.failed);
        if(video.error||player._failed)entry.failed();
        else entry.ready();
      }).catch(function(error){rejectEntry(entry,error&&error.name==='AbortError'?'cancelled':'error');});
    }
    if(!candidates.length||typeof options.create!=='function')finish(null);
    else{
      timer=root.setTimeout(function(){finish(null);},timeout);
      launch();
    }
    return {promise:promise,cancel:function(){finish(null);}};
  }
  root.SuxinSourceSelect={start:start};
  if(typeof module!=='undefined'&&module.exports)module.exports=root.SuxinSourceSelect;
})(typeof window!=='undefined'?window:globalThis);
