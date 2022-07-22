package repository_impl

import (
	"Tefnut/internal/domain/entity"
	"Tefnut/internal/infrastructure/do"
	"Tefnut/internal/infrastructure/do/convert"
	"context"
	"github.com/pkg/errors"
	"xorm.io/xorm"
)

type LibraryRepositoryImpl struct {
	db *xorm.Engine
}

func NewLibraryRepositoryImpl(db *xorm.Engine) *LibraryRepositoryImpl {
	return &LibraryRepositoryImpl{
		db: db,
	}
}

func (impl *LibraryRepositoryImpl) CreateNode(ctx context.Context, item *entity.FileItem) (*entity.FileItem, error) {
	doItem := convert.FileItemConverter.ToDo(item)
	_, err := impl.db.Table(doItem.TableName()).Insert(doItem)
	if err != nil {
		return nil, errors.Wrapf(err, "repository:LibraryRepositoryImpl:CreateNode insert failed, item: %v", item)
	}
	return convert.FileItemConverter.ToEntity(doItem), nil
}

func (impl *LibraryRepositoryImpl) ListChildNodes(ctx context.Context, parentId int) (entity.FileItemList, error) {
	doItems := make([]*do.FileItem, 0)
	err := impl.db.Table(do.TableNameFileItem).Where("parent_id = ?", parentId).Find(&doItems)
	if err != nil {
		return nil, errors.Wrapf(err, "repository:LibraryRepositoryImpl:ListChildNodes failed, parentId: %v", parentId)
	}
	return convert.FileItemConverter.ToEntityList(doItems), nil
}

func (impl *LibraryRepositoryImpl) DeleteNode(ctx context.Context, id int) error {
	_, err := impl.db.Table(do.TableNameFileItem).Where("id = ?", id).Delete(do.FileItem{})
	if err != nil {
		return errors.Wrapf(err, "repository:LibraryRepositoryImpl:DeleteNode failed, id: %v", id)
	}
	return nil
}

func (impl *LibraryRepositoryImpl) Query(ctx context.Context, condition *entity.LibraryQuery) (entity.FileItemList, int, error) {
	session := impl.db.NewSession()
	defer session.Close()
	doItems := make([]*do.FileItem, 0)
	session = condition.Process(ctx, session)
	total, err := session.Table(do.TableNameFileItem).FindAndCount(&doItems)
	if err != nil {
		return nil, 0, errors.Wrapf(err, "repository:LibraryRepositoryImpl:Query find failed, condition: %v", condition)
	}
	return convert.FileItemConverter.ToEntityList(doItems), int(total), nil
}
