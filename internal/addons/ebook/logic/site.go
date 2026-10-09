package album

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/net/ghttp"
	_ "golang.org/x/image/webp"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

const siteConfigFile = "storage/config/album-site.json"

var siteMu sync.Mutex
var logoID = regexp.MustCompile(`^[a-f0-9]{48}\.(png|jpg|webp)$`)

type SiteBrand struct {
	Name        string `json:"name"`
	Title       string `json:"title"`
	Headline    string `json:"headline"`
	Description string `json:"description"`
	Copyright   string `json:"copyright"`
	Website     string `json:"website"`
	Logo        string `json:"logo"`
}
type SiteConfig struct {
	ChineseURL string    `json:"chineseURL"`
	EnglishURL string    `json:"englishURL"`
	Chinese    SiteBrand `json:"chinese"`
	English    SiteBrand `json:"english"`
}

func DefaultSiteConfig() SiteConfig {
	return SiteConfig{ChineseURL: "/albums/", EnglishURL: "/albums-en/", Chinese: SiteBrand{Name: "电子画册", Title: "电子画册", Headline: "随时随地，翻阅精彩", Description: "浏览产品资料与品牌画册。"}, English: SiteBrand{Name: "Digital Catalogues", Title: "Digital Catalogues", Headline: "Discover our catalogues", Description: "Explore products and brand stories, wherever you are."}}
}
func SiteSettings() (SiteConfig, error) {
	siteMu.Lock()
	defer siteMu.Unlock()
	config := DefaultSiteConfig()
	b, err := os.ReadFile(siteConfigFile)
	if os.IsNotExist(err) {
		return config, nil
	}
	if err != nil {
		return config, err
	}
	if err = json.Unmarshal(b, &config); err != nil {
		return config, fmt.Errorf("站点配置读取失败: %w", err)
	}
	return config, nil
}
func siteURL(raw string, entry bool) (string, error) {
	raw = strings.TrimSpace(raw)
	if len(raw) > 2048 {
		return "", errors.New("网站或图片地址不能超过 2048 字节")
	}
	if raw == "" && !entry {
		return "", nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Fragment != "" || strings.ContainsAny(raw, "\\\r\n") {
		return "", errors.New("请输入有效的网站地址")
	}
	if u.IsAbs() {
		if (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" {
			return "", errors.New("网站地址只支持 http 或 https")
		}
	} else if !strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "//") {
		return "", errors.New("请输入完整网址或以 / 开头的站内路径")
	}
	if entry {
		if u.RawQuery != "" || u.RawPath != "" {
			return "", errors.New("画册入口不能包含查询参数、编码路径或井号")
		}
		if u.Path == "" {
			u.Path = "/"
		}
		if path.Clean(u.Path) != strings.TrimSuffix(u.Path, "/") && u.Path != "/" {
			return "", errors.New("画册路径不能包含点段或重复斜线")
		}
		for _, reserved := range []string{"/admin", "/common", "/suxinweb", "/suxinvideo", "/resource", "/install", "/wxapp", "/albums/assets"} {
			if strings.EqualFold(u.Path, reserved) || strings.HasPrefix(strings.ToLower(u.Path), reserved+"/") {
				return "", errors.New("画册路径与系统保留路径冲突")
			}
		}
		u.Path = strings.TrimRight(u.Path, "/") + "/"
	}
	return u.String(), nil
}
func ValidateSite(config SiteConfig) (SiteConfig, error) {
	var err error
	if config.ChineseURL, err = siteURL(config.ChineseURL, true); err != nil {
		return config, err
	}
	if config.EnglishURL, err = siteURL(config.EnglishURL, true); err != nil {
		return config, err
	}
	if strings.EqualFold(config.ChineseURL, config.EnglishURL) {
		return config, errors.New("中英文画册入口不能相同")
	}
	for _, b := range []*SiteBrand{&config.Chinese, &config.English} {
		b.Name = strings.TrimSpace(b.Name)
		b.Title = strings.TrimSpace(b.Title)
		if b.Name == "" || len([]rune(b.Name)) > 80 || len([]rune(b.Title)) > 120 || len([]rune(b.Headline)) > 200 || len([]rune(b.Description)) > 1000 || len([]rune(b.Copyright)) > 300 {
			return config, errors.New("请填写品牌名称，并检查文案长度")
		}
		if b.Title == "" {
			b.Title = b.Name
		}
		if b.Website, err = siteURL(b.Website, false); err != nil {
			return config, err
		}
		if b.Logo, err = siteURL(b.Logo, false); err != nil {
			return config, err
		}
	}
	return config, nil
}
func SaveSiteSettings(config SiteConfig) error {
	config, err := ValidateSite(config)
	if err != nil {
		return err
	}
	siteMu.Lock()
	defer siteMu.Unlock()
	if err = os.MkdirAll(filepath.Dir(siteConfigFile), 0700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(siteConfigFile), "site-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, err = f.Write(b)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), siteConfigFile)
}
func entryMatches(raw, host, requestPath string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	if u.Host != "" && !strings.EqualFold(u.Host, host) {
		return false
	}
	return strings.TrimRight(u.Path, "/") == strings.TrimRight(requestPath, "/")
}
func configuredLanguage(config SiteConfig, host, requestPath string) string {
	// Prefer explicitly configured domains over host-relative paths.
	for _, s := range []struct{ raw, lang string }{{config.EnglishURL, "en"}, {config.ChineseURL, "zh"}} {
		u, _ := url.Parse(s.raw)
		if u != nil && u.Host != "" && entryMatches(s.raw, host, requestPath) {
			return s.lang
		}
	}
	if entryMatches(config.EnglishURL, host, requestPath) {
		return "en"
	}
	return "zh"
}
func PublicSite(ctx context.Context) (g.Map, error) {
	config, err := SiteSettings()
	if err != nil {
		return nil, err
	}
	lang := SiteLanguage(ctx)
	brand := config.Chinese
	if lang == "en" {
		brand = config.English
	}
	// Both configured paths are public deployment metadata, not credentials.
	return g.Map{"language": lang, "branding": brand, "sites": g.Map{"zh": config.ChineseURL, "en": config.EnglishURL}}, nil
}
func ServeReader(r *ghttp.Request) {
	config, err := SiteSettings()
	if err != nil {
		r.Response.WriteStatus(500, "站点配置不可用")
		return
	}
	requestPath := r.URL.Path
	if !entryMatches(config.ChineseURL, r.Host, requestPath) && !entryMatches(config.EnglishURL, r.Host, requestPath) {
		r.Response.WriteStatus(404)
		return
	}
	r.Response.Header().Set("Cache-Control", "no-store")
	reader := "resource/plugins/ebook/reader/index.html"
	if dir := os.Getenv("SUXIN_PLUGIN_PACKAGE"); dir != "" {
		reader = filepath.Join(dir, "public/reader/index.html")
	}
	r.Response.ServeFile(reader)
}
func SaveSiteLogo(file *ghttp.UploadFile) (string, error) {
	if file == nil || file.Size > 5<<20 {
		return "", errors.New("请选择不超过 5 MB 的 PNG、JPG 或 WEBP 图片")
	}
	in, err := file.Open()
	if err != nil {
		return "", err
	}
	defer in.Close()
	conf, format, err := image.DecodeConfig(in)
	if err != nil || conf.Width < 1 || conf.Height < 1 || conf.Width > 8192 || conf.Height > 8192 {
		return "", errors.New("Logo 图片格式或尺寸不合法")
	}
	ext := map[string]string{"png": "png", "jpeg": "jpg", "webp": "webp"}[format]
	if ext == "" {
		return "", errors.New("Logo 仅支持 PNG、JPG、WEBP")
	}
	if _, err = in.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	id, err := NewKey()
	if err != nil {
		return "", err
	}
	id += "." + ext
	dir := "storage/album-brand"
	if err = os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	out, err := os.OpenFile(filepath.Join(dir, id), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", err
	}
	n, err := io.Copy(out, io.LimitReader(in, (5<<20)+1))
	closeErr := out.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil || n > 5<<20 {
		_ = os.Remove(filepath.Join(dir, id))
		return "", errors.New("Logo 保存失败或文件过大")
	}
	return "/common/album/site/logo/file?id=" + id, nil
}
func ServeSiteLogo(r *ghttp.Request) {
	id := r.Get("id").String()
	if !logoID.MatchString(id) {
		r.Response.WriteStatus(404)
		return
	}
	r.Response.Header().Set("X-Content-Type-Options", "nosniff")
	r.Response.Header().Set("Cache-Control", "public, max-age=86400")
	r.Response.ServeFile(filepath.Join("storage/album-brand", id))
}
