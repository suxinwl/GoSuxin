package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func (d *Downloader) fetchAllDramas(ctx context.Context, sourceFilter string) ([]Drama, error) {
	canonFilter := canonicalProviderSource(sourceFilter)
	if sourceFilter != "" && canonFilter == "" {
		return nil, errors.New("不支持该站源")
	}
	if more, _ := ctx.Value(libraryMoreKey{}).(bool); more {
		return d.fetchMoreLibrary(ctx, sourceFilter)
	}

	var targets []string
	if canonFilter != "" {
		targets = append(targets, canonFilter)
	} else {
		for _, choice := range accountSourceChoices {
			targets = append(targets, choice.ID)
		}
	}

	var allDramas []Drama
	failures := make(map[string]error)
	for _, src := range targets {
		select {
		case <-ctx.Done():
			if len(allDramas) > 0 {
				return allDramas, nil
			}
			return allDramas, ctx.Err()
		default:
		}

		if src == sourceHongguo {
			fmt.Println("正在同步红果片源目录...")
			ctxSub, cancelSub := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Minute)
			dramas, err := d.fetchHongguoDramas(ctxSub)
			cancelSub()
			dramas = onlyHongguoDramas(dramas)
			if len(dramas) == 0 && err == nil && !hongguoCatalogInitialized(d.hongguoCatalogSnapshot()) {
				err = errors.New("未返回可识别的视频数据")
			}
			reportLibraryProgress(ctx, sourceHongguo, dramas, err, true)
			if err != nil {
				failures[sourceHongguo] = err
				fmt.Printf(" 红果获取失败: %v\n", publicError(err))
			} else {
				allDramas = append(allDramas, dramas...)
				fmt.Printf(" 红果片源同步完成：%d 部\n", len(dramas))
			}
		} else if isMacCMSSource(src) {
			def := defaultMacCMSSources[src]
			fmt.Printf("正在获取%s剧库列表...\n", def.Name)
			ctxSub, cancelSub := context.WithTimeout(context.WithoutCancel(ctx), 8*time.Minute)
			dramas, err := d.fetchMacCMSDramas(ctxSub, src)
			cancelSub()
			reportLibraryProgress(ctx, src, dramas, err, true)
			if err != nil {
				failures[src] = err
				fmt.Printf(" %s获取失败: %v\n", def.Name, publicError(err))
			} else {
				allDramas = append(allDramas, dramas...)
				fmt.Printf(" %s剧库获取完成：%d 部\n", def.Name, len(dramas))
			}
		} else if src == sourceHuangdou {
			fmt.Println("正在获取黄豆剧库列表...")
			ctxSub, cancelSub := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Minute)
			dramas, err := d.fetchHuangdouDramas(ctxSub)
			cancelSub()
			reportLibraryProgress(ctx, sourceHuangdou, dramas, err, true)
			if err != nil {
				failures[sourceHuangdou] = err
				fmt.Printf(" 黄豆获取失败: %v\n", publicError(err))
			} else {
				allDramas = append(allDramas, dramas...)
				fmt.Printf(" 黄豆剧库获取完成：%d 部\n", len(dramas))
			}
		} else if src == source4KVM {
			fmt.Println("正在获取4KVM剧库列表...")
			ctxSub, cancelSub := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Minute)
			dramas, err := d.fetch4KVMDramas(ctxSub)
			cancelSub()
			reportLibraryProgress(ctx, source4KVM, dramas, err, true)
			if err != nil {
				failures[source4KVM] = err
				fmt.Printf(" 4KVM获取失败: %v\n", publicError(err))
			} else {
				allDramas = append(allDramas, dramas...)
				fmt.Printf(" 4KVM剧库获取完成：%d 部\n", len(dramas))
			}
		} else if src == "huangguo" || src == sourceHuangguoAI || src == sourceHuangguoVideo {
			fmt.Println("正在获取黄果剧库列表...")
			ctxSub, cancelSub := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Minute)
			var hgDramas []Drama
			dramasAI, errAI := d.fetchHuangguoAIDramas(ctxSub)
			if errAI != nil {
				fmt.Printf(" 黄果AI获取失败: %v\n", publicError(errAI))
			} else {
				hgDramas = append(hgDramas, dramasAI...)
			}
			dramasVid, errVid := d.fetchHuangguoVideoDramas(ctxSub)
			if errVid != nil {
				fmt.Printf(" 黄果视频获取失败: %v\n", publicError(errVid))
			} else {
				hgDramas = append(hgDramas, dramasVid...)
			}
			cancelSub()
			var hgErr error
			if len(hgDramas) == 0 {
				if errAI != nil {
					hgErr = errAI
				} else {
					hgErr = errVid
				}
			}
			reportLibraryProgress(ctx, "huangguo", hgDramas, hgErr, true)
			if hgErr != nil {
				failures["huangguo"] = hgErr
			} else {
				allDramas = append(allDramas, hgDramas...)
				fmt.Printf(" 黄果剧库获取完成：%d 部\n", len(hgDramas))
			}
		}
	}

	if len(failures) > 0 {
		return allDramas, &libraryLoadError{failures: failures}
	}
	return allDramas, nil
}

func (d *Downloader) GetDramaChapters(ctx context.Context, seriesID string) (string, []Chapter, error) {
	source, sourceID, valid := splitProviderDramaID(seriesID)
	if !valid {
		return "", nil, errors.New("无效的剧集 ID")
	}
	if source == sourceHongguo {
		title, chapters, err := d.fetchHongguoChapters(ctx, sourceID)
		return title, uniqueChapters(chapters), err
	}
	if source == sourceHuangdou {
		title, chapters, err := d.fetchHuangdouChapters(ctx, sourceID)
		return title, uniqueChapters(chapters), err
	}
	if source == source4KVM {
		title, chapters, err := d.fetch4KVMChapters(ctx, sourceID)
		return title, uniqueChapters(chapters), err
	}
	if source == sourceHuangguoAI {
		title, chapters, err := d.fetchHuangguoAIChapters(ctx, sourceID)
		return title, uniqueChapters(chapters), err
	}
	if source == sourceHuangguoVideo {
		title, chapters, err := d.fetchHuangguoVideoChapters(ctx, sourceID)
		return title, uniqueChapters(chapters), err
	}
	if isMacCMSSource(source) {
		title, chapters, err := d.fetchMacCMSChapters(ctx, source, sourceID)
		return title, uniqueChapters(chapters), err
	}
	return "", nil, fmt.Errorf("不支持的站源: %s", source)
}

func uniqueChapters(chapters []Chapter) []Chapter {
	seen := map[string]bool{}
	var out []Chapter
	for i, ch := range chapters {
		key := ch.ID
		if key == "" {
			key = ch.VideoURL
		}
		if key == "" {
			key = fmt.Sprintf("idx_%d", i)
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, ch)
	}
	return out
}

func (d *Downloader) BuildDramaTasks(ctx context.Context, drama Drama) ([]Task, error) {
	return d.buildDramaTasksInDirectory(ctx, drama, "")
}

func (d *Downloader) buildDramaTasksInDirectory(ctx context.Context, drama Drama, existingDirectory string) ([]Task, error) {
	var valid bool
	drama, valid = normalizeDrama(drama)
	if !valid {
		return nil, errors.New("无效的剧集信息")
	}
	title, chapters, err := d.GetDramaChapters(ctx, drama.ID)
	if err != nil {
		return nil, err
	}
	if len(chapters) == 0 {
		return nil, nil
	}
	if drama.Source == sourceHongguo {
		drama = d.hongguoCachedDrama(drama)
	}
	safeDrama := safeFilename(title)
	dramaDir, err := safeJoin(d.cfg.OutputDir, safeDrama)
	if err != nil {
		return nil, err
	}
	if existingDirectory != "" {
		dramaDir = existingDirectory
	}
	markerPath := filepath.Join(dramaDir, ".drama-id")
	if b, readErr := os.ReadFile(markerPath); readErr == nil && strings.TrimSpace(string(b)) != "" && strings.TrimSpace(string(b)) != drama.ID {
		dramaDir, err = safeJoin(d.cfg.OutputDir, safeFilename(fmt.Sprintf("%s_%s", title, hashShort(drama.ID))))
		if err != nil {
			return nil, err
		}
		markerPath = filepath.Join(dramaDir, ".drama-id")
	}
	if err := os.MkdirAll(dramaDir, 0o755); err != nil {
		return nil, err
	}
	if drama.ID != "" {
		_ = os.WriteFile(markerPath, []byte(drama.ID), 0o644)
	}
	var tasks []Task
	for i, ch := range chapters {
		ep := ch.EpisodeString(i + 1)
		epNum := padEpisode(ep)
		fileName := safeFilename(epNum + ".mp4")
		outPath, err := safeJoin(dramaDir, fileName)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, Task{DramaID: drama.ID, DramaTitle: title, Chapter: ch, Index: i + 1, Total: len(chapters), OutPath: outPath, ReleaseStatus: dramaReleaseStatus(drama)})
	}
	return tasks, nil
}
