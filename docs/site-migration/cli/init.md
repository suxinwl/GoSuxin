# Suxin CLI：初始化项目

适用：Suxin Framework V1.0.0 / Suxin CLI V1.0.0。

```bash
suxin init demo -g example.com/demo
cd demo
go mod tidy
suxin run main.go
```
初始化从 CLI 内置资源导出，要求目标目录为空。-g 指定 Go 模块名；不指定则使用项目名。开发尚未发布的 SDK 时使用 suxin init demo --sdk /absolute/path/GoSuxin/framework；这会在生成项目增加本地 replace，适合开发机，不能当作正式发布 go.mod。

-m 建立多项目目录，-a 建立子应用。远程模板 -r、交互选择 -i、在线更新 -u、远程版本选择 -s 暂停并返回明确错误。内置模板含 API、控制器、配置和 main.go；新项目不包含业务后台的账号、云盘配置或数据。
