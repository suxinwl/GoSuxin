package mediaplaylist

import (
	"math"
	"net/url"
	"strings"
)

// AdFingerprint identifies one complete, decoded and visually reviewed
// commercial payload. Duration locates candidates; only a matching full hash
// and length may authorize removal of the whole three-segment block.
type AdFingerprint struct {
	URL    string
	Bytes  int64
	SHA256 string
}

type AdBlock [3]AdFingerprint

var erciyuanAdHashes = [3]AdFingerprint{
	{Bytes: 1601760, SHA256: "fb2cb775102133e397d10124eac92974e6b1e7530fc1da3a8a0f40991e06ac1b"},
	{Bytes: 1808560, SHA256: "d3af00ea08487c14f47b0363179f6145e9b60d287d98b466420b39db550c7eba"},
	{Bytes: 166380, SHA256: "9ff503296875ed14b47292a126afadb06d9308f4fcb5035f7782b32ebb4019cc"},
}

// ErciyuanAdCandidates performs no I/O and never removes a clip. It returns
// at most two complete unencrypted three-segment blocks on the reviewed CDN.
// Live/master/unsupported playlists, partial blocks and already known exact
// resources incur no verification requests. Normal film discontinuities are
// retained even when their durations resemble the commercial.
func ErciyuanAdCandidates(raw, baseURL string) []AdBlock {
	if len(raw) > 4<<20 {
		return nil
	}
	base, err := url.Parse(baseURL)
	if err != nil || (base.Scheme != "https" && base.Scheme != "http") || base.User != nil ||
		base.Hostname() != "vip17.jimxtc.com" {
		return nil
	}
	p, ok := parse(raw, base, []string{"ffzyad.com"})
	if !ok {
		return nil
	}
	var out []AdBlock
	for index := 0; index+2 < len(p.segments) && len(out) < 2; index++ {
		clips := p.segments[index : index+3]
		if !clips[0].discontinuity || clips[1].discontinuity || clips[2].discontinuity ||
			(index+3 < len(p.segments) && !p.segments[index+3].discontinuity) {
			continue
		}
		var block AdBlock
		valid := true
		var directory string
		for i, clip := range clips {
			if clip.remove || clip.key.line != "" && clip.key.line != "#EXT-X-KEY:METHOD=NONE" ||
				clip.init.line != "" || clip.byterange != nil ||
				math.Abs(clip.duration-[]float64{4.6, 5.533, 0.233}[i]) > 0.000001 {
				valid = false
				break
			}
			remote, err := url.Parse(clip.resolved)
			if err != nil || remote.Hostname() != "vip17.jimxtc.com" || remote.User != nil ||
				!strings.HasSuffix(remote.Path, ".ts") || remote.Fragment != "" {
				valid = false
				break
			}
			dir := remote.Path[:strings.LastIndex(remote.Path, "/")+1]
			if i > 0 && dir != directory {
				valid = false
				break
			}
			directory = dir
			block[i] = erciyuanAdHashes[i]
			block[i].URL = clip.resolved
		}
		if valid {
			out = append(out, block)
			index += 2
		}
	}
	return out
}
