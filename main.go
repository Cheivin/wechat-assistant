package main

import (
	"fmt"
	"github.com/eatmoreapple/openwechat"
	"github.com/mdp/qrterminal/v3"
	"os"
	"strings"
)

func main() {
	bot := openwechat.DefaultBot(openwechat.Desktop) // 桌面模式

	// 注册登陆二维码回调
	bot.UUIDCallback = func(uuid string) {
		//qrcodeurl := openwechat.GetQrcodeUrl(uuid)
		qrcodeurl := "https://login.weixin.qq.com/l/" + uuid
		fmt.Println(qrcodeurl)
		qrterminal.Generate(qrcodeurl, qrterminal.L, os.Stdout)
	}
	// 创建热存储容器对象
	reloadStorage := openwechat.NewFileHotReloadStorage("storage.json")
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
	groups, err := self.Groups()
	if err != nil {
		fmt.Println(err)
	}
	for _, g := range groups.AsMembers() {
		fmt.Println("群:", g.NickName, g.DisplayName)
	}

	dispatcher := openwechat.NewMessageMatchDispatcher()
	dispatcher.OnGroup(func(ctx *openwechat.MessageContext) {
		if ctx.IsSystem() || !ctx.IsText() || !ctx.IsAt() {
			return
		}
		sender, _ := ctx.Sender()
		receiver := sender.MemberList.SearchByUserName(1, ctx.ToUserName)
		if receiver != nil {
			displayName := receiver.First().DisplayName
			if displayName == "" {
				displayName = receiver.First().NickName
			}
			var atFlag string
			msgContent := openwechat.FormatEmoji(ctx.Content)
			atName := openwechat.FormatEmoji(displayName)
			if strings.Contains(msgContent, "\u2005") {
				atFlag = "@" + atName + "\u2005"
			} else {
				atFlag = "@" + atName
			}
			content := strings.TrimSpace(strings.TrimPrefix(msgContent, atFlag))

			switch content {
			case "龙王":
				records, err := TopN(sender.UserName, 1)
				if err != nil {
					_, _ = ctx.ReplyText("查询出错啦~")
				} else if len(*records) == 0 {
					_, _ = ctx.ReplyText("今日龙王还没出现~")
				} else {
					rank := (*records)[0]
					dragon := sender.MemberList.SearchByUserName(1, rank.UID)
					msg := "今天的龙王是->"
					if dragon != nil {
						msg += fmt.Sprintf(" @%s\u2005, 水群 %d 条消息", dragon.First().NickName, rank.Total)
					} else {
						msg += fmt.Sprintf(" %s, 水群 %d 条消息", rank.Username, rank.Total)
					}
					msg += ", 恭喜这个B！！！"
					_, _ = ctx.ReplyText(msg)
				}
				ctx.Abort()
			case "龙王排名":
				records, err := TopN(sender.UserName, 10)
				if err != nil {
					_, _ = ctx.ReplyText("查询出错啦~")
				} else if len(*records) == 0 {
					_, _ = ctx.ReplyText("今日龙王排名还未产生~")
				} else {
					msg := "今日水群排名如下:\n"
					for i := range *records {
						rank := (*records)[i]
						msg += fmt.Sprintf("%s, 水群 %d 条消息\n", rank.Username, rank.Total)
					}
					msg += "恭喜这些水王为本群做出的贡献~"
					_, _ = ctx.ReplyText(msg)
				}
				ctx.Abort()
			}
		}
	})

	dispatcher.OnGroup(func(ctx *openwechat.MessageContext) {
		if ctx.IsSystem() {
			return
		}

		err := StatisticGroup(ctx.Message)
		if err != nil {
			fmt.Println("记录消息出错", err)
		}
		ctx.Abort()
	})

	bot.MessageHandler = dispatcher.AsMessageHandler()

	bot.Block()
}
