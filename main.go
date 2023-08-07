package main

import (
	"fmt"
	"github.com/eatmoreapple/openwechat"
	"github.com/mdp/qrterminal/v3"
	"os"
	"path/filepath"
	plugin2 "wechat-assistant/plugin"
	"wechat-assistant/schedule"
)

var (
	pluginManager *plugin2.Manager
	taskManager   *schedule.Manager
)

func main() {
	bot := openwechat.DefaultBot(openwechat.Desktop) // 桌面模式
	// 注册登陆二维码回调
	bot.UUIDCallback = func(uuid string) {
		fmt.Println(openwechat.GetQrcodeUrl(uuid))
		qrterminal.Generate("https://login.weixin.qq.com/l/"+uuid, qrterminal.L, os.Stdout)
	}
	// 创建热存储容器对象
	reloadStorage := openwechat.NewFileHotReloadStorage(filepath.Join(os.Getenv("DATA"), "storage.json"))
	defer reloadStorage.Close()

	// 执行热登录
	if err := bot.HotLogin(reloadStorage, openwechat.NewRetryLoginOption()); err != nil {
		fmt.Println(err)
		return
	}

	// 获取登陆的用户
	self, err := bot.GetCurrentUser()
	if err != nil {
		fmt.Println(err)
		return
	}

	// 获取所有的好友
	friends, err := self.Friends()
	fmt.Println(friends, err)
	// 获取所有的群组
	groups, err := self.Groups()
	for _, g := range groups.AsMembers() {
		fmt.Println("群:", g.NickName, g.DisplayName)
	}

	// 插件管理器
	pluginManager, err = plugin2.NewManager(db)
	if err != nil {
		panic(err)
	}
	taskManager, err = schedule.NewManager(db, bot)
	if err != nil {
		panic(err)
	}

	dispatcher := openwechat.NewMessageMatchDispatcher()
	dispatcher.SetAsync(true)

	dispatcher.OnGroup(CommandHandler)
	dispatcher.OnGroup(RecordMsgHandler)

	bot.MessageHandler = dispatcher.AsMessageHandler()

	bot.Block()
}
