package repository_impl

import (
    "Tefnut/internal/domain/entity"
    "Tefnut/internal/infrastructure/do"
    "Tefnut/internal/infrastructure/do/convert"
    "context"
    "github.com/pkg/errors"
    "xorm.io/xorm"
)

type FileSystemRepository struct {
    db *xorm.Engine
}

func NewFileSystemRepository( db *xorm.Engine) *FileSystemRepository {
    return &FileSystemRepository{
        db: db,
    }
}

func (impl *FileSystemRepository)CreateNode(ctx context.Context, item *entity.FileItem) (*entity.FileItem, error) {
    doItem := convert.FileItemConverter.ToDo(item)
    _, err := impl.db.Table(doItem.TableName()).Insert(doItem)
    if err != nil {
        return nil, errors.Wrapf(err, "repository:FileSystemRepository:CreateNode insert failed, item: %v", item)
    }
    return convert.FileItemConverter.ToEntity(doItem), nil
}

func (impl *FileSystemRepository)ListChildNodes(ctx context.Context, parentId int) (entity.FileItemList, error) {
    doItems := make([]*do.FileItem, 0)
    err := impl.db.Table(do.TableNameFileItem).Where("parent_id = ?", parentId).Find(&doItems)
    if err != nil {
        return nil, errors.Wrapf(err, "repository:FileSystemRepository:ListChildNodes failed, parentId: %v", parentId)
    }
    return convert.FileItemConverter.ToEntityList(doItems), nil
}

func (impl *FileSystemRepository)DeleteNode(ctx context.Context, id int) error {
    _, err := impl.db.Table(do.TableNameFileItem).Where("id = ?", id).Delete(do.FileItem{})
    if err != nil {
        return errors.Wrapf(err, "repository:FileSystemRepository:DeleteNode failed, id: %v", id)
    }
    return nil
}