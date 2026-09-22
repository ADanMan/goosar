package plugin

import (
	"context"
	"fmt"
	"sync"
	"testing"
)

type fakePlugin struct{ name string }

func (f fakePlugin) Name() string                      { return f.name }
func (f fakePlugin) Start(context.Context, Host) error { return nil }
func (f fakePlugin) Stop(context.Context) error        { return nil }

func resetRegistry(t *testing.T) {
	t.Helper()
	mu.Lock()
	saved := registry
	registry = nil
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		registry = saved
		mu.Unlock()
	})
}

func TestRegisterKeepsOrderAndSkipsNil(t *testing.T) {
	resetRegistry(t)
	if got := Registered(); len(got) != 0 {
		t.Fatalf("empty registry returned %d plugins", len(got))
	}

	Register(fakePlugin{name: "a"})
	Register(nil)
	Register(fakePlugin{name: "b"})

	got := Registered()
	if len(got) != 2 || got[0].Name() != "a" || got[1].Name() != "b" {
		t.Fatalf("Registered() = %v, want [a b] in registration order", got)
	}
}

func TestRegisteredReturnsCopy(t *testing.T) {
	resetRegistry(t)
	Register(fakePlugin{name: "a"})

	snapshot := Registered()
	snapshot[0] = fakePlugin{name: "mutated"}

	if got := Registered()[0].Name(); got != "a" {
		t.Fatalf("mutating a snapshot changed the registry: got %q", got)
	}
}

func TestRegisterConcurrent(t *testing.T) {
	resetRegistry(t)
	const n = 64
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			Register(fakePlugin{name: fmt.Sprintf("p%d", i)})
			_ = Registered()
		}(i)
	}
	wg.Wait()
	if got := len(Registered()); got != n {
		t.Fatalf("registered %d plugins, want %d", got, n)
	}
}
