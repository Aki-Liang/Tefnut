package archive

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/mholt/archiver/v4"
)

type Reader interface {
	List() []string
	Open(name string) (io.ReadCloser, error)
	Close() error
}

// Open returns a Reader for the archive at archivePath. zip/cbz use random
// access; rar/cbr/7z/cb7 are extracted into cacheDir on first use.
func Open(ctx context.Context, archivePath, cacheDir string) (Reader, error) {
	switch ext(archivePath) {
	case ".zip", ".cbz":
		return openZip(archivePath)
	default:
		return openExtracted(ctx, archivePath, cacheDir)
	}
}

// FirstImage opens the archive and returns the first image entry reader,
// its name, and the total image count.
func FirstImage(ctx context.Context, archivePath, cacheDir string) (io.ReadCloser, string, int, error) {
	r, err := Open(ctx, archivePath, cacheDir)
	if err != nil {
		return nil, "", 0, err
	}
	names := r.List()
	if len(names) == 0 {
		r.Close()
		return nil, "", 0, fmt.Errorf("archive: %s has no images", archivePath)
	}
	rc, err := r.Open(names[0])
	if err != nil {
		r.Close()
		return nil, "", 0, err
	}
	// Wrap so closing the page reader also closes the archive.
	return &closer{rc: rc, also: r}, names[0], len(names), nil
}

type closer struct {
	rc   io.ReadCloser
	also Reader
}

func (c *closer) Read(p []byte) (int, error) { return c.rc.Read(p) }
func (c *closer) Close() error {
	err := c.rc.Close()
	c.also.Close()
	return err
}

// ---- zip (random access) ----

type zipReader struct {
	zc    *zip.ReadCloser
	files map[string]*zip.File
	names []string
}

func openZip(path string) (Reader, error) {
	zc, err := zip.OpenReader(path)
	if err != nil {
		return nil, fmt.Errorf("archive: open zip %s: %w", path, err)
	}
	zr := &zipReader{zc: zc, files: map[string]*zip.File{}}
	for _, f := range zc.File {
		if f.FileInfo().IsDir() || IsJunk(f.Name) || !IsImage(f.Name) {
			continue
		}
		zr.files[f.Name] = f
		zr.names = append(zr.names, f.Name)
	}
	SortNatural(zr.names)
	return zr, nil
}

func (z *zipReader) List() []string { return z.names }

func (z *zipReader) Open(name string) (io.ReadCloser, error) {
	f, ok := z.files[name]
	if !ok {
		return nil, fmt.Errorf("archive: entry %q not found", name)
	}
	return f.Open()
}

func (z *zipReader) Close() error { return z.zc.Close() }

// ---- extracted (rar / 7z) ----

type dirReader struct {
	dir   string
	names []string
}

func openExtracted(ctx context.Context, archivePath, cacheDir string) (Reader, error) {
	if cacheDir == "" {
		return nil, errors.New("archive: cacheDir required for rar/7z")
	}
	if err := ensureExtracted(ctx, archivePath, cacheDir); err != nil {
		return nil, err
	}
	dr := &dirReader{dir: cacheDir}
	walkErr := filepath.WalkDir(cacheDir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(cacheDir, p)
		if IsJunk(rel) || !IsImage(rel) {
			return nil
		}
		dr.names = append(dr.names, filepath.ToSlash(rel))
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("archive: walk cache %s: %w", cacheDir, walkErr)
	}
	SortNatural(dr.names)
	return dr, nil
}

func (d *dirReader) List() []string { return d.names }

func (d *dirReader) Open(name string) (io.ReadCloser, error) {
	return os.Open(filepath.Join(d.dir, filepath.FromSlash(name)))
}

func (d *dirReader) Close() error { return nil }

// ensureExtracted extracts archivePath into cacheDir once; if cacheDir
// already contains files it is treated as already extracted.
func ensureExtracted(ctx context.Context, archivePath, cacheDir string) error {
	if entries, err := os.ReadDir(cacheDir); err == nil && len(entries) > 0 {
		return nil
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return err
	}
	src, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("archive: open %s: %w", archivePath, err)
	}
	defer src.Close()
	format, reader, err := archiver.Identify(filepath.Base(archivePath), src)
	if err != nil {
		return fmt.Errorf("archive: identify %s: %w", archivePath, err)
	}
	ex, ok := format.(archiver.Extractor)
	if !ok {
		return fmt.Errorf("archive: %s not extractable", archivePath)
	}
	return ex.Extract(ctx, reader, nil, func(ctx context.Context, f archiver.File) error {
		if f.IsDir() || IsJunk(f.NameInArchive) || !IsImage(f.NameInArchive) {
			return nil
		}
		dst := filepath.Join(cacheDir, filepath.FromSlash(f.NameInArchive))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		defer rc.Close()
		out, err := os.Create(dst)
		if err != nil {
			return err
		}
		defer out.Close()
		_, err = io.Copy(out, rc)
		return err
	})
}
