package server

import (
	"os"
	"path/filepath"
	"strings"
)

// PathWithinRoots reports whether dir (an absolute, existing path) resolves to a
// location inside one of roots. Symlinks in both dir and each root are resolved
// first, so a symlink inside a root cannot point outside it, and so comparison
// is correct on platforms where temp paths are themselves symlinks (macOS
// /var -> /private/var). A root that fails to resolve (e.g. it does not exist)
// is skipped. Returns a non-nil error only when dir itself cannot be resolved;
// callers MUST fail closed (reject) on a non-nil error.
func PathWithinRoots(dir string, roots []string) (bool, error) {
	rp, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return false, err
	}
	for _, root := range roots {
		rr, err := filepath.EvalSymlinks(root)
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(rr, rp)
		if err != nil {
			continue
		}
		// inside iff rel is "." (equal) or a descendant path that does not
		// climb out via "..". This rejects both "/lib/../etc" escapes and the
		// "/lib" vs "/lib-other" prefix trap a naive HasPrefix would allow.
		if rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))) {
			return true, nil
		}
	}
	return false, nil
}
