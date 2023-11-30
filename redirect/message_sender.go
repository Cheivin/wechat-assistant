package redirect

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/base64"
	"errors"
	"fmt"
	"github.com/eatmoreapple/openwechat"
	"github.com/go-resty/resty/v2"
	"golang.org/x/time/rate"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type MsgSender struct {
	Bot       *openwechat.Bot `aware:"bot"`
	Resty     *resty.Client   `aware:"resty"`
	CachePath string          `value:"bot.cache"`
	limit     *rate.Limiter
}

func (s *MsgSender) AfterPropertiesSet() {
	s.limit = rate.NewLimiter(rate.Every(1*time.Second), 1)
	if err := os.MkdirAll(s.CachePath, os.ModePerm); err != nil {
		log.Fatalln("创建缓存目录失败", err)
	}
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

	// 限流最大等待
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*3)
	_ = s.limit.Wait(ctx) // 忽略限流，只是为了人为等待
	cancel()

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

	// 限流最大等待
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*3)
	_ = s.limit.Wait(ctx) // 忽略限流，只是为了人为等待
	cancel()

	if sent, err := self.SendTextToGroup(group, msg); err != nil {
		return "", err
	} else {
		return sent.MsgId, nil
	}
}

func (s *MsgSender) SendGroupTextMsg(group *openwechat.Group, msg string) (string, error) {
	self, err := s.getSelf()
	if err != nil {
		return "", err
	}
	if group == nil {
		return "", errors.New("群不存在")
	}

	// 限流最大等待
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*3)
	_ = s.limit.Wait(ctx) // 忽略限流，只是为了人为等待
	cancel()

	if sent, err := self.SendTextToGroup(group, msg); err != nil {
		return "", err
	} else {
		return sent.MsgId, nil
	}
}

func (s *MsgSender) SendGroupMediaMsgByGid(gid string, mediaType int, src string, filename string, prompt string) (string, error) {
	self, err := s.getSelf()
	if err != nil {
		return "", err
	}
	groups, _ := self.Groups()
	group := groups.SearchByUserName(1, gid).First()
	if group == nil {
		return "", errors.New("群不存在")
	}
	return s.sendMediaMessage(group, mediaType, src, filename, prompt)
}

func (s *MsgSender) SendGroupMediaMsgByGroupName(gid string, mediaType int, src string, filename string, prompt string) (string, error) {
	self, err := s.getSelf()
	if err != nil {
		return "", err
	}
	groups, _ := self.Groups()
	group := groups.SearchByNickName(1, gid).First()
	if group == nil {
		return "", errors.New("群不存在")
	}
	return s.sendMediaMessage(group, mediaType, src, filename, prompt)
}

func (s *MsgSender) SendGroupMediaMsg(group *openwechat.Group, mediaType int, src string, filename string, prompt string) (string, error) {
	return s.sendMediaMessage(group, mediaType, src, filename, prompt)
}

func (s *MsgSender) sendMediaMessage(group *openwechat.Group, mediaType int, src string, filename string, prompt string) (string, error) {
	self, err := s.getSelf()
	if err != nil {
		return "", err
	}

	// 限流最大等待
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*3)
	_ = s.limit.Wait(ctx) // 忽略限流，只是为了人为等待
	cancel()

	var promptSent *openwechat.SentMessage
	switch mediaType {
	case 2:
		if filename == "" {
			filename = fmt.Sprintf("%x.jpg", md5.Sum([]byte(src)))
		}
		if prompt != "" {
			promptSent, _ = self.SendTextToGroup(group, prompt)
			defer func() {
				_ = promptSent.Revoke()
			}()
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
		if prompt != "" {
			promptSent, _ = self.SendTextToGroup(group, prompt)
			defer func() {
				_ = promptSent.Revoke()
			}()
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
		if prompt != "" {
			promptSent, _ = self.SendTextToGroup(group, prompt)
			defer func() {
				_ = promptSent.Revoke()
			}()
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
	filename = filepath.Join(s.CachePath, time.Now().Format("2006/01/02"), filename)
	_ = os.MkdirAll(filepath.Dir(filename), os.ModePerm)
	if strings.HasPrefix("BASE64:", src) {
		log.Println("转换BASE64资源", filename)
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
		log.Println("下载资源", src, ">>", filename)
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
