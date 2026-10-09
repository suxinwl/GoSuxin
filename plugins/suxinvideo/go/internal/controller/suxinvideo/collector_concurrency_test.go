package suxinvideo

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "github.com/suxinwl/GoSuxin/framework/contrib/drivers/mysql"
	"github.com/suxinwl/GoSuxin/framework/database/gdb"
	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/util/gconv"
)

func TestCollectorConcurrentDiscoveryKeepsBothLines(t *testing.T) {
	if os.Getenv("SUXIN_INTEGRATION") != "1" {
		t.Skip("set SUXIN_INTEGRATION=1 for isolated concurrent database updates")
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chdir(filepath.Clean("../../..")); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(wd)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	insert, err := g.DB().Exec(ctx, "INSERT INTO sx_vod(name,name_norm,status,play_from,play_url,pic,remarks,vip,points,addtime,updatetime) VALUES(?,?,1,'base',?,'/original.jpg','old remarks',1,9,1,2)", fmt.Sprintf("collector-lock-test-%d", time.Now().UnixNano()), "lock-fixture", "第01集$https://example.test/base.m3u8")
	if err != nil {
		t.Fatal(err)
	}
	id, _ := insert.LastInsertId()
	defer execSQL(context.Background(), "DELETE FROM sx_vod WHERE id=?", id)
	stale, err := one(ctx, "SELECT id,api_id,api_vid,play_from,play_url,pic,remarks FROM sx_vod WHERE id=?", id)
	if err != nil {
		t.Fatal(err)
	}
	locked, release := make(chan struct{}), make(chan struct{})
	discoveryDone, collectionDone := make(chan error, 1), make(chan error, 1)
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	go func() {
		discoveryDone <- g.DB().Transaction(ctx, func(ctx context.Context, tx gdb.TX) error {
			if _, err := tx.GetOne("SELECT id FROM sx_vod WHERE id=? FOR UPDATE", id); err != nil {
				return err
			}
			close(locked)
			select {
			case <-release:
			case <-ctx.Done():
				return ctx.Err()
			}
			_, err := tx.Exec("UPDATE sx_vod SET play_from='base$$$discovered',play_url=?,pic='/concurrent.jpg',remarks='fresh remarks' WHERE id=?", "第01集$https://example.test/base.m3u8$$$第1集$https://example.test/discovered.m3u8", id)
			return err
		})
	}()
	select {
	case <-locked:
	case err := <-discoveryDone:
		t.Fatalf("could not hold discovery row: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	go func() {
		collectionDone <- updateCollectedVod(ctx, stale, 0, "", "collected", "第01集$https://example.test/collected.m3u8", "", "")
	}()
	select {
	case err := <-collectionDone:
		t.Fatalf("collector wrote through another transaction's lock: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	if err = <-discoveryDone; err != nil {
		t.Fatal(err)
	}
	if err = <-collectionDone; err != nil {
		t.Fatal(err)
	}
	final, err := one(ctx, "SELECT * FROM sx_vod WHERE id=?", id)
	if err != nil {
		t.Fatal(err)
	}
	if gconv.String(final["play_from"]) != "base$$$discovered$$$collected" || !strings.Contains(gconv.String(final["play_url"]), "discovered.m3u8") || !strings.Contains(gconv.String(final["play_url"]), "collected.m3u8") {
		t.Fatalf("stale collector snapshot lost a concurrently discovered line: %s", final["play_from"])
	}
	if gconv.String(final["pic"]) != "/concurrent.jpg" || gconv.String(final["remarks"]) != "fresh remarks" || gconv.Int(final["vip"]) != 1 || gconv.Int(final["points"]) != 9 {
		t.Fatal("collector clobbered newer cover/remarks or access settings")
	}
}
