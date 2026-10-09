// SPDX-License-Identifier: GPL-3.0-only
// Bounded live FLV transport/tag probe. No source URL, secret or payload dump.
import { performance } from 'node:perf_hooks';
const port=Number(process.env.IPTV_PORT||9180),origin=`http://127.0.0.1:${port}`;
const secret=process.env.IPTV_INTERNAL_SECRET||'';
if(!secret)throw new Error('IPTV_INTERNAL_SECRET required');
const seconds=Math.max(5,Math.min(60,Number(process.env.IPTV_PROBE_SECONDS)||20));
const headers={'X-IPTV-Secret':secret};
const ref=process.env.IPTV_PROBE_REF||'fengshows-chinese.flv';
const resolved=await(await fetch(origin+'/internal/resolve',{method:'POST',headers:{...headers,'Content-Type':'application/json'},
  body:JSON.stringify({provider_key:'fengshows',provider_ref:ref}),signal:AbortSignal.timeout(20000)})).json();
if(!resolved.ok)throw new Error('internal source resolve failed');
const target=new URL(resolved.url);
if(target.origin!==origin||!target.pathname.startsWith('/internal/media/'))throw new Error('unexpected media origin');
const ctrl=new AbortController(),timer=setTimeout(()=>ctrl.abort(),seconds*1000),started=performance.now();
const report={ref,requested_seconds:seconds,status:null,mime:null,content_length:null,content_encoding:null,
  elapsed_seconds:0,bytes:0,normal_eof:false,video:{tags:0,keyframes:0,first_dts:null,last_dts:null,first_pts:null,last_pts:null},audio:{tags:0,first_dts:null,last_dts:null},bytes_each_second:[]};
let pending=Buffer.alloc(0),header=false;
function parse(chunk) {
  pending=Buffer.concat([pending,chunk]);
  if(!header) {
    if(pending.length<13)return;
    if(pending.subarray(0,3).toString()!=='FLV')throw new Error('upstream did not return FLV');
    const skip=pending.readUInt32BE(5)+4;if(pending.length<skip)return;pending=pending.subarray(skip);header=true;
  }
  let offset=0;
  while(pending.length-offset>=11) {
    const size=pending.readUIntBE(offset+1,3);if(size>16*1024*1024)throw new Error('invalid FLV tag size');
    if(pending.length-offset<size+15)break;
    const kind=pending[offset],body=pending.subarray(offset+11,offset+11+size);
    const dts=(pending.readUIntBE(offset+4,3)+pending[offset+7]*0x1000000)/1000;
    if(kind===9&&body.length>=5&&body[1]===1) {
      const pts=dts+body.readIntBE(2,3)/1000,row=report.video;
      row.tags++;if(body[0]>>4===1)row.keyframes++;row.first_dts??=dts;row.first_pts??=pts;row.last_dts=dts;row.last_pts=pts;
    } else if(kind===8&&body.length>=2&&body[1]===1) {
      const row=report.audio;row.tags++;row.first_dts??=dts;row.last_dts=dts;
    }
    offset+=size+15;
  }
  pending=pending.subarray(offset);
}
try {
  const response=await fetch(target,{headers,signal:ctrl.signal});
  report.status=response.status;report.mime=response.headers.get('content-type');report.content_length=response.headers.get('content-length');report.content_encoding=response.headers.get('content-encoding');
  if(!response.ok)throw new Error('internal media request failed');
  for await(const chunk of response.body) {
    const slot=Math.floor((performance.now()-started)/1000);
    report.bytes_each_second[slot]=(report.bytes_each_second[slot]||0)+chunk.length;report.bytes+=chunk.length;
    if(report.bytes>128*1024*1024)throw new Error('probe byte budget reached');parse(Buffer.from(chunk));
  }
  report.normal_eof=true;
} catch(error) {
  if(!ctrl.signal.aborted)report.error=String(error.message).replace(/https?:\/\/\S+/g,'[media]').slice(0,160);
} finally {clearTimeout(timer);report.elapsed_seconds=Number(((performance.now()-started)/1000).toFixed(2));}
console.log(JSON.stringify(report,null,2));
