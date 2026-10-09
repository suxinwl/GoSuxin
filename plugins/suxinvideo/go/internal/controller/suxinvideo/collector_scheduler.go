package suxinvideo

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/util/gconv"
	"github.com/suxinwl/GoSuxin/utility/gf"
)

func startCollectorScheduler(ctx context.Context) {
	if err := prepareCollectJobs(ctx); err != nil {
		g.Log().Warning(ctx, "CMS collection jobs:", err)
	}
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			work, cancel := context.WithTimeout(ctx, 5*time.Minute)
			if _, err := startAutoCollectJob(work, false); err != nil {
				g.Log().Warning(work, "CMS scheduled collection:", err)
			}
			cancel()
		}
	}()
}

type manualCollectResumeParams struct {
	SourceID            int64
	Page, TypeID, Hours int
	OnePage             bool
}

// Historical, explicitly stopped and automatic tasks are never resumed.
// A recent progress update permits long tasks without trusting stale records.
func manualCollectResumeParameters(snapshot row, now int64) (*manualCollectResumeParams, error) {
	if snapshot == nil || gconv.String(snapshot["mode"]) != "manual" || gconv.String(snapshot["status"]) != "running" || gconv.Int(snapshot["active_key"]) != 1 || gconv.Int(snapshot["source_count"]) != 1 || gconv.Int64(snapshot["source_id"]) <= 0 || gconv.Int(snapshot["cancel_requested"]) != 0 {
		return nil, nil
	}
	values := make(map[string]int64, 6)
	for _, key := range []string{"source_id", "page", "type_id", "hours", "one_page", "updated_at"} {
		value, err := strconv.ParseInt(strings.TrimSpace(gconv.String(snapshot[key])), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("采集恢复参数 %s 无效", key)
		}
		values[key] = value
	}
	if values["page"] < 0 || values["page"] > 10000 || values["type_id"] < 0 || values["type_id"] > 1<<31-1 || values["hours"] < 0 || values["hours"] > 720 || (values["one_page"] != 0 && values["one_page"] != 1) {
		return nil, fmt.Errorf("采集恢复参数超出有效范围")
	}
	updated := values["updated_at"]
	if updated <= 0 || updated > now || now-updated > 10*60 {
		return nil, fmt.Errorf("采集任务进度超过十分钟或更新时间无效")
	}
	return &manualCollectResumeParams{SourceID: values["source_id"], Page: int(max(int64(1), values["page"])), TypeID: int(values["type_id"]), Hours: int(values["hours"]), OnePage: values["one_page"] == 1}, nil
}

type AutoCollectReq struct {
	g.Meta `path:"/collect/auto" method:"post"`
}
type AutoCollectRes struct{}

func (*Admin) AutoCollect(ctx context.Context, _ *AutoCollectReq) (*AutoCollectRes, error) {
	job, err := startAutoCollectJob(ctx, true)
	if err != nil {
		return nil, err
	}
	g.RequestFromCtx(ctx).Response.WriteJson(gf.Success().SetData(job))
	return &AutoCollectRes{}, nil
}

func pushNewVodToBaidu(ctx context.Context, started int64) error {
	site := strings.TrimSpace(setting(ctx, "baidu_push_site", ""))
	token := strings.TrimSpace(setting(ctx, "baidu_push_token", ""))
	if site == "" || token == "" {
		return nil
	}
	for _, c := range site {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '-') {
			return fmt.Errorf("invalid Baidu site")
		}
	}
	vods, err := all(ctx, "SELECT id FROM sx_vod WHERE addtime>=? AND "+publicVodCondition(ctx, "")+" ORDER BY id DESC LIMIT 100", started)
	if err != nil || len(vods) == 0 {
		return err
	}
	prefix := "/suxinvideo/detail?id="
	if setting(ctx, "rewrite_enable", "0") == "1" {
		prefix = "/suxinvideo/detail/"
	}
	urls := make([]string, 0, len(vods))
	for _, vod := range vods {
		urls = append(urls, "https://"+site+prefix+gconv.String(vod["id"]))
	}
	endpoint := "http://data.zz.baidu.com/urls?" + url.Values{"site": {site}, "token": {token}}.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(strings.Join(urls, "\n")))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "text/plain")
	client := &http.Client{Timeout: 8 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 4096))
	if err != nil {
		return err
	}
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("Baidu submission returned status %d", response.StatusCode)
	}
	var result struct {
		Success int `json:"success"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return err
	}
	if result.Success < 1 {
		return fmt.Errorf("Baidu accepted no URLs")
	}
	return nil
}
