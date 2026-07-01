package archive

import (
	"archive/zip"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
)

// openEPUB opens an EPUB (a zip) for random access. Pages are ordered by the
// OPF spine when it lists images directly, else by natural sort of image
// entries (covers the common sequentially-named case, incl. XHTML-wrapper EPUBs).
func openEPUB(epubPath string) (Reader, error) {
	zc, err := zip.OpenReader(epubPath)
	if err != nil {
		return nil, fmt.Errorf("archive: open epub %s: %w", epubPath, err)
	}
	files := make(map[string]*zip.File, len(zc.File))
	for _, f := range zc.File {
		files[f.Name] = f
	}
	names := epubPageOrder(files)
	if len(names) == 0 {
		zc.Close()
		return nil, fmt.Errorf("archive: epub %s has no images", epubPath)
	}
	return &epubReader{zc: zc, files: files, names: names}, nil
}

type epubReader struct {
	zc    *zip.ReadCloser
	files map[string]*zip.File
	names []string
}

func (e *epubReader) List() []string { return append([]string(nil), e.names...) }

func (e *epubReader) Open(name string) (io.ReadCloser, error) {
	f, ok := e.files[name]
	if !ok {
		return nil, fmt.Errorf("archive: epub entry %q not found", name)
	}
	return f.Open()
}

func (e *epubReader) Close() error { return e.zc.Close() }

// epubPageOrder returns image entry names in reading order.
func epubPageOrder(files map[string]*zip.File) []string {
	if ordered := spineImages(files); len(ordered) > 0 {
		return ordered
	}
	var imgs []string
	for name := range files {
		if IsImage(name) && !IsJunk(name) {
			imgs = append(imgs, name)
		}
	}
	SortNatural(imgs)
	return imgs
}

type epubContainer struct {
	Rootfiles []struct {
		FullPath string `xml:"full-path,attr"`
	} `xml:"rootfiles>rootfile"`
}

type epubOPF struct {
	Manifest []struct {
		ID   string `xml:"id,attr"`
		Href string `xml:"href,attr"`
		Type string `xml:"media-type,attr"`
	} `xml:"manifest>item"`
	Spine []struct {
		IDRef string `xml:"idref,attr"`
	} `xml:"spine>itemref"`
}

// spineImages returns spine-ordered image hrefs, or nil if the OPF is missing,
// unparseable, or its spine lists no images.
func spineImages(files map[string]*zip.File) []string {
	opfPath := opfPathFrom(files["META-INF/container.xml"])
	if opfPath == "" {
		return nil
	}
	f, ok := files[opfPath]
	if !ok {
		return nil
	}
	var doc epubOPF
	if err := unmarshalZipXML(f, &doc); err != nil {
		return nil
	}
	base := path.Dir(opfPath)
	href := make(map[string]string, len(doc.Manifest))
	mtype := make(map[string]string, len(doc.Manifest))
	for _, it := range doc.Manifest {
		href[it.ID] = it.Href
		mtype[it.ID] = it.Type
	}
	var out []string
	for _, ref := range doc.Spine {
		h := href[ref.IDRef]
		if h == "" {
			continue
		}
		full := path.Clean(path.Join(base, h))
		if _, ok := files[full]; !ok {
			continue
		}
		if strings.HasPrefix(mtype[ref.IDRef], "image/") || IsImage(full) {
			out = append(out, full)
		}
	}
	return out
}

func opfPathFrom(f *zip.File) string {
	var c epubContainer
	if err := unmarshalZipXML(f, &c); err != nil {
		return ""
	}
	for _, rf := range c.Rootfiles {
		if rf.FullPath != "" {
			return rf.FullPath
		}
	}
	return ""
}

func unmarshalZipXML(f *zip.File, v any) error {
	if f == nil {
		return errors.New("archive: nil zip entry")
	}
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	return xml.NewDecoder(rc).Decode(v)
}
