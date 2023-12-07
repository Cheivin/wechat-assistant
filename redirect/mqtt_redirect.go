package redirect

import (
	"encoding/json"
	"github.com/eclipse/paho.mqtt.golang"
	"log"
	"os"
	"strings"
	"time"
)

type MQTTRedirect struct {
	Broker         string `value:"mqtt.broker"`
	Username       string `value:"mqtt.username"`
	Password       string `value:"mqtt.password"`
	Prefix         string `value:"mqtt.prefix"`
	client         mqtt.Client
	commandHandler func(request BotCommand)
}

const subTopic = "command/group/default"

func (r *MQTTRedirect) AfterPropertiesSet() {
	mqtt.ERROR = log.New(os.Stdout, "", 0)
	opts := mqtt.NewClientOptions().
		AddBroker(r.Broker).
		SetUsername(r.Username).
		SetPassword(r.Password).
		SetCleanSession(false).
		SetConnectRetry(true).
		SetAutoReconnect(true).
		SetConnectRetryInterval(5 * time.Second).
		SetOnConnectHandler(func(client mqtt.Client) {
			// 订阅主题
			if token := r.client.Subscribe(r.Prefix+subTopic, 1, func(_ mqtt.Client, msg mqtt.Message) {
				log.Println("收到命令消息:", msg.Topic(), string(msg.Payload()))
				if r.commandHandler != nil {
					request := new(BotCommand)
					if err := json.Unmarshal(msg.Payload(), request); err != nil {
						log.Println(err)
					} else {
						r.commandHandler(*request)
					}
				}
				msg.Ack()
			}); token.Wait() && token.Error() != nil {
				log.Println("订阅主题:", r.Prefix+subTopic, "失败", token.Error())
			} else {
				log.Println("订阅主题:", r.Prefix+subTopic, "成功")
			}
		}).
		SetClientID(strings.ReplaceAll(r.Prefix, "/", "_") + "wx_assistant")

	opts.SetKeepAlive(60 * time.Second)
	opts.SetPingTimeout(10 * time.Second)

	r.client = mqtt.NewClient(opts)
	if token := r.client.Connect(); token.Wait() && token.Error() != nil {
		panic(token.Error())
	}

	log.Println("MQTT连接成功!")
}

func (r *MQTTRedirect) Destroy() {
	if r.client != nil {
		r.client.Disconnect(250)
	}
}

func (r *MQTTRedirect) SetCommandHandler(fn func(BotCommand)) {
	r.commandHandler = fn
}

func (r *MQTTRedirect) RedirectCommand(message CommandMessage) bool {
	bytes, _ := json.Marshal(message)
	topic := r.Prefix + "msg/group/" + message.GID
	token := r.client.Publish(topic, 1, false, bytes)
	return token.Wait()
}

func (r *MQTTRedirect) RedirectMessage(message *Message) bool {
	bytes, _ := json.Marshal(message)
	topic := r.Prefix + "broadcast/msg/group/" + message.GID
	token := r.client.Publish(topic, 0, false, bytes)
	return token.Wait()
}
