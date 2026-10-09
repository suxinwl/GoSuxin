package suxinvideo

import (
	"context"
	"sync"

	"github.com/suxinwl/GoSuxin/framework/util/gconv"
)

var liveSchemaLock sync.Mutex
var liveSchemaReady bool

var liveSchemaStatements = []string{
	`CREATE TABLE IF NOT EXISTS sx_live_group (
 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,name VARCHAR(120) NOT NULL,
 identity_key CHAR(64) NOT NULL,sort INT NOT NULL DEFAULT 0,enabled TINYINT NOT NULL DEFAULT 1,
 manual_edited TINYINT NOT NULL DEFAULT 0,created BIGINT NOT NULL,updated BIGINT NOT NULL,
 UNIQUE KEY identity_key(identity_key)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	`CREATE TABLE IF NOT EXISTS sx_live_channel (
 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,tvg_id VARCHAR(200) NOT NULL DEFAULT '',
 identity_key CHAR(64) NOT NULL,name VARCHAR(200) NOT NULL,logo TEXT NOT NULL,group_id BIGINT UNSIGNED NOT NULL,
 aliases_json VARCHAR(2000) NOT NULL DEFAULT '[]',
 sort INT NOT NULL DEFAULT 0,enabled TINYINT NOT NULL DEFAULT 1,manual_edited TINYINT NOT NULL DEFAULT 0,
 created BIGINT NOT NULL,updated BIGINT NOT NULL,UNIQUE KEY identity_key(identity_key),KEY group_id(group_id,enabled))
 ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	`CREATE TABLE IF NOT EXISTS sx_live_stream (
 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,channel_id BIGINT UNSIGNED NOT NULL,
 subscription_id BIGINT UNSIGNED NOT NULL DEFAULT 0,name VARCHAR(200) NOT NULL,url TEXT NOT NULL,url_hash CHAR(64) NOT NULL,
 headers_json TEXT NOT NULL,priority INT NOT NULL DEFAULT 0,quality VARCHAR(100) NOT NULL DEFAULT '',
 enabled TINYINT NOT NULL DEFAULT 1,health VARCHAR(30) NOT NULL DEFAULT 'pending',last_checked BIGINT NOT NULL DEFAULT 0,
 last_error VARCHAR(300) NOT NULL DEFAULT '',manual_edited TINYINT NOT NULL DEFAULT 0,
 created BIGINT NOT NULL,updated BIGINT NOT NULL,UNIQUE KEY channel_url(channel_id,url_hash),KEY channel_id(channel_id,enabled),
 KEY subscription_id(subscription_id)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	`CREATE TABLE IF NOT EXISTS sx_live_subscription (
 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,name VARCHAR(120) NOT NULL,url TEXT NOT NULL,url_hash CHAR(64) NOT NULL,
 enabled TINYINT NOT NULL DEFAULT 1,last_refresh BIGINT NOT NULL DEFAULT 0,last_error VARCHAR(300) NOT NULL DEFAULT '',
 created BIGINT NOT NULL,updated BIGINT NOT NULL,UNIQUE KEY url_hash(url_hash)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	`CREATE TABLE IF NOT EXISTS sx_live_job (
 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,status VARCHAR(30) NOT NULL,kind VARCHAR(30) NOT NULL,
 total INT NOT NULL DEFAULT 0,processed INT NOT NULL DEFAULT 0,added INT NOT NULL DEFAULT 0,updated_count INT NOT NULL DEFAULT 0,
 failed INT NOT NULL DEFAULT 0,message VARCHAR(500) NOT NULL DEFAULT '',created BIGINT NOT NULL,updated BIGINT NOT NULL,
 KEY state(status,created)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
}

func ensureLiveSchema(ctx context.Context) error {
	liveSchemaLock.Lock()
	defer liveSchemaLock.Unlock()
	if liveSchemaReady {
		return nil
	}
	for _, sql := range liveSchemaStatements {
		if err := execSQL(ctx, sql); err != nil {
			return err
		}
	}
	if err := liveEnsureProviderSchema(ctx); err != nil {
		return err
	}
	if err := execSQL(ctx, "UPDATE sx_live_job SET status='interrupted',message='服务重启，任务已停止；已导入频道保留',updated=UNIX_TIMESTAMP() WHERE status IN ('pending','running')"); err != nil {
		return err
	}
	for _, country := range []string{"cn", "hk", "mo", "tw"} {
		name := map[string]string{"cn": "中国大陆", "hk": "中国香港", "mo": "中国澳门", "tw": "中国台湾"}[country]
		url := "https://iptv-org.github.io/iptv/countries/" + country + ".m3u"
		if err := execSQL(ctx, "INSERT IGNORE INTO sx_live_subscription(name,url,url_hash,enabled,created,updated) VALUES(?,?,?,0,UNIX_TIMESTAMP(),UNIX_TIMESTAMP())", "IPTV-org · "+name, url, liveIdentity(url)); err != nil {
			return err
		}
	}
	if err := upgradeLivePermissions(ctx); err != nil {
		return err
	}
	if err := liveUpgradeProviderPermissions(ctx); err != nil {
		return err
	}
	if err := LocalizeLiveCatalog(ctx); err != nil {
		return err
	}
	if err := UpgradeLiveChannelGroups(ctx); err != nil {
		return err
	}
	liveSchemaReady = true
	return nil
}

func liveRowEnabled(item row) bool { return gconv.Int(item["enabled"]) == 1 }
