package service

import (
	"Tefnut/internal/domain/entity"
	"context"
)

type LibraryService interface {
	ScanRoot(ctx context.Context) error
	Query(ctx context.Context, condition *entity.LibraryQuery) (entity.FileItemList, int, error)
}
