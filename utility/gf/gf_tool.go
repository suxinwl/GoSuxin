package gf

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/suxinwl/GoSuxin/framework/container/gvar"
	"github.com/suxinwl/GoSuxin/framework/crypto/gmd5"
	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/os/gcfg"
	"github.com/suxinwl/GoSuxin/framework/text/gstr"
)

// Md5 encryption
func Md5(str string) string {
	return gmd5.MustEncryptString(str)
}

// 隐藏手机号等敏感信息用*替换展示
func HideStrInfo(strtype, val string) string {
	if val == "" {
		return ""
	}
	switch strtype {
	case "email":
		var arr = strings.Split(val, "@")
		var star = ""
		if len(arr[0]) <= 3 {
			star = "*"
			arr[0] = gstr.SubStr(arr[0], 0, len(arr[0])) + star
		} else {
			star = "***"
			arr[0] = gstr.SubStr(arr[0], 0, 1) + star + gstr.SubStr(arr[0], len(arr[0])-1, 1)
		}
		return arr[0] + "@" + arr[1]
	case "mobile":
		if len(val) <= 10 {
			return val
		}
		return val[:3] + "****" + val[len(val)-4:]
	}
	return ""
}

// 多维数组合并-权限
func ArrayMerge(data []*gvar.Var) []interface{} {
	var rule_ids_arr []interface{}
	for _, mainv := range data {
		ids_arr := strings.Split(mainv.String(), `,`)
		for _, intv := range ids_arr {
			rule_ids_arr = append(rule_ids_arr, intv)
		}
	}
	return rule_ids_arr
}

// tool-获取树状数组
func GetTreeArray(list OrmResult, pid int64, itemprefix string) OrmResult {
	childs := ToolFar(list, pid) //获取pid下的所有数据
	var chridnum OrmResult
	if childs != nil {
		var number int = 1
		var total int = len(childs)
		for _, v := range childs {
			j := ""
			k := ""
			if number == total {
				j += "└"
				k = ""
				if itemprefix != "" {
					k = "&nbsp;"
				}

			} else {
				j += "├"
				k = ""
				if itemprefix != "" {
					k = "│"
				}
			}
			spacer := ""
			if itemprefix != "" {
				spacer = itemprefix + j
			}
			v["spacer"] = gvar.New(spacer)
			v["children"] = gvar.New(GetTreeArray(list, v["id"].Int64(), itemprefix+k+"&nbsp;"))
			chridnum = append(chridnum, v)
			number++
		}
	}
	return chridnum
}

// base_tool-获取pid下所有数组
func ToolFar(data OrmResult, pid int64) OrmResult {
	var mapString OrmResult
	for _, v := range data {
		if v["pid"].Int64() == pid {
			mapString = append(mapString, v)
		}
	}
	return mapString
}

// 获取后台菜单子树结构
func GetMenuChildrenArray(pdata OrmResult, parent_id int64, pid_file string) OrmResult {
	var returnList OrmResult
	for _, v := range pdata {
		if v[pid_file].Int64() == parent_id {
			children := GetMenuChildrenArray(pdata, v["id"].Int64(), pid_file)
			if children != nil {
				v["children"] = gvar.New(children)
			}
			returnList = append(returnList, v)
		}
	}
	return returnList
}

// 合并数组-两个数组合并为一个数组
func MergeArr(a []*gvar.Var, b []interface{}) []interface{} {
	var arr []interface{}
	for _, i := range a {
		arr = append(arr, i)
	}
	for _, j := range b {
		arr = append(arr, j)
	}
	return arr
}

// 把字符串打散为数组
func SplitAndStr(str, step string) []string {
	return strings.Split(str, step)
}

// 把数组转字符串,号分隔
func ArrayToStr(data interface{}, step string) string {
	if data != nil && data != "" {
		data_arr := data.([]interface{})
		var str_arr = make([]string, len(data_arr))
		for k, v := range data_arr {
			str_arr[k] = fmt.Sprintf("%v", v)
		}
		return strings.Join(str_arr, step)
	} else {
		return ""
	}
}

// 把字符串打散为数组(按,号分割)
func Axplode(data string) []interface{} {
	var rule_ids_arr []interface{}
	ids_arr := strings.Split(data, `,`)
	for _, intv := range ids_arr {
		rule_ids_arr = append(rule_ids_arr, intv)
	}
	return rule_ids_arr
}

// 把开始和结束日期字符串 组合成起始时间做为查询条件
func FindTimeCondition(timeStr string) []interface{} {
	if g.IsEmpty(timeStr) {
		return nil
	}
	datetime_arr := SplitAndStr(timeStr, ",")
	return Slice{datetime_arr[0] + " 00:00", datetime_arr[1] + " 23:59"}
}

// 字符串转JSON编码
func StringToJSON(val interface{}) interface{} {
	str := val.(string)
	if strings.HasPrefix(str, "{") && strings.HasSuffix(str, "}") {
		var parameter interface{}
		_ = json.Unmarshal([]byte(str), &parameter)
		return parameter
	} else {
		var parameter []interface{}
		_ = json.Unmarshal([]byte(str), &parameter)
		return parameter
	}
}

// 判断某个数据表是否存在指定字段
// tablename=表名 field=字段
func DbHaseField(tablename, fields string) bool {
	dbname, _ := gcfg.Instance().Get(ctx, "database.default.name")
	//获取数据库名
	dielddata, _ := g.DB().Query(ctx, "select COLUMN_NAME from information_schema.columns where TABLE_SCHEMA='"+String(dbname)+"' AND TABLE_NAME='"+tablename+"'")
	var tablefields []interface{}
	for _, val := range dielddata {
		var valjson map[string]interface{}
		mdata, _ := json.Marshal(val)
		json.Unmarshal(mdata, &valjson)
		tablefields = append(tablefields, valjson["COLUMN_NAME"].(string))
	}
	return IsContain(tablefields, fields)
}

// 判断元素是否存在数组中
func IsContain(items []interface{}, item string) bool {
	for _, eachItem := range items {
		if eachItem == item {
			return true
		}
	}
	return false
}

// 将getTreeArray的结果返回为二维数组
func GetTreeToList(list []Map, field string) []Map {
	var midleArr []Map
	for _, v := range list {
		var children []Map
		if childrendata, ok := v["children"]; ok && childrendata != nil {
			switch childrendata.(type) {
			case []interface{}:
				for _, cv := range childrendata.([]interface{}) {
					children = append(children, cv.(Map))
				}
			case []Map:
				children = childrendata.([]Map)
			}
		} else {
			children = nil
		}
		delete(v, "children")
		v[field+"_txt"] = fmt.Sprintf("%v %v", v["spacer"], v[field+""])
		if _, ok := v["id"]; ok {
			midleArr = append(midleArr, v)
		}
		if len(children) > 0 {
			newarr := GetTreeToList(children, field)
			midleArr = ArrayMerge_x(midleArr, newarr)
		}
	}
	return midleArr
}

// 数组拼接
func ArrayMerge_x(ss ...[]Map) []Map {
	n := 0
	for _, v := range ss {
		n += len(v)
	}
	s := make([]Map, 0, n)
	for _, v := range ss {
		s = append(s, v...)
	}
	return s
}

// string数组去重
func RemoveDuplicates(arr []string) []string {
	uniqueMap := make(map[string]bool)
	var result []string
	for _, str := range arr {
		if _, exists := uniqueMap[str]; !exists {
			uniqueMap[str] = true
			result = append(result, str)
		}
	}
	return result
}

// 下划线转驼峰
func SnakeToCamel(s string) string {
	words := strings.Split(s, "_")
	for i, word := range words {
		words[i] = gstr.UcFirst(word)
	}
	return strings.Join(words, "")
}
