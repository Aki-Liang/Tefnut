package entity

import (
	"fmt"
	"testing"
)

func TestGetTmpName(t *testing.T) {
	item := &FileItem{
		Path: "23333",
	}
	fmt.Println(item.GetTmpName())
}
