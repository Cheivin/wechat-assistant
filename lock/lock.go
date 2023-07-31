package lock

import (
	"errors"
	"gorm.io/gorm"
	"time"
)

type Locker interface {
	Lock(key string, ttl time.Duration) (int, error)
	Update(key string, ttl time.Duration)
}

type (
	DBLocker struct {
		db *gorm.DB
	}

	taskLock struct {
		ID      string `gorm:"primary"`
		Version int64  ``
	}
)

func NewDBLocker(db *gorm.DB) (Locker, error) {
	return &DBLocker{db}, db.AutoMigrate(taskLock{})
}

func (l DBLocker) Lock(lockKey string, ttl time.Duration) (int, error) {
	now := time.Now()
	access := -1
	err := l.db.Transaction(func(tx *gorm.DB) error {
		lock := new(taskLock)
		// 查询
		if err := tx.Take(lock, "id = ?", lockKey).Error; err != nil {
			// 插件没有导入gorm下所有symbol
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			} else {
				lock = nil
			}
		}
		// 不存在则新增
		if lock == nil {
			if err := tx.Create(taskLock{
				ID:      lockKey,
				Version: now.Unix(),
			}).Error; err != nil {
				return err
			}
			access = 0
			return nil
		}
		// 当前时间小于version，说明有任务在处理
		if time.Now().Unix() < lock.Version {
			access = -1
			return nil
		}
		// 判断时间，限制时间内则返回
		if time.Since(time.Unix(lock.Version, 0)) < ttl {
			access = int(ttl.Seconds() - time.Since(time.Unix(lock.Version, 0)).Seconds())
			return nil
		}
		executed := tx.Model(taskLock{}).
			Where("id = ?", lockKey).
			Where("version = ?", lock.Version).
			Update("version", time.Now().Add(ttl).Unix())
		if executed.RowsAffected != 0 {
			access = 0
		}
		return executed.Error
	})
	return access, err
}

func (l DBLocker) Update(lockKey string, ttl time.Duration) {
	l.db.Model(taskLock{}).
		Where("id = ?", lockKey).
		Update("version", time.Now().Add(ttl).Unix())
}
