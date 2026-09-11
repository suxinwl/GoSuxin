# Suxin CLI：开发运行

适用：Suxin Framework V1.0.0 / Suxin CLI V1.0.0。

```bash
suxin run main.go
```
在项目根目录运行，CLI 编译后启动程序并监听源码变化。配置由 hack/config.yaml 的 suxincli.run 节点读取，执行 suxin run -h 查看当前选项。业务服务端口来自项目 server.address，与 CLI 无关。已有 8601 服务运行时，隔离样例应设置另一个端口。

运行后访问实际业务路由；终端中断用于结束开发会话。常驻部署建议使用编译后的程序交由系统服务管理，不把文件监听器作为生产进程管理器。编译错误先检查 go.mod、工作区和完整错误输出，再检查服务端口。
