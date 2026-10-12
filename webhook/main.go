package main

import (
	"log"
	"time"

	"github.com/infinimesh/addons/sdk/api"
	"github.com/infinimesh/addons/sdk/message"
	"github.com/infinimesh/addons/sdk/runner"
	shadowpb "github.com/infinimesh/proto/shadow"
)

func main() {
	s, err := loadSettings()
	if err != nil {
		log.Fatal(err)
	}
	x := newSender(s)
	runner.Run(func(obj api.Object, sh *shadowpb.Shadow) {
		_, body, ok := message.Build(obj, sh, s.OnlyReported, time.Now())
		if !ok {
			return
		}
		if err := x.send(obj.UID, body); err != nil {
			log.Printf("webhook for device %s failed: %s\n", obj.UID, err)
			return
		}
		log.Printf("webhook sent for device %s\n", obj.UID)
	})
}
