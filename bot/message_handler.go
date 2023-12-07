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

type (
	MsgHistory struct {
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
)

type MsgHandler struct {
	Secret                  string                   `value:"bot.secret"`
	FilesPath               string                   `value:"bot.files"`
	DB                      *gorm.DB                 `aware:"db"`
	PluginManager           *plugin.Manager          `aware:""`
	KeywordForbiddenManager *KeywordForbiddenManager `aware:""`
	MsgRedirect             redirect.MsgRedirect     `aware:"omitempty"`
	Uploader                *redirect.S3Uploader     `aware:"omitempty"`
	limit                   *limiter.Limiter
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
		if groups != nil {
			g := groups.SearchByUserName(1, ctx.ToUserName).First()
			if g != nil {
				group = g.User
			}
		}
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
		ok, err = h.PluginManager.HandleManage(content, ctx)
	case "禁用词":
		if content == "" {
			return
		}
		ok, err = h.KeywordForbiddenManager.HandleManage(content, ctx)
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
		// 检查关键词
		ok, err := h.KeywordForbiddenManager.CheckKeyword(ctx, command)
		if err != nil {
			log.Println("检查关键词出错", command, err)
			return
		} else if !ok {
			log.Println("关键词已拦截", command, err)
			return
		}
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
