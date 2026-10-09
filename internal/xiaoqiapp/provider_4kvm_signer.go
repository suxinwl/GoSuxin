package app

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const fourKVMPlaySecret = "nbmovie2024secretkey"

// 4KVM's browser signer uses HMAC-SHA256 and a small XOR token. Keeping the
// signer in Go avoids a Node.js/browser dependency on a Linux server.
func build4KVMPlayURL(_ context.Context, dataID, secretKey, quality, playKey string) (string, error) {
	dataID = strings.TrimSpace(dataID)
	secretKey = strings.TrimSpace(secretKey)
	quality = strings.TrimSpace(quality)
	if dataID == "" || secretKey == "" || quality == "" {
		return "", fmt.Errorf("4KVM playback parameters are empty")
	}
	timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
	hash := hmac.New(sha256.New, []byte(secretKey))
	_, _ = hash.Write([]byte(dataID + ":" + timestamp + ":" + secretKey))
	signature := hex.EncodeToString(hash.Sum(nil)[:16])
	encodedKey := "0"
	if playKey != "" && playKey != "0" {
		keyBytes := []byte(playKey)
		secretBytes := []byte(fourKVMPlaySecret)
		for index := range keyBytes {
			keyBytes[index] ^= secretBytes[index%len(secretBytes)]
		}
		encodedKey = base64.StdEncoding.EncodeToString(keyBytes)
	}
	query := url.Values{"p": {dataID}, "v": {secretKey}, "q": {quality}, "s": {signature}, "t": {timestamp}, "k": {encodedKey}}
	return "/video/play?" + query.Encode(), nil
}
