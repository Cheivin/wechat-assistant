package main

import (
	"errors"
	"fmt"
	"github.com/eatmoreapple/openwechat"
	"github.com/traefik/yaegi/interp"
	"github.com/traefik/yaegi/stdlib"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"wechat-assistant/lock"
)

type Plugin struct {
	ID          string                                                          `gorm:"primary"` // 插件id
	Package     string                                                          ``               // 包名
	Code        string                                                          ``               // 加载内容
	Keyword     string                                                          ``               // 唤醒词
	Description string                                                          ``               // 描述
	interpreter *interp.Interpreter                                             `gorm:"-"`
	fn          func(db *gorm.DB, ctx *openwechat.MessageContext) (bool, error) `gorm:"-"` // 执行
}

func newInterpreter() *interp.Interpreter {
	interpreter := interp.New(interp.Options{}) // 初始化一个 yaegi 解释器
	_ = interpreter.Use(stdlib.Symbols)         // 容许脚本调用（简直）所有的 Go 官网 package 代码
	_ = interpreter.Use(map[string]map[string]reflect.Value{
		"github.com/eatmoreapple/openwechat/openwechat": {
			"MessageContext": reflect.ValueOf((*openwechat.MessageContext)(nil)),
		},
		"github.com/traefik/yaegi/interp/interp": {
			"Interpreter": reflect.ValueOf((*interp.Interpreter)(nil)),
			"New":         reflect.ValueOf(interp.New),
			"Options":     reflect.ValueOf((*interp.Options)(nil)),
		},
		"wechat-assistant/lock/lock": {
			"Locker": reflect.ValueOf((*lock.Locker)(nil)),
		},
		"gorm.io/gorm/gorm": {
			"DB":                reflect.ValueOf((*gorm.DB)(nil)),
			"Session":           reflect.ValueOf((*gorm.Session)(nil)),
			"ErrRecordNotFound": reflect.ValueOf(gorm.ErrRecordNotFound),
		},
		"gorm.io/gorm/clause/clause": {
			"Clause":     reflect.ValueOf((*clause.Clause)(nil)),
			"Builder":    reflect.ValueOf((*clause.Builder)(nil)),
			"Column":     reflect.ValueOf((*clause.Column)(nil)),
			"Expression": reflect.ValueOf((*clause.Expression)(nil)),
			"Assignment": reflect.ValueOf((*clause.Assignment)(nil)),
			"Locking":    reflect.ValueOf((*clause.Locking)(nil)),
			"Update":     reflect.ValueOf((*clause.Update)(nil)),
			"Delete":     reflect.ValueOf((*clause.Delete)(nil)),
		},
	})
	return interpreter
}

// getPluginCode 读取插件代码。可以从远程读取也可以从本地路径读取，返回包名、代码字符串、错误信息
func getPluginCode(pluginPath string) (string, string, error) {
	var code string
	if strings.HasPrefix(pluginPath, "http://") || strings.HasPrefix(pluginPath, "https://") {
		resp, err := http.Get(pluginPath)
		if err != nil {
			return "", "", err
		}
		defer resp.Body.Close()
		if data, err := io.ReadAll(resp.Body); err != nil {
			return "", "", err
		} else {
			code = string(data)
		}
	} else {
		data, err := os.ReadFile(pluginPath)
		if err != nil {
			return "", "", err
		}
		code = string(data)
	}
	packageName := strings.SplitN(code, "\n", 2)[0]
	if strings.HasPrefix(packageName, "package ") {
		packageName = strings.TrimSpace(strings.TrimPrefix(packageName, "package "))
	}
	if packageName == "" {
		_, packageName = filepath.Split(filepath.Dir(pluginPath))
	}
	return packageName, code, nil
}

func loadPlugin(packageName string, code string) (*Plugin, error) {
	interpreter := newInterpreter()
	// 加载
	if _, err := interpreter.Eval(code); err != nil {
		return nil, err
	}
	plugin := Plugin{
		interpreter: interpreter,
		Package:     packageName,
		Code:        code,
		ID:          packageName,
	}
	// 获取信息
	if v, err := interpreter.Eval(packageName + ".Info"); err == nil {
		if handler, ok := v.Interface().(func() (string, string)); ok {
			plugin.Keyword, plugin.Description = handler()
		}
	}
	// 目标方法
	if v, err := interpreter.Eval(packageName + ".Handle"); err != nil {
		return nil, err
	} else {
		if handler, ok := v.Interface().(func(db *gorm.DB, ctx *openwechat.MessageContext) (bool, error)); !ok {
			return nil, errors.New("handler not match")
		} else {
			plugin.fn = handler
		}
	}

	// 回收
	runtime.SetFinalizer(&plugin, func(pluginObj interface{}) {
		p := pluginObj.(*Plugin)
		fmt.Println("插件销毁:", p.ID, p.Keyword, p.Description)
		p.interpreter = nil
		p.fn = nil
	})

	return &plugin, nil
}

func initPlugin(plugin *Plugin, db *gorm.DB) (bool, error) {
	if v, err := plugin.interpreter.Eval(plugin.Package + ".Init"); err == nil {
		if handler, ok := v.Interface().(func(*gorm.DB) error); ok {
			if err = handler(db); err != nil {
				return false, err
			} else {
				return true, nil
			}
		}
	} else {
		if strings.Contains(err.Error(), "undefined selector") {
			return false, nil
		}
		return false, err
	}
	return false, nil
}

func destroyPlugin(plugin *Plugin, db *gorm.DB) (bool, error) {
	if v, err := plugin.interpreter.Eval(plugin.Package + ".Destroy"); err == nil {
		if handler, ok := v.Interface().(func(*gorm.DB) error); ok {
			if err = handler(db); err != nil {
				return false, err
			} else {
				return true, nil
			}
		}
	} else {
		if strings.Contains(err.Error(), "undefined selector") {
			return false, nil
		}
		return false, err
	}
	return false, nil
}
