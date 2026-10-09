package suxinvideo

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/utility/gf"
)

type InstallThemeReq struct {
	g.Meta `path:"/installTheme" method:"post"`
}
type InstallThemeRes struct{}

type themeManifest struct {
	Code    string `json:"code"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	Version string `json:"version"`
	Author  string `json:"author"`
}

func (*Admin) InstallTheme(ctx context.Context, _ *InstallThemeReq) (*InstallThemeRes, error) {
	r := g.RequestFromCtx(ctx)
	file, _, err := r.Request.FormFile("file")
	if err != nil {
		badRequest(r, "请选择主题 ZIP 文件")
		return &InstallThemeRes{}, nil
	}
	defer file.Close()
	payload, err := io.ReadAll(io.LimitReader(file, (10<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(payload) > 10<<20 {
		badRequest(r, "主题包超过 10MB")
		return &InstallThemeRes{}, nil
	}
	archive, err := zip.NewReader(bytes.NewReader(payload), int64(len(payload)))
	if err != nil {
		badRequest(r, "ZIP 文件无效")
		return &InstallThemeRes{}, nil
	}
	base := ""
	manifestData := []byte(nil)
	for _, entry := range archive.File {
		if entry.Name == "manifest.json" {
			reader, e := entry.Open()
			if e != nil {
				return nil, e
			}
			manifestData, e = io.ReadAll(io.LimitReader(reader, 16<<10))
			reader.Close()
			if e != nil {
				return nil, e
			}
			break
		}
	}
	if len(manifestData) == 0 {
		for _, entry := range archive.File {
			if strings.HasSuffix(entry.Name, "/manifest.json") && strings.Count(entry.Name, "/") == 1 {
				base = strings.TrimSuffix(entry.Name, "manifest.json")
				reader, e := entry.Open()
				if e != nil {
					return nil, e
				}
				manifestData, e = io.ReadAll(io.LimitReader(reader, 16<<10))
				reader.Close()
				if e != nil {
					return nil, e
				}
				break
			}
		}
	}
	var manifest themeManifest
	if json.Unmarshal(manifestData, &manifest) != nil || manifest.Type != "template" || cleanTheme(manifest.Code) != manifest.Code {
		badRequest(r, "主题清单无效")
		return &InstallThemeRes{}, nil
	}
	if builtInTheme(manifest.Code) {
		badRequest(r, "不能覆盖内置主题")
		return &InstallThemeRes{}, nil
	}
	dest := filepath.Join("resource", "static", "suxinvideo", "themes", manifest.Code)
	if _, err := os.Stat(dest); err == nil {
		badRequest(r, "主题已存在")
		return &InstallThemeRes{}, nil
	}
	allowed := map[string]bool{".css": true, ".js": true, ".svg": true, ".png": true, ".jpg": true, ".jpeg": true, ".webp": true, ".gif": true, ".woff2": true}
	contents := map[string][]byte{}
	hasCSS := false
	total := int64(0)
	for _, entry := range archive.File {
		name := strings.TrimPrefix(entry.Name, base)
		if name == "" || strings.HasSuffix(name, "/") {
			continue
		}
		if strings.Contains(name, "\\") || strings.HasPrefix(name, "/") || strings.Contains(name, "..") || path.Clean(name) != name || entry.Mode()&os.ModeSymlink != 0 {
			badRequest(r, "ZIP 包含非法路径")
			return &InstallThemeRes{}, nil
		}
		if name != "manifest.json" && (!strings.HasPrefix(name, "static/") || !allowed[strings.ToLower(path.Ext(name))]) {
			badRequest(r, "主题包含不允许的文件类型")
			return &InstallThemeRes{}, nil
		}
		if entry.UncompressedSize64 > 4<<20 {
			badRequest(r, "主题单个文件过大")
			return &InstallThemeRes{}, nil
		}
		reader, e := entry.Open()
		if e != nil {
			return nil, e
		}
		data, e := io.ReadAll(io.LimitReader(reader, (4<<20)+1))
		reader.Close()
		if e != nil {
			return nil, e
		}
		if len(data) > 4<<20 {
			badRequest(r, "主题单个文件过大")
			return &InstallThemeRes{}, nil
		}
		total += int64(len(data))
		if total > 20<<20 {
			badRequest(r, "解压后主题过大")
			return &InstallThemeRes{}, nil
		}
		contents[name] = data
		if name == "static/css/main.css" {
			hasCSS = true
		}
	}
	if !hasCSS {
		badRequest(r, "主题缺少 static/css/main.css")
		return &InstallThemeRes{}, nil
	}
	if err := os.MkdirAll(dest, 0755); err != nil {
		return nil, err
	}
	for name, data := range contents {
		target := filepath.Join(dest, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(target, data, 0644); err != nil {
			return nil, err
		}
	}
	if manifest.Name == "" {
		manifest.Name = manifest.Code
	}
	if manifest.Version == "" {
		manifest.Version = "1.0"
	}
	if err := execSQL(ctx, "INSERT INTO sx_plugin(code,name,type,version,author,status,expire) VALUES(?,?,'template',?,?,1,0) ON DUPLICATE KEY UPDATE name=VALUES(name),version=VALUES(version),author=VALUES(author),status=1", manifest.Code, cutRunes(manifest.Name, 60), cutRunes(manifest.Version, 20), cutRunes(manifest.Author, 60)); err != nil {
		return nil, err
	}
	r.Response.WriteJson(gf.Success().SetData(map[string]any{"code": manifest.Code, "name": manifest.Name}))
	return &InstallThemeRes{}, nil
}

type UninstallThemeReq struct {
	g.Meta `path:"/uninstallTheme" method:"post"`
	Code   string `p:"code"`
}
type UninstallThemeRes struct{}

func (*Admin) UninstallTheme(ctx context.Context, req *UninstallThemeReq) (*UninstallThemeRes, error) {
	r := g.RequestFromCtx(ctx)
	if cleanTheme(req.Code) != req.Code {
		badRequest(r, "主题代码无效")
		return &UninstallThemeRes{}, nil
	}
	if builtInTheme(req.Code) {
		badRequest(r, "不能卸载内置主题")
		return &UninstallThemeRes{}, nil
	}
	if setting(ctx, "site_template", "suxinlite") == req.Code {
		badRequest(r, "请先切换主题")
		return &UninstallThemeRes{}, nil
	}
	item, err := one(ctx, "SELECT id FROM sx_plugin WHERE code=? AND type='template'", req.Code)
	if err != nil {
		return nil, err
	}
	if item == nil {
		badRequest(r, "主题未安装")
		return &UninstallThemeRes{}, nil
	}
	dest := filepath.Join("resource", "static", "suxinvideo", "themes", req.Code)
	root, err := filepath.Abs(filepath.Join("resource", "static", "suxinvideo", "themes"))
	if err != nil {
		return nil, err
	}
	absolute, err := filepath.Abs(dest)
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(absolute, root+string(os.PathSeparator)) {
		return nil, errors.New("主题路径非法")
	}
	if err := os.RemoveAll(absolute); err != nil {
		return nil, err
	}
	if err := execSQL(ctx, "DELETE FROM sx_plugin WHERE code=? AND type='template'", req.Code); err != nil {
		return nil, err
	}
	r.Response.WriteJson(gf.Success().SetData(true))
	return &UninstallThemeRes{}, nil
}
