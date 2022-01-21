package service

import "context"

type FilesystemService interface {
    ScanRoot(ctx context.Context)error
}