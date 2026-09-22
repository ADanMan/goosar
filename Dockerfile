# --- Build stage ---
# The image ARGs and GOOSAR_OFFLINE_BUILD exist for the offline (perimeter)
# build kit — see SELF_HOSTING.md, «Закрытый контур (offline-поставка)». Their defaults keep the online
# build identical to the pre-kit behavior: same base images, same commands.
#
# Pinned by digest (supply-chain: a tag is mutable, a digest is not — see #137).
# The tag stays in the ref for readability; Docker resolves the digest. Both
# digests below are the multi-arch INDEX digest (covers linux/amd64 +
# linux/arm64), not a per-platform manifest digest, so both arches still
# build. To update: `docker buildx imagetools inspect <image>:<tag>` and copy
# the top-level "Digest:" line. offline/offline-kit.test.sh asserts every
# default here stays digest-pinned.
ARG GO_IMAGE=golang:1.26-alpine@sha256:28d89ee9cc0ff9fec75c82ca201e6bf7fdf9a679d4b7b24dfa04f2bb766bb468
ARG RUNTIME_IMAGE=alpine:3.21@sha256:48b0309ca019d89d40f670aa1bc06e426dc0931948452e8491e3d65087abc07d

FROM ${GO_IMAGE} AS builder

# GOOSAR_OFFLINE_BUILD=1 skips every network-touching step. The offline kit
# supplies server/vendor/ (go builds with -mod=vendor automatically when the
# vendor directory exists), which also removes the need for git.
ARG GOOSAR_OFFLINE_BUILD=0

RUN if [ "$GOOSAR_OFFLINE_BUILD" != "1" ]; then apk add --no-cache git; fi

WORKDIR /src

# Cache dependencies
COPY server/go.mod server/go.sum ./server/
RUN if [ "$GOOSAR_OFFLINE_BUILD" != "1" ]; then cd server && go mod download; fi

# Copy server source (includes server/vendor/ when the offline kit created it)
COPY server/ ./server/

# Build binaries
ARG VERSION=dev
ARG COMMIT=unknown
ARG DATE=unknown
RUN cd server && CGO_ENABLED=0 go build -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" -o bin/server ./cmd/server
RUN cd server && CGO_ENABLED=0 go build -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.date=${DATE}" -o bin/goosar ./cmd/goosar
RUN cd server && CGO_ENABLED=0 go build -ldflags "-s -w" -o bin/migrate ./cmd/migrate
# goosar_admin is the server-side second channel for deployment_admin
# composition changes (#426): the confirm_hint the API returns points at
# this binary inside the running backend container, so it must ship in the
# image — an operator on compose/Helm has neither Go nor the sources.
RUN cd server && CGO_ENABLED=0 go build -ldflags "-s -w" -o bin/goosar_admin ./cmd/goosar_admin
RUN cd server && CGO_ENABLED=0 go build -ldflags "-s -w" -o bin/backfill_task_usage_hourly ./cmd/backfill_task_usage_hourly
RUN cd server && CGO_ENABLED=0 go build -ldflags "-s -w" -o bin/backfill_codex_usage_cache ./cmd/backfill_codex_usage_cache

# --- Runtime stage ---
FROM ${RUNTIME_IMAGE}

# The offline kit passes a RUNTIME_IMAGE that already contains
# ca-certificates and tzdata, so the apk step can be skipped there.
ARG GOOSAR_OFFLINE_BUILD=0
RUN if [ "$GOOSAR_OFFLINE_BUILD" != "1" ]; then apk add --no-cache ca-certificates tzdata; fi

WORKDIR /app

COPY --from=builder /src/server/bin/server .
COPY --from=builder /src/server/bin/goosar .
COPY --from=builder /src/server/bin/migrate .
COPY --from=builder /src/server/bin/goosar_admin .
COPY --from=builder /src/server/bin/backfill_task_usage_hourly .
COPY --from=builder /src/server/bin/backfill_codex_usage_cache .
COPY server/migrations/ ./migrations/
COPY docker/entrypoint.sh .
RUN sed -i 's/\r$//' entrypoint.sh && chmod +x entrypoint.sh

# Non-root runtime (#384). Same shape as Dockerfile.web's nextjs user.
# The only path the server binary writes at runtime is the local upload
# directory (LOCAL_UPLOAD_DIR, default ./data/uploads — see
# server/internal/storage/local.go); everything else it touches is Postgres
# or, when S3 is configured, the object store. Multipart uploads larger than
# the in-memory limit spool through os.TempDir(), so /tmp must stay writable
# even under a read-only root filesystem.
#
# The binaries and migrations stay root-owned and world-readable on purpose:
# the runtime user can execute them but cannot rewrite them.
RUN addgroup --system --gid 1001 goosar && \
    adduser --system --uid 1001 --ingroup goosar goosar && \
    mkdir -p /app/data/uploads && \
    chown -R goosar:goosar /app/data

USER goosar

EXPOSE 8080

ENTRYPOINT ["./entrypoint.sh"]
