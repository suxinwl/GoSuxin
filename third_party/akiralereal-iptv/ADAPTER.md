# Xiaoqi IPTV 内部服务

本目录完整保存 akiralereal/iptv v4.27.0，commit `7b1e2d5fdc6dbe35de63f099be6264a201c6a975`。原文件不改动；`UPSTREAM.json` 记录每个原文件的 SHA256。适配器位于 `adapter/`，同样使用 GPL-3.0-only。上游 README、LICENSE、package-lock.json 保留。软件许可证不授予第三方节目的转播权。

## 启动约定

部署脚本使用 Node.js 24+（上游最低 Node 20），执行 `npm ci --omit=dev`。Chromium 用于网页取流及央视频会员桥；已有 Chrome 时可设 `PUPPETEER_SKIP_DOWNLOAD=true` 安装，并用 `PUPPETEER_EXECUTABLE_PATH` 指定浏览器。普通 HTTP 模块无浏览器依赖。适配器直播路径不运行 FFmpeg；宿主按需要处理转码。

宿主以项目根目录作为 cwd，启动 `node third_party/akiralereal-iptv/adapter/server.mjs`，设置：

| 环境变量 | 值 |
|---|---|
| `IPTV_PROFILE` | `public` 或 `member` |
| `IPTV_PORT` | 公开实例 9180；账号实例 9181 |
| `IPTV_DATA_DIR` | 独立绝对运行目录，例如 `data/iptv/public`、`data/iptv/member` |
| `IPTV_INTERNAL_SECRET` | 32 字节以上的服务间密钥；由 Go 管理，不在请求日志和公开页面显示 |
| `IPTV_PARENT_PID` | 宿主 PID；消失后 Node 退出并释放浏览器 |
| `IPTV_BROWSER_CONCURRENCY` | 默认 2，上限 2 |
| `IPTV_SOURCE_CONCURRENCY` | 默认 4，上限 4 |
| `IPTV_RESOURCE_DIR` | 可选，共享且不含凭据的资源租约目录；默认两个 profile 数据目录的父目录下 `resource-leases` |
| `IPTV_AUTO_REFRESH` | 默认自动启动刷新；测试可设 `false` |

服务只绑定 `127.0.0.1`，不加载上游 `app.js`，不暴露其 admin/API/播放器。目录的 profile 标记与 PID 锁阻止实例共用配置。公开实例拒绝登录和 secret 配置，且删除继承的平台凭据环境变量。账号实例初始所有模块关闭，管理员启用对应模块后使用本人凭据。公开实例默认启用电视模块；B站、抖音、虎牙、斗鱼及景观模块需手动开启。自动刷新保留上游模块的失败缓存和退避。

两个实例共享原子文件资源租约，主机最多 2 个 Chromium、4 个源刷新任务；租约只含 PID/随机标识，不读取另一个 profile 的账号文件。浏览器限制接入上游 `BrowserPool.acquire`，并将会员桥 `maxActiveChannels` 限为 2，原始源码保持不变。进程消失后的过期资源租约按 PID 回收。FFmpeg 由 Go 单独限制。媒体请求采用 20 秒连接/响应头超时、30 秒 TCP 空闲超时；健康连续 FLV 流没有总时长超时。

FLV 与 HLS 使用上游各自的媒体请求头规则：HLS 固定配置使用浏览器 UA，函数配置可指定播放器 UA；FLV 默认使用原生 fetch 的 `User-Agent: node` 与 `Accept-Encoding: identity`。凤凰 CDN 在本机给浏览器 UA 的 FLV 请求仅输出短暂片段并正常 EOF，故不能把 HLS UA 应用于 FLV。每个 checkedFetch agent 独立绑定已验证 DNS，body 关闭立即销毁对应 socket。

## HTTP 合同 v1

所有接口及所有媒体请求均要求 `X-IPTV-Secret`。JSON 成功使用 `ok:true`，错误为 `{ok:false,error,message}`。账号凭据不会回传，模块 secret 字段只回传 `secrets_set`。

| 方法与路由 | 请求 / 响应 |
|---|---|
| GET `/internal/health` | `{ok,version,commit,profile,ready,refreshing,updated_at,channel_count,epg_refreshing,epg_updated_at,epg_failures,browser_concurrency,source_concurrency}` |
| GET `/internal/catalog` | `{ok,profile,updated_at,channels:[{provider_key,provider_ref,name,group,logo,tvg_id,epg_id,format,catchup,account_required}]}` |
| POST `/internal/resolve` | `{provider_key,provider_ref,format?:"hls"或"flv",start?:RFC3339,end?:RFC3339,refresh?:boolean}` → `{ok,url,headers,format,mime,expires_at,catchup}` |
| GET `/internal/epg` | XMLTV 正文；返回的是最近成功缓存，不临时触发网络 |
| GET `/internal/modules` | `{ok,profile,modules:[{id,name,description,category,enabled,capabilities,config_schema,config,secrets_set,login,browser_login,refresh_minutes,health}]}` |
| POST `/internal/sync` | `{modules?:[moduleId]}` → HTTP 202 `{ok:true,refreshing:true}`；通过 health 查进度 |
| POST `/internal/modules/config` | `{id,enabled?:boolean,config?:object,refresh_minutes?:number}` → `{ok,profile,module}` |
| POST `/internal/modules/login` | `{id,action,key?,payload?}` → `{ok,data}`；仅 member |
| GET/HEAD `/internal/media/<opaque>/index.<ext>` | 受保护的 HLS、FLV、分片或密钥 |

登录 action 为 `start`、`poll`（B站二维码）；`browserStart`、`browserStatus`、`browserCheck`、`browserCancel`、`browserClose`、`browserImport`（央视频浏览器登录）。`browserImport.payload` 是完整 Cookie 请求头字符串。需要 Token/Cookie 的其他平台通过 schema 的 secret 字段保存。系统不接收账号密码。`browserStart` 只在有桌面的服务器有意义；远程/NAS 使用导入。

`provider_key` 是上游 module id。`provider_ref` 优先使用上游 `deferredRef`（如 `ysp-cctv1`、`bili-123`、数字咪咕 pID）；直链使用 `direct-<sha256(module id + NUL + 原频道名)前24位>`。身份不含短期签名地址，重命名后的业务显示名由宿主保存。`tvg_id/epg_id` 对应 XMLTV 输出 ID。

resolve 返回本实例 origin 的 opaque 地址，headers 中密钥仅供 Go 固定 origin transport 使用，禁止写到客户端播放地址或普通 HTTP 日志。HLS 的子清单、AES 密钥、分片均改写为 opaque 内部入口。每跳上游 URL 校验公网地址、重定向和 DNS 解析结果，固定连接到已检查的地址；只允许公网域名的 TUN 假 IP `198.18.0.0/15`，不允许该网段字面 IP。内部密钥不发送到 CDN，跨 origin 不继承 Cookie/Authorization。Go 必须继续校验固定 origin 和 `/internal/media/` 路由。

`expires_at` 为本机媒体租约期限，不是声称上游签名的有效期。直播 20 分钟并使用时续期；有限回看/赛事 ENDLIST 清单的子分片及 key 使用资产时长加 20 分钟，上限 24 小时加 20 分钟，防止长回放后段被本机提前失效。清单请求会重新调用平台解析器，因此直播续签遵循各平台缓存。关闭模块或账号变更立即撤销其媒体租约；重启后客户端需重新 resolve。有限回放的官方短期签名仍可能过期，客户端需重新解析，不能把本机租约当作官方版权/签名承诺。

`refresh:true` 最多每频道每十秒清理一次模块取流缓存，不清登录态。源目录刷新按模块周期和失败退避执行；EPG 自动刷新最短一小时，管理员手动 sync 可立即重取。Go 的二进制路径环境变量是 `SUXIN_NODE` 和 `SUXIN_FFMPEG`，浏览器使用上游 `PUPPETEER_EXECUTABLE_PATH`。

成功的扫码 poll、已认证 browserCheck/browserImport 和模块配置变更会撤销对应模块所有媒体租约及取流缓存；咪咕同时撤销赛事租约。租约身份包含模块配置代数，后续新账号 resolve 也不会恢复旧账号 opaque URL。登录响应只包含状态、昵称及 QR 流程标识，不包含 Cookie/Token。

## 手动体育赛事

`migu-sports` 是默认关闭的适配器虚拟模块。上游 `extractors/migu.fetch` 只取电视，赛事在 `utils/updateData.js:updatePE`；适配器复用其官方 `match-list`、`basic-data`、`all-view-list` 协议及原始 `migu.resolve(pID)`，不执行上游播放列表文件写入。赛事账号与画质使用同 profile 的 `migu` 配置，public 仍为游客。赛事失败不回填旧 pID。

赛事 catalog 额外字段为 `kind: event_live|event_replay`、`mode: live|event_replay`、`programme_id`、`title`、`start/end`（RFC3339 或 null）、`duration_seconds`（上游回放时长或 null）。identity 是 `event-<比赛mgdbId>-<live|replay>-<pID>`，不含签名地址。只纳入官方 liveList/replayList 已发布的节目，过滤集锦与训练。回放是独立的有限 VOD，不拼电视回看时间窗，所以 `catchup:false`。resolve 返回相同 mode/kind/programme_id/duration_seconds；电视时间窗返回 `mode:catchup`。start/end 描述比赛时段；duration_seconds 描述某一回放资产，两者不强行相等。平台版权、会员和地区门控仍适用。

## 回看与实际边界

仅咪咕且 `wantsPlayback` 的频道暴露 `catchup:true`；将上海时区时间转换为上游 `playbackbegin/playbackend`。还接受 14 位上海时间。其他模块收到 start/end 明确拒绝，不用节目单推断可回看。上游仍可能因账号、版权时段或地区限制拒绝。没有 DVR 录制 API，也不保证节目单里的每一档存在回看视频。

这层统一全代理适用于宿主内部播放器与 FFmpeg。部分 CDN 限制服务端取分片，公开部署的真实网络、浏览器、防火墙及平台状态必须实测；导入数量不等于可播数量。央视频会员桥最多同时两路、有起播缓冲，必须本人登录验证。上游 HTTP 模块和浏览器配置不是通用远程 URL 代理。

## 验证及发布

`node --test adapter/adapter.test.mjs` 测试鉴权、身份稳定、回看、HLS 改写、SSRF、重定向凭据隔离与完整 HTTP 合同。`python adapter/verify.py` 验证原文件哈希，并同步纯源码镜像到 `plugins/suxinvideo/go/third_party/akiralereal-iptv`；不复制 node_modules、运行目录、缓存、Cookie、日志和内部密钥。目标主机执行 npm ci。Go 启动器应回收整个进程树，Windows 使用 Job Object，Linux 由 parent watchdog 补充回收。

Ubuntu 运行 `sudo bash adapter/install-ubuntu.sh`，Windows 运行 `powershell -File adapter/install-windows.ps1`；只安装 Node/npm、Chrome/Chromium、FFmpeg 依赖并测试，不启动独立 Node 服务。Ubuntu Go systemd 配置参考 `adapter/xiaoqi.service.example`（`KillMode=control-group`）；Go 从项目根目录统一启动两个 profile。真实抽样见 [adapter/PROBE.md](adapter/PROBE.md)。打包必须使用 UPSTREAM.json 原文件白名单及 adapter 公开源码，排除整棵 runtime/data、node_modules、Cookie/browser profile、日志、内部密钥。
