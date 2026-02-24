.PHONY: all build eth2030 eth2030-geth docker devnet devnet-down clean

BIN_DIR := ./bin

all: build

build: eth2030 eth2030-geth

eth2030:
	@mkdir -p $(BIN_DIR)
	go build -o $(BIN_DIR)/eth2030 ./cmd/eth2030

eth2030-geth:
	@mkdir -p $(BIN_DIR)
	go build -o $(BIN_DIR)/eth2030-geth ./cmd/eth2030-geth

DOCKER_IMAGE := eth2030
DOCKER_TAG := latest
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")

docker:
	docker build --build-arg COMMIT=$(COMMIT) --build-arg VERSION=$(VERSION) -t $(DOCKER_IMAGE):$(DOCKER_TAG) .

devnet:
	docker compose -f devnet/docker-compose.yml up --build -d

devnet-down:
	docker compose -f devnet/docker-compose.yml down

clean:
	rm -rf $(BIN_DIR)
