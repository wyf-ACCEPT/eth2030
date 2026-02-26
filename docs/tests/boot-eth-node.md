# Booting an Ethereum Node with eth2030-geth

This guide explains how to run a full Ethereum node using `eth2030-geth` as the
Execution Layer (EL) client, paired with an external Consensus Layer (CL)
client.

## Architecture

Post-Merge Ethereum requires **two clients** running together:

```
┌─────────────────┐       Engine API (JWT)       ┌─────────────────┐
│  Consensus Layer │ ◄──────────────────────────► │  Execution Layer │
│  (CL Client)     │       localhost:8551         │  (eth2030-geth)  │
│  e.g. Prysm      │                              │                  │
└────────┬────────┘                              └────────┬────────┘
         │ Beacon P2P :9000                               │ EL P2P :30303
         ▼                                                ▼
    CL Peer Network                                 EL Peer Network
```

- **eth2030-geth** handles block execution, state, EVM, and transaction pool.
- **CL client** handles consensus (PoS), validator duties, and beacon chain.
- They communicate via the **Engine API** on port `8551`, authenticated with a
  shared JWT secret.

## Prerequisites

- Go 1.25+ (for building from source)
- Docker (optional, for containerized deployment)
- A CL client binary (Prysm, Lighthouse, Nimbus, Teku, or Lodestar)
- Sufficient disk space: ~50GB for Sepolia, ~1TB+ for mainnet

## Step 1: Build eth2030-geth

### From source

```bash
cd pkg
make eth2030-geth
# Binary at: ./bin/eth2030-geth
```

### With Docker

```bash
cd pkg
make docker
# Image: eth2030:latest
```

## Step 2: Generate JWT Secret

The JWT secret is a shared 32-byte hex token used to authenticate Engine API
requests between the EL and CL clients.

```bash
openssl rand -hex 32 > /tmp/jwtsecret
```

> **Note:** If `--authrpc.jwtsecret` is not specified, eth2030-geth
> auto-generates one at `<datadir>/jwtsecret`. However, you must provide the
> same file to your CL client, so generating it explicitly is recommended.

## Step 3: Start eth2030-geth (Execution Layer)

### Sepolia testnet (recommended for testing)

```bash
./bin/eth2030-geth \
  --network sepolia \
  --datadir ~/.eth2030-sepolia \
  --port 30303 \
  --http.port 8545 \
  --authrpc.port 8551 \
  --authrpc.jwtsecret /tmp/jwtsecret \
  --syncmode snap \
  --maxpeers 50 \
  --verbosity 3
```

### Mainnet

```bash
./bin/eth2030-geth \
  --network mainnet \
  --datadir ~/.eth2030-geth \
  --authrpc.jwtsecret /tmp/jwtsecret
```

### Docker (single container)

```bash
docker run -d --name eth2030-el \
  -p 30303:30303/tcp \
  -p 30303:30303/udp \
  -p 8545:8545 \
  -p 8551:8551 \
  -v $HOME/.eth2030-sepolia:/data \
  -v /tmp/jwtsecret:/jwt/secret:ro \
  eth2030:latest \
  --network sepolia \
  --datadir /data \
  --authrpc.jwtsecret /jwt/secret
```

### Docker Compose (EL + CL devnet)

The easiest way to run a full node (EL + CL) is with the devnet docker-compose
setup, which boots both eth2030-geth and Lighthouse together:

```bash
cd pkg

# Start the devnet (builds eth2030-geth image, pulls Lighthouse, generates JWT)
make devnet

# View logs
docker compose -f devnet/docker-compose.yml logs -f

# Stop
make devnet-down
```

The compose setup handles:
- Automatic JWT secret generation and sharing between EL and CL
- Correct startup ordering (JWT init -> EL -> CL)
- Lighthouse checkpoint sync for fast beacon chain sync
- Network aliases (`eth1` / `eth2`) for internal service discovery

Configuration is in `pkg/devnet/.env`. Key variables:

| Variable | Default | Description |
|----------|---------|-------------|
| `NETWORK` | `sepolia` | Ethereum network |
| `JWT_SECRET` | (auto-generated) | Hex-encoded 32-byte JWT secret |
| `CL_CHECKPOINT_SYNC_URL` | `https://sepolia.beaconstate.info` | CL checkpoint sync endpoint |
| `EL_SYNCMODE` | `snap` | EL sync mode |
| `LIGHTHOUSE_TAG` | `latest` | Lighthouse Docker image tag |

See `pkg/devnet/docker-compose.yml` for the full setup.

### CLI Flags Reference

| Flag | Default | Description |
|------|---------|-------------|
| `--network` | `mainnet` | Network: `mainnet`, `sepolia`, `holesky` |
| `--datadir` | `~/.eth2030-geth` | Data directory for chain data |
| `--port` | `30303` | P2P listening port (TCP + UDP) |
| `--http.addr` | `127.0.0.1` | HTTP-RPC server listen address |
| `--http.port` | `8545` | HTTP-RPC server port |
| `--authrpc.addr` | `127.0.0.1` | Engine API listen address |
| `--authrpc.port` | `8551` | Engine API port (for CL client) |
| `--authrpc.jwtsecret` | `<datadir>/jwtsecret` | Path to JWT secret file |
| `--syncmode` | `snap` | Sync mode: `full` or `snap` |
| `--maxpeers` | `50` | Maximum P2P peers |
| `--verbosity` | `3` | Log level (0=silent, 5=trace) |
| `--override.glamsterdam` | - | Override Glamsterdam fork timestamp (testing) |
| `--override.hogota` | - | Override Hegotá fork timestamp (testing) |

## Step 4: Peer Discovery (Bootnodes)

### How it works

eth2030-geth uses **go-ethereum's peer discovery** mechanism:

1. **Bootstrap nodes** — hardcoded enode URLs per network, used to discover
   initial peers via the Kademlia DHT (DiscV5 protocol).
2. **DiscV5** — after connecting to bootnodes, the node performs iterative
   lookups to find more peers using XOR-distance routing.
3. **Static peers** — can be added manually via `admin_addPeer` RPC.

### Built-in bootnodes

Bootnodes are loaded automatically based on the `--network` flag
(`pkg/cmd/eth2030-geth/config.go`):

- **Mainnet**: `params.MainnetBootnodes` from go-ethereum
- **Sepolia**: `params.SepoliaBootnodes`
- **Holesky**: `params.HoleskyBootnodes`

Both `BootstrapNodes` (DiscV4) and `BootstrapNodesV5` (DiscV5) are configured
with the same set.

### Adding peers manually

Use the `admin` RPC namespace (enabled by default):

```bash
# Add a static peer
curl -X POST http://localhost:8545 \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "method": "admin_addPeer",
    "params": ["enode://<pubkey>@<ip>:<port>"],
    "id": 1
  }'

# List connected peers
curl -X POST http://localhost:8545 \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"admin_peers","params":[],"id":1}'

# Get local node info (your enode URL)
curl -X POST http://localhost:8545 \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"admin_nodeInfo","params":[],"id":1}'
```

### Custom bootnodes (not yet supported)

Currently there is **no `--bootnodes` CLI flag** to override the default
bootnode list. Bootnodes are determined entirely by the `--network` flag.

**Missing config:** A `--bootnodes` flag would allow users to specify custom
enode URLs for private networks or devnets. As a workaround, use
`admin_addPeer` after startup.

## Step 5: Start a Consensus Layer Client

eth2030-geth does **not** include a built-in CL client. You must run a separate
CL client that connects to the Engine API.

### Option A: Prysm

```bash
# Install Prysm
mkdir -p ~/prysm && cd ~/prysm
curl https://raw.githubusercontent.com/prysmaticlabs/prysm/master/prysm.sh \
  --output prysm.sh && chmod +x prysm.sh

# Run beacon node (Sepolia)
./prysm.sh beacon-chain \
  --sepolia \
  --datadir ~/.prysm-sepolia \
  --execution-endpoint http://localhost:8551 \
  --jwt-secret /tmp/jwtsecret \
  --checkpoint-sync-url https://sepolia.beaconstate.info \
  --genesis-beacon-api-url https://sepolia.beaconstate.info
```

### Option B: Lighthouse

```bash
# Install Lighthouse
# See: https://lighthouse-book.sigmaprime.io/installation.html

# Run beacon node (Sepolia)
lighthouse bn \
  --network sepolia \
  --datadir ~/.lighthouse-sepolia \
  --execution-endpoint http://localhost:8551 \
  --execution-jwt /tmp/jwtsecret \
  --checkpoint-sync-url https://sepolia.beaconstate.info
```

### Option C: Nimbus

```bash
# Run beacon node (Sepolia)
nimbus_beacon_node \
  --network=sepolia \
  --data-dir=~/.nimbus-sepolia \
  --web3-url=http://localhost:8551 \
  --jwt-secret=/tmp/jwtsecret
```

### CL Client Comparison

| Client | Language | Memory | Notes |
|--------|----------|--------|-------|
| Prysm | Go | ~4-8 GB | Most popular, checkpoint sync |
| Lighthouse | Rust | ~2-4 GB | Memory efficient, reliable |
| Nimbus | Nim | ~1-2 GB | Lowest resource usage |
| Teku | Java | ~8-16 GB | Enterprise-grade, JVM-based |
| Lodestar | TypeScript | ~4-8 GB | JavaScript ecosystem |

## Step 6: Verify the Node is Running

### Check EL sync status

```bash
# eth_syncing returns false when fully synced
curl -s http://localhost:8545 \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"eth_syncing","params":[],"id":1}'
```

### Check peer count

```bash
curl -s http://localhost:8545 \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"net_peerCount","params":[],"id":1}'
```

### Check latest block

```bash
curl -s http://localhost:8545 \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}'
```

## Engine API Methods

eth2030-geth supports Engine API V3 through V6:

| Method | Version | Fork |
|--------|---------|------|
| `engine_newPayloadV3` | V3 | Dencun (EIP-4844 blobs) |
| `engine_newPayloadV4` | V4 | Prague (EIP-7685 requests) |
| `engine_newPayloadV5` | V5 | Amsterdam (EIP-7928 BAL) |
| `engine_forkchoiceUpdatedV3` | V3 | Dencun+ |
| `engine_forkchoiceUpdatedV4` | V4 | Amsterdam |
| `engine_getPayloadV3` | V3 | Dencun |
| `engine_getPayloadV4` | V4 | Prague |
| `engine_getPayloadV6` | V6 | Amsterdam |
| `engine_exchangeCapabilities` | V1 | All |

## Missing Configuration / Known Gaps

The following configurations are **not currently available** in eth2030-geth:

| Missing Config | Impact | Workaround |
|---------------|--------|------------|
| `--bootnodes` | Cannot specify custom bootnodes for private/dev networks | Use `admin_addPeer` RPC after startup |
| `--http.corsdomain` | CORS is fixed to `localhost` for HTTP-RPC | No workaround available |
| `--ws` / `--ws.port` | No WebSocket RPC support on HTTP port | Only Engine API has WebSocket |
| `--cache` | No cache size configuration | Uses go-ethereum defaults |
| `--nat` | No NAT traversal configuration | Manual port forwarding required |
| `--metrics` / `--pprof` | No metrics or profiling endpoint exposed | Not available |
| `--discovery.dns` | No DNS-based peer discovery | Relies on DiscV4/V5 only |

These flags exist in upstream go-ethereum but are not exposed in the
eth2030-geth CLI. See `pkg/cmd/eth2030-geth/main.go` for the current flag set.
