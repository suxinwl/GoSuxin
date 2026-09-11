# Suxin CLI：Service 接口生成

适用：Suxin Framework V1.0.0 / Suxin CLI V1.0.0。

```bash
suxin gen service
```
默认扫描 internal/logic 下的模块目录，按 sName 接收器的方法生成 internal/service 的接口和注册函数。逻辑模块在 init 中调用 RegisterName，程序启动时须导入逻辑包，否则调用入口可能提示服务未注册。

接口用于控制器与逻辑模块解耦。跨模块方法使用 context.Context 传递取消和事务上下文。先写实现再生成接口只是本项目的一种组织方式，也可以手写接口；手写文件不要带自动生成标记。生成后执行 go test ./...，确认注册、签名和导入不存在循环依赖。
