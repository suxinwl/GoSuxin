package album

import (
	"context"
	"errors"
	"net"
	"net/url"
	"os"
	"strings"

	"github.com/suxinwl/GoSuxin/framework/frame/g"
)

var ChineseDomain = configuredHost("SUXIN_ALBUM_ZH_HOST", "albums.localhost")
var EnglishDomain = configuredHost("SUXIN_ALBUM_EN_HOST", "en.albums.localhost")

func NormalizeLanguage(language string) string {
	if language == "en" {
		return "en"
	}
	return "zh"
}

// Use the actual request Host forwarded by our Nginx, never a client language
// query parameter or an arbitrary X-Forwarded-Host header.
func LanguageForHost(host string) string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	if strings.EqualFold(strings.TrimSuffix(host, "."), EnglishDomain) {
		return "en"
	}
	return "zh"
}
func SiteLanguage(ctx context.Context) string {
	if r := g.RequestFromCtx(ctx); r != nil {
		config, err := SiteSettings()
		if err == nil {
			entry := r.GetQuery("site").String()
			if entry == "" {
				entry = r.Header.Get("X-Album-Site")
			}
			if entry != "" {
				return configuredLanguage(config, r.Host, entry)
			}
			for _, site := range []struct{ raw, lang string }{{config.ChineseURL, "zh"}, {config.EnglishURL, "en"}} {
				u, _ := url.Parse(site.raw)
				if u != nil && u.Host != "" && strings.EqualFold(u.Host, r.Host) {
					return site.lang
				}
			}
		}
		return LanguageForHost(r.Host)
	}
	return "zh"
}
func targetForLanguage(target StorageProfile, language string) (StorageProfile, error) {
	if target.ID == "" {
		return target, nil
	}
	if language == "en" {
		if target.EnglishParentID == nil || *target.EnglishParentID == target.ParentID {
			return StorageProfile{}, errors.New("请配置并验证不同于中文目录的英文目标目录 ID")
		}
		target.ParentID = *target.EnglishParentID
	}
	return target, nil
}
func AlbumStorageTarget(ctx context.Context, id int64) (StorageProfile, error) {
	row, err := OwnAlbumModel(ctx, id).One()
	if err != nil {
		return StorageProfile{}, err
	}
	if row.IsEmpty() {
		return StorageProfile{}, errors.New("画册不存在或无权操作")
	}
	target, err := CurrentStorageTarget()
	if err != nil {
		return StorageProfile{}, err
	}
	return targetForLanguage(target, row["language"].String())
}

func configuredHost(key, fallback string) string {
	if host := os.Getenv(key); host != "" {
		return host
	}
	return fallback
}
