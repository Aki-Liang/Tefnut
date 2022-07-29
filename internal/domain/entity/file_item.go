package entity

import (
	commonDefines "Tefnut/common/defines"
	commonTools "Tefnut/common/tools"
	"context"
	"crypto/md5"
	"fmt"
	"io/ioutil"
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
	ext := filepath.Ext(item.Path)
	switch ext {
	case ".rar", ".zip", ".RAR", ".ZIP":
		return true
	default:
		return false
	}
}

func (item *FileItem) GetTmpName() string {
	return fmt.Sprintf("%x", md5.Sum([]byte(item.Path)))
}

func (item *FileItem) GetTmpFileList(ctx context.Context, tmpPath string) ([]string, error) {
	path := tmpPath + "/" + item.GetTmpName()
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	exist, err := commonTools.PathExist(absPath)
	if err != nil {
		return nil, err
	}

	if !exist {
		//unachive
		err = commonTools.Archive(ctx, item.Path, path)
		if err != nil {
			return nil, err
		}
	}

	//get file list
	result := make([]string, 0)
	fileInfos, err := ioutil.ReadDir(absPath)
	if err != nil {
		return nil, err
	}
	for _, info := range fileInfos {
		result = append(result, info.Name())
	}

	return result, nil
}
