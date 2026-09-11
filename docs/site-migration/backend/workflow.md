# 后端接口开发完整流程

适用：Suxin Framework V1.0.0 / Suxin CLI V1.0.0。

新增接口先明确输入、输出和访问权限。在 api/admin 对应业务目录定义 Req/Res，在 Req 的 g.Meta 中声明方法和路径。执行 suxin gen ctrl，再在 internal/controller/admin/admin_router.go 绑定相应控制器。控制器调用 service；具体逻辑放入 internal/logic 并注册。

数据层变更先编写迁移，再对隔离开发库执行，然后 suxin gen dao；逻辑方法完成后 suxin gen service。接口需要真实权限记录，给测试角色授权后验证允许访问和拒绝访问两种情形。不要为了调通接口添加 noLogin 或 noAuth。

以笔记模块为例：创建 sx_note(id,title,created_at)；API 接受经过长度校验的 title；逻辑写入表并返回 ID；查询接口按当前用户或组织范围过滤。删除接口应检查记录所属范围，不能仅凭前端隐藏按钮控制。教程表使用隔离库，不写入当前业务库。

本项目的图形代码工具可以从表结构生成基础文件，生成后仍需检查业务校验、权限和错误处理。代码生成负责样板结构，不会自动设计数据隔离策略。
