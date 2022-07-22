package cron_impl

import (
	"Tefnut/internal/domain/service"
	"context"
)

type FileScanCron struct {
	libService service.LibraryService
}

func NewFileScanCron() *FileScanCron {
	return &FileScanCron{}
}

func (impl *FileScanCron) SetLibraryService(libService service.LibraryService) *FileScanCron {
	impl.libService = libService
	return impl
}

func (impl *FileScanCron) Scan() {
	err := impl.libService.ScanRoot(context.Background())
	if err != nil {
		// TODO: log here [renzhi]
	}
}
