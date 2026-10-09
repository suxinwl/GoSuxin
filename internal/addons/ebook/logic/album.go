package album

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"path/filepath"
	"strings"
	"time"

	"github.com/suxinwl/GoSuxin/framework/database/gdb"
	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/os/gtime"
)

// EnsureSchema runs at startup (or before import), never on the request path.
func EnsureSchema(ctx context.Context) error {
	db := g.DB()
	statements := []string{
		`CREATE TABLE IF NOT EXISTS gf_album (
			id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
			owner_id BIGINT UNSIGNED NOT NULL, title VARCHAR(180) NOT NULL,
			description TEXT NULL, category VARCHAR(80) NOT NULL DEFAULT '',
			source_type VARCHAR(16) NOT NULL, visibility VARCHAR(16) NOT NULL DEFAULT 'private',
			status VARCHAR(20) NOT NULL DEFAULT 'draft', cover_url VARCHAR(500) NULL,
			original_url VARCHAR(500) NULL, page_count INT NOT NULL DEFAULT 0,
			sort_order INT NOT NULL DEFAULT 0, published_at DATETIME NULL,
			failure_reason VARCHAR(500) NULL, source_key VARCHAR(255) NULL,
			createtime DATETIME NOT NULL, updatetime DATETIME NULL,
			UNIQUE KEY uq_album_source_key (source_key), KEY idx_album_public (visibility,status,published_at),
			KEY idx_album_owner (owner_id,createtime)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS gf_album_page (
			id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY, album_id BIGINT UNSIGNED NOT NULL,
			page_no INT NOT NULL, image_url VARCHAR(500) NOT NULL, thumbnail_url VARCHAR(500) NULL,
			width INT NOT NULL DEFAULT 0, height INT NOT NULL DEFAULT 0, size_bytes BIGINT NOT NULL DEFAULT 0,
			createtime DATETIME NOT NULL, UNIQUE KEY uq_album_page (album_id,page_no), KEY idx_page_album (album_id)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS gf_album_share (
			id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY, album_id BIGINT UNSIGNED NOT NULL,
			share_key VARCHAR(64) NOT NULL, password_hash VARCHAR(255) NULL, expires_at DATETIME NULL,
			enabled TINYINT NOT NULL DEFAULT 1, visit_count INT NOT NULL DEFAULT 0,
			createtime DATETIME NOT NULL, UNIQUE KEY uq_album_share_key (share_key), KEY idx_share_album (album_id)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS gf_album_visit (
			id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY, album_id BIGINT UNSIGNED NOT NULL,
			share_id BIGINT UNSIGNED NULL, ip_hash CHAR(64) NULL, user_agent VARCHAR(500) NULL,
			createtime DATETIME NOT NULL, KEY idx_visit_album (album_id,createtime)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(ctx, statement); err != nil {
			return err
		}
	}
	if err := MigrateStorage(ctx); err != nil {
		return err
	}
	return MigrateCenter(ctx)
}

func OwnerID(ctx context.Context) int64 { return g.RequestFromCtx(ctx).GetCtxVar("uid").Int64() }

func CanManageAll(ctx context.Context) bool {
	roleIDs, err := g.Model("auth_role_access").Ctx(ctx).Where("uid", OwnerID(ctx)).Array("role_id")
	if err != nil || len(roleIDs) == 0 {
		return false
	}
	count, err := g.Model("auth_role").Ctx(ctx).WhereIn("id", roleIDs).Where("rules", "*").Count()
	return err == nil && count > 0
}

func OwnAlbumModel(ctx context.Context, id int64) *gdb.Model {
	model := g.Model("album").Ctx(ctx).Where("id", id)
	if !CanManageAll(ctx) {
		model = model.Where("owner_id", OwnerID(ctx))
	}
	return model
}

func NewKey() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func IsPublic(row gdb.Record) bool {
	return row["visibility"].String() == "public" && row["status"].String() == "published"
}

func PublicAlbum(ctx context.Context, id int64) (gdb.Record, error) {
	return g.Model("album").Ctx(ctx).Where("id", id).Where("visibility", "public").Where("status", "published").Where("language", SiteLanguage(ctx)).One()
}

func AccessibleShare(ctx context.Context, key string) (gdb.Record, gdb.Record, error) {
	share, err := g.Model("album_share").Ctx(ctx).Where("share_key", key).Where("enabled", 1).One()
	if err != nil || share.IsEmpty() {
		return nil, nil, errors.New("分享链接不存在或已失效")
	}
	if !share["expires_at"].IsNil() && share["expires_at"].Time().Before(time.Now()) {
		return nil, nil, errors.New("分享链接已过期")
	}
	album, err := g.Model("album").Ctx(ctx).Where("id", share["album_id"]).Where("status", "published").One()
	if err != nil || album.IsEmpty() {
		return nil, nil, errors.New("画册不可用")
	}
	return album, share, nil
}

func NormalizeImageName(name string) (string, error) {
	name = filepath.Base(name)
	ext := strings.ToLower(filepath.Ext(name))
	if ext != ".jpg" && ext != ".jpeg" && ext != ".png" && ext != ".webp" {
		return "", errors.New("仅支持 JPG、PNG、WEBP 图片")
	}
	return name, nil
}

func CreateShare(ctx context.Context, albumID int64, passwordHash string, expiresAt *gtime.Time) (gdb.Record, error) {
	key, err := NewKey()
	if err != nil {
		return nil, err
	}
	id, err := g.Model("album_share").Ctx(ctx).Data(g.Map{"album_id": albumID, "share_key": key, "password_hash": passwordHash, "expires_at": expiresAt, "enabled": 1, "createtime": gtime.Now()}).InsertAndGetId()
	if err != nil {
		return nil, err
	}
	return g.Model("album_share").Ctx(ctx).Where("id", id).One()
}
