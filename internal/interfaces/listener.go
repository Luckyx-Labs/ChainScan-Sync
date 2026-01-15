package interfaces

import (
	"context"

	"github.com/Luckyx-Labs/chainscan-sync/internal/types"
)

// ChainListener blockchain listener interface
type ChainListener interface {
	// Start starts the listener
	Start(ctx context.Context) error

	// Stop stops the listener
	Stop() error

	// GetStatus gets the listener status
	GetStatus() *types.ProcessStatus

	// GetChainType gets the chain type
	GetChainType() types.ChainType

	// GetChainName gets the chain name
	GetChainName() string

	// GetErrors gets the error channel
	GetErrors() <-chan error
}

// EventHandler event handler interface
type EventHandler interface {
	// HandleEvent handles an event
	HandleEvent(ctx context.Context, event *types.Event) error

	// GetHandlerName gets the handler name
	GetHandlerName() string

	// CanHandle determines if the event can be handled
	CanHandle(event *types.Event) bool
}

// Storage storage interface
type Storage interface {
	// GetLastBlock gets the last processed block (returns block number and hash)
	GetLastBlock(ctx context.Context, chainType types.ChainType, chainName string) (uint64, string, error)

	// SaveEvent saves an event
	SaveEvent(ctx context.Context, event *types.Event) error

	// GetEvent gets an event
	GetEvent(ctx context.Context, eventID string) (*types.Event, error)

	// IsEventProcessed determines if an event has been processed
	IsEventProcessed(ctx context.Context, eventID string) (bool, error)

	// GetEventsByChain gets events for a specific chain
	GetEventsByChain(ctx context.Context, chainType types.ChainType, chainName string, limit int) ([]*types.Event, error)

	// GetUnprocessedEvents gets unprocessed events
	GetUnprocessedEvents(ctx context.Context, limit int) ([]*types.Event, error)

	// GetEventsByContract gets events for a specific contract
	GetEventsByContract(ctx context.Context, contractAddr string, limit int) ([]*types.Event, error)

	// GetAllMonitorAddresses gets all monitored addresses
	GetAllMonitorAddresses(ctx context.Context) ([]string, error)

	// IsAddressMonitored checks if an address is monitored
	IsAddressMonitored(ctx context.Context, address string) (bool, error)

	// SaveWithdraw saves a withdraw record
	SaveWithdraw(ctx context.Context, withdraw interface{}) error

	// SaveDeposit saves a deposit record
	SaveDeposit(ctx context.Context, deposit interface{}) error

	// UpdateWithdrawNotifyStatus updates the withdraw notification status
	UpdateWithdrawNotifyStatus(ctx context.Context, txHash, status string, notifyError string) error

	// UpdateDepositNotifyStatus updates the deposit notification status
	UpdateDepositNotifyStatus(ctx context.Context, txHash, status string, notifyError string) error

	// GetPendingWithdraws gets pending withdraw records
	GetPendingWithdraws(ctx context.Context, limit int) ([]interface{}, error)

	// GetPendingDeposits gets pending deposit records
	GetPendingDeposits(ctx context.Context, limit int) ([]interface{}, error)

	// GetPendingNotifyTxHashes gets pending notification transaction hashes (grouped by tx_hash)
	GetPendingNotifyTxHashes(ctx context.Context, limit int) ([]string, error)

	// GetEventsByTxHash gets all events for a specific transaction
	GetEventsByTxHash(ctx context.Context, txHash string) ([]*types.Event, error)

	// UpdateEventsNotifyStatus batch updates event notification statuses
	UpdateEventsNotifyStatus(ctx context.Context, txHash, status string, notifyError string) error

	// GetFailedNotifyTxHashes gets transaction hashes with failed notifications
	GetFailedNotifyTxHashes(ctx context.Context, limit int) ([]string, error)

	// RollbackToBlock rolls back to a specified block (deletes all data after that block)
	RollbackToBlock(ctx context.Context, chainType types.ChainType, chainName string, blockNumber uint64) error

	// GetNotifiedEventsAfterBlock gets events that have been notified after a specified block (used for rollback notifications)
	GetNotifiedEventsAfterBlock(ctx context.Context, chainType types.ChainType, chainName string, blockNumber uint64) ([]*types.Event, error)

	// GetAddressBalance gets the balance of an address
	GetAddressBalance(ctx context.Context, chainType, chainName, address, tokenAddress string) (string, error)

	// UpdateAddressBalance updates the balance of an address
	UpdateAddressBalance(ctx context.Context, chainType, chainName, address, tokenAddress string, newBalance string, blockNumber uint64, txHash string) error

	// QueryBlocks query block list
	QueryBlocks(ctx context.Context, params map[string]interface{}, page, pageSize int) ([]interface{}, int64, error)

	// QueryTransactions query transaction list
	QueryTransactions(ctx context.Context, params map[string]interface{}, page, pageSize int) ([]interface{}, int64, error)

	// QueryEvents query event list
	QueryEvents(ctx context.Context, params map[string]interface{}, page, pageSize int) ([]interface{}, int64, error)

	// QueryAddresses query address list
	QueryAddresses(ctx context.Context, params map[string]interface{}, page, pageSize int) ([]interface{}, int64, error)

	// QueryBalances query balance list
	QueryBalances(ctx context.Context, params map[string]interface{}, page, pageSize int) ([]interface{}, int64, error)

	// QueryBalanceHistory query balance history
	QueryBalanceHistory(ctx context.Context, address, tokenAddress string, params map[string]interface{}, page, pageSize int) ([]interface{}, int64, error)

	// GetBlockByNumber gets a block by its number
	GetBlockByNumber(ctx context.Context, chainName string, blockNumber uint64) (interface{}, error)

	// GetTransactionByHash gets a transaction by its hash
	GetTransactionByHash(ctx context.Context, txHash string) (interface{}, error)

	// GetAddressInfo gets address information
	GetAddressInfo(ctx context.Context, chainName, address string) (interface{}, error)

	// GetStatistics gets statistics information
	GetStatistics(ctx context.Context) (map[string]interface{}, error)

	// GetChainStatistics gets chain statistics information
	GetChainStatistics(ctx context.Context, chainName string) (map[string]interface{}, error)

	// UpdateUnconfirmedBlocksConfirmations batch updates the confirmation count of unconfirmed blocks
	// Returns the number of records updated
	UpdateUnconfirmedBlocksConfirmations(ctx context.Context, chainType string, chainName string, latestBlockNumber uint64, requiredConfirmations uint64) (int64, error)

	// Close closes the storage
	Close() error
}

// Notifier notifier interface
type Notifier interface {
	// Notify sends a single event notification
	Notify(ctx context.Context, event *types.Event) error

	// BatchNotify sends batch event notifications
	BatchNotify(ctx context.Context, events []*types.Event) error

	// NotifyRollback sends rollback notifications (notifies external services to revoke transactions during blockchain reorganization)
	NotifyRollback(ctx context.Context, events []*types.Event, rollbackBlockNumber uint64) error

	// GetNotifierName gets the name of the notifier
	GetNotifierName() string
}
