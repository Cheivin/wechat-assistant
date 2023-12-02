package bot

import (
	"bytes"
	"crypto/md5"
	"encoding/xml"
	"errors"
	"fmt"
	"github.com/eatmoreapple/openwechat"
	"golang.org/x/time/rate"
	"gorm.io/gorm"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"wechat-assistant/plugin"
	"wechat-assistant/redirect"
	"wechat-assistant/util/limiter"
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
	MsgID      string `gorm:"type:varchar(50)"`
}

type MsgHandler struct {
	Secret        string               `value:"bot.secret"`
	FilesPath     string               `value:"bot.files"`
	DB            *gorm.DB             `aware:"db"`
	PluginManager *plugin.Manager      `aware:""`
	MsgRedirect   redirect.MsgRedirect `aware:"omitempty"`
	Uploader      *redirect.S3Uploader `aware:"omitempty"`
	limit         *limiter.Limiter
}

func (h *MsgHandler) BeanName() string {
	return "msgHandler"
}

func (h *MsgHandler) BeanConstruct() {
	h.limit = limiter.NewLimiter(rate.Every(1*time.Second), 2)
}

func (h *MsgHandler) AfterPropertiesSet() {
	if err := os.MkdirAll(h.FilesPath, os.ModePerm); err != nil {
		log.Fatalln("创建缓存目录失败", err)
	}
	if err := h.DB.AutoMigrate(MsgHistory{}); err != nil {
		log.Fatalln("初始化消息记录表出错", err)
	}
	if _, err := totp.TOTPToken(h.Secret, time.Now().Unix()); err != nil {
		log.Fatalln("初始化动态密码生成器出错", err)
	}
}

func (h *MsgHandler) GetHandler() openwechat.MessageHandler {
	dispatcher := openwechat.NewMessageMatchDispatcher()
	// 开启异步消息处理
	dispatcher.SetAsync(true)
	dispatcher.OnGroup(h.checkDuplicate)
	dispatcher.OnGroup(h.preParseContent)
	dispatcher.OnGroup(h.saveMedia)
	if h.MsgRedirect != nil {
		dispatcher.OnGroup(h.redirectMsg)
	}
	dispatcher.OnGroup(h.RecordMsgHandler)
	dispatcher.OnGroup(h.CommandHandler)
	return dispatcher.AsMessageHandler()
}

func (h *MsgHandler) saveMedia(msg *openwechat.MessageContext) {
	if !msg.HasFile() {
		return
	}
	var buf bytes.Buffer
	filename := msg.FileName
	if filename == "" {
		fileExt := ""
		filename = fmt.Sprintf("%x", md5.Sum([]byte(msg.Content)))
		if msg.IsVideo() {
			fileExt = ".mp4"
		} else if msg.IsVoice() {
			fileExt = ".mp3"
		} else if msg.IsPicture() || msg.IsEmoticon() {
			if err := msg.SaveFile(&buf); err != nil {
				log.Println("获取文件失败", err)
				msg.Content = strings.TrimSpace(filename)
				return
			}
			filetype := http.DetectContentType(buf.Bytes())
			filetype = filetype[6:]
			if strings.Contains(filetype, "-") || strings.EqualFold(filetype, "text/plain") {
				fileExt = ".jpg"
			} else {
				fileExt = "." + filetype
			}
		}
		filename = filename + fileExt
	}
	filename = filepath.Join(time.Now().Format("2006/01/02"), filename)
	savePath := filepath.Join(h.FilesPath, filename)
	_ = os.MkdirAll(filepath.Dir(savePath), os.ModePerm)
	if buf.Len() > 0 {
		if h.Uploader != nil {
			go func() {
				_, err := h.Uploader.Upload(filename, bytes.NewReader(buf.Bytes()))
				if err != nil {
					log.Println("上传文件失败", filename, err)
				} else {
					log.Println("上传文件完成", filename)
				}
			}()
		}
		if err := os.WriteFile(savePath, buf.Bytes(), 0666); err != nil {
			log.Println("写入文件失败", savePath, err)
			msg.Content = strings.TrimSpace(filename)
			return
		}
	} else {
		if err := msg.SaveFileToLocal(savePath); err != nil {
			log.Println("保存文件失败", savePath, err)
			msg.Content = strings.TrimSpace(filename)
			return
		}
		if h.Uploader != nil {
			go func() {
				_, err := h.Uploader.FUpload(filename, savePath)
				if err != nil {
					log.Println("上传文件失败", filename, err)
				} else {
					log.Println("上传文件完成", filename)
				}
			}()
		}
	}
	msg.Content = strings.TrimSpace(filename)
}

func (h *MsgHandler) redirectMsg(ctx *openwechat.MessageContext) {
	if ctx.IsSystem() {
		return
	}
	group, err := ctx.Sender()
	if err != nil {
		log.Println("获取消息来源群组信息失败", err)
		return
	}
	if ctx.IsSendBySelf() {
		groups, _ := ctx.Owner().Groups()
		group = groups.SearchByUserName(1, ctx.ToUserName).First().User
	}
	user, err := ctx.SenderInGroup()
	if err != nil {
		log.Println("获取消息来源群成员信息失败", err)
		return
	}
	username := user.DisplayName
	if user.DisplayName == "" {
		username = user.NickName
	}
	msg := &redirect.Message{
		MsgID:      ctx.MsgId,
		UID:        user.UserName,
		Username:   username,
		GID:        group.UserName,
		GroupName:  group.NickName,
		RawMessage: strings.TrimSpace(ctx.Content),
		MsgType:    int(ctx.MsgType),
		Time:       ctx.CreateTime,
	}
	if quote, exist := ctx.Get(QuoteKey); exist {
		q := quote.(*QuoteMessageInfo)
		msg.RawMessage = q.Content
		msg.Quote = &redirect.Quote{
			Quote: q.Quote,
			UID:   q.User.UserName,
		}
	}
	if ctx.IsRecalled() {
		var revokeMsg SysMsg
		err := xml.Unmarshal([]byte(ctx.Content), &revokeMsg)
		if err == nil {
			msg.Revoke = &redirect.Revoke{
				OldMsgID:   revokeMsg.RevokeMsg.MsgID,
				ReplaceMsg: revokeMsg.RevokeMsg.ReplaceMsg,
			}
		}
	}

	go func(msg *redirect.Message) {
		h.MsgRedirect.RedirectMessage(msg)
	}(msg)
}

func (h *MsgHandler) RecordMsgHandler(ctx *openwechat.MessageContext) {
	_ = ctx.AsRead()
	if ctx.IsSystem() || ctx.IsSendBySelf() {
		return
	}
	msg := ctx.Message
	group, err := msg.Sender()
	if err != nil {
		log.Println("获取消息来源群组信息失败", err)
		return
	}
	user, err := msg.SenderInGroup()
	if err != nil {
		log.Println("获取消息来源群成员信息失败", err)
		return
	}
	username := user.DisplayName
	if user.DisplayName == "" {
		username = user.NickName
	}
	content := strings.TrimSpace(msg.Content)
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
		MsgID:      msg.MsgId,
	}
	if err = h.DB.Save(record).Error; err != nil {
		if !errors.Is(err, gorm.ErrDuplicatedKey) {
			log.Println("记录消息出错", err)
		}
	}
}

func (h *MsgHandler) dealCommand(ctx *openwechat.MessageContext, command string, content string) {
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
		_, _ = ctx.ReplyText(err.Error())
		ctx.Abort()
		return
	} else if ok {
		ctx.Abort()
	}
}

func (h *MsgHandler) checkDuplicate(ctx *openwechat.MessageContext) {
	if ctx.IsSystem() || ctx.IsNotify() || ctx.IsSendBySelf() {
		return
	}
	var msgId string
	h.DB.Model(MsgHistory{}).
		Select("msg_id").
		Where("msg_id =?", ctx.MsgId).
		Limit(1).
		Take(&msgId)
	if msgId != "" {
		ctx.Abort()
		log.Println("跳过重复消息", msgId)
		return
	}
}

func (h *MsgHandler) preParseContent(ctx *openwechat.MessageContext) {
	if ctx.IsSystem() || !ctx.IsText() {
		return
	}
	sender, _ := ctx.Sender()
	group, _ := sender.AsGroup()
	if ctx.IsSendBySelf() {
		groups, _ := ctx.Owner().Groups()
		group = groups.SearchByUserName(1, ctx.ToUserName).First()
	}
	if group == nil {
		return
	}

	ctx.Content = openwechat.FormatEmoji(ctx.Content)
	// 处理引用内容
	if strings.HasPrefix(ctx.Content, quotePrefix) && strings.Contains(ctx.Content, quoteSuffix) {
		// 分离引用内容和正文
		quoteContent := ctx.Content[len(quotePrefix):strings.Index(ctx.Content, quoteSuffix)]
		// 只保留正文部分
		content := strings.TrimPrefix(ctx.Content, quotePrefix+quoteContent+quoteSuffix)
		// 搜索被引用的用户
		var quoteUser *openwechat.User
		// 先搜索目标用户，正文存在@的时候可能与用户不一致，需要搜索用户
		if u, err := group.SearchMemberByUsername(ctx.ToUserName); err == nil && u != nil {
			if strings.HasPrefix(quoteContent, u.RemarkName+"：") {
				quoteContent = strings.TrimSpace(strings.TrimPrefix(quoteContent, u.RemarkName+"："))
				quoteUser = u
			} else if strings.HasPrefix(quoteContent, u.DisplayName+"：") {
				quoteContent = strings.TrimSpace(strings.TrimPrefix(quoteContent, u.DisplayName+"："))
				quoteUser = u
			} else if strings.HasPrefix(quoteContent, u.NickName+"：") {
				quoteContent = strings.TrimSpace(strings.TrimPrefix(quoteContent, u.NickName+"："))
				quoteUser = u
			}
		}
		// 如果不存在，则根据名称搜索
		if quoteUser == nil {
			members, _ := group.Members()
			// 报错直接中止引用消息处理
			if members == nil {
				return
			}
			u := members.Search(1, func(u *openwechat.User) bool {
				return strings.HasPrefix(quoteContent, u.RemarkName+"：") ||
					strings.HasPrefix(quoteContent, u.DisplayName+"：") ||
					strings.HasPrefix(quoteContent, u.NickName+"：")
			}).First()
			// 未匹配到用户也中止处理
			if u == nil {
				return
			}
			if strings.HasPrefix(quoteContent, u.RemarkName+"：") {
				quoteContent = strings.TrimSpace(strings.TrimPrefix(quoteContent, u.RemarkName+"："))
				quoteUser = u
			} else if strings.HasPrefix(quoteContent, u.DisplayName+"：") {
				quoteContent = strings.TrimSpace(strings.TrimPrefix(quoteContent, u.DisplayName+"："))
				quoteUser = u
			} else if strings.HasPrefix(quoteContent, u.NickName+"：") {
				quoteContent = strings.TrimSpace(strings.TrimPrefix(quoteContent, u.NickName+"："))
				quoteUser = u
			}
		}
		quote := &QuoteMessageInfo{
			Content: content,
			Quote:   quoteContent,
			User:    quoteUser,
		}
		ctx.Set(QuoteKey, quote)
	}
}

func (h *MsgHandler) CommandHandler(ctx *openwechat.MessageContext) {
	if ctx.IsSystem() || ctx.IsSendBySelf() || !ctx.IsText() {
		return
	}
	sender, _ := ctx.Sender()
	msgContent := strings.TrimSpace(ctx.Content)
	if ctx.IsAt() {
		receiver := sender.MemberList.SearchByUserName(1, ctx.ToUserName)
		if receiver == nil {
			return
		}
		// 限流
		if !h.limit.GetOrAdd(sender.UserName).Allow() {
			ctx.Abort()
			return
		}
		displayName := receiver.First().DisplayName
		if displayName == "" {
			displayName = receiver.First().NickName
		}
		var atFlag string
		atName := openwechat.FormatEmoji(displayName)
		if strings.Contains(msgContent, "\u2005") {
			atFlag = "@" + atName + "\u2005"
		} else {
			atFlag = "@" + atName
		}
		var quote *QuoteMessageInfo
		if val, exist := ctx.Get(QuoteKey); exist {
			quote = val.(*QuoteMessageInfo)
		}
		if quote != nil {
			msgContent = quote.Content + " " + quote.Quote
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
		if !h.limit.GetOrAdd(sender.UserName).Allow() {
			ctx.Abort()
			return
		}
		var quote *QuoteMessageInfo
		if val, exist := ctx.Get(QuoteKey); exist {
			quote = val.(*QuoteMessageInfo)
		}
		if quote != nil {
			msgContent = quote.Content + " " + quote.Quote
		}
		content := strings.TrimSpace(strings.TrimPrefix(msgContent, "#"))
		commands := strings.SplitN(content, " ", 2)
		if len(commands) == 1 {
			content = ""
		} else {
			content = commands[1]
		}
		h.dealCommand(ctx, commands[0], content)
	} else {
		var quote *QuoteMessageInfo
		if val, exist := ctx.Get(QuoteKey); !exist {
			return
		} else {
			quote = val.(*QuoteMessageInfo)
		}
		// 判断回复人是否为自己
		if quote.User.UserName != ctx.Owner().UserName {
			return
		}
		// 限流
		if !h.limit.GetOrAdd(sender.UserName).Allow() {
			ctx.Abort()
			return
		}
		content := strings.TrimSpace(quote.Content)

		// 如果正文不存在指令前缀，但引用内容指令前缀，将引用内容的指令前缀作为指令，将引用内容剩余部分追加到正文
		if !strings.HasPrefix(content, "#") && strings.HasPrefix(quote.Quote, "#") {
			commands := strings.SplitN(strings.TrimSpace(strings.TrimPrefix(quote.Quote, "#")), " ", 2)
			if len(commands) != 1 {
				content = content + " " + commands[1]
			}
			h.dealCommand(ctx, commands[0], content)
		} else { // 否则直接将引用内容加在后面
			content = strings.TrimSpace(strings.TrimPrefix(content, "#"))
			commands := strings.SplitN(content, " ", 2)
			if len(commands) == 1 {
				content = quote.Quote
			} else {
				content = commands[1] + " " + quote.Quote
			}
			h.dealCommand(ctx, commands[0], content)
		}
	}

}

func (h *MsgHandler) handlePlugin(content string, ctx *openwechat.MessageContext) (ok bool, err error) {
	// xxxxxx 指令 参数...
	subCommands := strings.SplitN(content, " ", 3)
	if len(subCommands) < 2 {
		return
	}
	if subCommands[0] != "000000" && !totp.TOTPVerify(h.Secret, 30, subCommands[0]) {
		log.Println("验证失败", time.Now().Format(time.DateTime), subCommands[0])
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
			content := strings.TrimSpace(msg.Content)
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
				log.Println("调用插件出错:" + err.Error())
			}
		}
		return ok, nil
	}
}
