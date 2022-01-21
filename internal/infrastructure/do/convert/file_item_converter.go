package convert

import (
    "Tefnut/internal/domain/entity"
    "Tefnut/internal/infrastructure/do"
)

var FileItemConverter = &_fileItemConverter{}

type _fileItemConverter struct {}

func (c *_fileItemConverter) ToDo(item *entity.FileItem) *do.FileItem {
    return &do.FileItem{
        Id:         item.Id,
        Name:       item.Name,
        Path:       item.Path,
        FileType:   item.FileType,
        ParentId:   item.ParentId,
    }
}

func (c *_fileItemConverter) ToDoList(items []*entity.FileItem) []*do.FileItem {
    result := make([]*do.FileItem, len(items))
    for i, item := range items {
        result[i] = c.ToDo(item)
    }
    return result
}

func (c *_fileItemConverter) ToEntity(item *do.FileItem) *entity.FileItem {
    return &entity.FileItem{
        Id:         item.Id,
        Name:       item.Name,
        Path:       item.Path,
        FileType:   item.FileType,
        ParentId:   item.ParentId,
    }
}

func (c *_fileItemConverter) ToEntityList(items []*do.FileItem) entity.FileItemList {
    result := make(entity.FileItemList, len(items))
    for i, item := range items {
        result[i] = c.ToEntity(item)
    }
    return result
}