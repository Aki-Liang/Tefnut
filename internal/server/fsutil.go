package server

import (
	"os"
	"path"
	"strings"
)

func filepathStat(p string) (os.FileInfo, error) { return os.Stat(p) }

func contentType(name string) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".bmp":
		return "image/bmp"
	default:
		return "image/jpeg"
	}
}
