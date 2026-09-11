# GoSuxin V1.0.0

GoSuxin 是由 Suxin技术团队维护的 Go 开发框架与管理后台，包含独立基础框架、Suxin CLI、数据库驱动、Redis 适配器和 Vue 3 + TypeScript 后台。

- 项目仓库：[suxinwl/GoSuxin](https://github.com/suxinwl/GoSuxin)
- 本地文档：[GoSuxin V1.0.0 配套资料](docs/site-migration/README.md)
- 发布记录：[V1.0.0](https://github.com/suxinwl/GoSuxin/releases/tag/v1.0.0)
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

后台默认地址 `http://127.0.0.1:8601/webadmin/`。前端源码在 web，构建后部署到 resource/webadmin：

```bash
cd web
npm ci
npm run build
npm run type:check
```

前端依赖保持既有版本，已知类型诊断及验证范围见[验证报告](docs/site-migration/validation.md)。真实配置、上传文件、运行数据和缓存不进入发布仓库。

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

[安装](docs/site-migration/cli/install.md) · [初始化](docs/site-migration/cli/init.md) · [运行](docs/site-migration/cli/run.md) · [编译](docs/site-migration/cli/build.md) · [控制器](docs/site-migration/cli/gen-ctrl.md) · [DAO](docs/site-migration/cli/gen-dao.md) · [Service](docs/site-migration/cli/gen-service.md) · [快速开始](docs/site-migration/quick/index.md)

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

后台提供登录、RBAC、用户组织、系统配置、字典、附件、本地代码生成及插件打包安装。123 云盘支持连接测试、上传、稳定公开访问和回收站删除；密钥由部署者填写。首页已移除演示统计；原在线市场、支付、绑定入口显示筹备提示。

公司：深圳市速信网络科技有限公司。维护团队：Suxin技术团队。邮箱：56308750@qq.com。

项目采用 MIT 许可。必要第三方许可、版权和测试来源记录保持完整，详见 [LICENSE](LICENSE) 和[来源记录](docs/upstream/README.md)。
