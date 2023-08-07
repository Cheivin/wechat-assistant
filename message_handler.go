package main

import (
	"errors"
	"fmt"
	"github.com/eatmoreapple/openwechat"
	"golang.org/x/time/rate"
	"os"
	"strconv"
	"strings"
	"time"
)

var (
	totpSecret = "MZXW6YTBOI======"
	groupLimit = rate.NewLimiter(rate.Every(2*time.Second), 1)
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
		case "插件":
			if len(commands) == 1 {
				return
			}
			ok, err = handlePlugin(commands[1], ctx)
		case "定时任务":
			if len(commands) == 1 {
				return
			}
			ok, err = handleTask(commands[1], ctx)
		case "help":
			addons, _ := pluginManager.List(false)
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
			ok, err = true, nil
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

func handlePlugin(content string, ctx *openwechat.MessageContext) (ok bool, err error) {
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

		plugin, err := pluginManager.Install(pluginPath)
		if err != nil {
			return false, errors.New("安装插件出错:" + err.Error())
		}

		description := "插件安装成功，信息如下:\n"
		info := plugin.Info()
		description += "ID:" + plugin.ID() + "\n"
		if info.Keyword != "" {
			description += "默认唤醒词:" + info.Keyword + "\n"
		}
		if info.Description != "" {
			description += "说明:" + info.Description + "\n"
		}
		_, _ = ctx.ReplyText(description)
		return true, nil
	case "update":
		if len(commands) == 1 {
			return false, errors.New("更新插件出错:请输入插件ID")
		}
		params := strings.SplitN(commands[1], " ", 3)
		id := params[0]
		pluginPath := ""
		if len(params) > 1 {
			pluginPath = params[1]
		}
		err := pluginManager.Update(id, pluginPath)
		if err != nil {
			return false, errors.New("更新插件出错:" + err.Error())
		}
		_, _ = ctx.ReplyText(fmt.Sprintf("插件%s更新完成", id))
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
		plugin, err := pluginManager.Load(id)
		if err != nil {
			return false, err
		}
		err = pluginManager.Bind(keyword, plugin, force)

		info := plugin.Info()
		description := "插件绑定成功，信息如下:\n"
		description += "ID:" + plugin.ID() + "\n"
		description += "唤醒词:" + info.Keyword + "\n"
		if info.Description != "" {
			description += "说明:" + info.Description + "\n"
		}
		_, _ = ctx.ReplyText(description)
		return true, err
	case "unbind":
		if len(commands) == 1 {
			return false, errors.New("解绑插件出错:请输入唤醒词")
		}
		keyword := strings.SplitN(commands[1], " ", 2)[0]
		ok, err := pluginManager.Unbind(keyword)
		if !ok {
			return true, errors.New(fmt.Sprintf("解绑插件出错:唤醒词[%s]未绑定插件", keyword))
		}
		if err != nil {
			return true, errors.New(fmt.Sprintf("解绑插件出错:%s", err.Error()))
		}
		_, _ = ctx.ReplyText("插件解绑成功")
		return true, nil
	case "reload":
		if len(commands) == 1 {
			return false, errors.New("重载插件出错:请输入插件ID")
		}
		params := strings.SplitN(commands[1], " ", 2)
		id := params[0]
		if err := pluginManager.Reload(id); err != nil {
			return false, errors.New("重载插件出错:" + err.Error())
		}
		_, _ = ctx.ReplyText(fmt.Sprintf("插件%s重载完成", id))
		return true, nil
	case "uninstall":
		if len(commands) == 1 {
			return false, errors.New("请输入插件ID")
		}
		id := strings.SplitN(commands[1], " ", 2)[0]
		if ok, err := pluginManager.Uninstall(id); err != nil {
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
		addons, err := pluginManager.List(fromDB)
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
	}
	return false, nil
}

func handleTask(content string, ctx *openwechat.MessageContext) (ok bool, err error) {
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
				err = errors.New("定时任务操作出错")
			}
		}
	}()
	commands := subCommands[1:]
	switch commands[0] {
	case "install":
		if len(commands) == 1 {
			return false, errors.New("安装任务出错:请输入安装路径")
		}
		params := strings.SplitN(commands[1], " ", 3)
		installPath := ""
		if len(params) == 1 {
			installPath = params[0]
		}

		program, err := taskManager.Install(installPath)
		if err != nil {
			return false, errors.New("安装任务出错:" + err.Error())
		}

		description := "插件安装成功，信息如下:\n"
		info := program.Info()
		description += "ID:" + program.ID() + "\n"
		if info.Description != "" {
			description += "说明:" + info.Description + "\n"
		}
		_, _ = ctx.ReplyText(description)
		return true, nil
	case "update":
		if len(commands) == 1 {
			return false, errors.New("更新任务出错:请输入任务ID")
		}
		params := strings.SplitN(commands[1], " ", 3)
		id := params[0]
		installPath := ""
		if len(params) > 1 {
			installPath = params[1]
		}
		err := taskManager.Update(id, installPath)
		if err != nil {
			return false, errors.New("更新任务出错:" + err.Error())
		}
		_, _ = ctx.ReplyText(fmt.Sprintf("任务%s更新完成", id))
		return true, nil
	case "on":
		if len(commands) == 1 {
			return false, errors.New("启动任务出错:请输入任务ID和触发周期")
		}
		params := strings.SplitN(commands[1], " ", 2)
		if len(params) != 2 {
			return false, errors.New("启动任务出错:请输入任务ID和触发周期")
		}
		id := params[0]
		spec := params[1]

		program, err := taskManager.Load(id)
		if err != nil {
			return false, err
		}
		sender, _ := ctx.Sender()
		err = taskManager.Bind(program, spec, sender.UserName)

		info := program.Info()
		description := "任务启动成功，信息如下:\n"
		description += "定时任务ID:" + program.ID() + "\n"
		description += "触发周期:" + spec + "\n"
		if info.Description != "" {
			description += "说明:" + info.Description + "\n"
		}
		_, _ = ctx.ReplyText(description)
		return true, err
	case "off":
		if len(commands) == 1 {
			return false, errors.New("停止任务出错:请输入定时任务ID")
		}
		id, err := strconv.Atoi(strings.SplitN(commands[1], " ", 2)[0])
		if err != nil {
			return false, errors.New("停止任务出错:任务ID格式错误")
		}
		ok, err := taskManager.Unbind(id)
		if !ok {
			return true, errors.New(fmt.Sprintf("停止任务出错:定时任务[%d]未加载", id))
		}
		if err != nil {
			return true, errors.New(fmt.Sprintf("停止任务出错:%s", err.Error()))
		}
		_, _ = ctx.ReplyText("任务停止成功")
		return true, nil
	case "reload":
		if len(commands) == 1 {
			return false, errors.New("重载任务出错:请输入任务ID")
		}
		params := strings.SplitN(commands[1], " ", 2)
		id := params[0]
		if err := taskManager.Reload(id); err != nil {
			return false, errors.New("重载任务出错:" + err.Error())
		}
		_, _ = ctx.ReplyText(fmt.Sprintf("任务%s重载完成", id))
		return true, nil
	case "uninstall":
		if len(commands) == 1 {
			return false, errors.New("请输入任务ID")
		}
		id := strings.SplitN(commands[1], " ", 2)[0]
		if ok, err := taskManager.Uninstall(id); err != nil {
			return false, errors.New("卸载任务出错:" + err.Error())
		} else if ok {
			_, _ = ctx.ReplyText("任务卸载成功")
		} else {
			_, _ = ctx.ReplyText("未找到任务信息")
		}
		return true, nil
	case "list":
		fromDB := false
		if len(commands) > 1 {
			fromDB = "installed" == strings.SplitN(commands[1], " ", 2)[0]
		}
		sender, _ := ctx.Sender()
		infos, err := taskManager.List(sender.UserName, fromDB)
		if err != nil {
			return false, errors.New("查询已加载的插件列表出错")
		}
		switch len(*infos) {
		case 0:
			_, _ = ctx.ReplyText("当前没有任务")
		default:
			msg := "加载的任务信息如下:\n"
			for _, v := range *infos {
				if v.Spec == "" {
					msg += fmt.Sprintf("定时任务ID:%d(未启动)\n", v.ID)
					msg += fmt.Sprintf("--任务ID:[%s]\n", v.TaskID)
				} else {
					msg += fmt.Sprintf("ID:%d\n", v.ID)
					msg += fmt.Sprintf("--任务ID:[%s]\n", v.TaskID)
					msg += fmt.Sprintf("--触发周期:[%s]\n", v.Spec)
				}
			}
			_, _ = ctx.ReplyText(msg)
		}
		return true, nil
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

	if ok, err := pluginManager.Invoke(keyword, pluginParams, db, ctx); err != nil {
		return false, errors.New("调用插件出错:" + err.Error())
	} else if ok {
		return true, nil
	} else {
		// 尝试调用特殊插件
		ok, err = pluginManager.Invoke("default", append([]string{command}, params...), db, ctx)
		if err != nil {
			// 仅打印，不做特殊处理
			fmt.Println("调用插件出错:" + err.Error())
		}
		return ok, nil
	}
}
