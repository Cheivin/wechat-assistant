package plugin

import (
	"fmt"
	_ "github.com/eatmoreapple/openwechat"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	_ "gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
	"log"
	"os"
	"runtime"
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
	return NewManager(db)
}

func TestPluginManager_Install(t *testing.T) {
	manager, err := setupManager()
	if err != nil {
		t.Fatal(err)
	}
	plugin, err := manager.Install("../plugins/demo.go")
	if err != nil {
		t.Fatal(err)
	}
	t.Log(plugin.Info().Keyword, plugin.Info().Description)
	runtime.GC()
}

func TestPluginManager_Update(t *testing.T) {
	manager, err := setupManager()
	if err != nil {
		t.Fatal(err)
	}
	err = manager.Update("ping", "../plugins/demo2.go")
	if err != nil {
		t.Fatal(err)
	}
}

func TestPluginManager_Load(t *testing.T) {
	manager, err := setupManager()
	if err != nil {
		t.Fatal(err)
	}
	plugin, err := manager.Load("ping")
	if err != nil {
		t.Fatal(err)
	}
	t.Log(plugin.Info().Keyword, plugin.Info().Description)
	plugin, err = manager.Load("test2")
	if err != nil {
		t.Fatal(err)
	}
	if plugin == nil {
		t.Log("插件不存在")
	}
}

func TestPluginManager_Bind(t *testing.T) {
	manager, err := setupManager()
	if err != nil {
		t.Fatal(err)
	}
	plugin, err := manager.Load("ping")
	if err != nil {
		t.Fatal(err)
	}
	if err = manager.Bind(plugin.Info().Keyword, plugin, false); err != nil {
		t.Fatal(err)
	}
	if err = manager.Bind("ping2", plugin, false); err != nil {
		t.Fatal(err)
	}
	if err = manager.Bind("ping2", plugin, true); err != nil {
		t.Fatal(err)
	}
}

func TestPluginManager_Unbind(t *testing.T) {
	manager, err := setupManager()
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := manager.Unbind("ping3"); err != nil {
		t.Fatal(err)
	} else {
		t.Log("解绑:", ok)
	}
	if ok, err := manager.Unbind("ping2"); err != nil {
		t.Fatal(err)
	} else {
		t.Log("解绑:", ok)
	}
}

func TestPluginManager_List(t *testing.T) {
	manager, err := setupManager()
	if err != nil {
		t.Fatal(err)
	}
	addons, err := manager.List(true)
	if err != nil {
		t.Fatal(err)
	}
	for _, addon := range *addons {
		t.Log("FromDB", addon.ID, addon.BindKeyword, addon.Description)
	}

	addons, _ = manager.List(false)
	for _, addon := range *addons {
		t.Log("FromLoaded", addon.ID, addon.BindKeyword, addon.Description)
	}
}

func TestPluginManager_Uninstall(t *testing.T) {
	manager, err := setupManager()
	if err != nil {
		t.Fatal(err)
	}

	plugin, _ := manager.Load("ping")
	_ = manager.Bind(plugin.Info().Keyword, plugin, false)

	ok, err := manager.Uninstall("ping")
	if err != nil {
		t.Fatal(err)
	}
	t.Log("卸载插件:", ok)
	ok, err = manager.Uninstall("ping")
	if err != nil {
		t.Fatal(err)
	}
	t.Log("卸载插件:", ok)

}
