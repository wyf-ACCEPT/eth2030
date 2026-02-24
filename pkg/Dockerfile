ARG COMMIT=""
ARG VERSION=""

# Build stage
FROM golang:1.25-alpine AS builder

RUN apk add --no-cache gcc musl-dev linux-headers git

WORKDIR /build

# Cache dependencies
COPY go.mod go.sum ./
RUN go mod download

# Build both binaries
COPY . .
RUN go build -ldflags "-X main.version=${VERSION} -X main.commit=${COMMIT}" -o /build/bin/eth2030 ./cmd/eth2030
RUN go build -ldflags "-X main.version=${VERSION} -X main.commit=${COMMIT}" -o /build/bin/eth2030-geth ./cmd/eth2030-geth

# Runtime stage
FROM alpine:latest

RUN apk add --no-cache ca-certificates

COPY --from=builder /build/bin/eth2030 /usr/local/bin/
COPY --from=builder /build/bin/eth2030-geth /usr/local/bin/

# P2P, HTTP-RPC, Engine API
EXPOSE 30303 30303/udp 8545 8551

ENTRYPOINT ["eth2030-geth"]

ARG COMMIT=""
ARG VERSION=""
LABEL commit="$COMMIT" version="$VERSION"
