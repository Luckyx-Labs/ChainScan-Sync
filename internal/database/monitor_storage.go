package database

import (
	"context"
	"fmt"
)

// GetAllMonitorAddresses gets all monitored addresses (from the account database)
func (s *PostgresStorage) GetAllMonitorAddresses(ctx context.Context) ([]string, error) {
	var addresses []WebUserChainAddress
	result := s.accountDB.WithContext(ctx).Find(&addresses)

	if result.Error != nil {
		return nil, fmt.Errorf("failed to get monitor addresses: %w", result.Error)
	}

	addressList := make([]string, len(addresses))
	for i, addr := range addresses {
		addressList[i] = addr.Address
	}

	return addressList, nil
}

// IsAddressMonitored checks if an address is monitored (from the account database)
func (s *PostgresStorage) IsAddressMonitored(ctx context.Context, address string) (bool, error) {
	var count int64
	result := s.accountDB.WithContext(ctx).
		Model(&WebUserChainAddress{}).
		Where("LOWER(address) = LOWER(?)", address).
		Count(&count)

	if result.Error != nil {
		return false, fmt.Errorf("failed to check if address is monitored: %w", result.Error)
	}

	return count > 0, nil
}
