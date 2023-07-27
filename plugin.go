package main

import (
	"errors"
	"fmt"
	"github.com/eatmoreapple/openwechat"
	"github.com/traefik/yaegi/interp"
	"github.com/traefik/yaegi/stdlib"
	"gorm.io/gorm"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
)

var (
	pluginMap sync.Map
)

type Plugin struct {
	src         string   // 路径
	code        string   // 加载内容
	Keyword     string   // 关键词
	Description string   // 描述
	Fn          PluginFn // 执行
}

func init() {
	pluginMap = sync.Map{}
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

func LoadPlugin(pluginPath string) (*Plugin, error) {
	var code string
	if strings.HasPrefix(pluginPath, "http://") || strings.HasPrefix(pluginPath, "https://") {
		resp, err := http.Get(pluginPath)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if data, err := io.ReadAll(resp.Body); err != nil {
			return nil, err
		} else {
			code = string(data)
		}
	} else {
		data, err := os.ReadFile(pluginPath)
		if err != nil {
			return nil, err
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
	if plugin, err := loadPlugin(packageName, code); err != nil {
		return nil, err
	} else {
		plugin.src = pluginPath
		return plugin, nil
	}
}

func loadPlugin(packageName string, code string) (*Plugin, error) {
	intp := newInterpreter()
	// 加载
	if _, err := intp.Eval(code); err != nil {
		return nil, err
	}
	plugin := Plugin{
		code: code,
	}
	// 获取信息
	if v, err := intp.Eval(packageName + ".Info"); err == nil {
		if handler, ok := v.Interface().(func() (string, string)); ok {
			plugin.Keyword, plugin.Description = handler()
		}
	}
	// 目标方法
	if v, err := intp.Eval(packageName + ".Handle"); err != nil {
		return nil, err
	} else {
		if handler, ok := v.Interface().(func(db *gorm.DB, ctx *openwechat.MessageContext) (bool, error)); !ok {
			return nil, errors.New("handler not match")
		} else {
			plugin.Fn = handler
		}
	}
	return &plugin, nil
}

func bindPlugin(keyword string, pluginPath string) (info string, err error) {
	plugin, err := LoadPlugin(pluginPath)
	if err != nil {
		return "", err
	}
	if keyword != "" {
		plugin.Keyword = keyword
	}
	if plugin.Keyword == "" {
		return "", errors.New("插件未绑定唤醒词")
	}
	if plugin.Description == "" {
		plugin.Description = "唤醒词:" + plugin.Keyword
	}
	// 存到map
	pluginMap.Store(plugin.Keyword, plugin)
	return plugin.Description, err
}

func listPlugin() []*Plugin {
	var list []*Plugin
	pluginMap.Range(func(key, value any) bool {
		if v, ok := value.(*Plugin); ok {
			list = append(list, v)
		}
		return true
	})
	return list
}

func removePlugin(keyword string) (ok bool) {
	_, ok = pluginMap.LoadAndDelete(keyword)
	return ok
}

func PluginManageHandle(commands []string, ctx *openwechat.MessageContext) (ok bool, err error) {
	defer func() {
		if e := recover(); e != nil {
			switch e.(type) {
			case error:
				err = e.(error)
			case string:
				err = errors.New(e.(string))
			default:
				err = errors.New("加载出错")
			}
		}
	}()
	switch commands[0] {
	case "load":
		if len(commands) == 1 {
			return false, errors.New("请输入插件路径")
		}
		subCommands := strings.SplitN(commands[1], " ", 3)
		keyword := ""
		pluginPath := ""
		if len(subCommands) == 1 {
			pluginPath = subCommands[0]
		} else {
			keyword = subCommands[0]
			pluginPath = subCommands[1]
		}
		info, err := bindPlugin(keyword, pluginPath)
		if err != nil {
			return false, err
		}
		_, _ = ctx.ReplyText("插件加载成功，信息如下:\n" + info)
		return true, nil
	case "del":
		if len(commands) == 1 {
			return false, errors.New("请输入插件唤醒词")
		}
		keyword := strings.SplitN(commands[1], " ", 2)[0]
		if removePlugin(keyword) {
			_, _ = ctx.ReplyText("插件卸载成功")
		} else {
			_, _ = ctx.ReplyText("未找到插件信息")
		}
		return true, nil
	case "list":
		list := listPlugin()
		switch len(list) {
		case 0:
			_, _ = ctx.ReplyText("当前没有加载插件")
		default:
			msg := "已加载的插件信息如下:\n"
			for i := range list {
				msg += fmt.Sprintf("[%s]:%s\n", list[i].Keyword, list[i].Description)
			}
			_, _ = ctx.ReplyText(msg)
		}
		return true, nil
	}
	return false, nil
}

func InvokePlugin(keyword string, params []string, ctx *openwechat.MessageContext) (ok bool, err error) {
	defer func() {
		if e := recover(); e != nil {
			switch e.(type) {
			case error:
				err = e.(error)
			case string:
				err = errors.New(e.(string))
			default:
				err = errors.New("插件调用出错:" + keyword)
			}
		}
	}()
	v, ok := pluginMap.Load(keyword)
	if !ok {
		return false, nil
	}
	plugin, ok := v.(*Plugin)
	if !ok {
		return false, nil
	}
	ctx.Set("pluginParams", params)
	return plugin.Fn(db, ctx)
}
