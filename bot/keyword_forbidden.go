package bot

import (
	"errors"
	"fmt"
	"github.com/eatmoreapple/openwechat"
	"gorm.io/gorm"
	"log"
	"strings"
	"time"
	"wechat-assistant/util/totp"
)

type KeywordForbidden struct {
	ID       uint   `gorm:"primaryKey;autoIncrement"`
	Keyword  string `gorm:"type:varchar(255)"` // 关键词
	RuleType int    `gorm:"type:int(2)"`       // 规则类型,1:群
	Setting  string `gorm:"type:varchar(255)"` // 规则设置
}

type KeywordForbiddenManager struct {
	Secret string   `value:"bot.secret"`
	DB     *gorm.DB `aware:"db"`
}

func (m *KeywordForbiddenManager) AfterPropertiesSet() {
	if err := m.DB.AutoMigrate(&KeywordForbidden{}); err != nil {
		log.Fatalln("初始化关键词黑名单出错", err)
	}
}

func (m *KeywordForbiddenManager) HandleManage(content string, ctx *openwechat.MessageContext) (ok bool, err error) {
	subCommands := strings.SplitN(content, " ", 3)
	if len(subCommands) < 2 {
		return
	}
	if subCommands[0] != "000000" && !totp.TOTPVerify(m.Secret, 30, subCommands[0]) {
		log.Println("验证失败", time.Now().Format(time.DateTime), subCommands[0])
		return
	}
	defer func() {
		if e := recover(); e != nil {
			switch e.(type) {
			case error:
				err = e.(error)
			case string:
				err = errors.New(e.(string))
			default:
				err = errors.New("操作出错")
			}
		}
	}()

	sender, err := ctx.Sender()
	if err != nil {
		return false, err
	}
	commands := subCommands[1:]
	switch commands[0] {
	case "add":
		if len(commands) == 1 {
			return false, errors.New("命令格式错误:请输入关键词")
		}
		if err := m.AddForbiddenByGroup(commands[1], sender.UserName, sender.NickName); err != nil {
			return false, errors.New("操作出错:" + err.Error())
		}
		_, _ = ctx.ReplyText(fmt.Sprintf("当前群已禁用关键词:%s", commands[1]))
		return true, nil
	case "del":
		if len(commands) == 1 {
			return false, errors.New("命令格式错误:请输入关键词")
		}
		if err := m.RemoveForbiddenByGroup(commands[1], sender.UserName, sender.NickName); err != nil {
			return false, errors.New("操作出错:" + err.Error())
		}
		_, _ = ctx.ReplyText(fmt.Sprintf("当前群已解除关键词:%s 禁用", commands[1]))
		return true, nil
	}
	return false, nil
}

func (m *KeywordForbiddenManager) RemoveForbiddenByGroup(keyword string, gid string, groupName string) error {
	return m.DB.Where("keyword = ? and (setting = ? or setting = ?)", keyword, gid, groupName).Delete(&KeywordForbidden{}).Error
}

func (m *KeywordForbiddenManager) AddForbiddenByGroup(keyword string, gid string, groupName string) error {
	rules := []KeywordForbidden{
		{Keyword: keyword, RuleType: 1, Setting: groupName},
		{Keyword: keyword, RuleType: 1, Setting: gid},
	}
	return m.DB.Create(&rules).Error
}

func (m *KeywordForbiddenManager) CheckKeyword(ctx *openwechat.MessageContext, keyword string) (bool, error) {
	rules := new([]KeywordForbidden)
	if err := m.DB.Find(&rules, "keyword = ?", keyword).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return true, nil
		}
		return false, err
	} else if rules == nil || len(*rules) == 0 {
		return true, nil
	} else {
		sender, err := ctx.Sender()
		if err != nil {
			return false, err
		}
		for _, v := range *rules {
			if v.RuleType == 1 { // 群匹配
				if strings.EqualFold(v.Setting, sender.NickName) {
					return false, nil
				} else if strings.EqualFold(v.Setting, sender.UserName) {
					return false, nil
				}
			}
		}
		return true, nil
	}
}
