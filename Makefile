.PHONY: help makehelp dev server daemon cli goosar build test migrate-up migrate-down sqlc seed clean setup start stop check e2e worktree-env setup-main start-main stop-main check-main setup-worktree start-worktree stop-worktree check-worktree db-up db-down db-reset selfhost selfhost-build selfhost-packages selfhost-admin selfhost-stop offline-kit offline-build offline-manifest offline-verify fmt fmt-go fmt-ts

MAIN_ENV_FILE ?= .env
WORKTREE_ENV_FILE ?= .env.worktree
ENV_FILE ?= $(if $(wildcard $(MAIN_ENV_FILE)),$(MAIN_ENV_FILE),$(if $(wildcard $(WORKTREE_ENV_FILE)),$(WORKTREE_ENV_FILE),$(MAIN_ENV_FILE)))

ifneq ($(wildcard $(ENV_FILE)),)
include $(ENV_FILE)
endif

POSTGRES_DB ?= goosar
POSTGRES_USER ?= goosar
POSTGRES_PASSWORD ?= goosar
POSTGRES_PORT ?= 5433
PORT := $(or $(BACKEND_PORT),$(API_PORT),$(SERVER_PORT),$(PORT),8081)
FRONTEND_PORT ?= 3001
FRONTEND_ORIGIN ?= http://localhost:$(FRONTEND_PORT)
GOOSAR_APP_URL ?= $(FRONTEND_ORIGIN)
DATABASE_URL ?= postgres://$(POSTGRES_USER):$(POSTGRES_PASSWORD)@localhost:$(POSTGRES_PORT)/$(POSTGRES_DB)?sslmode=disable
NEXT_PUBLIC_API_URL ?= http://localhost:$(PORT)
NEXT_PUBLIC_WS_URL ?= ws://localhost:$(PORT)/ws
GOOSAR_SERVER_URL ?= ws://localhost:$(PORT)/ws
LOCAL_UPLOAD_BASE_URL ?= http://localhost:$(PORT)

export

GOOSAR_ARGS ?= $(ARGS)

COMPOSE := docker compose

define REQUIRE_ENV
	@if [ ! -f "$(ENV_FILE)" ]; then \
		echo "Missing env file: $(ENV_FILE)"; \
		echo "Create .env from .env.example, or run 'make worktree-env' and use .env.worktree."; \
		exit 1; \
	fi
endef

# Self-hosting requires the Docker Compose CLI plugin (`docker compose`).
# The self-host compose files use compose-spec syntax (top-level `name:`, no
# `version:`) that the legacy v1 `docker-compose` standalone cannot parse, so we
# fail early with an actionable message instead of a cryptic CLI parse error
# (e.g. "unknown shorthand flag: 'f' in -f") when the plugin is missing or v1.
# Keep the message short and OS-agnostic: per-OS install steps belong in docs.
define REQUIRE_COMPOSE
	@if ! compose_version=$$($(COMPOSE) version --short 2>/dev/null); then \
		echo "Docker Compose ('docker compose') was not found."; \
		echo "Self-hosting requires the Compose CLI plugin; legacy 'docker-compose' v1 is not supported."; \
		echo "Install Docker Compose from https://docs.docker.com/compose/install/ and verify with: docker compose version"; \
		exit 1; \
	fi; \
	case "$$compose_version" in \
		1.*|v1.*) \
			echo "'$(COMPOSE)' is legacy Docker Compose v1 ($$compose_version)."; \
			echo "Self-hosting requires the Compose CLI plugin; legacy 'docker-compose' v1 is not supported."; \
			echo "Install Docker Compose from https://docs.docker.com/compose/install/ and verify with: docker compose version"; \
			exit 1; \
			;; \
	esac
endef

# Default target changed from selfhost to help: bare `make` now prints this help
# instead of launching a full Docker Compose build, which is safer for onboarding.
.DEFAULT_GOAL := help

##@ Help

help: ## Show available make targets and common local workflows
	@awk 'BEGIN {FS = ":.*## "; printf "\nUsage:\n  make \033[36m<target>\033[0m\n\nQuick start:\n  \033[36mmake dev\033[0m          Bootstrap the current checkout and start everything\n  \033[36mmake check\033[0m        Run the full local verification pipeline\n\nCheckout modes:\n  Main checkout uses \033[36m.env\033[0m\n  Worktrees use \033[36m.env.worktree\033[0m (generate with \033[36mmake worktree-env\033[0m)\n\n"} \
		/^##@/ {printf "\n\033[1m%s\033[0m\n", substr($$0, 5); next} \
		/^[a-zA-Z0-9_.-]+:.*## / {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

makehelp: help ## Alias for `make help`

# ---------- Self-hosting (Docker Compose) ----------
##@ Self-hosting

selfhost: ## Create .env if needed, then pull and start the official self-hosted images
	$(REQUIRE_COMPOSE)
	@$(if $(PROFILE),GOOSAR_DELIVERY_PROFILE=$(PROFILE) )bash scripts/selfhost-env.sh
	@echo "==> Pulling official Goosar images..."
	@if ! $(COMPOSE) -f docker-compose.selfhost.yml pull; then \
		echo ""; \
		echo "Official images for tag '$${GOOSAR_IMAGE_TAG:-latest}' are not published yet."; \
		echo "If this is before the first GHCR release, build from the current checkout:"; \
		echo "  make selfhost-build"; \
		exit 1; \
	fi
	@echo "==> Starting Goosar via Docker Compose..."
	$(COMPOSE) -f docker-compose.selfhost.yml up -d
	@echo "==> Waiting for backend to be ready..."
	@for i in $$(seq 1 30); do \
		if curl -sf http://localhost:$(PORT)/health > /dev/null 2>&1; then \
			break; \
		fi; \
		sleep 2; \
	done
	@if curl -sf http://localhost:$(PORT)/health > /dev/null 2>&1; then \
		echo ""; \
		echo "✓ Goosar is running!"; \
		echo "  Frontend: http://localhost:$(FRONTEND_PORT)"; \
		echo "  Backend:  http://localhost:$(PORT)"; \
		echo ""; \
		echo "Images: $${GOOSAR_BACKEND_IMAGE:-ghcr.io/adanman/goosar-backend}:$${GOOSAR_IMAGE_TAG:-latest}"; \
		echo "        $${GOOSAR_WEB_IMAGE:-ghcr.io/adanman/goosar-web}:$${GOOSAR_IMAGE_TAG:-latest}"; \
		echo ""; \
		echo "Log in: open the frontend, enter your email, then print the code with:"; \
		echo "  make selfhost-code"; \
		echo "(This log path needs APP_ENV=development in .env: the stack defaults to"; \
		echo " APP_ENV=production, where a deployment with no mail transport refuses"; \
		echo " sign-in. Configure RESEND_API_KEY or SMTP_HOST to email codes instead.)"; \
		echo ""; \
		echo "Next — install the CLI from this deployment and connect your machine:"; \
		echo "  curl -fsSL http://localhost:$(FRONTEND_PORT)/install.sh | bash -s -- \\"; \
		echo "    --app-url http://localhost:$(FRONTEND_PORT) \\"; \
		echo "    --server-url http://localhost:$(PORT)"; \
		echo "  goosar setup self-host"; \
	else \
		echo ""; \
		echo "Services are still starting. Check logs:"; \
		echo "  $(COMPOSE) -f docker-compose.selfhost.yml logs"; \
	fi

selfhost-build: ## Build backend/web from the current checkout and start the self-hosted stack
	$(REQUIRE_COMPOSE)
	@$(if $(PROFILE),GOOSAR_DELIVERY_PROFILE=$(PROFILE) )bash scripts/selfhost-env.sh
	@echo "==> Building Goosar from the current checkout..."
	$(COMPOSE) -f docker-compose.selfhost.yml -f docker-compose.selfhost.build.yml up -d --build
	@echo "==> Waiting for backend to be ready..."
	@for i in $$(seq 1 30); do \
		if curl -sf http://localhost:$(PORT)/health > /dev/null 2>&1; then \
			break; \
		fi; \
		sleep 2; \
	done
	@if curl -sf http://localhost:$(PORT)/health > /dev/null 2>&1; then \
		echo ""; \
		echo "✓ Goosar is running!"; \
		echo "  Frontend: http://localhost:$(FRONTEND_PORT)"; \
		echo "  Backend:  http://localhost:$(PORT)"; \
		echo ""; \
		echo "Log in: open the frontend, enter your email, then print the code with:"; \
		echo "  make selfhost-code"; \
		echo "(This log path needs APP_ENV=development in .env: the stack defaults to"; \
		echo " APP_ENV=production, where a deployment with no mail transport refuses"; \
		echo " sign-in. Configure RESEND_API_KEY or SMTP_HOST to email codes instead.)"; \
		echo ""; \
		echo "Built images locally via docker-compose.selfhost.build.yml."; \
		echo "Local tags: goosar-backend:dev and goosar-web:dev."; \
		echo ""; \
		echo "Next — install the CLI from this deployment and connect your machine:"; \
		echo "  curl -fsSL http://localhost:$(FRONTEND_PORT)/install.sh | bash -s -- \\"; \
		echo "    --app-url http://localhost:$(FRONTEND_PORT) \\"; \
		echo "    --server-url http://localhost:$(PORT)"; \
		echo "  goosar setup self-host"; \
	else \
		echo ""; \
		echo "Services are still starting. Check logs:"; \
		echo "  $(COMPOSE) -f docker-compose.selfhost.yml logs"; \
	fi

selfhost-packages: ## Load a package catalog from an HTTP index into the self-host backend uploads volume (INDEX_URL=<url>, optional GOOSAR_PACKAGE_INDEX_TOKEN)
	$(REQUIRE_COMPOSE)
	@[ -n "$(INDEX_URL)" ] || { echo "usage: make selfhost-packages INDEX_URL=https://packages.example.test/index.json"; exit 1; }
	@echo "==> Loading the package catalog from $(INDEX_URL)..."
	@STAGE_DIR="$$(mktemp -d)"; trap 'rm -rf "$$STAGE_DIR"' EXIT; \
		bash offline/load-provisioning-packages.sh --index-url "$(INDEX_URL)" --store-dir "$$STAGE_DIR" && \
		echo "==> Copying the provisioning store into the backend uploads volume..." && \
		$(COMPOSE) -f docker-compose.selfhost.yml cp "$$STAGE_DIR/provisioning" backend:/app/data/uploads/
	@echo "==> Enabling the local provisioning store in .env..."
	@if grep -q '^GOOSAR_PROVISIONING_STORE=' .env 2>/dev/null; then \
		sed -i.selfhost-packages.bak 's/^GOOSAR_PROVISIONING_STORE=.*/GOOSAR_PROVISIONING_STORE=local/' .env && rm -f .env.selfhost-packages.bak; \
	else \
		printf 'GOOSAR_PROVISIONING_STORE=local\n' >> .env; \
	fi
	@echo "==> Restarting the backend to pick up the provisioning store..."
	$(COMPOSE) -f docker-compose.selfhost.yml up -d backend
	@echo ""
	@echo "✓ Package catalog loaded, GOOSAR_PROVISIONING_STORE=local applied, backend restarted."

selfhost-admin: ## Run goosar_admin inside the self-host backend container, e.g. make selfhost-admin ARGS="list-pending"
	$(REQUIRE_COMPOSE)
	$(COMPOSE) -f docker-compose.selfhost.yml exec backend ./goosar_admin $(ARGS)

selfhost-code: ## Print the latest [DEV] login verification code from the self-host backend logs
	$(REQUIRE_COMPOSE)
	@COMPOSE="$(COMPOSE)" bash scripts/selfhost-login-code.sh

selfhost-stop: ## Stop the self-hosted Docker Compose stack
	$(REQUIRE_COMPOSE)
	@echo "==> Stopping Goosar services..."
	$(COMPOSE) -f docker-compose.selfhost.yml down
	@echo "✓ All services stopped."

offline-kit: ## Assemble the offline (perimeter) build kit on a connected machine — see SELF_HOSTING.md, «Закрытый контур (offline-поставка)»
	bash offline/make-kit.sh

offline-build: ## Build the self-host images from the offline kit with no network (isolated machine)
	bash offline/build-offline.sh

offline-manifest: ## List every artifact for an air-gapped install with sha256 (DIR=<carry dir> [TAG=vX.Y.Z] [STRICT=1])
	@[ -n "$(DIR)" ] || { echo "usage: make offline-manifest DIR=<carry dir> [TAG=vX.Y.Z] [STRICT=1]"; exit 1; }
	bash offline/offline-manifest.sh generate --dir "$(DIR)" $(if $(TAG),--tag $(TAG)) $(if $(STRICT),--strict)

offline-verify: ## Verify a carried directory against its offline manifest (DIR=<carry dir> [MANIFEST=<file>])
	@[ -n "$(DIR)" ] || { echo "usage: make offline-verify DIR=<carry dir> [MANIFEST=<file>]"; exit 1; }
	bash offline/offline-manifest.sh verify --dir "$(DIR)" $(if $(MANIFEST),--manifest $(MANIFEST))

release-local: ## Build and publish a full release locally, without GitHub Actions (TAG=vX.Y.Z, extra flags via ARGS)
	@[ -n "$(TAG)" ] || { echo "usage: make release-local TAG=vX.Y.Z [ARGS='--skip-tests']"; exit 1; }
	bash scripts/release-local.sh "$(TAG)" $(ARGS)

# ---------- One-click commands ----------
##@ One-click

setup: ## Prepare the current checkout from its env file: install deps, ensure DB, run migrations
	$(REQUIRE_ENV)
	@echo "==> Using env file: $(ENV_FILE)"
	@echo "==> Installing dependencies..."
	pnpm install
	@bash scripts/ensure-postgres.sh "$(ENV_FILE)"
	@echo "==> Running migrations..."
	cd server && go run ./cmd/migrate up
	@echo ""
	@echo "✓ Setup complete! Run 'make start' to launch the app."

start: ## Start backend and frontend for the current checkout and run migrations first
	$(REQUIRE_ENV)
	@echo "Using env file: $(ENV_FILE)"
	@echo "Backend: http://localhost:$(PORT)"
	@echo "Frontend: http://localhost:$(FRONTEND_PORT)"
	@bash scripts/ensure-postgres.sh "$(ENV_FILE)"
	@echo "Running migrations..."
	cd server && go run ./cmd/migrate up
	@echo "Starting backend and frontend..."
	@trap 'kill 0' EXIT; \
		(cd server && go run ./cmd/server) & \
		pnpm dev:web & \
		wait

stop: ## Stop backend and frontend processes for the current checkout
	$(REQUIRE_ENV)
	@echo "Stopping services..."
	@-lsof -ti:$(PORT) | xargs kill -9 2>/dev/null
	@-lsof -ti:$(FRONTEND_PORT) | xargs kill -9 2>/dev/null
	@case "$(DATABASE_URL)" in \
		""|*@localhost:*|*@localhost/*|*@127.0.0.1:*|*@127.0.0.1/*|*@\[::1\]:*|*@\[::1\]/*) \
			echo "✓ App processes stopped. Shared PostgreSQL is still running on localhost:$(POSTGRES_PORT)." ;; \
		*) \
			echo "✓ App processes stopped. Remote PostgreSQL was not affected." ;; \
	esac

check: ## Run typecheck, TS tests, Go tests, and Playwright E2E for the current checkout
	$(REQUIRE_ENV)
	@ENV_FILE="$(ENV_FILE)" bash scripts/check.sh

fmt: fmt-go fmt-ts ## Format Go and TypeScript source with the house style

fmt-go: ## Format Go source with gofumpt (profile) and gci (import order: std, third-party, home module)
	cd server && go tool gofumpt -l -w .
	cd server && go tool gci write -s standard -s default -s localmodule --skip-generated .

fmt-ts: ## Format TypeScript/JavaScript source with Prettier
	pnpm format

e2e: ## Run only the Playwright E2E gate (starts/reuses backend+frontend, tears down what it started)
	$(REQUIRE_ENV)
	@ENV_FILE="$(ENV_FILE)" bash scripts/test-e2e.sh

db-up: ## Start the shared PostgreSQL container used by main and worktrees
	@$(COMPOSE) up -d postgres

db-down: ## Stop the shared PostgreSQL container without removing its Docker volume
	@$(COMPOSE) down

# Drop + recreate the current env's database, then run all migrations.
# Use for a clean slate in local dev. Only affects the DB named in
# ENV_FILE (POSTGRES_DB); the shared postgres container and other
# worktree DBs are untouched. Refuses to run against a remote host.
db-reset: ## Drop and recreate the current env's database, then re-run all migrations
	$(REQUIRE_ENV)
	@case "$(DATABASE_URL)" in \
		""|*@localhost:*|*@localhost/*|*@127.0.0.1:*|*@127.0.0.1/*|*@\[::1\]:*|*@\[::1\]/*) ;; \
		*) echo "Refusing to reset: DATABASE_URL points at a remote host."; exit 1 ;; \
	esac
	@bash scripts/ensure-postgres.sh "$(ENV_FILE)"
	@echo "==> Dropping and recreating database '$(POSTGRES_DB)'..."
	@$(COMPOSE) exec -T postgres psql -U $(POSTGRES_USER) -d postgres -v ON_ERROR_STOP=1 \
		-c "DROP DATABASE IF EXISTS \"$(POSTGRES_DB)\" WITH (FORCE);" \
		-c "CREATE DATABASE \"$(POSTGRES_DB)\";"
	@echo "==> Running migrations..."
	cd server && go run ./cmd/migrate up
	@echo ""
	@echo "✓ Database '$(POSTGRES_DB)' reset. Run 'make start' to launch the app."

worktree-env: ## Generate .env.worktree with a unique DB name and app ports for this worktree
	@bash scripts/init-worktree-env.sh .env.worktree

setup-main: ## Prepare the main checkout using .env
	@$(MAKE) setup ENV_FILE=$(MAIN_ENV_FILE)

start-main: ## Start the main checkout using .env
	@$(MAKE) start ENV_FILE=$(MAIN_ENV_FILE)

stop-main: ## Stop the main checkout processes defined by .env
	@$(MAKE) stop ENV_FILE=$(MAIN_ENV_FILE)

check-main: ## Run the full verification pipeline for the main checkout
	@ENV_FILE=$(MAIN_ENV_FILE) bash scripts/check.sh

setup-worktree: ## Ensure .env.worktree exists, then prepare this worktree
	@if [ ! -f "$(WORKTREE_ENV_FILE)" ]; then \
		echo "==> Generating $(WORKTREE_ENV_FILE) with unique ports..."; \
		bash scripts/init-worktree-env.sh $(WORKTREE_ENV_FILE); \
	else \
		echo "==> Using existing $(WORKTREE_ENV_FILE)"; \
	fi
	@$(MAKE) setup ENV_FILE=$(WORKTREE_ENV_FILE)

start-worktree: ## Start this worktree using .env.worktree
	@$(MAKE) start ENV_FILE=$(WORKTREE_ENV_FILE)

stop-worktree: ## Stop this worktree's backend and frontend processes
	@$(MAKE) stop ENV_FILE=$(WORKTREE_ENV_FILE)

check-worktree: ## Run the full verification pipeline for this worktree
	@ENV_FILE=$(WORKTREE_ENV_FILE) bash scripts/check.sh

# ---------- Individual commands ----------
##@ Individual commands

dev: ## Bootstrap this checkout end-to-end: create env if needed, ensure DB, migrate, start services
	@bash scripts/dev.sh

server: ## Run only the Go server for the current checkout
	$(REQUIRE_ENV)
	@bash scripts/ensure-postgres.sh "$(ENV_FILE)"
	cd server && go run ./cmd/server

daemon: ## Restart the local agent daemon using the CLI's stored auth/session
	@$(MAKE) goosar GOOSAR_ARGS="daemon restart --profile local"

cli: ## Run the goosar CLI with ARGS or GOOSAR_ARGS from source
	@$(MAKE) goosar GOOSAR_ARGS="$(GOOSAR_ARGS)"

goosar: ## Run the goosar CLI entrypoint directly from the Go source tree
	cd server && go run -ldflags "-X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)" ./cmd/goosar $(GOOSAR_ARGS)

VERSION ?= $(shell git describe --tags --match 'v[0-9]*' --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
DATE    ?= $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')

build: ## Build the server, CLI, and migrate binaries into server/bin
	cd server && go build -ldflags "-X main.version=$(VERSION) -X main.commit=$(COMMIT)" -o bin/server ./cmd/server
	cd server && go build -ldflags "-X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)" -o bin/goosar ./cmd/goosar
	cd server && go build -o bin/migrate ./cmd/migrate

test: ## Run Go tests after ensuring the target DB exists and migrations are applied
	$(REQUIRE_ENV)
	@bash scripts/ensure-postgres.sh "$(ENV_FILE)"
	cd server && go run ./cmd/migrate up
	bash scripts/test-go.sh --race

# Database
##@ Database

migrate-up: ## Create the target DB if needed, then apply database migrations
	$(REQUIRE_ENV)
	@bash scripts/ensure-postgres.sh "$(ENV_FILE)"
	cd server && go run ./cmd/migrate up

migrate-down: ## Create the target DB if needed, then roll back database migrations
	$(REQUIRE_ENV)
	@bash scripts/ensure-postgres.sh "$(ENV_FILE)"
	cd server && go run ./cmd/migrate down

sqlc: ## Regenerate sqlc code
	cd server && sqlc generate
	cd server && go tool gofumpt -l -w ./pkg/db/generated

# Cleanup
##@ Cleanup

clean: ## Remove build caches, generated binaries, and temp files
	rm -rf server/bin server/tmp
	rm -rf apps/*/.next apps/*/.source apps/*/.expo
	rm -rf apps/*/out apps/*/dist apps/*/dist-electron packages/*/dist
	rm -rf .turbo apps/*/.turbo packages/*/.turbo
	rm -rf apps/*/*.tsbuildinfo packages/*/*.tsbuildinfo
	@echo "✓ Clean complete."
