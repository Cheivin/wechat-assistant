package main

import (
	"errors"
	"fmt"
	"github.com/eatmoreapple/openwechat"
	"github.com/glebarez/sqlite"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
	"log"
	"os"
	"path/filepath"
	"time"
)

var db *gorm.DB

func init() {
	var err error
	dbType := os.Getenv("DB")

	config := &gorm.Config{
		NamingStrategy: schema.NamingStrategy{SingularTable: true},
		Logger: logger.New(
			log.New(os.Stdout, "\r\n", log.LstdFlags), // io writer
			logger.Config{
				SlowThreshold:             time.Second,  // Slow SQL threshold
				LogLevel:                  logger.Error, // Log level
				IgnoreRecordNotFoundError: true,         // Ignore ErrRecordNotFound error for logger
				ParameterizedQueries:      false,        // Don't include params in the SQL log
				Colorful:                  false,        // Disable color
			},
		),
	}

	switch dbType {
	case "mysql":
		host := os.Getenv("MYSQL_HOST")
		username := os.Getenv("MYSQL_USERNAME")
		password := os.Getenv("MYSQL_PASSWORD")
		port := os.Getenv("MYSQL_PORT")
		database := os.Getenv("MYSQL_DATABASE")
		parameters := os.Getenv("MYSQL_PARAMETERS")
		dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?%s", []interface{}{
			username,
			password,
			host,
			port,
			database,
			parameters,
		}...)
		// 配置数据库
		db, err = gorm.Open(mysql.Open(dsn), config)
		if err != nil {
			panic(errors.Join(err, errors.New("failed to connect database")))
		}
		err = db.Set("gorm:table_options", " DEFAULT CHARSET=utf8mb4").AutoMigrate(Statistics{})
		if err != nil {
			panic(errors.Join(err, errors.New("failed to auto migrate table")))
		}
	default:
		db, err = gorm.Open(sqlite.Open(filepath.Join(os.Getenv("DATA"), "data.db")), config)
		if err != nil {
			panic(errors.Join(err, errors.New("failed to connect database")))
		}
		err = db.AutoMigrate(Statistics{})
		if err != nil {
			panic(errors.Join(err, errors.New("failed to auto migrate table")))
		}
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
	GID      string `gorm:"primaryKey"`
	UID      string `gorm:"primaryKey"`
	Username string ``
	Total    int64  ``
}

func TopN(GID string, limit int) (*[]Rank, error) {
	date := time.Now().Format(time.DateOnly)
	ranks := new([]Rank)
	return ranks, db.Model(&Statistics{}).
		Select("g_id, uid, username, sum(count) as total").
		Where("g_id = ? and date = ?", GID, date).
		Group("g_id,uid,username").
		Order("total desc").
		Limit(limit).
		Find(ranks).Error
}
