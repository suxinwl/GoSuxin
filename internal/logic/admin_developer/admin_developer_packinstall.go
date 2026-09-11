// ================================
// 代码插件打包、安装、上传
// ================================
package admindeveloper

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/suxinwl/GoSuxin/api/admin/developer"
	"github.com/suxinwl/GoSuxin/internal/dao"
	"github.com/suxinwl/GoSuxin/internal/extend/clogic/clogic_developer"
	"github.com/suxinwl/GoSuxin/utility/gf"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/suxinwl/GoSuxin/framework/encoding/gcompress"
	"github.com/suxinwl/GoSuxin/framework/encoding/gjson"
	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/os/gfile"
	"github.com/suxinwl/GoSuxin/framework/util/gconv"
)

// 打包插件
func (c *sAdmindeveloper) PackCode(ctx context.Context, req *developer.PackCodeReq) (res *gf.R) {
	runEnv, _ := g.Cfg("app").Get(ctx, "app.runEnv")
	if runEnv.String() == "release" {
		res = gf.Failed().SetMsg("生产环境禁止操作，请在开发环境下操作")
		return
	}
	if g.IsEmpty(req.Name) {
		res = gf.Failed().SetMsg("请填写插件包名")
		return
	}
	path, err := os.Getwd()
	if err != nil {
		res = gf.Failed().SetMsg("项目路径获取失败")
		return
	}
	pack_path := filepath.Join(path, "/devsource/codemarket/release/", req.Name)
	//1制作打包文件
	if _, err := os.Stat(pack_path); err != nil && !os.IsExist(err) {
		os.MkdirAll(pack_path, os.ModePerm)
	} else {
		os.RemoveAll(pack_path)
		os.MkdirAll(pack_path, os.ModePerm)
	}
	//2复制包模板
	gfile.Copy(filepath.Join(path, "/devsource/developer/codetpl/packcode"), pack_path)
	//3.导出数据库表
	var tables = make([]string, 0)
	if !g.IsEmpty(req.Packtables) {
		pathname := filepath.Join(path, "/devsource/codemarket/release", req.Name, "install.sql")
		tables = strings.Split(req.Packtables, ",")
		clogic_developer.ExecSqlFile(tables, pathname)
	}
	//4.更新基础配置
	//处理数据表前缀
	var packtablesStr = ""
	if len(tables) > 0 {
		prefix, _ := g.Cfg().Get(ctx, "database.default.prefix")
		var tables_noprefix = make([]string, 0)
		for _, val := range tables {
			tables_noprefix = append(tables_noprefix, strings.Replace(val, prefix.String(), "", 1))
		}
		packtablesStr = strings.Join(tables_noprefix, ",")
	}
	upconf := map[string]interface{}{"version": req.Version, "title": req.Title, "installcover": req.Installcover, "isModTidy": req.IsModTidy, "commandLines": fmt.Sprintf("\"%v\"", req.CommandLines), "des": req.Des, "name": req.Name,
		"packtables": packtablesStr, "goFiles": fmt.Sprintf("'%v'", gf.String(req.GoFiles)), "vueFiles": fmt.Sprintf("'%v'", gf.String(req.VueFiles))}
	clogic_developer.UpConfFieldData(path+"/devsource/codemarket/release/"+req.Name, upconf)
	//5.把后台菜单数据写入adminmenu.json文件
	if len(req.Menujson) > 0 {
		meni_json := filepath.Join(path, "/devsource/codemarket/release/", req.Name, "adminmenu.json")
		if _, err := os.Stat(meni_json); err != nil {
			if !os.IsExist(err) {
				os.MkdirAll(meni_json, os.ModePerm)
			}
		}
		menudata, _ := gjson.Marshal(req.Menujson)
		os.WriteFile(meni_json, menudata, 0777)
	}
	//6. 查看/manifest/config/code是否存在动态配置文件-存在则复制到包目录下
	confFilePath := filepath.Join(path, "/manifest/config/code", req.Name+".yaml")
	if _, err := os.Stat(confFilePath); !os.IsNotExist(err) { //存在
		gfile.CopyFile(confFilePath, filepath.Join(pack_path, req.Name+".yaml"))
	}
	//7. 查看/resource/static是否存在静态文件-存在则复制到包目录下
	staticFilePath := filepath.Join(path, "/resource/static", req.Name)
	if _, err := os.Stat(staticFilePath); !os.IsNotExist(err) { //存在
		gfile.Copy(staticFilePath, filepath.Join(pack_path, req.Name))
	}
	//8.把后端(Go)代码复制到插件包中
	if !g.IsEmpty(req.GoFiles) {
		for _, item := range req.GoFiles {
			gfile.Copy(filepath.Join(path, gf.String(item["path"])), filepath.Join(path, "/devsource/codemarket/release/", req.Name, "/go", gf.String(item["path"])))
		}
	}
	//8.把前端(vue)代码复制到插件包中
	if !g.IsEmpty(req.VueFiles) {
		vueobjroot, _ := g.Cfg("app").Get(ctx, "app.vueobjroot")
		for _, item := range req.VueFiles {
			gfile.Copy(filepath.Join(vueobjroot.String(), gf.String(item["path"])), filepath.Join(path, "/devsource/codemarket/release/", req.Name, "/vue", gf.String(item["path"])))
		}
	}
	//打包文件路径
	if _, err := os.Stat(pack_path); err == nil {
		defer os.RemoveAll(pack_path) //最后删除文件夹
		dest := filepath.Join(path, "/devsource/codemarket/release", req.Name+".zip")
		err = gcompress.ZipPath(pack_path, dest)
		if err != nil {
			res = gf.Failed().SetMsg("打包压缩成zip错误").SetData(err)
			return
		}
		res = gf.Success().SetMsg("打包成功").SetData(true)
	} else {
		res = gf.Failed().SetMsg("文件不存在")
	}
	return
}

// 上传文件到代码仓
func (c *sAdmindeveloper) Upfile(ctx context.Context, req *developer.UpfileReq) (res *gf.R) {
	if suxinOnlineServicesPending {
		return gf.Failed().SetMsg("在线服务筹备中，将迁移至 Suxin 官网：https://www.suxinwl.com")
	}
	params := map[string]string{"code_token": req.CodeToken}
	result, err := clogic_developer.UpFileClient(req, params)
	if err != nil {
		res = gf.Failed().SetMsg(err.Error())
		return
	}
	var parameter map[string]interface{}
	if err := json.Unmarshal([]byte(string(result)), &parameter); err == nil {
		if parameter["status"] == "done" {
			res = gf.Success().SetMsg("附件上传成功").SetData(parameter)
		} else {
			res = gf.Failed().SetMsg(gf.String(parameter["message"])).SetData(parameter)
		}
	}
	return
}

// 下载插件代码到本地-安装使用
func (c *sAdmindeveloper) DownCode(ctx context.Context, req *developer.DownCodeReq) (res *gf.R) {
	if suxinOnlineServicesPending {
		return gf.Failed().SetMsg("在线服务筹备中，将迁移至 Suxin 官网：https://www.suxinwl.com")
	}
	runEnv, _ := g.Cfg("app").Get(ctx, "app.runEnv")
	if runEnv.String() == "release" {
		res = gf.Failed().SetMsg("生产环境禁止操作，请在开发环境下操作")
		return
	}
	path, _ := os.Getwd()
	install_apppath := filepath.Join(path, "/devsource/codemarket/install", req.Name)
	if _, err := os.Stat(install_apppath); err == nil {
		res = gf.Success().SetMsg("本地代码已存在直接安装").SetData(true)
		return
	}
	//1.请求社区下载插件下载地址
	result, err := g.Client().Get(ctx, req.Baseurl+"/goflycode/content/getDownUrl", gconv.MapDeep(req, "p"))
	if err != nil {
		res = gf.Failed().SetMsg("请求Suxin社区获取代码商店失败").SetData(err)
		return
	}
	defer result.Close()
	var parameter gf.Map
	if err := json.Unmarshal([]byte(result.ReadAllString()), &parameter); err != nil {
		res = gf.Failed().SetMsg("请求解析数据失败").SetData(err)
		return
	}
	if gconv.Int(parameter["code"]) != 0 || g.IsEmpty(parameter["data"]) {
		res = gf.Failed().SetMsg(gf.String(parameter["message"])).SetData(parameter)
		return
	}
	//2.下载插件代码zip
	downdir := filepath.Join(path, "/devsource/codemarket/install", req.Name+".zip")
	downstatus, downdir_zippath := clogic_developer.DownFileToDir(gf.String(parameter["data"]), downdir)
	if downstatus {
		dezipdir := filepath.Join(path, "/devsource/codemarket/install", req.Name)
		err := gcompress.UnZipFile(downdir_zippath, dezipdir, req.Name)
		if err == nil {
			os.Remove(downdir_zippath)
			res = gf.Success().SetMsg("下载代码成功").SetData(true)
		} else {
			res = gf.Failed().SetMsg("下载代码失败").SetData(err)
		}
	} else {
		res = gf.Failed().SetMsg("下载代码失败")
	}
	return
}

// 安装插件
func (c *sAdmindeveloper) InstallCode(ctx context.Context, req *developer.InstallCodeReq) (res *gf.R) {
	runEnv, _ := g.Cfg("app").Get(ctx, "app.runEnv")
	if runEnv.String() == "release" {
		res = gf.Failed().SetMsg("生产环境禁止操作，请在开发环境下操作")
		return
	}
	path, err := os.Getwd()
	if err != nil {
		res = gf.Failed().SetMsg("项目路径获取失败")
		return
	}
	//获取配置文件
	apppath := filepath.Join(path, "/devsource/codemarket/install", req.Name)
	installCofig, err := clogic_developer.GetInstallConfig(apppath)
	if err != nil {
		res = gf.Failed().SetMsg("插件配置文件解析失败").SetData(err)
		return
	}
	//1.导入后台菜单
	adminmenuPath := filepath.Join(path, "/devsource/codemarket/install", req.Name, "/adminmenu.json")
	amenufile, _ := os.Open(adminmenuPath)
	amenubytes, amenuerr := io.ReadAll(amenufile)
	anenuids := ""
	if amenuerr == nil && amenubytes != nil {
		var menudata interface{}
		json.Unmarshal([]byte(amenubytes), &menudata)
		m_nenuids := clogic_developer.Insertmenu(gf.Int64(ctx.Value("uid")), menudata, 0, "auth_rule")
		if len(m_nenuids) > 0 {
			var parent_ids = make([]string, 0)
			for _, inMenuid := range m_nenuids {
				parent_id, _ := dao.AuthRule.Ctx(ctx).Where("id", inMenuid).Value("pid")
				if parent_id != nil {
					cMenu_id, _ := dao.AuthRule.Ctx(ctx).Where("pid", parent_id).WhereNotIn("id", m_nenuids).Value("pid")
					if g.IsEmpty(cMenu_id) {
						parent_ids = append(parent_ids, parent_id.String())
					}
				}
			}
			m_nenuids = append(m_nenuids, gf.RemoveDuplicates(parent_ids)...)
		}
		anenuids = strings.Join(m_nenuids, ",")
	}
	//关闭menu.json读取
	amenufile.Close()

	//2.安装后端go
	var modelList []string
	if !g.IsEmpty(installCofig.App.GoFiles) {
		var goFileList []map[string]any
		if err := json.Unmarshal([]byte(installCofig.App.GoFiles), &goFileList); err != nil {
			res = gf.Failed().SetMsg("插件配置文件解析数据失败").SetData(err)
			return
		}

		//2.1导入数据表
		SqlPath := filepath.Join(path, "/devsource/codemarket/install", req.Name, "/install.sql")
		aqlerr := clogic_developer.ImportSqlFile(SqlPath)
		if aqlerr != nil {
			res = gf.Failed().SetMsg("导入插件sql数据文件失败").SetData(aqlerr)
			return
		}
		//如果存在前缀-添加导入表前缀
		prefix, _ := g.Cfg().Get(ctx, "database.default.prefix")
		if !g.IsEmpty(prefix) && installCofig.Sqldb.Packtables != "" {
			var tableNames_arr = strings.Split(installCofig.Sqldb.Packtables, ",")
			for _, tableName := range tableNames_arr {
				if tableName != "" {
					g.DB().Exec(ctx, "ALTER TABLE "+tableName+" RENAME TO "+gf.String(prefix)+tableName+";")
				}
			}
		}
		//2.2处理api接口文件
		for _, goDir := range goFileList {
			if strings.HasPrefix(gf.String(goDir["path"]), "/api/") {
				gfile.Copy(filepath.Join(path, "/devsource/codemarket/install", req.Name, "/go", gf.String(goDir["path"])), filepath.Join(path, gf.String(goDir["path"])))
				//处理路由
				dirPaths := strings.Split(gf.String(goDir["path"]), "/api/")
				if len(dirPaths) > 1 {
					modelArr := strings.Split(dirPaths[1], "/")
					if len(modelArr) > 1 {
						clogic_developer.RouteRegister(modelArr[0], modelArr[1], path)
						modelList = append(modelList, fmt.Sprintf("%v-%v", modelArr[0], modelArr[1]))
					} else if len(modelArr) == 1 {
						clogic_developer.CheckIsAddCtrRouter(filepath.Join(path, "internal/router", "router.go"), modelArr[0], "model")
						modelList = append(modelList, fmt.Sprintf("%v", modelArr[0]))
					}
				}
			}
		}
		err = exec.Command("gf", "gen", "ctrl", "-m").Run()
		if err != nil {
			res = gf.Failed().SetMsg("执行生成ctrl控制器命令失败").SetData(err)
		}
		err = gfile.Copy(filepath.Join(path, "/devsource/codemarket/install", req.Name, "/go"), filepath.Join(path))
		if err != nil {
			res = gf.Failed().SetMsg("安装后端go失败").SetData(err)
			return
		}

		//2.3运行生成实现层服务service
		err = exec.Command("gf", "gen", "service").Run()
		if err != nil {
			res = gf.Failed().SetMsg("执行生成实现层服务service失败").SetData(err)
			return
		}
		//2.4运行生成数据表结构dao
		err = exec.Command("gf", "gen", "dao").Run()
		if err != nil {
			res = gf.Failed().SetMsg("执行生成数据表结构dao命令失败").SetData(err)
			return
		}
		//2.5判断是否mod拉取依赖(使用框架为安装包)
		if installCofig.App.IsModTidy {
			exec.Command("go", "mod", "tidy").Run()
		}
	}
	//3.安装前端vue
	vueobjroot, _ := g.Cfg("app").Get(ctx, "app.vueobjroot")
	//3.1安装前端依赖包
	nodeCommand, _ := g.Cfg("app").Get(ctx, "app.nodeCommand")
	commandArr := strings.Split(installCofig.App.CommandLines, ",")
	for _, commstr := range commandArr {
		if nodeCommand.String() == "yarn" {
			cmd := exec.Command("yarn", "add", commstr)
			cmd.Dir = vueobjroot.String()
			cmd.Run()
		} else {
			cmd := exec.Command("npm", "install", commstr)
			cmd.Dir = vueobjroot.String()
			cmd.Run()
		}
	}
	//3.2安装前端代码
	if !g.IsEmpty(installCofig.App.VueFiles) {
		err = gfile.Copy(filepath.Join(path, "/devsource/codemarket/install", req.Name, "/vue"), filepath.Join(vueobjroot.String()))
		if err != nil {
			res = gf.Failed().SetMsg("安装前端vue失败").SetData(err)
			return
		}
	}
	//4.更新配置文件
	upconf := gf.Map{"isinstall": true, "adminmenuids": anenuids, "modellist": fmt.Sprintf("'%v'", gf.String(modelList))}
	cferr := clogic_developer.UpConfFieldData(path+"/devsource/codemarket/install/"+req.Name, upconf)
	if cferr != nil {
		res = gf.Failed().SetMsg("更新配置失败，请重新安装").SetData(req.Name)
		return
	}
	//5.复制配置文件到manifest/codeinstall的的配置文件夹
	gfile.CopyFile(filepath.Join(path, "/devsource/codemarket/install/", req.Name, "config.yml"), filepath.Join(path, "/manifest/codeinstall", req.Name, "config.yml"))
	//6.复制插件包配置到/复制配置文件到manifest/config/code下
	confFilePath := filepath.Join(path, "/devsource/codemarket/install/", req.Name, req.Name+".yaml")
	if _, err := os.Stat(confFilePath); !os.IsNotExist(err) { //存在到统一管理插件使用的配置文件夹
		gfile.CopyFile(confFilePath, filepath.Join(path, "/manifest/config/code", req.Name+".yaml"))
	}
	//6.如存在附件资源存在则复制到 /resource/static下
	staticFilePath := filepath.Join(path, "/devsource/codemarket/install/", req.Name, req.Name)
	if _, err := os.Stat(staticFilePath); !os.IsNotExist(err) { //存在则复制到 /resource/static下
		gfile.Copy(staticFilePath, filepath.Join(path, "/resource/static", req.Name))
	}
	//7.删除安装文件包
	os.RemoveAll(filepath.Join(path, "/devsource/codemarket/install/", req.Name))
	res = gf.Success().SetMsg("安装成功")
	return
}

// 卸载插件
func (c *sAdmindeveloper) UninstallCode(ctx context.Context, req *developer.UninstallCodeReq) (res *gf.R) {
	runEnv, _ := g.Cfg("app").Get(ctx, "app.runEnv")
	if runEnv.String() == "release" {
		res = gf.Failed().SetMsg("生产环境禁止操作，请在开发环境下操作")
		return
	}
	path, err := os.Getwd()
	if err != nil {
		res = gf.Failed().SetMsg("项目路径获取失败")
		return
	}
	//1.获取配置文件
	apppath := filepath.Join(path, "/manifest/codeinstall", req.Name)
	installCofig, err := clogic_developer.GetInstallConfig(apppath)
	if err != nil {
		res = gf.Failed().SetMsg("插件配置文件解析失败").SetData(err)
		return
	}

	//2.删除安装的后端Go代码
	if !g.IsEmpty(installCofig.App.GoFiles) {
		var goFileList []map[string]any
		if err := json.Unmarshal([]byte(installCofig.App.GoFiles), &goFileList); err != nil {
			res = gf.Failed().SetMsg("解析Go后端目录文件数据失败").SetData(err)
			return
		}
		for _, goDir := range goFileList {
			if gf.Bool(goDir["isDir"]) {
				os.RemoveAll(filepath.Join(path, gf.String(goDir["path"])))
			} else {
				os.Remove(filepath.Join(path, gf.String(goDir["path"])))
			}
		}
		//2.2更新admin.go和控制器注册相关
		err = exec.Command("gf", "gen", "ctrl", "-m").Run()
		if err != nil {
			res = gf.Failed().SetMsg("执行生成ctrl控制器命令失败").SetData(err)
		}
		//2.3运行生成实现层服务service
		err = exec.Command("gf", "gen", "service").Run()
		if err != nil {
			res = gf.Failed().SetMsg("执行生成实现层服务service失败").SetData(err)
		}
	}
	//3.删除路由
	if !g.IsEmpty(installCofig.App.Modellist) {
		var modellist []string
		if err := json.Unmarshal([]byte(installCofig.App.Modellist), &modellist); err != nil {
			res = gf.Failed().SetMsg("配置文件模块数据解析失败").SetData(err)
			return
		}
		if len(modellist) > 0 {
			for _, modelstr := range modellist {
				modelArr := strings.Split(modelstr, "-")
				if len(modelArr) == 2 {
					clogic_developer.DelRouter(modelArr[0], modelArr[1], path)
					//删除service
					os.Remove(filepath.Join(path, "/internal/service", fmt.Sprintf("%v_%v.go", modelArr[0], modelArr[1])))
				} else {
					//删除模块路由
					clogic_developer.DelModelRouter(modelArr[0], path)
				}
			}
		}
	}
	//4.删除数据表结构dao文件和数据库对应表
	if !g.IsEmpty(installCofig.Sqldb.Packtables) {
		prefix, _ := g.Cfg().Get(ctx, "database.default.prefix")
		tableNameArr := strings.Split(installCofig.Sqldb.Packtables, ",")
		for _, tableName := range tableNameArr {
			if tableName != "" {
				g.DB().Exec(ctx, "DROP TABLE IF EXISTS "+gf.String(prefix)+tableName)
				os.Remove(filepath.Join(path, "/internal/model/do/", tableName+".go"))
				os.Remove(filepath.Join(path, "/internal/model/entity/", tableName+".go"))
				//删除dao
				os.Remove(filepath.Join(path, "/internal/dao/", tableName+".go"))
				os.Remove(filepath.Join(path, "/internal/dao/internal/", tableName+".go"))
			}
		}
	}
	//5.卸载菜单
	if installCofig.Sqldb.Adminmenuids != "" {
		thirMenu, _ := dao.AuthRule.Ctx(ctx).Unscoped().WhereIn("pid", strings.Split(installCofig.Sqldb.Adminmenuids, ",")).Array("id")
		dao.AuthRule.Ctx(ctx).Unscoped().WhereIn("id", strings.Split(installCofig.Sqldb.Adminmenuids, ",")).Delete()
		dao.AuthRule.Ctx(ctx).Unscoped().WhereIn("pid", strings.Split(installCofig.Sqldb.Adminmenuids, ",")).Delete()
		if len(thirMenu) > 0 { //三级菜单
			dao.AuthRule.Ctx(ctx).Unscoped().WhereIn("pid", thirMenu).Delete()
		}
	}
	//6判断是否mod拉取依赖(使用框架为安装包)
	if installCofig.App.IsModTidy {
		exec.Command("go", "mod", "tidy").Run()
	}
	//7.删除配置文件
	os.RemoveAll(filepath.Join(path, "/manifest/codeinstall", req.Name)) //删除配置文件
	//8.删除config配置文件
	ResourceConfig := filepath.Join(path, "/manifest/config/code", req.Name+".yaml")
	if _, err := os.Stat(ResourceConfig); err == nil {
		os.Remove(ResourceConfig)
	}
	//9.如果/resource/static下存在资源文件则删除
	staticFilePath := filepath.Join(path, "/resource/static", req.Name)
	if _, err := os.Stat(staticFilePath); !os.IsNotExist(err) { //存在
		os.RemoveAll(staticFilePath)
	}
	//10.删除vue前端
	var vueFileList []map[string]any
	if err := json.Unmarshal([]byte(installCofig.App.VueFiles), &vueFileList); err != nil {
		res = gf.Failed().SetMsg("解析Vue前端目录文件数据失败").SetData(err)
		return
	}
	if len(vueFileList) > 0 {
		vueobjroot, _ := g.Cfg("app").Get(ctx, "app.vueobjroot")
		for _, vueDir := range vueFileList {
			if gf.Bool(vueDir["isDir"]) {
				os.RemoveAll(filepath.Join(vueobjroot.String(), gf.String(vueDir["path"])))
			} else {
				os.Remove(filepath.Join(vueobjroot.String(), gf.String(vueDir["path"])))
			}
		}
		//卸载前端依赖包
		nodeCommand, _ := g.Cfg("app").Get(ctx, "app.nodeCommand")
		commandArr := strings.Split(installCofig.App.CommandLines, ",")
		for _, commstr := range commandArr {
			if nodeCommand.String() == "yarn" {
				cmd := exec.Command("yarn", "remove", commstr)
				cmd.Dir = vueobjroot.String()
				cmd.Run()
			} else {
				cmd := exec.Command("npm", "uninstall", commstr)
				cmd.Dir = vueobjroot.String()
				cmd.Run()
			}
		}
	}
	res = gf.Success().SetMsg("卸载成功")
	return
}

// 安装本地插件
func (c *sAdmindeveloper) InstallLocalCode(ctx context.Context, req *developer.InstallLocalCodeReq) (res *gf.R) {
	//request := g.RequestFromCtx(ctx)
	runEnv, _ := g.Cfg("app").Get(ctx, "app.runEnv")
	if runEnv.String() == "release" {
		res = gf.Failed().SetMsg("生产环境禁止操作，请在开发环境下操作")
		return
	}
	path, err := os.Getwd()
	if err != nil {
		res = gf.Failed().SetMsg("项目路径获取失败")
		return
	}
	installPath := "/devsource/codemarket/install"
	downpathzip := filepath.Join(path, installPath)
	codeNames, err := req.File.Save(downpathzip)
	if err != nil {
		res = gf.Failed().SetMsg("上传本地插件包失败").SetData(err)
	}
	//解压
	dezipdir := filepath.Join(path, installPath)
	zipFilePath := filepath.Join(path, installPath, codeNames)
	err = gcompress.UnZipFile(zipFilePath, dezipdir)
	if err == nil {
		os.Remove(zipFilePath)
		filename_arr := strings.Split(codeNames, ".")
		res = gf.Success().SetMsg("解压上传本地插件包成功").SetData(filename_arr[0])
	} else {
		res = gf.Failed().SetMsg("解压上传本地插件包失败").SetData(err)
	}
	return
}

// 查找本地已经安装的包
func (c *sAdmindeveloper) GetInstallPack(ctx context.Context, req *developer.GetInstallPackReq) (res *gf.R) {
	path, err := os.Getwd()
	if err != nil {
		res = gf.Failed().SetMsg("项目路径获取失败")
		return
	}
	pathname := filepath.Join(path, "/manifest/codeinstall")
	rd, err := os.ReadDir(pathname)
	if err != nil {
		res = gf.Success().SetMsg("本地没有安装的包").SetData("")
		return
	}
	var folders = make([]string, 0)
	for _, fi := range rd {
		if fi.IsDir() {
			folders = append(folders, fi.Name())
		}
	}
	res = gf.Success().SetMsg("本地已经安装的包").SetData(strings.Join(folders, ","))
	return
}
