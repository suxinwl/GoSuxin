// SPDX-License-Identifier: GPL-3.0-only
// Compare official FLV transport with an anonymous ticket; never print the URL.
import { performance } from 'node:perf_hooks';
import nodeFetch from 'node-fetch';
import { resolveChannel } from '../extractors/fengshows/api.js';
import { checkedFetch } from './network.mjs';
const ticket=await resolveChannel('fengshows-chinese.flv',{config:{token:''}});
if(!ticket.url)throw new Error('anonymous official source resolve failed');
const seconds=20;
const modes=process.env.IPTV_TRANSPORT_MODES?.split(',')||['native_identity','checked_default','checked_keepalive'];
for(const mode of modes) {
  const started=performance.now(),report={mode,status:null,bytes:0,elapsed_seconds:0,normal_eof:false};
  let response,stop,timer;
  try {
    if(mode.startsWith('native_')) {
      const controller=new AbortController();stop=()=>controller.abort();timer=setTimeout(stop,seconds*1000);
      response=await fetch(ticket.url,{headers:{'Accept-Encoding':'identity',...(mode==='native_chrome'?{'User-Agent':'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36'}:{})},signal:controller.signal});
    } else {
      response=await checkedFetch(ticket.url,{headers:mode.startsWith('checked_identity')?()=>({'User-Agent':'node','Accept-Encoding':'identity'}):undefined,fetchImpl:mode.endsWith('keepalive')?async(url,options)=>{
        options.agent.keepAlive=true;options.agent.options.keepAlive=true;
        return nodeFetch(url,options);
      }:nodeFetch});
      stop=()=>response.body.destroy();timer=setTimeout(stop,seconds*1000);
    }
    report.status=response.status;report.content_length=response.headers.get('content-length');report.content_encoding=response.headers.get('content-encoding');
    for await(const chunk of response.body){report.bytes+=chunk.length;if(report.bytes>128*1024*1024){stop();break;}}
    report.normal_eof=performance.now()-started<seconds*1000-50;
  } catch(error) {report.error=String(error.message).replace(/https?:\/\/\S+/g,'[media]').slice(0,160);}
  finally{clearTimeout(timer);stop?.();report.elapsed_seconds=Number(((performance.now()-started)/1000).toFixed(2));}
  console.log(JSON.stringify(report));
}
