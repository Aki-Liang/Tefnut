package archive

import (
	"reflect"
	"testing"
)

func TestSortNatural(t *testing.T) {
	in := []string{"10.jpg", "2.jpg", "1.jpg", "page-100.png", "page-9.png"}
	SortNatural(in)
	want := []string{"1.jpg", "2.jpg", "10.jpg", "page-9.png", "page-100.png"}
	if !reflect.DeepEqual(in, want) {
		t.Fatalf("got %v want %v", in, want)
	}
}
