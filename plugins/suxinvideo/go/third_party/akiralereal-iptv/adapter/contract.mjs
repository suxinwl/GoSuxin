// SPDX-License-Identifier: GPL-3.0-only
import { createHash, timingSafeEqual } from 'node:crypto';
import { isIP } from 'node:net';

export const VERSION = '4.27.0';
export const COMMIT = '7b1e2d5fdc6dbe35de63f099be6264a201c6a975';
export const MANUAL_MODULES = new Set(['bilibili-live', 'douyin-live', 'douyu-live', 'huya-live', 'ipanda', 'livechina', 'songjiang', 'qtv']);

export function authenticated(value, secret) {
  const actual = Buffer.from(typeof value === 'string' ? value : '');
  const expected = Buffer.from(secret);
  return actual.length === expected.length && timingSafeEqual(actual, expected);
}

export function stableRef(moduleId, channel) {
  if (channel.deferredRef != null) return String(channel.deferredRef);
  return `direct-${createHash('sha256').update(moduleId + '\0' + channel.name).digest('hex').slice(0, 24)}`;
}

export function accountRequired(moduleId, channel) {
  return /^ysp-vip-/.test(String(channel.deferredRef || '')) ||
    (['beijing', 'sichuan'].includes(moduleId) && channel.requiresLogin === true);
}

export function catchupSupported(moduleId, channel) {
  return moduleId === 'migu' && channel.catchup !== 'none' && channel.wantsPlayback === true;
}

export function shanghaiTimestamp(value) {
  if (typeof value !== 'string') throw new Error('start/end must be timestamps');
  if (/^\d{14}$/.test(value)) {
    const date = new Date(`${value.slice(0,4)}-${value.slice(4,6)}-${value.slice(6,8)}T${value.slice(8,10)}:${value.slice(10,12)}:${value.slice(12,14)}+08:00`);
    if (!Number.isFinite(date.getTime()) || formatShanghai(date) !== value) throw new Error('invalid Shanghai timestamp');
    return value;
  }
  if (!/^\d{4}-\d{2}-\d{2}T.*(?:Z|[+-]\d{2}:\d{2})$/.test(value)) throw new Error('use RFC3339 with a timezone');
  const date = new Date(value);
  if (!Number.isFinite(date.getTime())) throw new Error('invalid timestamp');
  return formatShanghai(date);
}

function formatShanghai(value) {
  return new Date(value.getTime() + 8 * 3600000).toISOString().slice(0,19).replace(/[-T:]/g, '');
}

export function replayWindow(start, end, supported) {
  if (!start && !end) return null;
  if (!supported) throw new Error('this channel has no verified catchup support');
  if (!start || !end) throw new Error('start and end are both required');
  const begin = shanghaiTimestamp(start), stop = shanghaiTimestamp(end);
  if (begin >= stop) throw new Error('end must be after start');
  return { begin, end: stop };
}

export function mediaLeaseMs(item,replay=null) {
  if(!replay&&item.kind!=='event_replay')return 20*60000;
  const date=value=>Date.parse(`${value.slice(0,4)}-${value.slice(4,6)}-${value.slice(6,8)}T${value.slice(8,10)}:${value.slice(10,12)}:${value.slice(12,14)}+08:00`);
  const seconds=replay?(date(replay.end)-date(replay.begin))/1000:item.duration_seconds;
  // An immutable ENDLIST manifest may never reload during a multi-hour match.
  // Keep every issued fragment/key usable for its finite playback session.
  return (Math.min(24*3600,Number(seconds)>0?Number(seconds):24*3600)+20*60)*1000;
}

export function isPublicAddress(address, allowTun = false) {
  const raw = address.toLowerCase().split('%')[0];
  if (raw.startsWith('::ffff:')) return isPublicAddress(raw.slice(7), allowTun);
  if (isIP(raw) === 4) {
    const [a,b,c] = raw.split('.').map(Number);
    if (a === 198 && (b === 18 || b === 19)) return allowTun;
    return !(a === 0 || a === 10 || a === 127 || a >= 224 ||
      (a === 169 && b === 254) || (a === 172 && b >= 16 && b <= 31) ||
      (a === 192 && (b === 168 || b === 0 || (b === 88 && c === 99))) ||
      (a === 100 && b >= 64 && b <= 127) || (a === 198 && b === 51 && c === 100) ||
      (a === 203 && b === 0 && c === 113));
  }
  // Only globally routed IPv6 unicast. Reject mapped, loopback, ULA, link-local,
  // multicast, documentation and transition prefixes before a socket is opened.
  return isIP(raw) === 6 && /^[23]/.test(raw) && !raw.startsWith('2001:db8:') &&
    !raw.startsWith('2001:0:') && !raw.startsWith('2002:');
}

export function publicUrl(raw) {
  const url = new URL(raw);
  const hostname = url.hostname.replace(/^\[|\]$/g, '');
  if (!['http:', 'https:'].includes(url.protocol) || url.username || url.password ||
      url.hash || !hostname || hostname.endsWith('.') || hostname === 'localhost' ||
      /(?:^|\.)(?:local|localhost|internal|lan|home|test|invalid)$/.test(hostname) ||
      (!isIP(hostname) && !hostname.includes('.')) ||
      (isIP(hostname) && !isPublicAddress(hostname))) {
    throw new Error('media URL must use a public HTTP(S) host');
  }
  return url;
}

export function rewriteHls(text, base, register) {
  if (!text.trimStart().startsWith('#EXTM3U')) throw new Error('upstream did not return HLS');
  if (Buffer.byteLength(text) > 2 * 1024 * 1024) throw new Error('HLS manifest too large');
  const convert = (raw,hint) => register(new URL(raw, base).href,hint);
  return text.split('\n').map(line => {
    const trimmed = line.trim();
    if (!trimmed) return line;
    if (trimmed.startsWith('#')) return line.replace(/URI="([^"]+)"/g, (_all, raw) => `URI="${convert(raw,/^#EXT-X-(?:SESSION-)?KEY:/.test(trimmed)?'key':undefined)}"`);
    return convert(trimmed);
  }).join('\n');
}

export function mediaMime(bytes,fallback='video/mp2t') {
  if(bytes.length>=3&&bytes.subarray(0,3).toString()==='FLV')return 'video/x-flv';
  if(bytes.length>=8&&['ftyp','styp','moof','moov'].includes(bytes.subarray(4,8).toString()))return 'video/mp4';
  if(bytes.length>=2&&bytes[0]===255&&(bytes[1]&246)===240)return 'audio/aac';
  if(bytes.length&&bytes[0]===71)return 'video/mp2t';
  return fallback;
}

export function safeMessage(error) {
  return String(error?.message || error || 'service unavailable')
    .replace(/https?:\/\/\S+/g, '[upstream]')
    .replace(/(?:token|cookie|sessdata|authorization)\s*[:=]\s*\S+/ig, '[credential]')
    .slice(0, 500);
}
