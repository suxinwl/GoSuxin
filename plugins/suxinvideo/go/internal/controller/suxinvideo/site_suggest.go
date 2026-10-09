package suxinvideo

import (
	"context"
	"strings"

	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/util/gconv"
)

type SuggestReq struct {
	g.Meta `path:"/suggest" method:"get" noValApi:"1"`
	WD     string `p:"wd"`
}
type SuggestRes struct{}

func (*Site) Suggest(ctx context.Context, req *SuggestReq) (*SuggestRes, error) {
	word := strings.TrimSpace(req.WD)
	if len([]rune(word)) > 60 {
		word = string([]rune(word)[:60])
	}
	items := []row{}
	if word != "" {
		var err error
		items, err = all(ctx, "SELECT id,name,pic,remarks,year,area FROM sx_vod WHERE "+publicVodListingCondition(ctx, "")+" AND name LIKE ? ORDER BY total_hits DESC,id DESC LIMIT 8", "%"+word+"%")
		if err != nil {
			return nil, err
		}
		for _, item := range items {
			pic := gconv.String(item["pic"])
			if strings.HasPrefix(pic, "http://") || strings.HasPrefix(pic, "https://") {
				item["pic"] = imageLink(ctx, pic)
			}
		}
	}
	g.RequestFromCtx(ctx).Response.WriteJson(map[string]any{"code": 1, "data": items})
	return &SuggestRes{}, nil
}
