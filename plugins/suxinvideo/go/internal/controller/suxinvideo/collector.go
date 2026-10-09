package suxinvideo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/suxinwl/GoSuxin/framework/database/gdb"
	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/util/gconv"
)

type CollectReq struct {
	g.Meta `path:"/collect" method:"post"`
	APIID  int64 `p:"api_id"`
	Page   int   `p:"page"`
	TypeID int   `p:"type_id"`
	Hours  int   `p:"hours"`
}
type CollectRes struct{}
type macPayload struct {
	Code      int              `json:"code"`
	Total     int              `json:"total"`
	Page      int              `json:"page"`
	PageCount int              `json:"pagecount"`
	List      []map[string]any `json:"list"`
	Class     []map[string]any `json:"class"`
}

// MacCMS sources are inconsistent about numeric metadata: some return page
// as "1" while others return 1. Keep the response typed after decoding.
func (payload *macPayload) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	for _, field := range []struct {
		name string
		dest *int
	}{{"code", &payload.Code}, {"total", &payload.Total}, {"page", &payload.Page}, {"pagecount", &payload.PageCount}} {
		raw := strings.TrimSpace(string(fields[field.name]))
		if raw == "" || raw == "null" {
			continue
		}
		number, err := strconv.Atoi(strings.Trim(raw, `"`))
		if err != nil {
			return fmt.Errorf("采集源 %s 字段无效: %w", field.name, err)
		}
		*field.dest = number
	}
	for _, field := range []struct {
		name string
		dest *[]map[string]any
	}{{"list", &payload.List}, {"class", &payload.Class}} {
		raw := strings.TrimSpace(string(fields[field.name]))
		if raw == "" || raw == "null" || raw == `""` || raw == "false" {
			continue
		}
		if field.name == "class" && strings.HasPrefix(raw, "{") {
			var keyed map[string]map[string]any
			if err := json.Unmarshal(fields[field.name], &keyed); err != nil {
				return fmt.Errorf("采集源 %s 字段无效: %w", field.name, err)
			}
			keys := make([]string, 0, len(keyed))
			for key := range keyed {
				keys = append(keys, key)
			}
			sort.Slice(keys, func(i, j int) bool {
				left, leftErr := strconv.Atoi(keys[i])
				right, rightErr := strconv.Atoi(keys[j])
				if leftErr == nil && rightErr == nil {
					return left < right
				}
				return keys[i] < keys[j]
			})
			for _, key := range keys {
				*field.dest = append(*field.dest, keyed[key])
			}
			continue
		}
		if err := json.Unmarshal(fields[field.name], field.dest); err != nil {
			return fmt.Errorf("采集源 %s 字段无效: %w", field.name, err)
		}
	}
	return nil
}

// The legacy one-page endpoint uses the same persistent queue, per-source
// lease and global worker limit as /collect/job/start. It returns the queued
// job immediately; callers read its result through /collect/job/status.
func (admin *Admin) Collect(ctx context.Context, req *CollectReq) (*CollectRes, error) {
	_, err := admin.CollectJobStart(ctx, &CollectJobStartReq{
		APIID:   req.APIID,
		Page:    req.Page,
		TypeID:  req.TypeID,
		Hours:   req.Hours,
		OnePage: true,
	})
	return &CollectRes{}, err
}

// Detail metadata takes precedence, while missing fields retain list metadata.
// Category rules must see the actual detail category before the item is saved.
func collectedDetailItem(list, detail map[string]any) map[string]any {
	merged := make(map[string]any, len(list)+len(detail))
	for key, value := range list {
		merged[key] = value
	}
	for key, value := range detail {
		if value != nil && strings.TrimSpace(gconv.String(value)) != "" {
			merged[key] = value
		}
	}
	return merged
}

func fetchMac(ctx context.Context, raw string, params url.Values) (macPayload, error) {
	u, err := url.Parse(strings.TrimRight(raw, "?&/"))
	if err != nil || u.Host == "" {
		return macPayload{}, errors.New("采集源 URL 无效")
	}
	query := u.Query()
	for key, values := range params {
		query[key] = values
	}
	u.RawQuery = query.Encode()
	if err := safeCollectorURL(ctx, u.String()); err != nil {
		return macPayload{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return macPayload{}, err
	}
	request.Header.Set("User-Agent", "Mozilla/5.0 (iPhone; CPU iPhone OS 18_7 like Mac OS X) AppleWebKit/605.1.15 Mobile/15E148 Safari/604.1")
	request.Header.Set("Accept", "application/json, text/plain, */*")
	client := safeCollectorHTTPClient(30 * time.Second)
	response, err := client.Do(request)
	if err != nil {
		return macPayload{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return macPayload{}, fmt.Errorf("采集源返回 HTTP %d", response.StatusCode)
	}
	var data macPayload
	if err := json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&data); err != nil {
		return data, err
	}
	if !validMacPayload(data, params.Get("ac")) {
		return data, errors.New("采集源响应状态异常")
	}
	if len(data.List) > 1000 {
		return data, errors.New("单页影片数量过多")
	}
	return data, nil
}

func validMacPayload(data macPayload, action string) bool {
	return data.Code == 1 || (action == "list" && len(data.Class) > 0)
}

func cutRunes(s string, n int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}

var seasonPattern = regexp.MustCompile(`第([0-9一二三四五六七八九十]+)季`)

func normalizeVodName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	name = seasonPattern.ReplaceAllString(name, "$1季")
	var b strings.Builder
	for _, ch := range name {
		if unicode.Is(unicode.Han, ch) || unicode.IsLetter(ch) || unicode.IsDigit(ch) {
			b.WriteRune(ch)
		}
	}
	return cutRunes(b.String(), 130)
}
func mergePlay(oldFrom, oldURL, newFrom, newURL string) (string, string) {
	names := []string{}
	groups := map[string][]string{}
	add := func(from, raw string) {
		fs, urls := strings.Split(from, "$$$"), strings.Split(raw, "$$$")
		for i, f := range fs {
			f = strings.TrimSpace(f)
			if f == "" {
				continue
			}
			if _, ok := groups[f]; !ok {
				groups[f] = []string{}
				names = append(names, f)
			}
			if i >= len(urls) {
				continue
			}
			seen := map[string]int{}
			for index, v := range groups[f] {
				seen[collectedEpisodeKey(f, v)] = index
			}
			for _, v := range strings.Split(urls[i], "#") {
				v = strings.TrimSpace(v)
				if v == "" {
					continue
				}
				key := collectedEpisodeKey(f, v)
				if index, exists := seen[key]; exists {
					// An updated source may return a renewed signed URL for the same
					// episode. Replace the stale URL without doubling the episode.
					groups[f][index] = v
				} else {
					groups[f] = append(groups[f], v)
					seen[key] = len(groups[f]) - 1
				}
			}
		}
	}
	add(oldFrom, oldURL)
	add(newFrom, newURL)
	out := make([]string, 0, len(names))
	for _, f := range names {
		out = append(out, strings.Join(groups[f], "#"))
	}
	return strings.Join(names, "$$$"), strings.Join(out, "$$$")
}

var playEpisodeNumber = regexp.MustCompile(`^(?:第)?0*([0-9]+)(?:集|话|期)?$`)

func playEpisodeKey(raw string) string {
	parts := strings.SplitN(strings.TrimSpace(raw), "$", 2)
	if len(parts) < 2 || strings.TrimSpace(parts[0]) == "" {
		return "url:" + strings.TrimSpace(raw)
	}
	name := strings.ToLower(strings.Join(strings.Fields(parts[0]), ""))
	if match := playEpisodeNumber.FindStringSubmatch(name); len(match) == 2 {
		return "episode:" + match[1]
	}
	return "name:" + name
}
func ensureMacType(ctx context.Context, name string) (int64, error) {
	name = cutRunes(name, 30)
	if name == "" {
		name = "未分类"
	}
	release, err := collectionWrites.acquire(ctx, "type:mac:"+name)
	if err != nil {
		return 0, err
	}
	defer release()
	row, err := one(ctx, "SELECT id FROM sx_type WHERE name=? ORDER BY id LIMIT 1", name)
	if err != nil {
		return 0, err
	}
	if row != nil {
		return gconv.Int64(row["id"]), nil
	}
	result, err := g.DB().Exec(ctx, "INSERT INTO sx_type(pid,name,sort,status) VALUES(0,?,50,1)", name)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}
func fourKVMCollectionSourceEnabled(collector row) bool {
	return collector != nil && strings.TrimSpace(gconv.String(collector["api_url"])) == fourKVMSourceURL && gconv.Int(collector["status"]) == 1
}

func validateFourKVMCollectedPlaylist(collector row, remoteID, from, play string) (string, string, error) {
	if !fourKVMCollectionSourceEnabled(collector) {
		return "", "", errors.New("4KVM 采集源不存在或已停用")
	}
	// api_vid is varchar(32). Never silently truncate a slug and attach another
	// film's episodes to the truncated identifier.
	if remoteID == "" || len(remoteID) > 32 {
		return "", "", errors.New("4KVM 影片编号无效或过长")
	}
	sources := playlist(row{"play_from": from, "play_url": play})
	if from != "4kvm" || len(sources) != 1 || sources[0].Code != "4kvm" || len(sources[0].Episodes) == 0 {
		return "", "", errors.New("4KVM 影片详情未返回有效原生线路")
	}
	for _, ep := range sources[0].Episodes {
		slug, ok := fourKVMDiscoveryMarker(ep.URL)
		if !ok || slug != remoteID {
			return "", "", errors.New("4KVM 分集标识与影片编号不符")
		}
	}
	sources[0] = discoveryNormalizeSource(sources[0])
	from, play = serializeDiscoverySources(sources)
	return from, play, nil
}

func fourKVMCollectionCompatible(vod row, kind string, item map[string]any, apiID int64) bool {
	target := discoveryTarget{Name: gconv.String(vod["name"]), Year: gconv.String(vod["year"]), Area: gconv.String(vod["area"]), Kind: kind}
	if gconv.Int64(vod["api_id"]) == apiID && gconv.String(vod["api_vid"]) == gconv.String(item["vod_id"]) {
		target.SourceAPIID, target.SourceAPIVID = apiID, gconv.String(item["vod_id"])
	}
	// An established remote identifier tolerates absent metadata, never an
	// explicit conflicting title, year, region or kind.
	return discoveryMatches(target, item, apiID)
}

const collectVodBlocked = 3

func upsertMacVod(ctx context.Context, apiID int64, item map[string]any) (int, error) {
	id, name := cutRunes(gconv.String(item["vod_id"]), 32), cutRunes(gconv.String(item["vod_name"]), 120)
	if id == "" || name == "" {
		return 0, nil
	}
	if !contentMacAllowed(ctx, item) {
		return collectVodBlocked, nil
	}
	from, play := strings.TrimSpace(gconv.String(item["vod_play_from"])), gconv.String(item["vod_play_url"])
	if len(from) > maxPlaybackSourceNames || len(play) > 1<<20 {
		return 0, errors.New("播放列表过大")
	}
	isYQK := gconv.Bool(item["__yqk"]) || strings.HasPrefix(from, "yqk_")
	isErciyuan := gconv.Bool(item["__erciyuan"])
	isHongguo := gconv.Bool(item["__hongguo"])
	is4KVM := false
	for _, code := range strings.Split(from, "$$$") {
		is4KVM = is4KVM || strings.TrimSpace(code) == "4kvm"
		isErciyuan = isErciyuan || strings.HasPrefix(strings.TrimSpace(code), "ecy_")
		isHongguo = isHongguo || strings.TrimSpace(code) == "hongguo"
	}
	if isHongguo {
		collector, e := one(ctx, "SELECT api_url,status FROM sx_collect_api WHERE id=?", apiID)
		if e != nil {
			return 0, e
		}
		from, play, e = validateHongguoCollectedPlaylist(collector, gconv.String(item["vod_id"]), from, play)
		if e != nil {
			return 0, e
		}
		item["type_name"] = "短剧"
	}
	if isErciyuan {
		collector, e := one(ctx, "SELECT api_url,status FROM sx_collect_api WHERE id=?", apiID)
		if e != nil {
			return 0, e
		}
		from, play, e = validateErciyuanCollectedPlaylist(collector, gconv.String(item["vod_id"]), from, play)
		if e != nil {
			return 0, e
		}
	}
	if is4KVM {
		collector, e := one(ctx, "SELECT api_url,status FROM sx_collect_api WHERE id=?", apiID)
		if e != nil {
			return 0, e
		}
		from, play, e = validateFourKVMCollectedPlaylist(collector, gconv.String(item["vod_id"]), from, play)
		if e != nil {
			return 0, e
		}
	}
	if isYQK {
		collector, e := one(ctx, "SELECT api_url,status FROM sx_collect_api WHERE id=?", apiID)
		if e != nil {
			return 0, e
		}
		if collector == nil || !isYQKSource(gconv.String(collector["api_url"])) || gconv.Int(collector["status"]) != 1 {
			return 0, errors.New("小柒采集源不存在或已停用")
		}
		if !validYQKRemoteID(id) {
			return 0, errors.New("小柒影片编号无效")
		}
		sources := playlist(row{"play_from": from, "play_url": play})
		if len(sources) == 0 {
			return 0, errors.New("小柒影片详情未返回可用线路")
		}
		for i, src := range sources {
			if !yqkAllowedSource(src) {
				return 0, errors.New("小柒影片返回了未知线路或无效分集")
			}
			for _, ep := range src.Episodes {
				vodID, _, _, _ := yqkMarkerParts(ep.URL)
				if vodID != id {
					return 0, errors.New("小柒分集与影片编号不符")
				}
			}
			sources[i] = discoveryNormalizeSource(src)
		}
		from, play = serializeDiscoverySources(sources)
	}
	release, err := collectionWrites.acquire(ctx, collectionVodWriteKeys(apiID, id, name)...)
	if err != nil {
		return 0, err
	}
	defer release()
	ordinaryMatcher := newOrdinaryCollectionMatcher(apiID, item)
	remarks := cutRunes(gconv.String(item["vod_remarks"]), 60)
	existing, err := one(ctx, "SELECT id,api_id,api_vid,name,class,year,area,type_id,play_from,play_url,pic,remarks FROM sx_vod WHERE api_id=? AND api_vid=?", apiID, id)
	if err != nil {
		return 0, err
	}
	aliasBound := false
	if existing != nil {
		aliasBound, err = vodAliasCollectionBound(ctx, existing, apiID, item)
		if err != nil {
			return 0, err
		}
	}
	if isYQK && existing != nil && !aliasBound {
		if err = validateYQKCollectionIdentity(ctx, existing, apiID, item); err != nil {
			return 0, err
		}
	}
	if isErciyuan && existing != nil && !aliasBound {
		if err = validateErciyuanCollectionIdentity(ctx, existing, apiID, item); err != nil {
			return 0, err
		}
	}
	if isHongguo && existing != nil && !aliasBound {
		if err = validateHongguoCollectionIdentity(ctx, existing, apiID, item); err != nil {
			return 0, err
		}
	}
	if !aliasBound && (existing == nil || isYQK || isErciyuan || isHongguo) && setting(ctx, "collect_dedup_title", "1") == "1" {
		if isYQK || is4KVM || isErciyuan || isHongguo {
			var matched row
			var matchErr error
			if isHongguo {
				matched, matchErr = findHongguoCollectionTarget(ctx, apiID, item)
			} else if isErciyuan {
				matched, matchErr = findNativeCollectionTarget(ctx, apiID, item)
			} else {
				matched, matchErr = findYQKCollectionTarget(ctx, apiID, item)
			}
			if matchErr != nil && !(existing != nil && (errors.Is(matchErr, errCollectionIdentityIncomplete) || errors.Is(matchErr, errHongguoIdentityAmbiguous))) {
				return 0, matchErr
			}
			if matched != nil {
				existing = matched
			}
		} else {
			candidates, lookupErr := all(ctx, "SELECT id,api_id,api_vid,name,class,year,area,type_id,play_from,play_url,pic,remarks FROM sx_vod WHERE name=? OR name_norm=? OR name_norm=? ORDER BY id LIMIT 100", name, normalizeVodName(name), normalizeVodName(discoveryNormalizeTitle(name)))
			if lookupErr != nil {
				return 0, lookupErr
			}
			for _, candidate := range candidates {
				compatible, identityErr := ordinaryMatcher.compatible(ctx, candidate)
				if identityErr != nil {
					return 0, identityErr
				}
				if compatible {
					existing = candidate
					break
				}
			}
		}
		if err != nil {
			return 0, err
		}
	}
	if existing != nil {
		if !contentVodAllowed(ctx, existing) {
			return collectVodBlocked, nil
		}
		if aliasBound {
			existing["__collection_preserve_metadata"] = true
		}
		if !isYQK && !is4KVM && !isErciyuan && !isHongguo {
			compatible, identityErr := aliasBound, error(nil)
			if !aliasBound {
				compatible, identityErr = ordinaryMatcher.compatible(ctx, existing)
			}
			if identityErr != nil {
				return 0, identityErr
			}
			if !compatible {
				return 0, errors.New("采集影片身份与已有记录冲突，已保留原影片资源")
			}
			existing["__collection_identity"] = discoveryFilmIdentity(existing)
		}
		if isYQK || is4KVM || isErciyuan || isHongguo {
			if isYQK && !aliasBound {
				if err = validateYQKCollectionIdentity(ctx, existing, apiID, item); err != nil {
					return 0, err
				}
			}
			if isErciyuan {
				if !aliasBound {
					err = validateErciyuanCollectionIdentity(ctx, existing, apiID, item)
				}
				if err != nil {
					return 0, err
				}
				existing["__collection_source"] = "erciyuan"
				target, targetErr := erciyuanFilmTarget(ctx, existing, apiID, id)
				if targetErr != nil {
					return 0, targetErr
				}
				if discoveryErciyuanTheatricalMatch(target, item) {
					existing["__erciyuan_theatrical_id"] = id
					if !erciyuanTheatricalStoredIDCompatible(existing, item) {
						return 0, errErciyuanTheatricalAmbiguous
					}
					from, play, err = verifyErciyuanTheatricalCollection(ctx, target, item)
					if err != nil {
						return 0, err
					}
				}
			}
			if isHongguo {
				if !aliasBound {
					err = validateHongguoCollectionIdentity(ctx, existing, apiID, item)
				}
				if err != nil {
					return 0, err
				}
				existing["__collection_source"] = "hongguo"
			}
			existing["__yqk_identity"] = discoveryFilmIdentity(existing)
			kind, e := discoveryCategoryKind(ctx, gconv.Int64(existing["type_id"]))
			if e != nil {
				return 0, e
			}
			if is4KVM {
				if !aliasBound && !fourKVMCollectionCompatible(existing, kind, item, apiID) {
					return 0, errors.New("4KVM 影片身份与已有记录冲突")
				}
				existing["__collection_source"] = "4kvm"
			}
			existing["__yqk_movie"] = kind == "movie" || kind == "anime_movie"
		}
		if err = updateCollectedVod(ctx, existing, apiID, id, from, play, remarks, gconv.String(item["vod_pic"])); err != nil {
			return 0, err
		}
		if err = saveCollectedVodScore(ctx, gconv.Int64(existing["id"]), apiID, item); err != nil {
			return 0, err
		}
		if aliases, ok := existing["__collection_aliases"].([]row); ok {
			for _, alias := range aliases {
				if isErciyuan {
					alias["__collection_source"] = "erciyuan"
				}
				if isHongguo {
					alias["__collection_source"] = "hongguo"
				}
				if err = updateCollectedVod(ctx, alias, apiID, id, from, play, remarks, gconv.String(item["vod_pic"])); err != nil {
					return 0, err
				}
				if err = saveCollectedVodScore(ctx, gconv.Int64(alias["id"]), apiID, item); err != nil {
					return 0, err
				}
			}
		}
		return 2, nil
	}
	typeName := cutRunes(gconv.String(item["type_name"]), 30)
	if typeName == "" {
		typeName = strings.Split(gconv.String(item["vod_class"]), ",")[0]
	}
	var typeID int64
	if isErciyuan {
		unlockType, lockErr := collectionWrites.acquire(ctx, "type:erciyuan")
		if lockErr != nil {
			return 0, lockErr
		}
		typeID, err = ensureErciyuanType(ctx, item)
		unlockType()
	} else {
		typeID, err = ensureMacType(ctx, typeName)
	}
	if err != nil {
		return 0, err
	}
	if !contentVodAllowed(ctx, row{"name": name, "class": item["vod_class"], "type_id": typeID}) {
		return collectVodBlocked, nil
	}
	now := time.Now().Unix()
	score, _ := collectedSourceScore(item)
	query := `INSERT INTO sx_vod(type_id,api_id,api_vid,name,name_norm,sub,class,year,area,lang,remarks,score,director,actor,content,pic,play_from,play_url,total_hits,status,addtime,updatetime) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`
	args := []any{typeID, apiID, id, name, normalizeVodName(name), cutRunes(gconv.String(item["vod_sub"]), 120), cutRunes(gconv.String(item["vod_class"]), 200), cutRunes(gconv.String(item["vod_year"]), 20), cutRunes(gconv.String(item["vod_area"]), 40), cutRunes(gconv.String(item["vod_lang"]), 40), remarks, score, cutRunes(gconv.String(item["vod_director"]), 400), cutRunes(gconv.String(item["vod_actor"]), 1000), cutRunes(gconv.String(item["vod_content"]), 10000), cutRunes(localizeImage(ctx, gconv.String(item["vod_pic"])), 500), from, play, gconv.Int(item["vod_hits"]), 1, now, now}
	if !is4KVM && !isErciyuan && !isHongguo {
		result, err := g.DB().Exec(ctx, query, args...)
		if err != nil {
			return 0, err
		}
		insertedID, err := result.LastInsertId()
		if err != nil {
			return 0, err
		}
		return 1, saveCollectedVodScore(ctx, insertedID, apiID, item)
	}
	var insertedID int64
	err = g.DB().Transaction(ctx, func(_ context.Context, tx gdb.TX) error {
		collector, err := tx.GetOne("SELECT api_url,status FROM sx_collect_api WHERE id=? FOR UPDATE", apiID)
		if err != nil {
			return err
		}
		if isErciyuan && !erciyuanCollectionSourceEnabled(gconv.Map(collector)) {
			return errors.New("二次元采集源已停用")
		}
		if is4KVM && !fourKVMCollectionSourceEnabled(gconv.Map(collector)) {
			return errors.New("4KVM 采集源已停用")
		}
		if isHongguo && !hongguoCollectionSourceEnabled(gconv.Map(collector)) {
			return errors.New("红果采集源已停用")
		}
		result, err := tx.Exec(query, args...)
		if err != nil {
			return err
		}
		insertedID, err = result.LastInsertId()
		return err
	})
	if err != nil {
		return 0, err
	}
	return 1, saveCollectedVodScore(ctx, insertedID, apiID, item)
}

// The initial title/API lookup identifies a film; only a locked fresh row may
// supply the playlist used for an update. Otherwise a slower collection can
// overwrite lines committed meanwhile by another collector or discovery job.
func updateCollectedVod(ctx context.Context, existing row, apiID int64, remoteID, from, play, remarks, incomingPic string) error {
	hasIdentity := gconv.String(existing["__yqk_identity"]) != "" || gconv.String(existing["__collection_identity"]) != ""
	preserveMetadata := hasIdentity && (gconv.Int64(existing["api_id"]) != apiID || gconv.Bool(existing["__collection_preserve_metadata"]))
	needsPicture := func(vod row) bool {
		if preserveMetadata {
			return false
		}
		pic := gconv.String(vod["pic"])
		return pic == "" || (from == "hongguo" && gconv.Int64(vod["api_id"]) == apiID && gconv.String(vod["api_vid"]) == remoteID && isHEICURL(pic) && incomingPic != "")
	}
	preparedPic, pictureReady := "", false
	preparePicture := func() {
		preparedPic, pictureReady = cutRunes(localizeImage(ctx, incomingPic), 500), true
	}
	if needsPicture(existing) {
		preparePicture()
	}
	needPicture := errors.New("picture changed while collection was waiting")
	update := func() error {
		return g.DB().Transaction(ctx, func(_ context.Context, tx gdb.TX) error {
			return (collectedVodUpdate{existing: existing, apiID: apiID, from: from, play: play, remarks: remarks, preparedPic: preparedPic,
				preserveMetadata: preserveMetadata, pictureReady: pictureReady, needsPicture: needsPicture, needPicture: needPicture}).apply(tx)
		})
	}
	if err := update(); errors.Is(err, needPicture) {
		// A concurrent editor cleared the cover. Fetch it outside the row lock,
		// then re-read again instead of holding a transaction across HTTP I/O.
		preparePicture()
		return update()
	} else {
		return err
	}
}

// All identity and source checks use the same locked transaction that writes
// the merged playlist. The row is re-read before using its playback data.
type collectedVodUpdate struct {
	existing                         row
	apiID                            int64
	from, play, remarks, preparedPic string
	preserveMetadata, pictureReady   bool
	needsPicture                     func(row) bool
	needPicture                      error
}

func (update collectedVodUpdate) apply(tx gdb.TX) error {
	existing, apiID := update.existing, update.apiID
	from, play, remarks, preparedPic := update.from, update.play, update.remarks, update.preparedPic
	preserveMetadata, pictureReady := update.preserveMetadata, update.pictureReady
	needsPicture, needPicture := update.needsPicture, update.needPicture
	record, err := tx.GetOne("SELECT id,api_id,api_vid,name,class,year,area,type_id,play_from,play_url,pic,remarks FROM sx_vod WHERE id=? FOR UPDATE", existing["id"])
	if err != nil {
		return err
	}
	if record == nil {
		return errors.New("待更新影片已删除")
	}
	current := gconv.Map(record)
	replaceHongguo := false
	if identity := gconv.String(existing["__collection_identity"]); identity != "" && discoveryFilmIdentity(current) != identity {
		return errors.New("影片身份信息已更新，请重新采集")
	}
	if identity := gconv.String(existing["__yqk_identity"]); identity != "" {
		if discoveryFilmIdentity(current) != identity {
			return errors.New("影片身份信息已更新，请重新采集")
		}
		query := "SELECT api_url,status FROM sx_collect_api WHERE id=?"
		nativeSource := gconv.String(existing["__collection_source"])
		if nativeSource == "4kvm" || nativeSource == "erciyuan" || nativeSource == "hongguo" {
			query += " FOR UPDATE"
		}
		collector, e := tx.GetOne(query, apiID)
		if e != nil {
			return e
		}
		if nativeSource == "hongguo" {
			if !hongguoCollectionSourceEnabled(gconv.Map(collector)) {
				return errors.New("红果采集源已停用")
			}
			if _, _, err = validateHongguoCollectedPlaylist(gconv.Map(collector), hongguoIncomingSeries(from, play), from, play); err != nil {
				return err
			}
			oldSeries, valid := hongguoStoredSeries(current)
			if !valid || (oldSeries != "" && oldSeries != hongguoIncomingSeries(from, play)) {
				if gconv.Int64(current["api_id"]) != apiID || gconv.String(current["api_vid"]) != hongguoIncomingSeries(from, play) {
					return errors.New("红果原生线路身份已更新，请重新采集")
				}
				replaceHongguo = true
			}
		} else if nativeSource == "erciyuan" {
			if !erciyuanCollectionSourceEnabled(gconv.Map(collector)) {
				return errors.New("二次元采集源已停用")
			}
			if remoteID := gconv.String(existing["__erciyuan_theatrical_id"]); remoteID != "" {
				item := map[string]any{"vod_id": remoteID}
				if !erciyuanTheatricalStoredIDCompatible(current, item) || !erciyuanTheatricalStoredIDCompatible(row{"play_from": from, "play_url": play}, item) {
					return errErciyuanTheatricalAmbiguous
				}
			}
		} else if nativeSource == "4kvm" {
			if !fourKVMCollectionSourceEnabled(gconv.Map(collector)) {
				return errors.New("4KVM 采集源已停用")
			}
		} else if collector == nil || !isYQKSource(collector["api_url"].String()) || collector["status"].Int() != 1 {
			return errors.New("小柒采集源已停用")
		}
	}
	pic := gconv.String(current["pic"])
	if needsPicture(current) {
		if !pictureReady {
			return needPicture
		}
		pic = preparedPic
	}
	currentRemarks := remarks
	if currentRemarks == "" {
		currentRemarks = gconv.String(current["remarks"])
	}
	mergedFrom, mergedURL := mergePlay(gconv.String(current["play_from"]), gconv.String(current["play_url"]), from, play)
	if gconv.String(existing["__yqk_identity"]) != "" {
		merged, _, _ := mergeDiscoveryFilmPlaylists(playlist(current), playlist(row{"play_from": from, "play_url": play}), gconv.String(current["name"]), gconv.Bool(existing["__yqk_movie"]))
		mergedFrom, mergedURL = serializeDiscoverySources(merged)
	}
	if replaceHongguo {
		incoming := playlist(row{"play_from": from, "play_url": play})
		mergedFrom, mergedURL = replaceHongguoCollectedLine(current, incoming[0])
	}
	if len(mergedFrom) > maxPlaybackSourceNames || len(mergedURL) > 1<<20 {
		return errors.New("合并后的播放列表过大")
	}
	area := strings.TrimSpace(gconv.String(existing["__collection_area"]))
	fillArea := discoveryRegion(gconv.String(current["area"])) == "" && discoveryRegion(area) != ""
	if preserveMetadata {
		if fillArea {
			_, err = tx.Exec("UPDATE sx_vod SET play_from=?,play_url=?,area=? WHERE id=?", mergedFrom, mergedURL, area, existing["id"])
			return err
		}
		_, err = tx.Exec("UPDATE sx_vod SET play_from=?,play_url=? WHERE id=?", mergedFrom, mergedURL, existing["id"])
		return err
	}
	if fillArea {
		_, err = tx.Exec("UPDATE sx_vod SET play_from=?,play_url=?,remarks=?,pic=?,updatetime=?,area=? WHERE id=?", mergedFrom, mergedURL, currentRemarks, pic, time.Now().Unix(), area, existing["id"])
		return err
	}
	_, err = tx.Exec("UPDATE sx_vod SET play_from=?,play_url=?,remarks=?,pic=?,updatetime=? WHERE id=?", mergedFrom, mergedURL, currentRemarks, pic, time.Now().Unix(), existing["id"])
	return err
}

// Ordinary HTTP sources historically allow absent metadata. Retain that
// behavior, but never merge explicit title/year/region/category conflicts.
// Native providers keep their separate, stricter provenance checks above.
func ordinaryCollectionCompatible(ctx context.Context, vod row, apiID int64, item map[string]any) (bool, error) {
	return newOrdinaryCollectionMatcher(apiID, item).compatible(ctx, vod)
}

func ordinaryCollectionMatches(vod row, kind string, apiID int64, item map[string]any) bool {
	target := discoveryTarget{Name: gconv.String(vod["name"]), Year: gconv.String(vod["year"]), Area: gconv.String(vod["area"]), Kind: kind}
	if gconv.Int64(vod["api_id"]) == apiID && gconv.String(vod["api_vid"]) == gconv.String(item["vod_id"]) {
		target.SourceAPIID, target.SourceAPIVID = apiID, gconv.String(item["vod_id"])
	}
	if series, valid := hongguoStoredSeries(vod); valid && series != "" {
		return discoveryMatches(target, item, apiID) || hongguoForeignCollectionMatches(vod, target, item)
	}
	return discoveryCompatible(target, item, apiID, true)
}
