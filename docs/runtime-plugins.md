# GoSuxin 运行插件

影视 CMS、电子画册、私有插件仓、统计分析已提供独立运行包。宿主完成本次升级后，上传 ZIP 即可安装或更新；卸载和重新安装也不重新构建、不停止宿主服务。旧的后台、阅读器和影视 APP API 地址继续有效。

## 使用

1. 在“插件市场”上传 `suxin-runtime-v1` ZIP。运行包必须与宿主系统和 CPU 架构一致。
2. 上传时校验包结构、平台、路径和每个文件的 SHA-256；安装时启动预编译的插件进程，通过健康检查后生效。
3. 同名包执行更新，启动失败会恢复之前的运行版本。
4. 点击“卸载”关闭插件并隐藏菜单；保留安装包、业务数据、配置、文件、菜单 ID 和角色授权。点击“安装”直接恢复。

服务器不需要 Go、npm 或源码编译环境。历史 `suxin-source-v1` 和 `config.yml` 源码包用于开发机归档，不能作为运行包上传。安装接口已移除旧的服务器代码生成、npm 安装和删表卸载流程。

## 包位置

公开发行包从 [GitHub Release](https://github.com/suxinwl/GoSuxin/releases/tag/v1.0.0-runtime.20261009) 下载。运行 ZIP 与源码 ZIP 分别标注，`packages.json` 记录对应提交及包校验值，`SHA256SUMS.txt` 可用于核对下载。

- Windows x64：`devsource/codemarket/runtime-release/windows-amd64/`
- Linux x64：`devsource/codemarket/runtime-release/linux-amd64/`

每个平台都有四个插件 ZIP、宿主二进制和 `packages.json` 校验清单。ZIP 包含插件二进制、编译后的管理页面和必要静态资源，不含数据库记录、账号密码、业务上传或云盘密钥。

私有插件仓已另存四个“运行包”条目，每个包含 Windows x64 和 Linux x64 两个版本。原来的源码及小柒影视 APP 条目保留，下载文件的大小和 SHA-256 已逐包核对。

首次升级服务器时备份并替换宿主二进制、`resource/suxinweb`，保留已有 `manifest/config`、`manifest/codeinstall/state.json`、数据库、`storage` 和业务上传目录，然后启动新宿主。后续只上传插件运行包。此步骤需要一次宿主升级，正在运行的旧宿主无法自动获得新加载协议。

宿主部署 ZIP 包含可执行程序、已构建后台、基础静态资源和公开配置模板。新部署先运行 `python scripts/init-local-config.py`，再填写自己的数据库配置并按安装流程创建管理员。现有部署应在独立目录解压后只替换程序和需要更新的静态资源，保留原配置和业务目录；Linux 需 `chmod +x gosuxin`。在部署根目录启动宿主。

## 开发与部署

```powershell
python scripts/build-runtime-plugins.py --os windows --build-web --build-host
python scripts/build-runtime-plugins.py --os linux --build-host
```

可用 `--plugin ebook` 单独构建，`--arch arm64` 构建其他架构。编译只发生在开发机；生成的包由服务器直接执行。

后台上传是推荐方式。批量安装可调用 `scripts/install-runtime-plugin.ps1`，传入运行包路径、当前管理员会话的短期 Authorization 和 apiverify 请求头。脚本不保存凭据。请求头需新鲜有效，多个大包建议逐个获取当前会话的请求头。

运行协议入口位于 `internal/pluginworker`，四个入口位于 `plugins/<name>/cmd`。宿主管理器位于 `internal/runtimeplugin`。新插件可按同一协议独立编译，无需将业务 Go 代码链接到宿主。

## 状态与数据

- 不变的版本目录：`storage/plugins/<name>/packages/<sha256>/`
- 当前版本和卸载状态：`storage/plugins/<name>/active.json`
- 通用插件私有数据：`storage/plugins/<name>/data/`
- 日志：`runtime/plugins/<name>.log`

四个迁移插件继续使用原来的数据库表和业务文件目录。宿主先校验登录及原来的动作权限，插件通过独立随机密钥的本机通道获取身份；客户端自行提交的插件身份会被丢弃。管理页面使用短期 HttpOnly Cookie，退出宿主会话后不能继续使用该 Cookie。

操作日志移除 Authorization、Cookie、apiverify 以及插件密钥和身份头；JWT 只接受 HS256，短令牌及无效动态令牌会被拒绝。代理响应保持原始 JSON、文件字节、HTTP 状态和 Range 行为。

主服务在插件更新期间持续运行，插件进程会被替换；该插件正在处理的上传、播放或后台任务可能中断，更新应避开长任务。首次由内置逻辑迁移为运行插件时，宿主释放该插件后台任务和锁，再由独立进程接管。

画册 PDF 转换仍需 Poppler。Linux 影视转码需系统 FFmpeg。Windows 运行包沿用已有 FFmpeg。Linux 包在 Windows 上完成交叉编译和包检查，Linux 实机运行仍需在目标服务器验收。
