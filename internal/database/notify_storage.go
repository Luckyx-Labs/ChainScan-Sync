package database

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/Luckyx-Labs/chainscan-sync/internal/types"
)

// GetPendingNotifyTxHashes gets a list of transaction hashes pending notification (grouped by tx_hash)
func (s *PostgresStorage) GetPendingNotifyTxHashes(ctx context.Context, limit int) ([]string, error) {
	var txHashes []string
	result := s.db.WithContext(ctx).
		Model(&Event{}).
		Select("tx_hash").
		Where("notify_status = ? AND is_finalized = true", NotifyStatusPending).
		Group("tx_hash").
		Order("MIN(created_at) ASC").
		Limit(limit).
		Pluck("tx_hash", &txHashes)

	if result.Error != nil {
		return nil, fmt.Errorf("failed to get pending notify tx hashes: %w", result.Error)
	}

	return txHashes, nil
}

// UpdateEventsNotifyStatus updates the notification status of events in bulk
func (s *PostgresStorage) UpdateEventsNotifyStatus(ctx context.Context, txHash, status string, notifyError string) error {
	updates := map[string]interface{}{
		"notify_status": status,
		"notify_error":  notifyError,
	}

	if status == NotifyStatusNotified {
		now := time.Now()
		updates["notified_at"] = &now
	}

	if status == NotifyStatusFailed || status == NotifyStatusRetrying {
		result := s.db.WithContext(ctx).
			Model(&Event{}).
			Where("tx_hash = ?", txHash).
			UpdateColumn("notify_retry_count", gorm.Expr("notify_retry_count + 1")).
			Updates(updates)

		if result.Error != nil {
			return fmt.Errorf("failed to update events notify status: %w", result.Error)
		}
	} else {
		result := s.db.WithContext(ctx).
			Model(&Event{}).
			Where("tx_hash = ?", txHash).
			Updates(updates)

		if result.Error != nil {
			return fmt.Errorf("failed to update events notify status: %w", result.Error)
		}
	}

	return nil
}

// GetFailedNotifyTxHashes get a list of transaction hashes that failed notification (grouped by tx_hash)
func (s *PostgresStorage) GetFailedNotifyTxHashes(ctx context.Context, limit int) ([]string, error) {
	var txHashes []string
	result := s.db.WithContext(ctx).
		Model(&Event{}).
		Select("tx_hash").
		Where("notify_status = ? AND is_finalized = true AND notify_retry_count < ?", NotifyStatusFailed, 5). // Retry up to 5 times
		Group("tx_hash").
		Order("MIN(updated_at) ASC").
		Limit(limit).
		Pluck("tx_hash", &txHashes)

	if result.Error != nil {
		return nil, fmt.Errorf("failed to get failed notify tx hashes: %w", result.Error)
	}

	return txHashes, nil
}

// GetNotifiedEventsAfterBlock gets notified events after a specified block (used for rollback notifications)
func (s *PostgresStorage) GetNotifiedEventsAfterBlock(ctx context.Context, chainType types.ChainType, chainName string, blockNumber uint64) ([]*types.Event, error) {
	var dbEvents []Event
	result := s.db.WithContext(ctx).
		Where("chain_type = ? AND chain_name = ? AND block_number > ? AND notify_status = ?",
			chainType, chainName, blockNumber, NotifyStatusNotified).
		Find(&dbEvents)

	if result.Error != nil {
		return nil, result.Error
	}

	// Convert to types.Event
	events := make([]*types.Event, 0, len(dbEvents))
	for _, dbEvent := range dbEvents {
		event := &types.Event{
			ID:            dbEvent.ID,
			ChainType:     types.ChainType(dbEvent.ChainType),
			ChainName:     dbEvent.ChainName,
			BlockNumber:   dbEvent.BlockNumber,
			BlockHash:     dbEvent.BlockHash,
			TxHash:        dbEvent.TxHash,
			TxIndex:       dbEvent.TxIndex,
			LogIndex:      dbEvent.LogIndex,
			ContractAddr:  dbEvent.ContractAddress,
			EventName:     dbEvent.EventName,
			FromAddress:   dbEvent.FromAddress,
			ToAddress:     dbEvent.ToAddress,
			TokenAddress:  dbEvent.TokenAddress,
			Confirmations: dbEvent.Confirmations,
			IsFinalized:   dbEvent.IsFinalized,
			Timestamp:     dbEvent.Timestamp,
		}
		// Parse EventData
		if dbEvent.EventData != nil && *dbEvent.EventData != "" {
			json.Unmarshal([]byte(*dbEvent.EventData), &event.EventData)
		}
		events = append(events, event)
	}

	return events, nil
}
