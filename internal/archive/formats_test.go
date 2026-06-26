package archive

import "testing"

func TestIsArchive(t *testing.T) {
	for _, n := range []string{"a.zip", "a.CBZ", "b.rar", "b.cbr", "c.7z", "c.cb7"} {
		if !IsArchive(n) {
			t.Errorf("%s should be archive", n)
		}
	}
	for _, n := range []string{"a.txt", "a.jpg", "folder"} {
		if IsArchive(n) {
			t.Errorf("%s should NOT be archive", n)
		}
	}
}

func TestIsImage(t *testing.T) {
	for _, n := range []string{"a.jpg", "a.JPEG", "b.png", "c.webp", "d.gif", "e.bmp"} {
		if !IsImage(n) {
			t.Errorf("%s should be image", n)
		}
	}
	if IsImage("a.txt") {
		t.Error("txt is not image")
	}
}

func TestIsJunk(t *testing.T) {
	if !IsJunk("__MACOSX/._x.jpg") {
		t.Error("__MACOSX is junk")
	}
	if !IsJunk(".hidden.jpg") {
		t.Error("dotfile is junk")
	}
	if IsJunk("001.jpg") {
		t.Error("normal file is not junk")
	}
}
