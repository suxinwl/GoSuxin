package suxinvideo

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/suxinwl/GoSuxin/framework/util/gconv"
	"github.com/suxinwl/GoSuxin/internal/liveruntime"
)

// Serialize credential writes with restart initialization so a late replay of
// an old form configuration cannot overwrite a newly captured login session.
var liveProviderConfigLocks = map[string]*sync.Mutex{"public": {}, "member": {}}

func liveProviderConfigLock(key string) *sync.Mutex { return liveProviderConfigLocks[key] }

func liveProviderLoginStarts(action string) bool {
	return action == "start" || action == "browserStart" || action == "browserImport"
}

func liveProviderLoginSucceeded(action string, data map[string]any) bool {
	status := strings.ToLower(gconv.String(data["status"]))
	return action == "browserImport" || gconv.Bool(data["authenticated"]) ||
		status == "success" || status == "authenticated" || status == "logged_in" ||
		(action == "poll" && status == "ok")
}

// An empty catalogue is normal before the first account login. The health
// contract's ready flag describes the catalogue, not the management API.
func liveWaitProvider(ctx context.Context, key string, wait time.Duration) error {
	if !liveProviderValidKey(key) {
		return appError(400, "直播引擎标识无效")
	}
	ctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	for {
		var health row
		if err := liveProviderCall(ctx, key, "GET", "/internal/health", nil, &health); err == nil {
			if !gconv.Bool(health["ok"]) || gconv.String(health["profile"]) != key {
				return errors.New("直播引擎实例身份无效")
			}
			return nil
		}
		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.Canceled) {
				return ctx.Err()
			}
			label := "公共"
			if key == "member" {
				label = "账号"
			}
			for _, state := range liveruntime.States() {
				if state.Profile == key && state.Error != "" && state.Error != "未配置账号来源" {
					return fmt.Errorf("%s直播引擎尚未就绪：%s", label, state.Error)
				}
			}
			return fmt.Errorf("%s直播引擎启动超时，请刷新状态后重试", label)
		case <-time.After(250 * time.Millisecond):
		}
	}
}

func liveProviderLogin(ctx context.Context, module, action, qrKey string, payload any, allowEnable bool) (row, error) {
	if !liveProviderModulePattern.MatchString(module) {
		return nil, appError(400, "登录模块无效")
	}
	allowed := map[string]bool{"start": true, "poll": true, "browserStart": true, "browserStatus": true, "browserCheck": true, "browserCancel": true, "browserClose": true, "browserImport": true}
	if !allowed[action] {
		return nil, appError(400, "登录操作无效")
	}
	if text, ok := payload.(string); ok && len(text) > 16384 {
		return nil, appError(400, "账号凭据过长")
	}
	// Public metadata contains capability declarations, never account state.
	var metadata row
	if err := liveProviderCall(ctx, "public", "GET", "/internal/modules", nil, &metadata); err != nil {
		return nil, err
	}
	found, supported := false, false
	for _, candidate := range gconv.Maps(metadata["modules"]) {
		if gconv.String(candidate["id"]) != module {
			continue
		}
		found = true
		if action == "start" || action == "poll" {
			supported = gconv.Bool(candidate["login"])
		} else {
			supported = gconv.Bool(candidate["browser_login"])
		}
		break
	}
	if !found {
		return nil, appError(400, "直播来源模块不存在")
	}
	if !supported {
		return nil, appError(400, "此平台不支持该登录方式，请在配置中填写该平台的登录凭据")
	}
	lock := liveProviderConfigLock("member")
	lock.Lock()
	defer lock.Unlock()
	setting, err := one(ctx, "SELECT enabled,secrets_cipher FROM sx_live_module WHERE provider_key='member' AND module_key=?", module)
	if err != nil {
		return nil, err
	}
	if setting == nil || !gconv.Bool(setting["enabled"]) {
		if !liveProviderLoginStarts(action) {
			return nil, appError(409, "此会员模块尚未启用，请先开始账号登录")
		}
		if !allowEnable {
			return nil, appError(403, "启用账号来源需要直播模块配置权限")
		}
		if err = liveProviderSaveModuleLocked(ctx, "member", module, true, map[string]any{}); err != nil {
			return nil, err
		}
	}
	startLiveEngines(ctx)
	if err = liveWaitProvider(ctx, "member", 20*time.Second); err != nil {
		return nil, err
	}
	if liveProviderLoginStarts(action) {
		// The process can accept login before its periodic CMS initializer runs.
		setting, err = one(ctx, "SELECT secrets_cipher FROM sx_live_module WHERE provider_key='member' AND module_key=?", module)
		if err != nil {
			return nil, err
		}
		config, e := liveUnsealModuleConfig(ctx, gconv.String(setting["secrets_cipher"]))
		if e != nil {
			return nil, e
		}
		var configured row
		if err = liveProviderCall(ctx, "member", "POST", "/internal/modules/config", row{"id": module, "enabled": true, "config": config}, &configured); err != nil {
			return nil, err
		}
	}
	var out row
	err = liveProviderCall(ctx, "member", "POST", "/internal/modules/login", row{"id": module, "action": action, "key": qrKey, "payload": payload}, &out)
	if err == nil && liveProviderLoginSucceeded(action, gconv.Map(out["data"])) {
		err = liveProviderLoginChanged(ctx, module)
	}
	return liveMaskConfig(out), err
}
