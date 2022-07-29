package tools

import (
	"context"
	"github.com/mholt/archiver/v4"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func Archive(ctx context.Context, sourceFile string, targetPath string) error {
	err := CreatePathIfNotExists(targetPath, os.ModePerm)
	if err != nil {
		return err
	}
	source, err := os.Open(sourceFile)
	if err != nil {
		return err
	}
	defer source.Close()
	sourceName := filepath.Base(sourceFile)
	format, reader, err := archiver.Identify(sourceName, source)
	if err != nil {
		return err
	}

	err = format.(archiver.Extractor).Extract(ctx, reader, nil, func(ctx context.Context, f archiver.File) error {
		rc, err := f.Open()
		if err != nil {
			return err
		}
		defer rc.Close()

		if !f.IsDir() {
			if strings.Contains(f.NameInArchive, "__MACOSX") {
				// TODO: find a better way to avoid __MACOSX [renzhi]
				return nil
			}
			dst, err := os.Create(targetPath + "/" + f.Name())
			if err != nil {
				return err
			}
			defer dst.Close()
			data, err := io.ReadAll(rc)
			if err != nil {
				return err
			}
			_, err = dst.Write(data)
			if err != nil {
				return err
			}
		}
		return err
	})
	if err != nil {
		return err
	}
	return nil
}
