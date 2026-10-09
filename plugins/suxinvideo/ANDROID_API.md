# 小柒影视原生接口

基础地址：`https://xq.suxinwl.com:8600/suxinvideo/app/v1`。
机器可读合同：[app-v1.openapi.json](docs/app-v1.openapi.json)。

## 响应与认证

JSON 响应固定为 `{"code":0,"message":"ok","data":...}`。失败使用对应 HTTP 状态，`code` 为相同状态值，`data` 为 `null`。图片、HLS、媒体二进制及外部浏览器收银台使用自己的内容类型。

- APP 使用 `Authorization: Bearer <access_token>`，与 GoFramePro 管理员 JWT、网站 Cookie 分离。
- 访问凭据有效 1 小时；刷新凭据有效 30 天，并在每次刷新时轮换。刷新必须携带同一 `device_id`，旧凭据立即失效。
- `device_id` 使用每次安装生成、私有存储保存的 UUID，不使用硬件 IMEI。相同账号和设备再次登录会撤销原会话。
- `/auth/refresh` 不要求未过期的访问凭据。退出、重置密码、设备撤销会阻止后续媒体读取。
- 时间戳使用 Unix 秒；播放进度和总时长使用毫秒；`episode` 是从 0 开始的兼容索引。持久化和换源以稳定 `episode_key`、`line`、`version_key` 为准。
- 未登录可浏览和播放免费影片。收藏、历史、评论、下载和充值必须登录。VIP 与积分限制按每一影片执行，所有媒体请求重新校验。

## 目录与个人媒体库

| 请求 | 数据／用途 |
|---|---|
| `GET /config` | 站点名称、LOGO、`theme/template` 网站模板、会员／评论开关、支付方式、下载默认值 |
| `GET /home` | `banners/recent/hot/sections/categories/channels/catalog_revision`；轮播 `vod_id` 可直接进入原生播放 |
| `GET /channels` | 短剧、奈飞、电影、电视剧、动漫、综艺、韩剧、体育主频道 |
| `GET /categories` | 本地分类扁平树，`id/pid/name`；与频道 ID 是不同的命名空间 |
| `GET /films` | `q/channel_id/type_id/page/size/order/year/area`；返回 `items/page/size/total/pages/catalog_revision` |
| `GET /catalog/revision` | `revision`；命中目录缓存时先校验。屏蔽规则、分类／片源启停、幻灯及后台影片编辑／删除改变版本；普通采集更新与播放计数使用客户端短 TTL，避免持续采集导致预取全部失效 |
| `GET /catalog/featured` | `channel_id/topic_id/page/size/order`；读取小柒真实频道精选和专题，返回 `items/page/pages/size/total/exact_pages/has_next/topics/notice/catalog_revision`。`size` 为 1–100，默认 30；全部频道支持 `time/hits`，其它频道保留上游精选顺序 |
| `POST /catalog/featured/resolve` | `{remote_id,resolve_token}`；点击尚未收录的精选卡片时准备影片，返回稳定本站 `vod_id`，随后使用普通详情接口 |
| `GET /films/{id}` | `film/sources/preferred_line/comments/related/lock/lock_message/favorite` |
| `POST /films/{id}/discover` | 异步补充同影片片源，复用站点冷却、匹配和并发限制 |
| `GET /films/{id}/discovery` | `status/checked/total/added/updated/failed/message/next_check`；完成后含安全的线路列表 |
| `GET /topics`、`GET /topics/{id}` | 专题及已公开的本地影片 |
| `GET /articles`、`GET /articles/{id}` | 本地文章公告 |
| `GET /favorites`、`GET /history` | 最多 100 条个人影片；历史含稳定身份与 `position_ms/duration_ms` |
| `POST /favorites` | `{vod_id,favorite}`，显式设置收藏状态，不使用重复点击切换语义 |
| `POST /history` | `{vod_id,line,episode_key,version_key,position_ms,duration_ms}` |
| `GET /comments?vod_id=...`、`POST /comments` | 发布 `{vod_id,content}`，返回 `pending` 是否待审核 |
| `POST /film-request`、`POST /sign` | 求片 `{title,note}`、每日签到 |

列表为空时返回 `[]`。普通目录继续聚合现有片源，执行站点内容屏蔽、下架和别名去重策略。轮播和小柒／4KVM 推荐通过已采集影片映射到本地稳定 ID；未采集或存在多个无法区分版本的推荐不会输出无法点击的 `id=0` 卡片。果果模板的轮播与推荐读取现有 4KVM 快照；其它主题沿用站点推荐设置。

`/catalog/featured` 专用于可延迟准备影片的精选卡片，保留真实上游顺序和公开内容过滤。已收录卡片包含正常的正整数本站 `id`；尚未收录的卡片包含 `id=0/provider="yqk"/remote_id/resolve_token`。`remote_id` 必须按字符串保留，不能当成本地影片编号；客户端点击时先调用 resolve，再打开返回的本站影片。签名约两小时有效，不能用于其它远端影片，源停用、本站下架或内容屏蔽会拒绝准备。列表请求不会逐条导入，点击准备沿用网页的最多两个并发导入和同影片互斥保护。

全部频道使用真实下一页游标，`exact_pages=false`，`pages` 仅表示当前页及已确认存在的下一页；客户端应只显示当前页数。频道精选和专题返回 `exact_pages=true` 及实际页数。上游未提供精确影片总数时 `total=-1`，不能显示为精确数量；频道精选预览知道完整条数时返回实际总数。不同 App `size` 的缓存游标彼此隔离，并与网页固定 24 条的游标分开；游标链过期时返回 `503`，客户端从第一页重新浏览。

首页首屏只读取本地有界列表及上游的最近成功快照；快照更新在后台执行。首页不执行分类分页使用的全库总数统计，远程推荐通过一批片名／影片ID定位本地记录，避免按每条推荐扫描大体积播放地址。APP 外观偏好默认 `follow`，可持久化选择 `suxinlite/suxinpro/iqiyi/guoguo`，退出账号不重置外观。客户端对栏目、影片和分类去重，并在纯网络连接失败时使用最近10分钟的本地公开目录快照；服务端明确的权限或下架错误不会被缓存覆盖。

影片字段包含 `id/name/pic/type_id/type_name/year/remarks/score/score_label/score_source/vip/points/is_short/is_anime`。详情包含纯文字简介，线路仅公开名称和分集身份，不公开采集参数或解密密钥。

## 账号及验证码

`GET /config` 返回 `registration_requires_email_code` 和 `password_reset_enabled`。客户端进入注册时重取此能力，不根据旧目录缓存自动放宽验证；兼容旧服务缺字段时，默认仍要求邮箱验证码。这些状态由服务端 SMTP 配置完整性决定，不包含邮件账号或授权码。

1. `GET /captcha` 返回 `challenge_id/image_mime/image_base64/expires_at`。图片是 PNG，5 分钟有效，一次尝试后即销毁。
2. `POST /auth/login`：`{email,password,challenge_id,captcha,device_id,device_name}`。返回 `access_token/refresh_token/expires_at/device_id/user`。
3. `POST /auth/send-code`：`{email,purpose,challenge_id,captcha}`，用途为 `register` 或 `reset`。发送成功后重新获取图形验证码。
4. `POST /auth/register`：`{email,name,password,email_code,challenge_id,captcha}`。`registration_requires_email_code=true` 时要求有效的一次性邮件代码；为 `false` 时省略或留空 `email_code`，注册邮箱标记为未验证。两种情况均要求图形验证码，沿用注册开关、重复邮箱检查和注册积分。
5. `POST /auth/reset`：`{email,email_code,password,challenge_id,captcha}`，重置密码并撤销原 APP 会话。
6. `POST /auth/refresh`：`{refresh_token,device_id}`；`POST /auth/logout` 撤销当前会话。
7. `GET /me`、`POST /me {name}`、`GET /sessions`、`POST /sessions/revoke {device_id}` 管理会员资料和登录设备。TV 配对设备使用客户端集成模块的 `/devices`。

SMTP 未完整配置时，邮件发送和邮箱密码重置接口明确返回 `503`，前端不提供邮件找回表单；密码重置始终需要邮件代码。SMTP 已配置但发送失败时，不降级为免邮件验证注册。客户端参数不能覆盖服务器注册策略。账号状态、会员系统开关和登录失败封禁均在服务端执行。

## 播放及下载

`POST /playback/resolve` 的请求：

```json
{"vod_id":781,"line":"ecy_1","episode_key":"episode:1","version_key":"...","episode":0,"quality":"1080","position_ms":42000,"manual":false}
```

- 默认普通分类优先小柒 APP，再选择小柒线路；短剧优先红果；动漫优先二次元。`manual=true` 严格保留所选线路。
- 默认线路缺少该分集时寻找同版本、同分集线路。优先线路失败后才进行最多 3 路并发回退，单次解析总预算 35 秒，避免进页就遍历二十多条线路。
- 描述字段包括 `vod_id/line/base_code/version_key/episode_key/episode/name/url/type/duration_ms/seek_mode/resume_offset_ms/expires_at/qualities/quality/revision/download_key`。
- `qualities` 只包含源实际提供的清晰度。仅一档时标为“源清晰度”；HLS 自身的自适应轨道由 Media3 读取。
- 红果使用服务端解密的按需 VOD HLS，完整时长及分片拖动；二次元、其它 HLS 沿用现有广告过滤和媒体安全校验。
- 仅提供网页解析播放器的线路返回 `422` 并进入回退，不在原生客户端嵌入网页。
- `app_grant` 是绑定设备会话和单影片的短期媒体授权，随所有子清单、分片和密钥链接传递。解密密钥和源请求头保留在服务器。

`POST /downloads/authorize` 使用同一播放身份，返回 `{id,device_id,expires_at,issued_at,revision,descriptor}`。离线授权最长 7 天，VIP 内容不超过会员有效期。`GET /downloads/licenses` 校验当前设备授权、下架和权限撤销；`POST /downloads/renew {id}` 重新解析同一质量与版本，保留授权编号及内容修订号。修订变化返回 `409`，须清理旧缓存后重新下载。

下载缓存使用 `download_key` 加真实媒体资源身份：普通／原生代理要展开 `url` 参数，保留分片路径、稳定查询参数、HLS 初始化段与密钥的区别；红果保留 `segment` 编号。仅忽略本服务的短期 `app_grant/token/exp/sig`，不把整个清单或所有分片合并成同一个缓存键。

完全断网期间，客户端按私有存储中的账号／设备授权和到期时间判定；联网时重新检查授权。此授权机制不宣称具有 Android 外部硬件 DRM 的防篡改能力。

## 会员与支付

- `GET /packages`、`GET /orders` 读取已启用套餐和当前会员订单。
- `POST /unlock {vod_id}` 在事务内锁定余额并幂等扣分，重复请求不会重复购买。
- `POST /payment/create {goods_id,pay_type}` 创建真实渠道订单，返回 `order_no/type/url/checkout_url/qr/qr_image/tip`。可用 `pay_type` 从 `/config.payment_methods` 读取。
- 跳转型渠道通过系统浏览器完成；扫码型渠道可在原生页面显示二维码，或打开仅授权单笔订单的短期收银台。付款回调继续验证现有签名和金额规则。
- APP 返回前台后刷新订单及 `/me`。浏览器收银台令牌与完整会员访问令牌分离，20 分钟到期，关闭会话后失效。

## 升级与检查

启动或首次接口请求幂等创建 `sx_app_session/sx_app_captcha/sx_app_license/sx_app_checkout`，并为观看记录增加线路、分集身份、版本和总时长字段；既有会员、影片及订单保持。

```powershell
python plugins/suxinvideo/tools/test_android_api.py
python plugins/suxinvideo/tools/generate_app_openapi.py
```

接口测试创建可删除的独立 MySQL 实例／数据库，覆盖令牌轮换、防重放、验证码单次消费、会话媒体撤销、会员权限和幂等积分购买，不向当前 CMS 写测试影片或测试会员。
