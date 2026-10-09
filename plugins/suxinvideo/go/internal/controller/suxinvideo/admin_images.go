package suxinvideo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/utility/gf"
)

type ImagesReq struct {
	g.Meta  `path:"/images" method:"get"`
	Page    int `p:"page"`
	PerPage int `p:"per_page"`
}
type ImagesRes struct{}
type DeleteImagesReq struct {
	g.Meta `path:"/images/delete" method:"post"`
	Names  []string `p:"names"`
}
type DeleteImagesRes struct{}
type CleanImagesReq struct {
	g.Meta `path:"/images/clean" method:"post"`
}
type CleanImagesRes struct{}
type LocalImageReq struct {
	g.Meta `path:"/image/local" method:"get" noValApi:"1"`
	Name   string `p:"name"`
}
type LocalImageRes struct{}

var imageCacheName = regexp.MustCompile(`^[a-f0-9]{64}$`)

func cacheImageDir() string { return filepath.Join("data", "cache", "suxinvideo", "img") }
func imageReferences(ctx context.Context) (map[string]bool, error) {
	used := map[string]bool{}
	for _, table := range []string{"sx_vod", "sx_slide"} {
		rows, err := all(ctx, "SELECT pic FROM "+table+" WHERE pic<>''")
		if err != nil {
			return nil, err
		}
		for _, item := range rows {
			raw := strings.TrimSpace(fmt.Sprint(item["pic"]))
			if strings.HasPrefix(raw, "https://") || strings.HasPrefix(raw, "http://") {
				hash := sha256.Sum256([]byte(raw))
				used[hex.EncodeToString(hash[:])] = true
			} else if strings.HasPrefix(raw, "/suxinvideo/image/local?name=") {
				name := strings.TrimPrefix(raw, "/suxinvideo/image/local?name=")
				if imageCacheName.MatchString(name) {
					used[name] = true
				}
			}
		}
	}
	return used, nil
}
func listCachedImages() (map[string]os.FileInfo, error) {
	result := map[string]os.FileInfo{}
	entries, err := os.ReadDir(cacheImageDir())
	if os.IsNotExist(err) {
		return result, nil
	}
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if !imageCacheName.MatchString(entry.Name()) || !entry.Type().IsRegular() {
			continue
		}
		info, e := entry.Info()
		if e == nil {
			result[entry.Name()] = info
		}
	}
	return result, nil
}

func (*Admin) Images(ctx context.Context, req *ImagesReq) (*ImagesRes, error) {
	r := g.RequestFromCtx(ctx)
	page := clampPage(req.Page)
	per := req.PerPage
	if per != 50 && per != 100 && per != 200 && per != 500 && per != 1000 {
		per = 50
	}
	files, err := listCachedImages()
	if err != nil {
		return nil, err
	}
	used, err := imageReferences(ctx)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(files))
	var totalSize int64
	orphan := 0
	for name, info := range files {
		names = append(names, name)
		totalSize += info.Size()
		if !used[name] {
			orphan++
		}
	}
	sort.Slice(names, func(i, j int) bool { return files[names[i]].ModTime().After(files[names[j]].ModTime()) })
	start := (page - 1) * per
	if start > len(names) {
		start = len(names)
	}
	end := min(len(names), start+per)
	list := make([]map[string]any, 0, end-start)
	for _, name := range names[start:end] {
		info := files[name]
		list = append(list, map[string]any{"name": name, "url": "/suxinvideo/image/local?name=" + name, "size": info.Size(), "time": info.ModTime().Unix(), "used": used[name]})
	}
	r.Response.WriteJson(gf.Success().SetData(map[string]any{"list": list, "total": len(names), "size": totalSize, "orphan": orphan}))
	return &ImagesRes{}, nil
}
func (*Admin) DeleteImages(ctx context.Context, req *DeleteImagesReq) (*DeleteImagesRes, error) {
	r := g.RequestFromCtx(ctx)
	if len(req.Names) < 1 || len(req.Names) > 1000 {
		badRequest(r, "图片数量无效")
		return &DeleteImagesRes{}, nil
	}
	for _, name := range req.Names {
		if !imageCacheName.MatchString(name) {
			badRequest(r, "图片名称无效")
			return &DeleteImagesRes{}, nil
		}
	}
	for _, name := range req.Names {
		if err := os.Remove(filepath.Join(cacheImageDir(), name)); err != nil && !os.IsNotExist(err) {
			return nil, err
		}
	}
	r.Response.WriteJson(gf.Success().SetData(true))
	return &DeleteImagesRes{}, nil
}
func (*Admin) CleanImages(ctx context.Context, _ *CleanImagesReq) (*CleanImagesRes, error) {
	used, err := imageReferences(ctx)
	if err != nil {
		return nil, err
	}
	files, err := listCachedImages()
	if err != nil {
		return nil, err
	}
	count := 0
	for name := range files {
		if !used[name] {
			if err = os.Remove(filepath.Join(cacheImageDir(), name)); err != nil && !os.IsNotExist(err) {
				return nil, err
			}
			count++
		}
	}
	g.RequestFromCtx(ctx).Response.WriteJson(gf.Success().SetData(map[string]any{"deleted": count}))
	return &CleanImagesRes{}, nil
}
func (*Media) LocalImage(ctx context.Context, req *LocalImageReq) (*LocalImageRes, error) {
	r := g.RequestFromCtx(ctx)
	if !imageCacheName.MatchString(req.Name) {
		r.Response.WriteStatus(http.StatusNotFound)
		return &LocalImageRes{}, nil
	}
	data, err := os.ReadFile(filepath.Join(cacheImageDir(), req.Name))
	if err != nil || imageMIME(data) == "" {
		r.Response.WriteStatus(http.StatusNotFound)
		return &LocalImageRes{}, nil
	}
	r.Response.Header().Set("Content-Type", imageMIME(data))
	r.Response.Header().Set("Cache-Control", "public, max-age=86400")
	r.Response.Header().Set("Last-Modified", time.Now().UTC().Format(http.TimeFormat))
	r.Response.Write(data)
	return &LocalImageRes{}, nil
}
