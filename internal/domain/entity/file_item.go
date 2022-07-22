package entity

import (
	commonDefines "Tefnut/common/defines"
	"crypto/md5"
	"fmt"
	"path/filepath"
)

type FileItem struct {
	Id       int                        `json:"id"`
	Name     string                     `json:"name"`
	Path     string                     `json:"path"`
	FileType commonDefines.FileItemType `json:"file_type"`
	ParentId int                        `json:"parent_id"`
}

func (item *FileItem) ExtCorrect() bool {
	if item.FileType == commonDefines.FileItemTypeDirectory {
		return true
	}
	switch filepath.Ext(item.Path) {
	case "rar", "zip", "RAR", "ZIP":
		return true
	default:
		return false
	}
}

func (item *FileItem) GetTmpName() string {
	return fmt.Sprintf("%x", md5.Sum([]byte(item.Path)))
}
