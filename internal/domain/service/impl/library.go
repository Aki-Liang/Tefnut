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

type LibraryServiceImpl struct {
	conf          *configs.FilesystemConfig
	libRepository repository.LibraryRepository
}

func NewLibraryServiceImpl() *LibraryServiceImpl {
	return &LibraryServiceImpl{}
}

func (impl *LibraryServiceImpl) SetConfig(conf *configs.FilesystemConfig) *LibraryServiceImpl {
	impl.conf = conf
	return impl
}

func (impl *LibraryServiceImpl) SetLibraryRepository(repository repository.LibraryRepository) *LibraryServiceImpl {
	impl.libRepository = repository
	return impl
}

func (impl *LibraryServiceImpl) ScanRoot(ctx context.Context) error {
	if impl.conf == nil {
		return errors.Errorf("config is not set")
	}
	return impl.ScanPath(ctx, impl.conf.RootPath, nil)
}

func (impl *LibraryServiceImpl) ScanPath(ctx context.Context, path string, parentNode *entity.FileItem) error {
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

func (impl *LibraryServiceImpl) createNode(ctx context.Context, node *entity.FileItem) (*entity.FileItem, error) {
	if !node.ExtCorrect() {
		return nil, errors.Errorf("failed to create file node %s, incorrect ext", node.Path)
	}
	retNode, err := impl.libRepository.CreateNode(ctx, node)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create file node %s", node.Path)
	}
	return retNode, nil
}

func (impl *LibraryServiceImpl) deleteNode(ctx context.Context, node *entity.FileItem) error {
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
	// TODO: delete tmp data [renzhi]
	return impl.libRepository.DeleteNode(ctx, node.Id)
}

func (impl *LibraryServiceImpl) listChildNodes(ctx context.Context, parentId int) (entity.FileItemList, error) {
	nodes, err := impl.libRepository.ListChildNodes(ctx, parentId)
	if err != nil {
		return entity.FileItemList{}, errors.Wrapf(err, "failed to list child nodes of %d", parentId)
	}
	return nodes, nil
}

func (impl *LibraryServiceImpl) Query(ctx context.Context, condition *entity.LibraryQuery) (entity.FileItemList, int, error) {
	return impl.libRepository.Query(ctx, condition)
}

func (impl *LibraryServiceImpl) GetContent(ctx context.Context, id int) (string, []string, error) {
	node, err := impl.libRepository.GetNode(ctx, id)
	if err != nil {
		return "", nil, errors.Wrapf(err, "service:LibraryServiceImpl:GetContent GetNode failed, id:%v", id)
	}

	if node.FileType != commonDefines.FileItemTypeFile {
		return "", nil, errors.Wrapf(err, "service:LibraryServiceImpl:GetContent node without content, id:%v", id)
	}

	list, err := node.GetTmpFileList(ctx, impl.conf.TempPath)
	if err != nil {
		return "", nil, errors.Wrapf(err, "service:LibraryServiceImpl:GetContent getTmpFileList failed, id:%v", id)
	}

	return node.GetTmpName(), list, nil
}