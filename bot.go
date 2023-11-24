package main

import (
	"fmt"
	"github.com/eatmoreapple/openwechat"
	"github.com/go-resty/resty/v2"
	"github.com/mdp/qrterminal/v3"
	"github.com/robfig/cron/v3"
	"log"
	"os"
	"strconv"
)

type BotManager struct {
	Data             string          `value:"bot.data"`
	SyncHost         string          `value:"bot.syncHost"`
	Bot              *openwechat.Bot `aware:"bot"`
	MsgHandler       *MsgHandler     `aware:""`
	Resty            *resty.Client   `aware:"resty"`
	groupUserNameMap map[string]map[string][]string
}

func (b *BotManager) AfterPropertiesSet() {
	// 注册登陆二维码回调
	b.Bot.UUIDCallback = func(uuid string) {
		fmt.Println(openwechat.GetQrcodeUrl(uuid))
		qrterminal.Generate("https://login.weixin.qq.com/l/"+uuid, qrterminal.L, os.Stdout)
	}
	// 注册消息处理器
	b.Bot.MessageHandler = b.MsgHandler.GetHandler()
}

func (b *BotManager) Initialized() {
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
		fmt.Println("群:", g.NickName, g.DisplayName)
	}
	b.updateAndSyncModifyUser()
	b.startUpdateGroupTask()
}

func (b *BotManager) updateGroup() map[string]map[string]openwechat.User {
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

func (b *BotManager) startUpdateGroupTask() {
	c := cron.New(cron.WithSeconds(), cron.WithLogger(cron.DefaultLogger))
	_, err := c.AddFunc("@every 10m", b.updateAndSyncModifyUser)
	if err != nil {
		panic(err)
	}
	c.Start()
}

func (b *BotManager) updateAndSyncModifyUser() {
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

func (b *BotManager) callSyncByUid(gid, uid string, attrStatus int64) {
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
