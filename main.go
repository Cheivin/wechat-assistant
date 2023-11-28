package main

import (
	"context"
	"crypto/tls"
	"github.com/cheivin/di"
	"github.com/eatmoreapple/openwechat"
	"github.com/go-resty/resty/v2"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"
	"wechat-assistant/bot"
	"wechat-assistant/database"
	"wechat-assistant/lock"
	"wechat-assistant/plugin"
	"wechat-assistant/redirect"
)

func Select[V any](condition bool, True V, False V) V {
	if condition {
		return True
	}
	return False
}

var container = di.New()

func init() {
	container.SetPropertyMap(map[string]interface{}{
		"app": map[string]interface{}{
			"port": Select(os.Getenv("APP_PORT") != "", os.Getenv("APP_PORT"), "8080"),
		},
		"bot": map[string]interface{}{
			"data":   filepath.Join(os.Getenv("DATA"), "storage.json"),
			"secret": Select(os.Getenv("SECRET") != "", os.Getenv("SECRET"), "MZXW6YTBOI======"),
		},
		"db": map[string]interface{}{
			"type":       os.Getenv("DB"),
			"file":       filepath.Join(os.Getenv("DATA"), "data.db"),
			"host":       os.Getenv("MYSQL_HOST"),
			"port":       os.Getenv("MYSQL_PORT"),
			"username":   os.Getenv("MYSQL_USERNAME"),
			"password":   os.Getenv("MYSQL_PASSWORD"),
			"database":   os.Getenv("MYSQL_DATABASE"),
			"parameters": os.Getenv("MYSQL_PARAMETERS"),
		},
		"mqtt": map[string]interface{}{
			"broker":   os.Getenv("MQTT_BROKER"),
			"username": os.Getenv("MQTT_USERNAME"),
			"password": os.Getenv("MQTT_PASSWORD"),
			"prefix":   os.Getenv("MQTT_PREFIX"),
		},
	})
}

func initClient() *resty.Client {
	client := resty.New()

	var (
		dnsResolverIP        = "223.5.5.5:53"
		dnsResolverProto     = "udp"
		dnsResolverTimeoutMs = 5000
	)

	dialer := &net.Dialer{
		Resolver: &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
				d := net.Dialer{
					Timeout: time.Duration(dnsResolverTimeoutMs) * time.Millisecond,
				}
				return d.DialContext(ctx, dnsResolverProto, dnsResolverIP)
			},
		},
	}
	client.SetTransport(&http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		TLSClientConfig:       &tls.Config{InsecureSkipVerify: true},
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialer.DialContext(ctx, network, addr)
		},
	})
	return client
}

func main() {
	wechatBot := openwechat.DefaultBot(openwechat.Desktop) // 桌面模式

	container.RegisterNamedBean("resty", initClient()).
		RegisterNamedBean("bot", wechatBot)

	// 数据库配置
	if container.GetProperty("db.type") == "mysql" {
		container.Provide(database.MysqlConfiguration{})
	} else {
		container.Provide(database.SqliteConfiguration{})
	}

	// 消息转发器
	broker := container.GetProperty("mqtt.broker")
	if broker != nil && broker.(string) != "" {
		container.Provide(redirect.MQTTRedirect{})
	}

	container.Provide(lock.DBLocker{}).
		Provide(plugin.Manager{}).
		Provide(bot.MsgHandler{}).
		Provide(bot.MsgSender{}).
		Provide(bot.Manager{}).
		Provide(WebContainer{}).
		Load()
	container.Serve(wechatBot.Context())
}
