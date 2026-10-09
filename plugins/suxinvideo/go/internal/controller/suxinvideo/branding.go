package suxinvideo

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/util/gconv"
	_ "golang.org/x/image/webp"
)

type BrandReq struct {
	g.Meta `path:"/brand" method:"get" noValApi:"1"`
	File   string `p:"file"`
}
type BrandRes struct{}

var brandFilePattern = regexp.MustCompile(`^[a-f0-9]{32}\.(png|jpg|webp)$`)

const defaultLogoURL = "/suxinvideo/brand?file=app-icon.png"
const defaultFaviconURL = "/suxinvideo/brand?file=favicon.ico"
const defaultAvatarURL = "/suxinvideo/brand?file=default-avatar.png"

// These bundled names are a closed list. Uploaded branding continues to use
// random filenames, and this route cannot read arbitrary static files.
var bundledBrandFiles = map[string]string{
	"app-icon.png": "image/png", "xiaoqi-logo.png": "image/png",
	"default-avatar.png": "image/png", "favicon.ico": "image/x-icon",
}

func validBrandFile(name string) bool {
	_, bundled := bundledBrandFiles[name]
	return bundled || brandFilePattern.MatchString(name)
}

func brandDirectory() string { return filepath.Join("data", "suxinvideo", "branding") }

func validBrandURL(raw string) bool {
	if raw == "" {
		return true
	}
	u, err := url.Parse(raw)
	if err != nil || u.Fragment != "" || u.User != nil {
		return false
	}
	if u.IsAbs() {
		return u.Scheme == "https" && u.Hostname() != "" && len(raw) <= 500
	}
	return u.Path == "/suxinvideo/brand" && u.RawPath == "" && validBrandFile(u.Query().Get("file")) && len(u.Query()) == 1 && len(u.Query()["file"]) == 1
}

func brandImageFormat(data []byte) (string, string, error) {
	if len(data) == 0 || len(data) > 2<<20 {
		return "", "", errors.New("图片不得超过 2MB")
	}
	extensions := map[string]string{"image/png": "png", "image/jpeg": "jpg", "image/webp": "webp"}
	mime := imageMIME(data)
	extension := extensions[mime]
	if extension == "" {
		return "", "", errors.New("仅支持 PNG、JPEG 和 WebP 图片")
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || format != extension && !(format == "jpeg" && extension == "jpg") {
		return "", "", errors.New("图片内容无效")
	}
	if config.Width < 1 || config.Height < 1 || config.Width > 2048 || config.Height > 2048 {
		return "", "", errors.New("图片尺寸须在 2048×2048 以内")
	}
	return extension, mime, nil
}

func saveBrandUpload(ctx context.Context) (string, string, error) {
	r := g.RequestFromCtx(ctx)
	kind := r.Request.FormValue("kind")
	if kind != "site_logo" && kind != "site_favicon" && kind != "user_default_avatar" {
		return "", "", errors.New("图标类型无效")
	}
	file, _, err := r.Request.FormFile("file")
	if err != nil {
		return "", "", errors.New("请选择图片文件")
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, (2<<20)+1))
	if err != nil {
		return "", "", err
	}
	extension, _, err := brandImageFormat(data)
	if err != nil {
		return "", "", err
	}
	if err = os.MkdirAll(brandDirectory(), 0755); err != nil {
		return "", "", err
	}
	var random [16]byte
	if _, err = rand.Read(random[:]); err != nil {
		return "", "", err
	}
	name := hex.EncodeToString(random[:]) + "." + extension
	path := filepath.Join(brandDirectory(), name)
	dest, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return "", "", err
	}
	_, writeErr := dest.Write(data)
	closeErr := dest.Close()
	if writeErr != nil || closeErr != nil {
		_ = os.Remove(path)
		return "", "", errors.New("保存图片失败")
	}
	address := "/suxinvideo/brand?file=" + name
	if err = execSQL(ctx, "INSERT INTO sx_config(`key`,`value`) VALUES(?,?) ON DUPLICATE KEY UPDATE `value`=VALUES(`value`)", kind, address); err != nil {
		_ = os.Remove(path)
		return "", "", err
	}
	return address, kind, nil
}

func (*Media) Brand(ctx context.Context, req *BrandReq) (*BrandRes, error) {
	r := g.RequestFromCtx(ctx)
	if !validBrandFile(req.File) {
		r.Response.WriteStatus(http.StatusNotFound)
		return &BrandRes{}, nil
	}
	directory := brandDirectory()
	bundledMIME, bundled := bundledBrandFiles[req.File]
	if bundled {
		directory = filepath.Join("resource", "static", "suxinvideo", "branding")
	}
	data, err := os.ReadFile(filepath.Join(directory, req.File))
	if err != nil || len(data) > 2<<20 {
		r.Response.WriteStatus(http.StatusNotFound)
		return &BrandRes{}, nil
	}
	mime := bundledMIME
	if !bundled {
		_, mime, err = brandImageFormat(data)
	}
	if err != nil {
		r.Response.WriteStatus(http.StatusNotFound)
		return &BrandRes{}, nil
	}
	r.Response.Header().Set("Content-Type", mime)
	r.Response.Header().Set("X-Content-Type-Options", "nosniff")
	if bundled {
		sum := sha256.Sum256(data)
		etag := `"` + hex.EncodeToString(sum[:]) + `"`
		r.Response.Header().Set("Cache-Control", "public, max-age=0, must-revalidate")
		r.Response.Header().Set("ETag", etag)
		if r.Header.Get("If-None-Match") == etag {
			r.Response.WriteStatus(http.StatusNotModified)
			return &BrandRes{}, nil
		}
	} else {
		r.Response.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	r.Response.Write(data)
	return &BrandRes{}, nil
}

func brandSetting(ctx context.Context, key string) string {
	value := strings.TrimSpace(setting(ctx, key, ""))
	if value != "" && validBrandURL(value) {
		return value
	}
	return defaultBrandSetting(key)
}

func defaultBrandSetting(key string) string {
	switch key {
	case "site_logo":
		return defaultLogoURL
	case "site_favicon":
		return defaultFaviconURL
	case "user_default_avatar":
		return defaultAvatarURL
	}
	return ""
}

func applyDefaultUserAvatar(ctx context.Context, user row) row {
	if user != nil && strings.TrimSpace(gconv.String(user["avatar"])) == "" {
		user["avatar"] = brandSetting(ctx, "user_default_avatar")
	}
	return user
}
