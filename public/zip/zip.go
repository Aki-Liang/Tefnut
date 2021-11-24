package zip

import (
    "archive/zip"
    "github.com/pkg/errors"
    "io"
    "os"
    "path/filepath"
    "strings"
)

type ZipService struct {
    OutputPath string
}

func NewZipService(path string) *ZipService {
    return &ZipService{
        OutputPath: path,
    }
}

func (zs *ZipService)SetOutputPath(path string) *ZipService {
    zs.OutputPath = path
    return zs
}

func (zs *ZipService) getFileName(path string) string {
    fileNameWithExt := filepath.Base(path)
    names := strings.Split(fileNameWithExt, ".")
    if len(names) > 0 {
        return names[0]
    }
    return fileNameWithExt
}

func (zs *ZipService)UnZipFile(path string) (string, error) {
    archive, err := zip.OpenReader(path)
    if err != nil {
        return "", errors.Wrapf(err, "public:ZipService UnZipFile open reader failed, path:%v", path)
    }
    defer archive.Close()

    outputPath := zs.OutputPath+ string(os.PathSeparator) + zs.getFileName(path)
    for _, f := range archive.File {
        filePath := filepath.Join(outputPath, f.Name)
        if !strings.HasPrefix(filePath, filepath.Clean(outputPath)+string(os.PathSeparator)) {
            return "", errors.Errorf("public:ZipService UnZipFile incorrect output path, file path:%v, zip path:%v", filePath, path)
        }
        if f.FileInfo().IsDir() {
            os.MkdirAll(filePath, os.ModePerm)
            continue
        }
        if err := os.MkdirAll(filepath.Dir(filePath), os.ModePerm); err != nil {
            return "", errors.Wrapf(err, "public:ZipService UnZipFile mkdir failed, file path:%v, zip path:%v", filePath, path)
        }
        dstFile, err := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
        if err != nil {
            return "", errors.Wrapf(err, "public:ZipService UnZipFile create file failed, file path:%v, zip path:%v", filePath, path)
        }
        fileInArchive, err := f.Open()
        if err != nil {
            return "", errors.Wrapf(err, "public:ZipService UnZipFile open archive file failed, file path:%v, zip path:%v", filePath, path)
        }
        if _, err := io.Copy(dstFile, fileInArchive); err != nil {
            return "", errors.Wrapf(err, "public:ZipService UnZipFile save file failed, file path:%v, zip path:%v", filePath, path)
        }
        dstFile.Close()
        fileInArchive.Close()
    }
    return outputPath, nil
}