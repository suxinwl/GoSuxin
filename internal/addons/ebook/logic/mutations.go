package album

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/suxinwl/GoSuxin/framework/database/gdb"
	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/net/ghttp"
	"github.com/suxinwl/GoSuxin/framework/os/gtime"
)

func lockedAlbum(ctx context.Context, tx gdb.TX, id int64) (gdb.Record, error) {
	// Acquire the album lock before any consistent read starts a MySQL snapshot.
	// Otherwise concurrent uploads can calculate the same page number from an
	// older REPEATABLE READ snapshot even after waiting for the album lock.
	row, err := tx.Model("album").Where("id", id).LockUpdate().One()
	if err != nil {
		return nil, err
	}
	if row.IsEmpty() || (row["owner_id"].Int64() != OwnerID(ctx) && !CanManageAll(ctx)) {
		return nil, errors.New("画册不存在或无权操作")
	}
	if row["status"].String() == "processing" {
		return nil, errors.New("PDF 正在转换，请等待完成后再操作")
	}
	return row, nil
}

func savePrivateUpload(id int64, file *ghttp.UploadFile, pdf bool) (string, error) {
	if file == nil || file.FileHeader == nil {
		return "", errors.New("请选择文件")
	}
	stream, err := file.Open()
	if err != nil {
		return "", err
	}
	header := make([]byte, 512)
	n, readErr := stream.Read(header)
	stream.Close()
	if readErr != nil && readErr != io.EOF {
		return "", readErr
	}
	ext := strings.ToLower(filepath.Ext(file.Filename))
	if pdf {
		if ext != ".pdf" || !strings.HasPrefix(string(header[:n]), "%PDF-") {
			return "", errors.New("请选择有效的 PDF 文件")
		}
	} else {
		if _, err := NormalizeImageName(file.Filename); err != nil {
			return "", err
		}
		mime := http.DetectContentType(header[:n])
		allowed := map[string]string{".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".png": "image/png", ".webp": "image/webp"}
		if mime != allowed[ext] {
			return "", errors.New("图片内容与格式不符")
		}
	}
	key, err := NewKey()
	if err != nil {
		return "", err
	}
	rel := fmt.Sprintf("%d/%s", id, key)
	dir := filepath.Join("storage", "albums", filepath.FromSlash(rel))
	file.Filename = "source" + ext
	name, err := file.Save(dir)
	if err != nil {
		return "", err
	}
	return "/_album/" + rel + "/" + name, nil
}

func AddImage(ctx context.Context, id int64, file *ghttp.UploadFile, pageNo int) (int, error) {
	assetLifecycle.RLock()
	defer assetLifecycle.RUnlock()
	version, err := prepareImageEdit(ctx, id)
	if err != nil {
		return 0, err
	}
	target, err := AlbumStorageTarget(ctx, id)
	if err != nil {
		return 0, err
	}
	temporary, err := savePrivateUpload(id, file, false)
	if err != nil {
		return 0, err
	}
	location, err := stageImage(ctx, id, temporary, target)
	if err != nil {
		return 0, err
	}
	err = g.DB().Transaction(ctx, func(ctx context.Context, tx gdb.TX) error {
		row, err := EditableAlbum(ctx, tx, id)
		if err != nil {
			return err
		}
		if row["edit_version"].Int64() != version {
			return errors.New("画册已变化，请刷新后重新上传")
		}
		if pageNo <= 0 {
			max, err := tx.Model("album_page").Where("album_id", id).Max("page_no")
			if err != nil {
				return err
			}
			pageNo = int(max) + 1
		} else {
			count, err := tx.Model("album_page").Where("album_id", id).Where("page_no", pageNo).Count()
			if err != nil {
				return err
			}
			if count > 0 {
				return errors.New("页码已存在，请使用替换图片操作")
			}
		}
		_, err = tx.Model("album_page").Data(g.Map{"album_id": id, "page_no": pageNo, "image_url": location, "thumbnail_url": location, "size_bytes": file.Size, "createtime": gtime.Now()}).Save()
		if err != nil {
			return err
		}
		count, err := tx.Model("album_page").Where("album_id", id).Count()
		if err != nil {
			return err
		}
		first, err := tx.Model("album_page").Where("album_id", id).Order("page_no ASC").One()
		if err != nil {
			return err
		}
		cover := row["cover_url"].String()
		if cover == "" {
			cover = first["image_url"].String()
		}
		_, err = tx.Model("album").Where("id", id).Data(g.Map{"source_type": "images", "original_url": "", "pending_pdf": "", "processing_stage": "", "status": "ready", "failure_reason": "", "page_count": count, "cover_url": cover, "updatetime": gtime.Now()}).Update()
		return err
	})
	if err == nil && target.ID != "" {
		cleanupTemporary(temporary)
	}
	return pageNo, err
}

func prepareImageEdit(ctx context.Context, id int64) (int64, error) {
	var version int64
	err := g.DB().Transaction(ctx, func(ctx context.Context, tx gdb.TX) error {
		row, e := EditableAlbum(ctx, tx, id)
		if e != nil {
			return e
		}
		version = row["edit_version"].Int64()
		return nil
	})
	return version, err
}
func SubmitPDF(ctx context.Context, id int64, file *ghttp.UploadFile, retry bool) error {
	var location string
	version, err := prepareImageEdit(ctx, id)
	if err != nil {
		return err
	}
	target := StorageProfile{}
	if !retry {
		target, err = AlbumStorageTarget(ctx, id)
		if err != nil {
			return err
		}
	}
	if !retry {
		location, err = savePrivateUpload(id, file, true)
		if err != nil {
			return err
		}
	}
	err = g.DB().Transaction(ctx, func(ctx context.Context, tx gdb.TX) error {
		row, err := EditableAlbum(ctx, tx, id)
		if err != nil {
			return err
		}
		if row["edit_version"].Int64() != version {
			return errors.New("画册已变化，请重新操作")
		}
		if retry {
			if row["status"].String() != "failed" || row["source_type"].String() != "pdf" {
				return errors.New("仅可重试转换失败的 PDF")
			}
			location = row["pending_pdf"].String()
			if location == "" {
				location = row["original_url"].String()
			}
			if location == "" {
				return errors.New("原 PDF 不存在，请重新上传")
			}
			target = StorageProfile{ID: row["storage_profile_id"].String(), ParentID: row["storage_parent_id"].Int64()}
		}
		_, err = tx.Model("album").Where("id", id).Data(g.Map{"source_type": "pdf", "pending_pdf": location, "status": "processing", "processing_stage": "source_upload", "storage_profile_id": target.ID, "storage_parent_id": target.ParentID, "failure_reason": "", "edit_version": gdb.Raw("edit_version+1"), "updatetime": gtime.Now()}).Update()
		return err
	})
	if err == nil {
		go ConvertPDF(id, location)
	}
	return err
}

func CanPublish(status string, pages int) bool {
	return pages > 0 && (status == "ready" || status == "offline" || status == "published")
}

func SetPublished(ctx context.Context, id int64, publish bool) error {
	return g.DB().Transaction(ctx, func(ctx context.Context, tx gdb.TX) error {
		row, err := lockedAlbum(ctx, tx, id)
		if err != nil {
			return err
		}
		data := g.Map{"status": "offline", "updatetime": gtime.Now(), "edit_version": gdb.Raw("edit_version+1")}
		if !publish && row["status"].String() != "published" {
			return errors.New("仅已发布画册可以下架")
		}
		if publish {
			count, err := tx.Model("album_page").Where("album_id", id).Count()
			if err != nil {
				return err
			}
			if !CanPublish(row["status"].String(), count) {
				return errors.New("画册尚未准备完成，请上传页面或重试 PDF 转换")
			}
			data["status"] = "published"
			data["published_at"] = gtime.Now()
			data["page_count"] = count
		}
		_, err = tx.Model("album").Where("id", id).Data(data).Update()
		return err
	})
}

func Initialize(ctx context.Context) error {
	if err := EnsureSchema(ctx); err != nil {
		return err
	}
	if err := os.MkdirAll("storage/albums", 0700); err != nil {
		return err
	}
	// One API process per deployment: interrupted work retains its source for retry.
	_, err := g.Model("album").Ctx(ctx).Where("status", "processing").Data(g.Map{"status": "failed", "failure_reason": "服务重启中断了转换，请点击重试", "updatetime": gtime.Now()}).Update()
	return err
}
