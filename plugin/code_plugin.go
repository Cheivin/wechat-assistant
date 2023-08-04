package plugin

import (
	"crypto/md5"
	"errors"
	"fmt"
	"github.com/eatmoreapple/openwechat"
	"gorm.io/gorm"
	"runtime"
	"wechat-assistant/interpreter"
)

type CodePlugin struct {
	info        Info
	interpreter *interpreter.Code                                               `gorm:"-"`
	fn          func(db *gorm.DB, ctx *openwechat.MessageContext) (bool, error) `gorm:"-"` // 执行
	initFn      func(*gorm.DB) error                                            `gorm:"-"` // 初始方法
	destroyFn   func(*gorm.DB) error                                            `gorm:"-"` // 销毁方法
}

func (p *CodePlugin) Info() Info {
	return p.info
}

func (p *CodePlugin) ID() string {
	return p.info.ID
}

func (p *CodePlugin) Equals(compare Plugin) bool {
	if p.ID() == compare.ID() {
		return p.HashCode() == compare.HashCode()
	}
	return false
}

func (p *CodePlugin) Keyword(keyword ...string) string {
	if len(keyword) > 0 {
		if keyword[0] != "" {
			p.info.Keyword = keyword[0]
		}
	}
	return p.info.Keyword
}

func (p *CodePlugin) HashCode() string {
	return fmt.Sprintf("%x\n", md5.Sum([]byte(p.info.Code)))
}

func (p *CodePlugin) Init(db *gorm.DB) error {
	if p.initFn != nil {
		return p.initFn(db)
	}
	return nil
}

func (p *CodePlugin) Destroy(db *gorm.DB) error {
	if p.destroyFn != nil {
		return p.destroyFn(db)
	}
	return nil
}

func (p *CodePlugin) Handle(db *gorm.DB, ctx *openwechat.MessageContext) (bool, error) {
	return p.fn(db, ctx)
}

func NewCodePlugin(packageName string, codeStr string) (*CodePlugin, error) {
	// 加载
	code, err := interpreter.NewCode(packageName, codeStr)
	if err != nil {
		return nil, err
	}
	plugin := CodePlugin{
		info: Info{
			ID:      code.Package,
			Package: code.Package,
			Code:    codeStr,
		},
		interpreter: code,
	}
	// 获取信息
	if infoFn, err := interpreter.FindMethod[func() (string, string)](code, "Info"); err == nil && infoFn != nil {
		plugin.info.Keyword, plugin.info.Description = (*infoFn)()
	}
	// 目标方法
	if handler, err := interpreter.FindMethod[func(db *gorm.DB, ctx *openwechat.MessageContext) (bool, error)](code, "Handle"); err != nil {
		return nil, err
	} else if handler == nil {
		return nil, errors.New("handler not match")
	} else {
		plugin.fn = *handler
	}

	// 初始化方法
	if initFn, err := interpreter.FindMethod[func(*gorm.DB) error](code, "Init"); err == nil && initFn != nil {
		plugin.initFn = *initFn
	}
	// 销毁方法
	if destroyFn, err := interpreter.FindMethod[func(*gorm.DB) error](code, "Destroy"); err == nil && destroyFn != nil {
		plugin.destroyFn = *destroyFn
	}

	// 回收钩子
	runtime.SetFinalizer(&plugin, func(pluginObj interface{}) {
		p := pluginObj.(*CodePlugin)
		fmt.Println("Code插件销毁:", p.ID())
		p.interpreter = nil
		p.fn = nil
		p.initFn = nil
		p.destroyFn = nil
	})

	return &plugin, nil
}
