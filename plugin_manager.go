package main

import (
	"errors"
	"fmt"
	"github.com/eatmoreapple/openwechat"
	"gorm.io/gorm"
	"sync"
	"wechat-assistant/lock"
)

type (
	Addon struct {
		*Plugin
		BindKeyword string `` // 绑定的唤醒词
		Src         string `` // 路径
	}

	PluginManager struct {
		db        *gorm.DB
		locker    lock.Locker
		pluginMap sync.Map
	}
)

func NewPluginManager(db *gorm.DB) (*PluginManager, error) {
	err := db.AutoMigrate(&Addon{})
	if err != nil {
		return nil, err
	}
	locker, err := lock.NewDBLocker(db)
	if err != nil {
		return nil, err
	}
	return &PluginManager{db: db, locker: locker}, nil
}

func (p *PluginManager) InstallPlugin(pluginPath string) (*Plugin, error) {
	packageName, code, err := getPluginCode(pluginPath)
	if err != nil {
		return nil, err
	}
	plugin, err := loadPlugin(packageName, code)
	if err != nil {
		return nil, err
	}
	// 检查id是否重复
	record := new(Addon)
	if err := p.db.Take(record, "id = ?", plugin.ID).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("插件安装失败")
		}
	} else if record != nil {
		return nil, errors.New("存在同名插件")
	}
	if err := p.db.Create(Addon{
		Plugin: plugin,
		Src:    pluginPath,
	}).Error; err != nil {
		return nil, errors.New("插件安装失败")
	}
	return plugin, nil
}

func (p *PluginManager) ListPlugin(fromDB bool) (*[]Addon, error) {
	var addons []Addon
	if fromDB {
		err := p.db.Find(&addons).Error
		if err != nil {
			return &addons, err
		}
	} else {
		p.pluginMap.Range(func(key, value any) bool {
			if v, ok := value.(*Plugin); ok {
				addons = append(addons, Addon{
					BindKeyword: key.(string),
					Plugin:      v,
				})
			}
			return true
		})
	}
	return &addons, nil
}

func (p *PluginManager) LoadPlugin(id string) (*Plugin, error) {
	addon := new(Addon)
	if err := p.db.First(addon, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		} else {
			return nil, errors.New("加载插件出错")
		}
	}
	return loadPlugin(addon.Package, addon.Code)
}

func (p *PluginManager) UninstallPlugin(id string) (ok bool, err error) {
	err = p.db.Transaction(func(tx *gorm.DB) error {
		addon := new(Addon)
		if err := p.db.First(addon, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			} else {
				return errors.New("卸载插件出错")
			}
		} else if addon == nil {
			return nil
		}
		// 删除
		result := p.db.Delete(addon)
		if result.Error != nil {
			return result.Error
		}
		ok = true
		// 卸载
		if addon.BindKeyword == "" {
			return nil
		}
		_, err = p.UnbindPlugin(addon.BindKeyword)
		return err
	})
	return
}

func (p *PluginManager) BindPlugin(keyword string, plugin *Plugin, force bool) error {
	if keyword != "" {
		plugin.Keyword = keyword
	}
	if plugin.Keyword == "" {
		return errors.New("插件未绑定唤醒词")
	}

	return p.db.Transaction(func(tx *gorm.DB) error {
		if force {
			// 先替换
			if err := p.db.Model(&Addon{}).
				Where("bind_keyword = ?", plugin.Keyword).
				Update("bind_keyword", "").
				Error; err != nil {
				return errors.New("更新插件信息出错")
			}
			// 再配置
			if err := p.db.Model(&Addon{}).
				Where("id = ?", plugin.ID).
				Update("bind_keyword", plugin.Keyword).Error; err != nil {
				return errors.New("绑定插件出错")
			}

			// 旧插件卸载
			if v, ok := p.pluginMap.Load(plugin.Keyword); ok {
				_, _ = destroyPlugin(v.(*Plugin), p.db)
			}
		} else {
			if err := p.db.Model(&Addon{}).
				Where("id = ?", plugin.ID).
				Update("bind_keyword", plugin.Keyword).Error; err != nil {
				return errors.New("绑定插件出错")
			}

			actual, loaded := p.pluginMap.LoadOrStore(plugin.Keyword, plugin)
			if loaded {
				if actual.(*Plugin).ID != plugin.ID {
					return errors.New(fmt.Sprintf("唤醒词[%s]已被占用,请先卸载或更换唤醒词绑定", keyword))
				} else if actual.(*Plugin).Code == plugin.Code {
					// id相同代码相同同，忽略
					return nil
				}
				// 旧插件卸载
				_, _ = destroyPlugin(actual.(*Plugin), p.db)
			}
		}
		// 新插件初始化
		if _, err := initPlugin(plugin, p.db); err != nil {
			return errors.New("初始化插件出错")
		}
		// 存储信息
		p.pluginMap.Store(plugin.Keyword, plugin)
		return nil
	})
}

func (p *PluginManager) UnbindPlugin(keyword string) (bool, error) {
	plugin, ok := p.pluginMap.LoadAndDelete(keyword)
	if ok {
		err := p.db.Transaction(func(tx *gorm.DB) error {
			err := p.db.Model(&Addon{}).
				Where("id = ?", plugin.(*Plugin).ID).
				Update("bind_keyword", "").
				Error
			if err != nil {
				return errors.New("更新插件信息出错")
			}
			_, err = destroyPlugin(plugin.(*Plugin), p.db)
			return err
		})
		return true, err
	}
	return ok, nil
}

func (p *PluginManager) InvokePlugin(keyword string, params []string, db *gorm.DB, ctx *openwechat.MessageContext) (ok bool, err error) {
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
	v, ok := p.pluginMap.Load(keyword)
	if !ok {
		return false, nil
	}
	plugin, ok := v.(*Plugin)
	if !ok {
		return false, nil
	}
	ctx.Set("pluginParams", params)
	ctx.Set("locker", p.locker)
	return plugin.fn(db, ctx)
}
