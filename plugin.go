package main

import (
	"errors"
	"fmt"
	"github.com/eatmoreapple/openwechat"
	"github.com/traefik/yaegi/interp"
	"github.com/traefik/yaegi/stdlib"
	"gorm.io/gorm"
	"os"
	"path/filepath"
	"reflect"
)

var (
	pluginMap map[string]string
)

func init() {
	pluginMap = make(map[string]string)
}

type PluginFn func(db *gorm.DB, ctx *openwechat.MessageContext) (bool, error)

func newInterpreter() *interp.Interpreter {
	intp := interp.New(interp.Options{}) // 初始化一个 yaegi 解释器
	intp.Use(stdlib.Symbols)             // 容许脚本调用（简直）所有的 Go 官网 package 代码
	intp.Use(map[string]map[string]reflect.Value{
		"gorm.io/gorm/gorm": {
			"DB": reflect.ValueOf((*gorm.DB)(nil)),
		},
		"github.com/eatmoreapple/openwechat/openwechat": {
			"MessageContext": reflect.ValueOf((*openwechat.MessageContext)(nil)),
		},
	})
	return intp
}

func LoadPlugin(pluginPath string) (PluginFn, error) {
	data, err := os.ReadFile(pluginPath)
	if err != nil {
		return nil, err
	}
	intp := newInterpreter()
	if v, err := intp.Eval(string(data)); err != nil {
		return nil, err
	} else {
		fmt.Println(v)
	}
	_, packageName := filepath.Split(filepath.Dir(pluginPath))
	if v, err := intp.Eval(packageName + ".Handle"); err != nil {
		return nil, err
	} else {
		if handler, ok := v.Interface().(func(db *gorm.DB, ctx *openwechat.MessageContext) (bool, error)); !ok {
			return nil, errors.New("handler not match")
		} else {
			return handler, nil
		}
	}
}

func bindPlugin(keyword string, pluginPath string) {
	pluginMap[keyword] = pluginPath
}
