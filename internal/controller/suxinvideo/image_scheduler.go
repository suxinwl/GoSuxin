package suxinvideo

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/util/gconv"
)

func startImageCleanupScheduler(parent context.Context) {
	go func() {
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			ctx, cancel := context.WithTimeout(parent, 3*time.Minute)
			if err := scheduledImageCleanup(ctx); err != nil {
				g.Log().Warning(ctx, "CMS image cleanup:", err)
			}
			cancel()
			select {
			case <-parent.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func scheduledImageCleanup(ctx context.Context) error {
	if setting(ctx, "img_auto_clean_enable", "0") != "1" {
		return nil
	}
	if time.Now().Unix()-gconv.Int64(setting(ctx, "img_clean_last", "0")) < 86400 {
		return nil
	}
	used, err := imageReferences(ctx)
	if err != nil {
		return err
	}
	files, err := listCachedImages()
	if err != nil {
		return err
	}
	deleted := 0
	for name := range files {
		if !used[name] {
			if err := os.Remove(filepath.Join(cacheImageDir(), name)); err != nil && !os.IsNotExist(err) {
				return err
			}
			deleted++
		}
	}
	return execSQL(ctx, "INSERT INTO sx_config(`key`,`value`) VALUES('img_clean_last',?),('img_clean_last_result',?) ON DUPLICATE KEY UPDATE `value`=VALUES(`value`)", strconv.FormatInt(time.Now().Unix(), 10), strconv.Itoa(deleted))
}
