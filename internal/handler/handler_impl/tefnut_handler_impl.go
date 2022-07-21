package handler_impl

import "Tefnut/internal/domain/service"

type TefnutHandlerImpl struct {
	fsService service.FilesystemService
}

func NewTefnutHandlerImpl() *TefnutHandlerImpl {
	return &TefnutHandlerImpl{}
}

func (impl *TefnutHandlerImpl) SetFSService(fsService service.FilesystemService) *TefnutHandlerImpl {
	impl.fsService = fsService
	return impl
}
