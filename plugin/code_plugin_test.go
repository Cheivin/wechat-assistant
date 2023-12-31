package plugin

import (
	"testing"
	"wechat-assistant/interpreter"
)

func Test_loadPlugin(t *testing.T) {
	packageName, code, err := interpreter.GetCode("../plugins/demo.go")
	if err != nil {
		t.Fatal(err)
	}
	iPlugin, err := NewCodePlugin(packageName, code)
	if err != nil {
		t.Fatal(err)
	}
	plugin := iPlugin.(*CodePlugin)
	t.Log(plugin.info.Keyword, plugin.info.Description)
}

func Test_initPlugin(t *testing.T) {
	packageName, code, err := interpreter.GetCode("../plugins/demo.go")
	if err != nil {
		t.Fatal(err)
	}
	iPlugin, err := NewCodePlugin(packageName, code)
	if err != nil {
		t.Fatal(err)
	}
	plugin := iPlugin.(*CodePlugin)
	if plugin.initFn == nil {
		t.Error("Fail")
	}
	err = plugin.Init(nil)
	if err != nil {
		t.Fatal(err)
	}
}

func Test_destroyPlugin(t *testing.T) {
	packageName, code, err := interpreter.GetCode("../plugins/demo.go")
	if err != nil {
		t.Fatal(err)
	}
	iPlugin, err := NewCodePlugin(packageName, code)
	if err != nil {
		t.Fatal(err)
	}
	plugin := iPlugin.(*CodePlugin)
	if plugin.destroyFn == nil {
		t.Error("Fail")
	}
	err = plugin.Destroy(nil)
	if err != nil {
		t.Fatal(err)
	}
}
