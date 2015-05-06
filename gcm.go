// Copyright 2015 Google Inc. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package gcm provides send and receive GCM functionality.
package gcm

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"

	"github.com/mattn/go-xmpp"
)

const (
	httpAddress = "https://android.googleapis.com/gcm/send"
	xmppHost    = "gcm.googleapis.com"
	xmppPort    = "5235"
	xmppAddress = xmppHost + ":" + xmppPort
)

var (
	// DebugMode determines whether to have verbose logging.
	DebugMode = true
)

func debug(m string, v interface{}) {
	if DebugMode {
		log.Printf(m+":%+v", v)
	}
}

// Message is a downstream (server -> client) message.
type Message struct {
	To                    string       `json:"to,omitempty"`
	RegistrationIds       []string     `json:"registration_ids,omitempty"`
	NotificationKey       string       `json:"notification_key,omitempty"`
	CollapseKey           string       `json:"collapse_key,omitempty"`
	DelayWhileIdle        bool         `json:"delay_while_idle,omitempty"`
	TimeToLive            uint         `json:"time_to_live,omitempty"`
	RestrictedPackageName string       `json:"restricted_package_name,omitempty"`
	DryRun                bool         `json:"dry_run,omitempty"`
	Data                  Data         `json:"data,omitempty"`
	Notification          Notification `json:"notification,omitempty"`
}

// upstream is an upstream (client -> server) message.
type upstream struct {
	From      string `json:"from"`
	Category  string `json:"category"`
	MessageId string `json:"message_id"`
	Data      Data   `json:"data,omitempty"`
}

// Data is the custom data passed with every GCM message.
type Data map[string]interface{}

// A GCM notification
type Notification struct {
	Text        string `json:"text,omitempty"`
	Title       string `json:"title,omitempty"`
	Badge       string `json:"badge,omitempty"`
	ClickAction string `json:"click-action,omitempty"`
}

// StopChannel is a channel type to stop the server.
type StopChannel chan bool

// MessageHandler is the type for a function that handles a GCM message.
type MessageHandler func(from string, d Data) error

// Send sends a downstream message to a given device.
func Send(apiKey string, m Message) error {
	if m.To == "" && m.NotificationKey == "" && len(m.RegistrationIds) == 0 {
		return errors.New("One of To, RegistrationIds or NotificationKey must be defined for the message.")
	}
	bs, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("error marshalling message>%v", err)
	}
	debug("sending", string(bs))
	req, err := http.NewRequest("POST", httpAddress, bytes.NewReader(bs))
	if err != nil {
		return fmt.Errorf("error creating request>%v", err)
	}
	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("Authorization", authHeader(apiKey))
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("error making request>%v", err)
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("error reading body>%v", err)
	}
	debug("response body", string(body))
	return nil
}

// Listen blocks and connects to GCM waiting for messages, calling the handler
// for every "normal" type message. An optional stop channel can be provided to
// stop listening.
func Listen(senderId, apiKey string, h MessageHandler, stop StopChannel) error {
	cl, err := xmpp.NewClient(xmppAddress, xmppUser(senderId), apiKey, DebugMode)
	if err != nil {
		return fmt.Errorf("error connecting client>%v", err)
	}
	if stop != nil {
		go func() {
			select {
			case <-stop:
				cl.Close()
			}
		}()
	}
	for {
		stanza, err := cl.Recv()
		if err != nil {
			// This is likely fatal, so return.
			return fmt.Errorf("error on Recv>%v", err)
		}
		v, ok := stanza.(xmpp.Chat)
		if !ok {
			continue
		}
		switch v.Type {
		case "normal":
			up := &upstream{}
			json.Unmarshal([]byte(v.Other[0]), up)
			go h(up.From, up.Data)
		case "error":
			debug("error response", v)
		}
	}
	return nil
}

// authHeader generates an authorization header value for an api key.
func authHeader(apiKey string) string {
	return fmt.Sprintf("key=%v", apiKey)
}

// xmppUser generates an xmpp username from a sender ID.
func xmppUser(senderId string) string {
	return senderId + "@" + xmppHost
}
