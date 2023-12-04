package plugin

import (
	"errors"
	"fmt"
	"github.com/cheivin/di"
	"github.com/eatmoreapple/openwechat"
	"github.com/go-resty/resty/v2"
	"gorm.io/gorm"
	"log"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"wechat-assistant/interpreter"
	"wechat-assistant/lock"
	"wechat-assistant/redirect"
	"wechat-assistant/util/totp"
)

type (
	Addon struct {
		Info
		Src string `` // 路径
	}

	BindInfo struct {
		ID          string
		Keyword     string
		Description string
		BindKeyword string
	}

	AddonBind struct {
		ID      string `gorm:"primaryKey"` // 插件id
		Keyword string `gorm:"primaryKey"` // 唤醒词
	}
)

type Manager struct {
	container di.DI
	Secret    string              `value:"bot.secret"`
	DB        *gorm.DB            `aware:"db"`
	Locker    lock.Locker         `aware:""`
	Resty     *resty.Client       `aware:"resty"`
	Sender    *redirect.MsgSender `aware:""`
	mutex     sync.RWMutex
	loaded    map[string]Plugin // 已加载的插件
	bindMap   map[string]string // 映射关系
}

func (m *Manager) BeanName() string {
	return "pluginManager"
}

func (m *Manager) BeanConstruct(container di.DI) {
	m.container = container
	m.loaded = map[string]Plugin{}
	m.bindMap = map[string]string{}
}

func (m *Manager) AfterPropertiesSet() {
	if err := m.DB.AutoMigrate(&Addon{}, &AddonBind{}); err != nil {
		log.Fatalln("初始化插件表出错", err)
	}
	if err := m.init(); err != nil {
		log.Fatalln("初始化插件出错", err)
	}
}

func (m *Manager) init() error {
	var records []AddonBind
	if err := m.DB.Find(&records).Error; err != nil {
		return err
	}
	if len(records) == 0 {
		return nil
	}
	ids := make([]string, 0, len(records))
	for _, bind := range records {
		ids = append(ids, bind.ID)
		m.bindMap[bind.Keyword] = bind.ID
		log.Println(fmt.Sprintf("已启用插件 ID:%s, bindKeyword:%s", bind.ID, bind.Keyword))
	}
	var addons []Addon
	if err := m.DB.Find(&addons, ids).Error; err != nil {
		return err
	}
	for _, addon := range addons {
		var plugin Plugin
		var err error
		if strings.HasPrefix(addon.Code, "http") {
			plugin, err = NewRemotePlugin(addon.Package, addon.Code, m.Resty, m.Sender)
		} else {
			plugin, err = NewCodePlugin(addon.Package, addon.Code)
		}
		if err != nil {
			return err
		} else if err := plugin.Init(m.DB); err != nil {
			return err
		} else {
			m.loaded[addon.ID] = plugin
		}
	}

	return nil
}

func (m *Manager) listLoaded() []BindInfo {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	loaded := make([]BindInfo, 0, len(m.bindMap))
	for key, id := range m.bindMap {
		plugin := m.loaded[id]
		loaded = append(loaded, BindInfo{
			ID:          id,
			Keyword:     plugin.Info().Keyword,
			Description: plugin.Info().Description,
			BindKeyword: key,
		})
	}
	return loaded
}

func (m *Manager) HandleManage(content string, ctx *openwechat.MessageContext) (ok bool, err error) {
	// xxxxxx 指令 参数...
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
				err = errors.New("插件操作出错")
			}
		}
	}()
	commands := subCommands[1:]
	switch commands[0] {
	case "install":
		if len(commands) == 1 {
			return false, errors.New("安装插件出错:请输入插件路径")
		}
		params := strings.SplitN(commands[1], " ", 3)
		pluginPath := ""
		if len(params) == 1 {
			pluginPath = params[0]
		}

		plugin, err := m.Install(pluginPath)
		if err != nil {
			return false, errors.New("安装插件出错:" + err.Error())
		}

		description := "插件安装成功，信息如下:\n"
		info := plugin.Info()
		description += "ID:" + plugin.ID() + "\n"
		if info.Keyword != "" {
			description += "默认唤醒词:" + info.Keyword + "\n"
		}
		if info.Description != "" {
			description += "说明:" + info.Description + "\n"
		}
		_, _ = ctx.ReplyText(description)
		return true, nil
	case "update":
		if len(commands) == 1 {
			return false, errors.New("更新插件出错:请输入插件ID")
		}
		params := strings.SplitN(commands[1], " ", 3)
		id := params[0]
		pluginPath := ""
		if len(params) > 1 {
			pluginPath = params[1]
		}
		err := m.Update(id, pluginPath)
		if err != nil {
			return false, errors.New("更新插件出错:" + err.Error())
		}
		_, _ = ctx.ReplyText(fmt.Sprintf("插件%s更新完成", id))
		return true, nil
	case "bind":
		if len(commands) == 1 {
			return false, errors.New("绑定插件出错:请输入插件ID和唤醒词")
		}
		params := strings.SplitN(commands[1], " ", 3)
		id := ""
		keyword := ""
		force := false
		switch len(params) {
		case 1:
			id = params[0]
		case 2:
			id = params[0]
			keyword = params[1]
		default:
			id = params[0]
			keyword = params[1]
			force = "force" == params[2]
		}
		plugin, err := m.Load(id)
		if err != nil {
			return false, err
		}
		err = m.Bind(keyword, plugin, force)

		info := plugin.Info()
		description := "插件绑定成功，信息如下:\n"
		description += "ID:" + plugin.ID() + "\n"
		description += "唤醒词:" + info.Keyword + "\n"
		if info.Description != "" {
			description += "说明:" + info.Description + "\n"
		}
		_, _ = ctx.ReplyText(description)
		return true, err
	case "unbind":
		if len(commands) == 1 {
			return false, errors.New("解绑插件出错:请输入唤醒词")
		}
		keyword := strings.SplitN(commands[1], " ", 2)[0]
		ok, err := m.Unbind(keyword)
		if !ok {
			return true, errors.New(fmt.Sprintf("解绑插件出错:唤醒词[%s]未绑定插件", keyword))
		}
		if err != nil {
			return true, errors.New(fmt.Sprintf("解绑插件出错:%s", err.Error()))
		}
		_, _ = ctx.ReplyText("插件解绑成功")
		return true, nil
	case "reload":
		if len(commands) == 1 {
			return false, errors.New("重载插件出错:请输入插件ID")
		}
		params := strings.SplitN(commands[1], " ", 2)
		id := params[0]
		if err := m.Reload(id); err != nil {
			return false, errors.New("重载插件出错:" + err.Error())
		}
		_, _ = ctx.ReplyText(fmt.Sprintf("插件%s重载完成", id))
		return true, nil
	case "uninstall":
		if len(commands) == 1 {
			return false, errors.New("请输入插件ID")
		}
		id := strings.SplitN(commands[1], " ", 2)[0]
		if ok, err := m.Uninstall(id); err != nil {
			return false, errors.New("卸载插件出错:" + err.Error())
		} else if ok {
			_, _ = ctx.ReplyText("插件卸载成功")
		} else {
			_, _ = ctx.ReplyText("未找到插件信息")
		}
		return true, nil
	case "list":
		fromDB := false
		if len(commands) > 1 {
			fromDB = "installed" == strings.SplitN(commands[1], " ", 2)[0]
		}
		addons, err := m.List(fromDB)
		if err != nil {
			return false, errors.New("查询已安装的插件列表出错")
		}
		switch len(*addons) {
		case 0:
			_, _ = ctx.ReplyText("当前没有安装插件")
		default:
			msg := "已安装的插件信息如下:\n"
			for _, v := range *addons {
				if v.BindKeyword == "" {
					msg += fmt.Sprintf("ID:%s(未绑定)\n", v.ID)
					if v.Keyword != "" {
						msg += fmt.Sprintf("--默认唤醒词:[%s]\n", v.Keyword)
					}
					if v.Description != "" {
						msg += fmt.Sprintf("--说明:%s\n", v.Description)
					}
				} else {
					msg += fmt.Sprintf("ID:%s, 唤醒词:[%s]\n", v.ID, v.Keyword)
					if v.Description != "" {
						msg += fmt.Sprintf("--说明:%s\n", v.Description)
					}
				}
			}
			_, _ = ctx.ReplyText(msg)
		}
		return true, nil
	}
	return false, nil
}

// 回收销毁已加载但未绑定的插件
func (m *Manager) recycle() {
	// 扫描已绑定的插件id
	boundSet := map[string]struct{}{}
	for _, id := range m.bindMap {
		boundSet[id] = struct{}{}
	}
	// 过滤未绑定但已加载的插件id
	unbindId := make([]string, 0, len(m.loaded))
	for id, _ := range m.loaded {
		if _, ok := boundSet[id]; !ok {
			unbindId = append(unbindId, id)
		}
	}
	// 回收销毁插件
	for _, id := range unbindId {
		plugin := m.loaded[id]
		_ = plugin.Destroy(m.DB)
		delete(m.loaded, id)
	}
}

func (m *Manager) Install(pluginPath string) (plugin Plugin, err error) {
	var packageName, code string
	if strings.HasPrefix(pluginPath, "[remote]http") {
		api, err := url.ParseRequestURI(strings.TrimPrefix(pluginPath, "[remote]"))
		if err != nil {
			return nil, err
		}
		api.RawQuery = ""
		code = api.String()
		packageName = filepath.Base(api.Path)
		plugin, err = NewRemotePlugin(packageName, code, m.Resty, m.Sender)
	} else {
		packageName, code, err = interpreter.GetCode(pluginPath)
		if err != nil {
			return nil, err
		}
		plugin, err = NewCodePlugin(packageName, code)
	}
	if err != nil {
		return nil, err
	}
	// 检查id是否重复
	record := new(Addon)
	if err := m.DB.Take(record, "id = ?", plugin.ID()).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("插件安装失败")
		}
	} else if record != nil {
		return nil, errors.New("存在同名插件")
	}
	if err := m.DB.Create(Addon{
		Info: plugin.Info(),
		Src:  pluginPath,
	}).Error; err != nil {
		return nil, errors.New("插件安装失败")
	}
	return plugin, nil
}

func (m *Manager) Update(id string, src string) error {
	addon := new(Addon)
	if err := m.DB.First(addon, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New(fmt.Sprintf("插件%s不存在", id))
		} else {
			return errors.New("获取插件信息出错")
		}
	}
	if src != "" {
		addon.Src = src
	}
	packageName, code, err := interpreter.GetCode(addon.Src)
	if err != nil {
		return err
	}
	addon.Package = packageName
	addon.Code = code
	return m.DB.Save(addon).Error
}

func (m *Manager) Load(id string) (Plugin, error) {
	addon := new(Addon)
	if err := m.DB.First(addon, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		} else {
			return nil, errors.New("加载插件出错")
		}
	}
	if strings.HasPrefix(addon.Code, "http") {
		return NewRemotePlugin(addon.Package, addon.Code, m.Resty, m.Sender)
	} else {
		return NewCodePlugin(addon.Package, addon.Code)
	}
}

func (m *Manager) List(fromDB bool) (*[]BindInfo, error) {
	bindInfos := make([]BindInfo, 0, len(m.bindMap))
	if fromDB {
		var addons []Addon
		if err := m.DB.Find(&addons).Error; err != nil {
			return nil, err
		}
		var binds []AddonBind
		if err := m.DB.Find(&binds).Error; err != nil {
			return nil, err
		}
		for i := range addons {
			addon := addons[i]
			bound := false
			for k, v := range m.bindMap {
				if v == addon.ID {
					bound = true
					bindInfos = append(bindInfos, BindInfo{
						ID:          addon.ID,
						Keyword:     addon.Keyword,
						Description: addon.Description,
						BindKeyword: k,
					})
				}
			}
			if !bound {
				bindInfos = append(bindInfos, BindInfo{
					ID:          addon.ID,
					Keyword:     addon.Keyword,
					Description: addon.Description,
				})
			}
		}
	} else {
		bindInfos = m.listLoaded()
	}
	return &bindInfos, nil
}

func (m *Manager) Bind(keyword string, plugin Plugin, force bool) error {
	keyword = plugin.Keyword(keyword)
	if keyword == "" {
		return errors.New("插件未绑定唤醒词")
	}
	m.mutex.Lock()
	defer m.mutex.Unlock()

	// 插件加载状态
	old, loaded := m.loaded[plugin.ID()]
	if loaded { // 如果插件已加载，使用旧实例替换
		plugin = old
	}
	// 检查唤醒词绑定状态
	bindId, bound := m.bindMap[keyword]
	// 唤醒词已绑定且不为强制绑定时，返回错误
	if bound && !force {
		return errors.New(fmt.Sprintf("唤醒词[%s]已被占用,请先卸载或更换唤醒词绑定", keyword))
	}

	if err := m.DB.Transaction(func(tx *gorm.DB) error {
		// 如果已绑定，先清除原有绑定关系
		if bound {
			err := m.DB.Where("id = ? and keyword = ?", bindId, keyword).Delete(&AddonBind{}).Error
			if err != nil {
				return errors.New("更新插件信息出错")
			}
		}
		// 添加新的绑定关系
		if err := m.DB.Create(AddonBind{
			ID:      plugin.ID(),
			Keyword: keyword,
		}).Error; err != nil {
			return errors.New("绑定插件出错")
		}

		// 未加载过的插件需要初始化
		if !loaded {
			if err := plugin.Init(m.DB); err != nil {
				// 撤销绑定和加载
				return errors.New("初始化插件出错")
			}
		}
		m.loaded[plugin.ID()] = plugin
		m.bindMap[keyword] = plugin.ID()

		return nil
	}); err != nil {
		return err
	}
	// 旧插件如果没有引用，则需要触发回收
	if bound {
		m.recycle()
	}
	return nil
}

func (m *Manager) Reload(id string) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	_, loaded := m.loaded[id]
	if !loaded {
		return errors.New(fmt.Sprintf("插件%s未加载", id))
	}
	plugin, err := m.Load(id)
	if err != nil {
		return err
	}
	m.loaded[id] = plugin
	return nil
}

func (m *Manager) Unbind(keyword string) (bool, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	bindId, ok := m.bindMap[keyword]
	if ok {
		err := m.DB.Transaction(func(tx *gorm.DB) error {
			err := m.DB.Where("id = ? and keyword = ?", bindId, keyword).
				Delete(&AddonBind{}).
				Error
			if err != nil {
				return errors.New("解绑插件信息出错")
			}
			delete(m.bindMap, keyword)
			m.recycle()
			return err
		})
		return true, err
	}
	return ok, nil
}

func (m *Manager) Uninstall(id string) (ok bool, err error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	err = m.DB.Transaction(func(tx *gorm.DB) error {
		// 查找插件信息
		addon := new(Addon)
		if err := m.DB.First(addon, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			} else {
				return errors.New("卸载插件出错")
			}
		} else if addon == nil {
			return nil
		}
		// 删除插件和绑定
		result := m.DB.Delete(addon)
		if result.Error != nil {
			return result.Error
		}
		if err := m.DB.Where("id = ?", addon.ID).Delete(&AddonBind{}).Error; err != nil {
			return errors.New("卸载插件出错")
		}

		// 扫描插件id已绑定的关键词
		boundKeyword := make([]string, 0, len(m.bindMap))
		for keyword, id := range m.bindMap {
			if id == addon.ID {
				boundKeyword = append(boundKeyword, keyword)
			}
		}
		// 清除绑定
		for _, keyword := range boundKeyword {
			delete(m.bindMap, keyword)
		}
		// 回收
		m.recycle()

		ok = true
		return nil
	})
	return
}

func (m *Manager) FindByKeyword(keyword string) Plugin {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	id, ok := m.bindMap[keyword]
	if !ok {
		return nil
	}
	return m.loaded[id]
}

func (m *Manager) Invoke(keyword string, params []string, db *gorm.DB, ctx *openwechat.MessageContext) (ok bool, err error) {
	defer func() {
		if e := recover(); e != nil {
			switch e.(type) {
			case error:
				err = e.(error)
			case string:
				err = errors.New(e.(string))
			default:
				err = errors.New("插件调用出错:" + keyword)
			}
		}
	}()

	plugin := m.FindByKeyword(keyword)
	if plugin == nil {
		return false, nil
	}
	ctx.Set("pluginParams", params)
	ctx.Set("di", m.container)
	ctx.Set("locker", m.Locker)
	ctx.Set("resty", m.Resty)
	return plugin.Handle(db, ctx)
}
