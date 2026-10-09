// Adapted from the GoframePro privatecode 1.1.5 source plugin.
// Only schema and basic categories are installed; upstream demo content is excluded.
package privatecode

import (
	"context"
	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/net/ghttp"
	"github.com/suxinwl/GoSuxin/internal/addons/support"
	"github.com/suxinwl/GoSuxin/internal/plugins"
)

func init() {
	plugins.Register(plugins.Plugin{Name: "privatecode", Title: "私有插件仓", Version: "1.1.5", Description: "代码分类、源码包版本管理、发布与受权限保护的下载。", Entry: "/privatecode", MenuRoots: []string{"privatecode"}, Admin: []interface{}{&Controller{}}, Start: start})
}

func start(ctx context.Context, _ *ghttp.Server) (func(), error) {
	ready, err := support.Ready()
	if err != nil || !ready {
		return nil, err
	}
	if err = Initialize(ctx); err != nil {
		return nil, err
	}
	return func() {}, nil
}

func Initialize(ctx context.Context) error {
	for _, sql := range []string{
		`CREATE TABLE IF NOT EXISTS gf_privatecode_cate (id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY, stable_key VARCHAR(64) NULL UNIQUE, name VARCHAR(80) NOT NULL UNIQUE, remark VARCHAR(500) NOT NULL DEFAULT '', weigh INT NOT NULL DEFAULT 0, createtime DATETIME NOT NULL) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS gf_privatecode_content (id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY, owner_id BIGINT NOT NULL, cid BIGINT UNSIGNED NOT NULL, title VARCHAR(120) NOT NULL, name VARCHAR(64) NOT NULL UNIQUE, des VARCHAR(1000) NOT NULL DEFAULT '', content TEXT NOT NULL, author VARCHAR(80) NOT NULL DEFAULT '', version VARCHAR(32) NOT NULL DEFAULT '', status TINYINT NOT NULL DEFAULT 0, download BIGINT NOT NULL DEFAULT 0, createtime DATETIME NOT NULL, updatetime DATETIME NULL, KEY owner_status(owner_id,status), KEY category(cid)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS gf_privatecode_release (id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY, content_id BIGINT UNSIGNED NOT NULL, version VARCHAR(32) NOT NULL, filename VARCHAR(255) NOT NULL, file_key CHAR(64) NOT NULL UNIQUE, sha256 CHAR(64) NOT NULL, size BIGINT NOT NULL, note VARCHAR(1000) NOT NULL DEFAULT '', download BIGINT NOT NULL DEFAULT 0, createtime DATETIME NOT NULL, UNIQUE KEY package_version(content_id,version)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	} {
		if _, err := g.DB().Exec(ctx, sql); err != nil {
			return err
		}
	}
	// Stable-key seeding does not recreate a category that was renamed by the user.
	for i, c := range []struct{ key, name, remark string }{
		{"client", "客户端应用", "小程序、手机及桌面应用源码。"},
		{"business", "业务插件", "通用业务和行业功能。"},
		{"frontend", "前端组件", "页面模板及界面组件。"},
		{"backend", "后端插件", "接口、任务及服务端功能。"},
	} {
		if _, err := g.Model("privatecode_cate").Ctx(ctx).Data(g.Map{"stable_key": c.key, "name": c.name, "remark": c.remark, "weigh": i, "createtime": gtimeNow()}).InsertIgnore(); err != nil {
			return err
		}
	}
	return support.SeedMenu(ctx, "privatecode", "私有插件仓", "/privatecode/content/index", "icon-code-square", 4, []support.Action{
		{Path: "content/list", Title: "代码列表"}, {Path: "content/detail", Title: "版本详情"}, {Path: "content/save", Title: "保存代码资料"}, {Path: "content/delete", Title: "删除代码资料"},
		{Path: "cate/list", Title: "分类列表"}, {Path: "cate/save", Title: "保存分类"}, {Path: "cate/delete", Title: "删除分类"}, {Path: "release/upload", Title: "上传源码版本"}, {Path: "release/download", Title: "下载源码版本"},
	})
}
