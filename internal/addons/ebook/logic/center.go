package album

import (
	"context"
	"errors"
	"github.com/suxinwl/GoSuxin/framework/container/gvar"
	"github.com/suxinwl/GoSuxin/framework/database/gdb"
	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/net/ghttp"
	"github.com/suxinwl/GoSuxin/framework/os/gtime"
	"golang.org/x/crypto/bcrypt"
	"strings"
	"time"
)

func Categories(ctx context.Context) (gdb.Result, error) {
	return g.Model("album_category").Ctx(ctx).Order("sort_order ASC,id ASC").All()
}
func SaveCategory(ctx context.Context, id int64, name, en, description string, order int, englishDescription ...string) (int64, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return 0, errors.New("请输入分类名称")
	}
	err := g.DB().Transaction(ctx, func(ctx context.Context, tx gdb.TX) error {
		data := g.Map{"name": name, "english_title": strings.TrimSpace(en), "description": strings.TrimSpace(description), "sort_order": order, "updatetime": gtime.Now()}
		if len(englishDescription) > 0 {
			data["english_description"] = strings.TrimSpace(englishDescription[0])
		}
		if id == 0 {
			key, err := NewKey()
			if err != nil {
				return err
			}
			data["stable_key"] = "category-" + key[:12]
			data["createtime"] = gtime.Now()
			id, err = tx.Model("album_category").Data(data).InsertAndGetId()
			return err
		}
		row, err := tx.Model("album_category").Where("id", id).LockUpdate().One()
		if err != nil {
			return err
		}
		if row.IsEmpty() {
			return errors.New("分类不存在")
		}
		if _, err := tx.Model("album_category").Where("id", id).Data(data).Update(); err != nil {
			return err
		}
		_, err = tx.Model("album").Where("category_id", id).Data(g.Map{"category": name}).Update()
		return err
	})
	return id, err
}
func DeleteCategory(ctx context.Context, id int64) error {
	assetLifecycle.RLock()
	defer func() { assetLifecycle.RUnlock(); flushAssetDeletesSoon(0) }()
	return g.DB().Transaction(ctx, func(ctx context.Context, tx gdb.TX) error {
		row, err := tx.Model("album_category").Where("id", id).LockUpdate().One()
		if err != nil {
			return err
		}
		if row.IsEmpty() {
			return errors.New("分类不存在")
		}
		count, err := tx.Model("album").Where("category_id", id).Count()
		if err != nil {
			return err
		}
		if count > 0 {
			return errors.New("分类仍有画册，请先调整画册归属")
		}
		if err = queueAssetDeletes(ctx, tx, 0, []string{row["cover_url"].String(), row["english_cover_url"].String()}); err != nil {
			return err
		}
		_, err = tx.Model("album_category").Where("id", id).Delete()
		return err
	})
}
func EditableAlbum(ctx context.Context, tx gdb.TX, id int64) (gdb.Record, error) {
	row, err := lockedAlbum(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if row["status"].String() == "published" {
		return nil, errors.New("请先下架画册再修改图片")
	}
	return row, nil
}
func Pages(ctx context.Context, id int64) (gdb.Record, gdb.Result, error) {
	row, err := OwnAlbumModel(ctx, id).One()
	if err != nil {
		return nil, nil, err
	}
	if row.IsEmpty() {
		return nil, nil, errors.New("画册不存在或无权访问")
	}
	pages, err := g.Model("album_page").Ctx(ctx).Where("album_id", id).Order("page_no ASC").All()
	if err != nil {
		return nil, nil, err
	}
	if pages == nil {
		pages = gdb.Result{}
	}
	for _, p := range pages {
		p["is_cover"] = gvar.New(p["image_url"].String() == row["cover_url"].String())
	}
	cleanup, err := AssetDeleteStatus(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	row["cloud_cleanup"] = gvar.New(cleanup)
	PresentAlbum(row, pages, NewFileGrant(id, OwnerID(ctx), 0))
	return row, pages, nil
}
func updatePageSummary(tx gdb.TX, id int64, cover string) error {
	pages, err := tx.Model("album_page").Where("album_id", id).Order("page_no ASC").All()
	if err != nil {
		return err
	}
	found := false
	for _, p := range pages {
		if p["image_url"].String() == cover {
			found = true
		}
	}
	if !found {
		cover = ""
		if len(pages) > 0 {
			cover = pages[0]["image_url"].String()
		}
	}
	status := "ready"
	if len(pages) == 0 {
		status = "draft"
	}
	_, err = tx.Model("album").Where("id", id).Data(g.Map{"page_count": len(pages), "cover_url": cover, "status": status, "failure_reason": "", "updatetime": gtime.Now()}).Update()
	return err
}
func EditPages(ctx context.Context, id int64, action string, ids []int64, file *ghttp.UploadFile) error {
	assetLifecycle.RLock()
	committed := false
	defer func() {
		assetLifecycle.RUnlock()
		if committed && (action == "delete" || action == "replace") {
			flushAssetDeletesSoon(id)
		}
	}()

	var location, temporary, previous string
	var target StorageProfile
	var version int64
	if action == "replace" {
		if len(ids) != 1 {
			return errors.New("请选择一个页面")
		}
		var err error
		version, err = prepareImageEdit(ctx, id)
		if err != nil {
			return err
		}
		target, err = AlbumStorageTarget(ctx, id)
		if err != nil {
			return err
		}
		page, err := g.Model("album_page").Ctx(ctx).Where("album_id", id).Where("id", ids[0]).One()
		if err != nil {
			return err
		}
		if page.IsEmpty() {
			return errors.New("页面不存在")
		}
		previous = page["image_url"].String()
		temporary, err = savePrivateUpload(id, file, false)
		if err != nil {
			return err
		}
		location, err = stageImage(ctx, id, temporary, target)
		if err != nil {
			return err
		}
	}
	err := g.DB().Transaction(ctx, func(ctx context.Context, tx gdb.TX) error {
		row, err := EditableAlbum(ctx, tx, id)
		if err != nil {
			return err
		}
		pages, err := tx.Model("album_page").Where("album_id", id).Order("page_no ASC").All()
		if err != nil {
			return err
		}
		byID := map[int64]gdb.Record{}
		for _, p := range pages {
			byID[p["id"].Int64()] = p
		}
		seen := map[int64]bool{}
		for _, pid := range ids {
			if seen[pid] || byID[pid] == nil {
				return errors.New("页面列表包含重复项或不属于该画册")
			}
			seen[pid] = true
		}
		if len(ids) == 0 {
			return errors.New("请选择页面")
		}
		removed := []string{}
		if action == "delete" || action == "replace" {
			for _, pid := range ids {
				removed = append(removed, byID[pid]["image_url"].String(), byID[pid]["thumbnail_url"].String())
			}
		}
		cover := row["cover_url"].String()
		switch action {
		case "reorder":
			if len(ids) != len(pages) {
				return errors.New("页面已变化，请刷新后重新排序")
			}
			// Temporary negative positions avoid the unique (album_id,page_no) constraint.
			if _, err := tx.Model("album_page").Where("album_id", id).Data(g.Map{"page_no": gdb.Raw("-page_no")}).Update(); err != nil {
				return err
			}
			for i, pid := range ids {
				if _, err := tx.Model("album_page").Where("id", pid).Data(g.Map{"page_no": i + 1}).Update(); err != nil {
					return err
				}
			}
		case "delete":
			if _, err := tx.Model("album_page").Where("album_id", id).WhereIn("id", ids).Delete(); err != nil {
				return err
			}
			remaining, err := tx.Model("album_page").Where("album_id", id).Order("page_no ASC").All()
			if err != nil {
				return err
			}
			for i, p := range remaining {
				if _, err := tx.Model("album_page").Where("id", p["id"]).Data(g.Map{"page_no": i + 1}).Update(); err != nil {
					return err
				}
			}
		case "cover":
			if len(ids) != 1 {
				return errors.New("请选择一张封面")
			}
			cover = byID[ids[0]]["image_url"].String()
		case "replace":
			if len(ids) != 1 {
				return errors.New("请选择一个页面")
			}
			if row["edit_version"].Int64() != version || byID[ids[0]]["image_url"].String() != previous {
				return errors.New("页面已变化，请刷新后再替换")
			}
			if cover == byID[ids[0]]["image_url"].String() {
				cover = location
			}
			if _, err := tx.Model("album_page").Where("id", ids[0]).Data(g.Map{"image_url": location, "thumbnail_url": location, "size_bytes": file.Size}).Update(); err != nil {
				return err
			}
		default:
			return errors.New("无效页面操作")
		}
		if _, err := tx.Model("album").Where("id", id).Data(g.Map{"edit_version": gdb.Raw("edit_version+1")}).Update(); err != nil {
			return err
		}
		if err = updatePageSummary(tx, id, cover); err != nil {
			return err
		}
		return queueAssetDeletes(ctx, tx, id, removed)
	})
	committed = err == nil
	if err == nil && target.ID != "" {
		cleanupTemporary(temporary)
	}
	return err
}

func SharePassword(mode, password string) (hash, plain string, err error) {
	switch mode {
	case "none":
		return "", "", nil
	case "random":
		key, e := NewKey()
		if e != nil {
			return "", "", e
		}
		password = key[:12]
	case "custom":
		if len(password) < 4 || len(password) > 72 {
			return "", "", errors.New("密码长度须为4至72字节")
		}
	default:
		return "", "", errors.New("无效密码设置")
	}
	b, e := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(b), password, e
}
func ShareExpiry(expires *gtime.Time, permanent bool) (*gtime.Time, error) {
	if permanent {
		return nil, nil
	}
	if expires == nil {
		expires = gtime.New(time.Now().Add(7 * 24 * time.Hour))
	}
	if !expires.After(gtime.Now()) {
		return nil, errors.New("有效期必须晚于当前时间")
	}
	return expires, nil
}
func ShareList(ctx context.Context, id int64) (gdb.Result, error) {
	row, err := OwnAlbumModel(ctx, id).One()
	if err != nil {
		return nil, err
	}
	if row.IsEmpty() {
		return nil, errors.New("画册不存在或无权访问")
	}
	shares, err := g.Model("album_share").Ctx(ctx).Where("album_id", id).Order("id DESC").All()
	for _, s := range shares {
		s["has_password"] = gvar.New(s["password_hash"].String() != "")
		delete(s, "password_hash")
	}
	return shares, err
}
func UpdateShare(ctx context.Context, albumID, id int64, mode, password string, expires *gtime.Time, permanent, enabled bool) (string, error) {
	var hash, plain string
	var err error
	if mode != "keep" {
		hash, plain, err = SharePassword(mode, password)
		if err != nil {
			return "", err
		}
	}
	if !permanent && expires == nil {
		return "", errors.New("请选择有效期或永久有效")
	}
	if enabled {
		expires, err = ShareExpiry(expires, permanent)
		if err != nil {
			return "", err
		}
	} else if permanent {
		expires = nil
	}
	err = g.DB().Transaction(ctx, func(ctx context.Context, tx gdb.TX) error {
		row, err := tx.Model("album").Where("id", albumID).LockUpdate().One()
		if err != nil {
			return err
		}
		if row.IsEmpty() || (row["owner_id"].Int64() != OwnerID(ctx) && !CanManageAll(ctx)) {
			return errors.New("画册不存在或无权操作")
		}
		share, err := tx.Model("album_share").Where("id", id).Where("album_id", albumID).LockUpdate().One()
		if err != nil {
			return err
		}
		if share.IsEmpty() {
			return errors.New("分享不存在")
		}
		data := g.Map{"enabled": enabled, "expires_at": expires, "auth_version": gdb.Raw("auth_version+1")}
		if mode != "keep" {
			data["password_hash"] = hash
		}
		_, err = tx.Model("album_share").Where("id", id).Data(data).Update()
		return err
	})
	return plain, err
}
