package repository

import (
	"Tefnut/internal/domain/entity"
	"context"
)

type LibraryRepository interface {
	CreateNode(ctx context.Context, item *entity.FileItem) (*entity.FileItem, error)
	ListChildNodes(ctx context.Context, parentId int) (entity.FileItemList, error)
	DeleteNode(ctx context.Context, id int) error
}
