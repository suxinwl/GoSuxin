package codeplugin

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func fixture(t *testing.T, name, content string, corrupt bool) string {
	t.Helper()
	filename := filepath.Join(t.TempDir(), "ebook.zip")
	f, e := os.Create(filename)
	if e != nil {
		t.Fatal(e)
	}
	z := zip.NewWriter(f)
	sum := sha256.Sum256([]byte(content))
	m := Manifest{Format: "suxin-source-v1", Name: "ebook", Version: "1.0.0", Files: map[string]string{name: hex.EncodeToString(sum[:])}}
	if corrupt {
		m.Files[name] = "invalid"
	}
	w, _ := z.Create("plugin.json")
	_ = json.NewEncoder(w).Encode(m)
	w, _ = z.Create(name)
	_, _ = w.Write([]byte(content))
	_ = z.Close()
	_ = f.Close()
	return filename
}
func sourceFixture(t *testing.T, plugin, name string) string {
	t.Helper()
	filename := filepath.Join(t.TempDir(), plugin+".zip")
	f, err := os.Create(filename)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	content := []byte("package addon\n")
	sum := sha256.Sum256(content)
	m := Manifest{Format: "suxin-source-v1", Name: plugin, Version: sourceVersions[plugin], Files: map[string]string{name: hex.EncodeToString(sum[:])}}
	w, _ := z.Create("plugin.json")
	_ = json.NewEncoder(w).Encode(m)
	w, _ = z.Create(name)
	_, _ = w.Write(content)
	_ = z.Close()
	_ = f.Close()
	return filename
}
func TestSelectedSourcePluginsUseSeparateManifestsAndBoundaries(t *testing.T) {
	for _, plugin := range []string{"privatecode", "analysis"} {
		root := t.TempDir()
		name := "internal/addons/" + plugin + "/register.go"
		m, err := Install(root, sourceFixture(t, plugin, name))
		if err != nil || m.Name != plugin {
			t.Fatalf("%s: %v", plugin, err)
		}
		if _, err := os.Stat(filepath.Join(root, MarkerFor(plugin))); err != nil {
			t.Fatal(err)
		}
		for _, foreign := range []string{"internal/addons/ebook/example.go", "web/src/views/album/manage/index.vue", "manifest/config/config.yaml"} {
			if _, _, err := ReadBundle(sourceFixture(t, plugin, foreign)); err == nil {
				t.Fatalf("%s accepted foreign path %s", plugin, foreign)
			}
		}
	}
	if MarkerFor("../config") != "" {
		t.Fatal("unknown manifest escaped marker directory")
	}
}

func TestBundledSelectedSourcePluginsInstallIntoEmptyWorkspace(t *testing.T) {
	for _, name := range []string{"privatecode", "analysis"} {
		root := t.TempDir()
		archive := filepath.Join("..", "..", "devsource", "codemarket", "release", name+".zip")
		manifest, err := Install(root, archive)
		if err != nil || manifest.Name != name {
			t.Fatalf("%s: %v", name, err)
		}
		for _, file := range []string{"internal/router/plugin_" + name + ".go", "internal/addons/support/support.go", "plugins/" + name + "/README.md", MarkerFor(name)} {
			if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(file))); err != nil {
				t.Fatal(err)
			}
		}
	}
}
func TestInstallRejectsTraversalAndMismatch(t *testing.T) {
	for _, name := range []string{"../config.yaml", "C:/config.yaml", `internal\addons\ebook\escape.go`, "internal/addons/ebook/../other.go", "internal/addons/ebook/.. /other.go", "internal/addons/ebook/NUL.go", "manifest/config/app.yaml"} {
		if _, _, e := ReadBundle(fixture(t, name, "data", false)); e == nil {
			t.Fatalf("accepted %s", name)
		}
	}
	if _, _, e := ReadBundle(fixture(t, "internal/addons/ebook/example.go", "data", true)); e == nil {
		t.Fatal("accepted checksum mismatch")
	}
}
func TestInstallIsRepeatableAndPreservesCustomFiles(t *testing.T) {
	root := t.TempDir()
	name := "internal/addons/ebook/example.go"
	archive := fixture(t, name, "package ebook\n", false)
	for i := 0; i < 2; i++ {
		if _, e := Install(root, archive); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := os.Stat(filepath.Join(root, Marker)); e != nil {
		t.Fatal(e)
	}
	target := filepath.Join(root, filepath.FromSlash(name))
	_ = os.WriteFile(target, []byte("custom changes"), 0644)
	if _, e := Install(root, archive); e == nil {
		t.Fatal("overwrote user changes")
	}
	b, _ := os.ReadFile(target)
	if string(b) != "custom changes" {
		t.Fatal("modified existing file")
	}
}

func TestBundledEbookInstallsIntoEmptyWorkspace(t *testing.T) {
	archive := filepath.Join("..", "..", Bundle)
	root := t.TempDir()
	m, err := Install(root, archive)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"internal/router/plugin_ebook.go", "internal/addons/ebook/register.go", "web/src/views/album/manage/index.vue", "resource/plugins/ebook/reader/index.html", "plugins/ebook/README.md"} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(name))); err != nil {
			t.Fatal(err)
		}
	}
	if len(m.Files) < 40 {
		t.Fatal("incomplete album bundle")
	}
	for _, name := range []string{"storage", "manifest/config", "tools", ".git", "web/node_modules"} {
		if _, err := os.Stat(filepath.Join(root, name)); !os.IsNotExist(err) {
			t.Fatalf("unexpected private path: %s", name)
		}
	}
}
