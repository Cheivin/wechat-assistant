package main

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/base64"
	"errors"
	"fmt"
	"github.com/eatmoreapple/openwechat"
	"github.com/gin-gonic/gin"
	"github.com/go-resty/resty/v2"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

type WebContainer struct {
	Port   int             `value:"app.port"`
	Bot    *openwechat.Bot `aware:"bot"`
	Resty  *resty.Client   `aware:"resty"`
	router *gin.Engine
	server *http.Server
}

func (w *WebContainer) BeanName() string {
	return "ginWebContainer"
}

// BeanConstruct 初始化实例时，创建gin框架
func (w *WebContainer) BeanConstruct() {
	w.router = gin.New()
	w.router.RemoteIPHeaders = []string{"X-Forwarded-For", "X-Real-IP", "Proxy-Client-IP", "WL-Proxy-Client-IP", "HTTP_CLIENT_IP", "HTTP_X_FORWARDED_FOR"}
	w.router.POST("/msg/send", w.sendMsg)
}

// AfterPropertiesSet 注入完成时触发
func (w *WebContainer) AfterPropertiesSet() {
	w.server = &http.Server{
		Handler: w.router,
		Addr:    fmt.Sprintf(":%d", w.Port),
	}
}

// Initialized DI加载完成后，启动服务
func (w *WebContainer) Initialized() {
	go func() {
		fmt.Println("启动web服务", "端口", w.Port)
		if err := w.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			panic(err)
		}
	}()
}

func (w *WebContainer) Destroy() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_ = w.server.Shutdown(ctx)
}

func (w *WebContainer) sendMsg(c *gin.Context) {
	req := new(apiRequest)
	err := c.Bind(req)
	if err != nil {
		c.JSON(200, gin.H{
			"code":  400,
			"error": err.Error(),
		})
		return
	}
	if req.Gid == "" {
		c.JSON(200, gin.H{
			"code":  400,
			"error": "gid不能为空",
		})
		return
	}
	if !w.Bot.Alive() {
		c.JSON(200, gin.H{
			"code":  500,
			"error": "Bot已掉线",
		})
		return
	}

	self, err := w.Bot.GetCurrentUser()
	if err != nil {
		c.JSON(200, gin.H{
			"code":  500,
			"error": err.Error(),
		})
		return
	}

	groups, _ := self.Groups()
	group := groups.SearchByUserName(1, req.Gid).First()
	if group == nil {
		c.JSON(200, gin.H{
			"code":  404,
			"error": "群组不存在",
		})
		return
	}

	switch req.Type {
	case 1:
		if sent, err := self.SendTextToGroup(group, req.Body); err != nil {
			c.JSON(200, gin.H{
				"code":  500,
				"error": "发送失败:" + err.Error(),
			})
		} else {
			c.JSON(200, gin.H{
				"code":  0,
				"error": "",
				"msgId": sent.MsgId,
			})
		}
	case 2:
		if req.Filename == "" {
			req.Filename = fmt.Sprintf("%x.jpg", md5.Sum([]byte(req.Body)))
		}
		reader, err := w.download(req.Filename, req.Body)
		if err != nil {
			c.JSON(200, gin.H{
				"code":  500,
				"error": err.Error(),
			})
			return
		}
		defer func() {
			_ = reader.Close()
		}()
		if sent, err := self.SendImageToGroup(group, reader); err != nil {
			c.JSON(200, gin.H{
				"code":  500,
				"error": "发送失败:" + err.Error(),
			})
		} else {
			c.JSON(200, gin.H{
				"code":  0,
				"error": "",
				"msgId": sent.MsgId,
			})
		}

	case 3:
		if req.Filename == "" {
			req.Filename = fmt.Sprintf("%x.mp4", md5.Sum([]byte(req.Body)))
		}
		reader, err := w.download(req.Filename, req.Body)
		if err != nil {
			c.JSON(200, gin.H{
				"code":  500,
				"error": err.Error(),
			})
			return
		}
		defer func() {
			_ = reader.Close()
		}()
		if sent, err := self.SendVideoToGroup(group, reader); err != nil {
			c.JSON(200, gin.H{
				"code":  500,
				"error": "发送失败:" + err.Error(),
			})
		} else {
			c.JSON(200, gin.H{
				"code":  0,
				"error": "",
				"msgId": sent.MsgId,
			})
		}
	case 4:
		if req.Filename == "" {
			req.Filename = fmt.Sprintf("%x", md5.Sum([]byte(req.Body)))
		}
		reader, err := w.download(req.Filename, req.Body)
		if err != nil {
			c.JSON(200, gin.H{
				"code":  500,
				"error": err.Error(),
			})
			return
		}
		defer func() {
			_ = reader.Close()
		}()
		if sent, err := self.SendFileToGroup(group, reader); err != nil {
			c.JSON(200, gin.H{
				"code":  500,
				"error": "发送失败:" + err.Error(),
			})
		} else {
			c.JSON(200, gin.H{
				"code":  0,
				"error": "",
				"msgId": sent.MsgId,
			})
		}
	default:
		c.JSON(200, gin.H{
			"code":  405,
			"error": "暂不支持该类型",
		})
	}
}

func (w *WebContainer) download(filename string, src string) (io.ReadCloser, error) {
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
		resource, err := w.Resty.R().Get(src)
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
	apiRequest struct {
		Gid      string `json:"gid" form:"gid"`           // 群id
		Type     int    `json:"type" form:"type"`         // 回复类型 1:文本,2:图片,3:视频,4:文件
		Body     string `json:"body" form:"body"`         // 回复内容,type=1时为文本内容,type=2/3/4时为资源地址
		Filename string `json:"filename" form:"filename"` // 文件名称
	}
)
