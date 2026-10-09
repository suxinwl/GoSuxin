package admin

import (
	"context"
	"github.com/suxinwl/GoSuxin/framework/frame/g"
	api "github.com/suxinwl/GoSuxin/internal/addons/ebook/api/admin"
	album "github.com/suxinwl/GoSuxin/internal/addons/ebook/logic"
	"github.com/suxinwl/GoSuxin/utility/gf"
)

func (c *ControllerAlbum) StorageGet(ctx context.Context, req *api.StorageGetReq) (*api.StorageGetRes, error) {
	data, err := album.StorageSettings()
	if err != nil {
		return &api.StorageGetRes{R: gf.Failed().SetMsg(err.Error())}, nil
	}
	return &api.StorageGetRes{R: gf.Success().SetData(data)}, nil
}
func (c *ControllerAlbum) StorageSave(ctx context.Context, req *api.StorageSaveReq) (*api.StorageSaveRes, error) {
	id, err := album.SaveStorageProfile(album.StorageProfile{ID: req.ID, Name: req.Name, ClientID: req.ClientID, ClientSecret: req.ClientSecret, ParentID: req.ParentID, EnglishParentID: req.EnglishParentID, CDNKey: req.CDNKey, URLAuth: req.URLAuth})
	if err != nil {
		return &api.StorageSaveRes{R: gf.Failed().SetMsg(err.Error())}, nil
	}
	return &api.StorageSaveRes{R: gf.Success().SetData(g.Map{"id": id})}, nil
}
func (c *ControllerAlbum) StorageTest(ctx context.Context, req *api.StorageTestReq) (*api.StorageTestRes, error) {
	if err := album.CheckStorage(ctx, req.ID); err != nil {
		return &api.StorageTestRes{R: gf.Failed().SetMsg(err.Error())}, nil
	}
	return &api.StorageTestRes{R: gf.Success().SetData(g.Map{"verified": true})}, nil
}
func (c *ControllerAlbum) StorageActivate(ctx context.Context, req *api.StorageActivateReq) (*api.StorageActivateRes, error) {
	if err := album.ActivateStorage(req.ID); err != nil {
		return &api.StorageActivateRes{R: gf.Failed().SetMsg(err.Error())}, nil
	}
	return &api.StorageActivateRes{R: gf.Success().SetData(nil)}, nil
}
