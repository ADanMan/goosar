package middleware

import (
	"context"
	"net/http"
)

type clientMetadataKey int

const (
	ctxKeyClientPlatform clientMetadataKey = iota
	ctxKeyClientVersion
	ctxKeyClientOS
)

const (
	HeaderClientPlatform = "X-Client-Platform"
	HeaderClientVersion  = "X-Client-Version"
	HeaderClientOS       = "X-Client-OS"
)

func ClientMetadata(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if v := r.Header.Get(HeaderClientPlatform); v != "" {
			ctx = context.WithValue(ctx, ctxKeyClientPlatform, v)
		}
		if v := r.Header.Get(HeaderClientVersion); v != "" {
			ctx = context.WithValue(ctx, ctxKeyClientVersion, v)
		}
		if v := r.Header.Get(HeaderClientOS); v != "" {
			ctx = context.WithValue(ctx, ctxKeyClientOS, v)
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func ClientMetadataFromContext(ctx context.Context) (platform, version, os string) {
	platform, _ = ctx.Value(ctxKeyClientPlatform).(string)
	version, _ = ctx.Value(ctxKeyClientVersion).(string)
	os, _ = ctx.Value(ctxKeyClientOS).(string)
	return platform, version, os
}

func SetClientMetadata(ctx context.Context, platform, version, os string) context.Context {
	if platform != "" {
		ctx = context.WithValue(ctx, ctxKeyClientPlatform, platform)
	}
	if version != "" {
		ctx = context.WithValue(ctx, ctxKeyClientVersion, version)
	}
	if os != "" {
		ctx = context.WithValue(ctx, ctxKeyClientOS, os)
	}
	return ctx
}
