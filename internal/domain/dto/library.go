package dto

import (
	"Tefnut/common/defines"
	"Tefnut/internal/domain/entity"
)

type LibraryListRequest struct {
	ParentId int `json:"parentId"`
	LastId   int `json:"lastId"`
	Limit    int `json:"limit"`
}

func (req *LibraryListRequest) ToQuery() *entity.LibraryQuery {
	query := &entity.LibraryQuery{
		ParentId: req.ParentId,
		LastId:   req.LastId,
		Limit:    req.Limit,
	}
	if query.Limit == 0 {
		query.Limit = defines.DefaultLimit
	}
	return query
}

type LibraryListResponse struct {
	Limit int                 `json:"limit"`
	Total int                 `json:"total"`
	Data  entity.FileItemList `json:"data"`
}

type ContentResponse struct {
	TmpName string   `json:"url"`
	Files   []string `json:"files"`
}
