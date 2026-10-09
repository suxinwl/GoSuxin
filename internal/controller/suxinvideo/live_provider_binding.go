package suxinvideo

import (
	"context"
	"errors"
	"github.com/suxinwl/GoSuxin/framework/util/gconv"
	"strings"
	"time"
)

func liveProviderBindings(ctx context.Context, key string) (row, error) {
	if !liveProviderValidKey(key) {
		return nil, errors.New("直播引擎标识无效")
	}
	var catalog liveProviderCatalog
	if err := liveProviderCall(ctx, key, "GET", "/internal/catalog", nil, &catalog); err != nil {
		return nil, err
	}
	bindings, err := all(ctx, "SELECT b.*,c.name channel_name FROM sx_live_provider_binding b JOIN sx_live_channel c ON c.id=b.channel_id WHERE b.provider_key=?", key)
	if err != nil {
		return nil, err
	}
	mapped := map[string]row{}
	for _, b := range bindings {
		mapped[gconv.String(b["module_key"])+":"+gconv.String(b["provider_ref"])] = b
	}
	channels, err := all(ctx, "SELECT id,name,tvg_id,aliases_json,group_id,enabled FROM sx_live_channel ORDER BY sort,id")
	if err != nil {
		return nil, err
	}
	preview := []row{}
	for _, c := range catalog.Channels {
		tvg := liveCanonicalProviderID(c.TVGID, c.Name)
		matches := []row{}
		for _, ch := range channels {
			if (tvg != "" && strings.EqualFold(gconv.String(ch["tvg_id"]), tvg)) || liveImportNameKey(gconv.String(ch["name"])) == liveImportNameKey(c.Name) {
				matches = append(matches, ch)
			}
		}
		preview = append(preview, row{"module_key": c.ModuleKey, "provider_ref": c.Ref, "name": c.Name, "epg_id": c.EpgID, "canonical_tvg_id": tvg, "matches": matches, "conflict": len(matches) > 1, "binding": mapped[c.ModuleKey+":"+c.Ref]})
	}
	return row{"provider_key": key, "list": preview, "channels": channels}, nil
}
func liveSaveProviderBinding(ctx context.Context, key, module, ref string, channelID int64) error {
	if !liveProviderValidKey(key) || !liveProviderModulePattern.MatchString(module) || ref == "" || len(ref) > 500 || channelID < 1 {
		return appError(400, "频道绑定参数无效")
	}
	c, err := one(ctx, "SELECT id FROM sx_live_channel WHERE id=?", channelID)
	if err != nil {
		return err
	}
	if c == nil {
		return appError(400, "目标频道不存在")
	}
	var catalog liveProviderCatalog
	if err = liveProviderCall(ctx, key, "GET", "/internal/catalog", nil, &catalog); err != nil {
		return err
	}
	found := false
	for _, s := range catalog.Channels {
		if s.ModuleKey == module && s.Ref == ref {
			found = true
			break
		}
	}
	if !found {
		return appError(400, "来源频道不存在于当前目录")
	}
	now := time.Now().Unix()
	if err = execSQL(ctx, "INSERT INTO sx_live_provider_binding(provider_key,module_key,provider_ref,channel_id,created,updated) VALUES(?,?,?,?,?,?) ON DUPLICATE KEY UPDATE channel_id=VALUES(channel_id),updated=VALUES(updated)", key, module, ref, channelID, now, now); err != nil {
		return err
	}
	if err = execSQL(ctx, "UPDATE sx_live_stream SET channel_id=?,source_revision=source_revision+1,updated=? WHERE source_kind='provider' AND provider_key=? AND module_key=? AND provider_ref=?", channelID, now, key, module, ref); err != nil {
		return err
	}
	return execSQL(ctx, "UPDATE sx_live_programme SET channel_id=?,updated=? WHERE provider_key=? AND module_key=? AND provider_ref=?", channelID, now, key, module, ref)
}
