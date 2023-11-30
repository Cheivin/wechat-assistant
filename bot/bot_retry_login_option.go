package bot

import (
	"github.com/eatmoreapple/openwechat"
	"log"
)

// RetryLoginOption 在登录失败后进行扫码登录
type RetryLoginOption struct {
	openwechat.BaseBotLoginOption
	MaxRetryCount    int
	currentRetryTime int
	storage          openwechat.HotReloadStorage
	deviceId         string
}

// OnError 实现了 BotLoginOption 接口
// 当登录失败后，会调用此方法进行扫码登录
func (r *RetryLoginOption) OnError(bot *openwechat.Bot, err error) error {
	if r.currentRetryTime >= r.MaxRetryCount {
		return err
	}
	r.currentRetryTime++
	log.Println("尝试PushLogin")
	return bot.PushLogin(r.storage, openwechat.NewRetryLoginOption())
}

func NewRetryLoginOption(storage openwechat.HotReloadStorage) openwechat.BotLoginOption {
	return &RetryLoginOption{MaxRetryCount: 1, storage: storage}
}
