package database

import (
	"context"
	"fmt"
)

// QueryBlocks query block list
func (s *PostgresStorage) QueryBlocks(ctx context.Context, params map[string]interface{}, page, pageSize int) ([]interface{}, int64, error) {
	var blocks []Block
	var total int64

	query := s.db.WithContext(ctx).Model(&Block{})

	// Apply filter conditions
	if chainName, ok := params["chain_name"].(string); ok && chainName != "" {
		query = query.Where("chain_name = ?", chainName)
	}
	if fromBlock, ok := params["from_block"].(uint64); ok && fromBlock > 0 {
		query = query.Where("block_number >= ?", fromBlock)
	}
	if toBlock, ok := params["to_block"].(uint64); ok && toBlock > 0 {
		query = query.Where("block_number <= ?", toBlock)
	}
	if startTime, ok := params["start_time"].(int64); ok && startTime > 0 {
		query = query.Where("timestamp >= ?", startTime)
	}
	if endTime, ok := params["end_time"].(int64); ok && endTime > 0 {
		query = query.Where("timestamp <= ?", endTime)
	}
	if isReorged, ok := params["is_reorged"].(bool); ok {
		query = query.Where("is_reorged = ?", isReorged)
	}
	if isProcessed, ok := params["is_processed"].(bool); ok {
		query = query.Where("is_processed = ?", isProcessed)
	}

	// Get total count
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("failed to count blocks: %w", err)
	}

	// Paginated query
	offset := (page - 1) * pageSize
	if err := query.Order("block_number DESC").Limit(pageSize).Offset(offset).Find(&blocks).Error; err != nil {
		return nil, 0, fmt.Errorf("failed to query block list: %w", err)
	}

	// Convert to interface{} slice
	result := make([]interface{}, len(blocks))
	for i := range blocks {
		result[i] = blocks[i]
	}

	return result, total, nil
}

// QueryTransactions query transaction list
func (s *PostgresStorage) QueryTransactions(ctx context.Context, params map[string]interface{}, page, pageSize int) ([]interface{}, int64, error) {
	var transactions []Transaction
	var total int64

	query := s.db.WithContext(ctx).Model(&Transaction{})

	// Apply filter conditions
	if chainName, ok := params["chain_name"].(string); ok && chainName != "" {
		query = query.Where("chain_name = ?", chainName)
	}
	if blockNumber, ok := params["block_number"].(uint64); ok && blockNumber > 0 {
		query = query.Where("block_number = ?", blockNumber)
	}
	if fromAddress, ok := params["from_address"].(string); ok && fromAddress != "" {
		query = query.Where("from_address = ?", fromAddress)
	}
	if toAddress, ok := params["to_address"].(string); ok && toAddress != "" {
		query = query.Where("to_address = ?", toAddress)
	}
	if address, ok := params["address"].(string); ok && address != "" {
		query = query.Where("from_address = ? OR to_address = ?", address, address)
	}
	if status, ok := params["status"].(string); ok && status != "" {
		query = query.Where("status = ?", status)
	}
	if startTime, ok := params["start_time"].(int64); ok && startTime > 0 {
		query = query.Where("timestamp >= ?", startTime)
	}
	if endTime, ok := params["end_time"].(int64); ok && endTime > 0 {
		query = query.Where("timestamp <= ?", endTime)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("failed to count transactions: %w", err)
	}

	offset := (page - 1) * pageSize
	if err := query.Order("block_number DESC, tx_index DESC").Limit(pageSize).Offset(offset).Find(&transactions).Error; err != nil {
		return nil, 0, fmt.Errorf("failed to query transaction list: %w", err)
	}

	result := make([]interface{}, len(transactions))
	for i := range transactions {
		result[i] = transactions[i]
	}

	return result, total, nil
}

// QueryEvents query event list
func (s *PostgresStorage) QueryEvents(ctx context.Context, params map[string]interface{}, page, pageSize int) ([]interface{}, int64, error) {
	var events []Event
	var total int64

	query := s.db.WithContext(ctx).Model(&Event{})

	// Apply filter conditions
	if chainName, ok := params["chain_name"].(string); ok && chainName != "" {
		query = query.Where("chain_name = ?", chainName)
	}
	if contractAddress, ok := params["contract_address"].(string); ok && contractAddress != "" {
		query = query.Where("contract_address = ?", contractAddress)
	}
	if eventName, ok := params["event_name"].(string); ok && eventName != "" {
		query = query.Where("event_name = ?", eventName)
	}
	if address, ok := params["address"].(string); ok && address != "" {
		query = query.Where("event_data LIKE ?", "%"+address+"%")
	}
	if blockNumber, ok := params["block_number"].(uint64); ok && blockNumber > 0 {
		query = query.Where("block_number = ?", blockNumber)
	}
	if startTime, ok := params["start_time"].(int64); ok && startTime > 0 {
		query = query.Where("timestamp >= ?", startTime)
	}
	if endTime, ok := params["end_time"].(int64); ok && endTime > 0 {
		query = query.Where("timestamp <= ?", endTime)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("failed to count events: %w", err)
	}

	offset := (page - 1) * pageSize
	if err := query.Order("block_number DESC, log_index DESC").Limit(pageSize).Offset(offset).Find(&events).Error; err != nil {
		return nil, 0, fmt.Errorf("failed to query event list: %w", err)
	}

	result := make([]interface{}, len(events))
	for i := range events {
		result[i] = events[i]
	}

	return result, total, nil
}

// QueryAddresses query address list
func (s *PostgresStorage) QueryAddresses(ctx context.Context, params map[string]interface{}, page, pageSize int) ([]interface{}, int64, error) {
	var addresses []WebUserChainAddress
	var total int64

	query := s.db.WithContext(ctx).Model(&WebUserChainAddress{})

	// Apply filter conditions
	if address, ok := params["address"].(string); ok && address != "" {
		query = query.Where("address = ?", address)
	}
	if userID, ok := params["user_id"].(int); ok && userID > 0 {
		query = query.Where("user_id = ?", userID)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("failed to count addresses: %w", err)
	}

	offset := (page - 1) * pageSize
	if err := query.Order("created_at DESC").Limit(pageSize).Offset(offset).Find(&addresses).Error; err != nil {
		return nil, 0, fmt.Errorf("failed to query address list: %w", err)
	}

	result := make([]interface{}, len(addresses))
	for i := range addresses {
		result[i] = addresses[i]
	}

	return result, total, nil
}

// QueryBalances query balance list
func (s *PostgresStorage) QueryBalances(ctx context.Context, params map[string]interface{}, page, pageSize int) ([]interface{}, int64, error) {
	var balances []AddressBalance
	var total int64

	query := s.db.WithContext(ctx).Model(&AddressBalance{})

	// Apply filter conditions
	if chainName, ok := params["chain_name"].(string); ok && chainName != "" {
		query = query.Where("chain_name = ?", chainName)
	}
	if address, ok := params["address"].(string); ok && address != "" {
		query = query.Where("address = ?", address)
	}
	if tokenAddress, ok := params["token_address"].(string); ok && tokenAddress != "" {
		query = query.Where("token_address = ?", tokenAddress)
	}
	if minBalance, ok := params["min_balance"].(string); ok && minBalance != "" {
		query = query.Where("balance >= ?", minBalance)
	}
	if maxBalance, ok := params["max_balance"].(string); ok && maxBalance != "" {
		query = query.Where("balance <= ?", maxBalance)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("failed to count balances: %w", err)
	}

	offset := (page - 1) * pageSize
	if err := query.Order("last_updated_at DESC").Limit(pageSize).Offset(offset).Find(&balances).Error; err != nil {
		return nil, 0, fmt.Errorf("failed to query balance list: %w", err)
	}

	result := make([]interface{}, len(balances))
	for i := range balances {
		result[i] = balances[i]
	}

	return result, total, nil
}

// QueryBalanceHistory query balance history
func (s *PostgresStorage) QueryBalanceHistory(ctx context.Context, address, tokenAddress string, params map[string]interface{}, page, pageSize int) ([]interface{}, int64, error) {
	var history []BalanceHistory
	var total int64

	query := s.db.WithContext(ctx).Model(&BalanceHistory{}).
		Where("address = ? AND token_address = ?", address, tokenAddress)

	// Apply filter conditions
	if startTime, ok := params["start_time"].(int64); ok && startTime > 0 {
		query = query.Where("timestamp >= ?", startTime)
	}
	if endTime, ok := params["end_time"].(int64); ok && endTime > 0 {
		query = query.Where("timestamp <= ?", endTime)
	}
	if txHash, ok := params["tx_hash"].(string); ok && txHash != "" {
		query = query.Where("tx_hash = ?", txHash)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("failed to count balance history: %w", err)
	}

	offset := (page - 1) * pageSize
	if err := query.Order("block_number DESC, created_at DESC").Limit(pageSize).Offset(offset).Find(&history).Error; err != nil {
		return nil, 0, fmt.Errorf("failed to query balance history: %w", err)
	}

	result := make([]interface{}, len(history))
	for i := range history {
		result[i] = history[i]
	}

	return result, total, nil
}

// GetBlockByNumber get block by number
func (s *PostgresStorage) GetBlockByNumber(ctx context.Context, chainName string, blockNumber uint64) (interface{}, error) {
	var block Block

	result := s.db.WithContext(ctx).
		Where("chain_name = ? AND block_number = ?", chainName, blockNumber).
		First(&block)

	if result.Error != nil {
		return nil, fmt.Errorf("failed to get block: %w", result.Error)
	}

	return block, nil
}

// GetTransactionByHash get transaction by txhash
func (s *PostgresStorage) GetTransactionByHash(ctx context.Context, txHash string) (interface{}, error) {
	var transaction Transaction

	result := s.db.WithContext(ctx).
		Where("tx_hash = ?", txHash).
		First(&transaction)

	if result.Error != nil {
		return nil, fmt.Errorf("failed to get transaction: %w", result.Error)
	}

	return transaction, nil
}

// GetAddressInfo get address info
func (s *PostgresStorage) GetAddressInfo(ctx context.Context, chainName, address string) (interface{}, error) {
	var monitorAddr WebUserChainAddress

	result := s.db.WithContext(ctx).
		Where("address = ?", address).
		First(&monitorAddr)

	if result.Error != nil {
		return nil, fmt.Errorf("failed to get address info: %w", result.Error)
	}

	return monitorAddr, nil
}

// GetStatistics get statistics
func (s *PostgresStorage) GetStatistics(ctx context.Context) (map[string]interface{}, error) {
	stats := make(map[string]interface{})

	// Count blocks
	var blockCount int64
	if err := s.db.WithContext(ctx).Model(&Block{}).Count(&blockCount).Error; err != nil {
		return nil, fmt.Errorf("failed to count blocks: %w", err)
	}
	stats["total_blocks"] = blockCount

	// Count transactions
	var txCount int64
	if err := s.db.WithContext(ctx).Model(&Transaction{}).Count(&txCount).Error; err != nil {
		return nil, fmt.Errorf("failed to count transactions: %w", err)
	}
	stats["total_transactions"] = txCount

	// Count events
	var eventCount int64
	if err := s.db.WithContext(ctx).Model(&Event{}).Count(&eventCount).Error; err != nil {
		return nil, fmt.Errorf("failed to count events: %w", err)
	}
	stats["total_events"] = eventCount

	// Count addresses
	var addressCount int64
	if err := s.db.WithContext(ctx).Model(&WebUserChainAddress{}).Count(&addressCount).Error; err != nil {
		return nil, fmt.Errorf("failed to count addresses: %w", err)
	}
	stats["total_addresses"] = addressCount

	// Count chains
	var chainCount int64
	if err := s.db.WithContext(ctx).Model(&Block{}).Distinct("chain_name").Count(&chainCount).Error; err != nil {
		return nil, fmt.Errorf("failed to count chains: %w", err)
	}
	stats["total_chains"] = int(chainCount)
	stats["active_chains"] = int(chainCount) // Simplified handling

	// Count monitored tokens
	var tokenCount int64
	if err := s.db.WithContext(ctx).Model(&AddressBalance{}).Distinct("token_address").Count(&tokenCount).Error; err != nil {
		return nil, fmt.Errorf("failed to count tokens: %w", err)
	}
	stats["monitored_tokens"] = tokenCount

	return stats, nil
}

// GetChainStatistics get chain statistics
func (s *PostgresStorage) GetChainStatistics(ctx context.Context, chainName string) (map[string]interface{}, error) {
	stats := make(map[string]interface{})

	// Get latest block
	var latestBlock Block
	if err := s.db.WithContext(ctx).
		Where("chain_name = ?", chainName).
		Order("block_number DESC").
		First(&latestBlock).Error; err == nil {
		stats["latest_block"] = latestBlock.BlockNumber
		stats["last_processed_at"] = latestBlock.Timestamp
	}

	// Count blocks
	var blockCount int64
	if err := s.db.WithContext(ctx).Model(&Block{}).Where("chain_name = ?", chainName).Count(&blockCount).Error; err != nil {
		return nil, fmt.Errorf("failed to count blocks: %w", err)
	}
	stats["block_count"] = blockCount

	// Count transactions
	var txCount int64
	if err := s.db.WithContext(ctx).Model(&Transaction{}).Where("chain_name = ?", chainName).Count(&txCount).Error; err != nil {
		return nil, fmt.Errorf("failed to count transactions: %w", err)
	}
	stats["transaction_count"] = txCount

	// Count events
	var eventCount int64
	if err := s.db.WithContext(ctx).Model(&Event{}).Where("chain_name = ?", chainName).Count(&eventCount).Error; err != nil {
		return nil, fmt.Errorf("failed to count events: %w", err)
	}
	stats["event_count"] = eventCount

	// Count addresses (cross-database query on account.web_user_chain_address table, no chain name filtering)
	var addressCount int64
	if err := s.db.WithContext(ctx).Model(&WebUserChainAddress{}).Count(&addressCount).Error; err != nil {
		return nil, fmt.Errorf("failed to count addresses: %w", err)
	}
	stats["address_count"] = addressCount

	// Count reorgs
	var reorgCount int64
	if err := s.db.WithContext(ctx).Model(&Block{}).Where("chain_name = ? AND is_reorged = ?", chainName, true).Count(&reorgCount).Error; err != nil {
		return nil, fmt.Errorf("failed to count reorgs: %w", err)
	}
	stats["reorg_count"] = reorgCount

	// Get last reorg time
	var lastReorgBlock Block
	if err := s.db.WithContext(ctx).
		Where("chain_name = ? AND is_reorged = ?", chainName, true).
		Order("block_number DESC").
		First(&lastReorgBlock).Error; err == nil {
		stats["last_reorg_at"] = lastReorgBlock.Timestamp
	}

	// Calculate average block time (simplified calculation: take the most recent 100 blocks)
	var blocks []Block
	if err := s.db.WithContext(ctx).
		Where("chain_name = ?", chainName).
		Order("block_number DESC").
		Limit(100).
		Find(&blocks).Error; err == nil && len(blocks) > 1 {
		timeDiff := blocks[0].Timestamp.Unix() - blocks[len(blocks)-1].Timestamp.Unix()
		blockDiff := blocks[0].BlockNumber - blocks[len(blocks)-1].BlockNumber
		if blockDiff > 0 {
			stats["average_block_time"] = timeDiff / int64(blockDiff)
		}
	}

	return stats, nil
}
