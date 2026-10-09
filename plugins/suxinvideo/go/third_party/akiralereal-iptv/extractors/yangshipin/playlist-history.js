// Only extend the simple public TS feed. Stateful HLS tags require a different
// merger; pass those feeds through rather than lose encryption/discontinuities.
const UNSUPPORTED = /^#EXT-X-(?!VERSION:|MEDIA-SEQUENCE:|TARGETDURATION:|ALLOW-CACHE:|INDEPENDENT-SEGMENTS\s*$|START:|PROGRAM-DATE-TIME:)/m
const SEGMENT_TAG = /^#(?:EXTINF|EXT-QQHLS-MACHINEID|EXT-QQHLS-START-TIME|EXT-SID|EXT-X-PROGRAM-DATE-TIME)\b/
export const HISTORY_SECONDS = 30
export const HISTORY_MIN_SEGMENTS = 6
export const HISTORY_MAX_SEGMENTS = 10
export const HISTORY_IDLE_MS = 30_000

function parse(text) {
  const body = String(text).replace(/\r/g, '')
  if (UNSUPPORTED.test(body)) return null
  const first = Number(body.match(/^#EXT-X-MEDIA-SEQUENCE:\s*(\d+)/m)?.[1])
  const range = body.match(/^#EXT-QQHLS-SEGMENT_RANGE:\s*(\d+)-(\d+)/m)
  if (!Number.isSafeInteger(first) || !range) return null
  const kept = Number(range[1]), last = Number(range[2])
  const header = [], segments = []
  let tags = [], duration = null
  for (const line of body.trimEnd().split('\n')) {
    if (SEGMENT_TAG.test(line)) {
      tags.push(line)
      if (line.startsWith('#EXTINF:')) duration = Number(line.slice(8).split(',')[0])
    } else if (line && !line.startsWith('#')) {
      const seq = first + segments.length
      let name
      try { name = new URL(line).pathname.split('/').pop() } catch { return null }
      if (!(duration > 0 && duration <= 15) || !name.endsWith(`-${seq}.ts`)) return null
      segments.push({ seq, name, duration, lines: [...tags, line] })
      tags = []; duration = null
    } else if (line && !segments.length && !tags.length) header.push(line)
    else if (line) return null
  }
  if (!segments.length || tags.length || kept > first || last < segments.at(-1).seq) return null
  return { header, segments, kept, identity: segments[0].name.replace(/-\d+\.ts$/, '') }
}

export function createPlaylistHistory() {
  let entries = new Map(), identity = '', tail = -1, updatedAt = -Infinity, target = 0
  return {
    clear() { entries.clear(); identity = ''; tail = -1; updatedAt = -Infinity; target = 0 },
    extend(text, now = Date.now()) {
      const parsed = parse(text)
      if (!parsed) { this.clear(); return text }
      const end = parsed.segments.at(-1).seq
      if (identity !== parsed.identity || end < tail || now - updatedAt > HISTORY_IDLE_MS) this.clear()
      identity = parsed.identity; tail = end; updatedAt = now
      // Overlapping durations, tags and URLs stay immutable, just like URL pins.
      for (const segment of parsed.segments) if (!entries.has(segment.seq)) entries.set(segment.seq, segment)
      for (const seq of entries.keys()) if (seq < parsed.kept || seq > end) entries.delete(seq)
      const window = []
      let seconds = 0
      for (let seq = end; entries.has(seq) && window.length < HISTORY_MAX_SEGMENTS; seq--) {
        const segment = entries.get(seq)
        window.unshift(segment); seconds += segment.duration
        if (seconds >= HISTORY_SECONDS && window.length >= Math.max(HISTORY_MIN_SEGMENTS, parsed.segments.length)) break
      }
      for (const seq of entries.keys()) if (seq < window[0].seq) entries.delete(seq)
      // Keep a high watermark: older retained fragments may be longer than the
      // upstream's current three, so its smaller target cannot describe our window.
      target = Math.max(target, Number(text.match(/^#EXT-X-TARGETDURATION:\s*(\d+)/m)?.[1]) || 0,
        ...window.map(s => Math.ceil(s.duration)))
      if (window.length === parsed.segments.length && target === Number(text.match(/^#EXT-X-TARGETDURATION:\s*(\d+)/m)?.[1])) return text
      const header = parsed.header.filter(line => !/^#EXT-X-START:/.test(line))
        .map(line => line.startsWith('#EXT-X-MEDIA-SEQUENCE:') ? `#EXT-X-MEDIA-SEQUENCE:${window[0].seq}`
          : line.startsWith('#EXT-X-TARGETDURATION:') ? `#EXT-X-TARGETDURATION:${target}` : line)
      header.push(`#EXT-X-START:TIME-OFFSET=-${Math.min(HISTORY_SECONDS, seconds).toFixed(3)},PRECISE=NO`)
      return [...header, ...window.flatMap(s => s.lines)].join('\n') + '\n'
    },
  }
}
