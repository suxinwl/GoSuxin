# Android 客户端与局域网接入

## 可重复的集成验证

运行 `python plugins/suxinvideo/tools/test_app_clients.py`。脚本创建独立临时 MySQL 数据库，验证初次安装及重复安装、既有数据保留、客户端发布权限迁移、TV 配对与撤销、WebSocket 控制及越权拒绝、Jellyfin 登录／媒体库／收藏／进度以及递归媒体授权。测试使用实际本地 HTTP／WebSocket 连接和可控媒体服务，不重启当前 Go 服务，不修改正式影片或账号；完成后清理临时数据库与私有配置。结果写入 `data/tmp/app-client-integration.safe.json`，不包含密码或令牌。

该自动化验证不代替真实 TV、DLNA 接收设备和网易爆米花客户端的解码及交互验收。

## 原生客户端

手机和平板安装 `com.xiaoqi.video`，电视安装 `com.xiaoqi.video.tv`。业务接口前缀为 `/suxinvideo/app/v1`，使用会员 `Authorization: Bearer <access_token>`，与后台管理 JWT 分开。成功响应为 `{code:0,message:"",data:...}`；失败使用相应 HTTP 状态及同结构错误响应。

### TV 配对

1. TV 调用 `POST /devices/pair/create`，提交 `device_id`、`name`、`platform:"tv"`，获得 `pair_id`、六位 `code`、`poll_token`、`expires_at`、`qr_url`。
2. TV 使用 `GET /devices/pair/status?pair_id=...&poll_token=...` 轮询，每三秒一次；配对凭证五分钟到期。
3. 手机登录后调用 `POST /devices/pair/approve`，提交 `code`。
4. TV 轮询收到 `status:"approved"` 和 `tokens` 后保存会员会话。`poll_token` 不可用于其它业务请求。会话只签发一次；短期内重复轮询返回同一响应，服务重启后已消费配对需要重新申请。
5. 手机调用 `GET /devices` 查看已配对电视；`POST /devices/pair/revoke` 撤销设备，并同时撤销电视登录、投屏连接与已有配对。

### 自研 TV 投屏

手机调用 `POST /devices/cast/create {device_id}`，取得 `session_id`、`expires_at`、`ws_url`。TV 每三秒调用 `GET /devices/cast/current`，有活动会话时连接相同 WebSocket。

WebSocket 推荐用 Bearer 请求头认证，也支持 `access_token` 查询参数。连接校验账号和确切手机／TV 设备身份。手机消息为 `{type:"command",id,action,payload}`，动作包括 `play`、`resume`、`pause`、`seek`、`next`、`quality`、`stop`；播放载荷使用稳定影片、线路和分集身份。TV 回传 `{type:"status",payload}` 或 `{type:"ack",id,payload}`。尚未连接 TV 时，服务器保留最多十六条待处理命令并在 TV 连接时依次发送。会话两小时到期，定期复核登录与撤销状态。

### DLNA 媒体准备

手机可把现有 HLS／MP4 流通过自己的局域网临时中继提供给所选设备。设备不支持 HLS 时：

- `POST /cast/prepare` 提交与 `/playback/resolve` 相同的影片、线路、分集及清晰度标识，立即获得 `job_id`。
- `GET /cast/prepare/{id}` 返回状态、真实转换进度、已处理媒体时长、总时长、大小、错误及完成后的 `url`。
- 状态包括 `queued`、`resolving`、`running`、`completed`、`failed`、`cancelled`。总时长未知时使用处理时长，不推算虚假的百分比。
- `POST /cast/prepare/{id}/cancel` 停止任务并撤销媒体文件访问。完成后的媒体仍校验当前账号、设备和逐影片权限。

MP4 输出为 H.264／AAC，使用与在线播放共用的两路 FFmpeg 预算，准备任务为后台优先级。红果加密源先由校验证书及远端地址的 Go HTTP 传输下载到私有文件，再解密转换，避免与按需 HLS 形成嵌套转码等待。单文件限 1GB，临时缓存预算 5GB，授权两小时到期；服务器重启后的未完成任务显示失败，需重新准备。

## APP 发布

后台“客户端发布”上传签名 APK。服务器用 Android SDK 的 `apksigner.jar` 和 `aapt` 检查签名、包名、版本名称、版本号、最低 API，再计算 SHA256。相同版本号不可替换为不同二进制；同类型更新须保持原签名。

发布服务器需配置 `ANDROID_HOME`／`ANDROID_SDK_ROOT` 和 `JAVA_HOME`。SDK 工具只用于发布校验，普通浏览与播放不依赖它。Windows 直接使用 Java 调用签名工具，不拼接命令行执行批处理。

`GET /releases/latest?platform=mobile|tv&version_code=当前版本` 返回 `available` 和 `release`，包含版本、最低 API、大小、SHA256、下载 URL、更新说明。APP 必须验证下载校验值，由用户确认安装。网页入口为 `/suxinvideo/app-download`，二维码只指向已发布文件；下架立即停止下载与更新推荐。

## 网易爆米花／Jellyfin

用户、媒体库、影片、剧集、季与分集的对外 ID 使用标准 UUID 格式，兼容严格的客户端 DTO 解析。ID 内包含可还原的 CMS 记录编号及类型，分集和季同时包含版本指纹，不新增映射表；既有整数数据和观看记录不变。接受带连字符或 32 位十六进制 UUID，也保留旧适配器书签的读取。官方 [`UserDto.Id`](https://github.com/jellyfin/jellyfin/blob/master/MediaBrowser.Model/Dto/UserDto.cs) 与 [`BaseItemDto.Id`](https://github.com/jellyfin/jellyfin/blob/master/MediaBrowser.Model/Dto/BaseItemDto.cs) 的类型均为 `Guid`，因此不使用 `u123`、`library-all` 等任意字符串作为新的响应 ID。

默认局域网地址：`http://192.168.10.10:8601`。客户端选择 Jellyfin，使用网站会员邮箱和密码。监听只能绑定回环或私有 IP，公网请求被拒绝；本站原生 APP 保持 HTTPS。

兼容接口覆盖服务器信息、登录／退出、媒体库、列表与搜索、影片详情、按版本区分的季／集、图片、播放信息、媒体流、收藏与观看进度。会员、禁用账号、下架、内容屏蔽及积分解锁规则复用 CMS。其它 Jellyfin 管理操作返回不支持，不能作为通用 Jellyfin 管理服务使用。

Jellyfin 使用独立可撤销设备会话，有效期三十天；官方协议不支持原生 APP 的刷新令牌交换。内部媒体代理严格校验本站 HTTPS 证书，转换为局域网媒体地址，递归保留影片授权；不会把官方客户端重定向到自签名公网 HTTPS。

初次连接、真实电视解码与遥控器行为需在实际网易爆米花及 DLNA 设备上验收。公网网易爆米花接入待可信 HTTPS 证书具备后启用。
