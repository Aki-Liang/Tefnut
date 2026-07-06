package scan

import (
	"log"
	"os"
	"path/filepath"
)

// walkDirsFollowingSymlinks calls visit for root and every directory below
// it, following directory symlinks (which filepath.WalkDir does not). A link
// whose resolved target is already on the current recursion chain is skipped
// so symlink loops terminate; non-ancestor repeats (two links to the same
// directory) are each walked, matching the scanner which keeps one library
// entry per link. Unreadable entries are logged and skipped.
func walkDirsFollowingSymlinks(root string, visit func(dir string)) {
	info, err := os.Stat(root)
	if err != nil {
		log.Printf("scan: walk %s: %v", root, err)
		return
	}
	if !info.IsDir() {
		return
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		log.Printf("scan: resolve %s: %v", root, err)
		return
	}
	walkDirs(root, realRoot, map[string]bool{realRoot: true}, visit)
}

// walkDirs recurses under dir. realDir is dir with all symlinks resolved;
// onPath holds the resolved path of every directory on the recursion chain.
func walkDirs(dir, realDir string, onPath map[string]bool, visit func(dir string)) {
	visit(dir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		log.Printf("scan: read dir %s: %v", dir, err)
		return
	}
	for _, e := range entries {
		p := filepath.Join(dir, e.Name())
		realChild := filepath.Join(realDir, e.Name())
		if e.Type()&os.ModeSymlink != 0 {
			info, err := os.Stat(p)
			if err != nil || !info.IsDir() {
				continue // broken link or link to a file: nothing to watch
			}
			realChild, err = filepath.EvalSymlinks(p)
			if err != nil {
				log.Printf("scan: resolve %s: %v", p, err)
				continue
			}
			if onPath[realChild] {
				log.Printf("scan: symlink loop %s -> %s; skipping", p, realChild)
				continue
			}
		} else if !e.IsDir() {
			continue
		}
		onPath[realChild] = true
		walkDirs(p, realChild, onPath, visit)
		delete(onPath, realChild)
	}
}
