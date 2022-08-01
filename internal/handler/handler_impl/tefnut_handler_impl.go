package handler_impl

import "Tefnut/internal/domain/service"

type TefnutHandlerImpl struct {
	libService service.LibraryService
}

func NewTefnutHandlerImpl() *TefnutHandlerImpl {
	return &TefnutHandlerImpl{}
}

func (impl *TefnutHandlerImpl) SetLibraryService(libService service.LibraryService) *TefnutHandlerImpl {
	impl.libService = libService
	return impl
}
