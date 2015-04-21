package gcm

import (
  "testing"
)

func assertEqual(t *testing.T, e, v interface{}) {
  if e != v {
    t.Fatalf("%#v != %#v", v, e)
  }
}

func TestXmppUser(t *testing.T) {
  assertEqual(t, xmppUser("b", "b@gcm.googleapis.com"))
}
