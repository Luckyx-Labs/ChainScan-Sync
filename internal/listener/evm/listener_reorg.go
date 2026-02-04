package evm

import (
	"fmt"

	appTypes "github.com/Luckyx-Labs/chainscan-sync/internal/types"
	"github.com/Luckyx-Labs/chainscan-sync/pkg/logger"
	"github.com/ethereum/go-ethereum/core/types"
)

// detectReorg checks for blockchain reorganization by comparing the parent hash of the current block
// with the last recorded block hash in the database. If a reorg is detected, it triggers the
// reorg handling process and returns true along with the block number to roll back to.
func (l *EVMListener) detectReorg(header *types.Header) (bool, uint64, error) {
	blockNumber := header.Number.Uint64()

	// The first block does not need to be checked
	if blockNumber == 0 {
		return false, 0, nil
	}

	// Get the information of the previous block from the database (block number and hash)
	_, lastBlockHash, err := l.storage.GetLastBlock(l.ctx, appTypes.ChainTypeEVM, l.config.Name)
	if err != nil {
		return false, 0, fmt.Errorf("failed to get last block: %w", err)
	}

	// If there is no previous block record, it means this is the first time processing, no need to check
	if lastBlockHash == "" {
		return false, 0, nil
	}

	parentHash := header.ParentHash.Hex()
	if parentHash != lastBlockHash {
		logger.Warnf("   Blockchain reorg detected!")
		logger.Warnf("   Current block number: %d", blockNumber)
		logger.Warnf("   Current block parent hash: %s (what current block points to)", parentHash)
		logger.Warnf("   Database last block hash: %s (what we have in DB)", lastBlockHash)
		logger.Warnf("   Reason: Current block's parent is not the same as our last recorded block")

		// enter reorg handling process, return reorg flag and rollback block number
		rollbackToBlock, err := l.handleReorg(blockNumber)
		if err != nil {
			return false, 0, fmt.Errorf("failed to handle reorg: %w", err)
		}

		// Return reorg flag and rollback block number
		return true, rollbackToBlock, nil
	}

	return false, 0, nil
}

// handleReorg handles blockchain reorganization
func (l *EVMListener) handleReorg(currentBlockNumber uint64) (uint64, error) {
	// Set reorg flag to prevent new block scanning
	l.reorgMu.Lock()
	l.isReorging = true
	l.reorgMu.Unlock()

	// Ensure the reorg flag is cleared when the function exits
	defer func() {
		l.reorgMu.Lock()
		l.isReorging = false
		l.reorgMu.Unlock()
	}()

	logger.Warnf("========================================")
	logger.Warnf("Start processing blockchain reorg")
	logger.Warnf("========================================")

	// strategy for rollback:
	// 1. Prefer using the configured confirmation count as the rollback depth (more conservative)
	// 2. Rollback at least 10 blocks
	reorgDepth := uint64(l.config.Confirmations)
	if reorgDepth < 10 {
		reorgDepth = 10
	}

	// Ensure rollback target is not negative
	var rollbackToBlock uint64
	if currentBlockNumber <= reorgDepth {
		rollbackToBlock = 0
		logger.Warnf("Block height insufficient, rollback to genesis block")
	} else {
		rollbackToBlock = currentBlockNumber - reorgDepth
	}

	logger.Warnf("Rollback parameters:")
	logger.Warnf("  - Current block: %d", currentBlockNumber)
	logger.Warnf("  - Rollback depth: %d", reorgDepth)
	logger.Warnf("  - Rollback target: %d", rollbackToBlock)

	// Before rollback, notify external services about the events being rolled back
	if l.notifier != nil && l.storage != nil {
		// Get all notified events that need to be rolled back
		notifiedEvents, err := l.storage.GetNotifiedEventsAfterBlock(l.ctx, appTypes.ChainTypeEVM, l.config.Name, rollbackToBlock)
		if err != nil {
			logger.Errorf("Failed to get notified events for rollback notification: %v", err)
		} else if len(notifiedEvents) > 0 {
			// Send rollback notifications
			logger.Infof("Sending rollback notifications for %d events", len(notifiedEvents))
			if err := l.notifier.NotifyRollback(l.ctx, notifiedEvents, rollbackToBlock); err != nil {
				logger.Errorf("Failed to send rollback notifications: %v", err)
				// Continue processing rollback even if notification fails
			}
		}
	}

	// Execute database rollback (delete all data after rollbackToBlock)
	// TODO: Consider retry or alert if rollback fails
	if err := l.storage.RollbackToBlock(l.ctx, appTypes.ChainTypeEVM, l.config.Name, rollbackToBlock); err != nil {
		return 0, fmt.Errorf("failed to rollback database: %w", err)
	}

	logger.Infof("Database rollback successful")
	logger.Infof("Will restart scanning from block %d", rollbackToBlock+1)
	logger.Warnf("========================================")

	return rollbackToBlock, nil
}
