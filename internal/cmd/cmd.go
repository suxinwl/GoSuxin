package cmd

import (
	"context"
	"fmt"
	"github.com/suxinwl/GoSuxin/internal/adminweb"
	"github.com/suxinwl/GoSuxin/internal/router"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/net/ghttp"
	"github.com/suxinwl/GoSuxin/framework/os/gcmd"
	"github.com/suxinwl/GoSuxin/framework/util/gconv"
)

var (
	Main = gcmd.Command{
		Name:  "main",
		Usage: "main",
		Brief: "start http server",
		Func: func(ctx context.Context, parser *gcmd.Parser) (err error) {
			s := g.Server()
			// 配置静态资源目录
			s.AddStaticPath("/resource/uploads", "./resource/uploads") //访问本地附件
			admin := adminweb.Handler("./resource/suxinweb")
			serveAdmin := func(r *ghttp.Request) {
				// The framework trims trailing slashes; the file server needs the original URL.
				request := r.Request.Clone(r.Context())
				if original, parseErr := url.ParseRequestURI(r.RequestURI); parseErr == nil {
					request.URL = original
				}
				admin.ServeHTTP(r.Response.BufferWriter, request)
			}
			s.BindHandler("/suxinweb", serveAdmin)
			s.BindHandler("/suxinweb/*path", serveAdmin)                         //访问部署管理后台前端vue打包代码
			s.AddStaticPath("/resource/static/brand", "./resource/static/brand") // GoSuxin public brand assets
			//安装页面
			runEnv, _ := g.Cfg("app").Get(ctx, "app.runEnv")
			if runEnv.String() == "debug" {
				s.AddStaticPath("/install", "./devsource/developer/install")
				path, _ := os.Getwd() //获取当前路径
				filePath := filepath.Join(path, "/devsource/developer/install/install.lock")
				if _, err := os.Stat(filePath); err != nil {
					go func() {
						time.Sleep(2 * time.Second)
						fmt.Printf("%c[1;40;32m%s%c[0m\n", 0x1B, "如果还没有安装-请在浏览器访问​进行​安装：​​http://127.0.0.1:"+gconv.String(s.GetListenedPort())+"/install", 0x1B)
					}()
				}
			}
			router.BindController(ctx, s)
			s.Run()
			return nil
		},
	}
)
