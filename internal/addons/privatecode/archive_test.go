package privatecode

import (
	"archive/zip"
	"bytes"
	"os"
	"strings"
	"testing"
)

func archive(names []string, symlink bool) []byte {
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	for _, name := range names {
		h := &zip.FileHeader{Name: name, Method: zip.Store}
		if symlink {
			h.SetMode(os.ModeSymlink | 0600)
		}
		w, _ := z.CreateHeader(h)
		_, _ = w.Write([]byte("source contents"))
	}
	_ = z.Close()
	return b.Bytes()
}
func TestArchiveRejectsUnsafePathsAndCorruption(t *testing.T) {
	for _, names := range [][]string{{"../config.yaml"}, {"/config.yaml"}, {"C:/config.yaml"}, {`src\main.go`}, {"a/../../x"}, {"same.go", "SAME.go"}} {
		b := archive(names, false)
		if err := ValidateArchive(bytes.NewReader(b), int64(len(b))); err == nil {
			t.Fatalf("accepted unsafe archive %v", names)
		}
	}
	b := archive([]string{"link"}, true)
	if err := ValidateArchive(bytes.NewReader(b), int64(len(b))); err == nil {
		t.Fatal("accepted symlink")
	}
	b = archive([]string{"readme.txt"}, false)
	index := bytes.Index(b, []byte("source contents"))
	b[index] ^= 1
	if err := ValidateArchive(bytes.NewReader(b), int64(len(b))); err == nil {
		t.Fatal("accepted damaged contents")
	}
	if err := ValidateArchive(bytes.NewReader([]byte("not zip")), 7); err == nil {
		t.Fatal("accepted non-ZIP")
	}
	b = archive([]string{"main.go"}, false)
	if err := ValidateArchive(bytes.NewReader(b), maxPackageSize+1); err == nil {
		t.Fatal("accepted oversized package")
	}
}
func TestArchiveAllowsSourcesAndOpaqueFileKeys(t *testing.T) {
	b := archive([]string{"插件/config.yml", "插件/go/main.go", "插件/vue/index.vue"}, false)
	if err := ValidateArchive(bytes.NewReader(b), int64(len(b))); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"../secret", "C:/secret", strings.Repeat("a", 63), strings.Repeat("z", 64)} {
		if _, err := packagePath(key); err == nil {
			t.Fatalf("accepted file key %q", key)
		}
	}
	if _, err := packagePath(strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
}
