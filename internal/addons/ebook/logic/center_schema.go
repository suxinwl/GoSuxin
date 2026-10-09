package album

import (
	"context"
	"fmt"
	"github.com/suxinwl/GoSuxin/framework/database/gdb"
	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/os/gtime"
)

func MigrateCenter(ctx context.Context) error {
	for _, sql := range []string{
		`CREATE TABLE IF NOT EXISTS gf_album_migration (version VARCHAR(80) PRIMARY KEY, created_at DATETIME NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS gf_album_category (id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY, stable_key VARCHAR(64) NOT NULL UNIQUE, name VARCHAR(80) NOT NULL UNIQUE, english_title VARCHAR(120) NOT NULL DEFAULT '', description VARCHAR(500) NOT NULL DEFAULT '', sort_order INT NOT NULL DEFAULT 0, createtime DATETIME NOT NULL, updatetime DATETIME NULL) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	} {
		if _, err := g.DB().Exec(ctx, sql); err != nil {
			return err
		}
	}
	for _, column := range []struct{ table, name, definition string }{
		{"gf_album", "category_id", "BIGINT UNSIGNED NULL"}, {"gf_album_share", "auth_version", "BIGINT NOT NULL DEFAULT 1"},
		{"gf_album", "language", "VARCHAR(8) NOT NULL DEFAULT 'zh'"},
		{"gf_album_category", "english_description", "VARCHAR(500) NOT NULL DEFAULT ''"},
		{"gf_album_category", "cover_url", "VARCHAR(500) NOT NULL DEFAULT ''"},
		{"gf_album_category", "english_cover_url", "VARCHAR(500) NOT NULL DEFAULT ''"},
	} {
		n, err := g.DB().GetValue(ctx, `SELECT COUNT(*) FROM information_schema.columns WHERE table_schema=DATABASE() AND table_name=? AND column_name=?`, column.table, column.name)
		if err != nil {
			return err
		}
		if n.Int() == 0 {
			if _, err := g.DB().Exec(ctx, fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", column.table, column.name, column.definition)); err != nil {
				return err
			}
			if err := g.DB().GetCore().ClearTableFields(ctx, column.table); err != nil {
				return err
			}
		}
	}
	err := g.DB().Transaction(ctx, func(ctx context.Context, tx gdb.TX) error {
		done, err := tx.Model("album_migration").Where("version", "center-v1-categories").Count()
		if err != nil {
			return err
		}
		if done > 0 {
			return nil
		}
		defaults := []struct{ key, name, en, description string }{
			{"company", "公司简介", "ABOUT US", "品牌介绍与服务资料。"},
			{"equipment", "设备产品", "EQUIPMENT", "设备与产品画册。"},
			{"consumables", "耗材产品", "CONSUMABLES", "耗材及配套产品画册。"},
		}
		for i, c := range defaults {
			if _, err := tx.Model("album_category").Data(g.Map{"stable_key": c.key, "name": c.name, "english_title": c.en, "description": c.description, "sort_order": i, "createtime": gtime.Now()}).InsertIgnore(); err != nil {
				return err
			}
		}
		names, err := tx.Model("album").Distinct().Array("category")
		if err != nil {
			return err
		}
		for _, name := range names {
			label := name.String()
			if label == "" {
				label = "未分类"
			}
			count, err := tx.Model("album_category").Where("name", label).Count()
			if err != nil {
				return err
			}
			if count == 0 {
				key, err := NewKey()
				if err != nil {
					return err
				}
				if _, err := tx.Model("album_category").Data(g.Map{"stable_key": "category-" + key[:12], "name": label, "createtime": gtime.Now()}).Insert(); err != nil {
					return err
				}
			}
			category, err := tx.Model("album_category").Where("name", label).One()
			if err != nil {
				return err
			}
			if _, err := tx.Model("album").Where("category", name).Data(g.Map{"category_id": category["id"], "category": label}).Update(); err != nil {
				return err
			}
		}
		_, err = tx.Model("album_migration").Data(g.Map{"version": "center-v1-categories", "created_at": gtime.Now()}).Insert()
		return err
	})
	if err != nil {
		return err
	}
	// Replace only the old bundled default; preserve customer-edited categories.
	if _, err := g.Model("album_category").Ctx(ctx).Where("stable_key", "company").Where("english_title", "ABOUT SUXIN").Data(g.Map{"english_title": "ABOUT US"}).Update(); err != nil {
		return err
	}
	return SeedCenterMenus(ctx)
}

func SeedCenterMenus(ctx context.Context) error {
	exists, err := g.DB().GetValue(ctx, "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE() AND table_name='gf_auth_rule'")
	if err != nil {
		return err
	}
	if exists.Int() == 0 {
		return nil
	}
	return g.DB().Transaction(ctx, func(ctx context.Context, tx gdb.TX) error {
		add := func(name, title, route, component, api string, pid int64, kind, order int) (int64, error) {
			row, err := tx.Model("auth_rule").Where("routename", name).One()
			if err != nil {
				return 0, err
			}
			if !row.IsEmpty() {
				if kind == 1 && row["component"].String() == "PLUGIN:ebook" {
					if _, err := tx.Model("auth_rule").Where("id", row["id"]).Data(g.Map{"component": component}).Update(); err != nil {
						return 0, err
					}
				}
				return row["id"].Int64(), nil
			}
			data := g.Map{"uid": 1, "title": title, "des": "", "locale": "", "weigh": order, "type": kind, "pid": pid, "icon": "icon-book", "routepath": route, "routename": name, "component": component, "redirect": "", "path": api, "permission": name, "status": 0, "requiresauth": 1, "hideinmenu": 0, "createtime": gtime.Now()}
			if kind == 0 {
				data["redirect"] = "/albums-admin/manage"
			}
			return tx.Model("auth_rule").Data(data).InsertAndGetId()
		}
		root, err := add("albumCenter", "画册中心", "/albums-admin", "LAYOUT", "", 0, 0, 2)
		if err != nil {
			return err
		}
		for i, m := range []struct {
			name, title, route, component string
			actions                       []string
		}{
			{"albumCategories", "分类管理", "categories", "/album/categories/index", []string{"category/list", "category/save", "category/delete", "category/cover"}},
			{"albumManage", "画册管理", "manage", "/album/manage/index", []string{"list", "detail", "save", "publish", "pdf/upload", "pdf/retry"}},
			{"albumImages", "图片管理", "images", "/album/images/index", []string{"page/list", "page/add", "page/reorder", "page/replace", "page/delete", "page/cover", "page/cleanup/retry"}},
			{"albumShares", "分享管理", "shares", "/album/shares/index", []string{"share/list", "share/create", "share/update"}},
			{"albumSite", "站点设置", "site", "/album/site/index", []string{"site/get", "site/save", "site/logo"}},
			{"albumStorage", "存储设置", "storage", "/album/storage/index", []string{"storage/get", "storage/save", "storage/test", "storage/activate"}},
		} {
			id, err := add(m.name, m.title, m.route, m.component, "", root, 1, i)
			if err != nil {
				return err
			}
			for j, action := range m.actions {
				_, err := add("album:"+action, action, "", "", "/admin/album/"+action, id, 2, j)
				if err != nil {
					return err
				}
			}
		}
		return nil
	})
}
