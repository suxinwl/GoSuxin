package app

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type providerMedia struct {
	URL      string
	Referer  string
	Duration time.Duration
	Playlist string
	HLSKey   []byte
	CENCKey  []byte
	Quality  int
	Label    string
	Variants []providerMedia
}

func (d *Downloader) providerBaseURL(source string) string {
	canon := canonicalProviderSource(source)
	var configured, fallback string
	var huangdouURL, huangguoAIURL, huangguoVideoURL, hongguoURL string
	if d != nil {
		huangdouURL = d.cfg.HuangdouURL
		huangguoAIURL = d.cfg.HuangguoAIURL
		huangguoVideoURL = d.cfg.HuangguoVideoURL
		hongguoURL = d.cfg.HongguoURL
	}
	switch canon {
	case sourceHuangguoAI:
		configured, fallback = huangguoAIURL, huangguoAIBaseURL
	case sourceHuangguoVideo:
		configured, fallback = huangguoVideoURL, huangguoVideoBaseURL
	case sourceHuangdou:
		configured, fallback = huangdouURL, huangdouBaseURL
	case sourceHongguo:
		configured, fallback = hongguoURL, hongguoBaseURL
	case source4KVM:
		if d != nil && d.cfg.SourceURLs != nil {
			configured = d.cfg.SourceURLs[source4KVM]
		}
		fallback = fourKVMBaseURL
	default:
		if isMacCMSSource(canon) {
			if d != nil {
				return strings.TrimRight(d.macCMSBaseURL(canon), "/")
			}
			return strings.TrimRight(defaultMacCMSSources[canon].BaseURL, "/")
		}
		fallback = "https://d2pypzndaqisk.cloudfront.net"
	}
	return strings.TrimRight(firstNonEmpty(configured, fallback), "/")
}

func providerSourceForURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	host := strings.ToLower(parsed.Hostname())
	switch {
	case host == "huangguoai.com" || strings.HasSuffix(host, ".ediayikma.cc") || strings.HasSuffix(host, ".agdkczeyx.cc"):
		return sourceHuangguoAI
	case host == "huangguo.video":
		return sourceHuangguoVideo
	case host == "tideember.cc" || host == "xqjurgek.top":
		return sourceHuangdou
	case host == "hongguoduanju.com" || host == "www.hongguoduanju.com":
		return sourceHongguo
	default:
		return canonicalProviderSource(parsed.Hostname())
	}
}

func (d *Downloader) providerURLCandidates(raw string) []string {
	source := providerSourceForURL(raw)
	if source == "" {
		return []string{raw}
	}
	parsed, _ := url.Parse(raw)
	d.providerMu.Lock()
	preferred := d.providerHosts[source]
	d.providerMu.Unlock()
	var candidates []string
	seen := map[string]bool{}
	add := func(candidate string) {
		if candidate != "" && !seen[candidate] {
			seen[candidate] = true
			candidates = append(candidates, candidate)
		}
	}
	add(rehostProviderURL(parsed, preferred))
	configured := d.providerBaseURL(source)
	add(rehostProviderURL(parsed, configured))
	return candidates
}

func (d *Downloader) resolveProviderMedia(ctx context.Context, task Task) (providerMedia, error) {
	if !isSupportedTask(task) {
		return providerMedia{}, fmt.Errorf("不支持该站源剧集")
	}
	chapter := task.Chapter
	chapter.Source = canonicalProviderSource(chapter.Source)
	if chapter.Source == "" {
		chapter.Source = sourceFromDramaID(task.DramaID)
	}
	if strings.HasPrefix(chapter.VideoURL, "hongguo-cenc://") {
		return d.resolveHongguoMedia(ctx, task)
	}
	if chapter.Source == source4KVM {
		return d.resolve4KVMChapter(ctx, task, chapter)
	}
	if chapter.PageURL == "" && (chapter.Source == sourceHuangguoAI || chapter.Source == sourceHuangguoVideo) {
		if source, sourceID, valid := splitProviderDramaID(task.DramaID); valid {
			_, chapters, err := d.GetHuangguoChapters(ctx, source, sourceID)
			if err != nil {
				return providerMedia{}, fmt.Errorf("刷新旧任务播放地址失败: %w", err)
			}
			matched := false
			for _, fresh := range chapters {
				if fresh.ID == chapter.ID {
					chapter = fresh
					matched = true
					break
				}
			}
			if !matched {
				return providerMedia{}, fmt.Errorf("原章节已变化，请更新该合集后重新下载")
			}
		}
	}
	media := providerMedia{URL: chapter.VideoURL, Referer: firstNonEmpty(chapter.Referer, d.providerBaseURL(chapter.Source)+"/")}
	if chapter.Source == sourceHuangdou {
		_, sourceID, valid := splitProviderDramaID(task.DramaID)
		if valid {
			sequence, err := strconv.Atoi(chapter.EpisodeString(task.Index))
			if err != nil || sequence < 1 {
				return providerMedia{}, fmt.Errorf("黄豆集数无效")
			}
			client := newHuangdouAPIClient(d)
			media, err = d.resolveHuangdouPlayback(ctx, client, sourceID, sequence)
			if err != nil {
				return providerMedia{}, err
			}
			media.Referer = client.host + "/home"
		}
	}
	if chapter.PageURL != "" && (chapter.Source == sourceHuangguoAI || chapter.Source == sourceHuangguoVideo) {
		body, err := d.fetchProviderText(ctx, chapter.PageURL, media.Referer)
		if err != nil {
			return providerMedia{}, err
		}
		if chapter.Source == sourceHuangguoAI {
			media.URL = parseAIVideoURL(body, chapter.PageURL)
		} else {
			media.URL = parseDataHLS(body, chapter.PageURL)
		}
		d.providerMu.Lock()
		if preferred := d.providerHosts[chapter.Source]; preferred != "" {
			media.Referer = preferred + "/"
		}
		d.providerMu.Unlock()
	}

	urlsToTry := append([]string{media.URL}, chapter.BackupURLs...)
	var lastErr error
	for _, tryURL := range urlsToTry {
		if !isProviderHTTPMediaURL(tryURL) {
			continue
		}
		candidateMedia := media
		candidateMedia.URL = tryURL
		parsed, _ := url.Parse(candidateMedia.URL)
		if strings.HasSuffix(strings.ToLower(parsed.Path), ".m3u8") {
			playlist, err := d.fetchProviderText(ctx, candidateMedia.URL, candidateMedia.Referer)
			if err != nil {
				errMsg := err.Error()
				if strings.Contains(errMsg, "403") {
					lastErr = fmt.Errorf("获取播放列表失败: %s；若开启了代理请将视频域名设为直连，或建议在右上角切换其他片源播放", errMsg)
				} else if strings.Contains(errMsg, "404") {
					lastErr = fmt.Errorf("获取播放列表失败: %s；源站切片可能已下架，建议切换其他片源播放", errMsg)
				} else {
					lastErr = fmt.Errorf("获取播放列表失败: %w", err)
				}
				continue
			}
			if !strings.HasPrefix(strings.TrimSpace(strings.TrimPrefix(playlist, "\ufeff")), "#EXTM3U") {
				lastErr = fmt.Errorf("站点未返回有效 M3U8，可能需要登录或链接已失效")
				continue
			}
			if duration := m3u8Duration(playlist); duration > 0 {
				candidateMedia.Duration = duration
			}
			candidateMedia.Playlist = playlist
		}
		return candidateMedia, nil
	}
	if lastErr != nil {
		return providerMedia{}, lastErr
	}
	return providerMedia{}, fmt.Errorf("源站未返回有效播放地址，请更新合集或确认站点访问权限")
}
