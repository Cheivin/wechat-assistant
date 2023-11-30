package plugin

import (
	"crypto/md5"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/eatmoreapple/openwechat"
	"github.com/go-resty/resty/v2"
	"gorm.io/gorm"
	"strings"
	"wechat-assistant/redirect"
)

// RemotePlugin 远程插件
type RemotePlugin struct {
	info   Info
	client *resty.Client
	sender *redirect.MsgSender
}

func (p *RemotePlugin) loadInfo() {
	resp, err := p.client.R().Get(p.info.Code)
	if err != nil {
		return
	}
	info := new(remotePluginInfo)
	if err = json.Unmarshal(resp.Body(), info); err != nil {
		return
	}
	p.info.Keyword = info.Keyword
	p.info.Description = info.Description
}

func (p *RemotePlugin) Info() Info {
	return p.info
}

func (p *RemotePlugin) ID() string {
	return p.info.ID
}

func (p *RemotePlugin) Equals(compare Plugin) bool {
	if p.ID() == compare.ID() {
		return p.HashCode() == compare.HashCode()
	}
	return false
}

func (p *RemotePlugin) HashCode() string {
	return fmt.Sprintf("%x\n", md5.Sum([]byte(p.info.Code)))
}

func (p *RemotePlugin) Keyword(keyword ...string) string {
	if len(keyword) > 0 {
		if keyword[0] != "" {
			p.info.Keyword = keyword[0]
		}
	}
	return p.info.Keyword
}

func (p *RemotePlugin) Init(db *gorm.DB) error {
	return nil
}

func (p *RemotePlugin) Destroy(db *gorm.DB) error {
	return nil
}

func NewRemotePlugin(packageName string, api string, client *resty.Client, sender *redirect.MsgSender) (Plugin, error) {
	plugin := &RemotePlugin{
		info: Info{
			ID:      packageName,
			Package: packageName,
			Code:    api,
		},
		client: client,
		sender: sender,
	}
	plugin.loadInfo()
	return plugin, nil
}

func (p *RemotePlugin) Handle(_ *gorm.DB, ctx *openwechat.MessageContext) (bool, error) {
	v, ok := ctx.Get("pluginParams")
	if !ok {
		return false, nil
	}
	params, ok := v.([]string)
	if !ok {
		return false, nil
	}

	msg := remotePluginRequest{
		MsgID:      ctx.MsgId,
		Time:       ctx.CreateTime,
		MsgType:    int(ctx.MsgType),
		Message:    strings.Join(params, " "),
		RawMessage: ctx.Content,
	}
	// 发送者信息
	sender, err := ctx.Sender()
	if err != nil {
		return false, nil
	}
	group, _ := sender.AsGroup()
	msg.GID = group.UserName
	if group.RemarkName != "" {
		msg.GroupName = group.RemarkName
	} else if group.DisplayName != "" {
		msg.GroupName = group.DisplayName
	} else {
		msg.GroupName = group.NickName
	}
	user, _ := ctx.SenderInGroup()
	msg.UID = user.UserName
	if user.RemarkName != "" {
		msg.Username = user.RemarkName
	} else if user.DisplayName != "" {
		msg.Username = user.DisplayName
	} else {
		msg.Username = user.NickName
	}

	resp, err := p.client.R().SetBody(msg).Post(p.info.Code)
	if err != nil {
		return false, err
	}

	response := new(remotePluginResponse)
	err = json.Unmarshal(resp.Body(), response)
	if err != nil {
		return false, err
	}
	if response.Error != "" {
		return false, errors.New(response.Error)
	}

	msgType, _ := response.Type.Int64()
	switch int(msgType) {
	case -1:
		return false, nil
	case 0:
		return true, nil
	case 1:
		_, err := p.sender.SendGroupTextMsg(group, response.Body)
		return err == nil, err
	default:
		_, err := p.sender.SendGroupMediaMsg(group, int(msgType), response.Body, response.Filename, response.Prompt)
		return err == nil, err
	}
}

type (
	remotePluginInfo struct {
		Keyword     string `json:"keyword"`
		Description string `json:"description"`
	}
	remotePluginRequest struct {
		MsgID      string `json:"msgID"`
		UID        string `json:"uid"`
		Username   string `json:"username"`
		GID        string `json:"gid"`
		GroupName  string `json:"groupName"`
		Message    string `json:"message"`
		RawMessage string `json:"rawMessage"`
		MsgType    int    `json:"msgType"`
		Time       int64  `json:"time"`
	}
	remotePluginResponse struct {
		Error    string      `json:"error"`    // 错误信息，空表示没错误
		Type     json.Number `json:"type"`     // 回复类型 -1:不处理,0:不回复,1:文本,2:图片,3:视频,4:文件
		Body     string      `json:"body"`     // 回复内容,type=1时为文本内容,type=2/3/4时为资源地址
		Filename string      `json:"filename"` // 文件名称
		Prompt   string      `json:"prompt"`   // 发送媒体资源前的提示词,会自动撤回
	}
)
