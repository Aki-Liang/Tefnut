package service

import "context"

type LibraryService interface {
	ScanRoot(ctx context.Context) error
}
