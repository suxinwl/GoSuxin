package suxinvideo

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/net/ghttp"
	"github.com/suxinwl/GoSuxin/framework/util/gconv"
	"github.com/suxinwl/GoSuxin/utility/gf"
	"io"
	"sort"
	"strings"
	"time"
)

func liveProviderCipher(ctx context.Context) (cipher.AEAD, error) {
	secret := liveEngineSecret()
	if len(secret) < 32 {
		return nil, errors.New("直播引擎密钥尚未初始化")
	}
	key := sha256.Sum256([]byte(secret + ":module-config-v1"))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
func liveSealModuleConfig(ctx context.Context, config map[string]any) (string, error) {
	a, e := liveProviderCipher(ctx)
	if e != nil {
		return "", e
	}
	data, e := json.Marshal(config)
	if e != nil {
		return "", e
	}
	nonce := make([]byte, a.NonceSize())
	if _, e = io.ReadFull(rand.Reader, nonce); e != nil {
		return "", e
	}
	return base64.RawStdEncoding.EncodeToString(a.Seal(nonce, nonce, data, []byte("sx_live_module"))), nil
}
func liveUnsealModuleConfig(ctx context.Context, encoded string) (map[string]any, error) {
	out := map[string]any{}
	if encoded == "" {
		return out, nil
	}
	a, e := liveProviderCipher(ctx)
	if e != nil {
		return nil, e
	}
	data, e := base64.RawStdEncoding.DecodeString(encoded)
	if e != nil || len(data) < a.NonceSize() {
		return nil, errors.New("直播模块凭据存储无效")
	}
	plain, e := a.Open(nil, data[:a.NonceSize()], data[a.NonceSize():], []byte("sx_live_module"))
	if e != nil {
		return nil, errors.New("直播模块凭据无法解密")
	}
	if json.Unmarshal(plain, &out) != nil {
		return nil, errors.New("直播模块凭据格式无效")
	}
	return out, nil
}
func liveSecretField(key string) bool {
	key = strings.ToLower(key)
	if key == "pass" {
		return true
	}
	for _, part := range []string{"password", "passwd", "cookie", "authorization", "token", "secret", "credential", "sessdata"} {
		if strings.Contains(key, part) {
			return true
		}
	}
	return false
}
func liveMaskConfig(in map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range in {
		if liveSecretField(k) {
			if gconv.String(v) != "" {
				out[k] = "••••••"
			} else {
				out[k] = ""
			}
		} else if sub, ok := v.(map[string]any); ok {
			out[k] = liveMaskConfig(sub)
		} else if values, ok := v.([]any); ok {
			masked := make([]any, len(values))
			for i, value := range values {
				if sub, ok := value.(map[string]any); ok {
					masked[i] = liveMaskConfig(sub)
				} else {
					masked[i] = value
				}
			}
			out[k] = masked
		} else {
			out[k] = v
		}
	}
	return out
}

func liveConfigContainsSecrets(config map[string]any) bool {
	for key, value := range config {
		if liveSecretField(key) && gconv.String(value) != "" {
			return true
		}
		if nested, ok := value.(map[string]any); ok && liveConfigContainsSecrets(nested) {
			return true
		}
		if values, ok := value.([]any); ok {
			for _, item := range values {
				if nested, ok := item.(map[string]any); ok && liveConfigContainsSecrets(nested) {
					return true
				}
			}
		}
	}
	return false
}

func liveConfigWithoutSecrets(config map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range config {
		if liveSecretField(key) {
			continue
		}
		if nested, ok := value.(map[string]any); ok {
			out[key] = liveConfigWithoutSecrets(nested)
		} else {
			out[key] = value
		}
	}
	return out
}

func liveProviderSaveModule(ctx context.Context, key, module string, enabled bool, config map[string]any) error {
	if !liveProviderValidKey(key) {
		return appError(400, "直播模块参数无效")
	}
	lock := liveProviderConfigLock(key)
	lock.Lock()
	defer lock.Unlock()
	return liveProviderSaveModuleLocked(ctx, key, module, enabled, config)
}

func liveProviderSaveModuleLocked(ctx context.Context, key, module string, enabled bool, config map[string]any) error {
	if !liveProviderValidKey(key) || !liveProviderModulePattern.MatchString(module) {
		return appError(400, "直播模块参数无效")
	}
	if key == "public" && liveConfigContainsSecrets(config) {
		return appError(400, "凭据只能配置在会员实例")
	}
	existing, err := one(ctx, "SELECT * FROM sx_live_module WHERE provider_key=? AND module_key=?", key, module)
	if err != nil {
		return err
	}
	old := map[string]any{}
	if existing != nil {
		old, err = liveUnsealModuleConfig(ctx, gconv.String(existing["secrets_cipher"]))
		if err != nil {
			return err
		}
	}
	for k, v := range config {
		if len(k) > 100 || len(gconv.String(v)) > 16384 {
			return appError(400, "直播模块配置过大")
		}
		if key == "public" && liveSecretField(k) && gconv.String(v) != "" {
			return appError(400, "凭据只能配置在会员实例")
		}
		if text, ok := v.(string); ok && liveSecretField(k) && (text == "" || text == "••••••") {
			continue
		}
		old[k] = v
	}
	sealed, err := liveSealModuleConfig(ctx, old)
	if err != nil {
		return err
	}
	masked, _ := json.Marshal(liveMaskConfig(old))
	now := time.Now().Unix()
	err = execSQL(ctx, `INSERT INTO sx_live_module(provider_key,module_key,name,enabled,config_json,secrets_cipher,created,updated) VALUES(?,?,?,?,?,?,?,?)
 ON DUPLICATE KEY UPDATE enabled=VALUES(enabled),config_json=VALUES(config_json),secrets_cipher=VALUES(secrets_cipher),source_revision=source_revision+1,status='pending',updated=VALUES(updated)`, key, module, module, enabled, string(masked), sealed, now, now)
	if err != nil {
		return err
	}
	if err = execSQL(ctx, "UPDATE sx_live_stream s JOIN sx_live_module m ON m.provider_key=s.provider_key AND m.module_key=s.module_key SET s.source_revision=m.source_revision WHERE s.source_kind='provider' AND s.provider_key=? AND s.module_key=?", key, module); err != nil {
		return err
	}
	if module == "migu" {
		if err = liveProviderInvalidateDependentAccount(ctx, key); err != nil {
			return err
		}
	}
	var response row
	if err = liveProviderCall(ctx, key, "POST", "/internal/modules/config", row{"id": module, "enabled": enabled, "config": old}, &response); err != nil {
		_ = execSQL(ctx, "UPDATE sx_live_module SET last_error='配置已保存，等待直播引擎启动',status='pending' WHERE provider_key=? AND module_key=?", key, module)
		return nil
	}
	return nil
}

// Reapply persisted settings on every engine restart. Credentials stay encrypted
// at rest and are never returned by an administrator status request.
func liveApplyProviderConfig(ctx context.Context, key string) error {
	if !liveProviderValidKey(key) {
		return appError(400, "直播引擎标识无效")
	}
	lock := liveProviderConfigLock(key)
	lock.Lock()
	defer lock.Unlock()
	modules, err := all(ctx, "SELECT module_key,enabled,secrets_cipher FROM sx_live_module WHERE provider_key=?", key)
	if err != nil {
		return err
	}
	for _, m := range modules {
		config, e := liveUnsealModuleConfig(ctx, gconv.String(m["secrets_cipher"]))
		if e != nil {
			return e
		}
		var out row
		if e = liveProviderCall(ctx, key, "POST", "/internal/modules/config", row{"id": m["module_key"], "enabled": gconv.Bool(m["enabled"]), "config": config}, &out); e != nil {
			return e
		}
	}
	return nil
}

func liveProviderStatus(ctx context.Context) row {
	engines := []row{}
	available := []map[string]any{}
	for _, key := range []string{"public", "member"} {
		var health row
		var err error
		item := row{"provider_key": key, "ready": false, "status": "stopped", "message": "直播引擎未就绪"}
		if key == "public" || liveProviderAccountConfigured(ctx) {
			err = liveProviderCall(ctx, key, "GET", "/internal/health", nil, &health)
		} else {
			err = errors.New("会员引擎尚未启用")
		}
		if err == nil {
			item["health"] = health
			item["ready"] = gconv.Bool(health["ok"]) && gconv.String(health["profile"]) == key
			item["catalog_ready"] = gconv.Bool(health["ready"])
			item["status"] = "running"
			item["message"] = ""
		} else {
			item["message"] = err.Error()
		}
		var native row
		if key == "public" {
			if err = liveProviderCall(ctx, key, "GET", "/internal/modules", nil, &native); err == nil {
				available = gconv.Maps(native["modules"])
			}
		}
		if len(available) > 0 {
			sanitized := []row{}
			for _, original := range available {
				value := row{}
				for k, v := range original {
					value[k] = v
				}
				module := gconv.String(value["id"])
				if !liveProviderModulePattern.MatchString(module) {
					continue
				}
				now := time.Now().Unix()
				_ = execSQL(ctx, `INSERT IGNORE INTO sx_live_module(provider_key,module_key,name,enabled,config_json,secrets_cipher,created,updated) VALUES(?,?,?,?,'{}','',?,?)`, key, module, value["name"], gconv.Bool(value["enabled"]) && key == "public", now, now)
				value["config"] = liveMaskConfig(gconv.Map(value["config"]))
				value["provider_key"] = key
				sanitized = append(sanitized, value)
			}
			// Native metadata describes available options; the database decides enabled state.
			item["available_modules"] = sanitized
		}
		engines = append(engines, item)
	}
	rows, err := all(ctx, "SELECT id,provider_key,module_key,name,enabled,config_json,status,last_sync,last_error,source_revision FROM sx_live_module ORDER BY provider_key,module_key")
	if err != nil {
		rows = []row{}
	}
	for _, r := range rows {
		var config map[string]any
		_ = json.Unmarshal([]byte(gconv.String(r["config_json"])), &config)
		r["config"] = liveMaskConfig(config)
		delete(r, "config_json")
	}
	return row{"engines": engines, "modules": rows}
}

var liveProviderAdminActions = map[string]string{
	"provider/bindings": "直播频道绑定预览", "provider/bindings/save": "绑定直播来源频道", "epg": "查看直播节目单",
	"provider/status": "直播引擎状态", "provider/save": "配置直播模块", "provider/sync": "同步直播目录", "provider/login": "直播模块登录",
	"profiles": "直播分发配置", "profiles/save": "保存直播分发配置", "profiles/delete": "停用直播分发配置",
	"members": "直播会员白名单", "members/save": "保存直播会员授权", "members/delete": "撤销直播会员授权",
	"distribution": "直播订阅令牌", "distribution/save": "创建或重置直播订阅", "distribution/revoke": "撤销直播订阅",
}

func liveUpgradeProviderPermissions(ctx context.Context) error {
	parent, err := g.Model("auth_rule").Ctx(ctx).Where("routename", "suxinvideo_admin").One()
	if err != nil || parent == nil {
		return err
	}
	for action, title := range liveProviderAdminActions {
		name := "suxinvideo_admin_live_" + strings.ReplaceAll(action, "/", "_")
		entry, e := g.Model("auth_rule").Ctx(ctx).Where("routename", name).One()
		if e != nil {
			return e
		}
		values := row{"pid": parent["id"].Int64(), "title": title, "path": "/admin/suxinvideo/live/" + action, "routepath": "live_" + strings.ReplaceAll(action, "/", "_"), "routename": name, "permission": "", "type": 2, "status": 0, "requiresauth": 1, "hideinmenu": 1}
		if entry == nil {
			for k, v := range authRuleDefaults() {
				values[k] = v
			}
			_, err = g.Model("auth_rule").Ctx(ctx).Data(values).Insert()
		} else {
			_, err = g.Model("auth_rule").Ctx(ctx).Where("id", entry["id"]).Data(values).Update()
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func RegisterLiveProviderAdminRoutes(group *ghttp.RouterGroup) {
	bind := func(method, action string, handler func(*ghttp.Request) (any, error)) {
		fn := func(r *ghttp.Request) {
			ok, err := requireResource(r.Context(), r, "", "live/"+action)
			if !ok || err != nil {
				return
			}
			data, err := handler(r)
			if err != nil {
				r.SetCtxVar("cms_denied", true)
				r.Response.WriteJson(gf.Failed().SetMsg(err.Error()))
				return
			}
			r.Response.WriteJson(gf.Success().SetData(data))
		}
		if method == "GET" {
			group.GET("/live/"+action, fn)
		} else {
			group.POST("/live/"+action, fn)
		}
	}
	bind("GET", "provider/status", func(r *ghttp.Request) (any, error) { return liveProviderStatus(r.Context()), nil })
	bind("GET", "provider/bindings", func(r *ghttp.Request) (any, error) {
		return liveProviderBindings(r.Context(), r.Get("provider_key", "public").String())
	})
	bind("POST", "provider/bindings/save", func(r *ghttp.Request) (any, error) {
		return true, liveSaveProviderBinding(r.Context(), r.Get("provider_key").String(), r.Get("module_key").String(), r.Get("provider_ref").String(), r.Get("channel_id").Int64())
	})
	bind("GET", "epg", func(r *ghttp.Request) (any, error) {
		rows, err := all(r.Context(), "SELECT id,channel_id,title,description,start_at start,end_at stop,replay_kind kind FROM sx_live_programme WHERE channel_id=? AND start_at<? AND end_at>? ORDER BY start_at LIMIT 1000", r.Get("channel_id").Int64(), r.Get("end", time.Now().Add(24*time.Hour).Unix()).Int64(), r.Get("start", time.Now().Add(-24*time.Hour).Unix()).Int64())
		return row{"items": rows}, err
	})
	bind("POST", "provider/save", func(r *ghttp.Request) (any, error) {
		return true, liveProviderSaveModule(r.Context(), r.Get("provider_key").String(), r.Get("module_key").String(), r.Get("enabled").Bool(), r.Get("config").Map())
	})
	bind("POST", "provider/sync", func(r *ghttp.Request) (any, error) {
		key := r.Get("provider_key", "public").String()
		if !liveProviderValidKey(key) {
			return nil, appError(400, "直播引擎标识无效")
		}
		id, err := liveStartJob(r.Context(), "provider_sync", func(ctx context.Context, p *liveProgress) error {
			return liveProviderRefresh(ctx, p, key)
		})
		return row{"job_id": id}, err
	})
	bind("POST", "provider/login", func(r *ghttp.Request) (any, error) {
		allowEnable, err := resourceAllowed(r.Context(), "", "live/provider/save")
		if err != nil {
			return nil, err
		}
		return liveProviderLogin(r.Context(), r.Get("module_key").String(), r.Get("action").String(), r.Get("key").String(), r.Get("payload").Interface(), allowEnable)
	})
	bind("GET", "profiles", func(r *ghttp.Request) (any, error) {
		rows, err := all(r.Context(), "SELECT * FROM sx_live_profile ORDER BY id")
		for _, p := range rows {
			var modules []string
			_ = json.Unmarshal([]byte(gconv.String(p["modules_json"])), &modules)
			p["modules"] = modules
			delete(p, "modules_json")
			var channelIDs []int64
			_ = json.Unmarshal([]byte(gconv.String(p["channel_ids_json"])), &channelIDs)
			p["channel_ids"] = channelIDs
			delete(p, "channel_ids_json")
		}
		return row{"list": rows}, err
	})
	bind("POST", "profiles/save", func(r *ghttp.Request) (any, error) {
		name := strings.TrimSpace(r.Get("name").String())
		modules := r.Get("modules").Strings()
		if name == "" || len(modules) > 200 {
			return nil, appError(400, "分发配置无效")
		}
		sort.Strings(modules)
		for _, m := range modules {
			parts := strings.Split(m, ":")
			if len(parts) != 2 || !liveProviderValidKey(parts[0]) || !liveProviderModulePattern.MatchString(parts[1]) {
				return nil, appError(400, "分发模块标识无效")
			}
		}
		raw, _ := json.Marshal(modules)
		channelIDs := r.Get("channel_ids").Int64s()
		if len(channelIDs) > 5000 {
			return nil, appError(400, "分发频道过多")
		}
		for _, id := range channelIDs {
			if id < 1 {
				return nil, appError(400, "分发频道参数无效")
			}
		}
		rawChannels, _ := json.Marshal(channelIDs)
		id, err := liveWriteRecord(r.Context(), "sx_live_profile", r.Get("id").Int64(), row{"name": cutRunes(name, 120), "modules_json": string(raw), "channel_ids_json": string(rawChannels), "enabled": r.Get("enabled", 1).Int()})
		return row{"id": id}, err
	})
	bind("POST", "profiles/delete", func(r *ghttp.Request) (any, error) {
		return true, execSQL(r.Context(), "UPDATE sx_live_profile SET enabled=0,updated=? WHERE id=?", time.Now().Unix(), r.Get("id").Int64())
	})
	bind("GET", "members", func(r *ghttp.Request) (any, error) {
		rows, err := all(r.Context(), "SELECT a.id,a.member_id,a.module_key,a.enabled,u.name FROM sx_live_member_access a JOIN sx_user u ON u.id=a.member_id ORDER BY a.id DESC LIMIT 500")
		return row{"list": rows}, err
	})
	bind("POST", "members/save", func(r *ghttp.Request) (any, error) {
		member := r.Get("member_id").Int64()
		module := r.Get("module_key").String()
		if member < 1 || !liveProviderModulePattern.MatchString(module) {
			return nil, appError(400, "会员授权参数无效")
		}
		u, e := one(r.Context(), "SELECT id FROM sx_user WHERE id=? AND status=1", member)
		if e != nil {
			return nil, e
		}
		if u == nil {
			return nil, appError(400, "会员不存在或已停用")
		}
		now := time.Now().Unix()
		return true, execSQL(r.Context(), "INSERT INTO sx_live_member_access(member_id,module_key,enabled,created,updated) VALUES(?,?,?,?,?) ON DUPLICATE KEY UPDATE enabled=VALUES(enabled),updated=VALUES(updated)", member, module, r.Get("enabled", 1).Int(), now, now)
	})
	bind("POST", "members/delete", func(r *ghttp.Request) (any, error) {
		return true, execSQL(r.Context(), "UPDATE sx_live_member_access SET enabled=0,updated=? WHERE id=?", time.Now().Unix(), r.Get("id").Int64())
	})
	liveRegisterDistributionAdmin(bind)
}

func liveProviderLoginChanged(ctx context.Context, module string) error {
	item, err := one(ctx, "SELECT secrets_cipher FROM sx_live_module WHERE provider_key='member' AND module_key=?", module)
	if err != nil {
		return err
	}
	if item == nil {
		return nil
	}
	config, err := liveUnsealModuleConfig(ctx, gconv.String(item["secrets_cipher"]))
	if err != nil {
		return err
	}
	// The engine owns credentials captured by its login flow. Old CMS form secrets
	// must not overwrite a newly captured account when the process restarts.
	config = liveConfigWithoutSecrets(config)
	sealed, err := liveSealModuleConfig(ctx, config)
	if err != nil {
		return err
	}
	masked, _ := json.Marshal(liveMaskConfig(config))
	now := time.Now().Unix()
	if err = execSQL(ctx, "UPDATE sx_live_module SET secrets_cipher=?,config_json=?,source_revision=source_revision+1,updated=? WHERE provider_key='member' AND module_key=?", sealed, string(masked), now, module); err != nil {
		return err
	}
	if err = execSQL(ctx, "UPDATE sx_live_stream s JOIN sx_live_module m ON m.provider_key=s.provider_key AND m.module_key=s.module_key SET s.source_revision=m.source_revision WHERE s.provider_key='member' AND s.module_key=?", module); err != nil {
		return err
	}
	if module == "migu" {
		return liveProviderInvalidateDependentAccount(ctx, "member")
	}
	return nil
}

func liveProviderInvalidateDependentAccount(ctx context.Context, key string) error {
	if err := execSQL(ctx, "UPDATE sx_live_module SET source_revision=source_revision+1,updated=? WHERE provider_key=? AND module_key='migu-sports'", time.Now().Unix(), key); err != nil {
		return err
	}
	return execSQL(ctx, "UPDATE sx_live_stream s JOIN sx_live_module m ON m.provider_key=s.provider_key AND m.module_key=s.module_key SET s.source_revision=m.source_revision WHERE s.provider_key=? AND s.module_key='migu-sports'", key)
}
