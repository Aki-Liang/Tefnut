package archive

import (
	"path"
	"strings"
)

var archiveExts = map[string]bool{
	".zip": true, ".cbz": true,
	".rar": true, ".cbr": true,
	".7z": true, ".cb7": true,
}

var imageExts = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true,
	".gif": true, ".webp": true, ".bmp": true,
}

func ext(name string) string { return strings.ToLower(path.Ext(name)) }

// IsArchive reports whether name has a supported comic archive extension.
func IsArchive(name string) bool { return archiveExts[ext(name)] }

// IsImage reports whether name has a supported image extension.
func IsImage(name string) bool { return imageExts[ext(name)] }

// IsJunk reports whether an in-archive entry should be ignored.
func IsJunk(name string) bool {
	if strings.Contains(name, "__MACOSX") {
		return true
	}
	base := path.Base(name)
	return strings.HasPrefix(base, ".")
}
