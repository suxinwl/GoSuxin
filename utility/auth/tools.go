package auth

import (
	"github.com/suxinwl/GoSuxin/utility/tools/cryptojs"
	"strings"
	"time"

	"github.com/suxinwl/GoSuxin/framework/net/ghttp"
	"github.com/suxinwl/GoSuxin/framework/util/gconv"
)

const (
	BearerPrefix = "Bearer "
)

func decodeDynamicToken(value string) (token string, ok bool) {
	defer func() { if recover() != nil { token, ok = "", false } }()
	decoded, err := cryptojs.AesDecrypt(value)
	if err != nil { return "", false }
	parts := strings.Split(decoded, "#")
	if len(parts) != 2 || parts[0] == "" { return "", false }
	delta := time.Now().Unix() - gconv.Int64(parts[1])
	if delta < 0 || delta >= 30 { return "", false }
	return parts[0], true
}

// Configured API groups still require the short-lived encrypted wrapper.
// Plugin bootstrap also accepts it; the plugin cookie contains the base token.
// Credentials are deliberately not read from query parameters.
func GetRequestToken(r *ghttp.Request) string {
	value := strings.TrimSpace(r.Header.Get("Authorization"))
	value = strings.TrimSpace(strings.TrimPrefix(value, BearerPrefix))
	if value == "" { return "" }
	group := strings.Split(strings.Trim(r.URL.Path, "/"), "/")[0]
	required := false
	for _, configured := range strings.Split(DynamicToken.String(), ",") {
		if strings.TrimSpace(configured) != "" && strings.TrimSpace(configured) == group { required = true }
	}
	if decoded, ok := decodeDynamicToken(value); ok { return decoded }
	if required { return "" }
	return value
}

// 获取jwttoken
func GetJwtToken(r *ghttp.Request) (tData *TokenData, key string, err error) {
	token := GetRequestToken(r)
	tData, key, err = GetTokenData(r.GetCtx(), token)
	return
}
