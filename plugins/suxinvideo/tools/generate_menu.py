"""Generate the standalone CMS menu and exact host API permissions."""
import json
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
FIELDS = {
    "des": "", "locale": "", "weigh": 0, "icon": "", "redirect": "", "path": "",
    "permission": "", "status": 0, "isext": 0, "keepalive": 0, "requiresauth": 1,
    "hideinmenu": 0, "hidechildreninmenu": 0, "activemenu": 0, "noaffix": 0,
    "onlypage": 0,
}
ENDPOINTS = [
    ("仪表盘数据", "dashboard"), ("查看列表", "list"), ("保存内容", "save"),
    ("删除内容", "delete"), ("批量删除", "deleteBatch"),
    ("读取设置", "config"), ("保存设置", "saveConfig"), ("读取统计", "stats"),
    ("清理缓存", "clearCache"), ("采集资源", "collect"),
    ("执行自动采集", "collect/auto"), ("启动采集任务", "collect/job/start"),
    ("查看采集进度", "collect/job/status"), ("停止采集任务", "collect/job/cancel"),
    ("读取采集分类", "collect/classes"),
    ("预览采集资源", "collect/preview"), ("扫描重复影片", "collect/duplicates"),
    ("合并重复影片", "collect/merge"), ("保存会员", "user/save"), ("删除会员", "user/delete"),
    ("解除会员锁定", "user/unlock"), ("读取图片", "images"),
    ("删除图片", "images/delete"), ("清理未引用图片", "images/clean"),
    ("读取安全日志", "logs"), ("清空安全日志", "logs/clear"),
    ("测试邮件", "mailTest"), ("修改密码", "adminPassword"),
    ("安装模板", "installTheme"), ("卸载模板", "uninstallTheme"),
    ("批量清理订单与评论", "cleanup"), ("读取当前权限", "capabilities"),
    ("查看客户端发布", "clients/releases"), ("发布签名客户端", "clients/publish"),
    ("上下架客户端", "clients/status"),
    ("直播分组", "live/groups"), ("直播频道", "live/channels"),
    ("直播线路", "live/streams"), ("直播订阅", "live/subscriptions"),
    ("保存直播配置", "live/save"), ("停用直播配置", "live/delete"),
    ("导入直播列表", "live/import"), ("刷新直播订阅", "live/refresh"),
    ("直播导入进度", "live/job"), ("检测直播线路", "live/probe"),
    ("直播引擎状态", "live/provider/status"), ("配置直播模块", "live/provider/save"),
    ("同步直播目录", "live/provider/sync"), ("直播模块登录", "live/provider/login"),
    ("直播频道绑定预览", "live/provider/bindings"), ("绑定直播来源频道", "live/provider/bindings/save"),
    ("直播分发配置", "live/profiles"), ("保存直播分发配置", "live/profiles/save"),
    ("停用直播分发配置", "live/profiles/delete"), ("直播会员白名单", "live/members"),
    ("保存直播会员授权", "live/members/save"), ("撤销直播会员授权", "live/members/delete"),
    ("直播订阅令牌", "live/distribution"), ("创建或重置直播订阅", "live/distribution/save"),
    ("撤销直播订阅", "live/distribution/revoke"), ("查看直播节目单", "live/epg"),
]
RESOURCES = ("vod", "type", "slide", "article", "link", "film_request", "player",
             "user", "goods", "order", "comment", "collect_api", "plugin")


def record(title: str, routepath: str, routename: str, component: str, kind: int, path: str = "") -> dict:
    return {"title": title, **FIELDS, "type": kind, "routepath": routepath,
            "routename": routename, "component": component, "path": path}


root = record("影视CMS", "/suxinvideo/admin", "suxinvideo_admin", "/suxinvideo/index", 1)
root["icon"] = "icon-video-camera"
root["weigh"] = 2
root["onlypage"] = 1
root["children"] = [
    record(title, route.replace("/", "_"), "suxinvideo_admin_" + route.replace("/", "_"),
           "", 2, "/admin/suxinvideo/" + route)
    for title, route in ENDPOINTS
]
for entry in root["children"]:
    if entry["path"].startswith("/admin/suxinvideo/clients/"):
        entry["hideinmenu"] = 1
for table in RESOURCES:
    actions = ["list", "save", "delete"]
    if table in ("vod", "user"):
        actions.append("deleteBatch")
    if table in ("order", "comment"):
        actions.append("cleanup")
    for action in actions:
        endpoint = "user/" + action if table == "user" and action in ("save", "delete") else action
        entry = record(f"{table} {action}", f"{table}_{action}",
                       f"suxinvideo_admin_{table}_{action}", "", 2,
                       "/admin/suxinvideo/" + endpoint)
        entry["permission"] = f"suxinvideo:{table}:{action}"
        root["children"].append(entry)
(ROOT / "adminmenu.json").write_text(json.dumps([root], ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
