package impl

import (
	commonDefines "Tefnut/common/defines"
	"Tefnut/configs"
	"Tefnut/internal/domain/entity"
	"Tefnut/internal/domain/repository"
	"context"
	"github.com/pkg/errors"
	"io/ioutil"
	"path/filepath"
)

type FileService struct {
	conf           *configs.FilesystemConfig
	FileRepository repository.FilesystemRepository
}

func NewFileService() *FileService {
	return &FileService{}
}

func (impl *FileService) SetConfig(conf *configs.FilesystemConfig) *FileService {
	impl.conf = conf
	return impl
}

func (impl *FileService) SetFileRepository(repository repository.FilesystemRepository) *FileService {
	impl.FileRepository = repository
	return impl
}

func (impl *FileService) ScanRoot(ctx context.Context) error {
	if impl.conf == nil {
		return errors.Errorf("config is not set")
	}
	return impl.ScanPath(ctx, impl.conf.RootPath, nil)
}

func (impl *FileService) ScanPath(ctx context.Context, path string, parentNode *entity.FileItem) error {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return errors.Wrapf(err, "failed to get abs path, path:%s", path)
	}
	fileInfos, err := ioutil.ReadDir(absPath)
	if err != nil {
		return errors.Wrapf(err, "failed to read dir %s", path)
	}

	pId := 0
	if parentNode != nil {
		pId = parentNode.Id
	}

	//获取当前层的所有已记录节点 childList
	childs, _ := impl.listChildNodes(ctx, pId)
	childPathMap := childs.GetPathMap()

	for _, info := range fileInfos {
		itemPath := filepath.Join(absPath, info.Name())
		item := &entity.FileItem{
			Name:     info.Name(),
			Path:     itemPath,
			ParentId: pId,
		}
		if info.IsDir() {
			item.FileType = commonDefines.FileItemTypeDirectory
		} else {
			item.FileType = commonDefines.FileItemTypeFile
		}
		//如果childList中已存在则去掉已记录的节点
		existItem, ok := childPathMap[itemPath]
		if !ok {
			item, err = impl.createNode(ctx, item)
			if err != nil {
				//todo log
				continue
			}
		} else {
			item = existItem
			delete(childPathMap, itemPath)
		}

		if info.IsDir() {
			err = impl.ScanPath(ctx, itemPath, item)
			if err != nil {
				//todo log
				continue
			}
		}
	}

	//如果childList中还存在节点对象，则删除
	for _, item := range childPathMap {
		err = impl.deleteNode(ctx, item)
		if err != nil {
			//todo log
			continue
		}
	}

	return nil
}

func (impl *FileService) createNode(ctx context.Context, node *entity.FileItem) (*entity.FileItem, error) {
	retNode, err := impl.FileRepository.CreateNode(ctx, node)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create file node %s", node.Path)
	}
	return retNode, nil
}

func (impl *FileService) deleteNode(ctx context.Context, node *entity.FileItem) error {
	if node == nil {
		return nil
	}
	childs, _ := impl.listChildNodes(ctx, node.Id)
	for _, info := range childs {
		err := impl.deleteNode(ctx, info)
		if err != nil {
			//todo log
			continue
		}
	}
	return impl.FileRepository.DeleteNode(ctx, node.Id)
}

func (impl *FileService) listChildNodes(ctx context.Context, parentId int) (entity.FileItemList, error) {
	nodes, err := impl.FileRepository.ListChildNodes(ctx, parentId)
	if err != nil {
		return entity.FileItemList{}, errors.Wrapf(err, "failed to list child nodes of %d", parentId)
	}
	return nodes, nil
}
