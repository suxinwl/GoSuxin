package suxinvideo

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/internal/mediastream"
	xq "github.com/suxinwl/GoSuxin/internal/xiaoqiapp"
)

type NativeHLSReq struct {
	g.Meta  `path:"/native/hls" method:"get" noValApi:"1"`
	Token   string `p:"token"`
	Segment string `p:"segment"`
}
type NativeHLSRes struct{}
type appHLSRun struct {
	stream  *mediastream.Session
	expires time.Time
}

var appHLSRuns = struct {
	sync.Mutex
	items map[string]appHLSRun
}{items: map[string]appHLSRun{}}

func appMediaGrantLink(ctx context.Context, link string) string {
	r := g.RequestFromCtx(ctx)
	if r == nil {
		return link
	}
	grant := r.Get("app_grant").String()
	downloadKey := r.Get("download_key").String()
	if grant == "" && downloadKey == "" {
		return link
	}
	u, err := url.Parse(link)
	if err != nil {
		return link
	}
	q := u.Query()
	if grant != "" {
		q.Set("app_grant", grant)
	}
	if downloadKey != "" {
		q.Set("download_key", downloadKey)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func appNativeHLSURL(ctx context.Context, token string, media xq.CMSMedia, expires time.Time) (string, error) {
	if len(media.Key) != 16 || media.Duration <= 0 {
		return "", errors.New("片源未提供有效时长，无法生成原生HLS")
	}
	if err := safeCollectorURL(ctx, media.URL); err != nil {
		return "", err
	}
	return appMediaGrantLink(ctx, "/suxinvideo/native/hls?token="+url.QueryEscape(token)), nil
}

func appHLSStream(token string, item nativeSession, background bool) (*mediastream.Session, error) {
	appHLSRuns.Lock()
	defer appHLSRuns.Unlock()
	now := time.Now()
	for key, run := range appHLSRuns.items {
		if now.After(run.expires) {
			delete(appHLSRuns.items, key)
			go run.stream.Close()
		}
	}
	if run, ok := appHLSRuns.items[token]; ok {
		return run.stream, nil
	}
	if len(appHLSRuns.items) >= 200 {
		return nil, errors.New("原生播放会话已满，请稍后重试")
	}
	binary, err := ffmpegBinary()
	if err != nil {
		return nil, errors.New("服务器缺少FFmpeg")
	}
	stream, err := mediastream.New(mediastream.Config{URL: item.Media.URL, Referer: item.Media.Referer, Headers: item.Headers, Key: item.Media.Key, Duration: item.Media.Duration, Binary: binary, Background: background})
	if err != nil {
		return nil, err
	}
	appHLSRuns.items[token] = appHLSRun{stream: stream, expires: item.Expires}
	time.AfterFunc(time.Until(item.Expires), func() {
		appHLSRuns.Lock()
		run, found := appHLSRuns.items[token]
		if found {
			delete(appHLSRuns.items, token)
		}
		appHLSRuns.Unlock()
		if found {
			run.stream.Close()
		}
	})
	return stream, nil
}

func (*Media) NativeHLS(ctx context.Context, req *NativeHLSReq) (*NativeHLSRes, error) {
	r := g.RequestFromCtx(ctx)
	item, err := getNativeSession(ctx, req.Token)
	if err != nil || len(item.Media.Key) != 16 {
		r.Response.WriteStatus(http.StatusForbidden, "播放授权已失效")
		r.ExitAll()
		return &NativeHLSRes{}, nil
	}
	stream, err := appHLSStream(req.Token, item, item.Background || r.Header.Get("X-Xiaoqi-Download") == "1")
	if err != nil {
		r.Response.WriteStatus(http.StatusServiceUnavailable, err.Error())
		r.ExitAll()
		return &NativeHLSRes{}, nil
	}
	r.Response.Header().Set("Cache-Control", "private, no-store")
	r.Response.Header().Set("X-Content-Type-Options", "nosniff")
	if req.Segment == "" {
		r.Response.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		r.Response.Write(stream.Manifest(func(index int) string {
			return appMediaGrantLink(ctx, "/suxinvideo/native/hls?token="+url.QueryEscape(req.Token)+"&segment="+strconv.Itoa(index))
		}))
	} else {
		index, parseErr := strconv.Atoi(req.Segment)
		if parseErr != nil || index < 0 || index >= stream.Count() {
			r.Response.WriteStatus(http.StatusNotFound)
			r.ExitAll()
			return &NativeHLSRes{}, nil
		}
		body, readErr := stream.Segment(ctx, index)
		if readErr != nil {
			if ctx.Err() == nil {
				r.Response.WriteStatus(http.StatusBadGateway, "分片加载失败，请重试或换源")
			}
			r.ExitAll()
			return &NativeHLSRes{}, nil
		}
		r.Response.Header().Set("Content-Type", "video/mp2t")
		r.Response.Write(body)
	}
	r.ExitAll()
	return &NativeHLSRes{}, nil
}
