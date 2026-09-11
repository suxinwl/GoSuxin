# 前端开发与组件

适用：Suxin Framework V1.0.0 / Suxin CLI V1.0.0。

前端源码为 web，使用 Vue 3.5.13、TypeScript 4.9.5、Vite 3.2.10、Arco Design 2.57.0；准确安装结果以 package-lock.json 为准。运行 npm ci 使用锁文件，不在本次迁移中自动升级依赖。

views 保存页面，api 保存请求，components 保存复用组件，router 与权限加载配合，locale 保存多语言。新增后台页面先实现 API，再配置菜单路由与操作权限；请求通过现有封装发送，保持 token 和响应协议一致。

文件选择器、上传器、用户头像和列表统一识别本站附件路径。不要手动把当前存储域名拼到所有旧附件前面。新增图片保持比例，使用本站静态资源。深浅主题应依赖主题变量；新增可见字符串提供中文、英文及繁体翻译。

构建命令 npm run build；类型检查 npm run type:check。当前旧工程存在类型诊断，详见验证报告；构建通过不等于类型检查通过。
