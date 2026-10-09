package suxinvideo

import (
	"context"
	"encoding/json"
	"errors"
	"html/template"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/suxinwl/GoSuxin/framework/util/gconv"
	xq "github.com/suxinwl/GoSuxin/internal/xiaoqiapp"
)

var fourKVMSiteSnapshotPath = "data/suxinvideo-provider/4kvm-site.json"

type fourKVMSiteHomeSnapshot struct {
	Updated int64      `json:"updated"`
	Home    xq.CMSHome `json:"home"`
}

var fourKVMSiteState = struct {
	sync.Mutex
	loaded, refreshing bool
	nextRefresh        time.Time
	snapshot           fourKVMSiteHomeSnapshot
}{}

var fetchFourKVMSiteHome = func(ctx context.Context) (xq.CMSHome, error) {
	return cmsProviders.Home(ctx, "4kvm")
}

func fourKVMSiteSource(ctx context.Context) (row, error) {
	item, err := one(ctx, "SELECT * FROM sx_collect_api WHERE api_url=? AND status=1 ORDER BY id LIMIT 1", fourKVMSourceURL)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, errors.New("4KVM片源已停用")
	}
	return item, nil
}

// Only the Guoguo homepage requests this snapshot. A cold or failed upstream
// fetch never blocks rendering; the already collected 4KVM library is usable.
func fourKVMSiteSnapshot(ctx context.Context) fourKVMSiteHomeSnapshot {
	fourKVMSiteState.Lock()
	defer fourKVMSiteState.Unlock()
	if !fourKVMSiteState.loaded {
		fourKVMSiteState.loaded = true
		if data, err := os.ReadFile(fourKVMSiteSnapshotPath); err == nil && len(data) < 4<<20 {
			_ = json.Unmarshal(data, &fourKVMSiteState.snapshot)
		}
	}
	if !fourKVMSiteState.refreshing && time.Now().After(fourKVMSiteState.nextRefresh) {
		fourKVMSiteState.refreshing = true
		go refreshFourKVMSiteHome()
	}
	return fourKVMSiteState.snapshot
}

func refreshFourKVMSiteHome() {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	home, err := fetchFourKVMSiteHome(ctx)
	accepted := err == nil && (len(home.Banners) > 0 || len(home.Sections) > 0)
	fourKVMSiteState.Lock()
	if accepted {
		previous := fourKVMSiteState.snapshot.Home
		if len(home.Banners) == 0 {
			home.Banners = previous.Banners
		}
		if len(home.Sections) == 0 {
			home.Sections = previous.Sections
		}
		fourKVMSiteState.snapshot = fourKVMSiteHomeSnapshot{Updated: time.Now().Unix(), Home: home}
	}
	snapshot := fourKVMSiteState.snapshot
	fourKVMSiteState.nextRefresh = time.Now().Add(time.Minute)
	if accepted {
		fourKVMSiteState.nextRefresh = time.Now().Add(5 * time.Minute)
	}
	// Keep the single-refresh guard while writing so an older refresh cannot
	// overwrite a more recent snapshot on disk.
	if accepted {
		_ = saveFourKVMSiteSnapshot(snapshot)
	}
	fourKVMSiteState.refreshing = false
	fourKVMSiteState.Unlock()
}

func saveFourKVMSiteSnapshot(snapshot fourKVMSiteHomeSnapshot) error {
	data, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	dir := filepath.Dir(fourKVMSiteSnapshotPath)
	if err = os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, "4kvm-site-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err = file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), fourKVMSiteSnapshotPath)
}

func fourKVMSiteRows(ctx context.Context, dramas []xq.Drama) []row {
	items := make([]row, 0, min(len(dramas), 1000))
	seen := make(map[string]bool)
	for _, drama := range dramas {
		id, name := strings.TrimSpace(drama.SourceID), strings.TrimSpace(drama.DisplayTitle())
		if drama.Source != "4kvm" || !fourKVMSiteSlugPattern.MatchString(id) || name == "" || seen[id] || !contentMacAllowed(ctx, cmsProviderVodItem("4kvm", drama)) {
			continue
		}
		seen[id] = true
		items = append(items, row{"id": 0, "name": cutRunes(name, 120), "pic": drama.Cover, "remarks": cutRunes(drama.Remark, 60), "year": drama.OnlineDate, "area": drama.Area, "class": drama.Category, "score": drama.Score, "score_source": "4KVM", "content": drama.Desc, "vip": 0, "link": fourKVMSiteVodLink(ctx, id)})
		if len(items) == 1000 {
			break
		}
	}
	return items
}

func fourKVMSiteLocalRows(ctx context.Context, order string, limit int) []row {
	if limit < 1 || limit > 200 {
		limit = 42
	}
	sort := "updatetime DESC,id DESC"
	if order == "hot" {
		sort = "total_hits DESC,id DESC"
	}
	films, err := all(ctx, "SELECT id,name,pic,class,year,remarks,score,area,content,vip FROM sx_vod WHERE "+publicVodListingCondition(ctx, "")+" AND CONCAT('$$$',play_from,'$$$') LIKE '%$$$4kvm$$$%' ORDER BY "+sort+" LIMIT ?", limit)
	if err != nil {
		return nil
	}
	return films
}

func renderFourKVMSiteHome(ctx context.Context, types []row) (*HomeRes, error) {
	var heroes []homeHero
	var recent, hot []row
	var ranking []row
	blocks := []map[string]any{}
	if _, err := fourKVMSiteSource(ctx); err == nil {
		home := fourKVMSiteSnapshot(ctx).Home
		for _, banner := range home.Banners {
			films := fourKVMSiteRows(ctx, []xq.Drama{banner.Drama})
			if len(films) == 0 {
				continue
			}
			hero := filmHero(films[0])
			hero.Pic, hero.Link = banner.ImageURL, gconv.String(films[0]["link"])+"&play=1"
			heroes = append(heroes, hero)
			if len(heroes) == 10 {
				break
			}
		}
		for _, section := range home.Sections {
			films := fourKVMSiteRows(ctx, section.Dramas)
			if len(films) == 0 {
				continue
			}
			switch section.Key {
			case "recent":
				recent = films
			case "hot":
				hot = films
			case "rank":
				ranking = films
			default:
				blocks = append(blocks, map[string]any{"Name": section.Title, "Vods": films[:min(len(films), 12)], "MoreURL": "/suxinvideo/4kvm/catalog?section=" + url.QueryEscape(section.Key)})
			}
		}
		if len(recent) == 0 {
			recent = fourKVMSiteLocalRows(ctx, "recent", 42)
		}
		if len(hot) == 0 {
			hot = fourKVMSiteLocalRows(ctx, "hot", 12)
		}
		if len(heroes) == 0 {
			_ = hydrateVodSourceScores(ctx, hot)
			for _, film := range hot[:min(len(hot), 6)] {
				heroes = append(heroes, filmHero(film))
			}
		}
	}
	if setting(ctx, "home_slide_enable", "1") != "1" {
		heroes = nil
	}
	for i := range heroes {
		if strings.HasPrefix(heroes[i].Pic, "http://") || strings.HasPrefix(heroes[i].Pic, "https://") {
			heroes[i].Pic = imageLink(ctx, heroes[i].Pic)
		}
		if strings.HasPrefix(heroes[i].Poster, "http://") || strings.HasPrefix(heroes[i].Poster, "https://") {
			heroes[i].Poster = imageLink(ctx, heroes[i].Poster)
		}
	}
	links, err := all(ctx, "SELECT name,url FROM sx_link WHERE status=1 ORDER BY sort ASC,id ASC LIMIT 20")
	if err != nil {
		return nil, err
	}
	heroJSON, err := json.Marshal(heroes)
	if err != nil {
		return nil, err
	}
	var first homeHero
	if len(heroes) > 0 {
		first = heroes[0]
	}
	render(ctx, "首页", types, homeThemeBody("guoguo"), map[string]any{
		"FullWidth": true, "Vods": recent, "Recent": recent, "Hot": hot, "Hero": heroes, "HeroFirst": first,
		"RecentMoreURL": "/suxinvideo/4kvm/catalog?section=recent", "HotMoreURL": "/suxinvideo/4kvm/catalog?section=hot",
		"HeroJSON": template.JS(heroJSON), "ThemeJSON": template.JS(`"guoguo"`), "HomeBlocks": blocks, "Links": links,
		"GuoguoRankingVods": ranking,
	})
	return &HomeRes{}, nil
}
