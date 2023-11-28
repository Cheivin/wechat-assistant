package database

import (
	"errors"
	"github.com/cheivin/di"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
	"log"
	"os"
	"time"
)

type (
	dbSqliteProperty struct {
		File string `value:"file"` // 数据库文件路径
	}
	SqliteConfiguration struct {
	}
)

func (SqliteConfiguration) BeanConstruct(container di.DI) {
	sqliteProperty := container.LoadProperties("db.", dbSqliteProperty{}).(dbSqliteProperty)
	db, err := gorm.Open(sqlite.Open(sqliteProperty.File), &gorm.Config{
		NamingStrategy: schema.NamingStrategy{SingularTable: true},
		Logger: logger.New(
			log.New(os.Stdout, "\r\n", log.LstdFlags),
			logger.Config{
				SlowThreshold:             time.Second,
				LogLevel:                  logger.Error,
				IgnoreRecordNotFoundError: true,
				ParameterizedQueries:      false,
				Colorful:                  true,
			},
		),
	})
	if err != nil {
		panic(errors.Join(err, errors.New("failed to connect database")))
	}
	container.RegisterNamedBean("db", db)
}
