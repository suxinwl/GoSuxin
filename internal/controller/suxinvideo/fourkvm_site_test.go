package suxinvideo

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	xq "github.com/suxinwl/GoSuxin/internal/xiaoqiapp"
)

func isolateFourKVMSiteState(t *testing.T) {
	t.Helper()
	fourKVMSiteState.Lock()
	loaded, refreshing, next, snapshot := fourKVMSiteState.loaded, fourKVMSiteState.refreshing, fourKVMSiteState.nextRefresh, fourKVMSiteState.snapshot
	fourKVMSiteState.loaded, fourKVMSiteState.refreshing, fourKVMSiteState.nextRefresh, fourKVMSiteState.snapshot = false, false, time.Time{}, fourKVMSiteHomeSnapshot{}
	fourKVMSiteState.Unlock()
	oldPath, oldFetch := fourKVMSiteSnapshotPath, fetchFourKVMSiteHome
	fourKVMSiteSnapshotPath = filepath.Join(t.TempDir(), "4kvm-site.json")
	t.Cleanup(func() {
		fourKVMSiteSnapshotPath, fetchFourKVMSiteHome = oldPath, oldFetch
		fourKVMSiteState.Lock()
		fourKVMSiteState.loaded, fourKVMSiteState.refreshing, fourKVMSiteState.nextRefresh, fourKVMSiteState.snapshot = loaded, refreshing, next, snapshot
		fourKVMSiteState.Unlock()
	})
}

func fourKVMSiteHomeFixture() xq.CMSHome {
	drama := xq.Drama{Source: "4kvm", SourceID: "movie-one", Title: "Fixture movie", Cover: "https://images.example/poster.jpg"}
	return xq.CMSHome{
		Banners:  []xq.CMSHomeBanner{{Drama: drama, ImageURL: "https://images.example/wide.jpg"}},
		Sections: []xq.CMSHomeSection{{Key: "recent", Title: "Recent", Dramas: []xq.Drama{drama}}},
	}
}

func TestFourKVMSiteColdSnapshotDoesNotWaitAndCoalesces(t *testing.T) {
	isolateFourKVMSiteState(t)
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	fetchFourKVMSiteHome = func(ctx context.Context) (xq.CMSHome, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		select {
		case <-release:
			return fourKVMSiteHomeFixture(), nil
		case <-ctx.Done():
			return xq.CMSHome{}, ctx.Err()
		}
	}
	if snapshot := fourKVMSiteSnapshot(context.Background()); snapshot.Updated != 0 {
		t.Fatal("cold render unexpectedly waited for upstream data")
	}
	<-started
	for range 20 {
		fourKVMSiteSnapshot(context.Background())
	}
	if calls.Load() != 1 {
		t.Fatal("concurrent homepage reads started multiple refreshes")
	}
	close(release)
	deadline := time.Now().Add(2 * time.Second)
	for {
		fourKVMSiteState.Lock()
		complete := !fourKVMSiteState.refreshing
		fourKVMSiteState.Unlock()
		if complete {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("shared refresh did not complete")
		}
		time.Sleep(time.Millisecond)
	}
	snapshot := fourKVMSiteSnapshot(context.Background())
	if snapshot.Updated == 0 || len(snapshot.Home.Banners) != 1 || snapshot.Home.Banners[0].ImageURL == snapshot.Home.Banners[0].Drama.Cover {
		t.Fatal("snapshot lost provider data or replaced landscape artwork with poster")
	}
}

func TestFourKVMSiteRefreshFailureKeepsLastGoodSnapshot(t *testing.T) {
	for _, failure := range []error{errors.New("upstream timeout"), nil} {
		t.Run(map[bool]string{true: "error", false: "empty"}[failure != nil], func(t *testing.T) {
			isolateFourKVMSiteState(t)
			previous := fourKVMSiteHomeSnapshot{Updated: 123, Home: fourKVMSiteHomeFixture()}
			if err := saveFourKVMSiteSnapshot(previous); err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadFile(fourKVMSiteSnapshotPath)
			fourKVMSiteState.snapshot = previous
			fetchFourKVMSiteHome = func(context.Context) (xq.CMSHome, error) { return xq.CMSHome{}, failure }
			refreshFourKVMSiteHome()
			after, _ := os.ReadFile(fourKVMSiteSnapshotPath)
			if string(before) != string(after) || fourKVMSiteState.snapshot.Updated != 123 {
				t.Fatal("failed upstream refresh destroyed the usable disk or memory snapshot")
			}
		})
	}
}

func TestFourKVMSiteLiveWarmSnapshot(t *testing.T) {
	path := os.Getenv("SUXIN_FOURKVM_SNAPSHOT_PATH")
	if path == "" {
		t.Skip("optional live provider snapshot preparation")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	home, err := cmsProviders.Home(ctx, "4kvm")
	if err != nil {
		t.Fatal(err)
	}
	if len(home.Banners) == 0 || len(home.Sections) == 0 {
		t.Fatal("provider did not return banner and recommendations")
	}
	snapshot := fourKVMSiteHomeSnapshot{Updated: time.Now().Unix(), Home: home}
	data, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	t.Logf("prepared %d real banners and %d recommendation sections", len(home.Banners), len(home.Sections))
}
