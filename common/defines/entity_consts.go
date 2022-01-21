package defines

type FileItemType int32

const (
    FileItemTypeNil FileItemType = iota
    FileItemTypeFile
    FileItemTypeDirectory
    FileItemTypeEnd
)


func (t FileItemType) IsValid() bool {
    return FileItemTypeNil < t && t < FileItemTypeEnd
}

func (t FileItemType) String()string {
    switch t {
    case FileItemTypeFile:
        return "文件"
    case FileItemTypeDirectory:
        return "目录"
    }
    return "Unknown"
}