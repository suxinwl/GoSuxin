// SPDX-License-Identifier: GPL-3.0-only
import http from 'node:http';
import { createHmac } from 'node:crypto';
import { resolve } from 'node:path';
import { pipeline } from 'node:stream/promises';
import { Transform } from 'node:stream';
import { authenticated, VERSION, COMMIT, publicUrl, rewriteHls, replayWindow, safeMessage, mediaMime, mediaLeaseMs } from './contract.mjs';
import { checkedFetch } from './network.mjs';
import { initializeUpstream } from './upstream.mjs';

const secret = process.env.IPTV_INTERNAL_SECRET || '';
if (Buffer.byteLength(secret)<32) throw new Error('IPTV_INTERNAL_SECRET must contain at least 32 bytes');
const profile = process.env.IPTV_PROFILE === 'member' ? 'member' : process.env.IPTV_PROFILE === 'public' || !process.env.IPTV_PROFILE ? 'public' : '';
if (!profile) throw new Error('IPTV_PROFILE must be public or member');
const port = Number(process.env.IPTV_PORT || (profile==='public'?9180:9181));
if(!Number.isInteger(port)||port<1||port>65535)throw new Error('IPTV_PORT is invalid');
const browserConcurrency = Math.max(1,Math.min(2,Math.floor(Number(process.env.IPTV_BROWSER_CONCURRENCY)||2)));
const sourceConcurrency = Math.max(1,Math.min(4,Math.floor(Number(process.env.IPTV_SOURCE_CONCURRENCY)||4)));
const dataDir = resolve(process.env.IPTV_DATA_DIR || `data/iptv/${profile}`);
const upstream = await initializeUpstream({profile,dataDir,browserConcurrency,sourceConcurrency});
const origin = `http://127.0.0.1:${port}`;
const localPrefix = `${origin}/internal/upstream`;
const leases = new Map();
const resolveRefreshes = new Map();
const moduleEpochs = new Map();
const LEASE_MS = 20*60000;

function json(res,status,value) {
  const body=Buffer.from(JSON.stringify(value));
  res.writeHead(status,{'Content-Type':'application/json; charset=utf-8','Content-Length':body.length,'Cache-Control':'no-store'});res.end(body);
}
function fail(res,status,error,code='service_error') {
  if(res.headersSent){res.destroy();return;}
  json(res,status,{ok:false,error:code,message:safeMessage(error)});
}
async function body(req) {
  const chunks=[];let size=0;
  for await(const chunk of req){size+=chunk.length;if(size>128*1024)throw new Error('request body too large');chunks.push(chunk);}
  const value=JSON.parse(Buffer.concat(chunks).toString('utf8')||'{}');
  if(!value||typeof value!=='object'||Array.isArray(value))throw new Error('JSON object required');
  return value;
}
function lease(entry,extension='m3u8') {
  // HMAC identities remain opaque even when the source has predictable URL paths.
  const identity=JSON.stringify([entry.kind,entry.ownerKey,moduleEpochs.get(entry.ownerKey)||0,entry.ownerRef,entry.url||entry.path||'',entry.start||'',entry.end||'',entry.format||'']);
  const token=createHmac('sha256',secret).update(identity).digest('hex').slice(0,32);
  if(leases.size>50000){for(const [key,row] of leases)if(row.expires<Date.now())leases.delete(key);if(leases.size>50000)throw new Error('media lease capacity reached');}
  leases.set(token,{...entry,expires:Date.now()+(entry.ttlMs||LEASE_MS)});
  return `${origin}/internal/media/${token}/index.${extension}`;
}
function findLease(token) {
  const entry=leases.get(token);
  if(!entry||entry.expires<Date.now()){leases.delete(token);throw new Error('media lease expired; resolve the channel again');}
  upstream.lookup(entry.ownerKey,entry.ownerRef); // disabling a provider revokes every media lease immediately
  entry.expires=Date.now()+(entry.ttlMs||LEASE_MS);return entry;
}
function revokeModuleMedia(id) {
  const ids=new Set(id==='migu'?['migu','migu-sports']:[id]);
  for(const owner of ids)moduleEpochs.set(owner,(moduleEpochs.get(owner)||0)+1);
  for(const [token,row] of leases)if(ids.has(row.ownerKey))leases.delete(token);
  for(const key of resolveRefreshes.keys())if(ids.has(key.split('\0')[0]))resolveRefreshes.delete(key);
  upstream.getModule(id)?.clearResolveCache?.();
}
function registerMedia(raw,context,hint) {
  const url=new URL(raw);
  if(url.origin===origin&&url.pathname.startsWith('/internal/upstream/')) {
    const path=url.pathname.slice('/internal/upstream'.length);
    const module=upstream.registry.localRequestHandlerFor(path);
    if(!module||module.id!==context.ownerKey)throw new Error('invalid module media path');
    if(profile==='public'&&/^\/ysp-vip\//.test(path))throw new Error('member media cannot enter the public profile');
    return lease({kind:'local',path,ttlMs:context.ttlMs,ownerKey:context.ownerKey,ownerRef:context.ownerRef},/\.m3u8$/i.test(path)?'m3u8':/\.ts$/i.test(path)?'ts':'mp4');
  }
  publicUrl(raw);
  let target=raw;
  if(typeof context.urlTransform==='function')target=context.urlTransform(raw);
  publicUrl(target);
  const extension=hint==='key'?'key':/\.m3u8(?:\?|$)/i.test(target)?'m3u8':/\.m4s(?:\?|$)/i.test(target)?'m4s':/\.mp4(?:\?|$)/i.test(target)?'mp4':/\.key(?:\?|$)/i.test(target)?'key':'ts';
  return lease({kind:'target',url:target,headers:context.headers,transform:context.transform,
    urlTransform:context.urlTransform,ttlMs:context.ttlMs,mediaKind:hint,ownerKey:context.ownerKey,ownerRef:context.ownerRef},extension);
}
function headersFromOpts(channel) {
  const result={};
  for(const raw of channel.opts||[]){const match=/^(http-referrer|http-user-agent)=(.*)$/.exec(raw);if(match)result[match[1]==='http-referrer'?'Referer':'User-Agent']=match[2];}
  return result;
}
async function resolveChannel(record,input={}) {
  const {module,channel,item}=record;
  if(input.refresh===true&&typeof module.clearResolveCache==='function') {
    const refreshKey=`${module.id}\0${item.provider_ref}`,last=resolveRefreshes.get(refreshKey)||0;
    if(Date.now()-last>=10000){module.clearResolveCache();resolveRefreshes.set(refreshKey,Date.now());}
  }
  if(/^ysp-vip-/.test(item.provider_ref)) {
    const status=await module.browserLoginFlow.status();
    if(!status.authenticated)throw new Error('央视频会员频道需要在账号实例导入本人登录态');
    return {localPath:`/${item.provider_ref}`,streamType:'hls'};
  }
  const config={...upstream.effectiveConfig(module)};
  if(input.format&&module.configSchema?.some(field=>field.key==='format'))config.format=input.format;
  const resolved=channel.deferredRef!=null?await module.resolve(String(channel.deferredRef),{
    config,account:{userId:config.userId||'',token:config.token||''},selfBase:localPrefix,
  }):{url:channel.url||'',upstreamHeaders:headersFromOpts(channel)};
  if(!resolved?.url)throw new Error(resolved?.desc||'upstream returned no playable URL');
  publicUrl(resolved.url);
  const replay=replayWindow(input.start,input.end,item.catchup);
  if(replay)resolved.url+=`${resolved.url.includes('?')?'&':'?'}playbackbegin=${replay.begin}&playbackend=${replay.end}`;
  return {...resolved,streamType:resolved.streamType||module.streamType||'hls'};
}
async function sendLocal(entry,req,res) {
  const module=upstream.registry.localRequestHandlerFor(entry.path);
  if(!module||module.id!==entry.ownerKey)throw new Error('unknown local media');
  const result=await module.handleLocalRequest({path:entry.path,method:req.method,headers:req.headers,accessPrefix:localPrefix});
  let output=result.body||'';
  if(/mpegurl/i.test(result.contentType||result.headers?.['Content-Type']||''))output=rewriteHls(String(output),localPrefix+entry.path,(raw,hint)=>registerMedia(raw,entry,hint));
  const headers={...(result.headers||{}),'Content-Type':result.contentType||result.headers?.['Content-Type']||'application/octet-stream','Cache-Control':'no-store'};
  delete headers['Content-Length'];
  res.writeHead(result.status||result.statusCode||200,headers);res.end(req.method==='HEAD'?'':output);
}
async function sendTarget(entry,req,res,{manifestText,manifestUrl,format}={}) {
  const context={ownerKey:entry.ownerKey,ownerRef:entry.ownerRef,ttlMs:entry.ttlMs,headers:entry.headers,transform:entry.transform,urlTransform:entry.urlTransform};
  const manifest=/\.m3u8(?:\?|$)/i.test(entry.url)||format==='hls'||manifestText;
  if(manifestText) {
    const rewritten=rewriteHls(manifestText,manifestUrl||entry.url,(raw,hint)=>registerMedia(raw,context,hint));
    const result=Buffer.from(rewritten);
    res.writeHead(200,{'Content-Type':'application/vnd.apple.mpegurl','Cache-Control':'no-cache, no-store','Content-Length':result.length});res.end(req.method==='HEAD'?'':result);return;
  }
  const response=await checkedFetch(entry.url,{headers:entry.headers,range:req.headers.range,method:manifest?'GET':req.method,mediaType:format,timeoutMs:20000});
  res.once('close',()=>response.body?.destroy());
  if(!response.ok){response.body?.destroy();throw new Error(`media upstream HTTP ${response.status}`);}
  const type=response.headers.get('content-type')||'';
  if(manifest||/mpegurl/i.test(type)) {
    const text=await response.text();
    const result=Buffer.from(rewriteHls(text,response.url||entry.url,(raw,hint)=>registerMedia(raw,context,hint)));
    res.writeHead(200,{'Content-Type':'application/vnd.apple.mpegurl','Cache-Control':'no-cache, no-store','Content-Length':result.length});res.end(req.method==='HEAD'?'':result);return;
  }
  const headers={'Content-Type':entry.mediaKind==='key'?'application/octet-stream':format==='flv'?'video/x-flv':/\.m4s|\.mp4/i.test(entry.url)?'video/mp4':/\.key/i.test(entry.url)?'application/octet-stream':'video/mp2t','Cache-Control':'no-store','X-Accel-Buffering':'no'};
  for(const key of ['content-length','content-range','accept-ranges']){const value=response.headers.get(key);if(value)headers[key]=value;}
  if(req.method==='HEAD'){res.writeHead(response.status,headers);response.body?.destroy();res.end();return;}
  if(entry.transform) {
    const chunks=[];let size=0;
    for await(const chunk of response.body){size+=chunk.length;if(size>32*1024*1024){response.body.destroy();throw new Error('media segment exceeds size limit');}chunks.push(chunk);}
    const bytes=Buffer.concat(chunks);
    const output=await entry.transform(bytes)||bytes;if(entry.mediaKind!=='key')headers['Content-Type']=mediaMime(output,headers['Content-Type']);headers['content-length']=String(output.length);res.writeHead(response.status,headers);res.end(output);return;
  }
  let first=true;
  const sniff=new Transform({transform(chunk,_encoding,callback){
    if(first){first=false;if(format==='flv'&&chunk.subarray(0,3).toString()!=='FLV'){callback(new Error('upstream did not return FLV'));return;}if(entry.mediaKind!=='key'&&!req.headers.range)headers['Content-Type']=mediaMime(chunk,headers['Content-Type']);res.writeHead(response.status,headers);}
    callback(null,chunk);
  }});
  await pipeline(response.body,sniff,res);
}
async function media(entry,req,res) {
  if(entry.kind==='local')return sendLocal(entry,req,res);
  if(entry.kind==='target')return sendTarget(entry,req,res);
  const record=upstream.lookup(entry.ownerKey,entry.ownerRef);
  const result=await resolveChannel(record,entry);
  if(result.localPath)return sendLocal({...entry,path:result.localPath},req,res);
  return sendTarget({...entry,url:result.url,headers:result.upstreamHeaders,transform:result.segmentTransform,urlTransform:result.upstreamUrlTransform},req,res,
    {manifestText:result.manifestText,manifestUrl:result.manifestUrl,format:result.streamType});
}
function modules() {
  return upstream.manager.getState().modules.map(row=>{
    const module=upstream.registry.getModule(row.id);
    return {id:row.id,name:row.name,description:row.description,category:row.category,enabled:row.enabled,
      capabilities:{...module.capabilities,catchup:module.id==='migu'},
      config_schema:row.configSchema,config:row.config,secrets_set:row.secretsSet,
      login:!!module.loginFlow,browser_login:!!module.browserLoginFlow,
      refresh_configurable:row.refreshConfigurable,min_refresh_minutes:row.minRefreshMinutes,max_refresh_minutes:row.maxRefreshMinutes,refresh_description:row.refreshDescription,
      refresh_minutes:row.refreshMinutes,health:{...row.health,lastError:safeMessage(row.health.lastError||''),warnings:(row.health.warnings||[]).map(safeMessage)}};
  }).concat(upstream.sports.state());
}
async function handle(req,res) {
  if(!authenticated(req.headers['x-iptv-secret'],secret)){fail(res,401,'invalid internal credential','unauthorized');return;}
  const path=new URL(req.url,origin).pathname;
  if(path==='/internal/health'&&req.method==='GET')return json(res,200,{ok:true,version:VERSION,commit:COMMIT,profile,...upstream.status(),browser_concurrency:browserConcurrency,source_concurrency:sourceConcurrency});
  if(path==='/internal/catalog'&&req.method==='GET')return json(res,200,{ok:true,profile,updated_at:upstream.status().updated_at,channels:upstream.catalog()});
  if(path==='/internal/modules'&&req.method==='GET')return json(res,200,{ok:true,profile,modules:modules()});
  if(path==='/internal/epg'&&req.method==='GET'){const output=upstream.epg();res.writeHead(200,{'Content-Type':'application/xml; charset=utf-8','Cache-Control':'no-store','Content-Length':output.length});res.end(output);return;}
  const match=/^\/internal\/media\/([0-9a-f]{32})\/index\.(m3u8|ts|m4s|mp4|key|flv)$/.exec(path);
  if(match&&['GET','HEAD'].includes(req.method))return media(findLease(match[1]),req,res);
  if(req.method!=='POST'){fail(res,404,'unknown internal route','not_found');return;}
  const input=await body(req);
  if(path==='/internal/sync') {
    if(input.modules!==undefined&&(!Array.isArray(input.modules)||input.modules.some(id=>typeof id!=='string'||!upstream.getModule(id))))throw new Error('modules must be registered module ids');
    void upstream.refresh(input.modules).catch(error=>console.error('Catalog refresh:',safeMessage(error)));
    return json(res,202,{ok:true,refreshing:true});
  }
  if(path==='/internal/resolve') {
    if(input.format&&!['hls','flv'].includes(input.format))throw new Error('format must be hls or flv');
    if(input.refresh!==undefined&&typeof input.refresh!=='boolean')throw new Error('refresh must be boolean');
    const record=upstream.lookup(input.provider_key,input.provider_ref);
    const replay=replayWindow(input.start,input.end,record.item.catchup);
    const resolved=await resolveChannel(record,input);
    const format=resolved.streamType==='flv'?'flv':'hls';
    const ttlMs=mediaLeaseMs(record.item,replay);
    const url=lease({kind:'channel',ttlMs,ownerKey:input.provider_key,ownerRef:input.provider_ref,start:input.start,end:input.end,format},format==='flv'?'flv':'m3u8');
    return json(res,200,{ok:true,url,headers:{'X-IPTV-Secret':secret},format,mime:format==='flv'?'video/x-flv':'application/vnd.apple.mpegurl',expires_at:new Date(Date.now()+ttlMs).toISOString(),catchup:record.item.catchup,
      mode:input.start?'catchup':record.item.mode||'live',kind:record.item.kind||'tv',programme_id:record.item.programme_id||null,duration_seconds:record.item.duration_seconds||null});
  }
  if(path==='/internal/modules/config') {
    const module=upstream.getModule(input.id);if(!module)throw new Error('unknown module');
    if(input.enabled!==undefined&&typeof input.enabled!=='boolean')throw new Error('enabled must be boolean');
    if(input.id==='migu-sports') {
      upstream.sports.configure(input);revokeModuleMedia(input.id);upstream.rebuildCatalog();void upstream.refresh([input.id]);
      return json(res,200,{ok:true,profile,module:upstream.sports.state()});
    }
    if(input.config!==undefined||input.refresh_minutes!==undefined) {
      input.config??={};
      if(!input.config||typeof input.config!=='object'||Array.isArray(input.config))throw new Error('config must be an object');
      const schema=new Map((module.configSchema||[]).map(field=>[field.key,field]));
      for(const key of Object.keys(input.config)){if(!schema.has(key))throw new Error(`unknown config field: ${key}`);if(profile==='public'&&schema.get(key).secret){fail(res,403,'platform credentials belong to the member profile','profile_isolation');return;}}
      upstream.manager.updateModuleConfig(input.id,input.config,{refreshMinutes:input.refresh_minutes});
    }
    if(input.enabled!==undefined)upstream.manager.setModuleEnabled(input.id,input.enabled);
    revokeModuleMedia(input.id);
    upstream.rebuildCatalog();void upstream.refresh([input.id]).catch(error=>console.error('Module refresh:',safeMessage(error)));
    return json(res,200,{ok:true,profile,module:modules().find(row=>row.id===input.id)});
  }
  if(path==='/internal/modules/login') {
    if(profile!=='member'){fail(res,403,'login is only available in the member profile','profile_isolation');return;}
    if(!upstream.registry.getModule(input.id))throw new Error('unknown module');
    const api=await import('../utils/extractorsAPI.js');
    const actions={start:()=>api.startModuleLoginAPI(input.id),poll:()=>api.pollModuleLoginAPI(input.id,input.key),
      browserStart:()=>api.startBrowserLoginAPI(input.id),browserStatus:()=>api.getBrowserLoginStatusAPI(input.id),
      browserCheck:()=>api.checkBrowserLoginAPI(input.id),browserCancel:()=>api.cancelBrowserLoginAPI(input.id),
      browserClose:()=>api.closeBrowserLoginAPI(input.id),browserImport:()=>api.importBrowserLoginAPI(input.id,input.payload)};
    if(!actions[input.action])throw new Error('unsupported login action');
    const result=await actions[input.action]();
    if(!result.success){fail(res,400,result.message,'login_failed');return;}
    const source=result.data||{};
    if((input.action==='poll'&&source.status==='ok')||
      (['browserImport','browserCheck'].includes(input.action)&&source.authenticated===true)) {
      revokeModuleMedia(input.id);upstream.rebuildCatalog();
      void upstream.refresh(input.id==='migu'?['migu','migu-sports']:[input.id]).catch(error=>console.error('Login refresh:',safeMessage(error)));
    }
    const data={};
    for(const key of ['key','image','status','message','active','available','reason','running','visible','authenticated'])if(source[key]!==undefined)data[key]=source[key];
    if(source.account&&typeof source.account==='object')data.account={nickname:String(source.account.nickname||'').slice(0,120),type:String(source.account.type||'').slice(0,40),vip:source.account.vip===true};
    return json(res,200,{ok:true,data});
  }
  fail(res,404,'unknown internal route','not_found');
}
const server=http.createServer((req,res)=>{handle(req,res).catch(error=>fail(res,req.method==='POST'?400:502,error));});
server.keepAliveTimeout=65000;server.headersTimeout=66000;server.requestTimeout=30000;
const sockets=new Set();server.on('connection',socket=>{sockets.add(socket);socket.on('close',()=>sockets.delete(socket));});
let closing=false;
async function shutdown() {
  if(closing)return;closing=true;clearInterval(tick);clearInterval(watchdog);
  server.close();for(const socket of sockets)socket.destroy();
  await Promise.race([upstream.close(),new Promise(resolve=>setTimeout(resolve,8000))]);process.exit(0);
}
const tick=setInterval(()=>{void upstream.refresh(undefined,{auto:true}).catch(error=>console.error('Scheduled refresh:',safeMessage(error)));},60000);tick.unref();
const parentPid=Number(process.env.IPTV_PARENT_PID||0);
const watchdog=setInterval(()=>{if(!parentPid)return;try{process.kill(parentPid,0);}catch(error){if(error.code==='ESRCH')void shutdown();}},5000);watchdog.unref();
process.once('SIGINT',()=>void shutdown());process.once('SIGTERM',()=>void shutdown());
server.listen(port,'127.0.0.1',()=>{
  console.log(`IPTV adapter ${VERSION} ${profile} listening on 127.0.0.1:${port}`);
  if(process.env.IPTV_AUTO_REFRESH!=='false')void upstream.refresh().catch(error=>console.error('Initial refresh:',safeMessage(error)));
});
