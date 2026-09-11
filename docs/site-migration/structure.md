# 项目结构与模块边界

适用：Suxin Framework V1.0.0 / Suxin CLI V1.0.0。

| 目录 | 职责 |
|---|---|
| api | 请求、响应、路由元数据和校验规则 |
| internal/controller | 接收请求并调用业务服务 |
| internal/logic | 业务规则、事务和外部服务操作 |
| internal/service | 业务接口和注册入口 |
| internal/dao、internal/model | 数据访问与实体模型 |
| utility | 鉴权、上传、配置和通用工具 |
| framework | Suxin 底层框架独立模块 |
| framework/cmd/suxin | CLI 独立模块与内置模板 |
| framework/contrib | 数据库驱动、Redis 适配器 |
| web | 后台前端源码 |
| resource/webadmin | 已构建后台静态资源 |
| resource/static/brand | 本站品牌与默认头像 |
| devsource/developer | 生成模板、插件和安装资源 |
| manifest/config、manifest/sql | 配置和数据库迁移 |
| docs/site-migration | 可迁移到官网的本地文档 |
| runtime | 本机缓存、日志和临时产物，不进入发布源码 |

main.go 导入命令及初始化逻辑，internal/router 组织路由，controller 绑定业务对象。不要把跨模块共享代码放进别的模块的 internal 目录。生成文件与手写实现分别维护；在修改生成模板后，应使用隔离项目检查新输出的导入路径。
