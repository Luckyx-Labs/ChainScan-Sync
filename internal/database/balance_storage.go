package database

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"gorm.io/gorm"

	appLogger "github.com/Luckyx-Labs/chainscan-sync/pkg/logger"
)

// SaveOrUpdateBalance saves or updates the address balance
func (s *PostgresStorage) SaveOrUpdateBalance(ctx context.Context, balance *AddressBalance) error {
	result := s.db.WithContext(ctx).
		Where("chain_type = ? AND chain_name = ? AND address = ? AND token_address = ?",
			balance.ChainType, balance.ChainName, balance.Address, balance.TokenAddress).
		Assign(map[string]interface{}{
			"balance":         balance.Balance,
			"block_number":    balance.BlockNumber,
			"last_tx_hash":    balance.LastTxHash,
			"last_updated_at": balance.LastUpdatedAt,
		}).
		FirstOrCreate(balance)

	if result.Error != nil {
		return fmt.Errorf("failed to save or update address balance: %w", result.Error)
	}

	return nil
}

// GetBalance gets the address balance
func (s *PostgresStorage) GetBalance(ctx context.Context, chainType, chainName, address, tokenAddress string) (*AddressBalance, error) {
	var balance AddressBalance
	result := s.db.WithContext(ctx).
		Where("chain_type = ? AND chain_name = ? AND address = ? AND token_address = ?",
			chainType, chainName, address, tokenAddress).
		First(&balance)

	if result.Error == gorm.ErrRecordNotFound {
		return nil, nil // balance not found
	}
	if result.Error != nil {
		return nil, fmt.Errorf("failed to get address balance: %w", result.Error)
	}

	return &balance, nil
}

// GetAddressBalances gets all token balances for an address
func (s *PostgresStorage) GetAddressBalances(ctx context.Context, chainType, chainName, address string) ([]*AddressBalance, error) {
	var balances []*AddressBalance
	result := s.db.WithContext(ctx).
		Where("chain_type = ? AND chain_name = ? AND address = ?", chainType, chainName, address).
		Find(&balances)

	if result.Error != nil {
		return nil, fmt.Errorf("failed to query address balances: %w", result.Error)
	}

	return balances, nil
}

// SaveBalanceHistory saves balance change history
func (s *PostgresStorage) SaveBalanceHistory(ctx context.Context, history *BalanceHistory) error {
	result := s.db.WithContext(ctx).Create(history)
	if result.Error != nil {
		return fmt.Errorf("failed to save balance history: %w", result.Error)
	}
	return nil
}

// GetBalanceHistory gets balance change history
func (s *PostgresStorage) GetBalanceHistory(ctx context.Context, chainType, chainName, address, tokenAddress string, limit int) ([]*BalanceHistory, error) {
	var histories []*BalanceHistory
	result := s.db.WithContext(ctx).
		Where("chain_type = ? AND chain_name = ? AND address = ? AND token_address = ?",
			chainType, chainName, address, tokenAddress).
		Order("timestamp DESC").
		Limit(limit).
		Find(&histories)

	if result.Error != nil {
		return nil, fmt.Errorf("failed to query balance history: %w", result.Error)
	}

	return histories, nil
}

// GetAddressBalance gets the address balance
func (s *PostgresStorage) GetAddressBalance(ctx context.Context, chainType, chainName, address, tokenAddress string) (string, error) {
	var balance AddressBalance

	result := s.db.WithContext(ctx).
		Where("chain_type = ? AND chain_name = ? AND address = ? AND token_address = ?",
			chainType, chainName, address, tokenAddress).
		First(&balance)

	if result.Error == gorm.ErrRecordNotFound {
		return "0", nil // no record found, return 0
	}
	if result.Error != nil {
		return "", fmt.Errorf("failed to get address balance: %w", result.Error)
	}

	return balance.Balance, nil
}

// UpdateAddressBalance updates the address balance
func (s *PostgresStorage) UpdateAddressBalance(ctx context.Context,
	chainType, chainName, address, tokenAddress string,
	newBalance string, blockNumber uint64, txHash string) error {

	// Get old balance
	oldBalance, err := s.GetAddressBalance(ctx, chainType, chainName, address, tokenAddress)
	if err != nil {
		return fmt.Errorf("failed to get old balance: %w", err)
	}

	// Calculate balance change
	balanceChange, changeType := s.calculateBalanceChange(oldBalance, newBalance)

	// Begin transaction
	tx := s.db.WithContext(ctx).Begin()
	if tx.Error != nil {
		return fmt.Errorf("failed to begin transaction: %w", tx.Error)
	}

	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// Save balance history
	history := BalanceHistory{
		ChainType:     chainType,
		ChainName:     chainName,
		Address:       address,
		TokenAddress:  tokenAddress,
		BlockNumber:   blockNumber,
		TxHash:        txHash,
		BalanceBefore: oldBalance,
		BalanceAfter:  newBalance,
		BalanceChange: balanceChange,
		ChangeType:    changeType,
		Timestamp:     time.Now(),
	}

	if err := tx.Create(&history).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to save balance history: %w", err)
	}

	// Update or create the current balance record
	addressBalance := AddressBalance{
		ChainType:     chainType,
		ChainName:     chainName,
		Address:       address,
		TokenAddress:  tokenAddress,
		Balance:       newBalance,
		BlockNumber:   blockNumber,
		LastTxHash:    txHash,
		LastUpdatedAt: time.Now(),
	}

	result := tx.Where("chain_type = ? AND chain_name = ? AND address = ? AND token_address = ?",
		chainType, chainName, address, tokenAddress).
		Assign(map[string]interface{}{
			"balance":         newBalance,
			"block_number":    blockNumber,
			"last_tx_hash":    txHash,
			"last_updated_at": time.Now(),
		}).
		FirstOrCreate(&addressBalance)

	if result.Error != nil {
		tx.Rollback()
		return fmt.Errorf("failed to update balance: %w", result.Error)
	}

	// Commit transaction
	if err := tx.Commit().Error; err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	appLogger.Debugf("Updated balance: %s/%s %s [%s] %s -> %s",
		chainType, chainName, address, tokenAddress, oldBalance, newBalance)

	return nil
}

// calculateBalanceChange calculates the balance change using big.Int
func (s *PostgresStorage) calculateBalanceChange(oldBalance, newBalance string) (string, string) {
	oldBig := new(big.Int)
	newBig := new(big.Int)

	oldBig.SetString(oldBalance, 10)
	newBig.SetString(newBalance, 10)

	diff := new(big.Int).Sub(newBig, oldBig)

	if diff.Sign() > 0 {
		return diff.String(), "increase"
	} else if diff.Sign() < 0 {
		return new(big.Int).Abs(diff).String(), "decrease"
	}
	return "0", "no_change"
}

// GetAllAddressBalances gets all monitored address balances
func (s *PostgresStorage) GetAllAddressBalances(ctx context.Context,
	chainType, chainName string) ([]*AddressBalance, error) {

	var balances []*AddressBalance

	result := s.db.WithContext(ctx).
		Where("chain_type = ? AND chain_name = ?", chainType, chainName).
		Order("last_updated_at DESC").
		Find(&balances)

	if result.Error != nil {
		return nil, fmt.Errorf("failed to get all address balances: %w", result.Error)
	}

	return balances, nil
}
