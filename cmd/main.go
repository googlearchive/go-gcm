package main

import (
	"os"

	"github.com/aliafshar/toylog"
	"github.com/codegangsta/cli"

	"github.com/aliafshar/gcmd"
)

const (
	version = "1.0"
	author  = "afshar@google.com"

	actionKey      = "action"
	registerAction = "register"
	registerTopic  = "/topics/newuser"
)

type User struct {
	Name       string `json:"name,omitempty"`
	InstanceId string `json:"instance_id"`
}

type friendlyPing struct {
	apiKey   string
	senderId string
	users    map[string]*User
}

func newFriendlyPing(apiKey, senderId string) *friendlyPing {
	return &friendlyPing{
		apiKey:   apiKey,
		senderId: senderId,
		users:    make(map[string]*User),
	}
}

func (fp *friendlyPing) addUser(name, instanceId string) {
	fp.users[name] = &User{Name: name, InstanceId: instanceId}
}

func (fp *friendlyPing) broadcastNewuser(u *User) error {
	return gcm.Send(fp.apiKey, registerTopic, gcm.Message{"user": u})
}

func (fp *friendlyPing) sendUserlist(to string) error {
	return gcm.Send(fp.apiKey, to, gcm.Message{"users": fp.users})
}

func (fp *friendlyPing) onMessage(from string, m gcm.Message) error {
	// TODO (afshar) check the action
	v, ok := m["name"]
	if !ok {
		toylog.Errorln("No name.")
	}
	name, ok := v.(string)
	if !ok {
		toylog.Errorln("Bad name.", v)
	}

	w, ok := m["instanceId"]
	if !ok {
		toylog.Errorln("No instance ID.")
	}
	instanceId, ok := w.(string)
	if !ok {
		toylog.Errorln("Bad instance ID.", v)
	}

	// Add to list
	err := fp.broadcastNewuser(&User{Name: name, InstanceId: instanceId})
	if err != nil {
		toylog.Errorln("Can't broadcast.")
	}
	err = fp.sendUserlist(from)
	if err != nil {
		toylog.Errorln("Can't send user list.")
	}
	return nil
}

func (fp *friendlyPing) listen() error {
	return gcm.Listen(fp.senderId, fp.apiKey, fp.onMessage)
}

func start(c *cli.Context) {
	toylog.Infoln("starting FriendlyPing server")
	fp := newFriendlyPing(c.String("apiKey"), c.String("senderId"))
	err := fp.listen()
	if err != nil {
		panic(err)
	}
}

var flags = []cli.Flag{
	cli.StringFlag{
		Name:   "apiKey",
		Usage:  "GCM api key",
		EnvVar: "GCM_API_KEY",
	},
	cli.StringFlag{
		Name:   "senderId",
		Usage:  "GCM Sender ID",
		EnvVar: "GCM_SENDER_ID",
	},
}

func main() {
	app := cli.NewApp()
	app.Name = "friendlyping"
	app.Usage = "friendlyping server"
	app.Version = version
	app.Action = start
	app.Author = author
	app.Flags = flags
	app.Run(os.Args)
}
