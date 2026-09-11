# 安装业务后台

适用：Suxin Framework V1.0.0 / Suxin CLI V1.0.0。

准备 Go 1.25.1 或更新的兼容工具链、MySQL 8、Node.js 与 npm。前端锁文件随源码提供，本次不升级前端依赖。克隆仓库后先把示例配置复制为本机配置，并填写独立数据库连接。

```bash
git clone https://github.com/suxinwl/GoSuxin.git
cd GoSuxin
go run main.go
```

配置文件位于 manifest/config；server.address 控制监听端口。首次运行通过安装页填写数据库及管理员账号密码，安装器导入初始化 SQL 并释放前端源码到 web。不要对已有业务库重新运行初始化 SQL。初始化没有通用管理员密码，使用安装时填写的密码。

后台部署入口为 /webadmin/。前端开发进入 web 执行 npm ci、npm run serve；发布执行 npm run build，再把 web/dist 内容部署到 resource/webadmin。后端从项目根目录运行，以便读取配置和静态文件。安装后保留 install.lock。

已有项目升级仅运行所需的增量 SQL。V1.0.0 的 manifest/sql/v1.0.0.sql 新建元数据表并写入版本，不重建业务库；自定义前缀时先将 gf_ 替换为实际前缀。先备份数据库及本机配置，验证备份可恢复。
