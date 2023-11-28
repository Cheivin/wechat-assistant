package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/eatmoreapple/openwechat"
	"github.com/gin-gonic/gin"
	"net/http"
	"time"
	"wechat-assistant/bot"
)

type WebContainer struct {
	Port          int             `value:"app.port"`
	Bot           *openwechat.Bot `aware:"bot"`
	MessageSender *bot.MsgSender  `aware:""`
	router        *gin.Engine
	server        *http.Server
}

func (w *WebContainer) BeanName() string {
	return "ginWebContainer"
}

// BeanConstruct 初始化实例时，创建gin框架
func (w *WebContainer) BeanConstruct() {
	w.router = gin.New()
	w.router.RemoteIPHeaders = []string{"X-Forwarded-For", "X-Real-IP", "Proxy-Client-IP", "WL-Proxy-Client-IP", "HTTP_CLIENT_IP", "HTTP_X_FORWARDED_FOR"}
	w.router.POST("/msg/send", w.sendMsg)
	w.router.GET("/groups", w.getGroups)
	w.router.GET("/group", w.getGroupInfo)
	w.router.GET("/group/:gid", w.getGroupInfo)
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

func (w *WebContainer) getSelf() (*openwechat.Self, error) {
	if !w.Bot.Alive() {
		return nil, errors.New("bot已掉线")
	}
	return w.Bot.GetCurrentUser()
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
	if req.Gid == "" && req.GroupName == "" {
		c.JSON(200, gin.H{
			"code":  400,
			"error": "群id或群名称不能为空",
		})
		return
	}
	var msgId string
	err = errors.New("暂不支持该类型")
	switch req.Type {
	case 1:
		if req.Gid != "" {
			msgId, err = w.MessageSender.SendGroupTextMsgByGid(req.Gid, req.Body)
		} else if req.GroupName != "" {
			msgId, err = w.MessageSender.SendGroupTextMsgByGroupName(req.GroupName, req.Body)
		}
	case 2, 3, 4:
		if req.Gid != "" {
			msgId, err = w.MessageSender.SendGroupMediaMsgByGid(req.Gid, req.Type, req.Body, req.Filename)
		} else if req.GroupName != "" {
			msgId, err = w.MessageSender.SendGroupMediaMsgByGroupName(req.GroupName, req.Type, req.Body, req.Filename)
		}
	}
	if err != nil {
		c.JSON(200, gin.H{
			"code":  500,
			"error": err.Error(),
		})
	} else {
		c.JSON(200, gin.H{
			"code":  0,
			"error": "",
			"msgId": msgId,
		})
	}
}

func (w *WebContainer) getGroups(c *gin.Context) {
	_, update := c.GetQuery("update")
	self, err := w.getSelf()
	if err != nil {
		c.JSON(200, gin.H{
			"code":  500,
			"error": err.Error(),
		})
		return
	}
	groups, _ := self.Groups(update)
	result := make([]group, 0, groups.Count())
	for _, g := range groups {
		result = append(result, group{
			Gid:  g.UserName,
			Name: g.NickName,
		})
	}
	c.JSON(200, gin.H{
		"code":  0,
		"error": "",
		"data":  result,
	})
}

func (w *WebContainer) getGroupInfo(c *gin.Context) {
	var gid string
	gid = c.Param("gid")
	if gid == "" {
		gid = c.Query("gid")
	}
	_, update := c.GetQuery("update")
	if gid == "" {
		c.JSON(200, gin.H{
			"code":  400,
			"error": "gid不能为空",
		})
		return
	}
	self, err := w.getSelf()
	if err != nil {
		c.JSON(200, gin.H{
			"code":  500,
			"error": err.Error(),
		})
		return
	}
	groups, _ := self.Groups(update)
	g := groups.SearchByUserName(1, gid).First()
	if g == nil {
		c.JSON(200, gin.H{
			"code":  404,
			"error": "群组不存在",
		})
		return
	}
	members, _ := g.Members()
	users := make([]user, 0, members.Count())
	for _, member := range members {
		users = append(users, user{
			Uid:         member.UserName,
			NickName:    member.NickName,
			DisplayName: member.DisplayName,
			AttrStatus:  int(member.AttrStatus),
		})
	}
	c.JSON(200, gin.H{
		"code":  0,
		"error": "",
		"data": group{
			Gid:  g.UserName,
			Name: g.NickName,
			User: &users,
		},
	})
}

type (
	apiRequest struct {
		Gid       string `json:"gid" form:"gid"`           // 群id
		GroupName string `json:"groupName" form:"gid"`     // 群名称
		Type      int    `json:"type" form:"type"`         // 回复类型 1:文本,2:图片,3:视频,4:文件
		Body      string `json:"body" form:"body"`         // 回复内容,type=1时为文本内容,type=2/3/4时为资源地址
		Filename  string `json:"filename" form:"filename"` // 文件名称
	}

	group struct {
		Gid  string  `json:"gid"`
		Name string  `json:"name"`
		User *[]user `json:"user,omitempty"`
	}
	user struct {
		Uid         string `json:"uid"`
		NickName    string `json:"nickName"`
		DisplayName string `json:"displayName"`
		AttrStatus  int    `json:"attrStatus"`
	}
)
