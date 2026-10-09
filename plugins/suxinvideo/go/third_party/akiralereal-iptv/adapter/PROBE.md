# 真实网络探测记录

固定源码：akiralereal/iptv v4.27.0，commit `7b1e2d5fdc6dbe35de63f099be6264a201c6a975`。本机 Windows、Node 24.14.0，公开实例，无平台账号。以下是取目录、解析、清单改写和媒体字节探测，不替代浏览器连续播放验证。

2026-10-03 15:27:27 UTC：电视目录 487 条。手动临时开启赛事后，新增 320 条（234 event_replay、86 event_live），合计 807；XMLTV 366 个频道、16256 条节目。这些数字是本次目录结果，不能解释为全部可播。

| 抽样模块 / 节目 | 实际结果 |
|---|---|
| 央视频 CCTV1，ysp-cctv1 | resolve 200，HLS 子媒体 200 MPEG-TS，65536 字节 |
| 咪咕 CCTV1，608807420 | resolve 200，HLS 子媒体 200 MPEG-TS，65842 字节 |
| 黑龙江卫视 | resolve 200，媒体 200 MPEG-TS，81647 字节 |
| 香港卫视，hkstv-live | resolve 200，媒体 200 MPEG-TS，75557 字节 |
| 澳门莲花，lotustv-live | resolve 200，媒体 200 MPEG-TS，65700 字节 |
| 凤凰中文，fengshows-chinese.flv | resolve 200，连续 FLV 200，69290 字节，开头 FLV 标识 |
| 咪咕赛事 event-120000594699-replay-968088136 | resolve 200，mode=event_replay，duration_seconds=15838；真实 HLS ENDLIST，总 EXTINF 15838.24 秒，媒体 200 MPEG-TS，66752 字节 |

全部抽样 HLS 的 URI 属性和非注释媒体行均只指向受鉴权的固定 loopback opaque 路由，未输出官方签名 URL、Cookie 或服务间密钥。

较早的 15:12 UTC 抽样中央视频清单成功、第一分片 502，其余五条电视媒体成功。后续适配器按上游 hlsProxy 的固定/函数 User-Agent 契约发头，最新央视分片成功；CDN、短效票据、出口限制仍可能变化，不据单次成功声称所有央视频道稳定。

适配器测试 15 项通过，覆盖身份稳定、鉴权、无凭据回传、公网 DNS 固定、私网拒绝、跨 origin 凭据隔离、HLS key/init/子清单改写、真实 MIME、独立赛事回放、失败不恢复旧赛事、配置刷新策略和媒体撤销。模拟浏览器实际启动器峰值 2，共享 source permit 峰值 4。真实独立父 PID 被终止后，适配器约 5 秒退出并移除 runtime PID 锁。

15:42 UTC 收尾：新增不可变长回放分片租约测试，Node 共 16 项通过。有限回放分片/key 租约按时长加 20 分钟保留（上限 24 小时加 20 分钟），直播仍 20 分钟滑动；这个期限不代表官方签名寿命。

Go 受控媒体 gate 在独立临时 cwd、空闲的 127.0.0.1:9181 及临时内网凭据下通过（20.80 秒），未启动 CMS/数据库：FFmpeg 生成并持续输出 H.264/AAC FLV，实际桥转为推进序号的最多 6 片滑动 HLS、无 ENDLIST，TS 真实解码；相同源共享桥、第三个桥拒绝、空闲回收。生成真实 AES-128 HLS 并解码，key 请求均鉴权；0.8 秒 MP4 经真实宿主响应 middleware 的 full GET、Range 206、HEAD 206、无效 Range 416 精确保留字节与头。另 8000 分片有限清单重复重写、前后资产定位及 50000 资产上限测试通过。此 gate 不声称完成 CMS 登录/ACL、电视设备或会员实际节目播放验证。

`node adapter/probe.mjs` 可在宿主管理的实例上重复抽样；仅从环境读取内网密钥，输出不含媒体 URL 和密钥。赛事模块在探测后恢复关闭。未提供平台会员凭据，因此会员源、扫码完成、VIP 桥媒体未作实际账号验证。

原始协议来源：[赛事 updatePE](https://github.com/akiralereal/iptv/blob/7b1e2d5fdc6dbe35de63f099be6264a201c6a975/utils/updateData.js)、[migu.resolve](https://github.com/akiralereal/iptv/blob/7b1e2d5fdc6dbe35de63f099be6264a201c6a975/extractors/migu/resolve.js)、[HLS 请求头契约](https://github.com/akiralereal/iptv/blob/7b1e2d5fdc6dbe35de63f099be6264a201c6a975/utils/hlsProxy.js)。

## 凤凰连续流诊断及修复

15:52–15:55 UTC 的连续传输交叉对照发现：适配器错误复用了 HLS 浏览器 UA。生产 Node 公开凤凰中文请求 200，但 2.91 秒正常 EOF，只收到 104192 字节、24 视频 tag（0.92 秒）与 45 音频 tag（0.94 秒）；不是 `ffmpeg -t` 初始时间戳造成的短输出，也没有 Content-Encoding/Content-Length 长度差。

同一官方匿名票据：原生 fetch 加 identity 持续 20.01 秒、1879586 字节；checkedFetch 默认浏览器 UA 2.10 秒 EOF；仅启用 keep-alive 仍 3.29 秒 EOF。再次交叉对照，原生 fetch 仅换浏览器 UA 即 2.82 秒 EOF；checkedFetch 用原生 node UA 与 identity 连续 21.36 秒、1732274 字节，无正常 EOF；加 keep-alive 也连续 21.67 秒、1504876 字节。上述持续读取由探测计时器主动停止。

修复后 FLV 使用上游 [flvProxy](https://github.com/akiralereal/iptv/blob/7b1e2d5fdc6dbe35de63f099be6264a201c6a975/utils/flvProxy.js) 的原生 UA/identity 规则，HLS 规则保留。新增连续 FLV 请求超过响应头 deadline 的受控回归，Node 共 17 项通过。`continuity-probe.mjs` 在线解析有限时长 FLV tag/DTS/PTS/字节活跃度，`transport-probe.mjs` 比较匿名原始 transport；均不输出签名 URL 或内网密钥。后续生产重启和真实 Go 浏览器推进结果单独记录。

15:56–15:58 UTC：宿主管理的 Node 已单独重启为 PID 25420，Go PID 25212 保持运行。公开实例 9180 的三条凤凰源顺序、单并发各连续读取 30 秒。全部 HTTP 200、`video/x-flv`、无 Content-Length/Content-Encoding；均由探测计时器主动停止，`normal_eof=false`，后续每秒持续收到媒体字节。

| 公开 FLV ref | 读取耗时 | 接收字节 | 视频 tag / 关键帧 | 视频 DTS 推进 | 音频 DTS 推进 |
|---|---:|---:|---:|---:|---:|
| fengshows-chinese.flv | 30.01 s | 2491610 | 738 / 15 | 29.480 s | 29.461 s |
| fengshows-hongkong.flv | 30.02 s | 2334730 | 712 / 15 | 28.440 s | 28.437 s |
| fengshows-info.flv | 30.01 s | 2848630 | 797 / 16 | 31.840 s | 31.808 s |

上述结果验证新版 Node 的真实连续 FLV 传输修复，不等同于全部源长期稳定，也不替代 Go 桥与浏览器播放验收。资讯时间戳推进略快于墙钟包含起始积压媒体。
