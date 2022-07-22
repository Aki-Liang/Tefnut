package handler_impl

import "Tefnut/internal/domain/service"

type TefnutHandlerImpl struct {
	fsService service.LibraryService
}

func NewTefnutHandlerImpl() *TefnutHandlerImpl {
	return &TefnutHandlerImpl{}
}

func (impl *TefnutHandlerImpl) SetFSService(fsService service.LibraryService) *TefnutHandlerImpl {
	impl.fsService = fsService
	return impl
}
