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
	return NewPluginManager(db)
}

func TestPluginManager_InstallPlugin(t *testing.T) {
	manager, err := setupManager()
	if err != nil {
		t.Fatal(err)
	}
	plugin, err := manager.InstallPlugin("../plugins/demo.go")
	if err != nil {
		t.Fatal(err)
	}
	t.Log(plugin.Info().Keyword, plugin.Info().Description)
	runtime.GC()
}

func TestPluginManager_ListPlugin(t *testing.T) {
	manager, err := setupManager()
	if err != nil {
		t.Fatal(err)
	}
	addons, err := manager.ListPlugin(true)
	if err != nil {
		t.Fatal(err)
	}
	for _, addon := range *addons {
		t.Log(addon.ID, addon.BindKeyword, addon.Description)
	}

	plugin, err := manager.LoadPlugin("ping")
	if err != nil {
		t.Fatal(err)
	}
	if err = manager.BindPlugin(plugin.Keyword(), plugin, false); err != nil {
		t.Fatal(err)
	}

	addons, _ = manager.ListPlugin(false)
	for _, addon := range *addons {
		t.Log(addon.ID, addon.BindKeyword, addon.Description)
	}
}

func TestPluginManager_LoadPlugin(t *testing.T) {
	manager, err := setupManager()
	if err != nil {
		t.Fatal(err)
	}
	plugin, err := manager.LoadPlugin("ping")
	if err != nil {
		t.Fatal(err)
	}
	t.Log(plugin.Info().Keyword, plugin.Info().Description)
	plugin, err = manager.LoadPlugin("test2")
	if err != nil {
		t.Fatal(err)
	}
	if plugin == nil {
		t.Log("插件不存在")
	}
}

func TestPluginManager_BindPlugin(t *testing.T) {
	manager, err := setupManager()
	if err != nil {
		t.Fatal(err)
	}
	plugin, err := manager.LoadPlugin("ping")
	if err != nil {
		t.Fatal(err)
	}
	if err = manager.BindPlugin(plugin.Info().Keyword, plugin, false); err != nil {
		t.Fatal(err)
	}
	if err = manager.BindPlugin("ping", plugin, false); err != nil {
		t.Fatal(err)
	}
	if err = manager.BindPlugin("ping", plugin, true); err != nil {
		t.Fatal(err)
	}

	newPlugin1, _ := NewCodePlugin(plugin.Info().Package, plugin.Info().Code)
	newPlugin1.info.Code += "\n"
	if err = manager.BindPlugin("test", newPlugin1, false); err != nil {
		t.Fatal(err)
	}

	newPlugin2, _ := NewCodePlugin(plugin.Info().Package, plugin.Info().Code+"\n")
	newPlugin2.info.ID += "_"
	if err = manager.BindPlugin("test", newPlugin2, false); err != nil {
		t.Log(err)
	}
}

func TestPluginManager_UninstallPlugin(t *testing.T) {
	manager, err := setupManager()
	if err != nil {
		t.Fatal(err)
	}

	plugin, _ := manager.LoadPlugin("ping")
	_ = manager.BindPlugin(plugin.Info().Keyword, plugin, false)

	ok, err := manager.UninstallPlugin("ping")
	if err != nil {
		t.Fatal(err)
	}
	t.Log("卸载插件:", ok)
	ok, err = manager.UninstallPlugin("ping")
	if err != nil {
		t.Fatal(err)
	}
	t.Log("卸载插件:", ok)

}
