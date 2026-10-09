// SPDX-License-Identifier: GPL-3.0-only
// Run against a managed instance; output deliberately excludes media URLs and secrets.
const port=Number(process.env.IPTV_PORT||9180),origin=`http://127.0.0.1:${port}`;
const headers={'X-IPTV-Secret':process.env.IPTV_INTERNAL_SECRET||''};
if(!headers['X-IPTV-Secret'])throw new Error('IPTV_INTERNAL_SECRET required');
const get=path=>fetch(origin+path,{headers,signal:AbortSignal.timeout(60000)});
const health=await(await get('/internal/health')).json();
const catalog=await(await get('/internal/catalog')).json();
const epg=await(await get('/internal/epg')).text();
const selections=[['yangshipin','CCTV1'],['migu','CCTV1'],['heilongjiang','黑龙江卫视'],['hkstv',''],['lotustv',''],['fengshows','凤凰中文'],['migu-sports','']];
const results=[];
async function sample(url,depth=0){
  const parsed=new URL(url);
  if(parsed.origin!==origin||!parsed.pathname.startsWith('/internal/media/'))throw new Error('manifest exposed a URL outside the internal transport');
  const response=await fetch(url,{headers,signal:AbortSignal.timeout(60000)});
  if(!response.ok)return {media_status:response.status,mime:response.headers.get('content-type'),depth};
  const mime=response.headers.get('content-type')||'';
  if(mime.includes('mpegurl')){
    const text=await response.text();
    const targets=[...text.matchAll(/URI="([^"]+)"/g)].map(match=>match[1]);
    targets.push(...text.split('\n').map(line=>line.trim()).filter(line=>line&&!line.startsWith('#')));
    for(const target of targets){const value=new URL(target,url);if(value.origin!==origin||!value.pathname.startsWith('/internal/media/'))throw new Error('unprotected HLS target');}
    const first=text.split('\n').map(line=>line.trim()).find(line=>line&&!line.startsWith('#'));
    if(!first||depth>4)throw new Error('no HLS media fragment');
    const result=await sample(new URL(first,url).href,depth+1);
    if(text.includes('#EXT-X-ENDLIST')){result.manifest_endlist=true;result.manifest_duration_seconds=[...text.matchAll(/#EXTINF:([\d.]+)/g)].reduce((sum,match)=>sum+Number(match[1]),0);}
    return result;
  }
  const reader=response.body.getReader();let length=0,first;
  while(length<65536){const row=await reader.read();if(row.done)break;first||=row.value;length+=row.value.length;}
  await reader.cancel();
  return {media_status:response.status,mime,media_bytes:length,signature:Buffer.from(first||[]).subarray(0,12).toString('hex'),depth};
}
for(const [module,name]of selections){
  const channel=catalog.channels.find(row=>row.provider_key===module&&row.name.includes(name)&&(module!=='migu-sports'||row.kind==='event_replay'));
  if(!channel){results.push({module,status:'not_in_catalog'});continue;}
  try{
    const response=await fetch(origin+'/internal/resolve',{method:'POST',headers:{...headers,'Content-Type':'application/json'},body:JSON.stringify({provider_key:channel.provider_key,provider_ref:channel.provider_ref}),signal:AbortSignal.timeout(60000)});
    const result=await response.json();
    if(!result.ok){results.push({module,name:channel.name,ref:channel.provider_ref,resolve_status:response.status,error:result.message});continue;}
    results.push({module,name:channel.name,ref:channel.provider_ref,resolve_status:response.status,format:result.format,mode:result.mode,duration_seconds:result.duration_seconds,...await sample(result.url)});
  }catch(error){results.push({module,name:channel.name,ref:channel.provider_ref,error:String(error.message).replace(/https?:\/\/\S+/g,'[media]').slice(0,200)});}
}
console.log(JSON.stringify({at:new Date().toISOString(),health,catalog_count:catalog.channels.length,epg_channels:(epg.match(/<channel /g)||[]).length,epg_programmes:(epg.match(/<programme /g)||[]).length,results},null,2));
