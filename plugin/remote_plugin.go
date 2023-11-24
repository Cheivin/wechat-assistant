package plugin

import (
	"bytes"
	"crypto/md5"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/eatmoreapple/openwechat"
	"github.com/go-resty/resty/v2"
	"gorm.io/gorm"
	"io"
	"os"
	"strings"
)

// RemotePlugin 远程插件
type RemotePlugin struct {
	info   Info
	client *resty.Client
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

func NewRemotePlugin(packageName string, api string, client *resty.Client) (Plugin, error) {
	plugin := &RemotePlugin{
		info: Info{
			ID:      packageName,
			Package: packageName,
			Code:    api,
		},
		client: client,
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
	if ctx.IsSendByGroup() {
		group, _ := ctx.Sender()
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
	} else {
		user, _ := ctx.Sender()
		msg.UID = user.UserName
		if user.RemarkName != "" {
			msg.Username = user.RemarkName
		} else if user.DisplayName != "" {
			msg.Username = user.DisplayName
		} else {
			msg.Username = user.NickName
		}
	}

	resp, err := p.client.EnableTrace().R().SetBody(msg).Post(p.info.Code)
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

	filename := response.Filename
	msgType, _ := response.Type.Int64()
	switch int(msgType) {
	case -1:
		return false, nil
	case 1:
		_, _ = ctx.ReplyText(response.Body)
	case 2:
		if filename == "" {
			filename = fmt.Sprintf("%x.jpg", md5.Sum([]byte(response.Body)))
		}
		reader, err := p.download(filename, response.Body)
		if err != nil {
			return false, err
		}
		defer func() {
			_ = reader.Close()
		}()
		_, _ = ctx.ReplyImage(reader)
	case 3:
		if filename == "" {
			filename = fmt.Sprintf("%x.mp4", md5.Sum([]byte(response.Body)))
		}
		reader, err := p.download(filename, response.Body)
		if err != nil {
			return false, err
		}
		defer func() {
			_ = reader.Close()
		}()
		_, _ = ctx.ReplyVideo(reader)
	case 4:
		if filename == "" {
			filename = fmt.Sprintf("%x", md5.Sum([]byte(response.Body)))
		}
		reader, err := p.download(filename, response.Body)
		if err != nil {
			return false, err
		}
		defer func() {
			_ = reader.Close()
		}()
		_, _ = ctx.ReplyFile(reader)
	}
	return true, nil
}

func (p *RemotePlugin) download(filename string, src string) (io.ReadCloser, error) {
	if strings.HasPrefix("BASE64:", src) {
		srcBytes, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(src, "BASE64:"))
		if err != nil {
			return nil, errors.New("解析资源信息出错")
		}
		err = os.WriteFile(filename, srcBytes, 0644)
		if err != nil {
			return nil, errors.New("获取资源信息出错")
		}
		return os.Open(filename)
	} else {
		resource, err := p.client.R().Get(src)
		if err != nil {
			return nil, err
		}
		body := resource.Body()
		// 缓存
		out, err := os.Create(filename)
		if err != nil {
			return io.NopCloser(bytes.NewReader(body)), nil
		}
		_, err = out.Write(body)
		if err != nil {
			_ = out.Close()
			_ = os.Remove(filename)
			return nil, errors.New("获取资源信息出错")
		}
		return os.Open(filename)
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
	}
)
