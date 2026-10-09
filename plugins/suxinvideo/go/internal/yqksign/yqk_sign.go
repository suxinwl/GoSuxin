// Package yqksign signs Together APP 1.3.64 (versionCode 1107) request parameters.
package yqksign

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
)

const AppID = "d6d520ea90904f1ba680ed6c9c9f9007"
const AppKey = "70af67d2b6cf47679b397ea4c1886877"

// SigningText excludes sign, null and empty strings. Values are not URL encoded.
// Structured business values (queryValueJson/historyList) must be JSON strings.
// When decoding JSON, call Decoder.UseNumber to retain integers precisely.
func SigningText(params map[string]any) (string, error) {
	keys := make([]string, 0, len(params))
	for k, v := range params {
		if k == "sign" || v == nil || v == "" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := utf16.Encode([]rune(keys[i])), utf16.Encode([]rune(keys[j]))
		for p := 0; p < len(a) && p < len(b); p++ {
			if a[p] != b[p] {
				return a[p] < b[p]
			}
		}
		return len(a) < len(b)
	})
	pairs := make([]string, 0, len(keys))
	for _, k := range keys {
		v, err := valueText(params[k])
		if err != nil {
			return "", fmt.Errorf("parameter %s: %w", k, err)
		}
		pairs = append(pairs, k+"="+v)
	}
	return strings.Join(pairs, "&") + "&appKey=" + AppKey, nil
}

func valueText(v any) (string, error) {
	switch x := v.(type) {
	case string:
		return x, nil
	case bool:
		return strconv.FormatBool(x), nil
	case json.Number:
		n, err := x.Int64()
		if err != nil {
			return "", fmt.Errorf("only integer JSON numbers are verified: %w", err)
		}
		return strconv.FormatInt(n, 10), nil
	}
	r := reflect.ValueOf(v)
	switch r.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(r.Int(), 10), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.FormatUint(r.Uint(), 10), nil
	}
	return "", fmt.Errorf("use strings, integers, booleans or null; got %T", v)
}

// GenerateSign returns a lowercase, 32-character MD5 signature.
func GenerateSign(params map[string]any) (string, error) {
	text, err := SigningText(params)
	if err != nil {
		return "", err
	}
	sum := md5.Sum([]byte(text))
	return hex.EncodeToString(sum[:]), nil
}

// SignRequest copies the parameters and replaces any old sign.
func SignRequest(params map[string]any) (map[string]any, error) {
	sign, err := GenerateSign(params)
	if err != nil {
		return nil, err
	}
	result := make(map[string]any, len(params)+1)
	for k, v := range params {
		result[k] = v
	}
	result["sign"] = sign
	return result, nil
}
