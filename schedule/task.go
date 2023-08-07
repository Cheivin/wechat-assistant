package schedule

import (
	"context"
	"github.com/eatmoreapple/openwechat"
	"gorm.io/gorm"
)

type (
	Info struct {
		ID          string `gorm:"primary"` // 任务id
		Package     string ``               // 包名
		Code        string ``               // 加载内容
		Description string ``               // 描述
	}

	TaskHandler interface {
		Info() Info
		ID() string
		Equals(task TaskHandler) bool
		HashCode() string
		Handle(ctx context.Context, db *gorm.DB, self *openwechat.Self) error
	}
)
