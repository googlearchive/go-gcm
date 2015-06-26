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

package gcm

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"
	"time"
)

func assertEqual(t *testing.T, v, e interface{}) {
	if v != e {
		t.Fatalf("%#v != %#v", v, e)
	}
}

func assertDeepEqual(t *testing.T, v, e interface{}) {
	if !reflect.DeepEqual(v, e) {
		t.Fatalf("%#v != %#v", v, e)
	}
}

type stubBackoff struct {
}

func (sb stubBackoff) sendAnother() bool {
	return true
}

func (sb stubBackoff) setMin(min time.Duration) {
}

func (sb stubBackoff) wait() {
}

var multicastTos = []string{"4", "8", "15", "16", "23", "42"}
var multicastReply = []string{`{ "multicast_id": 216,
  "success": 3,
  "failure": 3,
  "canonical_ids": 1,
  "results": [
    { "message_id": "1:0408" },
    { "error": "Unavailable" },
    { "error": "InternalServerError" },
    { "message_id": "1:1517" },
    { "message_id": "1:2342", "registration_id": "32" },
    { "error": "NotRegistered"}
  ]
}`, `{ "multicast_id": 217,
  "success": 2,
  "failure": 0,
  "canonical_ids": 0,
  "results": [
    { "message_id": "1:0409" },
    { "message_id": "1:1516" }
  ]
}`}
var expectedResp = `{"multicast_id": 217,
  "success": 5,
  "failure": 1,
  "canonical_ids": 1,
  "results": [
    { "message_id": "1:0408" },
    { "message_id": "1:0409" },
    { "message_id": "1:1516" },
    { "message_id": "1:1517" },
    { "message_id": "1:2342", "registration_id": "32" },
    { "error": "NotRegistered"}
  ]
 }`

type stubHttpClient struct {
	InvocationNum int
	Requests      []string
}

func (c *stubHttpClient) send(apiKey string, message HttpMessage) (*HttpResponse, error) {
	response := &HttpResponse{}
	err := json.Unmarshal([]byte(multicastReply[c.InvocationNum]), &response)
	c.InvocationNum++
	return response, err
}

func (c stubHttpClient) getRetryAfter() string {
	return ""
}

var singleTargetMessage = &HttpMessage{To: "recipient"}
var multipleTargetMessage = &HttpMessage{RegistrationIds: multicastTos}

// Test send for http client
func TestHttpClientSend(t *testing.T) {
	expectedRetryAfter := "10"
	var authHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(http.CanonicalHeaderKey("Content-Type"), "application/json")
		w.Header().Set(http.CanonicalHeaderKey("Retry-After"), expectedRetryAfter)
		w.WriteHeader(200)
		fmt.Fprintln(w, expectedResp)
	}))
	defer server.Close()

	transport := &http.Transport{
		Proxy: func(req *http.Request) (*url.URL, error) {
			authHeader = req.Header.Get(http.CanonicalHeaderKey("Authorization"))
			return url.Parse(server.URL)
		},
	}
	httpClient := &http.Client{Transport: transport}
	c := &httpGcmClient{server.URL, httpClient, "0"}
	response, error := c.send("apiKey", *singleTargetMessage)
	expectedAuthHeader := "key=apiKey"
	expResp := &HttpResponse{}
	err := json.Unmarshal([]byte(expectedResp), &expResp)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	assertEqual(t, authHeader, expectedAuthHeader)
	assertEqual(t, error, nil)
	assertDeepEqual(t, response, expResp)
	assertEqual(t, c.getRetryAfter(), expectedRetryAfter)
}

// test sending a GCM message through the HTTP connection server (includes backoff)
func TestSendHttp(t *testing.T) {
	c := &stubHttpClient{}
	b := &stubBackoff{}
	expResp := &HttpResponse{}
	err := json.Unmarshal([]byte(expectedResp), &expResp)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	response, err := sendHttp("apiKey", *multipleTargetMessage, c, b)
	assertDeepEqual(t, err, nil)
	assertDeepEqual(t, response, expResp)
}

func TestBuildRespForMulticast(t *testing.T) {
	expResp := &HttpResponse{}
	err := json.Unmarshal([]byte(multicastReply[0]), &expResp)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	resultsState := &multicastResultsState{
		"4":  &Result{MessageId: "1:0408"},
		"8":  &Result{Error: "Unavailable"},
		"15": &Result{Error: "InternalServerError"},
		"16": &Result{MessageId: "1:1517"},
		"23": &Result{MessageId: "1:2342", RegistrationId: "32"},
		"42": &Result{Error: "NotRegistered"},
	}
	resp := buildRespForMulticast(multicastTos, *resultsState, 216)
	assertDeepEqual(t, resp, expResp)
}

func TestMessageTargetAsStringArray(t *testing.T) {
	targets, err := messageTargetAsStringsArray(*singleTargetMessage)
	assertDeepEqual(t, targets, []string{"recipient"})
	assertEqual(t, err, nil)
	targets, err = messageTargetAsStringsArray(*multipleTargetMessage)
	assertDeepEqual(t, targets, multicastTos)
	assertEqual(t, err, nil)
	invalidMessage := &HttpMessage{}
	targets, err = messageTargetAsStringsArray(*invalidMessage)
	assertDeepEqual(t, targets, []string{})
	assertEqual(t, "can't find any valid target field in message.", err.Error())
}

func TestCheckResults(t *testing.T) {
	response := &HttpResponse{}
	err := json.Unmarshal([]byte(multicastReply[0]), &response)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	resultsState := &multicastResultsState{}
	doRetry, toRetry, err := checkResults(response.Results, multicastTos, *resultsState)
	expectedToRetry := []string{"8", "15"}
	assertEqual(t, doRetry, true)
	assertDeepEqual(t, toRetry, expectedToRetry)
	expectedResultState := &multicastResultsState{
		"4":  &Result{MessageId: "1:0408"},
		"8":  &Result{Error: "Unavailable"},
		"15": &Result{Error: "InternalServerError"},
		"16": &Result{MessageId: "1:1517"},
		"23": &Result{MessageId: "1:2342", RegistrationId: "32"},
		"42": &Result{Error: "NotRegistered"},
	}
	assertDeepEqual(t, resultsState, expectedResultState)
}

func TestXmppUser(t *testing.T) {
	assertEqual(t, xmppUser("b"), "b@gcm.googleapis.com")
}
