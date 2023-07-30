package main

import "testing"

func Test_getPluginCode(t *testing.T) {
	packangeName, code, err := getPluginCode("plugins/demo.go")
	if err != nil {
		t.Fatal(err)
	}
	t.Log(packangeName, code)
}

func Test_loadPlugin(t *testing.T) {
	packangeName, code, err := getPluginCode("plugins/demo.go")
	if err != nil {
		t.Fatal(err)
	}
	plugin, err := loadPlugin(packangeName, code)
	if err != nil {
		t.Fatal(err)
	}
	t.Log(plugin.Keyword, plugin.Description)
}

func Test_initPlugin(t *testing.T) {
	packangeName, code, err := getPluginCode("plugins/demo.go")
	if err != nil {
		t.Fatal(err)
	}
	plugin, err := loadPlugin(packangeName, code)
	if err != nil {
		t.Fatal(err)
	}

	ok, err := initPlugin(plugin, nil)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Log("OK")
	} else {
		t.Error("Fail")
	}
}

func Test_destroyPlugin(t *testing.T) {
	packangeName, code, err := getPluginCode("plugins/demo.go")
	if err != nil {
		t.Fatal(err)
	}
	plugin, err := loadPlugin(packangeName, code)
	if err != nil {
		t.Fatal(err)
	}

	ok, err := destroyPlugin(plugin, nil)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Log("OK")
	} else {
		t.Error("Fail")
	}
}
