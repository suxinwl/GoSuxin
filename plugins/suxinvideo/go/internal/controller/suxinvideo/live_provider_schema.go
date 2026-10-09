package suxinvideo

import (
	"context"
	"fmt"
	"github.com/suxinwl/GoSuxin/framework/util/gconv"
	"time"
)

// Provider references are immutable; signed media URLs never enter these tables.
var liveProviderSchemaStatements = []string{
	`CREATE TABLE IF NOT EXISTS sx_live_module (
 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,provider_key VARCHAR(30) NOT NULL,module_key VARCHAR(100) NOT NULL,
 name VARCHAR(160) NOT NULL DEFAULT '',enabled TINYINT NOT NULL DEFAULT 0,config_json TEXT NOT NULL,secrets_cipher TEXT NOT NULL,
 status VARCHAR(30) NOT NULL DEFAULT 'pending',last_sync BIGINT NOT NULL DEFAULT 0,last_error VARCHAR(300) NOT NULL DEFAULT '',
 source_revision BIGINT NOT NULL DEFAULT 1,created BIGINT NOT NULL,updated BIGINT NOT NULL,
 UNIQUE KEY provider_module(provider_key,module_key)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	`CREATE TABLE IF NOT EXISTS sx_live_profile (
 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,name VARCHAR(120) NOT NULL,modules_json TEXT NOT NULL,channel_ids_json TEXT NULL,
 enabled TINYINT NOT NULL DEFAULT 1,created BIGINT NOT NULL,updated BIGINT NOT NULL) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	`CREATE TABLE IF NOT EXISTS sx_live_member_access (
 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,member_id BIGINT UNSIGNED NOT NULL,module_key VARCHAR(100) NOT NULL,
 enabled TINYINT NOT NULL DEFAULT 1,created BIGINT NOT NULL,updated BIGINT NOT NULL,
 UNIQUE KEY member_module(member_id,module_key)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	`CREATE TABLE IF NOT EXISTS sx_live_distribution (
 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,member_id BIGINT UNSIGNED NOT NULL DEFAULT 0,profile_id BIGINT UNSIGNED NOT NULL,
 name VARCHAR(120) NOT NULL DEFAULT '',token_hash CHAR(64) NOT NULL,enabled TINYINT NOT NULL DEFAULT 1,
 expires_at BIGINT NOT NULL DEFAULT 0,created BIGINT NOT NULL,updated BIGINT NOT NULL,
 UNIQUE KEY token_hash(token_hash),KEY member_id(member_id)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	`CREATE TABLE IF NOT EXISTS sx_live_programme (
 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,channel_id BIGINT UNSIGNED NOT NULL,provider_key VARCHAR(30) NOT NULL,
 module_key VARCHAR(100) NOT NULL,provider_ref VARCHAR(500) NOT NULL,epg_id VARCHAR(200) NOT NULL,
 identity_key CHAR(64) NOT NULL,title VARCHAR(300) NOT NULL,description TEXT NOT NULL,
 start_at BIGINT NOT NULL,end_at BIGINT NOT NULL,replay_kind VARCHAR(30) NOT NULL DEFAULT '',
 created BIGINT NOT NULL,updated BIGINT NOT NULL,UNIQUE KEY identity_key(identity_key),KEY channel_time(channel_id,start_at,end_at)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	`CREATE TABLE IF NOT EXISTS sx_live_provider_binding (
 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,provider_key VARCHAR(30) NOT NULL,module_key VARCHAR(100) NOT NULL,
 provider_ref VARCHAR(500) NOT NULL,channel_id BIGINT UNSIGNED NOT NULL,created BIGINT NOT NULL,updated BIGINT NOT NULL,
 UNIQUE KEY provider_ref(provider_key,module_key,provider_ref)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
}

func liveEnsureProviderSchema(ctx context.Context) error {
	for _, statement := range liveProviderSchemaStatements {
		if err := execSQL(ctx, statement); err != nil {
			return err
		}
	}
	columns := []struct{ name, definition string }{
		{"source_kind", "VARCHAR(30) NOT NULL DEFAULT 'url'"}, {"provider_key", "VARCHAR(30) NOT NULL DEFAULT ''"},
		{"module_key", "VARCHAR(100) NOT NULL DEFAULT ''"}, {"provider_ref", "VARCHAR(500) NOT NULL DEFAULT ''"},
		{"access_level", "VARCHAR(30) NOT NULL DEFAULT 'public'"}, {"source_revision", "BIGINT NOT NULL DEFAULT 1"},
		{"media_type", "VARCHAR(30) NOT NULL DEFAULT 'hls'"}, {"epg_id", "VARCHAR(200) NOT NULL DEFAULT ''"},
		{"catchup_enabled", "TINYINT NOT NULL DEFAULT 0"},
		{"source_mode", "VARCHAR(30) NOT NULL DEFAULT 'live'"}, {"event_id", "VARCHAR(200) NOT NULL DEFAULT ''"}, {"duration_ms", "BIGINT NOT NULL DEFAULT 0"}, {"programme_id", "BIGINT UNSIGNED NOT NULL DEFAULT 0"},
	}
	for _, c := range columns {
		n, err := one(ctx, "SELECT COUNT(*) n FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='sx_live_stream' AND COLUMN_NAME=?", c.name)
		if err != nil {
			return err
		}
		if gconv.Int(n["n"]) == 0 {
			if err = execSQL(ctx, fmt.Sprintf("ALTER TABLE sx_live_stream ADD COLUMN %s %s", c.name, c.definition)); err != nil {
				return err
			}
		}
	}
	profileColumn, err := one(ctx, "SELECT COUNT(*) n FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='sx_live_profile' AND COLUMN_NAME='channel_ids_json'")
	if err != nil {
		return err
	}
	if gconv.Int(profileColumn["n"]) == 0 {
		if err = execSQL(ctx, "ALTER TABLE sx_live_profile ADD COLUMN channel_ids_json TEXT NULL"); err != nil {
			return err
		}
	}
	n, err := one(ctx, "SELECT COUNT(*) n FROM sx_live_profile")
	if err != nil {
		return err
	}
	if gconv.Int(n["n"]) == 0 {
		now := time.Now().Unix()
		return execSQL(ctx, "INSERT INTO sx_live_profile(name,modules_json,created,updated) VALUES('全部直播','[]',?,?)", now, now)
	}
	return nil
}
