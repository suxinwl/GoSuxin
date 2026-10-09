# 影片来源评分

`sx_vod_source_score` 保存影片在各采集渠道的原始评分、来源 ID、远端影片 ID 和更新时间。影片展示优先使用已启用小柒的有效评分，其次红果，再使用其它采集渠道的有效评分。只接受明确的 0～10 分制字段；热度、播放次数不转换成评分。上游没有评分、评分为零或格式错误时显示“暂无评分”。历史数值没有来源记录时标注“已有评分”，不会冒充来源评分。

采集新增、更新和前台后台补源都会保存已验证影片详情中已有的评分字段。补源不为评分增加网络请求，同源存在多个匹配版本时不保存有歧义的评分；年份、地区、远端 ID 或本地身份变化时保留原数据。

## 刷新指定影片

默认只读，先取得已绑定原生片源的真实评分并生成报告：

```powershell
go run ./plugins/suxinvideo/tools/refresh_source_scores --ids 2164,11850 --limit 2 --output data/tmp/source-score-review
```

确认报告后使用新的报告目录保存评分：

```powershell
go run ./plugins/suxinvideo/tools/refresh_source_scores --apply --ids 2164,11850 --limit 2 --output data/tmp/source-score-apply
```

写入前保存该批影片、关联别名成员及各来源评分的 `score-before.private.json`。备份拒绝覆盖，重试请使用新批次目录；CLI 失败原因只保存在本地 `error.private.json`。评分刷新只修改评分，不修改标题、海报、会员权益、播放线路、用户收藏或历史。

## 分批回填已有小柒与红果的零分影片

仅处理数据库中已有原生绑定的零分记录，不按同名搜索影片、不扫描其它渠道的全部零分记录、不下载媒体文件。默认 3 个工作任务，同一原生渠道的请求启动间隔至少 400 毫秒：

```powershell
python plugins/suxinvideo/tools/backfill_native_scores.py --output data/tmp/native-score-review --batch-size 50 --concurrency 3 --interval-ms 400 --max-batches 1
```

省略 `--max-batches` 完成剩余批次；加 `--apply` 才会写入数据库，并须使用独立的写入目录：

```powershell
python plugins/suxinvideo/tools/backfill_native_scores.py --apply --output data/tmp/native-score-apply --batch-size 50 --concurrency 3 --interval-ms 400
```

进度保存在 `checkpoint.safe.json`。中断后使用相同命令和目录恢复；失败影片 ID 被保留，追加 `--retry-failed` 可单独重试一次。没有上游评分的影片正常保留“暂无评分”，不生成替代数值。只读和写入任务不得共享目录。

评分的来源优先、降级、别名共享、权益保护、备份以及过期身份校验可用隔离数据库验证：

```powershell
python plugins/suxinvideo/tools/test_hongguo_audit.py
```
