package types

import (
	"encoding/json"
	"time"
)

// ChainType blockchain type
type ChainType string

const (
	ChainTypeEVM ChainType = "evm"
)

// Event common event structure
type Event struct {
	ID            string                 `json:"id"`            // Unique event ID
	ChainType     ChainType              `json:"chain_type"`    // Chain type
	ChainName     string                 `json:"chain_name"`    // Chain name
	BlockNumber   uint64                 `json:"block_number"`  // Block number
	BlockHash     string                 `json:"block_hash"`    // Block hash
	TxHash        string                 `json:"tx_hash"`       // Transaction hash
	TxIndex       uint                   `json:"tx_index"`      // Transaction index
	LogIndex      uint                   `json:"log_index"`     // Log index
	ContractAddr  string                 `json:"contract_addr"` // Contract address
	EventName     string                 `json:"event_name"`    // Event name
	FromAddress   string                 `json:"from_address"`  // From address
	ToAddress     string                 `json:"to_address"`    // To address
	TokenAddress  string                 `json:"token_address"` // Token address
	GasUsed       uint64                 `json:"gas_used"`      // Gas used
	GasLimit      uint64                 `json:"gas_limit"`     // Gas limit
	GasPrice      string                 `json:"gas_price"`     // Gas price
	Confirmations uint64                 `json:"confirmations"` // Confirmations
	IsFinalized   bool                   `json:"is_finalized"`  // Is finalized
	EventData     map[string]interface{} `json:"event_data"`    // Event data
	Timestamp     time.Time              `json:"timestamp"`     // Timestamp
	RawData       json.RawMessage        `json:"raw_data"`      // Raw data
}

// BlockInfo block information
type BlockInfo struct {
	ChainType   ChainType `json:"chain_type"`
	ChainName   string    `json:"chain_name"`
	BlockNumber uint64    `json:"block_number"`
	BlockHash   string    `json:"block_hash"`
	Timestamp   time.Time `json:"timestamp"`
}

// ProcessStatus process status
type ProcessStatus struct {
	ChainType   ChainType `json:"chain_type"`
	ChainName   string    `json:"chain_name"`
	LastBlock   uint64    `json:"last_block"`
	LastUpdated time.Time `json:"last_updated"`
	IsRunning   bool      `json:"is_running"`
	Error       string    `json:"error,omitempty"`
}
