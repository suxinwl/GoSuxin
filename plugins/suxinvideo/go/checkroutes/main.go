package main

import (
    "context"
    "fmt"
    "os"

    cms "github.com/suxinwl/GoSuxin/plugins/suxinvideo/go/internal/controller/suxinvideo"
    _ "github.com/suxinwl/GoSuxin/internal/logic"
    _ "github.com/suxinwl/GoSuxin/framework/contrib/nosql/redis"
    "github.com/suxinwl/GoSuxin/framework/net/ghttp"
)

func main() {
    server:=ghttp.GetServer("suxinvideo-routes-check")
    server.SetAddr("127.0.0.1:0")
    server.Group("/",func(group *ghttp.RouterGroup){cms.R.BindController(context.Background(),group)})
    if err:=server.Start();err!=nil{panic(err)}
    defer server.Shutdown()
    want:=map[string]bool{"GET /suxinvideo":false,"GET /suxinvideo/detail":false,"GET /suxinvideo/captcha":false,"GET /suxinvideo/asset":false,"GET /admin/suxinvideo/list":false,"POST /admin/suxinvideo/save":false}
    for _,route:=range server.GetRoutes(){key:=route.Method+" "+route.Route;fmt.Println(key);if _,ok:=want[key];ok{want[key]=true}}
    failed:=false;for route,found:=range want{if !found{fmt.Fprintln(os.Stderr,"missing route:",route);failed=true}}
    if failed{os.Exit(1)};fmt.Println("all required SuxinVideo routes registered")
}
