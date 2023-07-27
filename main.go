package main

import (
	"fmt"
	"github.com/eatmoreapple/openwechat"
	"github.com/mdp/qrterminal/v3"
	"golang.org/x/time/rate"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var (
	totpSecret = "MZXW6YTBOI======"
	r          = rate.NewLimiter(rate.Every(3*time.Second), 1)
)

func init() {
	secret := os.Getenv("SECRET")
	if secret != "" {
		totpSecret = secret
	}
	_, err := TOTPToken(totpSecret, uint64(time.Now().Unix()))
	if err != nil {
		panic(err)
	}
}

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

	dispatcher := openwechat.NewMessageMatchDispatcher()
	dispatcher.OnGroup(func(ctx *openwechat.MessageContext) {
		if ctx.IsSystem() || !ctx.IsText() || !ctx.IsAt() {
			return
		}
		sender, _ := ctx.Sender()
		receiver := sender.MemberList.SearchByUserName(1, ctx.ToUserName)
		if receiver != nil {
			// 限流
			if !r.Allow() {
				ctx.Abort()
				return
			}
			displayName := receiver.First().DisplayName
			if displayName == "" {
				displayName = receiver.First().NickName
			}
			var atFlag string
			msgContent := strings.TrimSpace(openwechat.FormatEmoji(ctx.Content))
			atName := openwechat.FormatEmoji(displayName)
			if strings.Contains(msgContent, "\u2005") {
				atFlag = "@" + atName + "\u2005"
			} else {
				atFlag = "@" + atName
			}
			content := strings.TrimSpace(strings.TrimPrefix(msgContent, atFlag))
			commands := strings.SplitN(content, " ", 2)
			switch commands[0] {
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
						if dragon := sender.MemberList.SearchByUserName(1, rank.UID); dragon != nil {
							msg += fmt.Sprintf("%d. @%s\u2005, 水群 %d 条消息\n", i+1, dragon.First().NickName, rank.Total)
						} else {
							msg += fmt.Sprintf("%d. %s, 水群 %d 条消息", i+1, rank.Username, rank.Total)
						}
					}
					msg += "感谢这些水王为本群做出的贡献~"
					_, _ = ctx.ReplyText(msg)
				}
				ctx.Abort()
			case "插件":
				if len(commands) == 1 {
					return
				}
				// xxxxxx 指令 参数...
				subCommands := strings.SplitN(commands[1], " ", 3)
				if len(subCommands) < 2 {
					return
				}
				if !TOTPVerify(totpSecret, 30, subCommands[0]) {
					return
				}
				if ok, err := PluginManageHandle(subCommands[1:], ctx); err != nil {
					_, _ = ctx.ReplyText("指令执行出错:" + err.Error())
					ctx.Abort()
				} else if ok {
					ctx.Abort()
				}
			default:
				// 插件机制
				if ok, err := InvokePlugin(commands[0], commands[1:], ctx); err != nil {
					_, _ = ctx.ReplyText("调用插件出错:" + err.Error())
					ctx.Abort()
				} else if ok {
					ctx.Abort()
				}
			}
			if ctx.IsAbort() {
				_ = ctx.AsRead()
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
