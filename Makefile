.PHONY: build run test lint docker-up docker-down clean

APP_NAME    := zhitu-agent
BUILD_DIR   := ./bin
DOCKER_COMPOSE := docker compose

build:
	go build -ldflags="-s -w" -o $(BUILD_DIR)/$(APP_NAME) ./cmd/server

run: build
	$(BUILD_DIR)/$(APP_NAME)

test:
	go test -v -race ./...

test-cover:
	go test -v -race -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

lint:
	go vet ./...

docker-up:
	$(DOCKER_COMPOSE) up -d --build

docker-down:
	$(DOCKER_COMPOSE) down

docker-logs:
	$(DOCKER_COMPOSE) logs -f zhitu-agent

clean:
	rm -rf $(BUILD_DIR) coverage.out coverage.html
