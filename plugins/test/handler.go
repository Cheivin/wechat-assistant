package test

import (
	"github.com/eatmoreapple/openwechat"
	"gorm.io/gorm"
	"strings"
)

func Handle(db *gorm.DB, ctx *openwechat.MessageContext) (bool, error) {
	if strings.Contains(ctx.Content, "ping") {
		ctx.ReplyText("pong")
		return true, nil
	}
	return false, nil
}
