package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/redis/go-redis/v9"
)

const replicasEnvVar = "GOOSAR_REPLICAS"

func checkReplicaTopology(replicas, redisURL string) error {
	n, err := strconv.Atoi(strings.TrimSpace(replicas))
	if err != nil || n <= 1 {
		return nil
	}
	if strings.TrimSpace(redisURL) == "" {
		return fmt.Errorf("%s=%d requires REDIS_URL: without shared Redis every replica keeps its own rate-limit budget and realtime hub", replicasEnvVar, n)
	}
	if _, err := redis.ParseURL(redisURL); err != nil {
		return fmt.Errorf("%s=%d requires a usable REDIS_URL: %w", replicasEnvVar, n, err)
	}
	return nil
}
