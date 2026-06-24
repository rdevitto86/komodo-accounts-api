.DEFAULT_GOAL := bootstrap

APP_NAME := komodo-user-api
ENV ?= local
ENV := $(strip $(ENV))
TAG ?= $(ENV)
DOCKERFILE := Dockerfile
COMPOSE_FILE := docker-compose.yaml
AWS_REGION ?= us-east-1
VERSION ?= latest
AWS_SECRET_PREFIX := ${APP_NAME}/$(ENV)
AWS_SECRET_BATCH := /all-secrets
MEM_LIMIT ?= 512M
LOG_LEVEL ?= error
RESTART_POLICY ?= unless-stopped

ifeq ($(ENV),prod)
  AWS_ENDPOINT :=
  DISTROLESS_TAG := latest
  RESTART_POLICY := always
  MEM_LIMIT := 1g
else ifeq ($(ENV),staging)
  AWS_ENDPOINT :=
  DISTROLESS_TAG := latest
  MEM_LIMIT := 1g
else ifeq ($(ENV),dev)
  AWS_ENDPOINT := http://host.docker.internal:4566
  DISTROLESS_TAG := debug
  LOG_LEVEL := info
else
  AWS_ENDPOINT := http://host.docker.internal:4566
  DISTROLESS_TAG := debug
  RESTART_POLICY := no
  LOG_LEVEL := debug
endif

.PHONY: build run bootstrap stop restart clean test test_unit test_e2e lint

help:
	@echo "Targets:"
	@echo "  build             Build Docker image for ENV ($(ENV))"
	@echo "  run               Start container via Docker Compose"
	@echo "  bootstrap         Build + run in one command"
	@echo "  stop              Stop and remove container stack"
	@echo "  restart           Restart container stack"
	@echo "  clean             Prune Docker and remove local build artifacts"
	@echo "  test              Run all tests"
	@echo "  test_unit         Run unit tests"
	@echo "  test_e2e          Run end-to-end tests"
	@echo "  lint              Run golangci-lint"
	@echo ""
	@echo "Supported Environments:"
	@echo "  local    - Local development (LocalStack)"
	@echo "  dev      - Development (LocalStack)"
	@echo "  staging  - Staging (real AWS)"
	@echo "  prod     - Production (real AWS)"
	@echo ""
	@echo "Examples:"
	@echo "  make bootstrap               # Default: local"
	@echo "  make bootstrap ENV=dev       # Dev environment"
	@echo "  make bootstrap ENV=staging   # Staging environment"
	@echo "  make bootstrap ENV=prod      # Production environment"
	@echo "  make stop ENV=dev"
	@echo "  make restart ENV=staging"

build:
	@echo "Building $(APP_NAME) public + internal images for ENV=$(ENV)"
	@ENV=$(ENV) \
	APP_NAME=$(APP_NAME) \
	VERSION=$(TAG) \
	DISTROLESS_TAG=$(DISTROLESS_TAG) \
	docker compose -f $(COMPOSE_FILE) build

run:
	@echo "Starting $(APP_NAME) for ENV=$(ENV)"
	@docker network create komodo-network 2>/dev/null || true
	@ENV=$(ENV) \
	APP_NAME=$(APP_NAME) \
	VERSION=$(TAG) \
	LOG_LEVEL=$(LOG_LEVEL) \
	AWS_REGION=$(AWS_REGION) \
	AWS_ENDPOINT=$(AWS_ENDPOINT) \
	AWS_SECRET_PREFIX=$(AWS_SECRET_PREFIX) \
	AWS_SECRET_BATCH=$(AWS_SECRET_BATCH) \
	RESTART_POLICY=$(RESTART_POLICY) \
	MEM_LIMIT=$(MEM_LIMIT) \
	docker compose -p $(APP_NAME)-$(ENV) -f $(COMPOSE_FILE) up -d

stop:
	@echo "Stopping $(APP_NAME) for ENV=$(ENV)"
	@docker compose -p $(APP_NAME)-$(ENV) -f $(COMPOSE_FILE) down --remove-orphans

bootstrap: build run

restart: stop run

clean:
	docker container prune -f
	docker image prune -f
	docker network prune -f
	docker volume prune -f
	rm -rf bin

test:
	@go test ./... -race

test_unit:
	@go test -short ./...

test_e2e:
	@go test -tags=e2e ./...

lint:
	@golangci-lint run ./...
