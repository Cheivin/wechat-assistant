package bot

import (
	"fmt"
	"github.com/eatmoreapple/openwechat"
	"github.com/go-resty/resty/v2"
	"github.com/mdp/qrterminal/v3"
	"github.com/robfig/cron/v3"
	"log"
	"os"
	"strconv"
	"wechat-assistant/redirect"
)

type Manager struct {
	Data             string               `value:"bot.data"`
	SyncHost         string               `value:"bot.syncHost"`
	Bot              *openwechat.Bot      `aware:"bot"`
	MsgHandler       *MsgHandler          `aware:""`
	Redirect         redirect.MsgRedirect `aware:"omitempty"`
	MessageSender    *MsgSender           `aware:""`
	Resty            *resty.Client        `aware:"resty"`
	groupUserNameMap map[string]map[string][]string
}

func (b *Manager) AfterPropertiesSet() {
	// 注册登陆二维码回调
	b.Bot.UUIDCallback = func(uuid string) {
		fmt.Println(openwechat.GetQrcodeUrl(uuid))
		qrterminal.Generate("https://login.weixin.qq.com/l/"+uuid, qrterminal.L, os.Stdout)
	}
	// 注册消息处理器
	b.Bot.MessageHandler = b.MsgHandler.GetHandler()
	if b.Redirect != nil {
		b.Redirect.SetCommandHandler(b.commandHandler)
	}
}

func (b *Manager) commandHandler(command redirect.BotCommand) {
	switch command.Command {
	case "sendMessage":
		msg := command.Param
		switch msg.Type {
		case 1:
			if msg.Gid != "" {
				_, _ = b.MessageSender.SendGroupTextMsgByGid(msg.Gid, msg.Body)
			} else if msg.GroupName != "" {
				_, _ = b.MessageSender.SendGroupTextMsgByGroupName(msg.GroupName, msg.Body)
			}
		case 2, 3, 4:
			if msg.Gid != "" {
				_, _ = b.MessageSender.SendGroupMediaMsgByGid(msg.Gid, msg.Type, msg.Body, msg.Filename)
			} else if msg.GroupName != "" {
				_, _ = b.MessageSender.SendGroupMediaMsgByGroupName(msg.GroupName, msg.Type, msg.Body, msg.Filename)
			}

		}
	}
}

func (b *Manager) Initialized() {
	b.groupUserNameMap = make(map[string]map[string][]string)
	// 创建热存储容器对象
	reloadStorage := openwechat.NewFileHotReloadStorage(b.Data)
	defer reloadStorage.Close()

	// 执行热登录
	if err := b.Bot.HotLogin(reloadStorage, openwechat.NewRetryLoginOption()); err != nil {
		panic(err)
	}
	// 获取登陆的用户
	self, err := b.Bot.GetCurrentUser()
	if err != nil {
		panic(err)
	}

	// 获取所有的好友
	friends, err := self.Friends()
	fmt.Println(friends, err)
	// 获取所有的群组
	groups, err := self.Groups()
	for _, g := range groups {
		fmt.Println("群:", g.UserName, g.AvatarID(), g.NickName, g.DisplayName)
	}
	b.updateAndSyncModifyUser()
	b.startUpdateGroupTask()
}

// updateGroup 更新群组信息
func (b *Manager) updateGroup() map[string]map[string]openwechat.User {
	fmt.Printf("旧信息:%+v\n", b.groupUserNameMap)
	self, _ := b.Bot.GetCurrentUser()
	newGroups, _ := self.Groups(true)
	fmt.Println("更新群信息")
	modifyMap := make(map[string]map[string]openwechat.User)
	for i := range newGroups {
		group := newGroups[i]
		oldUserMap := b.groupUserNameMap[group.UserName]
		if oldUserMap == nil {
			oldUserMap = make(map[string][]string)
		}
		newUserMap := make(map[string][]string)
		userMap := make(map[string]openwechat.User)
		members, _ := group.Members()
		for _, member := range members {
			newUserMap[member.UserName] = []string{member.NickName, member.DisplayName}

			if names, ok := oldUserMap[member.UserName]; ok {
				if names[0] != member.NickName {
					userMap[member.UserName] = openwechat.User{UserName: member.UserName, NickName: member.NickName, DisplayName: member.DisplayName, AttrStatus: member.AttrStatus}
				} else if names[1] != member.DisplayName {
					userMap[member.UserName] = openwechat.User{UserName: member.UserName, NickName: member.NickName, DisplayName: member.DisplayName, AttrStatus: member.AttrStatus}
				}
			} else {
				userMap[member.UserName] = openwechat.User{UserName: member.UserName, NickName: member.NickName, DisplayName: member.DisplayName, AttrStatus: member.AttrStatus}
			}
		}

		if len(userMap) > 0 {
			modifyMap[group.UserName] = userMap
		}
		b.groupUserNameMap[group.UserName] = newUserMap
	}
	return modifyMap
}

// startUpdateGroupTask 开始定时更新群组信息任务
func (b *Manager) startUpdateGroupTask() {
	c := cron.New(cron.WithSeconds(), cron.WithLogger(cron.DefaultLogger))
	_, err := c.AddFunc("@every 10m", b.updateAndSyncModifyUser)
	if err != nil {
		panic(err)
	}
	c.Start()
}

// updateAndSyncModifyUser 刷新变更的用户信息
func (b *Manager) updateAndSyncModifyUser() {
	modifyMap := b.updateGroup()
	if len(modifyMap) == 0 {
		return
	}
	for gid, users := range modifyMap {
		for uid, user := range users {
			b.callSyncByUid(gid, uid, user.AttrStatus)
		}
	}
}

// callSyncByUid 刷新数据库用户信息
func (b *Manager) callSyncByUid(gid, uid string, attrStatus int64) {
	if b.SyncHost == "" {
		return
	}
	_, err := b.Resty.R().
		SetQueryParam("type", "2").
		SetQueryParam("gid", gid).
		SetQueryParam("uid", uid).
		SetQueryParam("attrStatus", strconv.Itoa(int(attrStatus))).
		Get(b.SyncHost)
	if err != nil {
		log.Println("同步出错", err)
	}
}
