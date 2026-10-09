package suxinvideo

import (
	"context"
	"encoding/json"
)

// Only visibility/configuration changes invalidate prefetched public cards.
// Ordinary collector updates and hit counters use the client's short page TTL.
// This bounded query reads configuration and taxonomy, never the film library.
func appCatalogRevision(ctx context.Context) (string, error) {
	items, err := all(ctx, `SELECT 'config' section,`+"`key`"+` identity,value content FROM sx_config
 WHERE `+"`key`"+` IN ('app_catalog_visibility','content_block_enable','content_block_keywords','content_block_categories',
 'site_name','site_logo','site_favicon','user_default_avatar','site_template','home_recommend_source','home_hero_source','home_slide_enable','yqk_navigation_enable')
 UNION ALL SELECT 'type',CAST(id AS CHAR),CONCAT_WS('|',pid,name,status,sort,show_home) FROM sx_type
 UNION ALL SELECT 'collector',CAST(id AS CHAR),CONCAT_WS('|',api_url,status) FROM sx_collect_api
 UNION ALL SELECT 'player',code,CAST(status AS CHAR) FROM sx_player
 UNION ALL SELECT 'slide',CAST(id AS CHAR),CONCAT_WS('|',name,pic,url,pos,sort,status) FROM sx_slide
 ORDER BY section,identity`)
	if err != nil {
		return "", err
	}
	encoded, err := json.Marshal(items)
	if err != nil {
		return "", err
	}
	return appHash(string(encoded)), nil
}

func invalidateAppFilmCatalog(ctx context.Context) error {
	token, err := appToken()
	if err != nil {
		return err
	}
	return execSQL(ctx, "INSERT INTO sx_config(`key`,`value`) VALUES('app_catalog_visibility',?) ON DUPLICATE KEY UPDATE `value`=VALUES(`value`)", token)
}
