package interpreter

import (
	"github.com/cheivin/di"
	"github.com/eatmoreapple/openwechat"
	"github.com/go-resty/resty/v2"
	"github.com/traefik/yaegi/interp"
	"github.com/traefik/yaegi/stdlib"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"wechat-assistant/lock"
)

type Code struct {
	Package     string              `` // 包名
	interpreter *interp.Interpreter `gorm:"-"`
}

// newInterpreter 代码解释器
func newInterpreter() *interp.Interpreter {
	interpreter := interp.New(interp.Options{}) // 初始化一个 yaegi 解释器
	_ = interpreter.Use(stdlib.Symbols)         // 容许脚本调用（简直）所有的 Go 官网 package 代码
	_ = interpreter.Use(map[string]map[string]reflect.Value{
		"github.com/eatmoreapple/openwechat/openwechat": {
			"MessageContext": reflect.ValueOf((*openwechat.MessageContext)(nil)),
			"Bot":            reflect.ValueOf((*openwechat.Bot)(nil)),
			"Self":           reflect.ValueOf((*openwechat.Self)(nil)),
			"User":           reflect.ValueOf((*openwechat.User)(nil)),
			"Members":        reflect.ValueOf((*openwechat.Members)(nil)),
			"Group":          reflect.ValueOf((*openwechat.Group)(nil)),
			"Groups":         reflect.ValueOf((*openwechat.Groups)(nil)),
			"Friend":         reflect.ValueOf((*openwechat.Friend)(nil)),
			"Friends":        reflect.ValueOf((*openwechat.Friends)(nil)),
		},
		"github.com/traefik/yaegi/interp/interp": {
			"Interpreter": reflect.ValueOf((*interp.Interpreter)(nil)),
			"New":         reflect.ValueOf(interp.New),
			"Options":     reflect.ValueOf((*interp.Options)(nil)),
		},
		"github.com/go-resty/resty/v2/v2": {
			"Client":   reflect.ValueOf((*resty.Client)(nil)),
			"Request":  reflect.ValueOf((*resty.Request)(nil)),
			"Response": reflect.ValueOf((*resty.Response)(nil)),
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
		"github.com/cheivin/di/di": {
			"DI":         reflect.ValueOf((*di.DI)(nil)),
			"ValueStore": reflect.ValueOf((*di.ValueStore)(nil)),
		},
		"wechat-assistant/lock/lock": {
			"Locker": reflect.ValueOf((*lock.Locker)(nil)),
		},
	})
	return interpreter
}

// GetCode 读取代码。可以从远程读取也可以从本地路径读取，返回包名、代码字符串、错误信息
func GetCode(pluginPath string) (string, string, error) {
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
	packageName := getPackageName(code)
	if packageName == "" {
		_, packageName = filepath.Split(filepath.Dir(pluginPath))
	}
	return packageName, code, nil
}

func getPackageName(code string) string {
	packageName := strings.SplitN(code, "\n", 2)[0]
	if strings.HasPrefix(packageName, "package ") {
		packageName = strings.TrimSpace(strings.TrimPrefix(packageName, "package "))
	}
	return packageName
}

func NewCode(packageName string, code string) (*Code, error) {
	interpreter := newInterpreter()
	// 加载
	if _, err := interpreter.Eval(code); err != nil {
		return nil, err
	}
	return &Code{
		Package:     packageName,
		interpreter: interpreter,
	}, nil
}

// FindMethod 获取代码中目标方法
func FindMethod[T any](code *Code, methodName string) (*T, error) {
	v, err := code.interpreter.Eval(code.Package + "." + methodName)
	if err == nil {
		if handler, ok := v.Interface().(T); ok {
			return &handler, err
		}
	} else {
		if strings.Contains(err.Error(), "undefined selector") {
			return nil, nil
		}
	}
	return nil, err
}
