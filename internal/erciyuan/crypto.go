package erciyuan

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"
)

func decryptResponse(raw []byte) ([]byte, error) {
	compact := strings.Join(strings.Fields(string(raw)), "")
	ciphertext, err := base64.StdEncoding.Strict().DecodeString(compact)
	if err != nil || len(ciphertext) == 0 || len(ciphertext)%aes.BlockSize != 0 {
		return nil, errors.New("二次元加密响应格式错误")
	}
	block, err := aes.NewCipher(originalKey[:])
	if err != nil {
		return nil, errors.New("二次元响应解码失败")
	}
	plaintext := make([]byte, len(ciphertext))
	cipher.NewCBCDecrypter(block, originalIV[:]).CryptBlocks(plaintext, ciphertext)
	padding := int(plaintext[len(plaintext)-1])
	if padding < 1 || padding > aes.BlockSize || padding > len(plaintext) {
		return nil, errors.New("二次元加密响应填充错误")
	}
	for _, value := range plaintext[len(plaintext)-padding:] {
		if int(value) != padding {
			return nil, errors.New("二次元加密响应填充错误")
		}
	}
	return plaintext[:len(plaintext)-padding], nil
}

func jsonDocument(raw []byte) (any, error) {
	var result any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&result); err != nil {
		return nil, errors.New("二次元响应不是有效 JSON")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return nil, errors.New("二次元响应包含额外数据")
	}
	switch result.(type) {
	case map[string]any, []any:
		return result, nil
	}
	return nil, errors.New("二次元响应结构无效")
}

// Accept plaintext JSON as well as the original Base64/AES-CBC response and
// encrypted configuration wrappers. Padding and JSON are fully validated.
func decodeDocument(raw []byte) (any, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil, errors.New("二次元返回空响应")
	}
	var document any
	var err error
	if trimmed[0] == '{' || trimmed[0] == '[' {
		document, err = jsonDocument(trimmed)
	} else {
		var plain []byte
		plain, err = decryptResponse(trimmed)
		if err == nil {
			document, err = jsonDocument(plain)
		}
	}
	if err != nil {
		return nil, err
	}
	if root, ok := document.(map[string]any); ok {
		if encrypted, ok := root["encrypted"].(string); ok {
			plain, err := decryptResponse([]byte(encrypted))
			if err != nil {
				return nil, err
			}
			return jsonDocument(plain)
		}
		if nested, ok := root["data"].(map[string]any); ok {
			if encrypted, ok := nested["encrypted"].(string); ok {
				plain, err := decryptResponse([]byte(encrypted))
				if err != nil {
					return nil, err
				}
				root["data"], err = jsonDocument(plain)
				if err != nil {
					return nil, err
				}
			}
		}
	}
	return document, nil
}
