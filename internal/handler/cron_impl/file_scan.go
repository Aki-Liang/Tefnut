package cron_impl

import (
	"Tefnut/internal/domain/service"
	"context"
)

type FileScanCron struct {
	fsService service.FilesystemService
}

func NewFileScanCron() *FileScanCron {
	return &FileScanCron{}
}

func (impl *FileScanCron) SetFSService(fsService service.FilesystemService) *FileScanCron {
	impl.fsService = fsService
	return impl
}

func (impl *FileScanCron) Scan() {
	err := impl.fsService.ScanRoot(context.Background())
	if err != nil {
		// TODO: log here [renzhi]
	}
}
