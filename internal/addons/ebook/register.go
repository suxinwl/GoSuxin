package ebook

import (
	"context"
	"fmt"
	"github.com/gofrs/flock"
	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/net/ghttp"
	"github.com/suxinwl/GoSuxin/internal/addons/ebook/controller/admin"
	"github.com/suxinwl/GoSuxin/internal/addons/ebook/controller/common"
	album "github.com/suxinwl/GoSuxin/internal/addons/ebook/logic"
	"github.com/suxinwl/GoSuxin/internal/plugins"
	"github.com/suxinwl/GoSuxin/utility/httpstream"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func init() {
	plugins.Register(plugins.Plugin{Name: "ebook", Title: "电子画册", Version: "1.0.0", Description: "分类、图片与 PDF 画册、翻页阅读、分享及存储设置。", Entry: "/albums-admin/manage", MenuRoots: []string{"albumCenter"}, Admin: []interface{}{admin.NewAlbum()}, Public: []interface{}{common.NewAlbumPublic()}, Bind: bindReader, Start: start})
}
func start(ctx context.Context, s *ghttp.Server) (func(), error) {
	// Keep the host installer available before database setup is complete.
	if os.Getenv("SUXIN_PLUGIN_PACKAGE") == "" {
		if _, err := os.Stat("devsource/developer/install/install.lock"); os.IsNotExist(err) {
			return func() {}, nil
		}
	}
	if g.DB().GetConfig().Prefix != "gf_" {
		return nil, fmt.Errorf("电子画册 V1.0.0 需要 gf_ 表前缀")
	}
	if err := os.MkdirAll("storage", 0700); err != nil {
		return nil, err
	}
	lock := flock.New("storage/album-worker.lock")
	ok, err := lock.TryLock()
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("画册工作进程已在运行")
	}
	if err = album.Initialize(ctx); err != nil {
		_ = lock.Unlock()
		return nil, err
	}
	workerCtx, cancel := context.WithCancel(ctx)
	finished := make(chan struct{})
	go func() { defer close(finished); album.RunAssetDeleteWorker(workerCtx) }()
	return func() { cancel(); <-finished; _ = lock.Unlock() }, nil
}

// Bind once at host startup; installations only start or stop the worker.
func bindReader(s *ghttp.Server) {
	reader := "./resource/plugins/ebook/reader"
	if dir := os.Getenv("SUXIN_PLUGIN_PACKAGE"); dir != "" {
		reader = filepath.Join(dir, "public", "reader")
	}
	assets := http.StripPrefix("/albums/assets/", http.FileServer(http.Dir(filepath.Join(reader, "assets"))))
	for _, method := range []string{"GET:", "HEAD:"} {
		s.BindHandler(method+"/albums/assets/*path", plugins.GuardHandler("ebook", func(r *ghttp.Request) {
			request := r.Request.Clone(r.Context())
			request.URL.Path = strings.TrimSuffix(r.URL.Path, "/")
			assets.ServeHTTP(httpstream.New(r.Response), request)
		}))
		s.BindHandler(method+"/*path", plugins.GuardHandler("ebook", album.ServeReader))
		s.BindHandler(method+"/common/album/site/logo/file", plugins.GuardHandler("ebook", album.ServeSiteLogo))
		s.BindHandler(method+"/common/album/file", plugins.GuardHandler("ebook", album.ServeAlbumFile))
		s.BindHandler(method+"/common/album/category-cover", plugins.GuardHandler("ebook", album.ServeCategoryCover))
	}
}
