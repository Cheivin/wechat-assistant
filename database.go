package main

import (
	"errors"
	"fmt"
	"github.com/cheivin/di"
	"github.com/glebarez/sqlite"
	"gorm.io/driver/mysql"
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
	dbMysqlProperty struct {
		Host       string `value:"host"`
		Port       int    `value:"port"`
		Username   string `value:"username"`
		Password   string `value:"password"`
		Database   string `value:"database"`
		Parameters string `value:"parameters"`
	}
	// 数据库配置
	dbConfiguration struct {
	}
)

func (p dbMysqlProperty) dsn() string {
	return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?%s", []interface{}{
		p.Username,
		p.Password,
		p.Host,
		p.Port,
		p.Database,
		p.Parameters,
	}...)
}

func (dbConfiguration) BeanConstruct(container di.DI) {
	var db *gorm.DB
	var err error

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

	dbType := container.GetProperty("db.type").(string)
	switch dbType {
	case "mysql":
		mysqlProperty := container.LoadProperties("db.", dbMysqlProperty{}).(dbMysqlProperty)
		// 配置数据库
		db, err = gorm.Open(mysql.Open(mysqlProperty.dsn()), config)
		if err != nil {
			panic(errors.Join(err, errors.New("failed to connect database")))
		}
	default:
		sqliteProperty := container.LoadProperties("db.", dbSqliteProperty{}).(dbSqliteProperty)
		db, err = gorm.Open(sqlite.Open(sqliteProperty.File), config)
		if err != nil {
			panic(errors.Join(err, errors.New("failed to connect database")))
		}
	}
	container.RegisterNamedBean("db", db)
}
