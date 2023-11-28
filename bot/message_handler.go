package bot

import (
	"errors"
	"fmt"
	"github.com/eatmoreapple/openwechat"
	"golang.org/x/time/rate"
	"gorm.io/gorm"
	"strings"
	"time"
	"wechat-assistant/plugin"
	"wechat-assistant/redirect"
	"wechat-assistant/util/totp"
)

type MsgHistory struct {
	ID         uint   `gorm:"primaryKey;autoIncrement"`
	GID        string `gorm:"type:varchar(255)"`
	UID        string `gorm:"type:varchar(255)"`
	AttrStatus int64  `gorm:"type:int(20)"`
	MsgType    int    `gorm:"type:int(2)"`
	GroupName  string `gorm:"type:varchar(255)"`
	Username   string `gorm:"type:varchar(255)"`
	WechatName string `gorm:"type:varchar(255)"`
	Message    string ``
	Time       int64  `gorm:"type:int(20)"`
}

type MsgHandler struct {
	Secret        string               `value:"bot.secret"`
	DB            *gorm.DB             `aware:"db"`
	PluginManager *plugin.Manager      `aware:""`
	MsgRedirect   redirect.MsgRedirect `aware:"omitempty"`
	limit         *rate.Limiter
}

func (h *MsgHandler) BeanName() string {
	return "msgHandler"
}

func (h *MsgHandler) BeanConstruct() {
	h.limit = rate.NewLimiter(rate.Every(1*time.Second), 1)
}

func (h *MsgHandler) AfterPropertiesSet() {
	if err := h.DB.AutoMigrate(MsgHistory{}); err != nil {
		panic(err)
	}
	if _, err := totp.TOTPToken(h.Secret, time.Now().Unix()); err != nil {
		panic(err)
	}
}

func (h *MsgHandler) GetHandler() openwechat.MessageHandler {
	dispatcher := openwechat.NewMessageMatchDispatcher()
	// 开启异步消息处理
	dispatcher.SetAsync(true)
	dispatcher.OnGroup(h.CommandHandler)
	dispatcher.OnGroup(h.RecordMsgHandler)
	return dispatcher.AsMessageHandler()
}

func (h *MsgHandler) recordHistory(msg *openwechat.Message) error {
	group, err := msg.Sender()
	if err != nil {
		return err
	}
	user, err := msg.SenderInGroup()
	if err != nil {
		return err
	}
	username := user.DisplayName
	if user.DisplayName == "" {
		username = user.NickName
	}
	content := strings.TrimSpace(openwechat.FormatEmoji(msg.Content))
	if h.MsgRedirect != nil {
		go func() {
			h.MsgRedirect.RedirectMessage(redirect.Message{
				MsgID:      msg.MsgId,
				UID:        user.UserName,
				Username:   username,
				GID:        group.UserName,
				GroupName:  group.NickName,
				RawMessage: content,
				MsgType:    int(msg.MsgType),
				Time:       msg.CreateTime,
			})
		}()
	}
	record := &MsgHistory{
		GID:        group.UserName,
		UID:        user.UserName,
		AttrStatus: user.AttrStatus,
		MsgType:    int(msg.MsgType),
		GroupName:  group.NickName,
		Username:   username,
		WechatName: user.NickName,
		Message:    content,
		Time:       msg.CreateTime,
	}
	return h.DB.Save(record).Error
}

func (h *MsgHandler) RecordMsgHandler(ctx *openwechat.MessageContext) {
	if ctx.IsSystem() {
		return
	}
	if ctx.IsSendBySelf() {
		return
	}
	if err := h.recordHistory(ctx.Message); err != nil {
		fmt.Println("记录消息出错", err)
	}
	ctx.Abort()
}

func (h *MsgHandler) dealCommand(ctx *openwechat.MessageContext, command string, content string) {
	//commands := strings.SplitN(content, " ", 2)

	var ok bool
	var err error
	switch command {
	case "插件":
		if content == "" {
			return
		}
		ok, err = h.handlePlugin(content, ctx)
	case "help":
		addons, _ := h.PluginManager.List(false)
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
		if content == "" {
			ok, err = h.Invoke(command, []string{}, ctx)
		} else {
			ok, err = h.Invoke(command, []string{content}, ctx)
		}
	}

	if err != nil {
		_, _ = ctx.ReplyText("调用插件出错:" + err.Error())
		_ = ctx.AsRead()
		ctx.Abort()
		return
	} else if ok {
		_ = ctx.AsRead()
		ctx.Abort()
	}
}

func (h *MsgHandler) CommandHandler(ctx *openwechat.MessageContext) {
	if ctx.IsSystem() || ctx.IsSendBySelf() || !ctx.IsText() {
		return
	}
	if ctx.IsAt() {
		sender, _ := ctx.Sender()
		receiver := sender.MemberList.SearchByUserName(1, ctx.ToUserName)
		if receiver == nil {
			return
		}
		// 限流
		if !h.limit.Allow() {
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
		if len(commands) == 1 {
			content = ""
		} else {
			content = commands[1]
		}
		h.dealCommand(ctx, commands[0], content)
	} else if strings.HasPrefix(ctx.Content, "#") {
		// 限流
		if !h.limit.Allow() {
			ctx.Abort()
			return
		}
		msgContent := strings.TrimSpace(openwechat.FormatEmoji(ctx.Content))
		content := strings.TrimSpace(strings.TrimPrefix(msgContent, "#"))
		commands := strings.SplitN(content, " ", 2)
		if len(commands) == 1 {
			content = ""
		} else {
			content = commands[1]
		}
		h.dealCommand(ctx, commands[0], content)
	}

}

func (h *MsgHandler) handlePlugin(content string, ctx *openwechat.MessageContext) (ok bool, err error) {
	// xxxxxx 指令 参数...
	subCommands := strings.SplitN(content, " ", 3)
	if len(subCommands) < 2 {
		return
	}
	if subCommands[0] != "000000" && !totp.TOTPVerify(h.Secret, 30, subCommands[0]) {
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

		plugin, err := h.PluginManager.Install(pluginPath)
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
		err := h.PluginManager.Update(id, pluginPath)
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
		plugin, err := h.PluginManager.Load(id)
		if err != nil {
			return false, err
		}
		err = h.PluginManager.Bind(keyword, plugin, force)

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
		ok, err := h.PluginManager.Unbind(keyword)
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
		if err := h.PluginManager.Reload(id); err != nil {
			return false, errors.New("重载插件出错:" + err.Error())
		}
		_, _ = ctx.ReplyText(fmt.Sprintf("插件%s重载完成", id))
		return true, nil
	case "uninstall":
		if len(commands) == 1 {
			return false, errors.New("请输入插件ID")
		}
		id := strings.SplitN(commands[1], " ", 2)[0]
		if ok, err := h.PluginManager.Uninstall(id); err != nil {
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
		addons, err := h.PluginManager.List(fromDB)
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

func (h *MsgHandler) Invoke(command string, params []string, ctx *openwechat.MessageContext) (bool, error) {
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

	if ok, err := h.PluginManager.Invoke(keyword, pluginParams, h.DB, ctx); err != nil {
		return false, errors.New("调用插件出错:" + err.Error())
	} else if ok {
		return true, nil
	} else {
		if h.MsgRedirect != nil {
			msg := ctx.Message
			group, err := msg.Sender()
			if err != nil {
				return false, err
			}
			user, err := msg.SenderInGroup()
			if err != nil {
				return false, err
			}
			username := user.DisplayName
			if user.DisplayName == "" {
				username = user.NickName
			}
			content := strings.TrimSpace(openwechat.FormatEmoji(msg.Content))
			ok := h.MsgRedirect.RedirectCommand(redirect.CommandMessage{
				Message: redirect.Message{
					MsgID:      msg.MsgId,
					UID:        user.UserName,
					Username:   username,
					GID:        group.UserName,
					GroupName:  group.NickName,
					RawMessage: content,
					MsgType:    int(msg.MsgType),
					Time:       msg.CreateTime,
				},
				Command: strings.Join(append([]string{command}, params...), " "),
			})
			return ok, nil
		} else {
			// 尝试调用特殊插件
			ok, err = h.PluginManager.Invoke("default", append([]string{command}, params...), h.DB, ctx)
			if err != nil {
				// 仅打印，不做特殊处理
				fmt.Println("调用插件出错:" + err.Error())
			}
		}
		return ok, nil
	}
}
