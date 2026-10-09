package suxinvideo

import (
	"context"
	"fmt"
	"net/mail"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/suxinwl/GoSuxin/framework/database/gdb"
	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/util/gconv"
	"github.com/suxinwl/GoSuxin/utility/gf"
	"golang.org/x/crypto/bcrypt"
)

type DashboardReq struct {
	g.Meta `path:"/dashboard" method:"get"`
}
type DashboardRes struct{}
type ClearCacheReq struct {
	g.Meta `path:"/clearCache" method:"post"`
}
type ClearCacheRes struct{}
type DeleteBatchReq struct {
	g.Meta `path:"/deleteBatch" method:"post"`
	Table  string  `p:"table"`
	IDs    []int64 `p:"ids"`
}
type DeleteBatchRes struct{}
type UserSaveReq struct {
	g.Meta   `path:"/user/save" method:"post"`
	ID       int64          `p:"id"`
	Data     map[string]any `p:"data"`
	Password string         `p:"password"`
}
type UserSaveRes struct{}
type UserDeleteReq struct {
	g.Meta `path:"/user/delete" method:"post"`
	ID     int64 `p:"id"`
}
type UserDeleteRes struct{}
type UserUnlockReq struct {
	g.Meta `path:"/user/unlock" method:"post"`
}
type UserUnlockRes struct{}

func (*Admin) Dashboard(ctx context.Context, _ *DashboardReq) (*DashboardRes, error) {
	counts, err := one(ctx, `SELECT
		(SELECT COUNT(*) FROM sx_vod) vod,
		(SELECT COUNT(*) FROM sx_vod WHERE addtime>=?) today_vod,
		(SELECT COUNT(*) FROM sx_user) user,
		(SELECT COUNT(*) FROM sx_user WHERE reg_time>=?) today_user,
		(SELECT COUNT(*) FROM sx_order WHERE status=1) order_paid,
		(SELECT COALESCE(SUM(amount),0) FROM sx_order WHERE status=1) order_money,
		(SELECT COALESCE(SUM(amount),0) FROM sx_order WHERE status=1 AND paid_time>=?) today_money,
		(SELECT COUNT(*) FROM sx_comment) comment`, todayStart(), todayStart(), todayStart())
	if err != nil {
		return nil, err
	}
	week := make([]row, 0, 7)
	for i := 6; i >= 0; i-- {
		start := todayStart() - int64(i)*86400
		vod, e := one(ctx, "SELECT COUNT(*) n FROM sx_vod WHERE addtime>=? AND addtime<?", start, start+86400)
		if e != nil {
			return nil, e
		}
		money, e := one(ctx, "SELECT COALESCE(SUM(amount),0) n FROM sx_order WHERE status=1 AND paid_time>=? AND paid_time<?", start, start+86400)
		if e != nil {
			return nil, e
		}
		week = append(week, row{"date": time.Unix(start, 0).Format("01-02"), "vod": vod["n"], "money": money["n"]})
	}
	orders, err := all(ctx, "SELECT o.id,o.order_no,o.title,o.amount,o.pay_type,o.status,o.created,o.paid_time,u.name,u.email FROM sx_order o LEFT JOIN sx_user u ON u.id=o.user_id ORDER BY o.id DESC LIMIT 10")
	if err != nil {
		return nil, err
	}
	g.RequestFromCtx(ctx).Response.WriteJson(gf.Success().SetData(row{"stats": counts, "week": week, "orders": orders}))
	return &DashboardRes{}, nil
}

func todayStart() int64 {
	now := time.Now()
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).Unix()
}

func (*Admin) ClearCache(ctx context.Context, _ *ClearCacheReq) (*ClearCacheRes, error) {
	used, err := imageReferences(ctx)
	if err != nil {
		return nil, err
	}
	files, err := listCachedImages()
	if err != nil {
		return nil, err
	}
	removed := 0
	for name := range files {
		if used[name] {
			continue
		}
		if err := os.Remove(filepath.Join(cacheImageDir(), name)); err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		removed++
	}
	g.RequestFromCtx(ctx).Response.WriteJson(gf.Success().SetData(map[string]int{"removed": removed}))
	return &ClearCacheRes{}, nil
}

func (*Admin) DeleteBatch(ctx context.Context, req *DeleteBatchReq) (*DeleteBatchRes, error) {
	r := g.RequestFromCtx(ctx)
	if (req.Table != "vod" && req.Table != "user") || len(req.IDs) < 1 || len(req.IDs) > 100 {
		badRequest(r, "批量删除参数无效")
		return &DeleteBatchRes{}, nil
	}
	allowed, err := requireResource(ctx, r, req.Table, "deleteBatch")
	if err != nil {
		return nil, err
	}
	if !allowed {
		return &DeleteBatchRes{}, nil
	}
	for _, id := range req.IDs {
		if id < 1 {
			badRequest(r, "无效 ID")
			return &DeleteBatchRes{}, nil
		}
	}
	err = g.DB().Transaction(ctx, func(ctx context.Context, tx gdb.TX) error {
		for _, id := range req.IDs {
			if req.Table == "user" {
				for _, table := range []string{"sx_fav", "sx_comment", "sx_play_record", "sx_user_vod", "sx_sign", "sx_order"} {
					if _, e := tx.Exec("DELETE FROM "+table+" WHERE user_id=?", id); e != nil {
						return e
					}
				}
			} else {
				for _, table := range []string{"sx_comment", "sx_fav", "sx_play_record", "sx_user_vod"} {
					if _, e := tx.Exec("DELETE FROM "+table+" WHERE vod_id=?", id); e != nil {
						return e
					}
				}
			}
			if _, e := tx.Exec("DELETE FROM sx_"+req.Table+" WHERE id=?", id); e != nil {
				return e
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if req.Table == "vod" {
		if err := invalidateAppFilmCatalog(ctx); err != nil {
			return nil, err
		}
	}
	r.Response.WriteJson(gf.Success().SetData(true))
	return &DeleteBatchRes{}, nil
}

func (*Admin) UserSave(ctx context.Context, req *UserSaveReq) (*UserSaveRes, error) {
	r := g.RequestFromCtx(ctx)
	allowed, permissionErr := requireResource(ctx, r, "user", "save")
	if permissionErr != nil {
		return nil, permissionErr
	}
	if !allowed {
		return &UserSaveRes{}, nil
	}
	name := strings.TrimSpace(gconv.String(req.Data["name"]))
	email := strings.ToLower(strings.TrimSpace(gconv.String(req.Data["email"])))
	points := gconv.Int64(req.Data["points"])
	status := 1
	if raw, ok := req.Data["status"]; ok {
		status = gconv.Int(raw)
	}
	if status != 0 && status != 1 {
		badRequest(r, "会员状态无效")
		return &UserSaveRes{}, nil
	}
	if req.ID == 0 {
		if _, err := mail.ParseAddress(email); err != nil || len(req.Password) < 6 {
			badRequest(r, "邮箱或密码无效")
			return &UserSaveRes{}, nil
		}
	}
	if len(name) > 60 || points < 0 || gconv.Int64(req.Data["vip_expire"]) < 0 || gconv.Int64(req.Data["vip_days"]) < 0 || gconv.Int64(req.Data["vip_days"]) > 36500 {
		badRequest(r, "会员信息无效")
		return &UserSaveRes{}, nil
	}
	if req.ID > 0 {
		if name == "" {
			badRequest(r, "昵称不能为空")
			return &UserSaveRes{}, nil
		}
		if err := execSQL(ctx, "UPDATE sx_user SET name=?,points=?,vip_expire=?,status=? WHERE id=?", name, points, req.Data["vip_expire"], status, req.ID); err != nil {
			return nil, err
		}
		if req.Password != "" {
			if len(req.Password) < 6 {
				badRequest(r, "密码至少6位")
				return &UserSaveRes{}, nil
			}
			hash, e := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
			if e != nil {
				return nil, e
			}
			if e = execSQL(ctx, "UPDATE sx_user SET pwd=? WHERE id=?", string(hash), req.ID); e != nil {
				return nil, e
			}
		}
	} else {
		if name == "" {
			name = strings.Split(email, "@")[0]
		}
		hash, e := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
		if e != nil {
			return nil, e
		}
		vipExpire := int64(0)
		if days := gconv.Int64(req.Data["vip_days"]); days > 0 {
			vipExpire = time.Now().Unix() + days*86400
		}
		if e = execSQL(ctx, "INSERT INTO sx_user(email,name,pwd,points,vip_expire,status,email_verified,reg_time,reg_ip) VALUES(?,?,?,?,?,?,1,?,?)", email, name, string(hash), points, vipExpire, status, time.Now().Unix(), cmsClientIP(ctx, r)); e != nil {
			return nil, fmt.Errorf("添加会员: %w", e)
		}
	}
	r.Response.WriteJson(gf.Success().SetData(true))
	return &UserSaveRes{}, nil
}

func (*Admin) UserDelete(ctx context.Context, req *UserDeleteReq) (*UserDeleteRes, error) {
	r := g.RequestFromCtx(ctx)
	if req.ID < 1 {
		badRequest(r, "会员 ID 无效")
		return &UserDeleteRes{}, nil
	}
	allowed, err := requireResource(ctx, r, "user", "delete")
	if err != nil {
		return nil, err
	}
	if !allowed {
		return &UserDeleteRes{}, nil
	}
	if err := execSQL(ctx, "DELETE FROM sx_user WHERE id=?", req.ID); err != nil {
		return nil, err
	}
	r.Response.WriteJson(gf.Success().SetData(true))
	return &UserDeleteRes{}, nil
}

func (*Admin) UserUnlock(ctx context.Context, _ *UserUnlockReq) (*UserUnlockRes, error) {
	if err := execSQL(ctx, "DELETE FROM sx_login_fail WHERE type='user'"); err != nil {
		return nil, err
	}
	g.RequestFromCtx(ctx).Response.WriteJson(gf.Success().SetData(true))
	return &UserUnlockRes{}, nil
}
