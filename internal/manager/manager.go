package manager

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Luckyx-Labs/chainscan-sync/internal/api"
	"github.com/Luckyx-Labs/chainscan-sync/internal/config"
	"github.com/Luckyx-Labs/chainscan-sync/internal/confirmation"
	storage "github.com/Luckyx-Labs/chainscan-sync/internal/database"
	"github.com/Luckyx-Labs/chainscan-sync/internal/handler"
	"github.com/Luckyx-Labs/chainscan-sync/internal/interfaces"
	"github.com/Luckyx-Labs/chainscan-sync/internal/listener/evm"
	"github.com/Luckyx-Labs/chainscan-sync/internal/notifier"
	"github.com/Luckyx-Labs/chainscan-sync/internal/types"
	"github.com/Luckyx-Labs/chainscan-sync/pkg/logger"
	"github.com/ethereum/go-ethereum/ethclient"
)

// Manager
type Manager struct {
	config              *config.Config
	storage             interfaces.Storage
	listeners           []interfaces.ChainListener
	handlers            []interfaces.EventHandler
	notifiers           []interfaces.Notifier
	apiServer           *api.Server
	confirmationUpdater *confirmation.Updater
	ctx                 context.Context
	cancel              context.CancelFunc
	wg                  sync.WaitGroup
}

// NewManager creates a new Manager
func NewManager(cfg *config.Config) *Manager {
	return &Manager{
		config:    cfg,
		listeners: make([]interfaces.ChainListener, 0),
		handlers:  make([]interfaces.EventHandler, 0),
		notifiers: make([]interfaces.Notifier, 0),
	}
}

// Initialize
func (m *Manager) Initialize() error {
	// Initialize storage
	if err := m.initializeStorage(); err != nil {
		return fmt.Errorf("failed to initialize storage: %w", err)
	}

	// Initialize notifiers (before listeners, because listeners need notifiers to send rollback notifications)
	if err := m.initializeNotifiers(); err != nil {
		return fmt.Errorf("failed to initialize notifiers: %w", err)
	}

	// Initialize listeners
	if err := m.initializeListeners(); err != nil {
		return fmt.Errorf("failed to initialize listeners: %w", err)
	}

	// Initialize confirmation updater
	if err := m.initializeConfirmationUpdater(); err != nil {
		return fmt.Errorf("failed to initialize confirmation updater: %w", err)
	}

	// Initialize handlers
	if err := m.initializeHandlers(); err != nil {
		return fmt.Errorf("failed to initialize handlers: %w", err)
	}

	// Initialize API server
	if err := m.initializeAPIServer(); err != nil {
		return fmt.Errorf("failed to initialize API server: %w", err)
	}

	return nil
}

// initializeStorage initializes storage
func (m *Manager) initializeStorage() error {
	if !m.config.Database.Enabled {
		logger.Info("Database storage not enabled")
		return nil
	}

	var err error
	switch m.config.Database.Driver {
	case "postgres", "postgresql":
		if m.config.Database.DSN2 != "" {
			m.storage, err = storage.NewPostgresStorage(m.config.Database.DSN, m.config.Database.DSN2)
		} else {
			m.storage, err = storage.NewPostgresStorage(m.config.Database.DSN)
		}
		if err != nil {
			return fmt.Errorf("failed to create PostgreSQL storage: %w", err)
		}
		logger.Infof("PostgreSQL storage initialized successfully (GORM)")

	default:
		return fmt.Errorf("unsupported database driver: %s (currently only postgres/postgresql supported)", m.config.Database.Driver)
	}

	return nil
}

// initializeListeners initializes chain listeners
func (m *Manager) initializeListeners() error {
	// Initialize EVM chain listeners
	for _, chainCfg := range m.config.EVMChains {
		if !chainCfg.Enabled {
			continue
		}

		listener, err := evm.NewEVMListener(chainCfg, m.config.Retry, m.config.WithdrawEOA.Address)
		if err != nil {
			return fmt.Errorf("failed to create EVM listener (%s): %w", chainCfg.Name, err)
		}

		// Set storage (for checkpointing) and notifier (for rollback notifications)
		if evmListener, ok := listener.(*evm.EVMListener); ok {
			if m.storage != nil {
				evmListener.SetStorage(m.storage)
			}
			// Set the first notifier for rollback notifications
			if len(m.notifiers) > 0 {
				evmListener.SetNotifier(m.notifiers[0])
			}
		}

		m.listeners = append(m.listeners, listener)
		logger.Infof("EVM listener initialized: %s", chainCfg.Name)
	}

	return nil
}

// initializeHandlers initializes handlers
func (m *Manager) initializeHandlers() error {
	if m.storage != nil {
		m.handlers = append(m.handlers, handler.NewStorageHandler(m.storage))
		logger.Info("Storage handler initialized")
	}

	// Add default logger handler
	m.handlers = append(m.handlers, handler.NewLoggerHandler())
	logger.Info("Logger handler initialized")

	return nil
}

// initializeAPIServer initializes API server
func (m *Manager) initializeAPIServer() error {
	if !m.config.API.Enabled {
		logger.Info("API service not enabled")
		return nil
	}

	// Create API server
	m.apiServer = api.NewServer(&m.config.API)

	// Register all EVM listeners (for dynamic reload of monitored addresses)
	for _, listener := range m.listeners {
		if evmListener, ok := listener.(*evm.EVMListener); ok {
			m.apiServer.RegisterListener(evmListener)
		}
	}

	logger.Infof("API server initialized (auth_token configured: %v)", m.config.API.AuthToken != "")
	return nil
}

// initializeNotifiers initializes notifiers
func (m *Manager) initializeNotifiers() error {
	if m.config.Webhooks.Enabled {
		m.notifiers = append(m.notifiers, notifier.NewWebhookNotifier(m.config.Webhooks, m.config.WithdrawEOA.Address))
		logger.Info("Webhook notifier initialized")
	}

	return nil
}

// initializeConfirmationUpdater initializes confirmation updater
func (m *Manager) initializeConfirmationUpdater() error {
	if m.storage == nil {
		logger.Info("Confirmation updater not enabled (requires database storage)")
		return nil
	}

	// Get the first enabled EVM chain config (for creating checker)
	var chainConfig *config.EVMChainConfig
	for i := range m.config.EVMChains {
		if m.config.EVMChains[i].Enabled {
			chainConfig = &m.config.EVMChains[i]
			break
		}
	}

	if chainConfig == nil {
		logger.Info("Confirmation updater not enabled (no enabled chains)")
		return nil
	}

	// create evm chain client
	client, err := ethclient.Dial(chainConfig.RPCURL)
	if err != nil {
		return fmt.Errorf("failed to connect to RPC node: %w", err)
	}

	// create confirmation checker
	checker := confirmation.NewChecker(
		chainConfig.Name,
		client,
		uint64(chainConfig.Confirmations),
	)

	// create confirmation updater (updates every 2 seconds, synchronized with block time)
	m.confirmationUpdater = confirmation.NewUpdater(
		m.storage,
		checker,
		2*time.Second,
	)

	logger.Infof("Confirmation updater initialized (updates every 2 seconds, requires %d confirmations)", chainConfig.Confirmations)
	return nil
}

// Start starts the manager
func (m *Manager) Start(ctx context.Context) error {
	m.ctx, m.cancel = context.WithCancel(ctx)

	// Start confirmation updater (background task)
	if m.confirmationUpdater != nil {
		m.wg.Add(1)
		go func() {
			defer m.wg.Done()
			defer func() {
				if r := recover(); r != nil {
					logger.Errorf("Panic in confirmation updater: %v", r)
				}
			}()
			m.confirmationUpdater.Start(m.ctx)
		}()
		logger.Info("Confirmation updater started")
	}

	// Start event notification tasks (background tasks)
	if m.storage != nil && len(m.notifiers) > 0 {
		// Normal notification task
		m.wg.Add(1)
		go func() {
			defer m.wg.Done()
			defer func() {
				if r := recover(); r != nil {
					logger.Errorf("Panic in confirmation updater: %v", r)
				}
			}()
			m.processEventNotifications()
		}()
		logger.Info("Event notification task started")

		// failed retry task
		m.wg.Add(1)
		go func() {
			defer m.wg.Done()
			defer func() {
				if r := recover(); r != nil {
					logger.Errorf("Panic in confirmation updater: %v", r)
				}
			}()
			m.processFailedNotifications()
		}()
		logger.Info("Failed notification retry task started")
	}

	// Start API server
	if m.apiServer != nil {
		if err := m.apiServer.Start(); err != nil {
			return fmt.Errorf("failed to start API server: %w", err)
		}
		logger.Infof("API server started at %s:%d", m.config.API.Host, m.config.API.Port)
	}

	for _, listener := range m.listeners {
		if err := listener.Start(m.ctx); err != nil {
			return fmt.Errorf("failed to start listener (%s): %w", listener.GetChainName(), err)
		}

		// Start error handling goroutine for each listener
		m.wg.Add(1)
		go m.processErrors(listener)
	}

	logger.Info("all listeners started successfully")
	return nil
}

// processErrors handles errors
func (m *Manager) processErrors(listener interfaces.ChainListener) {
	defer m.wg.Done()

	for {
		select {
		case <-m.ctx.Done():
			return
		case err, ok := <-listener.GetErrors():
			if !ok {
				return
			}

			logger.Errorf("Listener %s error: %v", listener.GetChainName(), err)
		}
	}
}

// processEventNotifications handles event notifications (normal flow)
func (m *Manager) processEventNotifications() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	logger.Info("Event notification processor started")

	for {
		select {
		case <-m.ctx.Done():
			logger.Info("Event notification processor stopped")
			return
		case <-ticker.C:
			logger.Debug("================= start sending pending notifications =================")
			if err := m.sendPendingNotifications(); err != nil {
				logger.Errorf("Failed to send pending notifications: %v", err)
			}
		}
	}
}

// processFailedNotifications handles retrying failed notifications (separate goroutine)
func (m *Manager) processFailedNotifications() {
	ticker := time.NewTicker(30 * time.Second) // check failed notifications every 30 seconds
	defer ticker.Stop()

	logger.Info("Failed notification retry processor started")

	for {
		select {
		case <-m.ctx.Done():
			logger.Info("Failed notification retry processor stopped")
			return
		case <-ticker.C:
			if err := m.retryFailedNotifications(); err != nil {
				logger.Errorf("Failed to retry notifications: %v", err)
			}
		}
	}
}

// sendPendingNotifications sends pending event notifications
func (m *Manager) sendPendingNotifications() error {
	// Get list of transaction hashes pending notification
	txHashes, err := m.storage.GetPendingNotifyTxHashes(m.ctx, 20) // up to 20 transactions at a time
	if err != nil {
		return fmt.Errorf("failed to get pending notify tx hashes: %w", err)
	}

	if len(txHashes) == 0 {
		return nil
	}

	logger.Infof("detected %d transactions pending notification", len(txHashes))

	// handle each transaction
	for _, txHash := range txHashes {
		if err := m.notifyTransactionEvents(txHash); err != nil {
			logger.Errorf("Failed to notify transaction %s: %v", txHash, err)
		}
	}

	return nil
}

// retryFailedNotifications retries failed event notifications
func (m *Manager) retryFailedNotifications() error {
	// Get list of failed notification transaction hashes
	txHashes, err := m.storage.GetFailedNotifyTxHashes(m.ctx, 20) // up to 20 transactions at a time
	if err != nil {
		return fmt.Errorf("failed to get failed notify tx hashes: %w", err)
	}

	if len(txHashes) == 0 {
		return nil
	}

	logger.Infof("Retrying %d failed notification transactions", len(txHashes))

	// Handle retry for each transaction
	for _, txHash := range txHashes {
		// Update status to retrying first
		if err := m.storage.UpdateEventsNotifyStatus(m.ctx, txHash, storage.NotifyStatusRetrying, ""); err != nil {
			logger.Errorf("Failed to update retry status for tx %s: %v", txHash, err)
			continue
		}

		// Perform retry
		if err := m.notifyTransactionEvents(txHash); err != nil {
			logger.Errorf("Failed to retry notification for tx %s: %v", txHash, err)
		}
	}

	return nil
}

// notifyTransactionEvents sends notifications for all events of a specified transaction (single send)
func (m *Manager) notifyTransactionEvents(txHash string) error {
	// Get all events for the transaction
	events, err := m.storage.GetEventsByTxHash(m.ctx, txHash)
	if err != nil {
		return fmt.Errorf("failed to get events by tx hash: %w", err)
	}
	for i := 0; i < len(events); i++ {
		logger.Debugf("Event[%d]: FromAddress=%s, ToAddress=%s, TokenAddress=%s, GasPrice=%s",
			i, events[i].FromAddress, events[i].ToAddress, events[i].TokenAddress, events[i].GasPrice)
	}

	if len(events) == 0 {
		return nil
	}

	logger.Infof("Sending %d event notifications for transaction [%s]", len(events), txHash)

	// Send event notifications one by one
	successCount := 0
	var lastError string

	for _, event := range events {
		eventNotified := false

		for _, notifier := range m.notifiers {
			if err := notifier.Notify(m.ctx, event); err != nil {
				lastError = fmt.Sprintf("%s: %v", notifier.GetNotifierName(), err)
				logger.Errorf("Event notification failed [%s] event %s via %s: %v",
					txHash, event.EventName, notifier.GetNotifierName(), err)
			} else {
				eventNotified = true
				logger.Infof("Event notification successful [%s] %s via %s",
					txHash, event.EventName, notifier.GetNotifierName())
			}
		}

		if eventNotified {
			successCount++
		}
	}

	// Update notification status (considered successful if at least one event is successful)
	if successCount > 0 {
		if err := m.storage.UpdateEventsNotifyStatus(m.ctx, txHash, storage.NotifyStatusNotified, ""); err != nil {
			logger.Errorf("Failed to update notify status: %v", err)
			return err
		}
		logger.Infof("Transaction [%s] notification completed: %d/%d events sent successfully",
			txHash, successCount, len(events))
	} else {
		if err := m.storage.UpdateEventsNotifyStatus(m.ctx, txHash, storage.NotifyStatusFailed, lastError); err != nil {
			logger.Errorf("Failed to update notify status: %v", err)
			return err
		}
		logger.Warnf("Transaction [%s] notification failed: 0/%d events sent", txHash, len(events))
	}

	return nil
}

// processWithdrawNotifications handles withdraw notifications
func (m *Manager) processWithdrawNotifications() error {
	// Get pending withdraw records (confirmed)
	items, err := m.storage.GetPendingWithdraws(m.ctx, 20)
	if err != nil {
		return fmt.Errorf("failed to get pending withdraw records: %w", err)
	}

	if len(items) == 0 {
		return nil
	}

	logger.Infof("Detected %d pending withdraw notifications", len(items))

	for _, item := range items {
		withdraw, ok := item.(*storage.Withdraw)
		if !ok {
			logger.Errorf("Invalid withdraw record type")
			continue
		}

		// Construct event for notification
		event := &types.Event{
			ID:          fmt.Sprintf("withdraw-%s", withdraw.TxHash),
			ChainType:   types.ChainType(withdraw.ChainType),
			ChainName:   withdraw.ChainName,
			BlockNumber: withdraw.BlockNumber,
			BlockHash:   withdraw.BlockHash,
			TxHash:      withdraw.TxHash,
			EventName:   "Withdraw", // Custom event name
			EventData: map[string]interface{}{
				"from":          withdraw.FromAddress,
				"to":            withdraw.ToAddress,
				"token":         withdraw.TokenAddress,
				"amount":        withdraw.Amount,
				"gas_fee":       withdraw.GasFee,
				"status":        withdraw.Status,
				"confirmations": withdraw.Confirmations,
			},
			Timestamp: withdraw.Timestamp,
		}

		// Send notification
		notified := false
		var lastError string
		for _, notifier := range m.notifiers {
			if err := notifier.Notify(m.ctx, event); err != nil {
				lastError = fmt.Sprintf("%s: %v", notifier.GetNotifierName(), err)
				logger.Errorf("Withdraw notification failed [%s] %s: %v", withdraw.TxHash, notifier.GetNotifierName(), err)
			} else {
				notified = true
				logger.Infof("Withdraw notification successful [%s] %s -> %s, amount: %s",
					withdraw.TxHash, withdraw.FromAddress, withdraw.ToAddress, withdraw.Amount)
			}
		}

		// Update notification status
		if notified {
			if err := m.storage.UpdateWithdrawNotifyStatus(m.ctx, withdraw.TxHash, storage.NotifyStatusNotified, ""); err != nil {
				logger.Errorf("Failed to update withdraw notification status: %v", err)
			}
		} else {
			if err := m.storage.UpdateWithdrawNotifyStatus(m.ctx, withdraw.TxHash, storage.NotifyStatusFailed, lastError); err != nil {
				logger.Errorf("Failed to update withdraw notification status: %v", err)
			}
		}
	}

	return nil
}

// processDepositNotifications handles deposit notifications
func (m *Manager) processDepositNotifications() error {
	items, err := m.storage.GetPendingDeposits(m.ctx, 20)
	if err != nil {
		return fmt.Errorf("failed to get pending deposit records: %w", err)
	}

	if len(items) == 0 {
		return nil
	}

	logger.Infof("Detected %d pending deposit notifications", len(items))

	for _, item := range items {
		deposit, ok := item.(*storage.Deposit)
		if !ok {
			logger.Errorf("Invalid deposit record type")
			continue
		}

		event := &types.Event{
			ID:          fmt.Sprintf("deposit-%s", deposit.TxHash),
			ChainType:   types.ChainType(deposit.ChainType),
			ChainName:   deposit.ChainName,
			BlockNumber: deposit.BlockNumber,
			BlockHash:   deposit.BlockHash,
			TxHash:      deposit.TxHash,
			EventName:   "Deposit", // Custom event name
			EventData: map[string]interface{}{
				"from":          deposit.FromAddress,
				"to":            deposit.ToAddress,
				"token":         deposit.TokenAddress,
				"amount":        deposit.Amount,
				"gas_fee":       deposit.GasFee,
				"status":        deposit.Status,
				"confirmations": deposit.Confirmations,
			},
			Timestamp: deposit.Timestamp,
		}

		notified := false
		var lastError string
		for _, notifier := range m.notifiers {
			if err := notifier.Notify(m.ctx, event); err != nil {
				lastError = fmt.Sprintf("%s: %v", notifier.GetNotifierName(), err)
				logger.Errorf("Deposit notification failed [%s] %s: %v", deposit.TxHash, notifier.GetNotifierName(), err)
			} else {
				notified = true
				logger.Infof("Deposit notification successful [%s] %s -> %s, amount: %s",
					deposit.TxHash, deposit.FromAddress, deposit.ToAddress, deposit.Amount)
			}
		}

		// Update notification status
		// TODO: Determine success based on the status code returned by the middle platform
		if notified {
			if err := m.storage.UpdateDepositNotifyStatus(m.ctx, deposit.TxHash, storage.NotifyStatusNotified, ""); err != nil {
				logger.Errorf("Failed to update deposit notification status: %v", err)
			}
		} else {
			if err := m.storage.UpdateDepositNotifyStatus(m.ctx, deposit.TxHash, storage.NotifyStatusFailed, lastError); err != nil {
				logger.Errorf("Failed to update deposit notification status: %v", err)
			}
		}
	}

	return nil
}

func (m *Manager) Stop() error {
	if m.cancel != nil {
		m.cancel()
	}

	if m.confirmationUpdater != nil {
		m.confirmationUpdater.Stop()
		logger.Info("Confirmation updater stopped")
	}

	// Stop API server
	if m.apiServer != nil {
		if err := m.apiServer.Stop(); err != nil {
			logger.Errorf("Failed to stop API server: %v", err)
		}
	}

	for _, listener := range m.listeners {
		if err := listener.Stop(); err != nil {
			logger.Errorf("Failed to stop listener (%s): %v", listener.GetChainName(), err)
		}
	}

	m.wg.Wait()

	if m.storage != nil {
		if err := m.storage.Close(); err != nil {
			logger.Errorf("Failed to close storage: %v", err)
		}
	}

	logger.Info("All listeners stopped, resources cleaned up")
	return nil
}

func (m *Manager) GetStorage() interfaces.Storage {
	return m.storage
}

func (m *Manager) GetStatus() []*types.ProcessStatus {
	statuses := make([]*types.ProcessStatus, 0, len(m.listeners))
	for _, listener := range m.listeners {
		statuses = append(statuses, listener.GetStatus())
	}
	return statuses
}

// ReloadMonitorAddresses reloads monitored addresses
// Used to dynamically update the bloom filter when monitored addresses change in the database
// Usage: Call when there are additions/deletions in the web_user_chain_address table in the middle platform database
func (m *Manager) ReloadMonitorAddresses() error {
	logger.Info("Reloading monitor addresses for all listeners...")

	var errCount int
	for _, listener := range m.listeners {
		// Only handle EVM listeners (other chain types to be added later)
		if evmListener, ok := listener.(*evm.EVMListener); ok {
			if err := evmListener.ReloadMonitorAddresses(); err != nil {
				logger.Errorf("Failed to reload monitor addresses for %s: %v",
					evmListener.GetChainName(), err)
				errCount++
			}
		}
	}

	if errCount > 0 {
		return fmt.Errorf("failed to reload monitor addresses for %d listener(s)", errCount)
	}

	logger.Info("Monitor addresses reloaded successfully for all listeners")
	return nil
}
