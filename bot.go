package main

import (
	"fmt"
	"github.com/eatmoreapple/openwechat"
	"github.com/mdp/qrterminal/v3"
	"github.com/robfig/cron/v3"
	"os"
)

type BotManager struct {
	Data       string          `value:"bot.data"`
	Bot        *openwechat.Bot `aware:"bot"`
	MsgHandler *MsgHandler     `aware:""`
}

func (b *BotManager) AfterPropertiesSet() {
	// 注册登陆二维码回调
	b.Bot.UUIDCallback = func(uuid string) {
		fmt.Println(openwechat.GetQrcodeUrl(uuid))
		qrterminal.Generate("https://login.weixin.qq.com/l/"+uuid, qrterminal.L, os.Stdout)
	}
	// 注册消息处理器
	b.Bot.MessageHandler = b.MsgHandler.GetHandler()
}

func (b *BotManager) Initialized() {
	// 创建热存储容器对象
	reloadStorage := openwechat.NewFileHotReloadStorage(b.Data)
	defer reloadStorage.Close()

	// 执行热登录
	if err := b.Bot.HotLogin(reloadStorage, openwechat.NewRetryLoginOption()); err != nil {
		panic(err)
	}
	// 获取登陆的用户
	self, err := b.Bot.GetCurrentUser()
	if err != nil {
		panic(err)
	}

	// 获取所有的好友
	friends, err := self.Friends()
	fmt.Println(friends, err)
	// 获取所有的群组
	groups, err := self.Groups()
	for _, g := range groups.AsMembers() {
		fmt.Println("群:", g.NickName, g.DisplayName)
	}
	b.updateGroup()
}

func (b *BotManager) updateGroup() {
	c := cron.New(cron.WithSeconds(), cron.WithLogger(cron.DefaultLogger))
	c.AddFunc("@every 30m", func() {
		self, _ := b.Bot.GetCurrentUser()
		self.Groups(true)
		fmt.Println("更新群信息")
	})
	c.Start()
}
