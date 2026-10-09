package app

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"

	xhtml "golang.org/x/net/html"
)

var (
	fourKVMSlugPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
	fourKVMPositiveID  = regexp.MustCompile(`^[1-9][0-9]{0,19}$`)
	fourKVMDefaultLine = regexp.MustCompile(`^\s*episodeManager\(\s*([1-9][0-9]*)\s*,`)
	fourKVMTitleSuffix = regexp.MustCompile(`\s+-\s+第\s*[0-9]+\s*集\s*$`)
)

func fourKVMParseHTML(body string) *xhtml.Node {
	doc, _ := xhtml.Parse(strings.NewReader(body))
	return doc
}

func fourKVMWalk(node *xhtml.Node, visit func(*xhtml.Node)) {
	if node == nil {
		return
	}
	visit(node)
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		fourKVMWalk(child, visit)
	}
}

func fourKVMNodeAttr(node *xhtml.Node, key string) string {
	for _, attr := range node.Attr {
		if strings.EqualFold(attr.Key, key) {
			return strings.TrimSpace(attr.Val)
		}
	}
	return ""
}

func fourKVMNodeText(node *xhtml.Node) string {
	var parts []string
	var collect func(*xhtml.Node)
	collect = func(n *xhtml.Node) {
		if n == nil || (n.Type == xhtml.ElementNode && (n.Data == "script" || n.Data == "style" || n.Data == "template")) {
			return
		}
		if n.Type == xhtml.TextNode {
			parts = append(parts, n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			collect(c)
		}
	}
	collect(node)
	return strings.Join(strings.Fields(strings.Join(parts, " ")), " ")
}

func fourKVMHasClass(node *xhtml.Node, class string) bool {
	for _, value := range strings.Fields(fourKVMNodeAttr(node, "class")) {
		if value == class {
			return true
		}
	}
	return false
}

func fourKVMSlug(href string) string {
	u, err := url.Parse(strings.TrimSpace(href))
	if err != nil || !strings.HasPrefix(u.Path, "/play/") {
		return ""
	}
	slug := strings.TrimPrefix(u.Path, "/play/")
	if !fourKVMSlugPattern.MatchString(slug) {
		return ""
	}
	return slug
}

// Catalog and search use different card markup. The href is a chapter slug;
// data-vod/data-vod-id can instead identify the underlying movie record.
func parse4KVMCardsDOM(body string, classify int) []Drama {
	var dramas []Drama
	seen := map[string]bool{}
	fourKVMWalk(fourKVMParseHTML(body), func(node *xhtml.Node) {
		if node.Type != xhtml.ElementNode || !(fourKVMHasClass(node, "movie-card") || (node.Data == "a" && fourKVMNodeAttr(node, "data-vod") != "")) {
			return
		}
		slug, title, cover, desc, year, score := "", "", "", "", "", ""
		fourKVMWalk(node, func(child *xhtml.Node) {
			if child.Type != xhtml.ElementNode {
				return
			}
			if slug == "" && child.Data == "a" {
				slug = fourKVMSlug(fourKVMNodeAttr(child, "href"))
			}
			if title == "" && child.Data == "h3" {
				title = fourKVMNodeText(child)
			}
			if cover == "" && child.Data == "img" {
				cover = firstNonEmpty(fourKVMNodeAttr(child, "data-src"), fourKVMNodeAttr(child, "src"))
			}
			if desc == "" && child.Data == "p" {
				desc = fourKVMNodeText(child)
			}
			text := fourKVMNodeText(child)
			// Read a year badge, never a date mentioned in the plot or image URL.
			if year == "" && (child.Data == "span" || child.Data == "div") && re4KVMYear.FindString(text) == text {
				year = text
			}
			if score == "" && (child.Data == "span") && fourKVMHasClass(child, "text-yellow-400") {
				if n, err := strconv.ParseFloat(text, 64); err == nil && n > 0 && n <= 10 {
					score = text
				}
			}
		})
		if slug == "" || title == "" || seen[slug] {
			return
		}
		seen[slug] = true
		category := fourKVMCategory(classify)
		dramas = append(dramas, Drama{
			ID: providerDramaID(source4KVM, slug), Source: source4KVM, SourceID: slug, Title: title, Name: title,
			Desc: desc, Intro: desc, Cover: cover, CoverURL: cover, Category: category, CategoryName: category,
			ChannelName: "4kvm.net", OnlineDate: year, Score: score, Remark: "4KVM",
		})
	})
	return dramas
}

func parse4KVMChapterRefsDOM(doc *xhtml.Node) []fourKVMChapterRef {
	var result []fourKVMChapterRef
	seen := map[string]bool{}
	defaultLine := ""
	fourKVMWalk(doc, func(node *xhtml.Node) {
		if match := fourKVMDefaultLine.FindStringSubmatch(fourKVMNodeAttr(node, "x-data")); len(match) == 2 {
			defaultLine = match[1]
		}
	})
	fourKVMWalk(doc, func(node *xhtml.Node) {
		if node.Type != xhtml.ElementNode || node.Data != "a" {
			return
		}
		slug := fourKVMSlug(fourKVMNodeAttr(node, "href"))
		dataID, episode := fourKVMNodeAttr(node, "dataid"), fourKVMNodeAttr(node, "data-episode")
		if slug == "" || !fourKVMPositiveID.MatchString(dataID) || !fourKVMPositiveID.MatchString(episode) {
			return
		}
		line := firstNonEmpty(fourKVMNodeAttr(node, "data-line"), "1")
		key := line + ":" + episode + ":" + dataID + ":" + slug
		if seen[key] {
			return
		}
		seen[key] = true
		title := fourKVMNodeText(node)
		if title == "" {
			title = "第" + episode + "集"
		}
		result = append(result, fourKVMChapterRef{Slug: slug, DataID: dataID, Line: line, Episode: episode, Title: title, DefaultLine: defaultLine != "" && line == defaultLine})
	})
	return result
}

func parse4KVMDetailDOM(doc *xhtml.Node, sourceID string) Drama {
	drama := Drama{ID: providerDramaID(source4KVM, sourceID), Source: source4KVM, SourceID: sourceID, ChannelName: "4kvm.net", Remark: "4KVM"}
	fields := map[string]string{}
	meta := map[string]string{}
	fourKVMWalk(doc, func(node *xhtml.Node) {
		if node.Type != xhtml.ElementNode {
			return
		}
		if node.Data == "meta" {
			key := firstNonEmpty(fourKVMNodeAttr(node, "property"), fourKVMNodeAttr(node, "name"))
			meta[key] = fourKVMNodeAttr(node, "content")
		}
		if classify, err := strconv.Atoi(fourKVMNodeAttr(node, "data-classify-id")); err == nil && classify >= 1 && classify <= 4 {
			drama.Category = fourKVMCategory(classify)
		}
		// The details modal explicitly labels these values in adjacent cells.
		if fourKVMHasClass(node, "col-span-1") {
			label := fourKVMNodeText(node)
			for next := node.NextSibling; next != nil; next = next.NextSibling {
				if next.Type != xhtml.ElementNode {
					continue
				}
				if fourKVMHasClass(next, "col-span-2") {
					fields[label] = fourKVMNodeText(next)
				}
				break
			}
		}
		if drama.Title == "" && node.Data == "h1" {
			drama.Title = fourKVMNodeText(node)
		}
	})
	if title := strings.TrimSpace(fourKVMTitleSuffix.ReplaceAllString(meta["og:title"], "")); title != "" {
		drama.Title = title
	}
	drama.Name = drama.Title
	drama.Desc, drama.Intro = meta["description"], meta["description"]
	drama.Cover, drama.CoverURL = meta["og:image"], meta["og:image"]
	drama.Area, drama.Language = fields["地区"], fields["语言"]
	drama.Director, drama.Actor = fields["导演"], fields["主演"]
	drama.OnlineDate = re4KVMYear.FindString(firstNonEmpty(fields["上映"], fields["年份"]))
	// The keyword list contains the exact release year immediately after title.
	// Leave absent values unknown instead of deriving them from the synopsis.
	if drama.OnlineDate == "" {
		parts := strings.Split(meta["keywords"], ",")
		for index := range parts {
			if strings.TrimSpace(parts[index]) == drama.Title && index+1 < len(parts) {
				year := strings.TrimSpace(parts[index+1])
				if re4KVMYear.FindString(year) == year {
					drama.OnlineDate = year
				}
				break
			}
		}
	}
	drama.CategoryName = drama.Category
	if genre := fields["类型"]; genre != "" {
		for _, tag := range strings.Split(genre, "/") {
			if tag = strings.TrimSpace(tag); tag != "" {
				drama.Tags = append(drama.Tags, tag)
			}
		}
	}
	return drama
}
