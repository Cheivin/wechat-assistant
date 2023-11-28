package bot

import (
	"fmt"
	"github.com/eatmoreapple/openwechat"
	"github.com/go-resty/resty/v2"
	"github.com/mdp/qrterminal/v3"
	"github.com/robfig/cron/v3"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"os"
	"wechat-assistant/redirect"
)

type Manager struct {
	Data          string               `value:"bot.data"`
	Bot           *openwechat.Bot      `aware:"bot"`
	MsgHandler    *MsgHandler          `aware:""`
	Redirect      redirect.MsgRedirect `aware:"omitempty"`
	MessageSender *MsgSender           `aware:""`
	Resty         *resty.Client        `aware:"resty"`
	DB            *gorm.DB             `aware:"db"`
}

func (b *Manager) AfterPropertiesSet() {
	if err := b.DB.AutoMigrate(Group{}); err != nil {
		panic(err)
	}
	if err := b.DB.AutoMigrate(GroupUser{}); err != nil {
		panic(err)
	}
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
	_, _ = self.Friends()
	// 获取所有的群组
	go b.updateAndSyncModifyUser()
	b.startUpdateGroupTask()
}

// updateGroup 更新群组信息
func (b *Manager) updateGroup() ([]Group, []GroupUser) {
	fmt.Println("更新群信息")
	self, _ := b.Bot.GetCurrentUser()
	newGroups, _ := self.Groups(true)

	var modifyUsers []GroupUser
	var modifyGroups []Group

	for i := range newGroups {
		group := newGroups[i]

		groupModel := Group{GID: group.UserName, GroupName: group.NickName}
		res := b.DB.Clauses(clause.OnConflict{DoUpdates: clause.Assignments(map[string]interface{}{
			"group_name": group.NickName,
		})}).Create(groupModel)
		if res.RowsAffected > 0 {
			modifyGroups = append(modifyGroups, groupModel)
		}
		members, _ := group.Members()
		for _, member := range members {
			username := member.UserName
			if member.DisplayName != "" {
				username = member.DisplayName
			}
			groupUser := GroupUser{
				GID:        group.UserName,
				UID:        member.UserName,
				Username:   username,
				WechatName: member.NickName,
				AttrStatus: member.AttrStatus,
			}
			res := b.DB.Clauses(clause.OnConflict{DoUpdates: clause.Assignments(map[string]interface{}{
				"username":    username,
				"wechat_name": member.NickName,
			})}).Create(groupUser)
			if res.RowsAffected > 0 {
				modifyUsers = append(modifyUsers, groupUser)
			}
		}
	}
	return modifyGroups, modifyUsers
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
	groups, users := b.updateGroup()
	if len(groups) > 0 {
		for i := range groups {
			b.updateMsgHistoryGroup(groups[i])
		}
	}
	if len(users) > 0 {
		for i := range users {
			b.updateMsgHistoryUser(users[i])
		}
	}
}

func (b *Manager) updateMsgHistoryGroup(group Group) {
	b.DB.Model(&MsgHistory{}).
		Where("g_id = ?", group.GID).
		Update("group_name", group.GroupName)
}

// updateMsgHistoryUser 刷新数据库用户信息
func (b *Manager) updateMsgHistoryUser(user GroupUser) {
	b.DB.Model(&MsgHistory{}).
		Where("g_id = ?", user.GID).
		Where("uid = ?", user.UID).
		Updates(map[string]interface{}{
			"username":    user.Username,
			"wechat_name": user.WechatName,
			"attr_status": user.AttrStatus,
		})
}

type (
	Group struct {
		GID       string `gorm:"primaryKey;type:varchar(100)"`
		GroupName string `gorm:"type:varchar(255)"`
	}
	GroupUser struct {
		GID        string `gorm:"primaryKey;type:varchar(100)"`
		UID        string `gorm:"primaryKey;type:varchar(100)"`
		Username   string `gorm:"type:varchar(255)"`
		WechatName string `gorm:"type:varchar(255)"`
		AttrStatus int64  `gorm:"type:int(20)"`
	}
)
