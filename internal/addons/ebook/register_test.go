package ebook

import (
	"context"
	"os"
	"testing"

	"github.com/suxinwl/GoSuxin/framework/net/ghttp"
)

func TestUninstalledHostDoesNotRequireDatabase(t *testing.T) {
	t.Setenv("SUXIN_PLUGIN_PACKAGE", "")
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })
	stop, err := start(context.Background(), ghttp.GetServer("ebook-install-test"))
	if err != nil {
		t.Fatalf("host installer must start without a database: %v", err)
	}
	if stop == nil {
		t.Fatal("missing cleanup callback")
	}
	stop()
	if _, err := os.Stat("storage"); !os.IsNotExist(err) {
		t.Fatalf("uninstalled plugin created storage: %v", err)
	}
}
