# 系统配置与版本

适用：Suxin Framework V1.0.0 / Suxin CLI V1.0.0。

manifest/config/config.yaml 保存服务与数据库设置；app.yaml 保存品牌、协议、版本和前端源码位置；upload.yaml 分别保存本地及云存储设置。hack/config.yaml 是 CLI 生成和构建配置，顶层节点为 suxincli。

公开源码使用示例配置，本机配置独立保留。新增配置字段要同步表单、后端读取、保存校验、示例及文档。密钥字段读取时仅返回已配置状态，编辑留空保留原值；操作日志必须脱敏。

应用配置与前端 package.json 均为 1.0.0，显示时加 V。数据库 gf_app_meta 使用 meta_key=app_version、meta_value=1.0.0。Go 模块使用 v1.0.0 标签，不能把第三方依赖版本一并替换。
