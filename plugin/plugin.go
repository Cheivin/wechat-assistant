package plugin

import (
	"github.com/eatmoreapple/openwechat"
	"gorm.io/gorm"
)

type (
	Info struct {
		ID          string `gorm:"primary"` // 插件id
		Package     string ``               // 包名
		Code        string ``               // 加载内容
		Keyword     string ``               // 唤醒词
		Description string ``               // 描述
	}

	Plugin interface {
		Info() Info
		ID() string
		Equals(plugin Plugin) bool
		HashCode() string
		Keyword(...string) string
		Init(db *gorm.DB) error
		Destroy(db *gorm.DB) error
		Handle(db *gorm.DB, ctx *openwechat.MessageContext) (bool, error)
	}
)
