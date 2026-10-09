package album

import (
	"context"
	"errors"
	"fmt"
	"github.com/suxinwl/GoSuxin/framework/database/gdb"
	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/os/gtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// One API process is enforced by album-worker.lock. Protect upload-to-commit
// windows from remote deletion, including deduplicated cloud file identities.
// This is NOT a database lock; no SQL transaction spans remote IO.
var assetLifecycle sync.RWMutex

func migrateAssetDeletes(ctx context.Context) error {
	_, err := g.DB().Exec(ctx, `CREATE TABLE IF NOT EXISTS gf_album_asset_delete (
 id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY, asset_id BIGINT UNSIGNED NOT NULL,
 album_id BIGINT UNSIGNED NOT NULL, state VARCHAR(16) NOT NULL DEFAULT 'pending',
 attempts INT NOT NULL DEFAULT 0, next_at DATETIME NULL, last_error VARCHAR(500) NOT NULL DEFAULT '',
 createtime DATETIME NOT NULL, updatetime DATETIME NULL,
 UNIQUE KEY uq_asset_delete(asset_id), KEY idx_delete_due(state,next_at), KEY idx_delete_album(album_id)
 ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`)
	return err
}

// Enqueue only files removed by an authorized mutation, in the SAME transaction.
// Never sweep historical unreferenced files or accept cloud IDs from clients.
func queueAssetDeletes(ctx context.Context, tx gdb.TX, albumID int64, locations []string) error {
	seen := map[string]bool{}
	for _, location := range locations {
		if seen[location] || !strings.HasPrefix(location, assetPrefix) {
			continue
		}
		seen[location] = true
		id, err := strconv.ParseInt(strings.TrimPrefix(location, assetPrefix), 10, 64)
		if err != nil {
			return err
		}
		a, err := tx.Model("album_asset").Where("id", id).Where("album_id", albumID).One()
		if err != nil {
			return err
		}
		if a.IsEmpty() || a["provider"].String() != "pan123" {
			continue
		}
		_, err = tx.Exec(`INSERT INTO gf_album_asset_delete(asset_id,album_id,state,createtime) VALUES(?,?,'pending',NOW()) ON DUPLICATE KEY UPDATE state=IF(state='done',state,'pending'),next_at=NULL,last_error=''`, id, albumID)
		if err != nil {
			return err
		}
	}
	return nil
}

func pageLocations(pages gdb.Result) []string {
	out := []string{}
	for _, page := range pages {
		out = append(out, page["image_url"].String(), page["thumbnail_url"].String())
	}
	return out
}

// A cloud ID is account-scoped. Multiple saved profiles can belong to one UID.
func cloudAliases(ctx context.Context, a gdb.Record, p StorageProfile) ([]string, []string, error) {
	storageConfigMu.Lock()
	cfg, err := readStorageConfig()
	storageConfigMu.Unlock()
	if err != nil {
		return nil, nil, err
	}
	profiles := []string{p.ID}
	for _, other := range cfg.Profiles {
		if other.ID != p.ID && ((p.UID > 0 && other.UID == p.UID) || (p.ClientID != "" && other.ClientID == p.ClientID)) {
			profiles = append(profiles, other.ID)
		}
	}
	records, err := g.Model("album_asset").Ctx(ctx).Where("provider", "pan123").Where("file_id", a["file_id"]).WhereIn("profile_id", profiles).All()
	if err != nil {
		return nil, nil, err
	}
	refs := []string{}
	for _, record := range records {
		refs = append(refs, assetPrefix+record["id"].String())
	}
	return refs, profiles, nil
}
func hasAssetReferences(ctx context.Context, locations []string) (bool, error) {
	if len(locations) == 0 {
		return true, errors.New("云盘资源记录不存在")
	}
	for _, query := range []struct {
		table  string
		fields []string
	}{{"album", []string{"cover_url", "original_url", "pending_pdf"}}, {"album_page", []string{"image_url", "thumbnail_url"}}, {"album_category", []string{"cover_url", "english_cover_url"}}} {
		for _, field := range query.fields {
			n, err := g.Model(query.table).Ctx(ctx).WhereIn(field, locations).Count()
			if err != nil {
				return true, err
			}
			if n > 0 {
				return true, nil
			}
		}
	}
	return false, nil
}
func (c *panClient) trashFile(ctx context.Context, id int64) error {
	if id <= 0 {
		return errors.New("无效云盘文件 ID")
	}
	info, err := c.detail(ctx, id)
	if err != nil {
		return err
	}
	if panNumber(info, "type") != 0 {
		return errors.New("禁止通过画册删除云盘目录")
	}
	if panNumber(info, "trashed") == 1 {
		return nil
	}
	if _, err = c.request(ctx, "POST", "https://open-api.123pan.com/api/v1/file/trash", g.Map{"fileIDs": []int64{id}}, true); err != nil {
		return err
	}
	info, err = c.detail(ctx, id)
	if err != nil {
		return err
	}
	if panNumber(info, "trashed") != 1 {
		return errors.New("云盘尚未确认文件已移入回收站")
	}
	return nil
}
func deleteCloudAsset(ctx context.Context, job gdb.Record) (string, error) {
	a, err := g.Model("album_asset").Ctx(ctx).Where("id", job["asset_id"]).One()
	if err != nil {
		return "", err
	}
	if a.IsEmpty() || a["provider"].String() != "pan123" || a["album_id"].Int64() != job["album_id"].Int64() {
		return "", errors.New("云盘删除任务与资源不匹配")
	}
	p, err := storageProfile(a["profile_id"].String())
	if err != nil {
		return "", err
	}
	aliases, profiles, err := cloudAliases(ctx, a, p)
	if err != nil {
		return "", err
	}
	referenced, err := hasAssetReferences(ctx, aliases)
	if err != nil {
		return "", err
	}
	if referenced {
		return "referenced", nil
	}
	if err = clientFor(p).trashFile(ctx, a["file_id"].Int64()); err != nil {
		return "", err
	}
	for _, profile := range profiles {
		directLinks.Delete(profile + ":" + a["file_id"].String())
	}
	return "done", nil
}

// Attempt immediately after commit; unfinished work is durable and resumes after restart.
func FlushAssetDeletes(ctx context.Context, albumID int64, force bool) error {
	if !assetLifecycle.TryLock() {
		return nil
	}
	defer assetLifecycle.Unlock()
	q := g.Model("album_asset_delete").Ctx(ctx).WhereIn("state", []string{"pending", "retry"})
	if albumID > 0 {
		q = q.Where("album_id", albumID)
	}
	if !force {
		q = q.Where("(next_at IS NULL OR next_at<=NOW())")
	}
	jobs, err := q.OrderAsc("id").Limit(10).All()
	if err != nil {
		return err
	}
	for _, job := range jobs {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		state, deleteErr := deleteCloudAsset(ctx, job)
		data := g.Map{"state": state, "attempts": job["attempts"].Int() + 1, "last_error": "", "next_at": nil, "updatetime": gtime.Now()}
		if deleteErr != nil {
			reason := []rune(deleteErr.Error())
			if len(reason) > 480 {
				reason = reason[:480]
			}
			seconds := min(30*(1<<min(job["attempts"].Int(), 7)), 3600)
			data["state"] = "retry"
			data["last_error"] = string(reason)
			data["next_at"] = gtime.New(time.Now().Add(time.Duration(seconds) * time.Second))
		}
		// Request cancellation must not lose the durable outcome of a completed cloud call.
		persist, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, err = g.Model("album_asset_delete").Ctx(persist).Where("id", job["id"]).Data(data).Update()
		cancel()
		if err != nil {
			return err
		}
	}
	return nil
}
func flushAssetDeletesSoon(albumID int64) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := FlushAssetDeletes(ctx, albumID, false); err != nil {
		g.Log().Warning(ctx, "云盘删除待办保留，将自动重试")
	}
}
func RunAssetDeleteWorker(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			work, cancel := context.WithTimeout(ctx, 25*time.Second)
			err := FlushAssetDeletes(work, 0, false)
			cancel()
			if err != nil && ctx.Err() == nil {
				g.Log().Warning(ctx, "云盘删除待办稍后重试")
			}
		}
	}
}
func AssetDeleteStatus(ctx context.Context, albumID int64) (g.Map, error) {
	rows, err := g.Model("album_asset_delete").Ctx(ctx).Where("album_id", albumID).WhereIn("state", []string{"pending", "retry"}).Fields("state,last_error").All()
	if err != nil {
		return nil, err
	}
	failed := 0
	last := ""
	for _, row := range rows {
		if row["state"].String() == "retry" {
			failed++
			last = row["last_error"].String()
		}
	}
	return g.Map{"pending": len(rows), "failed": failed, "lastError": last}, nil
}
func RetryAssetDeletes(ctx context.Context, albumID int64) (g.Map, error) {
	row, err := OwnAlbumModel(ctx, albumID).One()
	if err != nil {
		return nil, err
	}
	if row.IsEmpty() {
		return nil, fmt.Errorf("画册不存在或无权操作")
	}
	work, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err = FlushAssetDeletes(work, albumID, true); err != nil && work.Err() == nil {
		return nil, err
	}
	return AssetDeleteStatus(ctx, albumID)
}
