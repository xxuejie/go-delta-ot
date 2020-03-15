package ot

import (
	"testing"

	"github.com/fmpwizard/go-quilljs-delta/delta"
)

func deltaToText(d delta.Delta) string {
	result := make([]rune, 0)
	for _, op := range d.Ops {
		if op.Insert != nil {
			result = append(result, op.Insert...)
		}
	}
	return string(result)
}

func TestNullDeltaSubmit(t *testing.T) {
	file := NewFile(*delta.New(nil).Insert("abcde", nil))
	_, err := file.Submit(Change{
		Delta:   nil,
		Version: 0,
	})
	if err == nil {
		t.Fatalf("Submitting null Delta to file should generate error!")
	}
	if file.version != 0 {
		t.Fatalf("File version should not change! Current version: %d", file.version)
	}
	text := deltaToText(file.d)
	if text != "abcde" {
		t.Fatalf("File content should not change! Current content: %s", text)
	}
}
