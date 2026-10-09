package app

import (
	"context"
	"errors"
	"math"
	"net/url"
	"strconv"
	"strings"

	xhtml "golang.org/x/net/html"
)

// CMSHome contains provider editorial choices, rather than a category listing.
// Banners retain their landscape image separately from a film's poster.
type CMSHome struct {
	Banners  []CMSHomeBanner  `json:"banners"`
	Sections []CMSHomeSection `json:"sections"`
}

type CMSHomeBanner struct {
	Drama        Drama  `json:"drama"`
	ImageURL     string `json:"image_url"`
	TitleLogoURL string `json:"title_logo_url,omitempty"`
}

type CMSHomeSection struct {
	Key      string  `json:"key"`
	Title    string  `json:"title"`
	Subtitle string  `json:"subtitle,omitempty"`
	Dramas   []Drama `json:"dramas"`
}

// Home fetches the public 4KVM homepage. The caller owns refresh/cache policy.
// It does not fetch individual films, execute upstream scripts, or import data.
func (bridge *CMSBridge) Home(ctx context.Context, provider string) (CMSHome, error) {
	if provider != source4KVM {
		return CMSHome{}, errors.New("该片源不支持首页推荐")
	}
	ctx, cancel := context.WithTimeout(ctx, providerTimeout)
	defer cancel()
	base := bridge.downloader.fourKVMBaseURL()
	body, err := bridge.downloader.fetchProviderText(ctx, base+"/", base+"/")
	if err != nil {
		return CMSHome{}, err
	}
	home := parse4KVMHomeDOM(body, base)
	if len(home.Banners) == 0 && len(home.Sections) == 0 {
		return CMSHome{}, errors.New("4KVM 首页没有返回可用的幻灯或影片推荐")
	}
	return home, nil
}

func fourKVMHomeImage(raw, base string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.String() == "" || u.User != nil {
		return ""
	}
	root, err := url.Parse(base + "/")
	if err != nil {
		return ""
	}
	u = root.ResolveReference(u)
	if (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" {
		return ""
	}
	if strings.Contains(u.Path, "placeholder") || strings.HasPrefix(u.Path, "/static/images/numbers/") {
		return ""
	}
	return u.String()
}

func fourKVMHomeSlug(raw, base string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	root, rootErr := url.Parse(base)
	if err != nil || rootErr != nil || u.User != nil {
		return ""
	}
	if u.IsAbs() || u.Host != "" {
		if (u.Scheme != "https" && u.Scheme != "http" && u.Scheme != "") || !strings.EqualFold(u.Host, root.Host) {
			return ""
		}
	}
	return fourKVMSlug(u.String())
}

func fourKVMHomeScore(raw string) string {
	value := strings.TrimSpace(raw)
	score, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(score) || math.IsInf(score, 0) || score <= 0 || score > 10 {
		return ""
	}
	return value
}

func fourKVMHomeCard(node *xhtml.Node, base string, classify int) Drama {
	drama := Drama{Source: source4KVM, ChannelName: "4kvm.net", Remark: "4KVM", Category: fourKVMCategory(classify), CategoryName: fourKVMCategory(classify)}
	cover := ""
	fourKVMWalk(node, func(child *xhtml.Node) {
		if child.Type != xhtml.ElementNode {
			return
		}
		if drama.SourceID == "" && child.Data == "a" {
			drama.SourceID = fourKVMHomeSlug(fourKVMNodeAttr(child, "href"), base)
		}
		if drama.Title == "" && (child.Data == "h3" || child.Data == "h4") {
			drama.Title = fourKVMNodeText(child)
		}
		if cover == "" && child.Data == "img" && !fourKVMHasClass(child, "ranking-number-svg") {
			cover = fourKVMHomeImage(firstNonEmpty(fourKVMNodeAttr(child, "data-src"), fourKVMNodeAttr(child, "src")), base)
		}
		if drama.Desc == "" && child.Data == "p" {
			drama.Desc = fourKVMNodeText(child)
		}
		if child.Data == "span" || child.Data == "div" {
			text := fourKVMNodeText(child)
			if drama.OnlineDate == "" && re4KVMYear.FindString(text) == text {
				drama.OnlineDate = text
			}
			if drama.Score == "" && (fourKVMHasClass(child, "text-yellow-400") || fourKVMHasClass(child, "text-yellow-500") || fourKVMHasClass(child, "text-green-500")) {
				drama.Score = fourKVMHomeScore(text)
			}
		}
	})
	drama.ID = providerDramaID(source4KVM, drama.SourceID)
	drama.Name, drama.Intro, drama.Cover, drama.CoverURL = drama.Title, drama.Desc, cover, cover
	return drama
}

func fourKVMHomeCards(root *xhtml.Node, base string, classify, limit int) []Drama {
	var items []Drama
	seen := map[string]bool{}
	var walk func(*xhtml.Node)
	walk = func(node *xhtml.Node) {
		if node == nil || len(items) >= limit {
			return
		}
		if node.Type == xhtml.ElementNode && (fourKVMHasClass(node, "movie-card") || (node.Data == "a" && fourKVMNodeAttr(node, "data-vod") != "")) {
			drama := fourKVMHomeCard(node, base, classify)
			if drama.SourceID != "" && drama.Title != "" && drama.Cover != "" && !seen[drama.SourceID] {
				seen[drama.SourceID] = true
				items = append(items, drama)
			}
			return
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return items
}

func fourKVMHomeSectionKind(title string) (string, int) {
	switch strings.TrimSpace(title) {
	case "今天热播", "热播推荐":
		return "hot", 0
	case "最近更新":
		return "recent", 0
	case "最新上架":
		return "latest", 0
	case "近期热播剧", "电视剧推荐":
		return "tv", 2
	case "本季跟播新番", "动漫推荐":
		return "anime", 3
	case "电影推荐":
		return "movie", 1
	case "综艺推荐":
		return "variety", 4
	case "榜单排行":
		return "rank", 0
	case "人气排行榜":
		return "popular", 0
	case "vip影片", "VIP影片":
		return "vip", 0
	}
	return "", 0
}

func parse4KVMHomeDOM(body, base string) CMSHome {
	doc := fourKVMParseHTML(body)
	home := CMSHome{Banners: []CMSHomeBanner{}, Sections: []CMSHomeSection{}}
	posters := map[string]Drama{}
	for _, drama := range fourKVMHomeCards(doc, base, 0, 512) {
		posters[drama.SourceID] = drama
	}
	postersByFilm := map[string]Drama{}
	fourKVMWalk(doc, func(node *xhtml.Node) {
		if node.Type != xhtml.ElementNode || node.Data != "a" {
			return
		}
		film := fourKVMNodeAttr(node, "data-vod")
		if !fourKVMSlugPattern.MatchString(film) {
			return
		}
		if poster, exists := posters[fourKVMHomeSlug(fourKVMNodeAttr(node, "href"), base)]; exists {
			postersByFilm[film] = poster
		}
	})
	seenBanner := map[string]bool{}
	seenSection := map[string]bool{}
	fourKVMWalk(doc, func(node *xhtml.Node) {
		if node.Type != xhtml.ElementNode {
			return
		}
		if fourKVMHasClass(node, "slide-inner") && len(home.Banners) < 8 {
			inHero := false
			for parent := node.Parent; parent != nil; parent = parent.Parent {
				if fourKVMHasClass(parent, "hero-swiper") || fourKVMNodeAttr(parent, "id") == "hero-banner" {
					inHero = true
					break
				}
			}
			if !inHero || node.Parent == nil || node.Parent.Data != "a" {
				return
			}
			slug := fourKVMHomeSlug(fourKVMNodeAttr(node.Parent, "href"), base)
			if slug == "" || seenBanner[slug] {
				return
			}
			poster, exists := posters[slug]
			if !exists {
				// Hero and recommendation links may point at different chapters.
				// Match only the provider's explicit shared movie record in that case.
				poster, exists = postersByFilm[fourKVMNodeAttr(node.Parent, "data-vod")]
			}
			if !exists {
				poster.Cover, poster.CoverURL = "", ""
			}
			banner := CMSHomeBanner{Drama: poster}
			banner.Drama.ID, banner.Drama.Source, banner.Drama.SourceID = providerDramaID(source4KVM, slug), source4KVM, slug
			banner.Drama.Title = strings.TrimSpace(fourKVMNodeAttr(node, "data-title"))
			banner.Drama.Desc = firstNonEmpty(fourKVMNodeAttr(node, "data-info"), fourKVMNodeAttr(node, "data-subtitle"))
			if score := fourKVMHomeScore(fourKVMNodeAttr(node, "data-rating")); score != "" {
				banner.Drama.Score = score
			}
			fourKVMWalk(node, func(child *xhtml.Node) {
				if child.Type != xhtml.ElementNode || child.Data != "img" {
					return
				}
				image := fourKVMHomeImage(firstNonEmpty(fourKVMNodeAttr(child, "data-src"), fourKVMNodeAttr(child, "src")), base)
				if child.Parent == node && banner.ImageURL == "" {
					banner.ImageURL = image
				} else if banner.TitleLogoURL == "" && image != banner.ImageURL {
					banner.TitleLogoURL = image
				}
			})
			if banner.Drama.Title == "" || banner.ImageURL == "" {
				return
			}
			banner.Drama.Name, banner.Drama.Intro = banner.Drama.Title, banner.Drama.Desc
			banner.Drama.ChannelName, banner.Drama.Remark = "4kvm.net", "4KVM"
			seenBanner[slug] = true
			home.Banners = append(home.Banners, banner)
			return
		}
		if node.Data != "h2" || len(home.Sections) >= 12 {
			return
		}
		title := fourKVMNodeText(node)
		key, classify := fourKVMHomeSectionKind(title)
		if key == "" || seenSection[key] {
			return
		}
		section := CMSHomeSection{Key: key, Title: title}
		// Header labels sit inside two wrappers. Pick the first bounded content
		// container, never the shared main container (which mixes all sections).
		for parent, depth := node.Parent, 0; parent != nil && depth < 4; parent, depth = parent.Parent, depth+1 {
			if parent.Data == "main" || parent.Data == "body" || fourKVMHasClass(parent, "space-y-16") {
				break
			}
			section.Dramas = fourKVMHomeCards(parent, base, classify, 32)
			if len(section.Dramas) > 0 {
				break
			}
			if parent.Data == "section" {
				// The popularity header is a standalone section followed by its grid.
				for next := parent.NextSibling; next != nil; next = next.NextSibling {
					if next.Type == xhtml.ElementNode {
						section.Dramas = fourKVMHomeCards(next, base, classify, 32)
						break
					}
				}
				break
			}
		}
		if len(section.Dramas) == 0 {
			return
		}
		if node.Parent != nil {
			for child := node.Parent.FirstChild; child != nil; child = child.NextSibling {
				if child.Type == xhtml.ElementNode && child.Data == "p" {
					section.Subtitle = fourKVMNodeText(child)
					break
				}
			}
		}
		seenSection[key] = true
		home.Sections = append(home.Sections, section)
	})
	return home
}
