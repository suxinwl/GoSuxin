package auth

import (
	"github.com/suxinwl/GoSuxin/utility/tools/cryptojs"
	"strings"
	"time"

	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/net/ghttp"
	"github.com/suxinwl/GoSuxin/framework/util/gconv"
)

const (
	BearerPrefix = "Bearer "
)

// 获取请求中的token
func GetRequestToken(r *ghttp.Request) (token string) {
	// 请求头获取
	tokenstr := r.Header.Get("Authorization")
	if g.IsEmpty(tokenstr) {
		return
	}
	//处理动态token-30秒时效
	if pathArr := strings.Split(r.Request.URL.Path, "/"); len(pathArr) > 0 && strings.Contains(DynamicToken.String(), pathArr[1]) {
		tokenstrAes, err := cryptojs.AesDecrypt(tokenstr)
		if tokenstrAesArr := strings.Split(tokenstrAes, "#"); err == nil && len(tokenstrAesArr) == 2 && (time.Now().Unix()-gconv.Int64(tokenstrAesArr[1]) < 30) { //30秒时效
			tokenstr = tokenstrAesArr[0]
		} else {
			return
		}
	}
	tokenArr := strings.Split(tokenstr, BearerPrefix)
	if len(tokenstr) > 0 && len(tokenArr) >= 2 && tokenArr[0] == BearerPrefix {
		return tokenArr[1]
	} else if len(tokenstr) > 0 && len(tokenArr) == 1 {
		return tokenArr[0]
	}
	// 参数传递token
	if q := r.Get("token"); !q.IsEmpty() {
		return q.String()
	}
	// Cookies传递token
	if c := r.Cookie.Get("token"); !c.IsEmpty() {
		return c.String()
	}
	return
}

// 获取jwttoken
func GetJwtToken(r *ghttp.Request) (tData *TokenData, key string, err error) {
	token := GetRequestToken(r)
	tData, key, err = GetTokenData(r.GetCtx(), token)
	return
}
