package handler

import (
	"Tefnut/internal/handler/handler_impl"
	"fmt"
	"github.com/labstack/echo/v4"
)

type TefnutHandler struct {
	impl *handler_impl.TefnutHandlerImpl
}

func NewTefnutHandler() *TefnutHandler {
	return &TefnutHandler{}
}

func (handler *TefnutHandler) SetImpl(impl *handler_impl.TefnutHandlerImpl) *TefnutHandler {
	handler.impl = impl
	return handler
}

func (handler *TefnutHandler) LibList(c echo.Context) error {
	fmt.Println("do nothing")

	return nil
}
