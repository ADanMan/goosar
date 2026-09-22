package daemon

import (
	"net/http"
	"reflect"
	"testing"
)

func TestTaskWakeupDialerUsesEnvProxy(t *testing.T) {
	dialer := taskWakeupDialer()

	if dialer.Proxy == nil {
		t.Fatal("task wakeup dialer has nil Proxy: env HTTPS_PROXY would be ignored and the wakeup WS could never connect through a corporate proxy")
	}

	got := reflect.ValueOf(dialer.Proxy).Pointer()
	want := reflect.ValueOf(http.ProxyFromEnvironment).Pointer()
	if got != want {
		t.Fatal("task wakeup dialer Proxy is not http.ProxyFromEnvironment")
	}

	if dialer.HandshakeTimeout <= 0 {
		t.Fatal("task wakeup dialer must keep a positive handshake timeout")
	}
}
