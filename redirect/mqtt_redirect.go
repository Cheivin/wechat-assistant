package redirect

import (
	"encoding/json"
	"github.com/eclipse/paho.mqtt.golang"
	"log"
	"os"
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

func (r *MQTTRedirect) AfterPropertiesSet() {
	mqtt.ERROR = log.New(os.Stdout, "", 0)
	opts := mqtt.NewClientOptions().
		AddBroker(r.Broker).
		SetUsername(r.Username).
		SetPassword(r.Password).
		SetCleanSession(false).
		SetConnectRetry(true).
		SetConnectRetryInterval(5 * time.Second).
		SetClientID("wx_assistant")

	opts.SetKeepAlive(60 * time.Second)
	opts.SetPingTimeout(10 * time.Second)

	r.client = mqtt.NewClient(opts)
	if token := r.client.Connect(); token.Wait() && token.Error() != nil {
		panic(token.Error())
	}

	log.Println("MQTT连接成功!")

	// 订阅主题
	if token := r.client.Subscribe(r.Prefix+"command/group/#", 2, func(_ mqtt.Client, msg mqtt.Message) {
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
		panic(token.Error())
	}
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

func (r *MQTTRedirect) RedirectMessage(message Message) bool {
	bytes, _ := json.Marshal(message)
	topic := r.Prefix + "broadcast/msg/group/" + message.GID
	token := r.client.Publish(topic, 0, false, bytes)
	return token.Wait()
}