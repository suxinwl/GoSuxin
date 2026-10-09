package suxinvideo

import (
	"context"
	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/internal/extend/middleware"
	"github.com/suxinwl/GoSuxin/internal/extend/pluginlifecycle"
	"github.com/suxinwl/GoSuxin/internal/plugins"
	"os"

	"github.com/suxinwl/GoSuxin/framework/net/ghttp"
)

var R = new(Router)

type Router struct{}

func init() {
	plugins.Register(plugins.Plugin{Name: "suxinvideo", Title: "影视CMS", Version: "2.5.0", Description: "影视分类、播放器、模板、采集接口及后台基础配置。", Entry: "/suxinvideo/admin", MenuRoots: []string{"suxinvideo_admin", "suxinvideo"}, Start: startCMS})
}

func startCMS(ctx context.Context, _ *ghttp.Server) (func(), error) {
	if _, err := os.Stat("devsource/developer/install/install.lock"); os.IsNotExist(err) {
		return func() {}, nil
	}
	workerCtx, cancel := context.WithCancel(ctx)
	initializeCMS(workerCtx)
	jellyfinAddress := "disabled"
	if value, err := g.Cfg().Get(ctx, "suxinvideo.jellyfinAddress"); err == nil && value.String() != "" {
		jellyfinAddress = value.String()
	}
	if jellyfinAddress != "disabled" {
		if err := StartJellyfinListener(workerCtx, jellyfinAddress); err != nil {
			g.Log().Warning(ctx, "CMS Jellyfin LAN listener:", err)
		}
	}
	return func() { cancel(); pluginlifecycle.Stop("suxinvideo"); stopJellyfinListener() }, nil
}

func (*Router) BindController(ctx context.Context, group *ghttp.RouterGroup) {
	bindRoutes(ctx, group.Clone().Middleware(plugins.Gate("suxinvideo")))
}

func initializeCMS(ctx context.Context) {
	background, _ := g.Cfg().Get(ctx, "suxinvideo.backgroundEnabled", false)
	if err := ensureLiveSchema(ctx); err != nil {
		g.Log().Warning(ctx, "CMS live schema:", err)
	}
	if err := EnsureAppSchema(ctx); err != nil {
		g.Log().Warning(ctx, "CMS native API schema:", err)
	}
	if err := PrepareClientIntegrations(ctx); err != nil {
		g.Log().Warning(ctx, "CMS client integration schema:", err)
	}
	if err := prepareGuoguoTheme(ctx); err != nil {
		g.Log().Warning(ctx, "CMS built-in theme upgrade:", err)
	}
	if err := prepareYQKSource(ctx); err != nil {
		g.Log().Warning(ctx, "CMS YQK source upgrade:", err)
	}
	if err := prepareErciyuanSource(ctx); err != nil {
		g.Log().Warning(ctx, "CMS erciyuan source upgrade:", err)
	}
	if background.Bool() {
		if _, err := yqkSiteSource(ctx); err == nil {
			yqkSiteHomeSnapshot(ctx)
		}
	}
	if err := prepareSourceHealth(ctx); err != nil {
		g.Log().Warning(ctx, "CMS source health upgrade:", err)
	}
	if err := prepareSourceDiscovery(ctx); err != nil {
		g.Log().Warning(ctx, "CMS source discovery upgrade:", err)
	}
	if err := prepareVodAlias(ctx); err != nil {
		g.Log().Warning(ctx, "CMS verified film aliases upgrade:", err)
	}
	if err := prepareVodSourceScores(ctx); err != nil {
		g.Log().Warning(ctx, "CMS per-source score upgrade:", err)
	}
	if err := ensureCMSHostEntry(ctx); err != nil {
		g.Log().Warning(ctx, "CMS host entry:", err)
	}
	if err := upgradeCMSMenu(ctx); err != nil {
		g.Log().Warning(ctx, "CMS menu upgrade:", err)
	}
	if err := upgradeCollectJobPermissions(ctx); err != nil {
		g.Log().Warning(ctx, "CMS collection permissions:", err)
	}
	startImageCleanupScheduler(ctx)
	if background.Bool() {
		startCollectorScheduler(ctx)
		startLiveScheduler(ctx)
		startLiveEngines(ctx)
	}
}

func bindRoutes(ctx context.Context, group *ghttp.RouterGroup) {
	group.Group("/suxinvideo", func(public *ghttp.RouterGroup) {
		public.Bind(new(Site), new(Member), new(Payment))
	})
	// Native endpoints own their JSON format and member/device authorization.
	// This also works when installed into a host with browser API signing enabled.
	g.Server().Group("/suxinvideo", func(native *ghttp.RouterGroup) {
		native.Middleware(plugins.Gate("suxinvideo"))
		RegisterAppRoutes(native)
		RegisterLiveRoutes(native)
	})
	// Media segments have an independent budget. Downloading or streaming must
	// not exhaust the host's shared browser API limiter or signature middleware.
	g.Server().Group("/suxinvideo", func(media *ghttp.RouterGroup) {
		media.Middleware(plugins.Gate("suxinvideo"), middleware.HandlerResponse, appMediaMiddleware)
		media.Bind(new(Media))
	})
	RegisterClientRoutes(group)
	group.Group("/admin/suxinvideo", func(admin *ghttp.RouterGroup) {
		admin.Middleware(middleware.Token, middleware.Auth, cmsAudit)
		RegisterLiveProviderAdminRoutes(admin)
		admin.Bind(new(Admin))
	})
}
