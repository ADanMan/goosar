package main

import (
	"testing"

	"github.com/adanman/goosar/server/internal/analytics"
	"github.com/adanman/goosar/server/internal/deploymentprofile"
	"github.com/adanman/goosar/server/internal/events"
	"github.com/adanman/goosar/server/internal/realtime"
)

func TestDeploymentProfileReachesTheHandlerConfig(t *testing.T) {
	cases := []struct {
		env  string
		want deploymentprofile.Profile
	}{
		{"", deploymentprofile.Perimeter},
		{"demo", deploymentprofile.Demo},
		{"  Dev  ", deploymentprofile.Dev},
		{"local", deploymentprofile.Local},
	}
	for _, tc := range cases {
		t.Run(tc.env, func(t *testing.T) {
			t.Setenv(deploymentprofile.EnvVar, tc.env)
			_, h := NewRouterWithOptions(nil, realtime.NewHub(), events.New(), analytics.NoopClient{}, nil, RouterOptions{})
			if got := h.DeploymentProfile(); got != tc.want {
				t.Fatalf("%s=%q reached the handler as %q, want %q", deploymentprofile.EnvVar, tc.env, got, tc.want)
			}
		})
	}
}
