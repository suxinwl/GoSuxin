package suxinvideo

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/net/ghttp"
	"github.com/suxinwl/GoSuxin/framework/util/gconv"
	"github.com/suxinwl/GoSuxin/internal/extend/middleware"
	hostgf "github.com/suxinwl/GoSuxin/utility/gf"
)

const appReleaseSchema = `CREATE TABLE IF NOT EXISTS sx_app_release (
 id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,platform VARCHAR(12) NOT NULL,
 version_name VARCHAR(40) NOT NULL,version_code BIGINT NOT NULL,min_sdk INT NOT NULL DEFAULT 23,
 filename VARCHAR(200) NOT NULL,package_name VARCHAR(120) NOT NULL,signer_sha256 CHAR(64) NOT NULL,
 size BIGINT NOT NULL,sha256 CHAR(64) NOT NULL,changelog TEXT NOT NULL,status TINYINT NOT NULL DEFAULT 1,
 created BIGINT NOT NULL,UNIQUE KEY variant_version(platform,version_code)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`

type ClientRelease struct {
	ID          int64  `json:"id"`
	Platform    string `json:"platform"`
	VersionName string `json:"version_name"`
	VersionCode int64  `json:"version_code"`
	MinSDK      int    `json:"min_sdk"`
	Size        int64  `json:"size"`
	SHA256      string `json:"sha256"`
	URL         string `json:"url"`
	Changelog   string `json:"changelog"`
	Created     int64  `json:"created"`
	Status      int    `json:"status"`
}

func releaseFromRow(r row) ClientRelease {
	return ClientRelease{gconv.Int64(r["id"]), gconv.String(r["platform"]), gconv.String(r["version_name"]), gconv.Int64(r["version_code"]), gconv.Int(r["min_sdk"]), gconv.Int64(r["size"]), gconv.String(r["sha256"]), "/suxinvideo/app/releases/file/" + gconv.String(r["filename"]), gconv.String(r["changelog"]), gconv.Int64(r["created"]), gconv.Int(r["status"])}
}
func clientReleaseDirectory() string {
	return filepath.Join("resource", "static", "suxinvideo", "apps")
}

// APKs are never published by arbitrary URL. Parse the archive, verify the
// Android signature using the pinned SDK tool, then publish an immutable hash.
func inspectClientAPK(ctx context.Context, file string) (size int64, hash, packageName, signer string, version int64, minSDK int, versionName string, err error) {
	info, err := os.Stat(file)
	if err != nil {
		return 0, "", "", "", 0, 0, "", err
	}
	if info.Size() < 1024 || info.Size() > 512<<20 {
		return 0, "", "", "", 0, 0, "", fmt.Errorf("APK 大小须为 1KB 至 512MB")
	}
	archive, err := zip.OpenReader(file)
	if err != nil {
		return 0, "", "", "", 0, 0, "", fmt.Errorf("文件不是有效的 APK")
	}
	defer archive.Close()
	manifest, dex := false, false
	for _, entry := range archive.File {
		manifest = manifest || entry.Name == "AndroidManifest.xml"
		dex = dex || entry.Name == "classes.dex"
	}
	if !manifest || !dex {
		return 0, "", "", "", 0, 0, "", fmt.Errorf("APK 缺少 Android 清单或程序代码")
	}
	signerTool := findAndroidBuildTool("apksigner")
	aapt := findAndroidBuildTool("aapt")
	if signerTool == "" || aapt == "" {
		return 0, "", "", "", 0, 0, "", fmt.Errorf("服务器未配置 Android SDK，无法校验 APK 签名及版本")
	}
	verifyCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	signature, err := runAndroidTool(verifyCtx, signerTool, "verify", "--print-certs", file)
	if err != nil {
		return 0, "", "", "", 0, 0, "", fmt.Errorf("APK 签名校验失败")
	}
	signMatch := regexp.MustCompile(`(?m)Signer #1 certificate SHA-256 digest:\s*([0-9a-fA-F]{64})`).FindStringSubmatch(string(signature))
	if len(signMatch) != 2 {
		return 0, "", "", "", 0, 0, "", fmt.Errorf("APK 未包含有效签名证书")
	}
	signer = strings.ToLower(signMatch[1])
	badging, err := runAndroidTool(verifyCtx, aapt, "dump", "badging", file)
	if err != nil {
		return 0, "", "", "", 0, 0, "", fmt.Errorf("无法读取 APK 信息")
	}
	pkg := regexp.MustCompile(`package: name='([^']+)' versionCode='([0-9]+)'`).FindStringSubmatch(string(badging))
	sdk := regexp.MustCompile(`(?:sdkVersion|minSdkVersion):'([0-9]+)'`).FindStringSubmatch(string(badging))
	nameMatch := regexp.MustCompile(`versionName='([^']*)'`).FindStringSubmatch(string(badging))
	if len(pkg) != 3 || len(sdk) != 2 || len(nameMatch) != 2 {
		return 0, "", "", "", 0, 0, "", fmt.Errorf("APK 包名或系统版本无效")
	}
	packageName, version, minSDK, versionName = pkg[1], gconv.Int64(pkg[2]), gconv.Int(sdk[1]), nameMatch[1]
	source, err := os.Open(file)
	if err != nil {
		return 0, "", "", "", 0, 0, "", err
	}
	defer source.Close()
	digest := sha256.New()
	if _, err = io.Copy(digest, source); err != nil {
		return 0, "", "", "", 0, 0, "", err
	}
	return info.Size(), hex.EncodeToString(digest.Sum(nil)), packageName, signer, version, minSDK, versionName, nil
}
func findAndroidBuildTool(name string) string {
	if found, err := exec.LookPath(name); err == nil {
		return found
	}
	roots := []string{os.Getenv("ANDROID_HOME"), os.Getenv("ANDROID_SDK_ROOT"), filepath.Join(os.Getenv("LOCALAPPDATA"), "Android", "Sdk"), filepath.Join("D:", "Android", "Sdk"), filepath.Join("D:", "AI", "android-sdk"), filepath.Join("E:", "AndroidDev", "sdk")}
	for _, root := range roots {
		if root == "" {
			continue
		}
		matches, _ := filepath.Glob(filepath.Join(root, "build-tools", "*", name+"*"))
		for i := len(matches) - 1; i >= 0; i-- {
			base := strings.ToLower(filepath.Base(matches[i]))
			if base != strings.ToLower(name) && base != strings.ToLower(name)+".bat" && base != strings.ToLower(name)+".exe" {
				continue
			}
			ext := strings.ToLower(filepath.Ext(matches[i]))
			if ext == ".bat" || ext == ".exe" || ext == "" {
				return matches[i]
			}
		}
	}
	return ""
}
func runAndroidTool(ctx context.Context, tool string, args ...string) ([]byte, error) {
	var command *exec.Cmd
	if strings.EqualFold(filepath.Ext(tool), ".bat") {
		if !strings.EqualFold(filepath.Base(tool), "apksigner.bat") {
			return nil, fmt.Errorf("不支持此 Android 构建工具")
		}
		java := filepath.Join(os.Getenv("JAVA_HOME"), "bin", "java.exe")
		if _, err := os.Stat(java); err != nil {
			java, err = exec.LookPath("java")
			if err != nil {
				return nil, fmt.Errorf("未配置签名校验所需的 JDK")
			}
		}
		jar := filepath.Join(filepath.Dir(tool), "lib", "apksigner.jar")
		command = exec.CommandContext(ctx, java, append([]string{"-jar", jar}, args...)...)
	} else {
		command = exec.CommandContext(ctx, tool, args...)
	}
	castProcessHidden(command)
	return command.CombinedOutput()
}

// PublishClientRelease is shared by the Android release script and admin form.
// Existing data is retained; publishing the same binary/version is idempotent.
func PublishClientRelease(ctx context.Context, platform, versionName string, versionCode int64, minSDK int, sourceFile, changelog string) (ClientRelease, error) {
	if platform != "mobile" && platform != "tv" {
		return ClientRelease{}, fmt.Errorf("客户端类型无效")
	}
	if versionCode < 1 || len(versionName) == 0 || len(versionName) > 40 || minSDK < 23 || minSDK > 100 || len(changelog) > 16000 {
		return ClientRelease{}, fmt.Errorf("版本信息无效")
	}
	size, hash, pkg, signer, actualVersion, actualSDK, actualName, err := inspectClientAPK(ctx, sourceFile)
	if err != nil {
		return ClientRelease{}, err
	}
	if actualVersion != versionCode || actualSDK != minSDK || actualName != versionName {
		return ClientRelease{}, fmt.Errorf("填写的版本号或最低系统版本与 APK 不符")
	}
	expectedPackage := "com.xiaoqi.video"
	if platform == "tv" {
		expectedPackage += ".tv"
	}
	if pkg != expectedPackage {
		return ClientRelease{}, fmt.Errorf("APK 包名应为 %s", expectedPackage)
	}
	previous, err := one(ctx, "SELECT package_name,signer_sha256,version_code FROM sx_app_release WHERE platform=? ORDER BY version_code DESC LIMIT 1", platform)
	if err != nil {
		return ClientRelease{}, err
	}
	if previous != nil && (gconv.String(previous["signer_sha256"]) != signer || gconv.String(previous["package_name"]) != pkg) {
		return ClientRelease{}, fmt.Errorf("签名与此前发布版本不一致，无法覆盖安装")
	}
	exists, err := one(ctx, "SELECT * FROM sx_app_release WHERE platform=? AND version_code=?", platform, versionCode)
	if err != nil {
		return ClientRelease{}, err
	}
	if exists != nil {
		if gconv.String(exists["sha256"]) != hash {
			return ClientRelease{}, fmt.Errorf("此版本号已对应另一份 APK，请提升版本号")
		}
		return releaseFromRow(exists), nil
	}
	dir := clientReleaseDirectory()
	if err = os.MkdirAll(dir, 0755); err != nil {
		return ClientRelease{}, err
	}
	filename := fmt.Sprintf("xiaoqi-%s-%d-%s.apk", platform, versionCode, hash[:16])
	target := filepath.Join(dir, filename)
	input, err := os.Open(sourceFile)
	if err != nil {
		return ClientRelease{}, err
	}
	defer input.Close()
	output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err == nil {
		copiedHash := sha256.New()
		_, err = io.Copy(io.MultiWriter(output, copiedHash), input)
		closeErr := output.Close()
		if err == nil {
			err = closeErr
		}
		if err == nil && hex.EncodeToString(copiedHash.Sum(nil)) != hash {
			err = fmt.Errorf("APK 在校验后发生变化，请重新上传")
		}
		if err != nil {
			_ = os.Remove(target)
		}
	} else if os.IsExist(err) {
		stored, e := os.Open(target)
		if e != nil {
			return ClientRelease{}, e
		}
		storedHash := sha256.New()
		_, e = io.Copy(storedHash, stored)
		_ = stored.Close()
		if e != nil {
			return ClientRelease{}, e
		}
		if hex.EncodeToString(storedHash.Sum(nil)) != hash {
			return ClientRelease{}, fmt.Errorf("发布目录中存在不同内容的同名文件")
		}
		err = nil
	}
	if err != nil {
		return ClientRelease{}, err
	}
	if err = execSQL(ctx, "INSERT INTO sx_app_release(platform,version_name,version_code,min_sdk,filename,package_name,signer_sha256,size,sha256,changelog,status,created) VALUES(?,?,?,?,?,?,?,?,?,?,1,?)", platform, versionName, versionCode, minSDK, filename, pkg, signer, size, hash, changelog, time.Now().Unix()); err != nil {
		return ClientRelease{}, err
	}
	item, err := one(ctx, "SELECT * FROM sx_app_release WHERE platform=? AND version_code=?", platform, versionCode)
	if err != nil {
		return ClientRelease{}, err
	}
	return releaseFromRow(item), nil
}

type AppReleaseLatestReq struct {
	g.Meta      `path:"/app/v1/releases/latest" method:"get" noValApi:"1"`
	Platform    string `p:"platform"`
	VersionCode int64  `p:"version_code"`
}
type AppReleaseLatestRes struct{}

func (*AppClients) ReleaseLatest(ctx context.Context, req *AppReleaseLatestReq) (*AppReleaseLatestRes, error) {
	r := g.RequestFromCtx(ctx)
	if req.Platform != "mobile" && req.Platform != "tv" {
		clientReply(r, nil, appError(400, "客户端类型无效"))
		return &AppReleaseLatestRes{}, nil
	}
	item, err := one(ctx, "SELECT * FROM sx_app_release WHERE platform=? AND status=1 ORDER BY version_code DESC LIMIT 1", req.Platform)
	if err != nil {
		return nil, err
	}
	if item == nil {
		clientReply(r, row{"available": false, "release": nil}, nil)
	} else {
		release := releaseFromRow(item)
		release.URL = clientOrigin(r) + release.URL
		clientReply(r, row{"available": release.VersionCode > req.VersionCode, "release": release}, nil)
	}
	return &AppReleaseLatestRes{}, nil
}

type AppReleaseFileReq struct {
	g.Meta `path:"/app/releases/file/:name" method:"get,head" noValApi:"1"`
	Name   string `p:"name"`
}
type AppReleaseFileRes struct{}

func (*AppClients) ReleaseFile(ctx context.Context, req *AppReleaseFileReq) (*AppReleaseFileRes, error) {
	r := g.RequestFromCtx(ctx)
	if filepath.Base(req.Name) != req.Name || !strings.HasSuffix(req.Name, ".apk") {
		r.Response.WriteHeader(404)
		return &AppReleaseFileRes{}, nil
	}
	item, err := one(ctx, "SELECT filename,sha256 FROM sx_app_release WHERE filename=? AND status=1", req.Name)
	if err != nil {
		return nil, err
	}
	if item == nil {
		r.Response.WriteHeader(404)
		return &AppReleaseFileRes{}, nil
	}
	file, err := os.Open(filepath.Join(clientReleaseDirectory(), req.Name))
	if err != nil {
		r.Response.WriteHeader(404)
		return &AppReleaseFileRes{}, nil
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	r.Response.Header().Set("Content-Type", "application/vnd.android.package-archive")
	r.Response.Header().Set("Content-Disposition", `attachment; filename="`+req.Name+`"`)
	r.Response.Header().Set("ETag", `"`+gconv.String(item["sha256"])+`"`)
	r.Response.Header().Set("Cache-Control", "public, max-age=86400, immutable")
	http.ServeContent(r.Response.Writer, r.Request, req.Name, info.ModTime(), file)
	r.ExitAll()
	return &AppReleaseFileRes{}, nil
}

type AppDownloadPageReq struct {
	g.Meta `path:"/app-download" method:"get" noValApi:"1"`
	Pair   string `p:"pair"`
}
type AppDownloadPageRes struct{}

func (*AppClients) DownloadPage(ctx context.Context, req *AppDownloadPageReq) (*AppDownloadPageRes, error) {
	r := g.RequestFromCtx(ctx)
	data := row{"SiteName": setting(ctx, "site_name", "小柒影视"), "Logo": brandSetting(ctx, "site_logo"), "Favicon": brandSetting(ctx, "site_favicon"), "PairCode": "", "Logged": false, "LANAddress": "192.168.10.10", "LANPort": 8601}
	if appPairCode(req.Pair) {
		data["PairCode"] = req.Pair
	}
	user, _ := currentUser(ctx)
	data["Logged"] = user != nil
	for _, platform := range []string{"mobile", "tv"} {
		item, err := one(ctx, "SELECT * FROM sx_app_release WHERE platform=? AND status=1 ORDER BY version_code DESC LIMIT 1", platform)
		if err != nil {
			return nil, err
		}
		if item != nil {
			release := releaseFromRow(item)
			data[platform] = release
			data[platform+"QR"] = qrImageLink(ctx, clientOrigin(r)+release.URL)
		}
	}
	r.Response.Header().Set("Content-Type", "text/html; charset=utf-8")
	r.Response.Header().Set("Cache-Control", "no-store")
	var body bytes.Buffer
	if err := appDownloadTemplate.Execute(&body, data); err != nil {
		return nil, err
	}
	r.Response.Write(body.Bytes())
	return &AppDownloadPageRes{}, nil
}

var appDownloadTemplate = template.Must(template.New("apps").Funcs(template.FuncMap{"mb": func(size int64) string { return fmt.Sprintf("%.1f MB", float64(size)/(1024*1024)) }}).Parse(`<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><link rel="icon" type="image/x-icon" href="{{.Favicon}}"><link rel="apple-touch-icon" href="{{.Logo}}"><title>APP 下载 · {{.SiteName}}</title><style>*{box-sizing:border-box}body{margin:0;background:#0c1118;color:#f4f6fb;font-family:system-ui,-apple-system,sans-serif}a{color:inherit;text-decoration:none}header{height:72px;border-bottom:1px solid #26313e;display:flex;align-items:center;justify-content:space-between;padding:0 max(24px,calc((100% - 1160px)/2))}.brand{font-size:23px;font-weight:800;display:flex;align-items:center;gap:10px}.brand img{height:32px;width:32px;object-fit:contain}main{max-width:1160px;margin:auto;padding:64px 24px}.hero{text-align:center;max-width:760px;margin:0 auto 48px}.pill{color:#fbc852;border:1px solid #5c4d26;display:inline-block;border-radius:30px;padding:7px 16px;font-size:13px}h1{font-size:46px;line-height:1.2;margin:20px 0}p{line-height:1.8;color:#a6b1c2}.cards{display:grid;grid-template-columns:1fr 1fr;gap:24px}.card{border:1px solid #303a48;background:linear-gradient(140deg,#1b2532,#111820);border-radius:22px;padding:32px}.icon{font-size:42px}.card h2{margin:16px 0 8px}.tags{display:flex;gap:8px;flex-wrap:wrap;margin:18px 0}.tags span{padding:5px 10px;background:#253140;border-radius:6px;font-size:12px}.download{display:inline-block;background:#f8c52b;color:#201600;padding:13px 24px;border-radius:11px;font-weight:700}.qr{width:132px;height:132px;border-radius:10px;margin-top:20px;background:white}.meta{font-size:13px;color:#a6b1c2;overflow-wrap:anywhere;margin:12px 0}.changelog{white-space:pre-line}.guide{margin-top:32px;border:1px solid #303a48;border-radius:22px;padding:32px}.guide h2{margin-top:0}.steps{display:grid;grid-template-columns:repeat(3,1fr);gap:20px}.steps b{color:#f8c52b}.credentials{background:#0c1118;padding:18px;border-radius:12px;line-height:1.9}.notice{padding:16px;border:1px solid #806924;background:#372b10;border-radius:10px}.empty{padding:16px 0;color:#b4becd}footer{text-align:center;padding:30px;color:#8993a1}@media(max-width:700px){header{padding:0 18px}main{padding:36px 18px}h1{font-size:32px}.cards,.steps{grid-template-columns:1fr}.card,.guide{padding:24px}}</style></head><body><header><a class="brand" href="/suxinvideo/">{{if .Logo}}<img src="{{.Logo}}" alt="">{{end}}{{.SiteName}}</a><a href="/suxinvideo/">返回网站 →</a></header><main><section class="hero"><span class="pill">原生 Android 客户端</span><h1>把喜欢的影片<br>带到每一块屏幕</h1><p>手机、平板和电视共享收藏与观看进度。原生播放器、清晰度切换与续播，让观影更顺手。</p></section>{{if .PairCode}}<div class="notice">电视配对码：<strong>{{.PairCode}}</strong>。 <a class="download" href="xiaoqi://pair?code={{.PairCode}}">打开手机 APP 确认配对</a><br>请打开手机 APP 的“投屏／设备配对”，登录后输入此码确认；五分钟内有效。</div>{{end}}<div class="cards"><section class="card"><div class="icon">▣</div><h2>手机／平板版</h2><p>根据屏幕自适应布局，支持在线播放、离线下载、画中画和投屏。</p><div class="tags"><span>Android 6+</span><span>手机 · 平板</span><span>会员账号同步</span></div>{{with .mobile}}<a class="download" href="{{.URL}}">下载 Android APK</a><div class="meta">版本 {{.VersionName}} · {{mb .Size}} · 最低 API {{.MinSDK}}</div><div class="meta">SHA256：{{.SHA256}}</div><img class="qr" src="{{$.mobileQR}}" alt="手机版下载二维码"><p class="changelog">{{.Changelog}}</p>{{else}}<div class="empty">安装包尚未发布，发布后将在此提供下载与二维码。</div>{{end}}</section><section class="card"><div class="icon">▤</div><h2>Android TV 版</h2><p>海报墙与遥控器操作，支持扫码登录、手机控制和跨设备续播。</p><div class="tags"><span>Android 6+</span><span>电视 · 机顶盒</span><span>方向键操作</span></div>{{with .tv}}<a class="download" href="{{.URL}}">下载 TV APK</a><div class="meta">版本 {{.VersionName}} · {{mb .Size}} · 最低 API {{.MinSDK}}</div><div class="meta">SHA256：{{.SHA256}}</div><img class="qr" src="{{$.tvQR}}" alt="TV版下载二维码"><p class="changelog">{{.Changelog}}</p>{{else}}<div class="empty">安装包尚未发布，发布后将在此提供下载与二维码。</div>{{end}}</section></div><section class="guide"><h2>安装与使用</h2><div class="steps"><div><b>01 · 下载</b><p>手机直接下载安装包；TV 可通过 U 盘或局域网文件传输安装对应 TV APK。</p></div><div><b>02 · 安装</b><p>在系统提示时允许当前浏览器或文件管理器“安装未知应用”，安装完成后可关闭此权限。</p></div><div><b>03 · 登录</b><p>使用网站会员账号。TV 页面生成配对码，由已登录的手机版 APP 确认。</p></div></div></section><section class="guide"><h2>网易爆米花接入</h2><p>使用官方客户端添加 Jellyfin 媒体库，访问本站影片、收藏和观看进度。会员与积分影片权限沿用网站。</p><a class="download" href="https://bmh.163.com/" target="_blank" rel="noopener">下载官方网易爆米花</a>{{if .Logged}}<div class="credentials" style="margin-top:20px">媒体库类型：<b>Jellyfin</b><br>协议：<b>HTTP（局域网）</b><br>服务器：<b>{{.LANAddress}}</b><br>端口：<b>{{.LANPort}}</b><br>用户名：网站会员邮箱<br>密码：网站会员密码</div><p>设备需处于服务器所在局域网。积分影片先在网站或自研 APP 解锁；公网接入将在可信 HTTPS 证书就绪后启用。</p>{{else}}<p><a class="download" href="/suxinvideo/login">登录后查看接入地址</a></p>{{end}}</section></main><footer>{{.SiteName}} · Android 原生客户端</footer></body></html>`))

type ClientReleasesReq struct {
	g.Meta `path:"/clients/releases" method:"get"`
}
type ClientReleasesRes struct{}

func (*ClientAdminRoutes) Releases(ctx context.Context, _ *ClientReleasesReq) (*ClientReleasesRes, error) {
	r := g.RequestFromCtx(ctx)
	allowed, err := requireResource(ctx, r, "", "clients/releases")
	if err != nil {
		return nil, err
	}
	if !allowed {
		return &ClientReleasesRes{}, nil
	}
	items, err := all(ctx, "SELECT * FROM sx_app_release ORDER BY created DESC,id DESC LIMIT 100")
	if err != nil {
		return nil, err
	}
	releases := make([]ClientRelease, 0, len(items))
	for _, item := range items {
		releases = append(releases, releaseFromRow(item))
	}
	r.Response.WriteJson(hostgf.Success().SetData(row{"list": releases}))
	return &ClientReleasesRes{}, nil
}

type ClientPublishReq struct {
	g.Meta      `path:"/clients/publish" method:"post"`
	Platform    string `p:"platform"`
	VersionName string `p:"version_name"`
	VersionCode int64  `p:"version_code"`
	MinSDK      int    `p:"min_sdk"`
	Changelog   string `p:"changelog"`
}
type ClientPublishRes struct{}

func (*ClientAdminRoutes) Publish(ctx context.Context, req *ClientPublishReq) (*ClientPublishRes, error) {
	r := g.RequestFromCtx(ctx)
	allowed, err := requireResource(ctx, r, "", "clients/publish")
	if err != nil {
		return nil, err
	}
	if !allowed {
		return &ClientPublishRes{}, nil
	}
	file, _, err := r.Request.FormFile("file")
	if err != nil {
		badRequest(r, "请选择 APK 文件")
		return &ClientPublishRes{}, nil
	}
	defer file.Close()
	if err = os.MkdirAll(clientReleaseDirectory(), 0755); err != nil {
		return nil, err
	}
	temporary, err := os.CreateTemp(clientReleaseDirectory(), "upload-*.apk")
	if err != nil {
		return nil, err
	}
	name := temporary.Name()
	defer os.Remove(name)
	size, err := io.Copy(temporary, io.LimitReader(file, (512<<20)+1))
	_ = temporary.Close()
	if err != nil {
		return nil, err
	}
	if size > 512<<20 {
		badRequest(r, "APK 超过512MB")
		return &ClientPublishRes{}, nil
	}
	release, err := PublishClientRelease(ctx, req.Platform, req.VersionName, req.VersionCode, req.MinSDK, name, req.Changelog)
	if err != nil {
		badRequest(r, err.Error())
		return &ClientPublishRes{}, nil
	}
	r.Response.WriteJson(hostgf.Success().SetData(release))
	return &ClientPublishRes{}, nil
}

type ClientReleaseStatusReq struct {
	g.Meta `path:"/clients/status" method:"post"`
	ID     int64 `json:"id"`
	Status int   `json:"status"`
}
type ClientReleaseStatusRes struct{}

func (*ClientAdminRoutes) Status(ctx context.Context, req *ClientReleaseStatusReq) (*ClientReleaseStatusRes, error) {
	r := g.RequestFromCtx(ctx)
	allowed, err := requireResource(ctx, r, "", "clients/status")
	if err != nil {
		return nil, err
	}
	if !allowed {
		return &ClientReleaseStatusRes{}, nil
	}
	if req.ID < 1 || (req.Status != 0 && req.Status != 1) {
		badRequest(r, "状态无效")
		return &ClientReleaseStatusRes{}, nil
	}
	if err = execSQL(ctx, "UPDATE sx_app_release SET status=? WHERE id=?", req.Status, req.ID); err != nil {
		return nil, err
	}
	r.Response.WriteJson(hostgf.Success().SetData(true))
	return &ClientReleaseStatusRes{}, nil
}

func RegisterClientRoutes(group *ghttp.RouterGroup) {
	group.Group("/suxinvideo", func(public *ghttp.RouterGroup) { public.Bind(new(AppClients)) })
	group.Group("/admin/suxinvideo", func(admin *ghttp.RouterGroup) {
		admin.Middleware(middleware.Token, middleware.Auth, cmsAudit)
		admin.Bind(new(ClientAdminRoutes))
	})
}

// A small wrapper binds only new methods, avoiding duplicate routes for Admin.
type ClientAdminRoutes struct{}
