// Package mediaplaylist conservatively filters advertising from complete VOD
// playlists. It never fetches URLs or alters stored source records.
package mediaplaylist

import (
	"errors"
	"fmt"
	"math"
	"net"
	"net/url"
	"strconv"
	"strings"
)

type Result struct {
	Playlist       string
	Removed        int
	RemovedSeconds float64
}

// NormalizeDomains accepts up to 64 exact ASCII domains, including subdomains.
// It rejects URLs, IPs, wildcard patterns and empty match expressions.
func NormalizeDomains(raw string) (string, error) {
	var out []string
	seen := map[string]bool{}
	for _, entry := range strings.Fields(raw) {
		domain := strings.ToLower(entry)
		if len(domain) > 253 || net.ParseIP(domain) != nil || !validDomain(domain) {
			return "", errors.New("广告域名仅支持完整 ASCII 域名，不支持网址、IP 或通配符")
		}
		if !seen[domain] {
			seen[domain] = true
			out = append(out, domain)
		}
		if len(out) > 64 {
			return "", errors.New("广告域名最多设置 64 个")
		}
	}
	return strings.Join(out, "\n"), nil
}

func validDomain(domain string) bool {
	labels := strings.Split(domain, ".")
	if len(labels) < 2 {
		return false
	}
	for _, label := range labels {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range []byte(label) {
			if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-') {
				return false
			}
		}
	}
	// Numeric final labels also exclude unusual dotted IP representations.
	for _, c := range labels[len(labels)-1] {
		if c >= 'a' && c <= 'z' {
			return true
		}
	}
	return false
}

type keyState struct {
	line     string
	implicit bool
}
type mapState struct {
	line string
	key  keyState // An encrypted init section uses the key at MAP declaration.
}
type byteRange struct{ length, offset uint64 }
type segment struct {
	uri, resolved, extinf string
	duration              float64
	sequence              uint64
	key                   keyState
	init                  mapState
	byterange             *byteRange
	extras                []string
	discontinuity, remove bool
}
type playlist struct {
	head, tail []string
	segments   []segment
}

// Filter changes only supported ENDLIST playlists (VOD or completed EVENT).
// Unsupported or ambiguous
// input, live/master/LL-HLS, and playlists with no deletion remain byte-identical.
// A discontinuity or a segment's duration alone never identifies advertising.
func Filter(raw, baseURL string, domains []string) Result {
	return filter(raw, baseURL, domains, nil)
}

// FilterVerified adds only exact resolved resources whose complete payloads
// were independently verified by the caller. It uses the same conservative
// parser and timeline/key reconstruction as ordinary advertising rules.
func FilterVerified(raw, baseURL string, domains []string, verified map[string]bool) Result {
	return filter(raw, baseURL, domains, verified)
}

func filter(raw, baseURL string, domains []string, verified map[string]bool) Result {
	unchanged := Result{Playlist: raw}
	if len(raw) > 16*1024*1024 {
		return unchanged
	}
	normalized, err := NormalizeDomains(strings.Join(domains, "\n"))
	if err != nil {
		return unchanged
	}
	adDomains := append([]string{"ffzyad.com"}, strings.Fields(normalized)...)
	var base *url.URL
	if baseURL != "" {
		base, err = url.Parse(baseURL)
		if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Hostname() == "" || base.User != nil {
			return unchanged
		}
	}
	p, ok := parse(raw, base, adDomains)
	if !ok {
		return unchanged
	}
	for index := range p.segments {
		if verified[p.segments[index].resolved] {
			p.segments[index].remove = true
		}
	}
	result := Result{}
	for _, seg := range p.segments {
		if seg.remove {
			result.Removed++
			result.RemovedSeconds += seg.duration
		}
	}
	if result.Removed == 0 || result.Removed == len(p.segments) {
		return unchanged
	}
	lines := compatibleHeader(p.head, p.segments)
	currentKey := ""
	currentMap := mapState{}
	emitKey := func(key string) {
		if key == "" {
			if currentKey != "" && currentKey != "#EXT-X-KEY:METHOD=NONE" {
				lines = append(lines, "#EXT-X-KEY:METHOD=NONE")
			}
			currentKey = ""
		} else if key != currentKey {
			lines = append(lines, key)
			currentKey = key
		}
	}
	kept, gap := 0, false
	for _, seg := range p.segments {
		if seg.remove {
			gap = true
			continue
		}
		if seg.discontinuity || (gap && kept > 0) {
			lines = append(lines, "#EXT-X-DISCONTINUITY")
		}
		lines = append(lines, seg.extras...)
		if seg.init.line != "" && seg.init != currentMap {
			emitKey(seg.init.key.line)
			lines = append(lines, seg.init.line)
			currentMap = seg.init
		}
		key := seg.key.line
		if seg.key.implicit {
			// Preserve ORIGINAL sequence-derived IVs after deleting segments.
			key += fmt.Sprintf(",IV=0x%032x", seg.sequence)
		}
		emitKey(key)
		lines = append(lines, seg.extinf)
		if seg.byterange != nil {
			lines = append(lines, fmt.Sprintf("#EXT-X-BYTERANGE:%d@%d", seg.byterange.length, seg.byterange.offset))
		}
		lines = append(lines, seg.uri)
		kept++
		gap = false
	}
	lines = append(lines, "#EXT-X-ENDLIST")
	lines = append(lines, p.tail...)
	newline := "\n"
	if strings.Contains(raw, "\r\n") {
		newline = "\r\n"
	}
	result.Playlist = strings.Join(lines, newline)
	if strings.HasSuffix(raw, "\n") {
		result.Playlist += newline
	}
	return result
}

func parse(raw string, base *url.URL, domains []string) (playlist, bool) {
	var p playlist
	lines := strings.Split(strings.TrimSuffix(raw, "\n"), "\n")
	if len(lines) == 0 || strings.TrimSuffix(lines[0], "\r") != "#EXTM3U" {
		return p, false
	}
	p.head = []string{"#EXTM3U"}
	var key keyState
	var init mapState
	var extras []string
	var extinf, rangeRaw string
	var duration, cueDuration float64
	var sequence uint64
	var discontinuity, ended bool
	cueStart := -1
	seenHeader := map[string]bool{}
	for _, original := range lines[1:] {
		line := strings.TrimSuffix(original, "\r")
		if line != strings.TrimSpace(line) {
			return p, false
		}
		if ended {
			if line != "" && (!strings.HasPrefix(line, "#") || strings.HasPrefix(line, "#EXT")) {
				return p, false
			}
			p.tail = append(p.tail, line)
			continue
		}
		if line == "" || (strings.HasPrefix(line, "#") && !strings.HasPrefix(line, "#EXT")) {
			extras = append(extras, line)
			continue
		}
		if strings.HasPrefix(line, "#EXT") {
			tag, value, hasValue := strings.Cut(line, ":")
			switch tag {
			case "#EXT-X-VERSION", "#EXT-X-TARGETDURATION", "#EXT-X-MEDIA-SEQUENCE", "#EXT-X-PLAYLIST-TYPE", "#EXT-X-INDEPENDENT-SEGMENTS":
				if len(p.segments) != 0 || extinf != "" || seenHeader[tag] {
					return p, false
				}
				seenHeader[tag] = true
				switch tag {
				case "#EXT-X-PLAYLIST-TYPE":
					// ENDLIST below is mandatory, so a finished EVENT is equally
					// immutable. Ongoing EVENT/live playlists remain untouched.
					if value != "VOD" && value != "EVENT" {
						return p, false
					}
				case "#EXT-X-INDEPENDENT-SEGMENTS":
					if hasValue {
						return p, false
					}
				default:
					n, err := strconv.ParseUint(value, 10, 64)
					if err != nil || (tag != "#EXT-X-MEDIA-SEQUENCE" && n == 0) {
						return p, false
					}
					if tag == "#EXT-X-MEDIA-SEQUENCE" {
						sequence = n
					}
				}
				p.head = append(p.head, line)
			case "#EXTINF":
				if extinf != "" {
					return p, false
				}
				d, _, comma := strings.Cut(value, ",")
				var err error
				duration, err = strconv.ParseFloat(d, 64)
				if err != nil || !comma || !finitePositive(duration) {
					return p, false
				}
				extinf = line
			case "#EXT-X-BYTERANGE":
				if rangeRaw != "" || !hasValue {
					return p, false
				}
				rangeRaw = value
			case "#EXT-X-KEY":
				var ok bool
				key, ok = parseKey(line, value)
				if !ok {
					return p, false
				}
			case "#EXT-X-MAP":
				attrs, ok := attributes(value)
				if !ok || attrs["URI"] == "" || key.implicit || !onlyKeys(attrs, "URI", "BYTERANGE") {
					return p, false
				}
				if _, ok := mediaURL(attrs["URI"], base); !ok {
					return p, false
				}
				if br, has := attrs["BYTERANGE"]; has {
					if _, explicit, ok := parseRange(br); !ok || !explicit {
						return p, false
					}
				}
				init = mapState{line: line, key: key}
			case "#EXT-X-DISCONTINUITY":
				if hasValue || discontinuity || extinf != "" {
					return p, false
				}
				discontinuity = true
			case "#EXT-X-PROGRAM-DATE-TIME", "#EXT-X-GAP", "#EXT-X-BITRATE":
				extras = append(extras, line)
			case "#EXT-X-CUE-OUT":
				if cueStart != -1 || extinf != "" || rangeRaw != "" {
					return p, false
				}
				var ok bool
				cueDuration, ok = parseCueDuration(value, hasValue)
				if !ok {
					return p, false
				}
				cueStart = len(p.segments)
			case "#EXT-X-CUE-IN":
				if hasValue || cueStart < 0 || cueStart == len(p.segments) || extinf != "" || rangeRaw != "" {
					return p, false
				}
				var actual float64
				for i := cueStart; i < len(p.segments); i++ {
					actual += p.segments[i].duration
				}
				if cueDuration > 0 && math.Abs(actual-cueDuration) > 0.25 {
					return p, false
				}
				for i := cueStart; i < len(p.segments); i++ {
					p.segments[i].remove = true
				}
				cueStart = -1
			case "#EXT-X-ENDLIST":
				if hasValue || extinf != "" || rangeRaw != "" || cueStart >= 0 || discontinuity {
					return p, false
				}
				p.tail = append(p.tail, extras...)
				extras = nil
				ended = true
			default:
				// Includes master/LL-HLS, DATERANGE and unknown timeline semantics.
				return p, false
			}
			continue
		}
		if extinf == "" || len(p.segments) >= 100000 || uint64(len(p.segments)) > math.MaxUint64-sequence {
			return p, false
		}
		remote, ok := mediaURL(line, base)
		if !ok {
			return p, false
		}
		seg := segment{uri: line, resolved: remote.String(), extinf: extinf, duration: duration,
			sequence: sequence + uint64(len(p.segments)), key: key, init: init, extras: extras,
			discontinuity: discontinuity, remove: adURL(remote, domains)}
		if rangeRaw != "" {
			br, explicit, ok := parseRange(rangeRaw)
			if !ok {
				return p, false
			}
			if !explicit {
				if len(p.segments) == 0 {
					return p, false
				}
				prev := p.segments[len(p.segments)-1]
				if prev.byterange == nil || prev.resolved != seg.resolved {
					return p, false
				}
				br.offset = prev.byterange.offset + prev.byterange.length
			}
			if br.length > math.MaxUint64-br.offset {
				return p, false
			}
			seg.byterange = &br
		}
		p.segments = append(p.segments, seg)
		extinf, rangeRaw, extras, discontinuity = "", "", nil, false
	}
	return p, ended && len(p.segments) > 0 && cueStart < 0 && seenHeader["#EXT-X-TARGETDURATION"]
}

func finitePositive(n float64) bool { return n > 0 && !math.IsNaN(n) && !math.IsInf(n, 0) }

// Added IVs require version 2; preserve any higher version advertised upstream.
// Also respect existing floating EXTINF, ranges and initialization sections.
func compatibleHeader(head []string, segments []segment) []string {
	minimum := 1
	for _, seg := range segments {
		if seg.remove {
			continue
		}
		if seg.key.line != "" && seg.key.line != "#EXT-X-KEY:METHOD=NONE" {
			minimum = max(minimum, 2)
		}
		if strings.Contains(strings.SplitN(seg.extinf, ",", 2)[0], ".") {
			minimum = max(minimum, 3)
		}
		if seg.byterange != nil {
			minimum = max(minimum, 4)
		}
		if strings.Contains(seg.key.line, "KEYFORMAT") {
			minimum = max(minimum, 5)
		}
		if seg.init.line != "" {
			minimum = max(minimum, 6)
		}
	}
	lines := append([]string(nil), head...)
	for i, line := range lines {
		if strings.HasPrefix(line, "#EXT-X-VERSION:") {
			n, _ := strconv.ParseUint(strings.TrimPrefix(line, "#EXT-X-VERSION:"), 10, 64)
			if n < uint64(minimum) {
				lines[i] = fmt.Sprintf("#EXT-X-VERSION:%d", minimum)
			}
			return lines
		}
	}
	if minimum > 1 {
		lines = append(lines, fmt.Sprintf("#EXT-X-VERSION:%d", minimum))
	}
	return lines
}

func parseCueDuration(raw string, present bool) (float64, bool) {
	if !present {
		return 0, true
	}
	raw = strings.TrimPrefix(raw, "DURATION=")
	n, err := strconv.ParseFloat(raw, 64)
	return n, err == nil && finitePositive(n)
}

func parseRange(raw string) (byteRange, bool, bool) {
	n, offset, explicit := strings.Cut(raw, "@")
	length, err := strconv.ParseUint(n, 10, 64)
	if err != nil || length == 0 {
		return byteRange{}, explicit, false
	}
	var at uint64
	if explicit {
		at, err = strconv.ParseUint(offset, 10, 64)
	}
	return byteRange{length: length, offset: at}, explicit, err == nil && length <= math.MaxUint64-at
}

func parseKey(line, raw string) (keyState, bool) {
	a, ok := attributes(raw)
	if !ok || !onlyKeys(a, "METHOD", "URI", "IV", "KEYFORMAT", "KEYFORMATVERSIONS") {
		return keyState{}, false
	}
	if a["METHOD"] == "NONE" {
		return keyState{line: line}, len(a) == 1
	}
	if a["METHOD"] != "AES-128" || a["URI"] == "" || (a["KEYFORMAT"] != "" && a["KEYFORMAT"] != "identity") || (a["KEYFORMATVERSIONS"] != "" && a["KEYFORMATVERSIONS"] != "1") {
		return keyState{}, false
	}
	if _, ok := mediaURL(a["URI"], nil); !ok {
		return keyState{}, false
	}
	iv, has := a["IV"]
	if has {
		if len(iv) < 3 || len(iv) > 34 || !strings.EqualFold(iv[:2], "0x") {
			return keyState{}, false
		}
		for _, c := range iv[2:] {
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
				return keyState{}, false
			}
		}
	}
	return keyState{line: line, implicit: !has}, true
}

func attributes(raw string) (map[string]string, bool) {
	attrs := map[string]string{}
	for raw != "" {
		key, remainder, equal := strings.Cut(raw, "=")
		if !equal || key == "" || strings.TrimSpace(key) != key {
			return nil, false
		}
		if _, duplicate := attrs[key]; duplicate {
			return nil, false
		}
		var value, rest string
		if strings.HasPrefix(remainder, "\"") {
			var closed bool
			value, rest, closed = strings.Cut(remainder[1:], "\"")
			if !closed || (rest != "" && !strings.HasPrefix(rest, ",")) {
				return nil, false
			}
			rest = strings.TrimPrefix(rest, ",")
		} else {
			value, rest, _ = strings.Cut(remainder, ",")
		}
		if value == "" || strings.TrimSpace(value) != value || strings.HasSuffix(remainder, ",") {
			return nil, false
		}
		attrs[key] = value
		raw = rest
	}
	return attrs, len(attrs) > 0
}

func onlyKeys(attrs map[string]string, allowed ...string) bool {
	for key := range attrs {
		found := false
		for _, allowedKey := range allowed {
			found = found || key == allowedKey
		}
		if !found {
			return false
		}
	}
	return true
}

func mediaURL(raw string, base *url.URL) (*url.URL, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Fragment != "" || (u.Scheme != "" && u.Scheme != "http" && u.Scheme != "https") {
		return nil, false
	}
	if base != nil {
		u = base.ResolveReference(u)
	}
	if u.Scheme != "" && u.Hostname() == "" {
		return nil, false
	}
	return u, raw != ""
}

func adURL(u *url.URL, domains []string) bool {
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	// These exact three files were visually verified as one 13.2-second inserted
	// commercial in multiple episodes on 2026-10-02. Its position varies. Neither
	// the whole provider nor neighboring files/directories are advertisement rules.
	if host == "play.maoyanplay.top" {
		switch u.Path {
		case "/20260818/JMQDkGOL/hls/9OG1QJKR.ts", "/20260818/JMQDkGOL/hls/Dgaf3EqV.ts", "/20260818/JMQDkGOL/hls/Nr4MRgx5.ts":
			return true
		}
	}
	// These exact three files were decoded and visually checked on 2026-10-03
	// as the complete 818818.com commercial in 二次元's theatrical 64933.
	// Both neighboring files are the film itself. Normal film shots on this CDN
	// also have discontinuities, so neither its host nor that tag is an ad rule.
	if host == "vip17.jimxtc.com" {
		switch u.Path {
		case "/2026-10-01/41491_YTojUj5qXO8reGCMct/3000k/hls/39eef675e8ec73b3b567661ff036fbc2.ts",
			"/2026-10-01/41491_YTojUj5qXO8reGCMct/3000k/hls/fe29245fba20e2312e18b426fe063138.ts",
			"/2026-10-01/41491_YTojUj5qXO8reGCMct/3000k/hls/4181392cff3e2cbdee21449b5d972063.ts",
			// TV 35604 episode 160 reuses the exact same three payloads;
			// each complete SHA256/length matched the reviewed commercial.
			"/2026-09-27/40945_DK_c95_43k7DfvzlrT/3000k/hls/8954ad7b8ad06fa9a423119287456cfd.ts",
			"/2026-09-27/40945_DK_c95_43k7DfvzlrT/3000k/hls/a3e12149d4deb23609c0d49c1d1b8ef9.ts",
			"/2026-09-27/40945_DK_c95_43k7DfvzlrT/3000k/hls/b1661f6de2c31e2f56185960bf44c819.ts":
			return true
		}
	}
	for _, domain := range domains {
		if host == domain || strings.HasSuffix(host, "."+domain) {
			return true
		}
	}
	parts := strings.Split(strings.ToLower(u.Path), "/")
	for i, part := range parts {
		if part == "adjump" || (part == "video" && i+2 < len(parts) && parts[i+1] == "ad") {
			return true
		}
	}
	return false
}
