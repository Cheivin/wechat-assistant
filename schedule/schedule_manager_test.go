package schedule

import (
	"fmt"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
	"log"
	"os"
	"testing"
	"time"
)

func setupManager() (*Manager, error) {
	parameters := "charset=utf8mb4&collation=utf8mb4_unicode_ci&parseTime=true&loc=Asia%2FShanghai"
	dsn := fmt.Sprintf("assistant_test:assistant@tcp(172.30.0.1:3306)/assistant_test?%s", []interface{}{
		parameters,
	}...)
	// 配置数据库
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		NamingStrategy: schema.NamingStrategy{SingularTable: true},
		Logger: logger.New(
			log.New(os.Stdout, "\r\n", log.LstdFlags), // io writer
			logger.Config{
				SlowThreshold:             time.Second, // Slow SQL threshold
				LogLevel:                  logger.Info, // Log level
				IgnoreRecordNotFoundError: true,        // Ignore ErrRecordNotFound error for logger
				ParameterizedQueries:      false,       // Don't include params in the SQL log
				Colorful:                  false,       // Disable color
			},
		),
	})
	if err != nil {
		return nil, err
	}
	return NewManager(db, nil)
}

func TestManager_Install(t *testing.T) {
	manager, err := setupManager()
	if err != nil {
		t.Fatal(err)
	}
	handler, err := manager.Install("../task/ping/ping.go")
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println(handler.ID(), handler.Info())
}

func TestManager_Update(t *testing.T) {
	manager, err := setupManager()
	if err != nil {
		t.Fatal(err)
	}
	err = manager.Update("ping", "../task/ping2/ping.go")
	if err != nil {
		t.Fatal(err)
	}
}

func TestManager_Load(t *testing.T) {
	manager, err := setupManager()
	if err != nil {
		t.Fatal(err)
	}
	handler, err := manager.Load("ping")
	if err != nil {
		t.Fatal(err)
	}
	t.Log(handler.Info().ID, handler.Info().Description)
	handler, err = manager.Load("test2")
	if err != nil {
		t.Fatal(err)
	}
	if handler == nil {
		t.Log("任务不存在")
	}
}

func TestManager_Bind(t *testing.T) {
	manager, err := setupManager()
	if err != nil {
		t.Fatal(err)
	}
	handler, err := manager.Load("ping")
	if err != nil {
		t.Fatal(err)
	}
	if err = manager.Bind(handler, "@every 5s", "test1"); err != nil {
		t.Fatal(err)
	}
	if err = manager.Bind(handler, "@every 2s", "test2"); err != nil {
		t.Fatal(err)
	}

	time.Sleep(30 * time.Second)
}

func TestManager_Reload(t *testing.T) {
	manager, err := setupManager()
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Second)
	err = manager.Reload("ping")
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(30 * time.Second)
}

func TestPluginManager_Unbind(t *testing.T) {
	manager, err := setupManager()
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := manager.Unbind(1); err != nil {
		t.Fatal(err)
	} else {
		t.Log("解绑:", ok)
	}
	if ok, err := manager.Unbind(28); err != nil {
		t.Fatal(err)
	} else {
		t.Log("解绑:", ok)
	}
	time.Sleep(30 * time.Second)
}
