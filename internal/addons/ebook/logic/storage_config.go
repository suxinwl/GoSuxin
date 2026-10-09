package album

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/suxinwl/GoSuxin/framework/frame/g"
)

// Secrets never enter the database (including its SQL logger), API responses, or git.
const storageConfigPath = "storage/config/album-storage.json"

var storageConfigMu sync.Mutex

type StorageProfile struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	ClientID        string `json:"clientID"`
	ClientSecret    string `json:"clientSecret"`
	ParentID        int64  `json:"parentId"`
	EnglishParentID *int64 `json:"englishParentId,omitempty"`
	CDNKey          string `json:"cdnKey"`
	URLAuth         *bool  `json:"urlAuth,omitempty"`
	UID             int64  `json:"uid"`
	Revision        int64  `json:"revision"`
	VerifiedAt      int64  `json:"verifiedAt"`
}
type storageConfig struct {
	Active   string           `json:"active"` // local, or an immutable account/profile ID
	Profiles []StorageProfile `json:"profiles"`
}

// Missing in older configurations means enabled; never silently weaken them.
func (p StorageProfile) URLAuthEnabled() bool { return p.URLAuth == nil || *p.URLAuth }
func (p StorageProfile) CDNTTL() int {
	if p.URLAuthEnabled() {
		return 60
	}
	return 0 // no application-imposed CDN expiry
}

func sameStorageRoots(a, b StorageProfile) bool {
	if a.ParentID != b.ParentID {
		return false
	}
	if a.EnglishParentID == nil || b.EnglishParentID == nil {
		return a.EnglishParentID == nil && b.EnglishParentID == nil
	}
	return *a.EnglishParentID == *b.EnglishParentID
}

func readStorageConfig() (storageConfig, error) {
	c := storageConfig{Active: "local", Profiles: []StorageProfile{}}
	b, e := os.ReadFile(storageConfigPath)
	if os.IsNotExist(e) {
		return c, nil
	}
	if e != nil {
		return c, e
	}
	if e = json.Unmarshal(b, &c); e != nil {
		return c, errors.New("存储配置损坏，请恢复配置备份")
	}
	if c.Active == "" {
		c.Active = "local"
	}
	return c, nil
}
func writeStorageConfig(c storageConfig) error {
	if err := os.MkdirAll(filepath.Dir(storageConfigPath), 0700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(storageConfigPath), ".storage-*.tmp")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(name, storageConfigPath)
}
func StorageSettings() (g.Map, error) {
	storageConfigMu.Lock()
	defer storageConfigMu.Unlock()
	c, err := readStorageConfig()
	if err != nil {
		return nil, err
	}
	profiles := []g.Map{}
	for _, p := range c.Profiles {
		profiles = append(profiles, g.Map{"id": p.ID, "name": p.Name, "clientID": p.ClientID, "parentId": p.ParentID, "englishParentId": p.EnglishParentID, "uid": p.UID, "revision": p.Revision, "verifiedAt": p.VerifiedAt, "urlAuth": p.URLAuthEnabled(), "cdnTTL": p.CDNTTL(), "hasClientSecret": p.ClientSecret != "", "hasCDNKey": p.CDNKey != ""})
	}
	return g.Map{"active": c.Active, "profiles": profiles, "cdnTTL": 60}, nil
}
func SaveStorageProfile(in StorageProfile) (string, error) {
	storageConfigMu.Lock()
	defer storageConfigMu.Unlock()
	if in.EnglishParentID != nil && (*in.EnglishParentID < 0 || *in.EnglishParentID == in.ParentID) {
		return "", errors.New("中英文目标目录 ID 必须不同，且不能小于零")
	}
	if in.ParentID < 0 {
		return "", errors.New("目录 ID 不能小于零")
	}
	c, err := readStorageConfig()
	if err != nil {
		return "", err
	}
	if in.ID != "" {
		for i, p := range c.Profiles {
			if p.ID != in.ID {
				continue
			}
			if p.UID != 0 && p.ClientID != in.ClientID {
				return "", errors.New("更换开发者账号请新增连接，保留原连接供旧文件使用")
			}
			if in.ClientSecret == "" {
				in.ClientSecret = p.ClientSecret
			}
			if in.CDNKey == "" {
				in.CDNKey = p.CDNKey
			}
			if in.URLAuth == nil {
				in.URLAuth = p.URLAuth
			}
			if c.Active == p.ID && (in.ClientSecret != p.ClientSecret || in.CDNKey != p.CDNKey || !sameStorageRoots(in, p) || in.URLAuthEnabled() != p.URLAuthEnabled()) {
				return "", errors.New("请先切换至服务器存储，再修改当前连接并重新验证")
			}
			in.UID = p.UID
			in.Revision = p.Revision + 1
			in.VerifiedAt = 0
			if in.ClientID == p.ClientID && in.ClientSecret == p.ClientSecret && in.CDNKey == p.CDNKey && sameStorageRoots(in, p) && in.URLAuthEnabled() == p.URLAuthEnabled() {
				in.VerifiedAt = p.VerifiedAt
				in.Revision = p.Revision
			}
			c.Profiles[i] = in
			return in.ID, writeStorageConfig(c)
		}
		return "", errors.New("存储连接不存在")
	}
	in.ID, err = NewKey()
	if err != nil {
		return "", err
	}
	in.Revision = 1
	in.UID = 0
	in.VerifiedAt = 0
	c.Profiles = append(c.Profiles, in)
	return in.ID, writeStorageConfig(c)
}
func storageProfile(id string) (StorageProfile, error) {
	storageConfigMu.Lock()
	defer storageConfigMu.Unlock()
	c, err := readStorageConfig()
	if err != nil {
		return StorageProfile{}, err
	}
	for _, p := range c.Profiles {
		if p.ID == id {
			return p, nil
		}
	}
	return StorageProfile{}, errors.New("存储连接不可用，请恢复原连接配置")
}
func CurrentStorageTarget() (StorageProfile, error) {
	storageConfigMu.Lock()
	defer storageConfigMu.Unlock()
	c, err := readStorageConfig()
	if err != nil {
		return StorageProfile{}, err
	}
	if c.Active == "local" {
		return StorageProfile{}, nil
	}
	for _, p := range c.Profiles {
		if p.ID == c.Active && p.VerifiedAt > 0 {
			return p, nil
		}
	}
	return StorageProfile{}, errors.New("云盘配置尚未验证，不能上传")
}
func ActivateStorage(id string) error {
	storageConfigMu.Lock()
	defer storageConfigMu.Unlock()
	c, err := readStorageConfig()
	if err != nil {
		return err
	}
	if id != "local" {
		found := false
		for _, p := range c.Profiles {
			if p.ID == id && p.VerifiedAt > 0 {
				found = true
			}
		}
		if !found {
			return errors.New("请先通过云盘上传与 CDN 鉴权测试")
		}
	}
	c.Active = id
	return writeStorageConfig(c)
}
func markStorageVerified(p StorageProfile, uid int64) error {
	storageConfigMu.Lock()
	defer storageConfigMu.Unlock()
	c, err := readStorageConfig()
	if err != nil {
		return err
	}
	for i, cur := range c.Profiles {
		if cur.ID == p.ID && cur.Revision == p.Revision {
			if cur.UID != 0 && cur.UID != uid {
				return errors.New("账号 UID 改变，请新增存储连接")
			}
			c.Profiles[i].UID = uid
			c.Profiles[i].VerifiedAt = time.Now().Unix()
			return writeStorageConfig(c)
		}
	}
	return errors.New("配置已变更，请重新测试")
}
func MigrateStorage(ctx context.Context) error {
	_, err := g.DB().Exec(ctx, `CREATE TABLE IF NOT EXISTS gf_album_asset (id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY, album_id BIGINT UNSIGNED NOT NULL, provider VARCHAR(16) NOT NULL, profile_id VARCHAR(64) NOT NULL DEFAULT '', file_id BIGINT NOT NULL DEFAULT 0, local_path VARCHAR(500) NOT NULL DEFAULT '', size_bytes BIGINT NOT NULL, checksum CHAR(32) NOT NULL, createtime DATETIME NOT NULL, KEY idx_asset_album(album_id)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`)
	if err != nil {
		return err
	}
	for _, col := range []struct{ name, definition string }{{"storage_profile_id", "VARCHAR(64) NOT NULL DEFAULT ''"}, {"storage_parent_id", "BIGINT NOT NULL DEFAULT 0"}, {"processing_stage", "VARCHAR(32) NOT NULL DEFAULT ''"}, {"pending_pdf", "VARCHAR(500) NOT NULL DEFAULT ''"}, {"edit_version", "BIGINT NOT NULL DEFAULT 0"}} {
		n, e := g.DB().GetValue(ctx, "SELECT COUNT(*) FROM information_schema.columns WHERE table_schema=DATABASE() AND table_name='gf_album' AND column_name=?", col.name)
		if e != nil {
			return e
		}
		if n.Int() == 0 {
			if _, e = g.DB().Exec(ctx, "ALTER TABLE gf_album ADD COLUMN "+col.name+" "+col.definition); e != nil {
				return e
			}
			if e = g.DB().GetCore().ClearTableFields(ctx, "gf_album"); e != nil {
				return e
			}
		}
	}
	return migrateAssetDeletes(ctx)
}
