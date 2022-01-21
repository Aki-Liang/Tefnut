package do

import (
	commonDefines "Tefnut/common/defines"
	"time"
)

const TableNameFileItem = "file"

type FileItem struct {
	Id         int                        `xorm:"id pk autoincr"`
	Name       string                     `xorm:"name"`
	Path       string                     `xorm:"path"`
	FileType   commonDefines.FileItemType `xorm:"file_type"`
	ParentId   int                        `xorm:"parent_id"`
	CreateTime time.Time                  `xorm:"create_time created"`
	UpdateTime time.Time                  `xorm:"update_time updated"`
}

func (item *FileItem) TableName() string {
	return TableNameFileItem
}
