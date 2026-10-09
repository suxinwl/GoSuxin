# 私有插件仓 1.1.5

由 GoframePro 在线插件 `privatecode` 1.1.5 适配至 Gosuxin。参考源码通过官方接口取得，原包位于忽略发布的 `runtime/online-plugin-review/privatecode.zip`。未导入上游演示代码、外部下载链接或旧项目业务数据。

后台入口：`/suxinweb/privatecode`。提供分类管理、代码资料、发布与草稿、ZIP 源码包上传、不可覆盖的历史版本、完整性校验及权限内下载。初始化只创建三张 `gf_privatecode_*` 表、基础分类和菜单权限。

源码包保存在非公开的 `storage/privatecode/packages`，必须通过已登录的后台接口下载。普通成员能查看自己及已发布的代码，只能修改本人资料；超级角色可管理全部资料。每个接口均使用主程序 RBAC，并登记在“私有插件仓”的菜单权限下。内容以普通文本显示，包内代码不会执行或解压到主程序目录。

插件市场支持即时安装、卸载。卸载隐藏菜单并关闭接口，保留分类、资料、版本文件及权限，重装后恢复。资料页面的“删除”会删除该资料、版本及文件。

Release 提供 `privatecode-1.1.5-windows-amd64.zip` 和 `privatecode-1.1.5-linux-amd64.zip`，采用 `suxin-runtime-v1`。上传到支持运行协议的宿主后即可安装、更新、卸载及重装，无需重新编译或重启宿主。原菜单、接口和权限继续使用。

上传代码用于版本存档，原始 ZIP 不会作为代码执行。当前源码发行包从同一提交的实际主线文件生成，供开发机修改；独立入口为 `plugins/privatecode/cmd`，运行包构建命令为 `python scripts/build-runtime-plugins.py --os linux --plugin privatecode --build-web`。源码 ZIP 不能作为服务器运行包安装。完整流程见 [运行插件指南](../../docs/runtime-plugins.md)。
