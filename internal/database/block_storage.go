package database

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/Luckyx-Labs/chainscan-sync/internal/types"
	appLogger "github.com/Luckyx-Labs/chainscan-sync/pkg/logger"
)

// GetLastBlock gets the last processed block (including block number and hash)
func (s *PostgresStorage) GetLastBlock(ctx context.Context, chainType types.ChainType, chainName string) (uint64, string, error) {
	var block Block

	// Directly query the latest block from the blocks table (ordered by block number descending)
	err := s.db.WithContext(ctx).
		Where("chain_type = ? AND chain_name = ?", chainType, chainName).
		Order("block_number DESC").
		Limit(1).
		First(&block).Error

	if err == gorm.ErrRecordNotFound {
		// No record found, return 0 and empty hash
		return 0, "", nil
	}

	if err != nil {
		return 0, "", fmt.Errorf("failed to get last block from blocks table: %w", err)
	}

	appLogger.Debugf("Last block from blocks table: %s/%s -> %d (%s)", chainType, chainName, block.BlockNumber, block.BlockHash)

	return block.BlockNumber, block.BlockHash, nil
}

func (s *PostgresStorage) SaveBlock(ctx context.Context, block *Block) error {
	result := s.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "chain_type"}, {Name: "chain_name"}, {Name: "block_number"}},
			DoUpdates: clause.AssignmentColumns([]string{"confirmed", "updated_at"}),
		}).
		Create(block)

	if result.Error != nil {
		return fmt.Errorf("failed to save block: %w", result.Error)
	}

	return nil
}

func (s *PostgresStorage) GetBlock(ctx context.Context, chainType, chainName string, blockNumber uint64) (*Block, error) {
	var block Block
	result := s.db.WithContext(ctx).
		Where("chain_type = ? AND chain_name = ? AND block_number = ?", chainType, chainName, blockNumber).
		First(&block)

	if result.Error != nil {
		return nil, fmt.Errorf("failed to get block: %w", result.Error)
	}

	return &block, nil
}

func (s *PostgresStorage) GetLatestBlock(ctx context.Context, chainType, chainName string) (*Block, error) {
	var block Block
	result := s.db.WithContext(ctx).
		Where("chain_type = ? AND chain_name = ?", chainType, chainName).
		Order("block_number DESC").
		First(&block)

	if result.Error == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if result.Error != nil {
		return nil, fmt.Errorf("failed to get latest block: %w", result.Error)
	}

	return &block, nil
}

// RollbackToBlock rolls back the database to a specified block number
func (s *PostgresStorage) RollbackToBlock(ctx context.Context, chainType types.ChainType, chainName string, blockNumber uint64) error {
	tx := s.db.WithContext(ctx).Begin()
	if tx.Error != nil {
		return fmt.Errorf("failed to begin transaction: %w", tx.Error)
	}

	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// Delete events after the specified block
	if err := tx.Where("chain_type = ? AND chain_name = ? AND block_number > ?",
		chainType, chainName, blockNumber).
		Delete(&Event{}).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to delete event data: %w", err)
	}

	// Delete transactions after the specified block
	if err := tx.Where("chain_type = ? AND chain_name = ? AND block_number > ?",
		chainType, chainName, blockNumber).
		Delete(&Transaction{}).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to delete transaction data: %w", err)
	}

	// Delete deposits after the specified block
	if err := tx.Where("chain_type = ? AND chain_name = ? AND block_number > ?",
		chainType, chainName, blockNumber).
		Delete(&Deposit{}).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to delete deposit data: %w", err)
	}

	// Delete withdraws after the specified block
	if err := tx.Where("chain_type = ? AND chain_name = ? AND block_number > ?",
		chainType, chainName, blockNumber).
		Delete(&Withdraw{}).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to delete withdraw data: %w", err)
	}

	// Delete balance_history after the specified block
	if err := tx.Where("chain_type = ? AND chain_name = ? AND block_number > ?",
		chainType, chainName, blockNumber).
		Delete(&BalanceHistory{}).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to delete balance history data: %w", err)
	}

	// Delete blocks after the specified block
	if err := tx.Where("chain_type = ? AND chain_name = ? AND block_number > ?",
		chainType, chainName, blockNumber).
		Delete(&Block{}).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to delete block data: %w", err)
	}

	appLogger.Warnf("Rolled back to block %d, address balances may need recalculation", blockNumber)

	if err := tx.Commit().Error; err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	appLogger.Infof("Successfully rolled back to block: %s/%s -> %d", chainType, chainName, blockNumber)
	return nil
}

// UpdateUnconfirmedBlocksConfirmations batch updates the confirmations and is_finalized status of unconfirmed events
// 1. Updates the confirmations field of all unconfirmed events (for real-time display on the frontend)
// 2. Marks events as finalized when the required number of confirmations is reached
// Returns the number of records updated
func (s *PostgresStorage) UpdateUnconfirmedBlocksConfirmations(
	ctx context.Context,
	chainType string,
	chainName string,
	latestBlockNumber uint64,
	requiredConfirmations uint64,
) (int64, error) {
	var totalUpdated int64
	now := time.Now()

	// 1. Update confirmations for all unfinalized events
	// confirmations = latestBlockNumber - block_number
	updateConfirmationsSQL := `
		UPDATE events 
		SET confirmations = ? - block_number, updated_at = ?
		WHERE chain_type = ? AND chain_name = ? AND is_finalized = false
	`
	confirmResult := s.db.WithContext(ctx).Exec(updateConfirmationsSQL, latestBlockNumber, now, chainType, chainName)
	if confirmResult.Error != nil {
		return 0, fmt.Errorf("failed to update confirmations: %w", confirmResult.Error)
	}
	totalUpdated += confirmResult.RowsAffected

	// 2. Marks events as finalized when the required number of confirmations is reached
	if latestBlockNumber >= requiredConfirmations {
		confirmedMaxBlock := latestBlockNumber - requiredConfirmations
		finalizeResult := s.db.WithContext(ctx).Model(&Event{}).
			Where("chain_type = ? AND chain_name = ? AND is_finalized = ?", chainType, chainName, false).
			Where("block_number <= ?", confirmedMaxBlock).
			Updates(map[string]interface{}{
				"is_finalized": true,
				"confirmed_at": now,
				"updated_at":   now,
			})
		if finalizeResult.Error != nil {
			return 0, fmt.Errorf("failed to update is_finalized: %w", finalizeResult.Error)
		}
	}

	return totalUpdated, nil
}
