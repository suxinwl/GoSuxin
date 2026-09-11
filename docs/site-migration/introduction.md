# GoSuxin 开发框架

适用：Suxin Framework V1.0.0 / Suxin CLI V1.0.0。

GoSuxin 是包含业务后台、基础框架、数据库驱动和命令行工具的 Go 单仓库项目。后端采用 API、Controller、Service、Logic、DAO 分层；后台采用 Vue 3、TypeScript 和 Arco Design。业务后台提供登录、权限、用户、组织、字典、附件、代码生成和本地插件安装。

单仓库不等于单模块：framework、CLI 和每个驱动都有各自的 go.mod。完整仓库开发由根目录 go.work 关联；第三方业务项目引用正式模块版本。产品版本为 V1.0.0，源码版本值为 1.0.0，Go 模块标签为 v1.0.0。

本资料包基于当前源码重新编写，原文档的目录主题在 sources.json 中逐项对应。官网尚未上线，本地 Markdown 是当前文档入口。在线市场、购买、绑定、在线升级和远程模板暂不提供服务。历史接口、表前缀、配置环境变量可能保留兼容名称，不代表仍依赖原框架模块。

项目：https://github.com/suxinwl/GoSuxin 。维护团队：Suxin技术团队；公司：深圳市速信网络科技有限公司；联系：56308750@qq.com。
