package repository

import (
	"Tefnut/internal/domain/entity"
	"context"
)

type LibraryRepository interface {
	CreateNode(ctx context.Context, item *entity.FileItem) (*entity.FileItem, error)
	ListChildNodes(ctx context.Context, parentId int) (entity.FileItemList, error)
	DeleteNode(ctx context.Context, id int) error
	GetNode(ctx context.Context, id int) (*entity.FileItem, error)
	Query(ctx context.Context, condition *entity.LibraryQuery) (entity.FileItemList, int, error)
}
