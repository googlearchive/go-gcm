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
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"time"

	"github.com/jpillora/backoff"
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
	// Default Min and Max delay for backoff.
	DefaultMinBackoff = 1 * time.Second
	DefaultMaxBackoff = 10 * time.Second
	retryableErrors   = map[string]bool{
		"Unavailable":            true,
		"SERVICE_UNAVAILABLE":    true,
		"InternalServerError":    true,
		"INTERNAL_SERVER_ ERROR": true,
		// TODO(silvano): should we backoff with the same strategy on
		// DeviceMessageRateExceeded and TopicsMessageRateExceeded
	}
)

func debug(m string, v interface{}) {
	if DebugMode {
		log.Printf(m+":%+v", v)
	}
}

// A GCM Http message.
type HttpMessage struct {
	To                    string       `json:"to,omitempty"`
	RegistrationIds       []string     `json:"registration_ids,omitempty"`
	CollapseKey           string       `json:"collapse_key,omitempty"`
	Priority              uint         `json:"priority,omitempty"`
	ContentAvailable      bool         `json:"content_available,omitempty"`
	DelayWhileIdle        bool         `json:"delay_while_idle,omitempty"`
	TimeToLive            uint         `json:"time_to_live,omitempty"`
	RestrictedPackageName string       `json:"restricted_package_name,omitempty"`
	DryRun                bool         `json:"dry_run,omitempty"`
	Data                  Data         `json:"data,omitempty"`
	Notification          Notification `json:"notification,omitempty"`
}

// A GCM Xmpp message.
type XmppMessage struct {
	To                       string       `json:"to"`
	MessageId                string       `json:"message_id"`
	CollapseKey              string       `json:"collapse_key,omitempty"`
	Priority                 uint         `json:"priority,omitempty"`
	ContentAvailable         bool         `json:"content_available,omitempty"`
	DelayWhileIdle           bool         `json:"delay_while_idle,omitempty"`
	TimeToLive               uint         `json:"time_to_live,omitempty"`
	DeliveryReceiptRequested bool         `json:"delivery_receipt_requested,omitempty"`
	DryRun                   bool         `json:"dry_run,omitempty"`
	Data                     Data         `json:"data,omitempty"`
	Notification             Notification `json:"notification,omitempty"`
}

// HttpResponse is the GCM connection server response to an HTTP downstream message request
type HttpResponse struct {
	MulticastId  uint     `json:"multicast_id,omitempty"`
	Success      uint     `json:"success,omitempty"`
	Failure      uint     `json:"failure,omitempty"`
	CanonicalIds uint     `json:"canonical_ids,omitempty"`
	Results      []Result `json:"results,omitempty"`
	MessageId    uint     `json:"message_id,omitempty"`
	Error        string   `json:"error,omitempty"`
}

// Result represents the status of a processed message
type Result struct {
	MessageId      string `json:"message_id,omitempty"`
	RegistrationId string `json:"registration_id,omitempty"`
	Error          string `json:"error,omitempty"`
}

// XmppResponse is the GCM connection server response to an Xmpp downstream message request
type XmppResponse struct {
	From             string `json:"from"`
	MessageId        string `json:"message_id"`
	MessageType      string `json:"message_type"`
	RegistrationId   string `json:"registration_id,omitempty"`
	Error            string `json:"error,omitempty"`
	ErrorDescription string `json:"error_description,omitempty"`
}

// upstream is an upstream (client -> server) message.
type upstream struct {
	From      string `json:"from"`
	Category  string `json:"category"`
	MessageId string `json:"message_id"`
	Data      Data   `json:"data,omitempty"`
}

// use to compute results for multicast messages with retries
type multicastResultsState map[string]*Result

// Data is the custom data passed with every GCM message.
type Data map[string]interface{}

// A GCM notification
type Notification struct {
	Title        string `json:"title,omitempty"`
	Body         string `json:"body,omitempty"`
	Icon         string `json:"icon,omitempty"`
	Sound        string `json:"sound,omitempty"`
	Badge        string `json:"badge,omitempty"`
	Tag          string `json:"color,omitempty"`
	ClickAction  string `json:"click_action,omitempty"`
	BodyLocKey   string `json:"body_loc_key,omitempty"`
	BodyLocArgs  string `json:"body_loc_args,omitempty"`
	TitleLocArgs string `json:"title_loc_args,omitempty"`
	TitleLocKey  string `json:"title_loc_key,omitempty"`
}

// StopChannel is a channel type to stop the server.
type StopChannel chan bool

// MessageHandler is the type for a function that handles a GCM message.
type MessageHandler func(from string, d Data) error

// TODO: add client as dependency
type httpClient interface {
	send(apiKey string, m HttpMessage) (*HttpResponse, error)
	getRetryAfter() string
}

// Client for Gcm Http Connection Server
type httpGcmClient struct {
	GcmURL     string
	HttpClient *http.Client
	retryAfter string
}

// Send a message to the GCM HTTP Connection Server
func (c *httpGcmClient) send(apiKey string, m HttpMessage) (*HttpResponse, error) {
	bs, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("error marshalling message>%v", err)
	}
	debug("sending", string(bs))
	req, err := http.NewRequest("POST", c.GcmURL, bytes.NewReader(bs))
	if err != nil {
		return nil, fmt.Errorf("error creating request>%v", err)
	}
	req.Header.Add(http.CanonicalHeaderKey("Content-Type"), "application/json")
	req.Header.Add(http.CanonicalHeaderKey("Authorization"), authHeader(apiKey))
	httpResp, err := c.HttpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error sending request to HTTP connection server>%v", err)
	}
	gcmResp := &HttpResponse{}
	body, err := ioutil.ReadAll(httpResp.Body)
	defer httpResp.Body.Close()
	if err != nil {
		return gcmResp, fmt.Errorf("error reading http response body>%v", err)
	}
	debug("received body", string(body))
	err = json.Unmarshal(body, &gcmResp)
	if err != nil {
		return gcmResp, fmt.Errorf("error unmarshaling json from body: %v", err)
	}
	// TODO(silvano): this is assuming that the header contains seconds instead of a date, need to check
	c.retryAfter = httpResp.Header.Get(http.CanonicalHeaderKey("Retry-After"))
	return gcmResp, nil
}

func (c httpGcmClient) getRetryAfter() string {
	return c.retryAfter
}

type backoffProvider interface {
	sendAnother() bool
	setMin(min time.Duration)
	wait()
}

type exponentialBackoff struct {
	b            backoff.Backoff
	currentDelay time.Duration
}

func newExponentialBackoff() *exponentialBackoff {
	b := &backoff.Backoff{
		Min:    DefaultMinBackoff,
		Max:    DefaultMaxBackoff,
		Jitter: true,
	}
	return &exponentialBackoff{b: *b, currentDelay: b.Duration()}
}

func (eb exponentialBackoff) sendAnother() bool {
	return eb.currentDelay <= eb.b.Max
}

func (eb *exponentialBackoff) setMin(min time.Duration) {
	eb.b.Min = min
	if (eb.currentDelay) < min {
		eb.currentDelay = min
	}
}

func (eb exponentialBackoff) wait() {
	time.Sleep(eb.currentDelay)
	eb.currentDelay = eb.b.Duration()
}

// Send a message using the HTTP GCM connection server.
func SendHttp(apiKey string, m HttpMessage) (*HttpResponse, error) {
	c := &httpGcmClient{httpAddress, &http.Client{}, "0"}
	b := newExponentialBackoff()
	return sendHttp(apiKey, m, c, b)
}

func sendHttp(apiKey string, m HttpMessage, c httpClient, b backoffProvider) (*HttpResponse, error) {
	gcmResp := &HttpResponse{}
	var multicastId uint
	targets, err := messageTargetAsStringsArray(m)
	if err != nil {
		return gcmResp, fmt.Errorf("error extracting target from message: %v", err)
	}
	// make a copy of the targets to keep track of results during retries
	localTo := make([]string, len(targets))
	copy(localTo, targets)
	resultsState := &multicastResultsState{}
	for b.sendAnother() {
		gcmResp, err := c.send(apiKey, m)
		if err != nil {
			return gcmResp, fmt.Errorf("error sending request to GCM HTTP server: %v", err)
		}
		if len(gcmResp.Results) > 0 {
			doRetry, toRetry, err := checkResults(gcmResp.Results, localTo, *resultsState)
			multicastId = gcmResp.MulticastId
			if err != nil {
				return gcmResp, fmt.Errorf("error checking GCM results: %v", err)
			}
			if doRetry {
				retryAfter, err := time.ParseDuration(c.getRetryAfter())
				if err != nil {
					b.setMin(retryAfter)
				}
				localTo = make([]string, len(toRetry))
				copy(localTo, toRetry)
				if m.RegistrationIds != nil {
					m.RegistrationIds = toRetry
				}
				b.wait()
				continue
			} else {
				break
			}
		} else {
			break
		}
	}
	// if it was multicast, reconstruct response in case there have been retries
	if len(targets) > 1 {
		gcmResp = buildRespForMulticast(targets, *resultsState, multicastId)
	}
	return gcmResp, nil
}

func buildRespForMulticast(to []string, mrs multicastResultsState, mid uint) *HttpResponse {
	resp := &HttpResponse{}
	resp.MulticastId = mid
	resp.Results = make([]Result, len(to))
	for i, regId := range to {
		result, ok := mrs[regId]
		if !ok {
			continue
		}
		resp.Results[i] = *result
		if result.MessageId != "" {
			resp.Success++
		} else if result.Error != "" {
			resp.Failure++
		}
		if result.RegistrationId != "" {
			resp.CanonicalIds++
		}
	}
	return resp
}

func messageTargetAsStringsArray(m HttpMessage) ([]string, error) {
	if m.RegistrationIds != nil {
		return m.RegistrationIds, nil
	} else if m.To != "" {
		target := []string{m.To}
		return target, nil
	}
	target := []string{}
	return target, fmt.Errorf("can't find any valid target field in message.")
}

func checkResults(gcmResults []Result, recipients []string, resultsState multicastResultsState) (doRetry bool, toRetry []string, err error) {
	doRetry = false
	toRetry = []string{}
	for i := 0; i < len(gcmResults); i++ {
		result := gcmResults[i]
		regId := recipients[i]
		resultsState[regId] = &result
		if result.Error != "" {
			if retryableErrors[result.Error] {
				toRetry = append(toRetry, regId)
				if doRetry == false {
					doRetry = true
				}
			}
		}
	}
	return doRetry, toRetry, nil
}

// Listen blocks and connects to GCM waiting for messages, calling the handler
// for every "normal" type message. An optional stop channel can be provided to
// stop listening.
func Listen(senderId, apiKey string, h MessageHandler, stop StopChannel) error {
	cl, err := xmpp.NewClient(xmppAddress, xmppUser(senderId), apiKey, DebugMode)
	// TODO(silvano): check for CONNECTION_DRAINING
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
		case "control":
			debug("debugging control message: %v", v)
		case "error":
			debug("error response %v", v)
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
