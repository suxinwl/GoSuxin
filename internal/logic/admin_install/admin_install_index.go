package admininstall

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/suxinwl/GoSuxin/api/admin/install"
	"github.com/suxinwl/GoSuxin/utility/gf"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/suxinwl/GoSuxin/framework/database/gdb"
	"github.com/suxinwl/GoSuxin/framework/encoding/gcompress"
	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/text/gstr"
	"github.com/suxinwl/GoSuxin/framework/util/grand"
)

// 获取安装配置数据
func (s *sAdmininstall) GetConfig(ctx context.Context, req *install.GetConfigReq) (res *gf.R) {
	if _, err := os.Stat("devsource/developer/install/install.lock"); err == nil {
		return gf.Success().SetData(gf.Map{"isLock": true})
	}
	database, _ := g.Cfg().Get(ctx, "database.default")
	mapDatabase := database.Map()
	appConf, _ := g.Cfg("app").Get(ctx, "app")
	mapAppConf := appConf.Map()
	path, _ := os.Getwd() //获取当前路径
	filePath := filepath.Join(path, "/devsource/developer/install/install.lock")
	_, errLock := os.Stat(filePath)
	res = gf.Success().SetMsg("获取应用配置").SetData(gf.Map{
		"database": gf.Map{
			"host":   mapDatabase["host"],
			"port":   mapDatabase["port"],
			"user":   mapDatabase["user"],
			"pass":   "",
			"name":   mapDatabase["name"],
			"prefix": mapDatabase["prefix"],
		},
		"app": gf.Map{
			"vueobjroot": mapAppConf["vueobjroot"],
		},
		"isLock": errLock == nil,
	})
	return
}

// 执行安装操作
func (s *sAdmininstall) Save(ctx context.Context, req *install.SaveReq) (res *gf.R) {
	if _, err := os.Stat("devsource/developer/install/install.lock"); err == nil {
		return gf.Failed().SetMsg("项目已安装，禁止重复安装")
	}
	//1.修改数据库配置
	err := gf.UpConfigFild("/manifest/config/config.yaml", gf.Map{
		"host":   req.FormDB["host"],
		"port":   req.FormDB["port"],
		"user":   req.FormDB["user"],
		"pass":   req.FormDB["pass"],
		"name":   req.FormDB["name"],
		"prefix": req.FormDB["prefix"],
	}, "    ")
	if err != nil {
		res = gf.Failed().SetMsg("更新数据库配置失败！").SetData(err.Error())
		return
	}
	//2.修改app配置
	if !g.IsEmpty(req.FormApp["vueobjroot"]) { //判断目录是否有其他文件
		dir, _ := os.ReadDir(filepath.Join(req.FormApp["vueobjroot"]))
		if len(dir) > 0 && !strings.Contains(req.FormApp["vueobjroot"], "goframepro") { //如果存在文件创建新目录
			req.FormApp["vueobjroot"] = filepath.Join(req.FormApp["vueobjroot"]) + "/goframepro"
		}
	}
	apperr := gf.UpConfigFild("/manifest/config/app.yaml", gf.Map{
		"vueobjroot": gstr.Replace(req.FormApp["vueobjroot"], "\\", "/"),
	}, "  ")
	if apperr != nil {
		res = gf.Failed().SetMsg("修改app配置失败！").SetData(apperr.Error())
		return
	}
	//修改hack开发文件配置
	hackdberr := gf.UpConfigFild("/hack/config.yaml", gf.Map{
		"- link":         fmt.Sprintf("\"mysql:%v:%v@tcp(%v:%v)/%v\"", req.FormDB["user"], req.FormDB["pass"], req.FormDB["host"], req.FormDB["port"], req.FormDB["name"]),
		"  removePrefix": fmt.Sprintf("\"%v\"", req.FormDB["prefix"]),
	}, "      ")
	if hackdberr != nil {
		res = gf.Failed().SetMsg("修改hack/config数据库配置失败！").SetData(hackdberr.Error())
		return
	}

	//3.安装前端代码
	path, err := os.Getwd() //获取当前路径
	if err != nil {
		res = gf.Failed().SetMsg("项目路径获取失败").SetData(err.Error())
		return
	}
	//前端zip代码包
	srcFilePath := filepath.Join(path, "/devsource/developer/install/webcode.zip")
	dstFilePath := filepath.Join(path, "/web")
	if !g.IsEmpty(req.FormApp["vueobjroot"]) {
		dstFilePath = filepath.Join(req.FormApp["vueobjroot"])
	}
	if unzipErr := gcompress.UnZipFile(srcFilePath, dstFilePath, "webcode"); unzipErr != nil {
		return gf.Failed().SetMsg("释放前端安装包失败")
	}
	//4.导入数据sql数据
	//4.1导入基础数据库配置
	db, err := gdb.New(gdb.ConfigNode{
		Link: fmt.Sprintf("mysql:%v:%v@tcp(%v:%v)", req.FormDB["user"], req.FormDB["pass"], req.FormDB["host"], req.FormDB["port"]),
	})
	if err != nil {
		res = gf.Failed().SetMsg("连接数据库失败").SetData(err)
		return
	}
	//2创建数据库
	sqlstr := fmt.Sprintf("CREATE DATABASE IF NOT EXISTS %v DEFAULT CHARACTER SET utf8mb4 DEFAULT COLLATE utf8mb4_general_ci", req.FormDB["name"])
	_, adderr := db.Exec(ctx, sqlstr)
	if adderr != nil {
		res = gf.Failed().SetMsg(fmt.Sprintf("创建数据库Error %s when creating DB\n", adderr)).SetData(adderr)
		return
	}
	db.Close(ctx)
	//创建好数据库后重新连接并导入数据
	secoddb, err := sql.Open("mysql", dsn(req.FormDB["user"], req.FormDB["pass"], req.FormDB["host"], req.FormDB["port"], req.FormDB["name"]))
	if err != nil {
		res = gf.Failed().SetMsg(fmt.Sprintf("链接数据库%v，失败Error %s when opening DB", req.FormDB["name"], err))
		return
	}
	defer secoddb.Close()
	secoddb.SetMaxOpenConns(20)
	secoddb.SetMaxIdleConns(20)
	secoddb.SetConnMaxLifetime(time.Minute * 5)
	ctx, cancelfunc := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelfunc()
	err = secoddb.PingContext(ctx)
	if err != nil {
		res = gf.Failed().SetMsg(fmt.Sprintf("检查重链接数据库%v，失败Errors %s pinging DB", req.FormDB["name"], err))
		return
	}
	SqlPath := filepath.Join(path, "/devsource/developer/install/goframepro.sql")
	sqls, sqlerr := os.ReadFile(SqlPath)
	if sqlerr != nil {
		res = gf.Failed().SetMsg("数据库文件不存在：" + SqlPath).SetData(sqlerr)
		return
	}
	if _, importErr := secoddb.Exec(string(sqls)); importErr != nil {
		return gf.Failed().SetMsg("初始化数据库失败，请检查数据库权限与安装 SQL")
	}
	//5.修改表前缀
	rows, _ := secoddb.Query("SELECT TABLE_NAME FROM INFORMATION_SCHEMA.TABLES WHERE TABLE_TYPE = 'BASE TABLE' AND TABLE_SCHEMA='" + req.FormDB["name"] + "'")
	defer rows.Close()
	var tablename_str string
	var sql_str string
	var table_name string
	for rows.Next() {
		rows.Scan(&tablename_str)
		if strings.HasPrefix(tablename_str, "gf_") && req.FormDB["prefix"] != "gf_" {
			table_name = strings.Replace(tablename_str, "gf_", req.FormDB["prefix"], 1)
			// secoddb.Exec("DROP TABLE " + table_name + ";") //删除已存在数据表，再更新新表前缀
			sql_str = "ALTER TABLE " + tablename_str + " RENAME TO " + table_name + ";"
		} else if strings.HasPrefix(tablename_str, req.FormDB["prefix"]) {
			continue
		} else { //安装sql数据没有前缀
			sql_str = "ALTER TABLE " + tablename_str + " RENAME TO " + req.FormDB["prefix"] + tablename_str + ";"
		}
		secoddb.Exec(sql_str)
	}
	//6.修改后台账号
	salt := grand.Str("123456789", 6)
	businesspass := fmt.Sprintf("%v%v", gf.Md5(req.FormApp["password"]), salt)
	_, upberr := secoddb.Exec("update "+req.FormDB["prefix"]+"admin set username = ?, password = ?, salt = ? where id = ?", req.FormApp["username"], gf.Md5(businesspass), salt, 1)
	if upberr != nil {
		res = gf.Failed().SetMsg("更新超级管理员账号密码失败：").SetData(upberr)
		return
	}
	//7.创建安装锁文件-防止重复安装
	filePath := filepath.Join(path, "/devsource/developer/install/install.lock")
	file, ferr := os.Create(filePath)
	if ferr == nil {
		defer file.Close()
	}
	res = gf.Success().SetMsg("执行安装操作成功").SetData(path)
	return
}

// 链接数据库配置
func dsn(username, password, hostname, hostport, dbName interface{}) string {
	return fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8&parseTime=True&loc=Local&timeout=1000ms&sql_mode=%s&multiStatements=true", username, password, hostname, hostport, dbName, "NO_ENGINE_SUBSTITUTION")
}
