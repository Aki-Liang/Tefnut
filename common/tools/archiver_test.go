package tools

import (
	"context"
	"testing"
)

func TestArchive(t *testing.T) {
	err := Archive(context.Background(), "/Users/liangrenzhi/ws/gomod/Tefnut/cmd/tefnut/COMIC/test1/小桃舰长的秘密基地1--鹤田谦二.zip", "/Users/liangrenzhi/ws/gomod/Tefnut/cmd/tefnut/tmp/243a669cd8bcf31c0a24feaf1ae3408b")
	if err != nil {
		panic(err)
	}
}
