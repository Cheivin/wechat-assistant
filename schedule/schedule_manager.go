package schedule

import (
	"context"
	"errors"
	"fmt"
	"github.com/eatmoreapple/openwechat"
	"github.com/robfig/cron/v3"
	"gorm.io/gorm"
	"sync"
	"wechat-assistant/interpreter"
)

type (
	Task struct {
		Info
		Src string `` // 路径
	}

	Schedule struct {
		ID     int    `gorm:"primaryKey;autoIncrement"`
		TaskID string `` // 任务id
		Target string `` // 目标id
		Spec   string `` // 表达式
	}

	BindInfo struct {
		Schedule
		cron.EntryID
		Description string
	}
)

type Manager struct {
	DB      *gorm.DB        `aware:"db"`
	Bot     *openwechat.Bot `aware:"bot"`
	c       *cron.Cron
	mutex   sync.RWMutex
	loaded  map[string]TaskHandler
	bindMap map[string][]BindInfo
}

func (m *Manager) BeanName() string {
	return "scheduleManager"
}

func (m *Manager) BeanConstruct() {
	m.c = cron.New(cron.WithSeconds(), cron.WithLogger(cron.DefaultLogger))
	m.loaded = map[string]TaskHandler{}
	m.bindMap = map[string][]BindInfo{}
}

func (m *Manager) AfterPropertiesSet() {
	if err := m.DB.AutoMigrate(&Task{}, &Schedule{}); err != nil {
		panic(err)
	}
	if err := m.init(); err != nil {
		panic(err)
	}
}

func (m *Manager) init() error {
	var records []Schedule
	if err := m.DB.Find(&records).Error; err != nil {
		return err
	}
	if len(records) == 0 {
		m.c.Start()
		return nil
	}
	ids := make([]string, 0, len(records))
	for _, schedule := range records {
		entityId, err := m.c.AddFunc(schedule.Spec, m.job(schedule.TaskID, schedule.Target))
		if err != nil {
			return err
		}
		bindInfo := BindInfo{
			Schedule: schedule,
			EntryID:  entityId,
		}
		if jobs, ok := m.bindMap[schedule.TaskID]; !ok {
			m.bindMap[schedule.TaskID] = []BindInfo{bindInfo}
		} else {
			m.bindMap[schedule.TaskID] = append(jobs, bindInfo)
		}
		ids = append(ids, schedule.TaskID)
		fmt.Println(fmt.Sprintf("载入任务 ID:%d, 任务ID:%s, 目标位置:%s", schedule.ID, schedule.TaskID, schedule.Target))
	}
	var tasks []Task
	if err := m.DB.Find(&tasks, ids).Error; err != nil {
		return err
	}
	for _, task := range tasks {
		handler, err := NewCodeTask(task.Package, task.Code)
		if err != nil {
			return err
		}
		m.loaded[task.ID] = handler
	}
	m.c.Start()
	return nil
}

func (m *Manager) job(id string, target string) func() {
	return func() {
		handler, ok := m.loaded[id]
		if ok {
			ctx := context.WithValue(context.TODO(), "target", target)
			if m.Bot != nil {
				self, _ := m.Bot.GetCurrentUser()
				_ = handler.Handle(ctx, m.DB, self)
			} else {
				_ = handler.Handle(ctx, m.DB, nil)
			}
		}
	}
}

func (m *Manager) Install(codePath string) (TaskHandler, error) {
	packageName, code, err := interpreter.GetCode(codePath)
	if err != nil {
		return nil, err
	}
	handler, err := NewCodeTask(packageName, code)
	if err != nil {
		return nil, err
	}
	// 检查id是否重复
	record := new(Task)
	if err := m.DB.Take(record, "id = ?", handler.ID()).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("定时任务安装失败")
		}
	} else if record != nil {
		return nil, errors.New("存在同名定时任务")
	}
	if err := m.DB.Create(Task{
		Info: handler.Info(),
		Src:  codePath,
	}).Error; err != nil {
		return nil, errors.New("定时任务安装失败")
	}
	return handler, nil
}

func (m *Manager) Update(id string, src string) error {
	task := new(Task)
	if err := m.DB.First(task, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New(fmt.Sprintf("任务%s不存在", id))
		} else {
			return errors.New("获取务信息出错")
		}
	}
	if src != "" {
		task.Src = src
	}
	packageName, code, err := interpreter.GetCode(task.Src)
	if err != nil {
		return err
	}
	handler, err := NewCodeTask(packageName, code)
	if err != nil {
		return err
	}
	task.Info = handler.Info()
	return m.DB.Save(task).Error
}

func (m *Manager) Load(id string) (TaskHandler, error) {
	record := new(Task)
	if err := m.DB.First(record, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		} else {
			return nil, errors.New("加载定时任务出错")
		}
	}
	return NewCodeTask(record.Package, record.Code)
}

func (m *Manager) List(target string, fromDB bool) (*[]BindInfo, error) {
	bindInfos := make([]BindInfo, 0, len(m.bindMap))
	if fromDB {
		var tasks []Task
		if err := m.DB.Find(&tasks).Error; err != nil {
			return nil, err
		}
		var schedules []Schedule
		if err := m.DB.Find(&schedules).Error; err != nil {
			return nil, err
		}

		for i := range tasks {
			task := tasks[i]
			bound := false
			for _, schedule := range schedules {
				if schedule.Target == target && schedule.TaskID == task.ID {
					bound = true
					bindInfos = append(bindInfos, BindInfo{
						Schedule:    schedule,
						Description: task.Description,
					})
				}
			}
			if !bound {
				bindInfos = append(bindInfos, BindInfo{
					Schedule: Schedule{
						TaskID: task.ID,
					},
					Description: task.Description,
				})
			}
		}
	} else {
		for _, binds := range m.bindMap {
			for _, bind := range binds {
				if bind.Target == target {
					if task, ok := m.loaded[bind.TaskID]; ok {
						bindInfos = append(bindInfos, BindInfo{
							Schedule:    bind.Schedule,
							Description: task.Info().Description,
						})
					}
				}
			}
		}
	}
	return &bindInfos, nil

}
func (m *Manager) Bind(handler TaskHandler, spec string, target string) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	// 插件加载状态
	old, loaded := m.loaded[handler.ID()]
	if loaded { // 如果已加载，使用旧实例替换
		handler = old
	}
	if err := m.DB.Transaction(func(tx *gorm.DB) error {
		// 添加绑定关系
		schedule := Schedule{
			TaskID: handler.ID(),
			Target: target,
			Spec:   spec,
		}
		if err := m.DB.Create(&schedule).Error; err != nil {
			return errors.New("绑定定时任务出错")
		}

		entityId, err := m.c.AddFunc(schedule.Spec, m.job(schedule.TaskID, schedule.Target))
		if err != nil {
			return err
		}
		fmt.Println("taskId", entityId)

		m.loaded[handler.ID()] = handler

		bindInfo := BindInfo{
			Schedule: schedule,
			EntryID:  entityId,
		}
		binds, bound := m.bindMap[handler.ID()]
		if bound {
			m.bindMap[handler.ID()] = append(binds, bindInfo)
		} else {
			m.bindMap[handler.ID()] = []BindInfo{bindInfo}
		}
		return nil
	}); err != nil {
		return err
	}
	return nil

}

func (m *Manager) Reload(id string) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	_, loaded := m.loaded[id]
	if !loaded {
		return errors.New(fmt.Sprintf("定时任务%s未加载", id))
	}
	plugin, err := m.Load(id)
	if err != nil {
		return err
	}
	m.loaded[id] = plugin
	return nil
}

func (m *Manager) Unbind(id int) (bool, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	var bindInfo BindInfo
	var ok bool
search:
	for _, bindInfos := range m.bindMap {
		for _, info := range bindInfos {
			if info.ID == id {
				bindInfo = info
				ok = true
				break search
			}
		}
	}

	if ok {
		err := m.DB.Transaction(func(tx *gorm.DB) error {
			err := m.DB.Where("id = ?", bindInfo.ID).
				Delete(&Schedule{}).
				Error
			if err != nil {
				return errors.New("解绑定时任务出错")
			}
			// 移除定时任务
			m.c.Remove(bindInfo.EntryID)
			// 过滤解绑的任务
			bindInfos := m.bindMap[bindInfo.TaskID]
			infos := make([]BindInfo, 0, len(bindInfos))
			for _, info := range bindInfos {
				if info.ID != bindInfo.ID {
					infos = append(infos, info)
				}
			}
			if len(infos) == 0 {
				delete(m.bindMap, bindInfo.TaskID)
				delete(m.loaded, bindInfo.TaskID)
			} else {
				m.bindMap[bindInfo.TaskID] = infos
			}
			return nil
		})
		return true, err
	}
	return ok, nil
}

func (m *Manager) Uninstall(id string) (ok bool, err error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	err = m.DB.Transaction(func(tx *gorm.DB) error {
		// 查找任务信息
		task := new(Task)
		if err := m.DB.First(task, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			} else {
				return errors.New("卸载插件出错")
			}
		} else if task == nil {
			return nil
		}
		// 删除任务和绑定
		result := m.DB.Delete(task)
		if result.Error != nil {
			return result.Error
		}
		if err := m.DB.Where("task_id = ?", task.ID).Delete(&Schedule{}).Error; err != nil {
			return errors.New("卸载插件出错")
		}
		// 移除任务
		bindInfos, bound := m.bindMap[task.ID]
		if bound {
			for _, bindInfo := range bindInfos {
				m.c.Remove(bindInfo.EntryID)
			}
			delete(m.bindMap, task.ID)
			delete(m.loaded, task.ID)
		}
		ok = true
		return nil
	})
	return
}
