// SPDX-License-Identifier: GPL-3.0-only
import test from 'node:test';
import assert from 'node:assert/strict';
import { mkdtemp, writeFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { spawn } from 'node:child_process';
import net from 'node:net';
import { once } from 'node:events';
import { authenticated, stableRef, catchupSupported, replayWindow, isPublicAddress, publicUrl, rewriteHls, safeMessage, mediaMime, mediaLeaseMs } from './contract.mjs';
import { checkedFetch, mediaHeaders } from './network.mjs';
import { createSports, eventChannels, durationSeconds } from './sports.mjs';
import { createResourceBudget, installBrowserBudget } from './resources.mjs';
import { EventEmitter } from 'node:events';

test('internal authentication rejects omitted, changed and repeated credentials',()=>{
  assert(authenticated('exact-secret','exact-secret'));
  for(const value of ['',undefined,'exact-secreu',['exact-secret']])assert(!authenticated(value,'exact-secret'));
});
test('provider identity survives refreshed signed addresses and group changes',()=>{
  assert.equal(stableRef('tv',{name:'新闻',url:'https://a.example/live?expires=1'}),stableRef('tv',{name:'新闻',url:'https://b.example/rotated?expires=2',group:'new'}));
  assert.notEqual(stableRef('tv',{name:'新闻'}),stableRef('other',{name:'新闻'}));
  assert.equal(stableRef('tv',{name:'renamed',deferredRef:'room-100'}),'room-100');
});
test('EPG does not imply catchup, and Shanghai conversion respects the supplied timezone',()=>{
  assert(!catchupSupported('yangshipin',{wantsPlayback:true}));
  assert(catchupSupported('migu',{wantsPlayback:true}));
  assert.deepEqual(replayWindow('2026-10-03T11:00:00Z','2026-10-03T12:00:00Z',true),{begin:'20261003190000',end:'20261003200000'});
  assert.throws(()=>replayWindow('20261003190000','20261003200000',false));
  assert.throws(()=>replayWindow('20260230090000','20260301090000',true));
  assert.throws(()=>replayWindow('20261003200000','20261003190000',true));
});
test('SSRF rejects private, metadata, multicast and mapped IPv6 destinations',()=>{
  for(const address of ['127.0.0.1','10.1.2.3','172.16.0.1','192.168.1.2','169.254.169.254','100.64.0.1','0.0.0.0','255.255.255.255','::1','fc00::1','fe80::1','::ffff:127.0.0.1','2001:db8::1','198.18.1.2'])assert(!isPublicAddress(address),address);
  assert(isPublicAddress('8.8.8.8'));assert(isPublicAddress('2606:4700::1111'));
  assert(isPublicAddress('198.18.1.2',true));
  for(const value of ['http://127.0.0.1/a','http://2130706433/a','http://[::1]/a','http://host.local/a','http://localhost/a','http://private/a','file:///etc/passwd','https://user:password@example.com/a','http://198.18.0.1/a'])assert.throws(()=>publicUrl(value),value);
});
test('HLS rewrites child manifests, AES keys, init segments and relative fragments',()=>{
  const targets=[];
  const output=rewriteHls('#EXTM3U\n#EXT-X-MEDIA:TYPE=AUDIO,URI="audio/live.m3u8"\n#EXT-X-KEY:METHOD=AES-128,URI="../key.bin"\n#EXT-X-MAP:URI="init.mp4"\npart1.ts\n','https://cdn.example.com/a/live.m3u8',url=>{publicUrl(url);targets.push(url);return `http://127.0.0.1:9180/internal/media/opaque${targets.length}/index.ts`;});
  assert.equal(targets.length,4);assert(targets.includes('https://cdn.example.com/key.bin'));
  assert(!output.includes('cdn.example.com'));assert.equal((output.match(/internal\/media/g)||[]).length,4);
  assert.throws(()=>rewriteHls('#EXTM3U\nhttp://127.0.0.1/admin\n','https://cdn.example.com/a',publicUrl));
});
test('public DNS is checked and pinned before media fetch',async()=>{
  let called=false;
  await assert.rejects(checkedFetch('https://cdn.example.com/a',{lookupImpl:async()=>[{address:'127.0.0.1',family:4}],fetchImpl:async()=>{called=true;}}),/private/);
  assert(!called);
  const response=await checkedFetch('https://cdn.example.com/a',{lookupImpl:async()=>[{address:'198.18.1.2',family:4}],fetchImpl:async(_url,options)=>{
    await new Promise((resolve,reject)=>options.agent.options.lookup('cdn.example.com',{},(error,address)=>error?reject(error):(assert.equal(address,'198.18.1.2'),resolve())));
    return {status:200};
  }});assert.equal(response.status,200);
});
test('redirects recheck destinations and never send internal secrets or cross-origin cookies',async()=>{
  const observed=[];
  const result=await checkedFetch('https://first.example.com/a',{headers:{Cookie:'account-cookie',Authorization:'Bearer account','X-IPTV-Secret':'internal'},lookupImpl:async()=>[{address:'8.8.8.8',family:4}],fetchImpl:async(url,options)=>{
    observed.push(options.headers);
    return observed.length===1?{status:302,headers:new Headers({location:'https://second.example.com/b'}),body:{destroy(){}}}:{status:200};
  }});
  assert.equal(result.status,200);assert.equal(observed[0].Cookie,'account-cookie');assert(!observed[0]['X-IPTV-Secret']);assert(!observed[1].Cookie);assert(!observed[1].Authorization);
  await assert.rejects(checkedFetch('https://first.example.com/a',{lookupImpl:async()=>[{address:'8.8.8.8',family:4}],fetchImpl:async()=>({status:302,headers:new Headers({location:'http://169.254.169.254/'}),body:{destroy(){}}})}));
  assert(!mediaHeaders({'X-IPTV-Secret':'hidden'},'https://example.com/a')['X-IPTV-Secret']);
});
test('errors omit upstream URLs and credential values',()=>{
  const message=safeMessage(new Error('request https://example.com/a?token=private failed cookie=secret'));
  assert(!message.includes('private'));assert(!message.includes('secret'));
});
test('proxy UA follows the upstream static/function header contract',()=>{
  assert.match(mediaHeaders({'User-Agent':'qqlive'},'https://example.com/a')['User-Agent'],/Mozilla/);
  assert.equal(mediaHeaders(()=>({'User-Agent':'official-player'}),'https://example.com/a')['User-Agent'],'official-player');
  assert.equal(mediaHeaders(undefined,'https://example.com/a',undefined,'flv')['User-Agent'],'node');
  assert.equal(mediaHeaders(undefined,'https://example.com/a',undefined,'flv')['Accept-Encoding'],'identity');
});
test('FLV transport preserves a continuous native-identity source beyond the header deadline',async t=>{
  const http=await import('node:http'),{default:nodeFetch}=await import('node-fetch');
  const server=http.createServer((req,res)=>{
    res.writeHead(200,{'Content-Type':'video/x-flv'});res.write('FLV');
    if(req.headers['user-agent']!=='node'){res.end('short browser burst');return;}
    let count=0;
    const timer=setInterval(()=>{res.write('continuous');if(++count===4){clearInterval(timer);res.end();}},60);
    res.once('close',()=>clearInterval(timer));
  });
  server.listen(0,'127.0.0.1');await once(server,'listening');t.after(()=>new Promise(resolve=>server.close(resolve)));
  const fixture=`http://127.0.0.1:${server.address().port}/live.flv`;
  // Only this test injection maps the validated public request to its owned
  // loopback fixture. Production keeps its DNS-pinned checked agent.
  const response=await checkedFetch('http://cdn.example.com/live.flv',{mediaType:'flv',timeoutMs:100,
    lookupImpl:async()=>[{address:'8.8.8.8',family:4}],fetchImpl:(_url,options)=>nodeFetch(fixture,{...options,agent:undefined})});
  const text=await response.text();assert.equal(text,'FLV'+'continuous'.repeat(4));
});
test('sports replay has an independent stable identity and actual finite duration',()=>{
  const match={mgdbId:'1200001',title:'决赛',competitionName:'联赛',startTime:1790910000000,endTime:1790917200000};
  const [row]=eventChannels(match,match,[{name:'全场回放',pID:'968001',duration:'01:30:12'},{name:'比赛集锦',pID:'968002',duration:'00:03:00'}],true);
  assert.equal(row.providerRef,'event-1200001-replay-968001');assert.equal(row.kind,'event_replay');assert.equal(row.duration_seconds,5412);
  assert.equal(row.catchup,'none');assert.equal(row.start,'2026-10-02T03:00:00.000Z');
  assert.equal(durationSeconds('0'),null);assert.equal(durationSeconds('01:99:20'),null);
});
test('immutable long replay fragments remain leased throughout finite playback',()=>{
  assert.equal(mediaLeaseMs({kind:'event_replay',duration_seconds:15838}),(15838+1200)*1000);
  assert.equal(mediaLeaseMs({},replayWindow('20261003080000','20261003120000',true)),(4*3600+1200)*1000);
  assert.equal(mediaLeaseMs({kind:'event_live'}),1200000);
  assert.equal(mediaLeaseMs({kind:'event_replay',duration_seconds:1000000}),(24*3600+1200)*1000);
});
test('manual sports refresh never restores stale event pIDs after an upstream failure',async t=>{
  const dataDir=await mkdtemp(join(tmpdir(),'xiaoqi-iptv-sports-'));t.after(()=>rm(dataDir,{recursive:true,force:true}));
  const today=new Date(Date.now()+8*3600000).toISOString().slice(0,10).replaceAll('-','');
  let failed=false;
  const sports=createSports({dataDir,profile:'public',migu:{resolve(){},clearResolveCache(){}},sourceConcurrency:4,
    mapSettled:async(items,_limit,task)=>Promise.allSettled(items.map(task)),fetchImpl:async url=>{
      if(failed)throw new Error('network unavailable');
      const body=url.includes('match-list')?{days:[today],matchList:{[today]:[{mgdbId:'123',title:'决赛',startTime:Date.now()-7200000,endTime:Date.now()-3600000}]}}:
        url.includes('basic-data')?{startTime:Date.now()-7200000,endTime:Date.now()-3600000,multiPlayList:{replayList:[]}}:
          {replayList:[{name:'决赛全场回放',pID:'987',duration:'01:20:00'}]};
      return {ok:true,json:async()=>({body})};
    }});
  assert(!sports.enabled());sports.configure({enabled:true});assert(await sports.refresh());assert.equal(sports.channels().length,1);
  failed=true;assert.equal(await sports.refresh(),false);assert.equal(sports.channels().length,0);assert.equal(sports.state().health.consecutiveFailures,1);
});
test('real media bytes determine MPEG-TS, fMP4 and FLV response types',()=>{
  assert.equal(mediaMime(Buffer.from('47400010','hex')),'video/mp2t');
  assert.equal(mediaMime(Buffer.from('000000186674797069736f6d','hex')),'video/mp4');
  assert.equal(mediaMime(Buffer.from('000000206d6f6f66','hex')),'video/mp4');
  assert.equal(mediaMime(Buffer.from('464c560105','hex')),'video/x-flv');
  assert.equal(mediaMime(Buffer.from('fff15080','hex')),'audio/aac');
});
test('shared public/member budgets observe browser peak 2 and source peak 4',{timeout:10000},async t=>{
  const directory=await mkdtemp(join(tmpdir(),'xiaoqi-iptv-budget-'));
  const publicBudget=createResourceBudget(directory),memberBudget=createResourceBudget(directory);
  t.after(()=>{publicBudget.close();memberBudget.close();return rm(directory,{recursive:true,force:true});});
  let active=0,peak=0;
  await Promise.all(Array.from({length:12},(_,index)=>(index%2?publicBudget:memberBudget).run('source',async()=>{
    active++;peak=Math.max(peak,active);await new Promise(resolve=>setTimeout(resolve,25));active--;
  })));
  assert.equal(peak,4);active=0;peak=0;
  await installBrowserBudget(publicBudget,2);
  const {launchBrowser,closeBrowser,getBrowserPool}=await import('../utils/browserLauncher.js');
  class MockBrowser extends EventEmitter {connected=true;process(){return null;}async close(){this.connected=false;active--;this.emit('disconnected');}}
  await Promise.all(Array.from({length:8},()=> (async()=>{
    const browser=await launchBrowser({waitMs:2000,launchImpl:async()=>{active++;peak=Math.max(peak,active);return new MockBrowser();}});
    await new Promise(resolve=>setTimeout(resolve,15));await closeBrowser(browser);
  })()));
  assert.equal(peak,2);assert.equal(getBrowserPool().size,0);
  const {VipMseBridge}=await import('../extractors/yangshipin/vip-bridge.js');
  const bridge=new VipMseBridge({},{maxActiveChannels:3});assert.equal(bridge.maxActiveChannels,2);clearInterval(bridge.cleanupTimer);
});

async function freePort(){const server=net.createServer();server.listen(0,'127.0.0.1');await once(server,'listening');const port=server.address().port;await new Promise(resolve=>server.close(resolve));return port;}
test('HTTP contract isolates public/member, redacts credentials, and revokes disabled media',{timeout:30000},async t=>{
  const dataDir=await mkdtemp(join(tmpdir(),'xiaoqi-iptv-test-'));
  const secret='test-internal-secret-32-bytes-at-least';
  const port=await freePort();
  await writeFile(join(dataDir,'extractors.json'),JSON.stringify({enabled:true,masterSwitchRetired:true,modules:{heilongjiang:{enabled:true,config:{}},yangshipin:{enabled:true,config:{}}}}));
  await writeFile(join(dataDir,'system-config.json'),JSON.stringify({enableMigu:false}));
  await writeFile(join(dataDir,'extractor-cache.json'),JSON.stringify({modules:{heilongjiang:{groups:[{name:'测试',dataList:[{name:'测试台',url:'https://cdn.example.com/live.m3u8?expires=1'}]}]},yangshipin:{groups:[{name:'央视频',dataList:[{name:'会员',deferredRef:'ysp-vip-cctvfyjc',catchup:'none'}]}]}}}));
  const child=spawn(process.execPath,[new URL('./server.mjs',import.meta.url).pathname.replace(/^\/([A-Za-z]:)/,'$1')],{env:{...process.env,IPTV_PROFILE:'public',IPTV_PORT:String(port),IPTV_DATA_DIR:dataDir,IPTV_RESOURCE_DIR:join(dataDir,'budget'),IPTV_INTERNAL_SECRET:secret,IPTV_AUTO_REFRESH:'false'},stdio:['ignore','pipe','pipe']});
  let errors='';child.stderr.on('data',value=>{errors+=value;});
  t.after(async()=>{child.kill();await new Promise(resolve=>{if(child.exitCode!==null)return resolve();child.once('exit',resolve);setTimeout(()=>{child.kill('SIGKILL');resolve();},10000).unref();});await rm(dataDir,{recursive:true,force:true});});
  const base=`http://127.0.0.1:${port}`;
  for(let i=0;i<100;i++){try{await fetch(base+'/internal/health');break;}catch{await new Promise(resolve=>setTimeout(resolve,50));}}
  assert.equal(child.exitCode,null,errors);
  const request=(path,method='GET',value)=>fetch(base+path,{method,headers:{'X-IPTV-Secret':secret,'Content-Type':'application/json'},body:value?JSON.stringify(value):undefined});
  assert.equal((await fetch(base+'/internal/health')).status,401);
  assert.equal((await request('/admin')).status,404);
  const health=await (await request('/internal/health')).json();assert.equal(health.profile,'public');assert.equal(health.commit,'7b1e2d5fdc6dbe35de63f099be6264a201c6a975');
  const catalog=await(await request('/internal/catalog')).json();assert.equal(catalog.channels.length,1);assert.equal(catalog.channels[0].catchup,false);
  const modules=await(await request('/internal/modules')).json();assert(modules.modules.some(row=>row.id==='bilibili-live'&&row.config_schema.length));
  assert(modules.modules.some(row=>row.id==='migu-sports'&&!row.enabled&&row.capabilities.event_replay));
  assert.equal((await request('/internal/modules/login','POST',{id:'yangshipin',action:'browserStatus'})).status,403);
  assert.equal((await request('/internal/modules/config','POST',{id:'migu',config:{token:'account-secret'}})).status,403);
  assert.equal((await request('/internal/sync','POST',{modules:['unregistered']})).status,400);
  const channel=catalog.channels[0];
  const resolved=await(await request('/internal/resolve','POST',{provider_key:channel.provider_key,provider_ref:channel.provider_ref})).json();assert(resolved.ok);assert.equal(new URL(resolved.url).origin,base);assert.equal(resolved.format,'hls');assert(Number.isFinite(Date.parse(resolved.expires_at)));
  assert.equal((await request('/internal/resolve','POST',{provider_key:channel.provider_key,provider_ref:channel.provider_ref,start:'20261003190000',end:'20261003200000'})).status,400);
  const configResult=await(await request('/internal/modules/config','POST',{id:'migu',enabled:false,refresh_minutes:75})).json();assert(configResult.ok,configResult.message);assert.equal(configResult.module.refresh_minutes,75);
  assert.equal((await request('/internal/modules/config','POST',{id:'heilongjiang',enabled:false})).status,200);
  assert.equal((await fetch(resolved.url,{headers:{'X-IPTV-Secret':secret}})).status,502);
});

test('adapter exits and cleans its runtime lock after its supervising parent disappears',{timeout:25000},async t=>{
  const dataDir=await mkdtemp(join(tmpdir(),'xiaoqi-iptv-parent-'));
  const parent=spawn(process.execPath,['-e','setInterval(()=>{},1000)'],{stdio:'ignore'});
  const port=await freePort();
  const child=spawn(process.execPath,[new URL('./server.mjs',import.meta.url).pathname.replace(/^\/([A-Za-z]:)/,'$1')],{
    env:{...process.env,IPTV_PROFILE:'member',IPTV_PORT:String(port),IPTV_DATA_DIR:dataDir,IPTV_RESOURCE_DIR:join(dataDir,'budget'),IPTV_PARENT_PID:String(parent.pid),IPTV_INTERNAL_SECRET:'parent-test-internal-secret-32bytes',IPTV_AUTO_REFRESH:'false'},stdio:'ignore'});
  t.after(async()=>{parent.kill();child.kill();await rm(dataDir,{recursive:true,force:true});});
  for(let i=0;i<100;i++){try{await fetch(`http://127.0.0.1:${port}/internal/health`);break;}catch{await new Promise(resolve=>setTimeout(resolve,50));}}
  const parentExited=once(parent,'exit');parent.kill();await parentExited;
  const [code]=await once(child,'exit');assert.equal(code,0);
  const {access}=await import('node:fs/promises');await assert.rejects(access(join(dataDir,'adapter.pid')));
});
