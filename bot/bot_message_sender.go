package bot

import (
	"bytes"
	"crypto/md5"
	"encoding/base64"
	"errors"
	"fmt"
	"github.com/eatmoreapple/openwechat"
	"github.com/go-resty/resty/v2"
	"io"
	"os"
	"strings"
)

type MsgSender struct {
	Bot   *openwechat.Bot `aware:"bot"`
	Resty *resty.Client   `aware:"resty"`
}

func (s *MsgSender) SendGroupTextMsgByGid(gid string, msg string) (string, error) {
	self, err := s.getSelf()
	if err != nil {
		return "", err
	}
	groups, _ := self.Groups()
	group := groups.SearchByUserName(1, gid).First()
	if group == nil {
		return "", errors.New("群不存在")
	}
	if sent, err := self.SendTextToGroup(group, msg); err != nil {
		return "", err
	} else {
		return sent.MsgId, nil
	}
}

func (s *MsgSender) SendGroupTextMsgByGroupName(gid string, msg string) (string, error) {
	self, err := s.getSelf()
	if err != nil {
		return "", err
	}
	groups, _ := self.Groups()
	group := groups.SearchByNickName(1, gid).First()
	if group == nil {
		return "", errors.New("群不存在")
	}
	if sent, err := self.SendTextToGroup(group, msg); err != nil {
		return "", err
	} else {
		return sent.MsgId, nil
	}
}

func (s *MsgSender) SendGroupMediaMsgByGid(gid string, mediaType int, src string, filename string) (string, error) {
	self, err := s.getSelf()
	if err != nil {
		return "", err
	}
	groups, _ := self.Groups()
	group := groups.SearchByUserName(1, gid).First()
	if group == nil {
		return "", errors.New("群不存在")
	}
	return s.sendMediaMessage(group, mediaType, src, filename)
}

func (s *MsgSender) SendGroupMediaMsgByGroupName(gid string, mediaType int, src string, filename string) (string, error) {
	self, err := s.getSelf()
	if err != nil {
		return "", err
	}
	groups, _ := self.Groups()
	group := groups.SearchByNickName(1, gid).First()
	if group == nil {
		return "", errors.New("群不存在")
	}
	return s.sendMediaMessage(group, mediaType, src, filename)
}

func (s *MsgSender) sendMediaMessage(group *openwechat.Group, mediaType int, src string, filename string) (string, error) {
	self, err := s.getSelf()
	if err != nil {
		return "", err
	}

	switch mediaType {
	case 2:
		if filename == "" {
			filename = fmt.Sprintf("%x.jpg", md5.Sum([]byte(src)))
		}
		reader, err := s.download(s.Resty, filename, src)
		if err != nil {
			return "", err
		}
		defer func() {
			_ = reader.Close()
		}()
		if sent, err := self.SendImageToGroup(group, reader); err != nil {
			return "", err
		} else {
			return sent.MsgId, nil
		}
	case 3:
		if filename == "" {
			filename = fmt.Sprintf("%x.mp4", md5.Sum([]byte(src)))
		}
		reader, err := s.download(s.Resty, filename, src)
		if err != nil {
			return "", err
		}
		defer func() {
			_ = reader.Close()
		}()
		if sent, err := self.SendVideoToGroup(group, reader); err != nil {
			return "", err
		} else {
			return sent.MsgId, nil
		}
	case 4:
		if filename == "" {
			filename = fmt.Sprintf("%x", md5.Sum([]byte(src)))
		}
		reader, err := s.download(s.Resty, filename, src)
		if err != nil {
			return "", err
		}
		defer func() {
			_ = reader.Close()
		}()
		if sent, err := self.SendFileToGroup(group, reader); err != nil {
			return "", err
		} else {
			return sent.MsgId, nil
		}
	default:
		return "", errors.New("暂不支持该类型")
	}
}

func (s *MsgSender) getSelf() (*openwechat.Self, error) {
	if !s.Bot.Alive() {
		return nil, errors.New("bot已掉线")
	}
	return s.Bot.GetCurrentUser()
}

func (s *MsgSender) download(client *resty.Client, filename string, src string) (io.ReadCloser, error) {
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
		resource, err := client.R().Get(src)
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
