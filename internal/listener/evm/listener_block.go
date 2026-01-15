package evm

import (
	"fmt"
	"math/big"
	"time"

	storage "github.com/Luckyx-Labs/chainscan-sync/internal/database"
	appTypes "github.com/Luckyx-Labs/chainscan-sync/internal/types"
	"github.com/Luckyx-Labs/chainscan-sync/pkg/logger"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// BlockInfo block information structure, used for saving block info
type BlockInfo struct {
	Number    uint64
	Hash      common.Hash
	Timestamp uint64
}

// processBlock processes a block (full version: parses transactions and events)
func (l *EVMListener) processBlock(blockNumber uint64) error {
	// Get block header information (used for reorg detection and saving block info)
	// Use HeaderByNumber instead of BlockByNumber to avoid parsing errors caused by unsupported new transaction types
	header, err := l.client.HeaderByNumber(l.ctx, big.NewInt(int64(blockNumber)))
	if err != nil {
		return fmt.Errorf("failed to get block header: %w", err)
	}

	// Detect blockchain reorg
	if blockNumber > 0 && l.storage != nil {
		isReorg, rollbackToBlock, err := l.detectReorg(header)
		if err != nil {
			// Failed to detect reorg (e.g., database error), return error
			return fmt.Errorf("failed to detect reorg: %w", err)
		}
		if isReorg {
			return fmt.Errorf("reorg:%d", rollbackToBlock)
		}
	}

	logger.Debugf("Processing block %d", blockNumber)

	// ========== Process native token (ETH) transfers (using standard RPC) ==========
	if l.storage != nil && l.bloomFilter != nil {
		if err := l.processNativeTransfersWithStandardRPC(blockNumber, header); err != nil {
			logger.Warnf("Standard RPC native transfer scan failed: %v", err)
		}
	}

	// ========== Process contract event logs ==========
	// Query contract event logs using FilterLogs
	// Use HeaderByNumber + FilterLogs to avoid parsing errors caused by unsupported new transaction types
	if len(l.contracts) > 0 {
		addresses := make([]common.Address, 0, len(l.contracts))
		for addr := range l.contracts {
			addresses = append(addresses, addr)
		}

		query := ethereum.FilterQuery{
			FromBlock: big.NewInt(int64(blockNumber)),
			ToBlock:   big.NewInt(int64(blockNumber)),
			Addresses: addresses,
		}

		logs, err := l.client.FilterLogs(l.ctx, query)
		if err != nil {
			return fmt.Errorf("failed to filter logs: %w", err)
		}

		logger.Debugf("Block %d: Found %d contract event logs", blockNumber, len(logs))

		// batch obtain all transactions in the block (reduce RPC calls)
		// Use map to cache obtained transactions to avoid fetching the same transaction multiple times
		txCache := make(map[common.Hash]*types.Transaction)
		txSenderCache := make(map[common.Hash]common.Address)

		// Process contract event logs
		// Note: Even if the native token transfers of the transaction have been processed, contract events still need to be processed
		// because contract events contain additional business logic and data (such as ETHDeposit events)
		for _, vLog := range logs {
			// First get the transaction from the cache
			tx, exists := txCache[vLog.TxHash]
			var from common.Address

			if !exists {
				// Cache miss, get transaction information
				var err error
				tx, _, err = l.client.TransactionByHash(l.ctx, vLog.TxHash)
				if err != nil {
					logger.Debugf("Failed to get transaction %s: %v", vLog.TxHash.Hex(), err)
					continue
				}
				txCache[vLog.TxHash] = tx
				from, _ = types.Sender(types.LatestSignerForChainID(new(big.Int).SetUint64(l.config.ChainID)), tx)
				txSenderCache[vLog.TxHash] = from
			} else {
				from = txSenderCache[vLog.TxHash]
			}

			// Parse event and check if it is related to monitored addresses
			contractInfo, contractOk := l.contracts[vLog.Address]
			var shouldProcess bool

			if contractOk && len(vLog.Topics) > 0 {
				// Try to parse event
				event, parseErr := l.parseEvent(vLog, contractInfo)
				if parseErr == nil && event != nil {
					logger.Debugf("Parsed event: %s, tx=%s, contract=%s",
						event.EventName, vLog.TxHash.Hex(), vLog.Address.Hex())

					// Successfully parsed event, check all address fields in event data
					shouldProcess = l.isEventRelatedToMonitoredAddresses(event)

					if shouldProcess {
						logger.Infof("Event %s related to monitored address: tx=%s, contract=%s",
							event.EventName, vLog.TxHash.Hex(), vLog.Address.Hex())
					} else {
						logger.Debugf("Event %s not related: tx=%s", event.EventName, vLog.TxHash.Hex())
					}
				} else {
					// Parsing failed, use transaction addresses to determine
					shouldProcess = l.shouldProcessTransaction(from, tx.To())
					logger.Warnf("Parse event failed: tx=%s, error=%v, fallback shouldProcess=%v",
						vLog.TxHash.Hex(), parseErr, shouldProcess)
				}
			} else {
				// Unable to parse, use transaction addresses to determine
				shouldProcess = l.shouldProcessTransaction(from, tx.To())
			}

			if shouldProcess {
				logger.Infof("Processing log: tx=%s, logIndex=%d", vLog.TxHash.Hex(), vLog.Index)
				if err := l.processLog(vLog); err != nil {
					logger.Errorf("Failed to process log: %v", err)
				} else {
					logger.Infof("Successfully processed log: tx=%s", vLog.TxHash.Hex())
				}
			} else {
				logger.Warnf("Skipping log: not related to monitored addresses, tx=%s, logIndex=%d",
					vLog.TxHash.Hex(), vLog.Index)
			}
		}
	}

	// ========== Save block information ==========
	// Strategy: Save all blocks to ensure blockchain continuity, which is crucial for reliable reorganization detection
	if l.storage != nil {
		pgStorage, ok := l.storage.(*storage.PostgresStorage)
		if ok {
			blockRecord := &storage.Block{
				ChainType:   string(appTypes.ChainTypeEVM),
				ChainName:   l.config.Name,
				BlockNumber: blockNumber,
				BlockHash:   header.Hash().Hex(),
				ParentHash:  header.ParentHash.Hex(),
				GasUsed:     header.GasUsed,
				GasLimit:    header.GasLimit,
				Timestamp:   time.Unix(int64(header.Time), 0),
				TxCount:     0, // Cannot get transaction count using HeaderByNumber
				Confirmed:   false,
			}

			if err := pgStorage.SaveBlock(l.ctx, blockRecord); err != nil {
				logger.Errorf("Failed to save block %d: %v", blockNumber, err)
			} else {
				logger.Debugf("Saved block %d", blockNumber)
			}
		}
	}
	return nil
}
