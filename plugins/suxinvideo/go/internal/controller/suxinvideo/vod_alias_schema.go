package suxinvideo

import "context"

// A verified duplicate group keeps every original sx_vod row and URL. Each
// member, including the canonical member, points to the group's oldest ID.
// The separate table is retained with the rest of the CMS data on uninstall.
const vodAliasTable = `CREATE TABLE IF NOT EXISTS sx_vod_alias (
 vod_id INT UNSIGNED NOT NULL,
 canonical_id INT UNSIGNED NOT NULL,
 reason VARCHAR(255) NOT NULL DEFAULT '',
 created INT UNSIGNED NOT NULL DEFAULT 0,
 updatetime INT UNSIGNED NOT NULL DEFAULT 0,
 PRIMARY KEY (vod_id), KEY canonical_id (canonical_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`

func prepareVodAlias(ctx context.Context) error { return execSQL(ctx, vodAliasTable) }
