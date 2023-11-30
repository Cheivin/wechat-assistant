package database

import (
	"errors"
	"fmt"
	"github.com/cheivin/di"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
	"log"
	"os"
	"time"
)

type (
	dbMysqlProperty struct {
		Host       string `value:"host"`
		Port       int    `value:"port"`
		Username   string `value:"username"`
		Password   string `value:"password"`
		Database   string `value:"database"`
		Parameters string `value:"parameters"`
	}
	MysqlConfiguration struct {
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

func (MysqlConfiguration) BeanConstruct(container di.DI) {
	mysqlProperty := container.LoadProperties("db.", dbMysqlProperty{}).(dbMysqlProperty)
	// 配置数据库
	db, err := gorm.Open(mysql.Open(mysqlProperty.dsn()), &gorm.Config{
		NamingStrategy: schema.NamingStrategy{SingularTable: true},
		Logger: logger.New(
			log.New(os.Stdout, "\r\n", log.LstdFlags),
			logger.Config{
				SlowThreshold:             time.Second,
				LogLevel:                  logger.Error,
				IgnoreRecordNotFoundError: true,
				ParameterizedQueries:      false,
				Colorful:                  false,
			},
		),
	})
	if err != nil {
		log.Fatalln(errors.Join(err, errors.New("failed to connect database")))
	}
	container.RegisterNamedBean("db", db)
}
