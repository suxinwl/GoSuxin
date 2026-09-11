# ORM、DAO 与事务

适用：Suxin Framework V1.0.0 / Suxin CLI V1.0.0。

应用使用数据库驱动模块注册到框架 gdb。业务后台默认 MySQL；框架还提供 PostgreSQL、SQLite、SQL Server、Oracle 和 ClickHouse 驱动，导入路径见根 README。驱动可编译不等于已在所有数据库版本实测。

通过 DAO 的 Ctx(ctx) 建立带上下文的查询，Where 使用参数化条件，Scan 读取类型化结果。写入时检查 error；更新和删除带明确条件。分页先限制查询范围，再取总数与列表。实体中的零值与数据库 NULL 不总是等价，应选用合适字段类型。

事务使用模型或数据库的 Transaction(ctx, func(ctx context.Context, tx gdb.TX) error { ... })。回调内传递事务 ctx 给 DAO；任一步错误返回后回滚，不吞掉错误再返回成功。远端云盘操作无法自动与 SQL 原子提交，因此附件删除会先确认云端操作结果再移除记录，并保留失败记录供重试。

数据库迁移在 manifest/sql，初始化 SQL 仅供新库使用。元数据表 app_version 记录应用版本，不能作为跳过全部迁移的唯一依据。
