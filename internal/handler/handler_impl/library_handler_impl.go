package handler_impl

import "context"

func (impl *TefnutHandlerImpl) LibraryList(ctx context.Context) (interface{}, error) {

	return map[string]interface{}{
		"key": "value",
	}, nil
}
