package main

import (
	"errors"
	"fmt"
	"github.com/eatmoreapple/openwechat"
	"golang.org/x/time/rate"
	"os"
	"strings"
	"time"
)

var (
	totpSecret    = "MZXW6YTBOI======"
	groupLimit    = rate.NewLimiter(rate.Every(2*time.Second), 1)
	pluginManager *PluginManager
)

func init() {
	secret := os.Getenv("SECRET")
	if secret != "" {
		totpSecret = secret
	}
	_, err := TOTPToken(totpSecret, time.Now().Unix())
	if err != nil {
		panic(err)
	}

	// 插件管理器
	pluginManager, err = NewPluginManager(db)
	if err != nil {
		panic(err)
	}
	// 加载初始插件
	addons, err := pluginManager.ListPlugin(true)
	if err != nil {
		panic(err)
	}
	for i := range *addons {
		addon := (*addons)[i]
		// 未绑定的忽略掉
		if addon.BindKeyword == "" {
			continue
		}
		plugin, err := pluginManager.LoadPlugin(addon.ID)
		if err != nil {
			fmt.Println(fmt.Sprintf("加载插件出错 id:%s, err:%s", plugin.ID, err.Error()))
		}
		if err = pluginManager.BindPlugin(addon.BindKeyword, plugin, true); err != nil {
			fmt.Println(fmt.Sprintf("绑定插件出错 id:%s, bindKeyword:%s, err:%s", plugin.ID, addon.BindKeyword, err.Error()))
		}
		fmt.Println(fmt.Sprintf("已启用插件 id:%s, bindKeyword:%s", plugin.ID, addon.BindKeyword))
	}
}

func RecordMsgHandler(ctx *openwechat.MessageContext) {
	if ctx.IsSystem() {
		return
	}

	err := StatisticGroup(ctx.Message)
	if err != nil {
		fmt.Println("记录消息出错", err)
	}
	ctx.Abort()
}

func CommandHandler(ctx *openwechat.MessageContext) {
	if ctx.IsSystem() || !ctx.IsText() || !ctx.IsAt() {
		return
	}
	sender, _ := ctx.Sender()
	receiver := sender.MemberList.SearchByUserName(1, ctx.ToUserName)
	if receiver != nil {
		// 限流
		if !groupLimit.Allow() {
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

		var ok bool
		var err error
		switch commands[0] {
		case "龙王":
			ok, err = dragon(ctx)
		case "龙王排名":
			ok, err = dragonRank(ctx)
		case "插件":
			if len(commands) == 1 {
				return
			}
			ok, err = plugin(commands[1], ctx)
		default:
			ok, err = Invoke(commands[0], commands[1:], ctx)
		}

		if err != nil {
			_, _ = ctx.ReplyText("调用插件出错:" + err.Error())
			ctx.Abort()
			_ = ctx.AsRead()
			return
		} else if ok {
			ctx.Abort()
			_ = ctx.AsRead()
		}
	}
}

func dragon(ctx *openwechat.MessageContext) (bool, error) {
	sender, _ := ctx.Sender()
	records, err := TopN(sender.UserName, 1)
	if err != nil {
		return false, errors.New("查询出错啦~")
	}
	if len(*records) == 0 {
		_, _ = ctx.ReplyText("今日龙王还没出现~")
		return true, nil
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
		return true, nil
	}
}

func dragonRank(ctx *openwechat.MessageContext) (bool, error) {
	sender, _ := ctx.Sender()
	records, err := TopN(sender.UserName, 10)
	if err != nil {
		return false, errors.New("查询出错啦~")
	} else if len(*records) == 0 {
		_, _ = ctx.ReplyText("今日龙王排名还未产生~")
		return true, nil
	} else {
		msg := "今日水群排名如下:\n"
		for i := range *records {
			rank := (*records)[i]
			if dragon := sender.MemberList.SearchByUserName(1, rank.UID); dragon != nil {
				msg += fmt.Sprintf("%d. @%s\u2005, 水群 %d 条消息\n", i+1, dragon.First().NickName, rank.Total)
			} else {
				msg += fmt.Sprintf("%d. %s, 水群 %d 条消息\n", i+1, rank.Username, rank.Total)
			}
		}
		msg += "感谢这些水王为本群做出的贡献~"
		_, _ = ctx.ReplyText(msg)
		return true, nil
	}
}

func plugin(content string, ctx *openwechat.MessageContext) (ok bool, err error) {
	// xxxxxx 指令 参数...
	subCommands := strings.SplitN(content, " ", 3)
	if len(subCommands) < 2 {
		return
	}
	if subCommands[0] != "000000" && !TOTPVerify(totpSecret, 30, subCommands[0]) {
		fmt.Println("验证失败", time.Now().Format(time.DateTime), subCommands[0])
		return
	}
	defer func() {
		if e := recover(); e != nil {
			switch e.(type) {
			case error:
				err = e.(error)
			case string:
				err = errors.New(e.(string))
			default:
				err = errors.New("插件操作出错")
			}
		}
	}()
	commands := subCommands[1:]
	switch commands[0] {
	case "install":
		if len(commands) == 1 {
			return false, errors.New("安装插件出错:请输入插件路径")
		}
		params := strings.SplitN(commands[1], " ", 3)
		pluginPath := ""
		if len(params) == 1 {
			pluginPath = params[0]
		}

		plugin, err := pluginManager.InstallPlugin(pluginPath)
		if err != nil {
			return false, errors.New("安装插件出错:" + err.Error())
		}

		description := "插件安装成功，信息如下:\n"
		description += "ID:" + plugin.ID + "\n"
		if plugin.Keyword != "" {
			description += "默认唤醒词:" + plugin.Keyword + "\n"
		}
		if plugin.Description != "" {
			description += "说明:" + plugin.Description + "\n"
		}
		_, _ = ctx.ReplyText(description)
		return true, nil
	case "bind":
		if len(commands) == 1 {
			return false, errors.New("绑定插件出错:请输入插件ID和唤醒词")
		}
		params := strings.SplitN(commands[1], " ", 3)
		id := ""
		keyword := ""
		force := false
		switch len(params) {
		case 1:
			id = params[0]
		case 2:
			id = params[0]
			keyword = params[1]
		default:
			id = params[0]
			keyword = params[1]
			force = "force" == params[2]
		}
		plugin, err := pluginManager.LoadPlugin(id)
		if err != nil {
			return false, err
		}
		err = pluginManager.BindPlugin(keyword, plugin, force)

		description := "插件绑定成功，信息如下:\n"
		description += "ID:" + plugin.ID + "\n"
		if plugin.Keyword != "" {
			description += "唤醒词:" + plugin.Keyword + "\n"
		}
		if plugin.Description != "" {
			description += "说明:" + plugin.Description + "\n"
		}
		_, _ = ctx.ReplyText(description)
		return true, err
	case "unbind":
		if len(commands) == 1 {
			return false, errors.New("解绑插件出错:请输入唤醒词")
		}
		keyword := strings.SplitN(commands[1], " ", 2)[0]
		ok, err := pluginManager.UnbindPlugin(keyword)
		if !ok {
			return true, errors.New(fmt.Sprintf("解绑插件出错:唤醒词[%s]未绑定插件", keyword))
		}
		if err != nil {
			return true, errors.New(fmt.Sprintf("解绑插件出错:%s", err.Error()))
		}
		return true, err
	case "uninstall":
		if len(commands) == 1 {
			return false, errors.New("请输入插件ID")
		}
		id := strings.SplitN(commands[1], " ", 2)[0]
		if ok, err := pluginManager.UninstallPlugin(id); err != nil {
			return false, errors.New("卸载插件出错:" + err.Error())
		} else if ok {
			_, _ = ctx.ReplyText("插件卸载成功")
		} else {
			_, _ = ctx.ReplyText("未找到插件信息")
		}
		return true, nil
	case "list":
		fromDB := false
		if len(commands) > 1 {
			fromDB = "installed" == strings.SplitN(commands[1], " ", 2)[0]
		}
		if fromDB {
			addons, err := pluginManager.ListPlugin(fromDB)
			if err != nil {
				return false, errors.New("查询已安装的插件列表出错")
			}
			switch len(*addons) {
			case 0:
				_, _ = ctx.ReplyText("当前没有安装插件")
			default:
				msg := "已安装的插件信息如下:\n"
				for _, v := range *addons {
					if v.BindKeyword == "" {
						msg += fmt.Sprintf("ID:%s(未绑定)\n", v.ID)
						if v.Keyword != "" {
							msg += fmt.Sprintf("--默认唤醒词:[%s]\n", v.Keyword)
						}
						if v.Description != "" {
							msg += fmt.Sprintf("--说明:%s\n", v.Description)
						}
					} else {
						msg += fmt.Sprintf("ID:%s, 唤醒词:[%s]\n", v.ID, v.Keyword)
						if v.Description != "" {
							msg += fmt.Sprintf("--说明:%s\n", v.Description)
						}
					}
				}
				_, _ = ctx.ReplyText(msg)
			}
			return true, nil
		} else {
			addons, _ := pluginManager.ListPlugin(fromDB)
			switch len(*addons) {
			case 0:
				_, _ = ctx.ReplyText("当前没有加载插件")
			default:
				msg := "已加载的插件信息如下:\n"
				for _, v := range *addons {
					msg += fmt.Sprintf("[%s]:%s\n", v.BindKeyword, v.Description)
				}
				_, _ = ctx.ReplyText(msg)
			}
			return true, nil
		}
	}
	return false, nil
}

func Invoke(command string, params []string, ctx *openwechat.MessageContext) (bool, error) {
	// 换行符分隔
	keyword := command
	pluginParams := make([]string, 0, len(params))
	if strings.Contains(command, "\n") {
		parts := strings.SplitN(command, "\n", 2)
		keyword = parts[0]
		if len(parts) == 2 {
			pluginParams = append(pluginParams, parts[1])
		}
		pluginParams = append(pluginParams, params...)
	} else {
		if len(params) > 0 {
			pluginParams = append(pluginParams, params...)
		}
	}

	if ok, err := pluginManager.InvokePlugin(keyword, pluginParams, db, ctx); err != nil {
		_, _ = ctx.ReplyText("调用插件出错:" + err.Error())
		ctx.Abort()
		return false, errors.New("调用插件出错:" + err.Error())
	} else if ok {
		return true, nil
	} else {
		// 尝试调用特殊插件
		ok, err = pluginManager.InvokePlugin("default", append([]string{command}, params...), db, ctx)
		if err != nil {
			// 仅打印，不做特殊处理
			fmt.Println("调用插件出错:" + err.Error())
		}
		return ok, nil
	}
}