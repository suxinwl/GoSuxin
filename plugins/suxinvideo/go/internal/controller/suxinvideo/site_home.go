package suxinvideo

import (
	"context"
	"encoding/json"
	"html/template"
	"net/url"
	"strconv"
	"strings"

	"github.com/suxinwl/GoSuxin/framework/util/gconv"
)

type homeHero struct {
	Name        string `json:"name"`
	Pic         string `json:"pic"`
	Link        string `json:"link"`
	Button      string `json:"button"`
	Category    string `json:"category"`
	Year        string `json:"year"`
	Remarks     string `json:"remarks"`
	Score       string `json:"score"`
	Area        string `json:"area"`
	Description string `json:"description"`
	Poster      string `json:"poster"`
	Film        bool   `json:"film"`
}

func slideURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "/suxinvideo/") || raw == "/suxinvideo" {
		return raw
	}
	u, err := url.Parse(raw)
	if err == nil && u.Host != "" && (u.Scheme == "https" || u.Scheme == "http") && u.User == nil {
		return raw
	}
	return "/suxinvideo"
}

func filmHero(v row) homeHero {
	id := gconv.Int64(v["id"])
	desc := filmDescription(gconv.String(v["content"]))
	if len([]rune(desc)) > 120 {
		desc = string([]rune(desc)[:120])
	}
	category := strings.TrimSpace(strings.Split(gconv.String(v["class"]), ",")[0])
	if category == "" {
		category = "精选推荐"
	}
	return homeHero{
		Name: gconv.String(v["name"]), Pic: gconv.String(v["pic"]),
		Link: "/suxinvideo/play?id=" + strconv.FormatInt(id, 10), Button: "立即播放",
		Category: category, Year: gconv.String(v["year"]), Remarks: gconv.String(v["remarks"]),
		Score: scoreLabel(v), Area: gconv.String(v["area"]), Description: desc,
		Poster: gconv.String(v["pic"]), Film: true,
	}
}

func managedHero(ctx context.Context, v row) homeHero {
	h := homeHero{Name: gconv.String(v["name"]), Pic: gconv.String(v["pic"]),
		Link: slideURL(gconv.String(v["url"])), Button: "查看详情", Category: "精选推荐"}
	u, err := url.Parse(h.Link)
	if err != nil || !(u.Path == "/suxinvideo/detail" || u.Path == "/suxinvideo/play") {
		return h
	}
	id, err := strconv.ParseInt(u.Query().Get("id"), 10, 64)
	if err != nil || id < 1 {
		return h
	}
	film, err := one(ctx, "SELECT id,name,pic,class,year,remarks,score,area,content FROM sx_vod WHERE id=? AND "+publicVodCondition(ctx, ""), id)
	if err != nil || film == nil {
		return h
	}
	_ = hydrateVodSourceScores(ctx, []row{film})
	meta := filmHero(film)
	h.Year, h.Remarks, h.Score, h.Area = meta.Year, meta.Remarks, meta.Score, meta.Area
	h.Description, h.Poster, h.Film = meta.Description, meta.Poster, true
	if u.Path == "/suxinvideo/play" {
		h.Button = "立即播放"
	}
	return h
}

func renderHome(ctx context.Context) (*HomeRes, error) {
	types, err := all(ctx, "SELECT id,name FROM sx_type WHERE pid=0 AND status=1 ORDER BY sort,id LIMIT 30")
	if err != nil {
		return nil, err
	}
	theme := cleanTheme(setting(ctx, "site_template", "suxinlite"))
	if theme == "guoguo" {
		return renderFourKVMSiteHome(ctx, types)
	}
	remote := yqkSiteHome(ctx)
	var recent, hot []row
	recentMoreURL, hotMoreURL := "/suxinvideo/channel?id=0&order=time&source=local", "/suxinvideo/channel?id=0&order=hits&source=local"
	if setting(ctx, "home_recommend_source", "local") == "yqk" {
		recent, hot = copyHomeVods(remote.Recent), copyHomeVods(remote.Hot)
		if len(recent) > 0 {
			recentMoreURL = "/suxinvideo/channel?id=0&order=time&source=remote"
		}
		if len(hot) > 0 {
			hotMoreURL = "/suxinvideo/channel?id=0&order=hits&source=remote"
		}
	}
	if len(recent) == 0 {
		recent, err = all(ctx, "SELECT id,name,pic,remarks,year,score,area,vip FROM sx_vod WHERE "+publicVodListingCondition(ctx, "")+" ORDER BY updatetime DESC,id DESC LIMIT 42")
		if err != nil {
			return nil, err
		}
	}
	if len(hot) == 0 {
		hot, err = all(ctx, "SELECT id,name,pic,remarks,year,score,area,vip FROM sx_vod WHERE "+publicVodListingCondition(ctx, "")+" ORDER BY total_hits DESC,id DESC LIMIT 12")
		if err != nil {
			return nil, err
		}
	}
	hero := make([]homeHero, 0, 10)
	if setting(ctx, "home_slide_enable", "1") == "1" && (theme != "suxinpro" || setting(ctx, "kp_slide_enable", "1") == "1") {
		limit := 6
		if theme == "iqiyi" {
			limit = 3
		} else if theme == "suxinpro" {
			limit = gconv.Int(setting(ctx, "kp_slide_count", "6"))
			if limit < 2 || limit > 10 {
				limit = 6
			}
		}
		remoteHero := remote.Hero
		if len(remoteHero) > limit {
			remoteHero = remoteHero[:limit]
		}
		if setting(ctx, "home_hero_source", "yqk") == "yqk" {
			hero = append(hero, remoteHero...)
		}
		if len(hero) == 0 {
			managed, e := all(ctx, "SELECT name,pic,url FROM sx_slide WHERE status=1 AND (pos='top' OR pos='') ORDER BY sort ASC,id DESC LIMIT 10")
			if e != nil {
				return nil, e
			}
			if len(managed) == 0 && theme == "suxinpro" {
				managed, e = all(ctx, "SELECT name,pic,url FROM sx_slide WHERE status=1 AND pos='movie' ORDER BY sort ASC,id DESC LIMIT 10")
				if e != nil {
					return nil, e
				}
			}
			for _, slide := range managed {
				hero = append(hero, managedHero(ctx, slide))
			}
		}
		if len(hero) == 0 {
			hero = append(hero, remoteHero...)
		}
		if len(hero) == 0 {
			order := "total_hits DESC,id DESC"
			if theme == "suxinpro" && setting(ctx, "kp_slide_source", "new") == "new" {
				order = "updatetime DESC,id DESC"
			}
			films, e := all(ctx, "SELECT id,name,pic,class,year,remarks,score,area,content FROM sx_vod WHERE "+publicVodListingCondition(ctx, "")+" ORDER BY "+order+" LIMIT ?", limit)
			if e != nil {
				return nil, e
			}
			_ = hydrateVodSourceScores(ctx, films)
			for _, film := range films {
				hero = append(hero, filmHero(film))
			}
		}
	}
	links, err := all(ctx, "SELECT name,url FROM sx_link WHERE status=1 ORDER BY sort ASC,id ASC LIMIT 20")
	if err != nil {
		return nil, err
	}
	homeTypes, err := all(ctx, "SELECT id,name FROM sx_type WHERE pid=0 AND status=1 AND show_home=1 ORDER BY sort,id LIMIT 30")
	if err != nil {
		return nil, err
	}
	blocks := make([]map[string]any, 0, len(homeTypes))
	for _, category := range homeTypes {
		items, e := all(ctx, "SELECT id,name,pic,remarks,year,score,area,vip FROM sx_vod WHERE "+publicVodListingCondition(ctx, "")+" AND (type_id=? OR type_id IN (SELECT id FROM sx_type WHERE pid=? AND status=1)) ORDER BY updatetime DESC,id DESC LIMIT 12", category["id"], category["id"])
		if e != nil {
			return nil, e
		}
		if len(items) > 0 {
			blocks = append(blocks, map[string]any{"Name": category["name"], "ID": category["id"], "Vods": items})
		}
	}
	for i := range hero {
		if strings.HasPrefix(hero[i].Pic, "http://") || strings.HasPrefix(hero[i].Pic, "https://") {
			hero[i].Pic = imageLink(ctx, hero[i].Pic)
		}
		if strings.HasPrefix(hero[i].Poster, "http://") || strings.HasPrefix(hero[i].Poster, "https://") {
			hero[i].Poster = imageLink(ctx, hero[i].Poster)
		}
	}
	encoded, err := json.Marshal(hero)
	if err != nil {
		return nil, err
	}
	encodedTheme, err := json.Marshal(theme)
	if err != nil {
		return nil, err
	}
	var first homeHero
	if len(hero) > 0 {
		first = hero[0]
	}
	render(ctx, "首页", types, homeThemeBody(theme), map[string]any{
		"FullWidth": true, "Vods": recent, "Recent": recent, "Hot": hot, "Hero": hero, "HeroFirst": first,
		"RecentMoreURL": recentMoreURL, "HotMoreURL": hotMoreURL,
		"HeroJSON": template.JS(encoded), "ThemeJSON": template.JS(encodedTheme), "HomeBlocks": blocks, "Links": links,
	})
	return &HomeRes{}, nil
}

// Image preparation must not alter shared provider snapshots across requests.
func copyHomeVods(items []row) []row {
	result := make([]row, 0, len(items))
	for _, item := range items {
		copy := make(row, len(item))
		for key, value := range item {
			copy[key] = value
		}
		result = append(result, copy)
	}
	return result
}

const homeHeroScript = `<script>
(function(){
 const list={{.HeroJSON}}, theme={{.ThemeJSON}}, fallback='/suxinvideo/asset?theme='+theme+'&file=banner.svg', posterFallback='/suxinvideo/asset?theme='+theme+'&file=nopic.svg';
 if(!list.length)return;
 let index=0,timer,layer=0,startX=0,generation=0;
 const q=s=>document.querySelector(s), qa=s=>document.querySelectorAll(s), box=q('#kHero')||q('#xhero')||q('.iqhero');
 const posterFrame=q('#sxHeroPoster')||q('.iqhero-r'),posterLink=q('#sxHeroPosterLink')||q('#iqPosterLink'),posterImage=q('#sxHeroPosterImg')||q('#iqPosterImg'),notice=q('.sx-hero-image-notice');
 function loadImage(candidates,seq,callback){
   const urls=[...new Set(candidates.filter(s=>typeof s==='string'&&s.trim()))];let attempt=0;
   function next(){if(seq!==generation)return;if(attempt>=urls.length){callback({src:fallback,width:1920,height:640,failed:true});return}
     const src=urls[attempt++],image=new Image();image.decoding='async';let done=false;
     const finish=ok=>{if(done)return;done=true;clearTimeout(timeout);image.onload=image.onerror=null;if(seq!==generation)return;
       if(ok&&image.naturalWidth&&image.naturalHeight)callback({src,width:image.naturalWidth,height:image.naturalHeight,failed:src===fallback,alternate:attempt>1});
       else{image.removeAttribute('src');next()}};
     const timeout=setTimeout(()=>finish(false),6000);image.onload=()=>finish(true);image.onerror=()=>finish(false);image.src=src;
   }next();
 }
 function background(src,portrait){
   if(!box)return;box.classList.toggle('sx-hero-poster-mode',portrait);
   const next=theme==='suxinlite'?q(layer?'#kHeroBg1':'#kHeroBg2'):q('#sxHeroBg');
   if(!next)return;next.style.backgroundImage='url('+JSON.stringify(src)+')';next.classList.toggle('sx-hero-soft',portrait);
   if(theme==='suxinlite'){const old=q(layer?'#kHeroBg2':'#kHeroBg1');layer=1-layer;next.classList.add('on');next.classList.remove('zoom');void next.offsetWidth;next.classList.add('zoom');old?.classList.remove('on')}
 }
 function showNotice(text){if(notice){notice.textContent=text;notice.hidden=!text}}
 function setPoster(src,d,seq){
   if(!posterFrame||!posterImage||!posterLink)return;
   posterFrame.hidden=!src;if(!src)return;posterLink.href=d.link;posterLink.setAttribute('aria-label',d.name||'查看详情');posterImage.alt=d.name||'影片海报';
   posterImage.onerror=()=>{if(seq!==generation)return;posterImage.onerror=null;posterImage.src=posterFallback;showNotice('海报暂不可用，已显示备用图')};posterImage.src=src;
 }
 function artwork(d,seq){
   loadImage([d.pic,d.poster,fallback],seq,image=>{
     const portrait=!image.failed&&image.width/image.height<1.2;background(image.src,portrait);
     showNotice(image.failed?'图片暂不可用，已显示备用图':image.alternate?'横幅图片暂不可用，已显示海报':'');
     const src=portrait?image.src:theme==='iqiyi'&&d.film?d.poster:'';setPoster(src,d,seq);
   });
 }
 function show(i){index=(i+list.length)%list.length;const d=list[index],seq=++generation;if(posterFrame)posterFrame.hidden=true;showNotice('');artwork(d,seq);
   const title=q(theme==='suxinlite'?'#kHeroTitle':theme==='iqiyi'?'.iqtitle':'#xhTitle');if(title)title.textContent=d.name;
   const cat=q(theme==='suxinlite'?'#kHeroCat':'#xhCat');if(cat)cat.textContent=d.category||'精选推荐';
   const tags=q(theme==='suxinlite'?'#kHeroTags':'#xhTags');if(tags){tags.replaceChildren();[d.year,d.remarks].filter(Boolean).forEach(t=>{const s=document.createElement('span');s.textContent=t;tags.appendChild(s)})}
   const desc=q('#xhDesc')||q('.iqdesc');if(desc)desc.textContent=d.description||'';
   const action=q('#kHeroPlay')||q('#xhLink')||q('#iqHeroLink');if(action){action.href=d.link;action.textContent=theme==='iqiyi'&&d.film?'▶':d.button;action.classList.toggle('sx-detail-link',theme==='iqiyi'&&!d.film)}
   const meta=q('.iqmeta');if(meta){meta.style.display=d.film?'flex':'none';meta.textContent=[d.score,d.year,d.area,d.remarks].filter(Boolean).join('  |  ')}
   const badge=q('.iqtop .t1');if(badge)badge.textContent=d.film?'热映 TOP'+(index+1):'精选推荐';
   qa('.khero-dots i,.xhero-dots i,.iqdots i').forEach((dot,n)=>{dot.classList.toggle('on',n===index);dot.setAttribute('aria-current',n===index?'true':'false')});
 }
 function auto(){clearInterval(timer);if(list.length>1)timer=setInterval(()=>show(index+1),theme==='iqiyi'?5200:5500)}
 function go(i){show(i);auto()}
 qa('.khero-dots i,.xhero-dots i,.iqdots i').forEach((dot,n)=>{dot.setAttribute('role','button');dot.setAttribute('aria-label','查看第'+(n+1)+'张幻灯');dot.tabIndex=0;dot.addEventListener('click',()=>go(n));dot.addEventListener('keydown',e=>{if(e.key==='Enter'||e.key===' '){e.preventDefault();go(n)}})});
 if(box){box.addEventListener('mouseenter',()=>clearInterval(timer));box.addEventListener('mouseleave',auto);
   box.addEventListener('touchstart',e=>{startX=e.touches[0].clientX;clearInterval(timer)},{passive:true});
   box.addEventListener('touchend',e=>{const dx=e.changedTouches[0].clientX-startX;if(Math.abs(dx)>40)go(index+(dx<0?1:-1));else auto()},{passive:true});
 }
 q('#kHeroPrev')?.addEventListener('click',()=>go(index-1));q('#kHeroNext')?.addEventListener('click',()=>go(index+1));
 show(0);auto();
})();
</script>`

func homeThemeBody(theme string) string {
	if theme == "guoguo" {
		return guoguoHomeBody
	}
	sections := `{{range .HomeBlocks}}<section class="sec wrap"><div class="sec-h"><h3>{{.Name}}</h3><a href="/suxinvideo/type?id={{.ID}}">更多 ›</a></div>{{template "cards" .Vods}}</section>{{end}}{{if .Links}}<section class="sec wrap"><div class="sec-h"><h3>友情链接</h3></div><div class="sx-links">{{range .Links}}<a href="{{.url}}" target="_blank" rel="noopener noreferrer">{{.name}}</a>{{end}}</div></section>{{end}}`
	var body string
	switch theme {
	case "suxinlite":
		body = `{{if .Hero}}<div class="khero sx-home-hero" id="kHero"><div class="khero-bg sx-hero-background on" id="kHeroBg1"></div><div class="khero-bg sx-hero-background" id="kHeroBg2"></div><div class="khero-shade"></div><button class="khero-arrow pl" id="kHeroPrev" type="button" aria-label="上一张">‹</button><button class="khero-arrow pr" id="kHeroNext" type="button" aria-label="下一张">›</button><div class="wrap khero-in" id="kHeroIn"><span class="khero-cat" id="kHeroCat"></span><h1 class="khero-title" id="kHeroTitle"></h1><div class="khero-tags" id="kHeroTags"></div><a class="khero-play" id="kHeroPlay" href="{{.HeroFirst.Link}}">{{.HeroFirst.Button}}</a></div><div class="sx-hero-poster" id="sxHeroPoster" hidden><a id="sxHeroPosterLink" href="{{.HeroFirst.Link}}"><img id="sxHeroPosterImg" alt="{{.HeroFirst.Name}}" decoding="async"></a></div><span class="sx-hero-image-notice" role="status" aria-live="polite" hidden></span><div class="khero-dots">{{range .Hero}}<i></i>{{end}}</div></div>{{end}}`
	case "suxinpro":
		body = `{{if .Hero}}<div class="xhero sx-home-hero" id="xhero"><div class="sx-hero-background" id="sxHeroBg"></div><div class="xhero-shade"></div><div class="wrap xhero-in"><span class="xhero-cat" id="xhCat"></span><h1 class="xhero-title" id="xhTitle"></h1><div class="xhero-tags" id="xhTags"></div><p class="xhero-desc" id="xhDesc"></p><a class="button" id="xhLink" href="{{.HeroFirst.Link}}">{{.HeroFirst.Button}}</a></div><div class="sx-hero-poster" id="sxHeroPoster" hidden><a id="sxHeroPosterLink" href="{{.HeroFirst.Link}}"><img id="sxHeroPosterImg" alt="{{.HeroFirst.Name}}" decoding="async"></a></div><span class="sx-hero-image-notice" role="status" aria-live="polite" hidden></span><div class="xhero-dots">{{range .Hero}}<i></i>{{end}}</div></div>{{end}}`
	default:
		body = `{{if .Hero}}<div class="iqhero sx-home-hero"><div class="sx-hero-background" id="sxHeroBg"></div><div class="iqhero-shade"></div><div class="wrap iqhero-in"><div class="iqhero-l"><h1 class="iqtitle"></h1><div class="iqtop"><span class="t1"></span></div><div class="iqmeta"></div><p class="iqdesc"></p><div class="iqbtns"><a id="iqHeroLink" class="p" href="{{.HeroFirst.Link}}">{{.HeroFirst.Button}}</a></div></div><div class="iqhero-r sx-hero-poster" hidden><a id="iqPosterLink" href="{{.HeroFirst.Link}}"><img id="iqPosterImg" src="{{pic .HeroFirst.Poster}}" alt="{{.HeroFirst.Name}}"></a></div></div><span class="sx-hero-image-notice" role="status" aria-live="polite" hidden></span><div class="iqdots">{{range .Hero}}<i style="cursor:pointer"></i>{{end}}</div></div>{{end}}`
	}
	recommendations := `<section class="sec wrap"><div class="sec-h"><h3>最近更新</h3><a href="{{.RecentMoreURL}}">更多 ›</a></div>{{template "cards" .Recent}}</section><section class="sec wrap"><div class="sec-h"><h3>热播</h3><a href="{{.HotMoreURL}}">更多 ›</a></div>{{template "cards" .Hot}}</section>`
	if theme == "suxinlite" {
		recommendations = `<section class="ksec wrap"><div class="ksec-h"><b>最近更新</b><a href="{{.RecentMoreURL}}">更多 ›</a></div>{{template "cards" .Recent}}</section><section class="ksec wrap"><div class="ksec-h"><b>热播</b><a href="{{.HotMoreURL}}">更多 ›</a></div>{{template "cards" .Hot}}</section>`
	}
	return body + recommendations + sections + homeHeroScript
}
