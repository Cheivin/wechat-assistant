package schedule

import (
	"context"
	"crypto/md5"
	"errors"
	"fmt"
	"github.com/eatmoreapple/openwechat"
	"gorm.io/gorm"
	"runtime"
	"wechat-assistant/interpreter"
)

type CodeTask struct {
	info        Info
	interpreter *interpreter.Code
	fn          func(ctx context.Context, db *gorm.DB, self *openwechat.Self) error
}

func (t *CodeTask) Info() Info {
	return t.info
}

func (t *CodeTask) ID() string {
	return t.info.ID
}

func (t *CodeTask) Equals(compare TaskHandler) bool {
	if t.ID() == compare.ID() {
		return t.HashCode() == compare.HashCode()
	}
	return false
}

func (t *CodeTask) HashCode() string {
	return fmt.Sprintf("%x\n", md5.Sum([]byte(t.info.Code)))
}

func (t *CodeTask) Handle(ctx context.Context, db *gorm.DB, self *openwechat.Self) error {
	return t.fn(ctx, db, self)
}

func NewCodeTask(packageName string, codeStr string) (*CodeTask, error) {
	// 加载
	code, err := interpreter.NewCode(packageName, codeStr)
	if err != nil {
		return nil, err
	}
	plugin := CodeTask{
		info: Info{
			ID:      code.Package,
			Package: code.Package,
			Code:    codeStr,
		},
		interpreter: code,
	}
	// 获取信息
	if infoFn, err := interpreter.FindMethod[func() string](code, "Info"); err == nil && infoFn != nil {
		plugin.info.Description = (*infoFn)()
	}
	// 目标方法
	if handler, err := interpreter.FindMethod[func(ctx context.Context, db *gorm.DB, self *openwechat.Self) error](code, "Handle"); err != nil {
		return nil, err
	} else if handler == nil {
		return nil, errors.New("handler not match")
	} else {
		plugin.fn = *handler
	}

	// 回收钩子
	runtime.SetFinalizer(&plugin, func(pluginObj interface{}) {
		p := pluginObj.(*CodeTask)
		fmt.Println("Code任务销毁:", p.ID())
		p.interpreter = nil
		p.fn = nil
	})

	return &plugin, nil
}
