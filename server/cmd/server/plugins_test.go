package main

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/adanman/goosar/server/internal/integrations/plugin"
)

type recordingPlugin struct {
	name     string
	startErr error
	log      *[]string
}

func (r *recordingPlugin) Name() string { return r.name }

func (r *recordingPlugin) Start(context.Context, plugin.Host) error {
	*r.log = append(*r.log, "start:"+r.name)
	return r.startErr
}

func (r *recordingPlugin) Stop(context.Context) error {
	*r.log = append(*r.log, "stop:"+r.name)
	return nil
}

func TestStartAndStopPlugins(t *testing.T) {
	var log []string
	plugins := []plugin.Plugin{
		&recordingPlugin{name: "a", log: &log},
		&recordingPlugin{name: "broken", startErr: errors.New("boom"), log: &log},
		&recordingPlugin{name: "b", log: &log},
	}

	started := startPlugins(context.Background(), plugins, plugin.Host{})
	if len(started) != 2 {
		t.Fatalf("started %d plugins, want 2 (the failing one is skipped)", len(started))
	}
	stopPlugins(context.Background(), started)

	want := []string{"start:a", "start:broken", "start:b", "stop:b", "stop:a"}
	if !reflect.DeepEqual(log, want) {
		t.Fatalf("call order = %v, want %v", log, want)
	}
}

func TestStartPluginsNoneRegistered(t *testing.T) {
	if got := startPlugins(context.Background(), nil, plugin.Host{}); len(got) != 0 {
		t.Fatalf("started %d plugins from an empty list", len(got))
	}
}
