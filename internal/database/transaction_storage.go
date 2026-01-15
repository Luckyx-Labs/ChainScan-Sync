package database

import (
	"context"
	"fmt"

	"gorm.io/gorm/clause"
)

// SaveTransaction saves a transaction record
func (s *PostgresStorage) SaveTransaction(ctx context.Context, tx *Transaction) error {
	result := s.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "tx_hash"}},
			DoUpdates: clause.AssignmentColumns([]string{"confirmations", "is_finalized", "status", "updated_at"}),
		}).
		Create(tx)

	if result.Error != nil {
		return fmt.Errorf("failed to save transaction: %w", result.Error)
	}

	return nil
}

// GetTransaction gets a transaction by its hash
func (s *PostgresStorage) GetTransaction(ctx context.Context, txHash string) (*Transaction, error) {
	var tx Transaction
	result := s.db.WithContext(ctx).Where("tx_hash = ?", txHash).First(&tx)
	if result.Error != nil {
		return nil, fmt.Errorf("failed to get transaction: %w", result.Error)
	}
	return &tx, nil
}

// GetTransactionsByAddress gets transactions related to an address
func (s *PostgresStorage) GetTransactionsByAddress(ctx context.Context, address string, limit int) ([]*Transaction, error) {
	var transactions []*Transaction
	result := s.db.WithContext(ctx).
		Where("from_address = ? OR to_address = ?", address, address).
		Order("block_number DESC").
		Limit(limit).
		Find(&transactions)

	if result.Error != nil {
		return nil, fmt.Errorf("failed to query transactions by address: %w", result.Error)
	}

	return transactions, nil
}
