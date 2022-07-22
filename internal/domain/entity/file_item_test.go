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

func TestExtCorrect(t *testing.T) {
	item := &FileItem{
		Path: "/233/蠢沫沫 - 可畏 巫女 + 可畏 婚纱 + 可畏 绅士版.zip",
	}
	item.ExtCorrect()
}
