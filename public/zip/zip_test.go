package zip

import "testing"

var zs = NewZipService("./output")

func TestUnZipFile(t *testing.T) {
    path, err := zs.UnZipFile("./test_file/wallpaper.zip")
    if err != nil {
        panic(err)
    }
    t.Logf("path:%s", path)
}