package entity

import (
	commonDefines "Tefnut/common/defines"
)

type FileItem struct {
	Id       int                        `json:"id"`
	Name     string                     `json:"name"`
	Path     string                     `json:"path"`
	FileType commonDefines.FileItemType `json:"file_type"`
	ParentId int                        `json:"parent_id"`
}
