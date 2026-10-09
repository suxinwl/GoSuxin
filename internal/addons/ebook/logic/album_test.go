package album

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestFileGrantRejectsForgeryAndWrongAlbum(t *testing.T) {
	token := NewFileGrant(12, 0, 7)
	if grant, err := parseFileGrant(token, 12); err != nil || grant.Share != 7 {
		t.Fatalf("valid grant: %+v %v", grant, err)
	}
	if _, err := parseFileGrant(token, 13); err == nil {
		t.Fatal("cross-album access allowed")
	}
	parts := strings.Split(token, ".")
	parts[0] = base64.RawURLEncoding.EncodeToString([]byte(`{"a":12,"o":1,"e":9999999999}`))
	if _, err := parseFileGrant(strings.Join(parts, "."), 12); err == nil {
		t.Fatal("forged owner grant allowed")
	}
	for _, bad := range []string{"", "x", "x.y.z", "bad.signature"} {
		if _, err := parseFileGrant(bad, 12); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
}

func TestConfinedFile(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "page.jpg"), []byte("image"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := confinedFile(root, "page.jpg"); err != nil {
		t.Fatal(err)
	}
	for _, relative := range []string{"../page.jpg", "./page.jpg", "/page.jpg", `..\page.jpg`, "page.jpg:secret", "page.jpg.", "page.jpg ", "", "folder//page.jpg"} {
		if _, err := confinedFile(root, relative); err == nil {
			t.Fatalf("unsafe path accepted: %q", relative)
		}
	}
	outside := filepath.Join(t.TempDir(), "outside.jpg")
	if err := os.WriteFile(outside, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "link.jpg")); err == nil {
		if _, err := confinedFile(root, "link.jpg"); err == nil {
			t.Fatal("symlink escape allowed")
		}
	}
}

func TestPublishStates(t *testing.T) {
	for _, status := range []string{"draft", "processing", "failed", "unknown", ""} {
		if CanPublish(status, 10) {
			t.Fatalf("published invalid state %s", status)
		}
	}
	for _, status := range []string{"ready", "offline", "published"} {
		if !CanPublish(status, 1) || CanPublish(status, 0) {
			t.Fatalf("incorrect page/state check: %s", status)
		}
	}
}

func TestPDFPageOrder(t *testing.T) {
	names := []string{"page-10.jpg", "page-2.jpg", "page-1.jpg"}
	sort.Slice(names, func(i, j int) bool { return pageNumber(names[i]) < pageNumber(names[j]) })
	if strings.Join(names, ",") != "page-1.jpg,page-2.jpg,page-10.jpg" {
		t.Fatal(names)
	}
	if pageNumber("source.pdf") != 0 || pageNumber("page-1.png") != 0 {
		t.Fatal("non-JPEG page accepted")
	}
}
