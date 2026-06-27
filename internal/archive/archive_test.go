package archive

import (
	"archive/zip"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// makeZip writes a .zip at dir/name.zip containing the given files.
func makeZip(t *testing.T, dir, name string, files map[string]string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	for n, body := range files {
		w, err := zw.Create(n)
		if err != nil {
			t.Fatal(err)
		}
		io.WriteString(w, body)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestZipListSortedAndFiltered(t *testing.T) {
	dir := t.TempDir()
	zp := makeZip(t, dir, "c.zip", map[string]string{
		"10.jpg":           "a",
		"2.jpg":            "b",
		"1.jpg":            "c",
		"notes.txt":        "ignore",
		"__MACOSX/._1.jpg": "junk",
	})
	r, err := Open(context.Background(), zp, "")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	got := r.List()
	want := []string{"1.jpg", "2.jpg", "10.jpg"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}

func TestZipOpenEntry(t *testing.T) {
	dir := t.TempDir()
	zp := makeZip(t, dir, "c.zip", map[string]string{"1.jpg": "hello"})
	r, err := Open(context.Background(), zp, "")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	rc, err := r.Open("1.jpg")
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	b, _ := io.ReadAll(rc)
	if string(b) != "hello" {
		t.Fatalf("got %q", b)
	}
}

func TestWithinDir(t *testing.T) {
	base := "/data/cache/5"
	if !withinDir(base, base+"/001.jpg") {
		t.Error("normal entry should be within base")
	}
	if withinDir(base, base+"/../../etc/passwd") {
		t.Error("traversal entry must be rejected")
	}
}

func TestFirstImage(t *testing.T) {
	dir := t.TempDir()
	zp := makeZip(t, dir, "c.zip", map[string]string{"2.jpg": "two", "1.jpg": "one"})
	rc, name, count, err := FirstImage(context.Background(), zp, "")
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	if name != "1.jpg" || count != 2 {
		t.Fatalf("name=%q count=%d", name, count)
	}
	b, _ := io.ReadAll(rc)
	if string(b) != "one" {
		t.Fatalf("got %q", b)
	}
}
