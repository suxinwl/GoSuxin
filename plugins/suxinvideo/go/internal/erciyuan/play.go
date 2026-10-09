package erciyuan

import (
	"context"
	"errors"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var markerPattern = regexp.MustCompile(`^erciyuan://([1-9][0-9]{0,11})/(aa02|aa03|dd02|4k01)/(0|[1-9][0-9]{0,5})$`)
var fieldPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_]*){0,7}$`)

func ParseMarker(marker string) (filmID, line string, index int, ok bool) {
	parts := markerPattern.FindStringSubmatch(marker)
	if len(parts) != 4 {
		return "", "", 0, false
	}
	index, err := strconv.Atoi(parts[3])
	if err != nil {
		return "", "", 0, false
	}
	return parts[1], parts[2], index, true
}
func ValidLineCode(code string) bool {
	switch code {
	case "ecy_aa02", "ecy_aa03", "ecy_dd02", "ecy_4k01":
		return true
	}
	return false
}
func lineKey(name string) string {
	upper := strings.ToUpper(strings.TrimSpace(name))
	for _, pair := range [][2]string{{"AA-02", "aa02"}, {"AA-03", "aa03"}, {"DD-02", "dd02"}, {"4K-01", "4k01"}} {
		if upper == pair[0] || strings.HasPrefix(upper, pair[0]+"[") || strings.HasPrefix(upper, pair[0]+"【") {
			return pair[1]
		}
	}
	return ""
}

type rawLine struct {
	key, name string
	episodes  []Episode
	indexes   []int
	parsers   []map[string]any
}

func extractLines(item map[string]any) []rawLine {
	names := strings.Split(stringValue(item["vod_play_from"]), "$$$")
	playlists := strings.Split(stringValue(item["vod_play_url"]), "$$$")
	parserRows, _ := item["play_from_parsers"].([]any)
	var lines []rawLine
	for i, playlist := range playlists {
		if i >= len(names) {
			continue
		}
		key := lineKey(names[i])
		if key == "" {
			continue
		}
		line := rawLine{key: key, name: names[i]}
		for originalIndex, entry := range strings.Split(playlist, "#") {
			parts := strings.SplitN(entry, "$", 2)
			if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
				continue
			}
			line.episodes = append(line.episodes, Episode{Name: strings.TrimSpace(parts[0]), URL: strings.TrimSpace(parts[1])})
			line.indexes = append(line.indexes, originalIndex)
		}
		if i < len(parserRows) {
			line.parsers = objectRows(parserRows[i])
		}
		if len(line.episodes) > 0 {
			lines = append(lines, line)
		}
	}
	return lines
}

func (c *Client) detailItem(ctx context.Context, id string) (map[string]any, error) {
	if !validID(id) {
		return nil, errors.New("二次元影片编号无效")
	}
	data, err := c.api(ctx, "/vod/play", url.Values{"id": {id}})
	if err != nil {
		return nil, err
	}
	rows := objectRows(data["list"])
	if encrypted, ok := data["list"].(string); ok && encrypted != "" {
		decoded, err := decodeDocument([]byte(encrypted))
		if err != nil {
			return nil, err
		}
		rows = objectRows(decoded)
	}
	if len(rows) == 0 {
		return nil, errors.New("二次元影片不存在或暂未提供播放线路")
	}
	item := rows[0]
	if stringValue(item["vod_id"]) != id {
		return nil, errors.New("二次元详情返回了其他影片")
	}
	if intValue(item["web_play_blocked"]) != 0 || item["web_play_blocked"] == true {
		return nil, errors.New("二次元上游限制该影片网页播放")
	}
	c.mu.Lock()
	merged := cloneRow(c.metadata[id])
	c.mu.Unlock()
	for key, value := range item {
		if value != nil && (stringValue(value) != "" || key == "play_from_parsers") {
			merged[key] = value
		}
	}
	return merged, nil
}

func (c *Client) Detail(ctx context.Context, id string) (Detail, error) {
	item, err := c.detailItem(ctx, id)
	if err != nil {
		return Detail{}, err
	}
	result := Detail{Item: cloneRow(item)}
	for _, raw := range extractLines(item) {
		line := Line{Code: "ecy_" + raw.key, Name: "二次元·" + raw.name}
		for index, ep := range raw.episodes {
			line.Episodes = append(line.Episodes, Episode{Name: ep.Name, URL: "erciyuan://" + id + "/" + raw.key + "/" + strconv.Itoa(raw.indexes[index])})
		}
		result.Lines = append(result.Lines, line)
	}
	// Do not expose temporary CDN URLs or parser templates to persistent CRUD
	// fields. Consumers use Lines and stable markers to merge source resources.
	delete(result.Item, "vod_play_url")
	delete(result.Item, "play_from_parsers")
	if len(result.Lines) == 0 {
		return Detail{}, errors.New("二次元暂无受支持的播放线路")
	}
	return result, nil
}

func (c *Client) Resolve(ctx context.Context, marker string) (Media, error) {
	id, key, index, ok := ParseMarker(marker)
	if !ok {
		return Media{}, errors.New("二次元分集标识无效")
	}
	item, err := c.detailItem(ctx, id)
	if err != nil {
		return Media{}, err
	}
	for _, line := range extractLines(item) {
		if line.key != key {
			continue
		}
		position := -1
		for i, originalIndex := range line.indexes {
			if originalIndex == index {
				position = i
				break
			}
		}
		if position < 0 {
			return Media{}, errors.New("二次元该分集已不存在，请刷新影片线路")
		}
		episode := line.episodes[position]
		if len(line.parsers) == 0 {
			if err := validateMedia(episode.URL); err != nil {
				return Media{}, err
			}
			return Media{URL: episode.URL, Headers: map[string]string{}}, nil
		}
		var lastErr error
		for _, parser := range line.parsers {
			media, err := c.parseMedia(ctx, parser, episode.URL)
			if err == nil {
				return media, nil
			}
			if ctx.Err() != nil {
				return Media{}, ctx.Err()
			}
			lastErr = err
		}
		if lastErr != nil {
			return Media{}, lastErr
		}
		return Media{}, errors.New("二次元该线路缺少有效解析器")
	}
	return Media{}, errors.New("二次元该播放线路已不存在，请刷新影片")
}

func (c *Client) parseMedia(ctx context.Context, parser map[string]any, episode string) (Media, error) {
	if stringValue(parser["type"]) != "json" {
		return Media{}, errors.New("二次元解析器类型尚未支持")
	}
	method := strings.ToUpper(stringValue(parser["method"]))
	if method != "" && method != "GET" {
		return Media{}, errors.New("二次元解析器请求方式尚未支持")
	}
	if len(episode) > 8192 || strings.ContainsAny(episode, "\r\n\x00") {
		return Media{}, errors.New("二次元解析参数无效")
	}
	template := stringValue(parser["url"])
	if strings.Count(template, "{url}") != 1 {
		return Media{}, errors.New("二次元解析器地址模板无效")
	}
	parsed, err := url.Parse(template)
	if err != nil || validRequestURL(parsed) != nil || !strings.Contains(parsed.RawQuery, "{url}") {
		return Media{}, errors.New("二次元解析器地址不受支持")
	}
	field := stringValue(parser["jsonPlayUrl"])
	if !fieldPattern.MatchString(field) {
		return Media{}, errors.New("二次元解析器媒体字段不受支持")
	}
	address := strings.Replace(template, "{url}", url.QueryEscape(episode), 1)
	raw, err := c.read(ctx, address, nil)
	if err != nil {
		return Media{}, err
	}
	document, err := decodeDocument(raw)
	if err != nil {
		return Media{}, err
	}
	if object, ok := document.(map[string]any); ok {
		if _, exists := object["code"]; exists {
			code := intValue(object["code"])
			if code != 0 && code != 1 && code != 200 {
				return Media{}, errors.New("二次元解析服务未返回可播放媒体")
			}
		}
	}
	value := document
	for _, component := range strings.Split(field, ".") {
		object, ok := value.(map[string]any)
		if !ok {
			return Media{}, errors.New("二次元解析响应缺少媒体字段")
		}
		value = object[component]
	}
	media := stringValue(value)
	if err := validateMedia(media); err != nil {
		return Media{}, err
	}
	return Media{URL: media, Headers: map[string]string{}}, nil
}
