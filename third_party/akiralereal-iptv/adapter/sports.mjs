// SPDX-License-Identifier: GPL-3.0-only
// The upstream migu module deliberately excludes updateData.updatePE. This
// adapter uses the same official endpoints without importing its file writers.
import { readFileSync, writeFileSync, existsSync, renameSync } from 'node:fs';
import { join } from 'node:path';
import { checkedFetch } from './network.mjs';
import { safeMessage } from './contract.mjs';

export function durationSeconds(value) {
  const text=String(value??'');
  if(/^\d+:\d{2}:\d{2}$/.test(text)) {
    const [hours,minutes,seconds]=text.split(':').map(Number);
    if(minutes<60&&seconds<60&&hours*3600+minutes*60+seconds>0)return hours*3600+minutes*60+seconds;
  }
  return null;
}
const timestamp=value=>Number.isFinite(Number(value))&&Number(value)>0?new Date(Number(value)).toISOString():null;
export function eventChannels(match,detail,rows,replay) {
  const programme=String(match.mgdbId||'');
  if(!/^\d+$/.test(programme))return [];
  const title=(match.confrontTeams?.length>=2?`${match.confrontTeams[0].name} VS ${match.confrontTeams[1].name}`:match.pkInfoTitle||match.title||detail.title||'').trim();
  const start=timestamp(detail.matchStartTime||detail.startTime||match.matchStartTime||match.startTime);
  const end=timestamp(detail.endTime||match.endTime);
  return (Array.isArray(rows)?rows:[]).filter(row=>{
    const name=String(row.name||'');
    return /^\d+$/.test(String(row.pID||''))&&name&&!/集锦|训练/.test(name)&&
      (replay?/回放|赛/.test(name):!!row.startTimeStr);
  }).map(row=>({
    deferredRef:String(row.pID),name:[match.competitionName,title,row.name].filter(Boolean).join(' '),
    providerRef:`event-${programme}-${replay?'replay':'live'}-${row.pID}`,
    logo:row.image?.highResolutionH||match.competitionLogo||'',
    group:'体育赛事',kind:replay?'event_replay':'event_live',mode:replay?'event_replay':'live',
    programme_id:programme,title,start,end,duration_seconds:replay?durationSeconds(row.duration):null,
    catchup:'none',wantsPlayback:false,
  }));
}

export function createSports({dataDir,profile,migu,sourceConcurrency,mapSettled,budget,fetchImpl=checkedFetch}) {
  const path=join(dataDir,'adapter-sports.json');
  let config=existsSync(path)?JSON.parse(readFileSync(path,'utf8')):{enabled:false,refresh_minutes:60};
  let channels=[],refreshing=null;
  const health={lastSuccessAt:null,lastAttemptAt:null,nextRetryAt:null,lastError:'',consecutiveFailures:0,warnings:[]};
  const module={id:'migu-sports',name:'咪咕体育赛事',description:'手动启用官方赛事直播及已发布的有限回放；账号和画质使用本实例咪咕模块配置。',
    category:'live',configSchema:[],capabilities:{resolve:true,epg:true,catchup:false,event_replay:true},
    resolve:migu.resolve,clearResolveCache:migu.clearResolveCache};
  function configure(input) {
    if(input.config!==undefined&&(!input.config||typeof input.config!=='object'||Array.isArray(input.config)))throw new Error('config must be an object');
    if(input.config&&Object.keys(input.config).length)throw new Error('sports uses this profile\'s migu account configuration');
    if(input.refresh_minutes!==undefined&&(!Number.isFinite(input.refresh_minutes)||input.refresh_minutes<10||input.refresh_minutes>360))throw new Error('sports refresh_minutes must be between 10 and 360');
    config={...config,...(input.enabled===undefined?{}:{enabled:input.enabled}),...(input.refresh_minutes===undefined?{}:{refresh_minutes:input.refresh_minutes})};
    writeFileSync(path+'.tmp',JSON.stringify(config,null,2)+'\n',{mode:0o600});renameSync(path+'.tmp',path);
    if(!config.enabled)channels=[];
  }
  async function fetchJson(url) {
    const task=async()=>{
      const response=await fetchImpl(url,{timeoutMs:10000});
      if(!response.ok){response.body?.destroy();throw new Error(`sports upstream HTTP ${response.status}`);}
      return response.json();
    };
    return budget?budget.run('source',task):task();
  }
  function refresh({auto=false}={}) {
    if(!config.enabled)return Promise.resolve(false);
    if(refreshing)return refreshing;
    if(auto&&((health.nextRetryAt&&Date.now()<health.nextRetryAt)||
      (health.lastSuccessAt&&Date.now()-health.lastSuccessAt<config.refresh_minutes*60000)))return Promise.resolve(false);
    refreshing=(async()=>{
      health.lastAttemptAt=Date.now();
      try {
        const list=await fetchJson('http://v0-sc.miguvideo.com/vms-match/v6/staticcache/basic/match-list/normal-match-list/0/all/default/1/miguvideo');
        if(!Array.isArray(list?.body?.days)||!list.body.matchList)throw new Error('sports match list is unavailable');
        const today=new Date(Date.now()+8*3600000).toISOString().slice(0,10).replaceAll('-','');
        const offset=list.body.days.indexOf(today);
        if(offset<0)throw new Error('sports list does not include today');
        const days=list.body.days.slice(Math.max(0,offset-1),offset+2),seen=new Set();
        const jobs=days.flatMap(day=>list.body.matchList[day]||[]).filter(row=>/^\d+$/.test(String(row.mgdbId||''))&&!seen.has(row.mgdbId)&&(seen.add(row.mgdbId),true));
        const outcomes=await mapSettled(jobs,sourceConcurrency,async match=>{
          const result=await fetchJson(`https://vms-sc.miguvideo.com/vms-match/v6/staticcache/basic/basic-data/${match.mgdbId}/miguvideo`);
          const detail=result?.body;if(!detail?.multiPlayList)throw new Error('sports detail is unavailable');
          const replay=Number(detail.endTime)>0&&Number(detail.endTime)<Date.now();
          let rows=detail.multiPlayList.liveList;
          if(replay) {
            let replayResult;
            try{replayResult=await fetchJson(`http://app-sc.miguvideo.com/vms-match/v5/staticcache/basic/all-view-list/${match.mgdbId}/2/miguvideo`);}catch{}
            rows=replayResult?.body?.replayList??detail.multiPlayList.replayList;
          }
          return eventChannels(match,detail,rows,replay);
        });
        const failures=outcomes.filter(row=>row.status==='rejected');
        if(jobs.length&&failures.length===jobs.length)throw new Error('all sports detail requests failed');
        channels=outcomes.flatMap(row=>row.status==='fulfilled'?row.value:[]);
        health.lastSuccessAt=Date.now();health.lastError='';health.consecutiveFailures=0;health.nextRetryAt=null;
        health.warnings=failures.length?[`${failures.length} sports details were unavailable this round`]:[];
        return true;
      } catch(error) {
        // Event pIDs change with a match. Never restore expired event records.
        channels=[];health.lastError=safeMessage(error);health.consecutiveFailures++;
        health.nextRetryAt=Date.now()+Math.min(60,5*2**(health.consecutiveFailures-1))*60000;
        return false;
      }
    })().finally(()=>{refreshing=null;});
    return refreshing;
  }
  return {module,configure,refresh,enabled:()=>config.enabled===true,channels:()=>config.enabled?channels:[],
    state:()=>({id:module.id,name:module.name,description:module.description,category:module.category,enabled:config.enabled,
      capabilities:module.capabilities,config_schema:[],config:{},secrets_set:{},login:false,browser_login:false,
      refresh_configurable:true,min_refresh_minutes:10,max_refresh_minutes:360,refresh_description:'手动开启，按官方昨天/今天/明天赛事列表更新',
      refresh_minutes:config.refresh_minutes,health:{...health,channelCount:channels.length}})};
}
