package app

import (
	"context"
	"io"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"
)

const cueAdPlaylist = "#EXTM3U\n#EXT-X-TARGETDURATION:8\n#EXT-X-MEDIA-SEQUENCE:0\n#EXTINF:6,\nmain0.ts\n#EXT-X-CUE-OUT:6.666667\n#EXTINF:6.666667,\ncommercial.ts\n#EXT-X-CUE-IN\n#EXTINF:6,\nmain1.ts\n#EXT-X-ENDLIST\n"

func TestPlaylistFilterKeepsOrdinaryDurationsAndOpaqueNames(t *testing.T) {
	raw := "#EXTM3U\n#EXT-X-TARGETDURATION:17\n#EXT-X-MEDIA-SEQUENCE:0\n#EXTINF:6.433333,\npart1only.ts\n#EXTINF:6.666667,\n921c07e8bfad6789-main.ts\n#EXT-X-DISCONTINUITY\n#EXTINF:8,\nplain.ts?token=adjump\n#EXTINF:16.666667,6.666667\nlast.ts\n#EXT-X-ENDLIST\n"
	if got := cleanM3U8Playlist(raw); got != raw {
		t.Fatal("normal short segments, opaque names and a timestamp boundary must remain unchanged")
	}
	if got := cleanM3U8Playlist(raw, "", " ", "1o", "6.666667", "/video/ad/"); got != raw {
		t.Fatal("blank or invalid legacy keywords must not select normal segments")
	}
	want := 6_433_333*time.Microsecond + 6_666_667*time.Microsecond + 8*time.Second + 16_666_667*time.Microsecond
	if got := m3u8Duration(raw); got != want {
		t.Fatalf("duration must reflect the supplied playlist: got %v, want %v", got, want)
	}
}

func TestPlaylistDurationDoesNotApplyAdPolicy(t *testing.T) {
	want := 18*time.Second + 666_667*time.Microsecond
	if got := m3u8Duration(cueAdPlaylist); got != want {
		t.Fatalf("original duration must include the ad when filtering is disabled: got %v, want %v", got, want)
	}
	filtered := cleanM3U8Playlist(cueAdPlaylist)
	if strings.Contains(filtered, "commercial.ts") || !strings.Contains(filtered, "main0.ts") || !strings.Contains(filtered, "main1.ts") {
		t.Fatal("explicit cue filtering must remove only its marked media segment")
	}
	if got := m3u8Duration(filtered); got != 12*time.Second {
		t.Fatalf("filtered duration must use exactly the retained segments: %v", got)
	}
}

func TestPlaylistFilterKeepsImplicitIVEncryption(t *testing.T) {
	raw := strings.Replace(cueAdPlaylist, "#EXT-X-MEDIA-SEQUENCE:0\n", "#EXT-X-MEDIA-SEQUENCE:20\n#EXT-X-KEY:METHOD=AES-128,URI=\"enc.key\"\n", 1)
	filtered := cleanM3U8Playlist(raw)
	if filtered != raw {
		if strings.Contains(filtered, "commercial.ts") || !strings.Contains(filtered, "main0.ts") || !strings.Contains(filtered, "main1.ts") {
			t.Fatal("encrypted filtering must remove only the explicitly marked ad")
		}
		ivPattern := regexp.MustCompile(`IV=0[xX]([0-9a-fA-F]{32})`)
		currentIV := ""
		retained := 0
		for _, line := range strings.Split(filtered, "\n") {
			if strings.HasPrefix(line, "#EXT-X-KEY:") {
				if !strings.Contains(line, `URI="enc.key"`) {
					t.Fatal("filter must retain the original key resource")
				}
				match := ivPattern.FindStringSubmatch(line)
				if len(match) != 2 {
					t.Fatal("filtered encrypted media must make its original implicit IV explicit")
				}
				currentIV = strings.ToLower(match[1])
			}
			want := ""
			switch line {
			case "main0.ts":
				want = "00000000000000000000000000000014"
			case "main1.ts":
				want = "00000000000000000000000000000016"
			}
			if want != "" {
				retained++
				if currentIV != want {
					t.Fatalf("retained encrypted media lost its original sequence IV: got %s, want %s", currentIV, want)
				}
			}
		}
		if retained != 2 {
			t.Fatal("encrypted filter omitted a retained segment")
		}
	}
	if got := rewriteM3U8(raw, "https://media.example/media/index.m3u8"); !strings.Contains(got, "#EXT-X-KEY:METHOD=AES-128,URI=\"enc.key\"") || !strings.Contains(got, "https://media.example/media/commercial.ts") {
		t.Fatal("URI rewriting must not independently remove encrypted media or its key")
	}
}

func TestHLSProxyFiltersEmbeddedAndFetchedPlaylistsOnlyWhenEnabled(t *testing.T) {
	for _, embedded := range []bool{false, true} {
		for _, disabled := range []bool{false, true} {
			name := "fetched/enabled"
			if embedded {
				name = "embedded/enabled"
			}
			if disabled {
				name = strings.TrimSuffix(name, "enabled") + "disabled"
			}
			t.Run(name, func(t *testing.T) {
				upstreamCalls := 0
				d := &Downloader{cfg: Config{DisableAdFilter: disabled}, client: &http.Client{Transport: cmsRoundTripFunc(func(request *http.Request) (*http.Response, error) {
					upstreamCalls++
					return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/vnd.apple.mpegurl"}}, Body: io.NopCloser(strings.NewReader(cueAdPlaylist)), Request: request}, nil
				})}}
				media := providerMedia{URL: "https://media.example/movie/index.m3u8"}
				if embedded {
					media.Playlist = cueAdPlaylist
				}
				proxy, err := d.newHLSProxy(context.Background(), media, nil)
				if err != nil {
					t.Fatal(err)
				}
				defer proxy.Close()
				if embedded {
					proxy.mu.Lock()
					for _, asset := range proxy.assets {
						if asset.remote == media.URL && string(asset.body) != cueAdPlaylist {
							t.Error("embedded body must remain original until its one ServeHTTP filter pass")
						}
					}
					proxy.mu.Unlock()
				}
				response, err := http.Get(proxy.root)
				if err != nil {
					t.Fatal(err)
				}
				defer response.Body.Close()
				body, err := io.ReadAll(response.Body)
				if err != nil || response.StatusCode != http.StatusOK {
					t.Fatalf("proxy playlist request: status=%d, err=%v", response.StatusCode, err)
				}
				wantDuration := 12 * time.Second
				if disabled {
					wantDuration = m3u8Duration(cueAdPlaylist)
				}
				if got := m3u8Duration(string(body)); got != wantDuration {
					t.Fatalf("proxy policy must control the actual served media duration: got %v, want %v", got, wantDuration)
				}
				proxy.mu.Lock()
				foundAd := false
				for _, asset := range proxy.assets {
					if strings.HasSuffix(asset.remote, "/commercial.ts") {
						foundAd = true
					}
				}
				proxy.mu.Unlock()
				if foundAd != disabled {
					t.Fatal("only disabled filtering may register the explicitly marked ad for forwarding")
				}
				wantCalls := 1
				if embedded {
					wantCalls = 0
				}
				if upstreamCalls != wantCalls {
					t.Fatalf("embedded content must not be fetched twice: got %d, want %d", upstreamCalls, wantCalls)
				}
			})
		}
	}
}
