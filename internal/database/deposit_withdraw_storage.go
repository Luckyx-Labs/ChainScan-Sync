package database

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (s *PostgresStorage) SaveWithdraw(ctx context.Context, withdraw interface{}) error {
	w, ok := withdraw.(*Withdraw)
	if !ok {
		return fmt.Errorf("invalid Withdraw type")
	}

	// Use UPSERT to avoid duplicate inserts (based on unique index of tx_hash + from_address)
	result := s.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "tx_hash"}, {Name: "from_address"}},
			DoUpdates: clause.AssignmentColumns([]string{"confirmations", "is_finalized", "updated_at"}),
		}).
		Create(w)

	if result.Error != nil {
		return fmt.Errorf("failed to save withdraw record: %w", result.Error)
	}

	return nil
}

// SaveDeposit saves a deposit record
func (s *PostgresStorage) SaveDeposit(ctx context.Context, deposit interface{}) error {
	d, ok := deposit.(*Deposit)
	if !ok {
		return fmt.Errorf("invalid Deposit type")
	}

	// Use UPSERT to avoid duplicate inserts (based on unique index of tx_hash + from_address)
	result := s.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "tx_hash"}, {Name: "from_address"}},
			DoUpdates: clause.AssignmentColumns([]string{"confirmations", "is_finalized", "updated_at"}),
		}).
		Create(d)

	if result.Error != nil {
		return fmt.Errorf("failed to save deposit record: %w", result.Error)
	}

	return nil
}

// UpdateWithdrawNotifyStatus updates the withdraw notification status
func (s *PostgresStorage) UpdateWithdrawNotifyStatus(ctx context.Context, txHash, status string, notifyError string) error {
	updates := map[string]interface{}{
		"notify_status": status,
		"updated_at":    time.Now(),
	}

	if status == NotifyStatusNotified {
		now := time.Now()
		updates["notified_at"] = &now
	}

	if notifyError != "" {
		updates["notify_error"] = notifyError
		updates["notify_retry_count"] = gorm.Expr("notify_retry_count + 1")
	}

	result := s.db.WithContext(ctx).
		Model(&Withdraw{}).
		Where("tx_hash = ?", txHash).
		Updates(updates)

	if result.Error != nil {
		return fmt.Errorf("failed to update withdraw notify status: %w", result.Error)
	}

	return nil
}

// UpdateDepositNotifyStatus updates the deposit notification status
func (s *PostgresStorage) UpdateDepositNotifyStatus(ctx context.Context, txHash, status string, notifyError string) error {
	updates := map[string]interface{}{
		"notify_status": status,
		"updated_at":    time.Now(),
	}

	if status == NotifyStatusNotified {
		now := time.Now()
		updates["notified_at"] = &now
	}

	if notifyError != "" {
		updates["notify_error"] = notifyError
		updates["notify_retry_count"] = gorm.Expr("notify_retry_count + 1")
	}

	result := s.db.WithContext(ctx).
		Model(&Deposit{}).
		Where("tx_hash = ?", txHash).
		Updates(updates)

	if result.Error != nil {
		return fmt.Errorf("failed to update deposit notify status: %w", result.Error)
	}

	return nil
}

// GetPendingWithdraws gets pending withdraw records
func (s *PostgresStorage) GetPendingWithdraws(ctx context.Context, limit int) ([]interface{}, error) {
	var withdraws []Withdraw
	result := s.db.WithContext(ctx).
		Where("notify_status = ? AND is_finalized = true", NotifyStatusPending).
		Order("created_at ASC").
		Limit(limit).
		Find(&withdraws)

	if result.Error != nil {
		return nil, fmt.Errorf("failed to get pending withdraws: %w", result.Error)
	}

	items := make([]interface{}, len(withdraws))
	for i := range withdraws {
		items[i] = &withdraws[i]
	}

	return items, nil
}

// GetPendingDeposits gets pending deposit records
func (s *PostgresStorage) GetPendingDeposits(ctx context.Context, limit int) ([]interface{}, error) {
	var deposits []Deposit
	result := s.db.WithContext(ctx).
		Where("notify_status = ? AND is_finalized = true", NotifyStatusPending).
		Order("created_at ASC").
		Limit(limit).
		Find(&deposits)

	if result.Error != nil {
		return nil, fmt.Errorf("failed to get pending deposits: %w", result.Error)
	}

	items := make([]interface{}, len(deposits))
	for i := range deposits {
		items[i] = &deposits[i]
	}

	return items, nil
}
