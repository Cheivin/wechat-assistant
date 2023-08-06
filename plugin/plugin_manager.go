package plugin

import (
	"errors"
	"fmt"
	"github.com/eatmoreapple/openwechat"
	"gorm.io/gorm"
	"sync"
	"wechat-assistant/interpreter"
	"wechat-assistant/lock"
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

	Manager struct {
		db      *gorm.DB
		locker  lock.Locker
		mutex   sync.RWMutex
		loaded  map[string]Plugin // 已加载的插件
		bindMap map[string]string // 映射关系
	}
)

func NewManager(db *gorm.DB) (*Manager, error) {
	err := db.AutoMigrate(&Addon{}, &AddonBind{})
	if err != nil {
		return nil, err
	}
	locker, err := lock.NewDBLocker(db)
	if err != nil {
		return nil, err
	}
	m := &Manager{db: db, locker: locker, loaded: map[string]Plugin{}, bindMap: map[string]string{}}
	// 初始化
	return m, m.init()
}

func (m *Manager) init() error {
	var records []AddonBind
	if err := m.db.Find(&records).Error; err != nil {
		return err
	}
	if len(records) == 0 {
		return nil
	}
	ids := make([]string, 0, len(records))
	for _, bind := range records {
		ids = append(ids, bind.ID)
		m.bindMap[bind.Keyword] = bind.ID
		fmt.Println(fmt.Sprintf("已启用插件 ID:%s, bindKeyword:%s", bind.ID, bind.Keyword))
	}
	var addons []Addon
	if err := m.db.Find(&addons, ids).Error; err != nil {
		return err
	}
	for _, addon := range addons {
		plugin, err := NewCodePlugin(addon.Package, addon.Code)
		if err != nil {
			return err
		} else if err := plugin.Init(m.db); err != nil {
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
		_ = plugin.Destroy(m.db)
		delete(m.loaded, id)
	}
}

func (m *Manager) Install(pluginPath string) (Plugin, error) {
	packageName, code, err := interpreter.GetCode(pluginPath)
	if err != nil {
		return nil, err
	}
	plugin, err := NewCodePlugin(packageName, code)
	if err != nil {
		return nil, err
	}
	// 检查id是否重复
	record := new(Addon)
	if err := m.db.Take(record, "id = ?", plugin.ID()).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("插件安装失败")
		}
	} else if record != nil {
		return nil, errors.New("存在同名插件")
	}
	if err := m.db.Create(Addon{
		Info: plugin.Info(),
		Src:  pluginPath,
	}).Error; err != nil {
		return nil, errors.New("插件安装失败")
	}
	return plugin, nil
}

func (m *Manager) Update(id string, src string) error {
	addon := new(Addon)
	if err := m.db.First(addon, "id = ?", id).Error; err != nil {
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
	return m.db.Save(addon).Error
}

func (m *Manager) Load(id string) (Plugin, error) {
	addon := new(Addon)
	if err := m.db.First(addon, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		} else {
			return nil, errors.New("加载插件出错")
		}
	}
	return NewCodePlugin(addon.Package, addon.Code)
}

func (m *Manager) List(fromDB bool) (*[]BindInfo, error) {
	bindInfos := make([]BindInfo, 0, len(m.bindMap))
	if fromDB {
		var addons []Addon
		if err := m.db.Find(&addons).Error; err != nil {
			return nil, err
		}
		var binds []AddonBind
		if err := m.db.Find(&binds).Error; err != nil {
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

	if err := m.db.Transaction(func(tx *gorm.DB) error {
		// 如果已绑定，先清除原有绑定关系
		if bound {
			err := m.db.Where("id = ? and keyword = ?", bindId, keyword).Delete(&AddonBind{}).Error
			if err != nil {
				return errors.New("更新插件信息出错")
			}
		}
		// 添加新的绑定关系
		if err := m.db.Create(AddonBind{
			ID:      plugin.ID(),
			Keyword: keyword,
		}).Error; err != nil {
			return errors.New("绑定插件出错")
		}

		// 未加载过的插件需要初始化
		if !loaded {
			if err := plugin.Init(m.db); err != nil {
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
		err := m.db.Transaction(func(tx *gorm.DB) error {
			err := m.db.Where("id = ? and keyword = ?", bindId, keyword).
				Delete(&AddonBind{}).
				Error
			if err != nil {
				return errors.New("更新插件信息出错")
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

	err = m.db.Transaction(func(tx *gorm.DB) error {
		// 查找插件信息
		addon := new(Addon)
		if err := m.db.First(addon, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			} else {
				return errors.New("卸载插件出错")
			}
		} else if addon == nil {
			return nil
		}
		// 删除插件和绑定
		result := m.db.Delete(addon)
		if result.Error != nil {
			return result.Error
		}
		if err := m.db.Where("id = ?", addon.ID).Delete(&AddonBind{}).Error; err != nil {
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
	ctx.Set("locker", m.locker)
	return plugin.Handle(db, ctx)
}
