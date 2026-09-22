package main

import (
	"strings"
	"testing"
)

func TestCheckReplicaTopology(t *testing.T) {
	cases := []struct {
		name     string
		replicas string
		redisURL string
		wantErr  string
	}{
		{name: "unset is single node", replicas: "", redisURL: ""},
		{name: "one replica needs no redis", replicas: "1", redisURL: ""},
		{name: "many replicas with redis", replicas: "3", redisURL: "redis://redis:6379/0"},
		{name: "many replicas without redis", replicas: "3", wantErr: "REDIS_URL"},
		{name: "many replicas with unusable redis url", replicas: "2", redisURL: "not-a-url", wantErr: "REDIS_URL"},
		{name: "garbage replica count is ignored", replicas: "many", redisURL: ""},
		{name: "whitespace is trimmed", replicas: " 2 ", wantErr: "REDIS_URL"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := checkReplicaTopology(tc.replicas, tc.redisURL)
			switch {
			case tc.wantErr == "" && err != nil:
				t.Fatalf("expected no error, got %v", err)
			case tc.wantErr != "" && err == nil:
				t.Fatalf("expected an error mentioning %q", tc.wantErr)
			case tc.wantErr != "" && !strings.Contains(err.Error(), tc.wantErr):
				t.Fatalf("expected an error mentioning %q, got %v", tc.wantErr, err)
			}
		})
	}
}
