# Suxin CLI：控制器生成

适用：Suxin Framework V1.0.0 / Suxin CLI V1.0.0。

```bash
suxin gen ctrl
suxin gen ctrl -m
```
先在 api 下定义 Req/Res。Req 中用 g.Meta 指定 path、method、tags 和 summary。默认模式按方法拆分控制器，-m 合并同一接口集合的方法。生成后在对应路由组绑定 NewX()；生成控制器并不自动开放权限。

手写实现留在可维护的方法文件，接口文件上的生成标记表明它会被重新生成。新增 API 后再生成并检查 git diff，确认没有覆盖业务逻辑。业务后台的 admin 路由还需要添加菜单或操作权限记录。
