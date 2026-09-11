# 静态资源与部署路径

适用：Suxin Framework V1.0.0 / Suxin CLI V1.0.0。

resource/webadmin 对应 /webadmin/ 后台静态入口；resource/static/brand 提供本站品牌头像，浏览器通过 /resource/static/brand/logo.png 访问。前端构建 base 必须适配 /webadmin/，不假设部署在网站根路径。

resource 中的公开目录只存可公开文件。上传凭证缓存、数据库备份、访问令牌、日志和临时文件放在 runtime 或其他非公开目录。CLI 资源打包把指定资源嵌入 Go 文件，修改模板后必须重新生成并验证导出内容。

图标更新带版本参数用于刷新缓存，保持 logo 原始比例。反向代理配置应保留 API 与静态路径，不能将所有请求都回退到后台 index.html，否则下载路径会返回 HTML。
