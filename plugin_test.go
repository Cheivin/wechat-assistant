package main

import (
	"fmt"
	"path/filepath"
	"testing"
)

func TestParentDir(t *testing.T) {
	_, parent := filepath.Split(filepath.Dir("plugins/test/a.go"))
	fmt.Println(parent)
}

func TestLoadPlugin(t *testing.T) {
	fn, err := LoadPlugin("plugins/test/handler.go")
	if err != nil {
		t.Fatal(err)
	}
	t.Log(fn(nil, nil))
}
