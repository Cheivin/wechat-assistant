package main

import (
	"testing"
)

func TestLoadPlugin(t *testing.T) {
	plugin, err := LoadPlugin("plugins/test2.go")
	if err != nil {
		t.Fatal(err)
	}

	t.Log(plugin.Keyword, plugin.Description)
}

func TestLoadRemotePlugin(t *testing.T) {
	src := "http://halo.suitwe.com:8080/upload/test2.go"
	plugin, err := LoadPlugin(src)
	if err != nil {
		t.Fatal(err)
	}

	t.Log(plugin.Keyword, plugin.Description)
}
