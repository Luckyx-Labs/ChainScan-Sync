package database

import (
	"time"

	"gorm.io/gorm"
)

const TableNameSchema = "blockscan"

// Event table
type Event struct {
	ID               string     `gorm:"primaryKey;type:varchar(100)"`                        // event unique ID
	ChainType        string     `gorm:"type:varchar(20);not null;index"`                     // chain type
	ChainName        string     `gorm:"type:varchar(50);not null;index"`                     // chain name
	BlockNumber      uint64     `gorm:"type:bigint;not null;index"`                          // block number
	BlockHash        string     `gorm:"type:varchar(66);index"`                              // block hash
	TxHash           string     `gorm:"type:varchar(66);not null;uniqueIndex:idx_tx_log"`    // transaction hash
	TxIndex          uint       `gorm:"type:integer"`                                        // transaction index
	LogIndex         uint       `gorm:"type:integer;uniqueIndex:idx_tx_log"`                 // log index
	ContractAddress  string     `gorm:"type:varchar(66);index"`                              // contract address
	EventName        string     `gorm:"type:varchar(100);not null;index"`                    // event name
	FromAddress      string     `gorm:"type:varchar(66);index"`                              // from address
	ToAddress        string     `gorm:"type:varchar(66);index"`                              // to address
	TokenAddress     string     `gorm:"type:varchar(66);index"`                              // token address
	EventSignature   string     `gorm:"type:varchar(66)"`                                    // event signature
	EventData        *string    `gorm:"type:jsonb"`                                          // event data (JSONB, optional)
	RawData          *string    `gorm:"type:jsonb"`                                          // raw data (JSONB, optional)
	GasUsed          uint64     `gorm:"type:bigint"`                                         // gas used
	GasLimit         uint64     `gorm:"type:bigint"`                                         // gas limit
	GasPrice         string     `gorm:"type:varchar(78)"`                                    // gas price
	Timestamp        time.Time  `gorm:"not null;index"`                                      // event timestamp
	Confirmations    uint64     `gorm:"type:integer;default:0;index"`                        // current confirmations
	IsFinalized      bool       `gorm:"default:false;index"`                                 // is finalized
	ConfirmedAt      *time.Time `gorm:"index"`                                               // confirmed at
	Processed        bool       `gorm:"default:false;index"`                                 // is processed
	NotifyStatus     string     `gorm:"type:varchar(20);default:'pending';index:idx_notify"` // notify status
	NotifiedAt       *time.Time `gorm:"index"`                                               // notified at
	NotifyRetryCount int        `gorm:"default:0"`                                           // notify retry count
	NotifyError      string     `gorm:"type:text"`                                           // notify error message
	RetryCount       int        `gorm:"default:0"`                                           // processing retry count
	ErrorMessage     string     `gorm:"type:text"`                                           // processing error message
	CreatedAt        time.Time
	UpdatedAt        time.Time
	DeletedAt        gorm.DeletedAt `gorm:"index"` // soft delete
}

// TableName specifies the table name
func (Event) TableName() string {
	return TableNameSchema + ".events"
}

// Transaction transaction table
type Transaction struct {
	ID              uint       `gorm:"primaryKey;autoIncrement"`
	ChainType       string     `gorm:"type:varchar(20);not null;index"`
	ChainName       string     `gorm:"type:varchar(50);not null;index"`
	BlockNumber     uint64     `gorm:"type:bigint;not null;index"`
	BlockHash       string     `gorm:"type:varchar(66);index"`
	TxHash          string     `gorm:"type:varchar(66);not null;uniqueIndex:idx_chain_tx"`
	TxIndex         uint       `gorm:"type:integer"`
	FromAddress     string     `gorm:"type:varchar(66);not null;index"`
	ToAddress       string     `gorm:"type:varchar(66);index"` // the to address may be empty for contract creation
	Value           string     `gorm:"type:varchar(78)"`       // transfer amount
	GasUsed         uint64     `gorm:"type:bigint"`
	GasPrice        string     `gorm:"type:varchar(78)"` // gas price
	GasLimit        uint64     `gorm:"type:bigint"`
	Nonce           uint64     `gorm:"type:bigint"`
	InputData       string     `gorm:"type:text"`              // transaction input data
	Status          uint64     `gorm:"type:smallint;index"`    // 0=failed, 1=successful
	ContractAddress string     `gorm:"type:varchar(66);index"` // contract address created during deployment
	Timestamp       time.Time  `gorm:"not null;index"`
	Confirmations   uint64     `gorm:"type:integer;default:0;index"` // current confirmation count
	IsFinalized     bool       `gorm:"default:false;index"`          // whether it is finally confirmed
	ConfirmedAt     *time.Time `gorm:"index"`                        // final confirmation time
	RawData         *string    `gorm:"type:jsonb"`                   // raw transaction data (optional)
	CreatedAt       time.Time
	UpdatedAt       time.Time
	DeletedAt       gorm.DeletedAt `gorm:"index"`
}

// TableName specifies the table name
func (Transaction) TableName() string {
	return TableNameSchema + ".transactions"
}

// AddressBalance address balance table
type AddressBalance struct {
	ID            uint      `gorm:"primaryKey"`
	ChainType     string    `gorm:"type:varchar(20);not null;uniqueIndex:idx_address"`
	ChainName     string    `gorm:"type:varchar(50);not null;uniqueIndex:idx_address"`
	Address       string    `gorm:"type:varchar(66);not null;uniqueIndex:idx_address"`
	TokenAddress  string    `gorm:"type:varchar(66);default:'0x0';uniqueIndex:idx_address"` // 0x0 indicates native token
	Balance       string    `gorm:"type:varchar(78)"`                                       // balance
	BlockNumber   uint64    `gorm:"type:bigint;not null;index"`                             // last updated block number
	LastTxHash    string    `gorm:"type:varchar(66)"`                                       // last transaction hash
	LastUpdatedAt time.Time `gorm:"not null;index"`                                         // last update time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// TableName specifies the table name
func (AddressBalance) TableName() string {
	return TableNameSchema + ".address_balances"
}

// BalanceHistory address balance change history table
type BalanceHistory struct {
	ID            uint      `gorm:"primaryKey"`
	ChainType     string    `gorm:"type:varchar(20);not null;index"`
	ChainName     string    `gorm:"type:varchar(50);not null;index"`
	Address       string    `gorm:"type:varchar(66);not null;index;uniqueIndex:idx_balance_history_unique"`
	TokenAddress  string    `gorm:"type:varchar(66);default:'0x0';index;uniqueIndex:idx_balance_history_unique"` // 0x0 indicates native token
	BlockNumber   uint64    `gorm:"type:bigint;not null;index"`
	TxHash        string    `gorm:"type:varchar(66);not null;index;uniqueIndex:idx_balance_history_unique"` // unique index with address, token_address
	BalanceBefore string    `gorm:"type:varchar(78)"`                                                       // balance before change
	BalanceAfter  string    `gorm:"type:varchar(78)"`                                                       // balance after change
	BalanceChange string    `gorm:"type:varchar(78)"`                                                       // balance change amount (may be negative)
	ChangeType    string    `gorm:"type:varchar(20)"`                                                       // increase, decrease
	Timestamp     time.Time `gorm:"not null;index"`
	CreatedAt     time.Time
}

// TableName specifies the table name
func (BalanceHistory) TableName() string {
	return TableNameSchema + ".balance_histories"
}

// Block block information table
type Block struct {
	ID          uint      `gorm:"primaryKey"`
	ChainType   string    `gorm:"type:varchar(20);not null;uniqueIndex:idx_block"`
	ChainName   string    `gorm:"type:varchar(50);not null;uniqueIndex:idx_block"`
	BlockNumber uint64    `gorm:"type:bigint;not null;uniqueIndex:idx_block"`
	BlockHash   string    `gorm:"type:varchar(66);not null"`
	ParentHash  string    `gorm:"type:varchar(66)"`
	Miner       string    `gorm:"type:varchar(66);index"` // EVM specific
	GasUsed     uint64    `gorm:"type:bigint"`
	GasLimit    uint64    `gorm:"type:bigint"`
	Timestamp   time.Time `gorm:"not null;index"`
	TxCount     uint      `gorm:"type:integer"` // transaction count
	Confirmed   bool      `gorm:"default:false;index"`
	RawData     *string   `gorm:"type:jsonb"` // raw block data (optional)
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// TableName specifies the table name
func (Block) TableName() string {
	return TableNameSchema + ".blocks"
}

// WebUserChainAddress user chain address table (business layer monitoring address)
type WebUserChainAddress struct {
	ID        int64  `gorm:"primaryKey;autoIncrement"`
	Address   string `gorm:"type:varchar(66);not null;index"` // address
	PublicKey string `gorm:"type:varchar(132)"`               // public key
	UserID    int    `gorm:"type:integer;not null;index"`     // user ID
	Nonce     int64  `gorm:"type:bigint;default:0"`           // nonce value
	CreatedAt time.Time
	UpdatedAt time.Time
}

// TableName specifies the table name (cross-database query account database)
func (WebUserChainAddress) TableName() string {
	return "account.web_user_chain_address"
}

// NotifyStatus notification status constants
const (
	NotifyStatusPending  = "pending"  // pending notification
	NotifyStatusNotified = "notified" // notified
	NotifyStatusFailed   = "failed"   // notification failed
	NotifyStatusRetrying = "retrying" // retrying
)

// Withdraw withdrawal/outbound table
type Withdraw struct {
	ID            uint      `gorm:"primaryKey"`
	ChainType     string    `gorm:"type:varchar(20);not null;index"`
	ChainName     string    `gorm:"type:varchar(50);not null;index"`
	TxHash        string    `gorm:"type:varchar(66);not null;index;uniqueIndex:idx_withdraw_unique"` // transaction hash
	BlockNumber   uint64    `gorm:"type:bigint;not null;index"`
	BlockHash     string    `gorm:"type:varchar(66)"`
	FromAddress   string    `gorm:"type:varchar(66);not null;index;uniqueIndex:idx_withdraw_unique"` // monitored address (withdrawal address, unique index with TxHash)
	ToAddress     string    `gorm:"type:varchar(66);not null;index"`                                 // target address
	TokenAddress  string    `gorm:"type:varchar(66);default:'0x0';index"`                            // token address (0x0 = native coin)
	Amount        string    `gorm:"type:varchar(78);not null"`                                       // withdrawal amount
	GasUsed       uint64    `gorm:"type:bigint"`
	GasFee        string    `gorm:"type:varchar(78)"`    // Gas fee
	Status        uint64    `gorm:"type:smallint;index"` // transaction status: 0=failed, 1=success
	Confirmations uint64    `gorm:"type:integer;default:0;index"`
	IsFinalized   bool      `gorm:"default:false;index"`
	Timestamp     time.Time `gorm:"not null;index"`
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// TableName specifies the table name
func (Withdraw) TableName() string {
	return TableNameSchema + ".withdraws"
}

// Deposit deposit/inbound table
type Deposit struct {
	ID            uint      `gorm:"primaryKey"`
	ChainType     string    `gorm:"type:varchar(20);not null;index"`
	ChainName     string    `gorm:"type:varchar(50);not null;index"`
	TxHash        string    `gorm:"type:varchar(66);not null;index;uniqueIndex:idx_deposit_unique"` // transaction hash
	BlockNumber   uint64    `gorm:"type:bigint;not null;index"`
	BlockHash     string    `gorm:"type:varchar(66)"`
	FromAddress   string    `gorm:"type:varchar(66);not null;index;uniqueIndex:idx_deposit_unique"` // source address (unique index with TxHash)
	ToAddress     string    `gorm:"type:varchar(66);not null;index"`                                // monitored address (deposit address)
	TokenAddress  string    `gorm:"type:varchar(66);default:'0x0';index"`                           // token address (0x0 = native coin)
	Amount        string    `gorm:"type:varchar(78);not null"`                                      // deposit amount
	GasUsed       uint64    `gorm:"type:bigint"`
	GasFee        string    `gorm:"type:varchar(78)"`    // Gas fee
	Status        uint64    `gorm:"type:smallint;index"` // transaction status: 0=failed, 1=success
	Confirmations uint64    `gorm:"type:integer;default:0;index"`
	IsFinalized   bool      `gorm:"default:false;index"`
	Timestamp     time.Time `gorm:"not null;index"`
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// TableName specifies the table name
func (Deposit) TableName() string {
	return TableNameSchema + ".deposits"
}
