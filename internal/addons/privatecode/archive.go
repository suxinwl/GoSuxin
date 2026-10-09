package privatecode

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
)

const maxPackageSize int64 = 128 << 20

// ValidateArchive checks paths and CRCs without extracting or executing any content.
func ValidateArchive(in io.ReaderAt, size int64) error {
	if size < 1 || size > maxPackageSize {
		return fmt.Errorf("源码包不能超过 128 MB")
	}
	z, err := zip.NewReader(in, size)
	if err != nil {
		return fmt.Errorf("不是有效的 ZIP 源码包")
	}
	if len(z.File) == 0 || len(z.File) > 5000 {
		return fmt.Errorf("源码包文件数量必须为 1 至 5000")
	}
	seen := map[string]bool{}
	var total uint64
	for _, f := range z.File {
		name := strings.TrimSuffix(f.Name, "/")
		if name == "" || strings.ContainsAny(name, "\\:\x00") || strings.HasPrefix(name, "/") || path.Clean(name) != name || f.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("源码包包含不安全路径或链接")
		}
		for _, part := range strings.Split(name, "/") {
			if part == "." || part == ".." {
				return fmt.Errorf("源码包包含越界路径")
			}
		}
		key := strings.ToLower(name)
		if seen[key] {
			return fmt.Errorf("源码包包含重复路径")
		}
		seen[key] = true
		if f.FileInfo().IsDir() {
			continue
		}
		if f.UncompressedSize64 > 256<<20 {
			return fmt.Errorf("源码包单个文件解压大小超限")
		}
		total += f.UncompressedSize64
		if total > 512<<20 {
			return fmt.Errorf("源码包解压总大小超过 512 MB")
		}
		reader, err := f.Open()
		if err != nil {
			return fmt.Errorf("源码包文件不可读取")
		}
		n, readErr := io.Copy(io.Discard, io.LimitReader(reader, int64(f.UncompressedSize64)+1))
		closeErr := reader.Close()
		if readErr != nil || closeErr != nil || uint64(n) != f.UncompressedSize64 {
			return fmt.Errorf("源码包内容或 CRC 校验失败")
		}
	}
	return nil
}
