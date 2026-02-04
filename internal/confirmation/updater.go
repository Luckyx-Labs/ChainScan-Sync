package confirmation

import (
	"context"
	"time"

	storage "github.com/Luckyx-Labs/chainscan-sync/internal/database"
	"github.com/Luckyx-Labs/chainscan-sync/internal/interfaces"
	"github.com/Luckyx-Labs/chainscan-sync/pkg/logger"
)

// Updater confirmation updater struct
type Updater struct {
	storage  interfaces.Storage
	checker  *Checker
	interval time.Duration
	ctx      context.Context
	cancel   context.CancelFunc
}

// NewUpdater creates a new confirmation updater
func NewUpdater(storage interfaces.Storage, checker *Checker, interval time.Duration) *Updater {
	return &Updater{
		storage:  storage,
		checker:  checker,
		interval: interval,
	}
}

// Start starts the periodic update task
func (u *Updater) Start(ctx context.Context) {
	u.ctx, u.cancel = context.WithCancel(ctx)
	logger.Info("confirmation updater started")

	ticker := time.NewTicker(u.interval)
	defer ticker.Stop()

	// Execute once immediately
	u.updateConfirmations()

	for {
		select {
		case <-ticker.C:
			u.updateConfirmations()
		case <-u.ctx.Done():
			logger.Info("confirmation updater stopped")
			return
		}
	}
}

// Stop stops the update task
func (u *Updater) Stop() {
	if u.cancel != nil {
		u.cancel()
	}
}

// updateConfirmations updates the confirmations for all unfinalized events
func (u *Updater) updateConfirmations() {
	startTime := time.Now()

	// Get the current block number
	currentBlock, err := u.checker.GetCurrentBlockNumber()
	if err != nil {
		logger.Errorf("Failed to get current block: %v", err)
		return
	}

	// Update event confirmations
	updatedEventCount, err := u.updateEventConfirmations(currentBlock)
	if err != nil {
		logger.Errorf("Failed to update event confirmations: %v", err)
	}

	duration := time.Since(startTime)
	if updatedEventCount > 0 {
		logger.Infof("Confirmation update completed: events %d, current block %d, elapsed %v",
			updatedEventCount, currentBlock, duration)
	}
}

// updateEventConfirmations updates the confirmations for all unfinalized events
func (u *Updater) updateEventConfirmations(currentBlock uint64) (int64, error) {
	query := `
		UPDATE blockscan.events
		SET 
			confirmations = ? - block_number,
			is_finalized = CASE 
				WHEN ? - block_number >= ? THEN true 
				ELSE false 
			END,
			confirmed_at = CASE
				WHEN is_finalized = false AND ? - block_number >= ?
				THEN NOW()
				ELSE confirmed_at
			END,
			updated_at = NOW()
		WHERE is_finalized = false
		AND block_number <= ?
	`

	result := u.storage.(*storage.PostgresStorage).GetDB().Exec(query,
		currentBlock,
		currentBlock,
		u.checker.requiredConfirmations,
		currentBlock,
		u.checker.requiredConfirmations,
		currentBlock,
	)

	if result.Error != nil {
		return 0, result.Error
	}

	return result.RowsAffected, nil
}
