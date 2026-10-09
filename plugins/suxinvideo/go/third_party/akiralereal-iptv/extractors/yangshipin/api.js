import { createCKey, clientConstants } from './ckey.js'

export const API_URL = 'https://bkliveinfo.ysp.cctv.cn/'
export const UPSTREAM_HEADERS = Object.freeze({
  Accept: 'application/vnd.apple.mpegurl,application/json,*/*',
  Referer: 'https://live.cctv.cn/',
  'User-Agent': 'qqlive',
})

// 只声明 AVC/H.264 能力，不向接口申报 HEVC；用于电视盒子和内置播放器兼容模式。
const H264_CAPABILITY = Buffer.from('H(30:1080,60:1080|30:1080,60:1080)').toString('base64')

function withTimeout(timeoutMs) {
  const controller = new AbortController()
  const timer = setTimeout(() => controller.abort(), timeoutMs)
  return { signal: controller.signal, done: () => clearTimeout(timer) }
}

export function isOfficialMediaUrl(value) {
  try {
    const url = new URL(value)
    return url.protocol === 'https:' && (
      url.hostname === 'cctv.cn'
      || url.hostname.endsWith('.cctv.cn')
      || url.hostname === 'ysp.cctv.cn'
      || url.hostname.endsWith('.ysp.cctv.cn')
      || url.hostname === 'cctv.com'
      || url.hostname.endsWith('.cctv.com')
    )
  } catch {
    return false
  }
}

function collectUrls(payload) {
  const values = [payload?.playurl]
  const backups = payload?.backurl_list ?? payload?.backurlList ?? payload?.backurl
  if (Array.isArray(backups)) {
    for (const item of backups) values.push(typeof item === 'string' ? item : item?.url || item?.playurl)
  } else if (typeof backups === 'string') {
    values.push(...backups.split(/[;,]/))
  }
  return [...new Set(values.map(value => String(value || '').trim()).filter(isOfficialMediaUrl))]
}

export async function requestPlayUrls(channel, options = {}) {
  const fetchImpl = options.fetchImpl || fetch
  const ticket = createCKey(channel.channelId, { now: options.now })
  const query = new URLSearchParams({
    atime: '120',
    livepid: channel.livePid,
    cnlid: channel.channelId,
    appVer: clientConstants.appVersion,
    app_version: '300090',
    caplv: '1',
    cmd: '2',
    defn: channel.defn || 'fhd',
    device: 'iPhone',
    encryptVer: '4.2',
    getpreviewinfo: '0',
    hevclv: '0',
    lang: 'zh-Hans_CN',
    livequeue: '0',
    logintype: '1',
    nettype: '1',
    newnettype: '1',
    newplatform: String(clientConstants.platform),
    platform: String(clientConstants.platform),
    sdtfrom: 'v3021',
    spacode: '23',
    spaudio: '1',
    spdemuxer: '6',
    spdrm: '2',
    spdynamicrange: '1',
    spflv: '1',
    spflvaudio: '1',
    sphdrfps: '60',
    sphttps: '1',
    spvcode: H264_CAPABILITY,
    spvideo: '4',
    stream: '1',
    system: '1',
    sysver: 'ios18.2.1',
    uhd_flag: '0',
    cKey: ticket.cKey,
    guid: ticket.guid,
    fntick: String(ticket.timestamp),
    flowid: ticket.flowId,
    playbacktime: '0',
  })
  const timeout = withTimeout(Number(options.timeoutMs || 12_000))
  try {
    const response = await fetchImpl(`${API_URL}?${query}`, {
      redirect: 'follow',
      signal: timeout.signal,
      headers: { 'User-Agent': 'qqlive', Accept: 'application/json' },
    })
    if (!response.ok) throw new Error(`官方接口 HTTP ${response.status}`)
    const payload = await response.json()
    if (Number(payload?.iretcode) !== 0) throw new Error(payload?.errinfo || `官方接口返回 ${payload?.iretcode ?? '未知错误'}`)
    const urls = collectUrls(payload)
    if (!urls.length) throw new Error('官方接口没有返回可用的 HLS 地址')
    return { urls, payload }
  } finally {
    timeout.done()
  }
}

function firstVariant(text, base) {
  const lines = text.split(/\r?\n/)
  for (let i = 0; i < lines.length; i++) {
    if (!lines[i].trim().startsWith('#EXT-X-STREAM-INF')) continue
    for (let j = i + 1; j < lines.length; j++) {
      const value = lines[j].trim()
      if (!value || value.startsWith('#')) continue
      try { return new URL(value, base).href } catch { return '' }
    }
  }
  return ''
}

function mediaSegments(text, base) {
  return text.split(/\r?\n/)
    .map(line => line.trim())
    .filter(line => line && !line.startsWith('#'))
    .map(line => {
      try { return new URL(line, base).href } catch { return '' }
    })
    .filter(isOfficialMediaUrl)
}

async function fetchManifest(url, fetchImpl, signal) {
  const response = await fetchImpl(url, { redirect: 'follow', signal, headers: UPSTREAM_HEADERS })
  if (!response.ok) throw Object.assign(new Error(`清单 HTTP ${response.status}`), { status: response.status })
  const text = await response.text()
  if (!text.trimStart().startsWith('#EXTM3U')) throw new Error('响应不是 HLS 清单')
  return { text, url: response.url || url }
}

/**
 * 逐一检查官方主/备 CDN，取回能拍平成媒体清单的那一条。
 *
 * 解析层传入 segmentProbeState 时，每频道、每 CDN 至多一分钟探测一次 TS 的前 188 字节。
 * 清单正常、分片却 403 的线路可让位给分片验证成功的备用线路。服务端出口不等于播放器出口，
 * 且额外请求可能遇限频：主备都未通过分片探测时仍保留第一条有效清单，不把频道判死或触发冷却。
 *
 * 每个入口最多等 MANIFEST_TIMEOUT_MS。官方入口偶尔会挂住不回应，原先等满 10 秒才换备用入口，
 * 而播放器手里只有十几秒内容，这一等画面必停（libVLC 实测晚到 5～8 秒，主备都挂时等了 20 秒）。
 * 正常取清单很快：经 NAS 实例统计 205 次，中位 0.15 秒、95% 在 0.7 秒内、最慢的正常响应不到 1 秒。
 */
export const MANIFEST_TIMEOUT_MS = 3_000
export const SEGMENT_PROBE_INTERVAL_MS = 60_000
export const SEGMENT_PROBE_TIMEOUT_MS = 1_000

async function probeSegment(url, options) {
  const state = options.segmentProbeState
  if (!state) return { ok: true }
  const now = Number(options.now ?? Date.now())
  const parsed = new URL(url)
  // Ticket paths and sequence numbers change; probe at most once per CDN/stream.
  const key = `${parsed.origin}/${parsed.pathname.split('/').pop().replace(/-\d+\.ts$/, '')}`
  const cached = state.get(key)
  if (cached && now - cached.at < SEGMENT_PROBE_INTERVAL_MS) return cached
  const timeout = withTimeout(Number(options.segmentProbeTimeoutMs || SEGMENT_PROBE_TIMEOUT_MS))
  let result
  try {
    let current = url, response
    for (let redirects = 0; redirects <= 3; redirects++) {
      const target = new URL(current)
      if (!isOfficialMediaUrl(current) || (target.port && target.port !== '443')) throw new Error('分片重定向不是官方 HTTPS 地址')
      response = await (options.fetchImpl || fetch)(current, {
        redirect: 'manual', signal: timeout.signal,
        headers: { ...UPSTREAM_HEADERS, Range: 'bytes=0-187' },
      })
      if (![301, 302, 303, 307, 308].includes(response.status)) break
      const location = response.headers.get('location')
      await response.body?.cancel()
      if (!location || redirects === 3) throw new Error('分片重定向异常')
      current = new URL(location, current).href
    }
    if (!response.ok) {
      await response.body?.cancel()
      throw new Error(`分片 HTTP ${response.status}`)
    }
    // Some nodes ignore Range. Read just one TS packet, then cancel the body.
    const reader = response.body?.getReader()
    if (!reader) throw new Error('分片没有正文')
    let count = 0, sync = false
    try {
      while (count < 188) {
        const chunk = await reader.read()
        if (chunk.done) break
        if (!count && chunk.value.length) sync = chunk.value[0] === 0x47
        count += chunk.value.length
      }
    } finally { await reader.cancel() }
    if (count < 188 || !sync) throw new Error('分片不是有效 TS 包')
    result = { at: now, ok: true }
  } catch (error) {
    result = { at: now, ok: false, reason: error?.name === 'AbortError' ? '分片超时' : error.message }
  } finally { timeout.done() }
  state.delete(key)
  state.set(key, result)
  while (state.size > 8) state.delete(state.keys().next().value)
  options.onSegmentProbe?.({ host: parsed.hostname, ...result })
  return result
}

export async function selectWorkingManifest(urls, options = {}) {
  const fetchImpl = options.fetchImpl || fetch
  const errors = []
  let forbidden = 0
  let fallback
  for (const url of urls) {
    const timeout = withTimeout(Number(options.timeoutMs || MANIFEST_TIMEOUT_MS))
    try {
      let manifest = await fetchManifest(url, fetchImpl, timeout.signal)
      const variant = firstVariant(manifest.text, manifest.url)
      if (variant && isOfficialMediaUrl(variant)) manifest = await fetchManifest(variant, fetchImpl, timeout.signal)
      const segments = mediaSegments(manifest.text, manifest.url)
      if (!segments.length) throw new Error('媒体清单没有分片')
      const candidate = { ...manifest, sourceUrl: url }
      // The newest fragment may still be publishing. Probe the penultimate one.
      const probe = await probeSegment(segments[Math.max(0, segments.length - 2)], options)
      if (probe.ok) return candidate
      fallback ||= candidate
    } catch (error) {
      if (error?.status === 403) forbidden++
      let host = '未知节点'
      try { host = new URL(url).hostname } catch { /* 保留默认文案 */ }
      errors.push(`${host}: ${error?.name === 'AbortError' ? '超时' : error?.message || error}`)
    } finally {
      timeout.done()
    }
  }
  if (fallback) return fallback
  // allForbidden：主备全是 403，即本机出口被 CDN 限流的特征，由解析层据此冷却
  throw Object.assign(new Error(`主、备用 CDN 均不可用（${errors.join('；')}）`), {
    allForbidden: urls.length > 0 && forbidden === urls.length,
  })
}
