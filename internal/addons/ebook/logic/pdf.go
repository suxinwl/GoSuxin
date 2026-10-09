package album

import (
	"context"
	"fmt"
	"github.com/suxinwl/GoSuxin/framework/database/gdb"
	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/os/gtime"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

var pdfSlots = make(chan struct{}, 2)

func pageNumber(name string) int {
	if !strings.HasPrefix(name, "page-") || !strings.HasSuffix(name, ".jpg") {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(name, "page-"), ".jpg"))
	return n
}

func ConvertPDF(id int64, source string) {
	assetLifecycle.RLock()
	defer func() { assetLifecycle.RUnlock(); flushAssetDeletesSoon(id) }()
	ctx := context.Background()
	fail := func(cause error) {
		reason := []rune(cause.Error())
		if len(reason) > 480 {
			reason = reason[:480]
		}
		_, err := g.Model("album").Ctx(ctx).Where("id", id).Where("status", "processing").Where("pending_pdf", source).Data(g.Map{"status": "failed", "failure_reason": string(reason), "updatetime": gtime.Now()}).Update()
		if err != nil {
			g.Log().Error(ctx, "record PDF conversion failure", err)
		}
	}
	seconds := g.Cfg("app").MustGet(ctx, "album.pdfTimeoutSeconds", 300).Int()
	if seconds < 1 {
		seconds = 300
	}
	jobCtx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	waitCtx, waitCancel := context.WithTimeout(jobCtx, time.Duration(seconds)*time.Second)
	defer waitCancel()
	select {
	case pdfSlots <- struct{}{}:
		defer func() { <-pdfSlots }()
	case <-waitCtx.Done():
		fail(fmt.Errorf("PDF 等待转换超时"))
		return
	}
	job, err := g.Model("album").Ctx(ctx).Where("id", id).One()
	if err != nil || job.IsEmpty() {
		fail(fmt.Errorf("转换任务不存在"))
		return
	}
	target := StorageProfile{}
	if job["storage_profile_id"].String() != "" {
		target, err = storageProfile(job["storage_profile_id"].String())
		if err != nil {
			fail(err)
			return
		}
		target.ParentID = job["storage_parent_id"].Int64()
	}
	stage := func(value string) {
		_, _ = g.Model("album").Ctx(ctx).Where("id", id).Where("status", "processing").Where("pending_pdf", source).Data(g.Map{"processing_stage": value}).Update()
	}
	stage("source_upload")
	original := source
	if !strings.HasPrefix(source, assetPrefix) {
		original, err = PutAsset(jobCtx, id, source, target)
		if err != nil {
			fail(err)
			return
		}
	}
	pdfPath, cleanupSource, err := materializeAsset(jobCtx, source)
	if err != nil {
		fail(err)
		return
	}
	defer cleanupSource()
	key, err := NewKey()
	if err != nil {
		fail(err)
		return
	}
	rel := fmt.Sprintf("%d/%s", id, key)
	outputDir := filepath.Join("storage", "albums", filepath.FromSlash(rel))
	if err := os.MkdirAll(outputDir, 0700); err != nil {
		fail(err)
		return
	}
	committed := false
	defer func() {
		// outputDir is a new, randomly named directory owned only by this job.
		if !committed || target.ID != "" {
			if err := os.RemoveAll(outputDir); err != nil {
				g.Log().Error(ctx, "clean failed PDF output", err)
			}
		}
	}()
	bin := g.Cfg("app").MustGet(ctx, "album.pdfToPpmBin", defaultPDFBinary()).String()
	if configured := os.Getenv("ALBUM_PDFTOPPM_BIN"); configured != "" {
		bin = configured
	}
	// Render at most one extra page to detect oversized documents.
	maxPages := g.Cfg("app").MustGet(ctx, "album.pdfMaxPages", 500).Int()
	if maxPages < 1 {
		maxPages = 500
	}
	stage("converting")
	convertCtx, convertCancel := context.WithTimeout(jobCtx, time.Duration(seconds)*time.Second)
	defer convertCancel()
	output, err := exec.CommandContext(convertCtx, bin, "-jpeg", "-r", "150", "-f", "1", "-l", strconv.Itoa(maxPages+1), pdfPath, filepath.Join(outputDir, "page")).CombinedOutput()
	if convertCtx.Err() != nil {
		fail(fmt.Errorf("PDF 转换超时（%d 秒）", seconds))
		return
	}
	if err != nil {
		fail(fmt.Errorf("PDF 转换失败：%v %s", err, strings.TrimSpace(string(output))))
		return
	}
	entries, err := os.ReadDir(outputDir)
	if err != nil {
		fail(err)
		return
	}
	var names []string
	for _, entry := range entries {
		if !entry.IsDir() && pageNumber(entry.Name()) > 0 {
			names = append(names, entry.Name())
		}
	}
	sort.Slice(names, func(i, j int) bool { return pageNumber(names[i]) < pageNumber(names[j]) })
	if len(names) == 0 {
		fail(fmt.Errorf("PDF 转换未生成页面"))
		return
	}
	if len(names) > maxPages {
		fail(fmt.Errorf("PDF 超过 %d 页限制", maxPages))
		return
	}
	stage("page_upload")
	locations := make([]string, len(names))
	sizes := make([]int64, len(names))
	for i, name := range names {
		info, e := os.Stat(filepath.Join(outputDir, name))
		if e != nil {
			fail(e)
			return
		}
		sizes[i] = info.Size()
		locations[i], err = PutAsset(jobCtx, id, "/_album/"+rel+"/"+name, target)
		if err != nil {
			fail(err)
			return
		}
	}
	err = g.DB().Transaction(ctx, func(ctx context.Context, tx gdb.TX) error {
		row, err := tx.Model("album").Where("id", id).LockUpdate().One()
		if err != nil {
			return err
		}
		if row.IsEmpty() || row["status"].String() != "processing" || row["pending_pdf"].String() != source {
			return fmt.Errorf("转换任务已失效")
		}
		previousPages, err := tx.Model("album_page").Where("album_id", id).All()
		if err != nil {
			return err
		}
		if err = queueAssetDeletes(ctx, tx, id, pageLocations(previousPages)); err != nil {
			return err
		}
		if _, err := tx.Model("album_page").Where("album_id", id).Delete(); err != nil {
			return err
		}
		for i, location := range locations {
			_, err = tx.Model("album_page").Data(g.Map{"album_id": id, "page_no": i + 1, "image_url": location, "thumbnail_url": location, "size_bytes": sizes[i], "createtime": gtime.Now()}).Insert()
			if err != nil {
				return err
			}
		}
		_, err = tx.Model("album").Where("id", id).Data(g.Map{"status": "ready", "original_url": original, "pending_pdf": "", "processing_stage": "", "page_count": len(names), "cover_url": locations[0], "failure_reason": "", "updatetime": gtime.Now()}).Update()
		return err
	})
	if err != nil {
		fail(err)
	} else {
		committed = true
		if target.ID != "" && !strings.HasPrefix(source, assetPrefix) {
			cleanupTemporary(source)
		}
	}
}

func defaultPDFBinary() string {
	for _, candidate := range []string{"tools/poppler/Library/bin/pdftoppm.exe", "tools/poppler/bin/pdftoppm"} {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			absolute, _ := filepath.Abs(candidate)
			return absolute
		}
	}
	return "pdftoppm"
}
