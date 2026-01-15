# ChainScan-Sync - Blockchain Scanning & Synchronization Tool

![License](https://img.shields.io/badge/license-MIT-blue.svg)
![Go Version](https://img.shields.io/badge/go-1.21+-00ADD8.svg)

A powerful blockchain data synchronization tool that supports real-time monitoring of transactions, smart contract events, and address balance changes.

## Features

- **Complete Monitoring**: Transaction monitoring, smart contract event listening, address balance tracking
- **PostgreSQL Storage**: Data persistence using GORM with optimized indexes
- **Checkpoint Resume**: Automatically saves scan progress, supports resuming from breakpoints after interruption
- **Reorg Handling**: Supports automatic detection and rollback of blockchain reorganizations
- **High Performance**: Concurrent scanning, batch processing, intelligent retry mechanism
- **Notification System**: Webhook notifications with batch push and retry support
- **ABI Parsing**: Complete smart contract ABI decoding support
- **Structured Logging**: Detailed logging

## System Requirements

- Go 1.21 or higher
- PostgreSQL 12+
- Stable RPC node connection

## Quick Start

### 1. Installation

```bash
# Clone the repository
git clone https://github.com/Luckyx-Labs/ChainScan-Sync.git
cd chainscan-sync

# Install dependencies
go mod download

# Build
make build
# Or
go build -o bin/chainscan-sync cmd/relayer/main.go
```

### 2. Configure Database

Create PostgreSQL database:

```sql
CREATE DATABASE chainscan;
CREATE USER chainscan_user WITH PASSWORD 'your_password';
GRANT ALL PRIVILEGES ON DATABASE chainscan TO chainscan_user;
```

### 3. Configuration File

Copy and modify the example configuration file:

```bash
cp config.example.yaml config.yaml
```

Edit `config.yaml` and modify the following key configurations.

### 4. Prepare ABI Files

Place smart contract ABI files in the `abis/` directory:

```bash
# Example: Place ERC20 ABI
cp your_contract.abi.json ./abis/ERC20.json
```

### 5. Run

```bash
# Or specify configuration file
./bin/chainscan-sync -config=./config.yaml
```

## Feature Modules

### Block Scanner

- ✅ Get latest block height
- ✅ Sequential block data scanning
- ✅ Parse transaction information within blocks
- ✅ Checkpoint resume mechanism
- ✅ Support starting scan from specified block

### Data Parsing Module

- ✅ Parse transaction data (from, to, value, gas, etc.)
- ✅ Parse smart contract event logs
- ✅ ABI decoding (events and function calls)
- ✅ ERC20 token transfer recognition
- ✅ Transaction receipt parsing

### Data Storage Module

Well-designed database table structure:

- `events` - Event records
- `transactions` - Transaction records
- `deposits` - Deposit records
- `withdraws` - Withdrawal records
- `address_balances` - Address balances
- `balance_histories` - Balance change history
- `blocks` - Block information

### Notification Module

- ✅ Webhook notifications
- ✅ Batch push
- ✅ Retry mechanism
- ✅ Notification logging

### Exception Handling & Optimization

- ✅ Network timeout retry mechanism
- ✅ Blockchain reorganization (reorg) detection

## Project Structure

```
chainscan-sync/
├── cmd/
│   └── relayer/              # Main program entry
│       └── main.go
├── internal/
│   ├── api/                  # API server
│   ├── config/               # Configuration management
│   ├── confirmation/         # Block confirmation handling
│   ├── database/             # Database models and storage
│   │   ├── models.go                    # Data models
│   │   ├── postgres_storage.go          # Core storage (init, close)
│   │   ├── event_storage.go             # Event CRUD operations
│   │   ├── transaction_storage.go       # Transaction CRUD operations
│   │   ├── balance_storage.go           # Balance & history operations
│   │   ├── block_storage.go             # Block operations & rollback
│   │   ├── deposit_withdraw_storage.go  # Deposit/Withdraw operations
│   │   ├── notify_storage.go            # Notification status management
│   │   └── monitor_storage.go           # Monitor address queries
│   ├── handler/              # Event handlers
│   ├── interfaces/           # Interface definitions
│   ├── listener/             # Chain listeners
│   │   └── evm/              # EVM chain implementation
│   ├── manager/              # Manager
│   ├── notifier/             # Notifier
│   ├── parser/               # Data parsing
│   │   └── evm_parser.go
│   └── types/                # Data types
├── pkg/
│   ├── logger/               # Logging utilities
│   └── utils/                # Utility functions
├── abis/                     # Smart contract ABI files
├── config.yaml               # Configuration file
└── README.md
```

## Configuration Guide

### Logging Configuration

```yaml
log:
  level: debug           # debug, info, warn, error
  format: text          # json, text
  output: file          # stdout, file, both
  file_path: ./logs/chainscan-sync.log
```

### Database Configuration

```yaml
database:
  enabled: true
  driver: postgres
  dsn: "host=localhost port=5432 user=postgres password=xxxx dbname=chainscan sslmode=disable"
  dsn2: "host=localhost port=5432 user=postgres password=xxxx dbname=chainscan sslmode=disable"
```

### EVM Chain Configuration

```yaml
evm_chains:
  - name: base-sepolia
    enabled: true
    chain_id: 84532
    rpc_url: https://eth-sepolia.g.alchemy.com/v2/YOUR_API_KEY
    ws_url: ""                    # Optional, WebSocket address
    start_block: latest           # latest, earliest or specific block number
    confirmations: 12              # Number of confirmation blocks
    contracts:
      - address: "0x..."
        name: "ContractName"
        abi_path: "./abis/xxx.json"
        events:
          - Transfer
          - Approval
```

### Performance Configuration

```yaml
performance:
  batch_size: 50                 # Batch processing size
  worker_count: 3                # Number of worker goroutines
  buffer_size: 500               # Buffer size
  poll_interval: 5s              # Polling interval
  max_concurrent_blocks: 5       # Maximum concurrent blocks
```

## License

MIT License

