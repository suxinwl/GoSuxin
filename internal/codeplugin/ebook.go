// Package codeplugin installs the bundled, hash-checked source plugins.
// It never executes archive-provided commands or imports production data.
package codeplugin

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
)

type Manifest struct {
	Format      string            `json:"format"`
	Name        string            `json:"name"`
	Title       string            `json:"title"`
	Version     string            `json:"version"`
	Description string            `json:"description"`
	Files       map[string]string `json:"files"`
}

var installMu sync.Mutex

const Bundle = "devsource/codemarket/release/ebook.zip"
const Marker = "manifest/codeinstall/ebook/plugin.json"

var sourceVersions = map[string]string{"ebook": "1.0.0", "privatecode": "1.1.5", "analysis": "1.0.0"}

func MarkerFor(name string) string {
	if _, ok := sourceVersions[name]; !ok {
		return ""
	}
	return filepath.Join("manifest", "codeinstall", name, "plugin.json")
}

func IsNative(filename string) bool {
	z, err := zip.OpenReader(filename)
	if err != nil {
		return false
	}
	defer z.Close()
	for _, f := range z.File {
		if f.Name == "plugin.json" {
			return true
		}
	}
	return false
}

func allowed(name string) bool {
	if name == "" || path.Clean(name) != name || strings.ContainsAny(name, "\\:") || strings.HasPrefix(name, "/") {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		if part == ".." || strings.TrimRight(part, ". ") != part || part == ".git" || part == "node_modules" || strings.HasSuffix(part, ".local") {
			return false
		}
		stem := strings.ToUpper(strings.SplitN(part, ".", 2)[0])
		if stem == "CON" || stem == "PRN" || stem == "AUX" || stem == "NUL" || len(stem) == 4 && (strings.HasPrefix(stem, "COM") || strings.HasPrefix(stem, "LPT")) && stem[3] >= '0' && stem[3] <= '9' {
			return false
		}
	}
	for plugin := range sourceVersions {
		if allowedFor(name, plugin) {
			return true
		}
	}
	return false
}

func allowedFor(name, plugin string) bool {
	if plugin == "ebook" {
		return strings.HasPrefix(name, "internal/addons/ebook/") || strings.HasPrefix(name, "web/src/views/album/") || strings.HasPrefix(name, "resource/plugins/ebook/") || strings.HasPrefix(name, "plugins/ebook/") || name == "web/src/api/album.ts" || name == "web/src/utils/album-site.ts" || name == "internal/router/plugin_ebook.go"
	}
	if plugin == "privatecode" || plugin == "analysis" {
		return strings.HasPrefix(name, "internal/addons/"+plugin+"/") || strings.HasPrefix(name, "internal/addons/support/") || strings.HasPrefix(name, "web/src/views/"+plugin+"/") || strings.HasPrefix(name, "plugins/"+plugin+"/") || name == "internal/router/plugin_"+plugin+".go"
	}
	return false
}

func ReadBundle(filename string) (Manifest, map[string][]byte, error) {
	var m Manifest
	z, err := zip.OpenReader(filename)
	if err != nil {
		return m, nil, err
	}
	defer z.Close()
	if len(z.File) > 2000 {
		return m, nil, errors.New("插件文件过多")
	}
	files := map[string][]byte{}
	seen := map[string]bool{}
	var total uint64
	for _, f := range z.File {
		if f.FileInfo().IsDir() {
			continue
		}
		if f.Mode()&os.ModeSymlink != 0 {
			return m, nil, errors.New("插件不能包含链接")
		}
		if f.Name != "plugin.json" && !allowed(f.Name) {
			return m, nil, fmt.Errorf("插件文件路径不允许: %s", f.Name)
		}
		key := strings.ToLower(f.Name)
		if seen[key] {
			return m, nil, errors.New("重复插件文件")
		}
		seen[key] = true
		total += f.UncompressedSize64
		if total > 100<<20 || f.UncompressedSize64 > 20<<20 {
			return m, nil, errors.New("插件解压大小超限")
		}
		in, e := f.Open()
		if e != nil {
			return m, nil, e
		}
		b, e := io.ReadAll(io.LimitReader(in, 20<<20+1))
		_ = in.Close()
		if e != nil {
			return m, nil, e
		}
		if len(b) > 20<<20 {
			return m, nil, errors.New("插件文件过大")
		}
		files[f.Name] = b
	}
	if err = json.Unmarshal(files["plugin.json"], &m); err != nil {
		return m, nil, errors.New("不是 Suxin 源码插件包")
	}
	delete(files, "plugin.json")
	version, supported := sourceVersions[m.Name]
	if m.Format != "suxin-source-v1" || !supported || m.Version != version || len(m.Files) == 0 || len(m.Files) != len(files) {
		return m, nil, errors.New("插件清单不兼容或不完整")
	}
	for name, want := range m.Files {
		b, ok := files[name]
		if !ok || !allowed(name) || !allowedFor(name, m.Name) {
			return m, nil, errors.New("插件清单路径不匹配")
		}
		digest := sha256.Sum256(b)
		if hex.EncodeToString(digest[:]) != want {
			return m, nil, fmt.Errorf("插件内容校验失败: %s", name)
		}
	}
	return m, files, nil
}

func Install(root, filename string) (Manifest, error) {
	installMu.Lock()
	defer installMu.Unlock()
	m, files, err := ReadBundle(filename)
	if err != nil {
		return m, err
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return m, err
	}
	// Validate every target before writing. Existing user changes are never overwritten.
	for name, b := range files {
		target := filepath.Join(root, filepath.FromSlash(name))
		for cur := target; cur != root; cur = filepath.Dir(cur) {
			info, e := os.Lstat(cur)
			if e == nil && info.Mode()&os.ModeSymlink != 0 {
				return m, fmt.Errorf("目标包含链接: %s", name)
			}
			if e != nil && !os.IsNotExist(e) {
				return m, e
			}
		}
		old, e := os.ReadFile(target)
		if e == nil && !bytes.Equal(old, b) {
			return m, fmt.Errorf("目标已有不同文件，请先合并: %s", name)
		}
		if e != nil && !os.IsNotExist(e) {
			return m, e
		}
	}
	var created []string
	rollback := func() {
		for _, p := range created {
			_ = os.Remove(p)
		}
	}
	for name, b := range files {
		target := filepath.Join(root, filepath.FromSlash(name))
		if _, e := os.Stat(target); e == nil {
			continue
		}
		if err = os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			rollback()
			return m, err
		}
		f, e := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
		if e != nil {
			rollback()
			return m, e
		}
		created = append(created, target)
		_, e = f.Write(b)
		closeErr := f.Close()
		if e == nil {
			e = closeErr
		}
		if e != nil {
			rollback()
			return m, e
		}
	}
	marker := filepath.Join(root, MarkerFor(m.Name))
	if err = os.MkdirAll(filepath.Dir(marker), 0755); err != nil {
		rollback()
		return m, err
	}
	b, _ := json.MarshalIndent(m, "", "  ")
	tmp, err := os.CreateTemp(filepath.Dir(marker), "plugin-*.json")
	if err != nil {
		rollback()
		return m, err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	_, err = tmp.Write(b)
	closeErr := tmp.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(tmpName, marker)
	}
	if err != nil {
		rollback()
	}
	return m, err
}
