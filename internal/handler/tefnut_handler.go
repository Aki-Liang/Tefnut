package handler

import (
	"Tefnut/internal/handler/handler_impl"
	"context"
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
	resp, err := handler.impl.LibraryList(context.Background())
	return handler.response(c, resp, err)
}

func (handler *TefnutHandler) response(c echo.Context, resp interface{}, err error) error {
	if err != nil {
		return c.JSON(200, map[string]interface{}{
			"code":    -1,
			"message": err.Error(),
		})
	}
	return c.JSON(200, map[string]interface{}{
		"code":    0,
		"message": "success",
		"data":    resp,
	})
}
