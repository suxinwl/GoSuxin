package suxinvideo

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/util/gconv"
	xq "github.com/suxinwl/GoSuxin/internal/xiaoqiapp"
)

type ImageReq struct {
	g.Meta `path:"/image" method:"get" noValApi:"1"`
	URL    string `p:"url"`
	Exp    int64  `p:"exp"`
	Sig    string `p:"sig"`
}
type ImageRes struct{}

func imageLink(ctx context.Context, raw string) string {
	// Serve external artwork through the signed proxy. Its URL validation and
	// transport share the TUN-aware public-host policy used by media requests.
	exp := time.Now().Add(24 * time.Hour).Unix()
	return "/suxinvideo/image?url=" + url.QueryEscape(raw) + "&exp=" + fmt.Sprint(exp) + "&sig=" + signProxy(ctx, raw, exp)
}

func isHEICURL(raw string) bool {
	address, err := url.Parse(raw)
	return err == nil && strings.HasSuffix(strings.ToLower(address.Path), ".heic")
}

func preparePictures(ctx context.Context, data map[string]any) {
	modify := func(v row) {
		pic := gconv.String(v["pic"])
		if strings.HasPrefix(pic, "http://") || strings.HasPrefix(pic, "https://") {
			v["pic"] = imageLink(ctx, pic)
		}
	}
	for _, key := range []string{"Vod", "Topic", "Feature"} {
		if single, ok := data[key].(row); ok {
			modify(single)
		}
	}
	for _, key := range []string{"Vods", "Topics", "Related", "Records", "Hot", "Recent", "Trending", "GuoguoRankingVods"} {
		if list, ok := data[key].([]row); ok {
			for _, item := range list {
				modify(item)
			}
		}
	}
	if blocks, ok := data["HomeBlocks"].([]map[string]any); ok {
		for _, block := range blocks {
			if list, ok := block["Vods"].([]row); ok {
				for _, item := range list {
					modify(item)
				}
			}
		}
	}
}
func imageMIME(data []byte) string {
	if len(data) > 3 && data[0] == 0xff && data[1] == 0xd8 && data[2] == 0xff {
		return "image/jpeg"
	}
	if len(data) > 8 && string(data[:8]) == "\x89PNG\r\n\x1a\n" {
		return "image/png"
	}
	if len(data) > 6 && (string(data[:6]) == "GIF87a" || string(data[:6]) == "GIF89a") {
		return "image/gif"
	}
	if len(data) > 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP" {
		return "image/webp"
	}
	return ""
}

func saveImageCache(path string, data []byte) bool {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return false
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".cover-*")
	if err != nil {
		return false
	}
	defer os.Remove(temporary.Name())
	if _, err = temporary.Write(data); err != nil {
		temporary.Close()
		return false
	}
	if err = temporary.Close(); err != nil {
		return false
	}
	if err = os.Rename(temporary.Name(), path); err != nil {
		cached, readErr := os.ReadFile(path)
		return readErr == nil && imageMIME(cached) != ""
	}
	return true
}
func localizeImage(ctx context.Context, raw string) string {
	if setting(ctx, "collect_img_local", "0") != "1" || !(strings.HasPrefix(raw, "https://") || strings.HasPrefix(raw, "http://")) {
		return raw
	}
	if err := safeCollectorURL(ctx, raw); err != nil {
		return raw
	}
	hash := sha256.Sum256([]byte(raw))
	name := hex.EncodeToString(hash[:])
	path := filepath.Join(cacheImageDir(), name)
	if data, err := os.ReadFile(path); err == nil && imageMIME(data) != "" {
		return "/suxinvideo/image/local?name=" + name
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return raw
	}
	response, err := safeCollectorHTTPClient(12 * time.Second).Do(request)
	if err != nil {
		return raw
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return raw
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, (5<<20)+1))
	if err != nil || len(data) > 5<<20 || imageMIME(data) == "" {
		return raw
	}
	// Parallel sources can share a cover URL. Publish a complete temporary
	// file atomically so another reader never observes a truncated cache.
	if !saveImageCache(path, data) {
		return raw
	}
	return "/suxinvideo/image/local?name=" + name
}
func (*Media) Image(ctx context.Context, req *ImageReq) (*ImageRes, error) {
	r := g.RequestFromCtx(ctx)
	if req.Exp < time.Now().Unix() || req.Exp > time.Now().Add(48*time.Hour).Unix() || !hmac.Equal([]byte(req.Sig), []byte(signProxy(ctx, req.URL, req.Exp))) {
		r.Response.WriteStatus(http.StatusForbidden)
		return &ImageRes{}, nil
	}
	safeErr := safeCollectorURL(ctx, req.URL)
	if safeErr != nil {
		badRequest(r, safeErr.Error())
		return &ImageRes{}, nil
	}
	hash := sha256.Sum256([]byte(req.URL))
	cache := filepath.Join("data", "cache", "suxinvideo", "img", hex.EncodeToString(hash[:]))
	data, err := os.ReadFile(cache)
	cached := err == nil && imageMIME(data) != ""
	if !cached {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, req.URL, nil)
		if err != nil {
			return nil, err
		}
		client := safeCollectorHTTPClient(12 * time.Second)
		response, err := client.Do(request)
		if err != nil {
			r.Response.WriteStatus(http.StatusBadGateway)
			return &ImageRes{}, nil
		}
		defer response.Body.Close()
		if response.StatusCode != 200 {
			r.Response.WriteStatus(http.StatusBadGateway)
			return &ImageRes{}, nil
		}
		limit := int64(5 << 20)
		if isHEICURL(req.URL) {
			limit = xq.MaxCMSCoverBytes
		}
		data, err = io.ReadAll(io.LimitReader(response.Body, limit+1))
		if err != nil {
			return nil, err
		}
		if int64(len(data)) > limit {
			r.Response.WriteStatus(http.StatusUnsupportedMediaType)
			return &ImageRes{}, nil
		}
		if xq.CMSIsHEICCover(data) {
			binary, binaryErr := ffmpegBinary()
			if binaryErr != nil {
				r.Response.WriteStatus(http.StatusServiceUnavailable)
				return &ImageRes{}, nil
			}
			data, err = xq.CMSConvertHEICCover(ctx, binary, data)
			if err != nil {
				r.Response.WriteStatus(http.StatusBadGateway)
				return &ImageRes{}, nil
			}
		}
		if imageMIME(data) == "" {
			r.Response.WriteStatus(http.StatusUnsupportedMediaType)
			return &ImageRes{}, nil
		}
		cached = saveImageCache(cache, data)
	}
	if cached && isHEICURL(req.URL) && imageMIME(data) == "image/jpeg" {
		_ = execSQL(ctx, "UPDATE sx_vod SET pic=? WHERE pic=?", "/suxinvideo/image/local?name="+hex.EncodeToString(hash[:]), req.URL)
	}
	r.Response.Header().Set("Content-Type", imageMIME(data))
	r.Response.Header().Set("Cache-Control", "public, max-age=86400")
	r.Response.Write(data)
	return &ImageRes{}, nil
}
