package entity

type FileItemList []*FileItem

func (fileItemList FileItemList) GetPathMap() map[string]*FileItem {
    res := make(map[string]*FileItem)
	for _, fileItem := range fileItemList {
        res[fileItem.Path] = fileItem
	}
	return res
}
