// SPDX-License-Identifier: GPL-3.0-only
import dns from 'node:dns/promises';
import http from 'node:http';
import https from 'node:https';
import { isIP } from 'node:net';
import fetch from 'node-fetch';
import { publicUrl, isPublicAddress } from './contract.mjs';

const REDIRECTS = new Set([301,302,303,307,308]);
const FORBIDDEN_HEADERS = new Set(['host', 'connection', 'x-iptv-secret', 'proxy-authorization', 'proxy-connection']);

export function mediaHeaders(source, url, range, mediaType) {
  const original = typeof source === 'function' ? source(url) : source;
  // FLV uses the upstream flvProxy's native fetch identity. Phoenix's official
  // CDN ends the response after a short burst when given the HLS browser UA.
  const result = mediaType==='flv'?{'User-Agent':'node','Accept-Encoding':'identity'}:
    { 'User-Agent': 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36' };
  for (const [key,value] of Object.entries(original || {})) {
    // Match upstream hlsProxy: static declarations use the proxy browser UA;
    // per-target functions may select the official player UA for that CDN.
    if(key.toLowerCase()==='user-agent'&&typeof source!=='function'&&mediaType!=='flv')continue;
    if (!FORBIDDEN_HEADERS.has(key.toLowerCase()) && typeof value === 'string' && !/[\r\n]/.test(value)) result[key] = value;
  }
  if (range && /^bytes=\d*-\d*(?:,\d*-\d*)*$/.test(range)) result.Range = range;
  return result;
}

export async function checkedFetch(raw, { headers, range, method = 'GET', mediaType, timeoutMs = 15000, idleMs = 30000, fetchImpl = fetch, lookupImpl = dns.lookup } = {}) {
  let target = publicUrl(raw);
  for (let hop=0; hop<=5; hop++) {
    const hostname = target.hostname.replace(/^\[|\]$/g, '');
    const addresses = isIP(hostname) ? [{ address: hostname, family: isIP(hostname) }] : await lookupImpl(hostname, {all:true});
    // A fake-IP TUN mapping is accepted only for a syntactically public DNS name,
    // never an IP literal or a hostname resolving into actual private networks.
    if (!addresses.length || addresses.some(row => !isPublicAddress(row.address, !isIP(hostname)))) throw new Error('private media destination rejected');
    const address = addresses.find(row => row.family === 4) || addresses[0];
    const lookup = (_name, options, callback) => {
      if (options?.all) callback(null, [address]);
      else callback(null, address.address, address.family);
    };
    const agent = target.protocol === 'https:' ? new https.Agent({lookup}) : new http.Agent({lookup});
    const connect=agent.createConnection;
    agent.createConnection=function(options,callback) {
      const socket=connect.call(this,options,callback);
      socket.setTimeout(idleMs,()=>socket.destroy(new Error('media upstream idle timeout')));
      return socket;
    };
    const controller=new AbortController();
    const deadline=setTimeout(()=>controller.abort(),Math.min(timeoutMs,30000));deadline.unref();
    let response;
    try {
      response=await fetchImpl(target.href, {method, headers:mediaHeaders(headers,target.href,range,mediaType), redirect:'manual', signal:controller.signal, agent,size:32*1024*1024});
    } finally {clearTimeout(deadline);}
    // Body length is unlimited for streaming. TCP inactivity closes a stalled
    // source, while a healthy FLV connection can outlive the request deadline.
    // Each agent belongs to this one checked request. Closing the response body
    // must immediately stop the socket when its downstream viewer disconnects.
    response.body?.once?.('close',()=>agent.destroy());
    if (!REDIRECTS.has(response.status)) return response;
    response.body?.destroy();
    const location = response.headers.get('location');
    if (!location || hop === 5) throw new Error('invalid media redirect');
    const next = publicUrl(new URL(location,target).href);
    // Account credentials never follow a redirect to a different origin.
    if (next.origin !== target.origin && typeof headers !== 'function') {
      headers = Object.fromEntries(Object.entries(headers || {}).filter(([key]) => !/^(cookie|authorization)$/i.test(key)));
    }
    target = next;
  }
  throw new Error('media redirect limit exceeded');
}
