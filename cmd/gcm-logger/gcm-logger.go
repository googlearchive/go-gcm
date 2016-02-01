// Program gcm-logger logs and echoes as a GCM "server".
package main

import (
	"github.com/alecthomas/kingpin"
	"github.com/aliafshar/toylog"
	"github.com/google/go-gcm"
)

var (
	serverKey = kingpin.Flag("server_key", "The server key to use for GCM.").Short('k').Required().String()
	senderId  = kingpin.Flag("sender_id", "The sender ID to use for GCM.").Short('s').Required().String()
)

// onMessage receives messages, logs them, and echoes a response.
func onMessage(cm gcm.CcsMessage) error {
	toylog.Infoln("Message, from:", cm.From, "with:", cm.Data)
	// Echo the message with a tag.
	cm.Data["echoed"] = true
	m := gcm.HttpMessage{To: cm.From, Data: cm.Data}
	r, err := gcm.SendHttp(*serverKey, m)
	if err != nil {
		toylog.Errorln("Error sending message.", err)
		return err
	}
	toylog.Infof("Sent message. %+v -> %+v", m, r)
	return nil
}

func main() {
	toylog.Infoln("GCM Logger, starting.")
	kingpin.Parse()
	gcm.Listen(*senderId, *serverKey, onMessage, nil)
}
