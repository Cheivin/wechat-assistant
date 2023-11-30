package bot

import (
	"github.com/eatmoreapple/openwechat"
	"github.com/go-resty/resty/v2"
	"github.com/mdp/qrterminal/v3"
	"github.com/robfig/cron/v3"
	"gorm.io/gorm"
	"log"
	"time"
	"wechat-assistant/redirect"
)

type Manager struct {
	Data          string               `value:"bot.data"`
	Bot           *openwechat.Bot      `aware:"bot"`
	MsgHandler    *MsgHandler          `aware:""`
	Redirect      redirect.MsgRedirect `aware:"omitempty"`
	MessageSender *redirect.MsgSender  `aware:""`
	Resty         *resty.Client        `aware:"resty"`
	DB            *gorm.DB             `aware:"db"`
}

func (b *Manager) AfterPropertiesSet() {
	if err := b.DB.AutoMigrate(Group{}); err != nil {
		log.Fatalln("初始化群组表失败", err)
	}
	if err := b.DB.AutoMigrate(GroupUser{}); err != nil {
		log.Fatalln("初始化群组用户表失败", err)
	}
	// 注册登陆二维码回调
	b.Bot.UUIDCallback = func(uuid string) {
		log.Println(openwechat.GetQrcodeUrl(uuid))
		qrterminal.Generate("https://login.weixin.qq.com/l/"+uuid, qrterminal.L, log.Writer())
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
				_, _ = b.MessageSender.SendGroupMediaMsgByGid(msg.Gid, msg.Type, msg.Body, msg.Filename, msg.Prompt)
			} else if msg.GroupName != "" {
				_, _ = b.MessageSender.SendGroupMediaMsgByGroupName(msg.GroupName, msg.Type, msg.Body, msg.Filename, msg.Prompt)
			}

		}
	}
}

func (b *Manager) Initialized() {
	// 创建热存储容器对象
	reloadStorage := openwechat.NewFileHotReloadStorage(b.Data)
	defer reloadStorage.Close()

	// 执行热登录
	if err := b.Bot.HotLogin(reloadStorage, NewRetryLoginOption(reloadStorage)); err != nil {
		log.Fatalln("登录出错", err)
	}
	// 获取登陆的用户
	self, err := b.Bot.GetCurrentUser()
	if err != nil {
		log.Fatalln("获取用户出错", err)
	}

	// 获取所有的好友
	friends, err := self.Friends()
	log.Println(friends, err)
	// 获取所有的群组
	groups, err := self.Groups()
	for _, g := range groups {
		log.Println("群:", g.UserName, g.AvatarID(), g.NickName, g.DisplayName)
	}
	b.updateAndSyncModifyUser()
	b.startUpdateGroupTask()
}

// updateGroup 更新群组信息
func (b *Manager) updateGroup() ([]Group, []GroupUser) {
	log.Println("刷新群信息")
	self, err := b.Bot.GetCurrentUser()
	if err != nil {
		log.Println("获取当前用户信息失败", err)
	}
	newGroups, err := self.Groups(true)
	if err != nil {
		log.Println("刷新群信息失败", err)
		return nil, nil
	}

	var modifyUsers []GroupUser
	var modifyGroups []Group

	for i := range newGroups {
		group := newGroups[i]

		groupModel := new(Group)
		b.DB.Take(groupModel, "g_id=?", group.UserName)
		if groupModel == nil || groupModel.GID == "" {
			groupModel = &Group{GID: group.UserName, GroupName: group.NickName, Time: time.Now().Unix()}
			res := b.DB.Create(groupModel)
			if res.Error != nil {
				log.Println("更新群信息失败", *groupModel, err)
			} else if res.RowsAffected > 0 {
				modifyGroups = append(modifyGroups, *groupModel)

			}
		} else if groupModel.GroupName != group.NickName {
			groupModel.GroupName = group.NickName
			groupModel.Time = time.Now().Unix()
			res := b.DB.Model(Group{}).
				Where("g_id=?", groupModel.GID).
				Updates(map[string]interface{}{
					"group_name": groupModel.GroupName,
					"`time`":     groupModel.Time,
				})
			if res.Error != nil {
				log.Println("更新群信息失败", *groupModel, err)
			} else if res.RowsAffected > 0 {
				modifyGroups = append(modifyGroups, *groupModel)
			}
		}

		members, _ := group.Members()
		for _, member := range members {
			groupUser := new(GroupUser)
			b.DB.Take(groupUser, "g_id=? and uid=? and attr_status=?", groupModel.GID, member.UserName, member.AttrStatus)
			username := member.NickName
			if member.DisplayName != "" {
				username = member.DisplayName
			}
			if groupUser == nil || groupUser.GID == "" {
				groupUser := GroupUser{
					GID:        group.UserName,
					UID:        member.UserName,
					Username:   username,
					WechatName: member.NickName,
					AttrStatus: member.AttrStatus,
					Time:       time.Now().Unix(),
				}
				res := b.DB.Create(groupUser)
				if res.Error != nil {
					log.Println("更新群成员信息失败", groupUser.GID, groupUser.Username, groupUser.WechatName, err)
				} else if res.RowsAffected > 0 {
					modifyUsers = append(modifyUsers, groupUser)
				}
			} else if groupUser.Username != username || groupUser.WechatName != member.NickName {
				groupUser.Username = username
				groupUser.WechatName = member.NickName
				groupUser.AttrStatus = member.AttrStatus
				groupUser.Time = time.Now().Unix()
				res := b.DB.Model(GroupUser{}).
					Where("g_id=? and uid=? and attr_status=?", groupModel.GID, groupUser.UID, groupUser.AttrStatus).
					Updates(map[string]interface{}{
						"username":    groupUser.Username,
						"wechat_name": groupUser.WechatName,
						"attr_status": groupUser.AttrStatus,
						"`time`":      groupUser.Time,
					})
				if res.Error != nil {
					log.Println("更新群成员信息失败", groupUser.GID, groupUser.Username, groupUser.WechatName, err)
				} else if res.RowsAffected > 0 {
					modifyUsers = append(modifyUsers, *groupUser)
				}
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
		log.Fatalln("添加定时任务出错", err)
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
	log.Println("修正群历史记录-群信息", group.GID, group.GroupName)
	groups := make([]Group, 0)
	b.DB.Model(&Group{}).Where("group_name = ?", group.GroupName).Find(&groups)
	if len(groups) > 0 {
		groupIds := make([]string, 0, len(groups))
		for _, g := range groups {
			groupIds = append(groupIds, g.GID)
		}
		b.DB.Model(&MsgHistory{}).
			Where("g_id in ?", groupIds).
			Update("g_id", group.GID)
	}

	b.DB.Model(&MsgHistory{}).
		Where("g_id = ?", group.GID).
		Update("group_name", group.GroupName)
}

// updateMsgHistoryUser 刷新数据库用户信息
func (b *Manager) updateMsgHistoryUser(user GroupUser) {
	log.Println("修正群历史记录-群成员信息", user.GID, user.WechatName, user.Username)
	users := make([]GroupUser, 0)
	b.DB.Model(&GroupUser{}).Where("wechat_name = ? and attr_status=?", user.WechatName, user.AttrStatus).Find(&users)
	if len(users) > 0 {
		userIds := make([]string, 0, len(users))
		for _, u := range users {
			userIds = append(userIds, u.GID)
		}
		for _, u := range users {
			b.DB.Model(&MsgHistory{}).
				Where("g_id = ? and uid in ?", u.GID, userIds).
				Update("uid", user.UID)
		}
	}

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
		Time      int64  `gorm:"type:int(13)"`
	}
	GroupUser struct {
		GID        string `gorm:"primaryKey;type:varchar(100)"`
		UID        string `gorm:"primaryKey;type:varchar(100)"`
		Username   string `gorm:"type:varchar(255)"`
		WechatName string `gorm:"type:varchar(255)"`
		AttrStatus int64  `gorm:"type:int(20)"`
		Time       int64  `gorm:"type:int(13)"`
	}
)
