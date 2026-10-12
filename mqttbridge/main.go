package main

import (
	"log"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/infinimesh/addons/sdk/api"
	"github.com/infinimesh/addons/sdk/message"
	"github.com/infinimesh/addons/sdk/runner"
	shadowpb "github.com/infinimesh/proto/shadow"
)

func clientOptions(s *settings) *mqtt.ClientOptions {
	o := mqtt.NewClientOptions().
		AddBroker(s.Broker).
		SetClientID(s.ClientID).
		SetUsername(s.Username).
		SetPassword(s.Password).
		SetCleanSession(true).
		SetAutoReconnect(true).
		SetMaxReconnectInterval(30 * time.Second).
		SetWriteTimeout(s.Timeout).
		SetConnectTimeout(s.Timeout)
	if s.TLS != nil {
		o.SetTLSConfig(s.TLS)
	}
	o.OnConnect = func(mqtt.Client) { log.Printf("connected to %s\n", s.Broker) }
	o.OnConnectionLost = func(_ mqtt.Client, err error) { log.Printf("connection to %s lost: %s\n", s.Broker, err) }
	return o
}

func main() {
	s, err := loadSettings()
	if err != nil {
		log.Fatal(err)
	}
	client := mqtt.NewClient(clientOptions(s))
	// Keep trying the first connect and log why it fails, such as a wrong
	// password. After that paho reconnects by itself.
	go func() {
		for {
			tok := client.Connect()
			tok.Wait()
			if tok.Error() == nil {
				return
			}
			log.Printf("connect to %s failed: %s\n", s.Broker, tok.Error())
			time.Sleep(5 * time.Second)
		}
	}()
	defer client.Disconnect(1000)

	runner.Run(func(obj api.Object, sh *shadowpb.Shadow) {
		_, body, ok := message.Build(obj, sh, s.OnlyReported, time.Now())
		if !ok {
			return
		}
		t := topic(s.Topic, obj.UID, obj.Name)
		tok := client.Publish(t, s.QoS, s.Retain, body)
		if !tok.WaitTimeout(s.Timeout) {
			log.Printf("publish to %s timed out\n", t)
			return
		}
		if err := tok.Error(); err != nil {
			log.Printf("publish to %s failed: %s\n", t, err)
			return
		}
		log.Printf("published to %s\n", t)
	})
}
