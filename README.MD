# GoSuxin V1.0.0

GoSuxin 是由 Suxin技术团队维护的 Go 开发框架与管理后台，包含独立基础框架、Suxin CLI、数据库驱动、Redis 适配器和 Vue 3 + TypeScript 后台。

- 项目仓库：[suxinwl/GoSuxin](https://github.com/suxinwl/GoSuxin)
- 发布记录：[V1.0.0](https://github.com/suxinwl/GoSuxin/releases/tag/v1.0.0)
- 运行插件发行版：[2026-10-09](https://github.com/suxinwl/GoSuxin/releases/tag/v1.0.0-runtime.20261009)，包含 Windows/Linux x64 运行包、对应源码和校验清单。
- 本次变更：[迁移与运行插件说明](docs/changes/2026-10-09.md)；安装部署：[运行插件指南](docs/runtime-plugins.md)。
- 官网预留：[Suxin](https://www.suxinwl.com)，文档网站暂未部署。

## 开始开发

准备 Go 1.25.1 或更新的兼容工具链、MySQL 8、Node.js/npm 和 Python 3（初始化配置脚本使用）。

```bash
git clone https://github.com/suxinwl/GoSuxin.git
cd GoSuxin
python scripts/init-local-config.py
go run main.go
```

脚本仅创建缺失配置并生成本机鉴权密钥，保留已有配置。编辑 manifest/config/config.yaml 填写自己的数据库，再通过安装流程设置管理员账号和密码；没有通用默认密码。安装器使用独立数据库，勿对旧业务库重跑初始化 SQL。

后台默认地址 `http://127.0.0.1:8601/suxinweb/`。前端源码在 web，构建后部署到 resource/suxinweb：

```bash
cd web
npm ci
npm run build
npm run type:check
npm run test:layout
```

前端已合入 GoframePro 2.9.7 的界面增强，Arco Design 固定为 2.58.0；类型检查仍有既有诊断，构建成功不代表类型检查全部通过。真实配置、上传文件、运行数据和缓存不进入发布仓库。

在右上角设置面板可切换左侧、顶部、双列布局，以及半圆角、卡片、间隔卡片、圆角页签。保存设置后刷新仍保留选择，旧版浏览器设置自动补齐新增字段；默认保持左侧布局。开发和生产环境的 `VITE_APP_SETTING=false` 可隐藏布局选项。

开发机保留 Suxin CLI 代码生成和源码打包工具。服务器插件安装使用预编译的 `suxin-runtime-v1` ZIP，不执行 Go 编译、npm 安装或代码生成。独立页面同时保留 `business_id` 并提供 2.9.7 的 `tenant_id` 参数，兼容已有插件。

修改前端后运行 `python scripts/repack_webcode.py` 同步安装包，再运行 `python scripts/check-release.py` 检查源码与安装包一致性。构建产物位于 `web/dist`，部署目录为 `resource/suxinweb`。在线代码服务继续保持筹备状态；本次合并不改变 Suxin 模块名、版本和数据库初始化方式。

## 本地业务插件

已集成 [速信影视 CMS](plugins/suxinvideo/README.md)、[电子画册](plugins/ebook/README.md)、[私有插件仓](plugins/privatecode/README.md) 和 [统计分析](plugins/analysis/README.md)，共用主程序后台登录及权限，仅迁入表结构、菜单、分类和默认配置。影视内容、会员订单、画册及上传文件均不从旧项目导入，自动采集和直播后台任务默认关闭。私有插件仓支持源码版本存档和权限内下载，统计读取真实日志与业务数据。

后台菜单包含“影视CMS”“画册中心”“私有插件仓”和“统计分析”，首页快捷操作也包含“影视CMS后台”；私有仓与统计入口为 `/suxinweb/privatecode`、`/suxinweb/analysis`。画册中英文前台为 `/albums/`、`/albums-en/`，影视前台为 `/suxinvideo/`。Release 提供四个插件的当前源码包和两个平台的运行包；完整仓库源码包含共用框架及依赖模块。小柒影视 Android 手机/平板和 TV 开发源码位于 `apk/videoapk/android`，版本为 1.0.16。

插件市场显示四个插件及安装状态，支持“进入后台”“卸载”和“安装”。四个已编译内置插件也可切换到独立运行包；后续安装、更新、卸载和重装均无需重编译或重启宿主。上传运行包校验平台、路径、文件哈希及健康检查，启动失败恢复旧版本。卸载关闭接口和后台任务并隐藏菜单，保留数据表、角色权限、上传文件及配置，重装恢复功能。内置状态保存在 `manifest/codeinstall/state.json`，运行版本及状态保存在 `storage/plugins/<name>/active.json`。

首次使用本协议需要升级宿主及后台静态文件；旧宿主不能直接安装运行包。插件源码包供开发机修改和编译，不作为服务器安装包。插件更新会替换该插件进程，正在执行的插件长任务可能中断；宿主服务持续运行。详情见 [运行插件指南](docs/runtime-plugins.md)。

## Suxin CLI V1.0.0

发布模块的固定版本安装命令：

```bash
go install github.com/suxinwl/GoSuxin/framework/cmd/suxin@v1.0.0
suxin version
suxin init demo -g example.com/demo
cd demo
go mod tidy
suxin run main.go
```

完整源码开发可先在仓库根目录运行 `go build -o bin/suxin ./framework/cmd/suxin`。需要本地 SDK 时使用 `suxin init demo --sdk /path/to/GoSuxin/framework`。在线升级和远程模板暂停，初始化使用内置模板。


可通过 `suxin --help` 和 `suxin <命令> --help` 查看命令参数。框架说明见 [framework/README.MD](framework/README.MD)。

## 模块与版本

所有 Suxin 模块版本为 v1.0.0，根目录 go.work 仅用于同仓库开发。发布 go.mod 不使用本机 replace。

| 内容 | Go 模块 |
|---|---|
| 应用 | github.com/suxinwl/GoSuxin |
| 框架 | github.com/suxinwl/GoSuxin/framework |
| CLI | github.com/suxinwl/GoSuxin/framework/cmd/suxin |
| SQL 驱动 | github.com/suxinwl/GoSuxin/framework/contrib/drivers/{mysql,pgsql,sqlite,mssql,oracle,clickhouse} |
| Redis | github.com/suxinwl/GoSuxin/framework/contrib/nosql/redis |
| 生成 SDK 的 HTTP 客户端 | github.com/suxinwl/GoSuxin/framework/contrib/sdk/httpclient |

产品显示 V1.0.0，配置和数据库存储 1.0.0。已有数据库使用 `manifest/sql/v1.0.0.sql` 幂等增量迁移；保留现有数据库名、表前缀和业务数据。不同前缀需要调整 SQL。

## 功能与维护

后台提供登录、RBAC、用户组织、系统配置、字典、附件、本地代码生成及插件打包安装。123 云盘支持连接测试、上传、稳定公开访问和回收站删除；密钥由部署者填写。

公司：深圳市速信网络科技有限公司。维护团队：Suxin技术团队。邮箱：56308750@qq.com。

项目采用 MIT 许可。第三方许可证、版权声明和源码头部声明保留，详见 [LICENSE](LICENSE)、[框架许可证](framework/LICENSE) 及各子模块 LICENSE。
