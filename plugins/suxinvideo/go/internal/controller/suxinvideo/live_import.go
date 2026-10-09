package suxinvideo

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

const liveImportLimit = 16 << 20

type liveImportItem struct {
	TVGID   string            `json:"tvg_id"`
	Name    string            `json:"name"`
	Logo    string            `json:"logo"`
	Group   string            `json:"group"`
	GroupID int64             `json:"-"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
	Quality string            `json:"quality"`
}
type liveImportPreview struct {
	Channels int              `json:"channels"`
	Streams  int              `json:"streams"`
	Groups   []string         `json:"groups"`
	Sample   []liveImportItem `json:"sample"`
	Warnings []string         `json:"warnings"`
}

var liveImportAttributes = regexp.MustCompile(`([\w-]+)\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s,]+))`)
var liveResolution = regexp.MustCompile(`(?i)\b(2160|1080|720|576|480|360)[pi]?\b|\b(4K|UHD|FHD|HD|SD)\b`)
var liveNameAliases = map[string]string{
	"jade.hk": "翡翠台", "goldenjade.hk": "黄金翡翠台", "pearl.hk": "明珠台", "j2.hk": "TVB Plus（82台）", "tvbplus.hk": "TVB Plus（82台）",
	"tvbnews.hk": "TVB 新闻台", "tvbfinance.hk": "TVB 财经体育资讯台", "viutv.hk": "ViuTV", "viutvsix.hk": "ViuTVsix",
	"rthktv31.hk": "港台电视31", "rthktv32.hk": "港台电视32", "rthktv33.hk": "港台电视33", "rthktv34.hk": "港台电视34",
	"phoenixchinese.hk": "凤凰中文台", "phoenixinfo.hk": "凤凰资讯台", "tdmoumun.mo": "澳门澳视台", "tdmsports.mo": "澳门体育台",
	"phoenixchinesechannel.hk": "凤凰中文台", "phoenixinfonewschannel.hk": "凤凰资讯台",
	"phoenixhongkongchannel.hk": "凤凰香港台",
	"beijingtv.cn":              "北京卫视", "dragontv.cn": "东方卫视", "zhejiangtv.cn": "浙江卫视", "jiangsu.tv.cn": "江苏卫视",
	"jiangsu.tv": "江苏卫视", "jiangsutv.cn": "江苏卫视", "hunantv.cn": "湖南卫视", "hubeitv.cn": "湖北卫视",
	"anhuistv.cn": "安徽卫视", "anhuitv.cn": "安徽卫视", "guangdongtv.cn": "广东卫视", "shandongtv.cn": "山东卫视",
	"shanghaidragontelevision.cn": "东方卫视", "sztv.cn": "深圳卫视", "shenzhenweishi.cn": "深圳卫视",
	"bstv.hk": "BSTV 香港频道", "hoytv.hk": "HOY 电视台", "petclubtv.hk": "宠物频道", "tvnasia.hk": "tvN 亚洲频道",
	"canalmacau.mo": "澳门葡语频道", "lotustv.mo": "澳门莲花卫视", "axnasia.sg": "AXN 亚洲频道",
	"beautifullifetv.tw": "人间卫视", "daai2.tw": "大爱二台", "dalitv.tw": "大立电视",
	"ebcfinancialnews.tw": "东森财经新闻台", "ebcnews.tw": "东森新闻台", "goodtv.tw": "好消息电视台",
	"ntdtvasiapacific.us": "新唐人亚太台", "setnews.tw": "三立新闻台", "suprememastertv.tw": "无上师电视台",
	"taiwanindigenoustv.tw": "原住民族电视台", "tvbsnews.tw": "TVBS 新闻台", "tvbsasia.tw": "TVBS 亚洲台", "wxtv.tw": "唯心电视",
	"abnchina.us": "ABN 中文频道", "andotv.cn": "安多卫视", "angeltv.in": "天使电视中文台",
	"cctvplus1.cn": "CCTV+ 国际传播1", "cctvplus2.cn": "CCTV+ 国际传播2", "dunhuangtv.cn": "敦煌电视台",
	"goldeneaglecartoon.cn": "金鹰卡通", "guangdongsatellitetv.cn": "广东卫视", "guangzhoutv.cn": "广州电视台",
	"guizhoutv.cn": "贵州卫视", "harbinmoviechannel.cn": "哈尔滨影视频道", "hebinewschannel.cn": "鹤壁新闻频道",
	"hezhengtv.cn": "和政电视台", "highchanneltv.cn": "高点电视", "homeplus.ir": "HomePlus 家居频道",
	"jiangxitv.cn": "江西卫视", "jiuquantvnewscomprehensivechannel.cn": "酒泉新闻综合频道", "liaoningtv.cn": "辽宁卫视",
	"neimonggoltv2mongolianculturechannel.cn": "内蒙古蒙语文化频道", "pingxiangtvnewschannel.cn": "萍乡新闻频道",
	"qinghaitv.cn": "青海卫视", "shandongtvagriculturalsciencechannel.cn": "山东农科频道",
	"shandongtvchildrenschannel.cn": "山东少儿频道", "shandongtvlifechannel.cn": "山东生活频道",
	"shandongtvqiluchannel.cn": "山东齐鲁频道", "shandongtvsportschannel.cn": "山东体育频道", "shandongtvvarietychannel.cn": "山东综艺频道",
	"sichuansatellitetv.cn": "四川卫视", "sichuantvwomenandchildrenchannel.cn": "四川妇女儿童频道", "tianjintv.cn": "天津卫视",
	"tvbricschinese.cn": "金砖国家电视中文频道", "xianbusinessinformationchannel.cn": "西安商务资讯频道", "xiansilkroadchannel.cn": "西安丝路频道",
	"xinjiangtv1.cn": "新疆卫视", "xinjiangtv2.cn": "新疆维语新闻综合频道", "xinjiangtv3.cn": "新疆哈语新闻综合频道", "xinjiangtv8.cn": "新疆哈语综艺频道",
	"xizangtvtibetan.cn": "西藏藏语频道", "xuzhoueconomiclifechannel.cn": "徐州经济生活频道",
	"yanbiansatellitetv.cn": "延边卫视", "youmancartoonchannel.cn": "优漫卡通", "zhejianginternationalchannel.cn": "浙江国际频道",
	"cctv4asia.cn": "CCTV-4 中文国际", "cctv4k.cn": "CCTV-4K 超高清", "cctv8k.cn": "CCTV-8K 超高清",
}

func liveDisplayName(tvgID, name string) string {
	id := strings.ToLower(liveNormalizeTVGID(tvgID))
	if alias := liveNameAliases[id]; alias != "" {
		return alias
	}
	fullID := id
	for _, suffix := range []string{".cn", ".hk", ".mo", ".tw"} {
		id = strings.TrimSuffix(id, suffix)
	}
	if strings.HasPrefix(id, "cctv") {
		names := map[string]string{"cctv1": "CCTV-1 综合", "cctv2": "CCTV-2 财经", "cctv3": "CCTV-3 综艺", "cctv4": "CCTV-4 中文国际", "cctv5": "CCTV-5 体育", "cctv5plus": "CCTV-5+ 体育赛事", "cctv6": "CCTV-6 电影", "cctv7": "CCTV-7 国防军事", "cctv8": "CCTV-8 电视剧", "cctv9": "CCTV-9 纪录", "cctv10": "CCTV-10 科教", "cctv11": "CCTV-11 戏曲", "cctv12": "CCTV-12 社会与法", "cctv13": "CCTV-13 新闻", "cctv14": "CCTV-14 少儿", "cctv15": "CCTV-15 音乐", "cctv16": "CCTV-16 奥林匹克", "cctv17": "CCTV-17 农业农村"}
		if alias := names[id]; alias != "" {
			return alias
		}
	}
	if alias := liveOfficialChineseNames[fullID]; alias != "" {
		return alias
	}
	return strings.TrimSpace(name)
}
func liveChannelIdentity(item liveImportItem) string {
	if id := liveNormalizeTVGID(item.TVGID); id != "" {
		return liveIdentity("tvg:" + strings.ToLower(id))
	}
	return liveIdentity("name:" + strings.ToLower(strings.Join(strings.Fields(item.Name), "")))
}

// IPTV-org's raw stream lists attach edition hints such as @HD to the same
// EPG channel identifier. Editions are sources, not separate TV channels.
func liveNormalizeTVGID(raw string) string {
	id, _, _ := strings.Cut(strings.TrimSpace(raw), "@")
	return id
}

func liveOfficialSubscriptionFallback(raw string) string {
	for _, country := range []string{"cn", "hk", "mo", "tw"} {
		if raw == "https://iptv-org.github.io/iptv/countries/"+country+".m3u" {
			return "https://raw.githubusercontent.com/iptv-org/iptv/master/streams/" + country + ".m3u"
		}
	}
	return ""
}
func liveCheckImportURL(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Hostname() == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return errors.New("仅支持 HTTP/HTTPS 直播地址")
	}
	if len(raw) > 8192 {
		return errors.New("直播地址过长")
	}
	return nil
}
func liveAllowedHeader(name string) bool {
	switch strings.ToLower(name) {
	case "user-agent", "referer", "origin", "cookie", "authorization", "accept", "accept-language", "x-token", "x-api-key":
		return true
	}
	return false
}
func liveCleanHeaders(input map[string]string) (map[string]string, error) {
	result := map[string]string{}
	for k, v := range input {
		if !liveAllowedHeader(k) || len(v) > 4096 || strings.ContainsAny(k+v, "\r\n") {
			return nil, errors.New("直播请求头无效")
		}
		result[http.CanonicalHeaderKey(k)] = v
	}
	return result, nil
}
func liveParseImport(content, defaultGroup string) ([]liveImportItem, liveImportPreview, error) {
	preview := liveImportPreview{Groups: []string{}, Sample: []liveImportItem{}, Warnings: []string{}}
	if len(content) > liveImportLimit {
		return nil, preview, errors.New("直播列表最多 16 MB")
	}
	if defaultGroup == "" {
		defaultGroup = "其他频道"
	}
	group := defaultGroup
	var pending *liveImportItem
	var result []liveImportItem
	seen := map[string]bool{}
	channels := map[string]bool{}
	groups := map[string]bool{}
	scanner := bufio.NewScanner(strings.NewReader(strings.TrimPrefix(content, "\ufeff")))
	scanner.Buffer(make([]byte, 4096), 128<<10)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#EXTINF:") {
			attrs := map[string]string{}
			for _, m := range liveImportAttributes.FindAllStringSubmatch(line, -1) {
				v := m[2]
				if v == "" {
					v = m[3]
				}
				if v == "" {
					v = m[4]
				}
				attrs[strings.ToLower(m[1])] = v
			}
			name := ""
			quoted := rune(0)
			for i, r := range line {
				if r == '"' || r == '\'' {
					if quoted == 0 {
						quoted = r
					} else if quoted == r {
						quoted = 0
					}
				}
				if r == ',' && quoted == 0 {
					name = strings.TrimSpace(line[i+1:])
					break
				}
			}
			if name == "" {
				name = attrs["tvg-name"]
			}
			itemGroup := attrs["group-title"]
			if itemGroup == "" {
				itemGroup = group
			}
			pending = &liveImportItem{TVGID: attrs["tvg-id"], Name: name, Logo: attrs["tvg-logo"], Group: itemGroup, Headers: map[string]string{}}
			continue
		}
		if strings.HasPrefix(line, "#EXTGRP:") {
			group = strings.TrimSpace(strings.TrimPrefix(line, "#EXTGRP:"))
			if pending != nil {
				pending.Group = group
			}
			continue
		}
		if strings.HasPrefix(line, "#EXTVLCOPT:") && pending != nil {
			key, value, ok := strings.Cut(strings.TrimPrefix(line, "#EXTVLCOPT:"), "=")
			if ok {
				header := map[string]string{"http-user-agent": "User-Agent", "http-referrer": "Referer", "http-referer": "Referer", "http-origin": "Origin", "http-cookie": "Cookie"}[strings.ToLower(key)]
				if header != "" {
					pending.Headers[header] = value
				}
			}
			continue
		}
		if strings.HasPrefix(line, "#EXTHTTP:") && pending != nil {
			var input map[string]string
			if json.Unmarshal([]byte(strings.TrimPrefix(line, "#EXTHTTP:")), &input) == nil {
				for k, v := range input {
					pending.Headers[k] = v
				}
			}
			continue
		}
		if strings.HasPrefix(line, "#") {
			continue
		}
		var item liveImportItem
		if pending != nil {
			item = *pending
			pending = nil
			item.URL = line
		} else {
			name, address, ok := strings.Cut(line, ",")
			if !ok {
				preview.Warnings = append(preview.Warnings, fmt.Sprintf("第 %d 行没有频道名，已略过", lineNo))
				continue
			}
			if strings.EqualFold(strings.TrimSpace(address), "#genre#") {
				group = strings.TrimSpace(name)
				continue
			}
			item = liveImportItem{Name: strings.TrimSpace(name), URL: strings.TrimSpace(address), Group: group, Headers: map[string]string{}}
		}
		if before, options, ok := strings.Cut(item.URL, "|"); ok {
			item.URL = before
			for _, pair := range strings.Split(options, "&") {
				k, v, found := strings.Cut(pair, "=")
				if found {
					decoded, e := url.QueryUnescape(v)
					if e == nil {
						item.Headers[k] = decoded
					}
				}
			}
		}
		if _, edition, found := strings.Cut(item.TVGID, "@"); found {
			item.Quality = strings.TrimSpace(edition)
		}
		// The upstream name contains resolution hints which a Chinese alias
		// deliberately omits. Read them before replacing the display name.
		if quality := liveResolution.FindString(item.Name); quality != "" {
			item.Quality = quality
		}
		item.TVGID = liveNormalizeTVGID(item.TVGID)
		item.Name = liveDisplayName(item.TVGID, item.Name)
		if item.Name == "" || len([]rune(item.Name)) > 200 || len(item.TVGID) > 200 || len([]rune(item.Group)) > 120 || liveCheckImportURL(item.URL) != nil {
			preview.Warnings = append(preview.Warnings, fmt.Sprintf("第 %d 行频道或地址无效，已略过", lineNo))
			continue
		}
		if item.Group == "" {
			item.Group = defaultGroup
		}
		item.Group = liveGroupDisplayName(item.Group)
		headers, err := liveCleanHeaders(item.Headers)
		if err != nil {
			preview.Warnings = append(preview.Warnings, fmt.Sprintf("第 %d 行请求头无效，已略过", lineNo))
			continue
		}
		item.Headers = headers
		if item.Logo != "" && liveCheckImportURL(item.Logo) != nil {
			item.Logo = ""
		}
		identity := liveChannelIdentity(item)
		key := identity + "|" + liveIdentity(item.URL)
		if seen[key] {
			continue
		}
		seen[key] = true
		channels[identity] = true
		if !groups[item.Group] {
			groups[item.Group] = true
			preview.Groups = append(preview.Groups, item.Group)
		}
		result = append(result, item)
		if len(preview.Sample) < 20 {
			sample := item
			sample.Headers = liveRedactHeaders(sample.Headers)
			preview.Sample = append(preview.Sample, sample)
		}
		if len(result) > 10000 {
			return nil, preview, errors.New("单次导入最多 10000 条直播源")
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, preview, errors.New("直播列表行过长或读取失败")
	}
	if len(preview.Warnings) > 30 {
		preview.Warnings = preview.Warnings[:30]
	}
	preview.Channels = len(channels)
	preview.Streams = len(result)
	if len(result) == 0 {
		return nil, preview, errors.New("列表未包含有效的 HTTP/HTTPS 频道；现有数据不会被覆盖")
	}
	return result, preview, nil
}

func liveFetchSubscription(ctx context.Context, address string) (string, error) {
	content, err := liveFetchSubscriptionURL(ctx, address)
	if err != nil {
		if fallback := liveOfficialSubscriptionFallback(address); fallback != "" {
			return liveFetchSubscriptionURL(ctx, fallback)
		}
	}
	return content, err
}

// The exact official mirror is independently subject to the same URL/DNS and
// redirect validation; arbitrary subscription URLs never acquire a fallback.
func liveFetchSubscriptionURL(ctx context.Context, address string) (string, error) {
	if err := liveCheckImportURL(address); err != nil {
		return "", err
	}
	if err := validateLiveSubscriptionURL(ctx, address); err != nil {
		return "", errors.New("订阅地址不是允许的公网地址")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return "", errors.New("订阅地址无效")
	}
	req.Header.Set("User-Agent", mediaUserAgent)
	response, err := liveSubscriptionHTTPClient(20e9).Do(req)
	if err != nil {
		return "", errors.New("订阅下载超时或网络不可用")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("订阅返回 HTTP %d；现有数据保留", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, liveImportLimit+1))
	if err != nil || len(body) > liveImportLimit {
		return "", errors.New("订阅读取失败或超过 16 MB")
	}
	return string(body), nil
}

func liveSensitiveHeader(k string) bool {
	switch strings.ToLower(k) {
	case "authorization", "cookie", "x-token", "x-api-key":
		return true
	}
	return false
}
func liveRedactHeaders(input map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range input {
		if liveSensitiveHeader(k) && v != "" {
			out[k] = "••••••"
		} else {
			out[k] = v
		}
	}
	return out
}
