// SPDX-License-Identifier: GPL-3.0-only
import { mkdirSync, readFileSync, writeFileSync, existsSync, renameSync, readdirSync, unlinkSync } from 'node:fs';
import { resolve, join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';
import { MANUAL_MODULES, stableRef, accountRequired, catchupSupported, safeMessage } from './contract.mjs';
import { createSports } from './sports.mjs';
import { createResourceBudget, installBrowserBudget } from './resources.mjs';

export const UPSTREAM_ROOT = fileURLToPath(new URL('../', import.meta.url));
const readJson = (path, fallback) => existsSync(path) ? JSON.parse(readFileSync(path,'utf8')) : fallback;
const writeJson = (path, value) => { writeFileSync(path+'.tmp', JSON.stringify(value,null,2)+'\n', {mode:0o600}); renameSync(path+'.tmp',path); };

export async function initializeUpstream({profile,dataDir,browserConcurrency=2,sourceConcurrency=4}) {
  mkdirSync(dataDir,{recursive:true,mode:0o700});
  const marker = join(dataDir,'adapter-profile.json');
  const previous = readJson(marker,null);
  if (previous && previous.profile !== profile) throw new Error('public and member instances require different data directories');
  writeJson(marker,{profile});
  const lockPath = join(dataDir,'adapter.pid');
  const previousPid = Number(existsSync(lockPath) ? readFileSync(lockPath,'utf8') : 0);
  if (previousPid && previousPid !== process.pid) {
    try { process.kill(previousPid,0); throw new Error('runtime data directory already belongs to a running process'); }
    catch (error) { if (error.code !== 'ESRCH') throw error; }
  }
  writeFileSync(lockPath,String(process.pid),{mode:0o600});
  process.env.mdataDir = resolve(dataDir);
  process.env.mbrowserConcurrency = String(browserConcurrency);
  process.env.mdebug = '';
  // Never inherit another profile's platform login from the host environment.
  for (const key of Object.keys(process.env)) if (/^m/i.test(key) && /token|cookie|sessdata|userid|pass$/i.test(key)) delete process.env[key];
  const moduleIds = readdirSync(join(UPSTREAM_ROOT,'extractors'),{withFileTypes:true}).filter(entry=>entry.isDirectory()).map(entry=>entry.name);
  const configPath = join(dataDir,'extractors.json');
  if (!existsSync(configPath)) writeJson(configPath,{enabled:true,masterSwitchRetired:true,modules:Object.fromEntries(moduleIds.map(id=>[id,{enabled:profile==='public'&&!MANUAL_MODULES.has(id),config:{}}]))});
  const systemPath = join(dataDir,'system-config.json');
  if (!existsSync(systemPath)) writeJson(systemPath,{enableMigu:profile==='public',enableBuiltInSources:false,enableBuiltInSubscriptions:false,debug:false});
  process.env.menableMigu = profile==='public'?'true':'false';
  const budget=createResourceBudget(resolve(process.env.IPTV_RESOURCE_DIR||join(dirname(dataDir),'resource-leases')));
  await installBrowserBudget(budget,browserConcurrency);
  const registry = await import('../extractors/registry.js');
  const {getExtractorManager} = await import('../utils/extractorManager.js');
  const {epgChannelId} = await import('../utils/epgAggregator.js');
  const {channelXml,providerProgrammes,mapSettled,escapeXml} = await import('../utils/epgXmltv.js');
  const {updatePlaybackData} = await import('../utils/playback.js');
  const {proxyAwareFetch} = await import('../utils/systemProxy.js');
  const manager = getExtractorManager();
  manager.load();
  const sports=createSports({dataDir,profile,migu:registry.getModule('migu'),sourceConcurrency,mapSettled,budget});
  if (profile==='public') for (const module of registry.listModules()) {
    const config = manager.effectiveConfig(module);
    if ((module.configSchema||[]).some(field=>field.secret&&config[field.key])) throw new Error('public profile must not contain platform credentials');
  }
  let catalog = [], channelIndex = new Map(), updatedAt = null, refreshing = null;
  let epgRefreshing = null, epgUpdatedAt = null, epgFailures = 0;
  const epgPath = join(dataDir,'adapter-epg.xml');
  if (!existsSync(epgPath)) writeFileSync(epgPath,'<?xml version="1.0" encoding="UTF-8"?><tv generator-info-name="Xiaoqi IPTV adapter"></tv>');

  function rebuildCatalog() {
    const index = new Map(), result = [];
    const bySource = new Map(registry.listModules().map(module=>[module.sourceId||registry.sourceIdOf(module.id),module]));
    for (const group of manager.getValidChannels()) for (const channel of group.dataList||[]) {
      const module = bySource.get(channel.sourceId);
      if (!module || (profile==='public'&&accountRequired(module.id,channel))) continue;
      const providerRef = stableRef(module.id,channel), key = `${module.id}\0${providerRef}`;
      if (index.has(key)) continue;
      const epgId = epgChannelId(channel.name);
      const format = channel.streamType || module.streamType || 'hls';
      const item = {provider_key:module.id,provider_ref:providerRef,name:channel.name,group:group.name,
        logo:channel.logo||channel.pics?.highResolutionH||channel.pics?.highResolutionV||'',
        tvg_id:epgId,epg_id:epgId,format:format==='flv'?'flv':'hls',
        catchup:catchupSupported(module.id,channel),account_required:profile==='member'||accountRequired(module.id,channel)};
      index.set(key,{module,channel,item}); result.push(item);
    }
    for(const channel of sports.channels()) {
      const ref=channel.providerRef,epgId=`migu-event-${channel.programme_id}`;
      const item={provider_key:'migu-sports',provider_ref:ref,name:channel.name,group:channel.group,logo:channel.logo,
        tvg_id:epgId,epg_id:epgId,format:'hls',catchup:false,account_required:profile==='member',
        kind:channel.kind,mode:channel.mode,programme_id:channel.programme_id,title:channel.title,
        start:channel.start,end:channel.end,duration_seconds:channel.duration_seconds};
      index.set(`migu-sports\0${ref}`,{module:sports.module,channel,item});result.push(item);
    }
    channelIndex=index; catalog=result;
    return result;
  }
  rebuildCatalog();

  async function refreshEpg({force=false}={}) {
    if (epgRefreshing) return epgRefreshing;
    if(!force&&epgUpdatedAt&&Date.now()-Date.parse(epgUpdatedAt)<3600000)return;
    epgRefreshing=(async()=>{
      const seen=new Set(), fragments=[];
      const jobs=[...channelIndex.values()].filter(({item})=>{if(seen.has(item.epg_id))return false;seen.add(item.epg_id);return true;});
      const outcomes=await mapSettled(jobs,sourceConcurrency,async record=>budget.run('source',async()=>{
        const {module,channel,item}=record;
        if(module.id==='migu-sports') {
          const start=Date.parse(item.start),stop=Date.parse(item.end);
          return Number.isFinite(start)&&Number.isFinite(stop)&&stop>start?channelXml(item.epg_id,[{title:item.title,start,stop}]):'';
        }
        if(module.id==='migu'&&channel.wantsPlayback) {
          const temporary=join(dataDir,`epg-${stableRef(module.id,channel)}.tmp`);
          writeFileSync(temporary,'');
          try {
            await updatePlaybackData(channel,temporary,10000);
            return readFileSync(temporary,'utf8').replaceAll(`id="${escapeXml(channel.name)}"`,`id="${escapeXml(item.epg_id)}"`).replaceAll(`channel="${escapeXml(channel.name)}"`,`channel="${escapeXml(item.epg_id)}"`);
          } finally { try {unlinkSync(temporary);} catch {} }
        }
        if(!module.epg) return '';
        const row=module.epg.channels().find(row=>channel.deferredRef!=null?String(row.ref)===String(channel.deferredRef):row.name===channel.name);
        if(!row) return '';
        const programmes=await providerProgrammes(module.epg,row.key,{fetchImpl:proxyAwareFetch,timeoutMs:10000});
        return programmes.length?channelXml(item.epg_id,programmes):'';
      }));
      epgFailures=outcomes.filter(row=>row.status==='rejected').length;
      for(const row of outcomes) if(row.status==='fulfilled'&&row.value) fragments.push(row.value);
      // Retain previously useful data during a completely failed upstream round.
      if(fragments.length||!catalog.length) {
        writeFileSync(epgPath+'.tmp',`<?xml version="1.0" encoding="UTF-8"?>\n<tv generator-info-name="Xiaoqi IPTV adapter">\n${fragments.join('')}</tv>\n`);
        renameSync(epgPath+'.tmp',epgPath); epgUpdatedAt=new Date().toISOString();
      }
    })().finally(()=>{epgRefreshing=null;});
    return epgRefreshing;
  }

  function refresh(ids,{auto=false}={}) {
    if(refreshing) return refreshing;
    const selected=registry.listModules().filter(module=>manager.isModuleEnabled(module)&&(!ids||ids.includes(module.id)));
    refreshing=(async()=>{
      let updated=false;
      // Calling onlyId under the adapter's pool gives the required source limit;
      // the upstream manager still owns persistence, per-module serialization and failure caching.
      await mapSettled(selected,sourceConcurrency,async module=>{
        if(auto) {
          const state=manager.getState().modules.find(row=>row.id===module.id), health=state.health;
          if(health.nextRetryAt&&Date.now()<health.nextRetryAt)return;
          if(health.lastSuccessAt&&Date.now()-health.lastSuccessAt<state.refreshMinutes*60000)return;
        }
        const result=await budget.run('source',()=>manager.updateAll({onlyId:module.id}));updated=updated||result.updated;rebuildCatalog();
      });
      if(!ids||ids.includes('migu-sports')){updated=await sports.refresh({auto})||updated;rebuildCatalog();}
      if(updated||!updatedAt)updatedAt=new Date().toISOString();
      void refreshEpg({force:!auto}).catch(error=>console.error('EPG refresh:',safeMessage(error)));
    })().finally(()=>{refreshing=null;});
    return refreshing;
  }

  function lookup(providerKey,providerRef) {
    if(typeof providerKey!=='string'||typeof providerRef!=='string')throw new Error('provider_key and provider_ref are required');
    const record=channelIndex.get(`${providerKey}\0${providerRef}`);
    if(!record||!(record.module.id==='migu-sports'?sports.enabled():manager.isModuleEnabled(record.module)))throw new Error('channel is not enabled in this profile');
    return record;
  }

  return {registry,manager,sports,profile,dataDir,lookup,rebuildCatalog,refresh,refreshEpg,
    getModule:id=>id==='migu-sports'?sports.module:registry.getModule(id),
    effectiveConfig:module=>manager.effectiveConfig(module.id==='migu-sports'?registry.getModule('migu'):module),
    catalog:()=>catalog,epg:()=>readFileSync(epgPath),
    status:()=>({ready:catalog.length>0,refreshing:!!refreshing,updated_at:updatedAt,channel_count:catalog.length,
      epg_refreshing:!!epgRefreshing,epg_updated_at:epgUpdatedAt,epg_failures:epgFailures,shared_resource_budget:true}),
    async close(){await registry.shutdownModules();budget.close();try{unlinkSync(lockPath);}catch{}},
  };
}
