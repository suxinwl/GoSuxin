package suxinvideo

import (
	"context"
	"time"

	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/utility/gf"
)

type CleanupReq struct {
	g.Meta `path:"/cleanup" method:"post"`
	Table  string `p:"table"`
	Status int    `p:"status"`
	Days   int    `p:"days"`
	Mode   int    `p:"mode"`
}
type CleanupRes struct{}

func (*Admin) Cleanup(ctx context.Context, req *CleanupReq) (*CleanupRes, error) {
	r := g.RequestFromCtx(ctx)
	if req.Table != "order" && req.Table != "comment" {
		badRequest(r, "清理类型无效")
		return &CleanupRes{}, nil
	}
	allowed, err := requireResource(ctx, r, req.Table, "cleanup")
	if err != nil {
		return nil, err
	}
	if !allowed {
		return &CleanupRes{}, nil
	}
	var query string
	var args []any
	if req.Table == "order" {
		if (req.Status != 0 && req.Status != 2) || req.Days < 0 || req.Days > 36500 {
			badRequest(r, "仅可清理待支付或已取消订单")
			return &CleanupRes{}, nil
		}
		query = "DELETE FROM sx_order WHERE status=?"
		args = append(args, req.Status)
		if req.Days > 0 {
			query += " AND created<?"
			args = append(args, time.Now().Unix()-int64(req.Days)*86400)
		}
	} else {
		if req.Mode != 0 && req.Mode != 1 {
			badRequest(r, "评论清理方式无效")
			return &CleanupRes{}, nil
		}
		query = "DELETE FROM sx_comment"
		if req.Mode == 0 {
			query += " WHERE status=0"
		}
	}
	result, err := g.DB().Exec(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	r.Response.WriteJson(gf.Success().SetData(map[string]any{"deleted": n}))
	return &CleanupRes{}, nil
}
