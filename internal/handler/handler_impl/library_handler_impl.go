package handler_impl

import (
	"Tefnut/internal/domain/dto"
	"context"
)

func (impl *TefnutHandlerImpl) LibraryList(ctx context.Context, req *dto.LibraryListRequest) (*dto.LibraryListResponse, error) {
	resp := &dto.LibraryListResponse{
		Limit: req.Limit,
	}
	list, total, err := impl.libService.Query(ctx, req.ToQuery())
	if err != nil {
		return nil, err
	}
	resp.Total = total
	resp.Data = list
	return resp, nil
}

func (impl *TefnutHandlerImpl) LibraryContentGet(ctx context.Context, id int) (*dto.ContentResponse, error) {
	resp := &dto.ContentResponse{}
	tmpName, list, err := impl.libService.GetContent(ctx, id)
	if err != nil {
		return nil, err
	}
	resp.Files = list
	resp.TmpName = tmpName
	return resp, nil
}
