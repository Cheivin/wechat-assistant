package main

import (
	"errors"
	"fmt"
	"github.com/eatmoreapple/openwechat"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
	"log"
	"os"
	"time"
)

var db *gorm.DB

func init() {
	newLogger := logger.New(
		log.New(os.Stdout, "\r\n", log.LstdFlags), // io writer
		logger.Config{
			SlowThreshold:             time.Second,  // Slow SQL threshold
			LogLevel:                  logger.Error, // Log level
			IgnoreRecordNotFoundError: true,         // Ignore ErrRecordNotFound error for logger
			ParameterizedQueries:      false,        // Don't include params in the SQL log
			Colorful:                  false,        // Disable color
		},
	)

	var err error
	db, err = gorm.Open(sqlite.Open("data.db"), &gorm.Config{
		Logger: newLogger,
	})
	if err != nil {
		panic(errors.Join(err, errors.New("failed to connect database")))
	}
	err = db.AutoMigrate(Statistics{})
	if err != nil {
		panic(errors.Join(err, errors.New("failed to auto migrate table")))
	}
}

type Statistics struct {
	GID      string `gorm:"primaryKey"`
	UID      string `gorm:"primaryKey"`
	Date     string `gorm:"primaryKey"`
	MsgType  int    `gorm:"primaryKey"`
	Username string ``
	Count    int64  ``
}

func StatisticGroup(msg *openwechat.Message) error {
	group, err := msg.Sender()
	if err != nil {
		return err
	}
	user, err := msg.SenderInGroup()
	if err != nil {
		return err
	}
	username := user.DisplayName
	if user.DisplayName == "" {
		username = user.NickName
	}
	record := Statistics{
		GID:      group.UserName,
		UID:      user.UserName,
		Date:     time.Now().Format(time.DateOnly),
		Username: username,
		MsgType:  int(msg.MsgType),
		Count:    1,
	}
	update := map[string]interface{}{
		"username": username,
		"count":    gorm.Expr("count + 1"),
	}
	return db.Clauses(clause.OnConflict{DoUpdates: clause.Assignments(update)}).Create(&record).Error
}

type Rank struct {
	UID      string ``
	Username string ``
	Total    int64  ``
}

func TopN(GID string, limit int) (*[]Rank, error) {
	date := time.Now().Format(time.DateOnly)
	ranks := new([]Rank)
	fmt.Println(GID, date)
	return ranks, db.Model(&Statistics{}).
		Select("uid,username, sum(count) as total").
		Where("g_id = ? and date = ?", GID, date).
		Group("g_id,uid").
		Order("total desc").
		Limit(limit).
		Find(ranks).Error
}
