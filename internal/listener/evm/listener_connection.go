package evm

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	appTypes "github.com/Luckyx-Labs/chainscan-sync/internal/types"
	"github.com/Luckyx-Labs/chainscan-sync/pkg/logger"
	"github.com/Luckyx-Labs/chainscan-sync/pkg/utils"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
)

// connect connects to the Ethereum node with failover support and optional WebSocket
func (l *EVMListener) connect() error {
	var err error

	// Create failover RPC client
	l.failoverClient, err = NewFailoverRPCClient(l.ctx, l.config.RPCURL, l.config.BackupRPCURLs, l.config.RateLimit)
	if err != nil {
		return fmt.Errorf("failed to create failover RPC client: %w", err)
	}

	// Set compatibility references (pointing to the current active endpoint)
	l.client = l.failoverClient.GetClient()
	l.rpcClient = l.failoverClient.GetRPCClient()

	// Connect WebSocket (if configured)
	if l.config.WSURL != "" {
		l.wsClient, err = utils.RetryWithResult(l.ctx, l.retryConfig, func() (*ethclient.Client, error) {
			return ethclient.Dial(l.config.WSURL)
		}, fmt.Sprintf("connect to %s WebSocket", l.config.Name))

		if err != nil {
			logger.Warnf("WebSocket connection failed, will use polling mode: %v", err)
			l.wsClient = nil
		}
	}

	return nil
}

// listen listens for events
func (l *EVMListener) listen() {
	defer l.wg.Done()

	if l.wsClient != nil {
		l.listenWithWebSocket()
	} else {
		l.listenWithPolling()
	}
}

// listenWithWebSocket listens using WebSocket
func (l *EVMListener) listenWithWebSocket() {
	logger.Infof("Listening with WebSocket mode: %s", l.config.Name)

	// Build filter
	addresses := make([]common.Address, 0, len(l.contracts))
	for addr := range l.contracts {
		addresses = append(addresses, addr)
	}

	query := ethereum.FilterQuery{
		Addresses: addresses,
	}

	logs := make(chan types.Log)
	sub, err := l.wsClient.SubscribeFilterLogs(l.ctx, query, logs)
	if err != nil {
		l.errorChan <- fmt.Errorf("failed to subscribe to logs: %w", err)
		return
	}
	defer sub.Unsubscribe()

	for {
		select {
		case <-l.ctx.Done():
			logger.Infof("Stop listening %s", l.config.Name)
			return
		case err := <-sub.Err():
			l.errorChan <- fmt.Errorf("WebSocket subscription error: %w", err)
			return
		case vLog := <-logs:
			if err := l.processLog(vLog); err != nil {
				logger.Errorf("failed to process log: %v", err)
			}
		}
	}
}

// listenWithPolling listens using polling mode
// New strategy: immediately scan new blocks, confirmation updates are handled asynchronously by a separate confirmation.Updater
// 1. Immediately scan and store each new block as it is produced (Confirmations=0, IsFinalized=false)
// 2. Confirmation updates are handled uniformly by the confirmationUpdater in the manager (to avoid lock contention caused by repeated updates)
// 3. Once the confirmation count reaches the configured value, the block enters the pending notification state
func (l *EVMListener) listenWithPolling() {
	logger.Infof("Listening with polling mode: %s (immediate scan, confirmation updates handled by manager)", l.config.Name)

	// Get start block
	startBlock, err := l.getStartBlock()
	if err != nil {
		l.errorChan <- fmt.Errorf("failed to get start block: %w", err)
		return
	}

	currentBlock := startBlock
	// 1 second polling interval, quickly catch up with new blocks
	blockTicker := time.NewTicker(1 * time.Second)
	defer blockTicker.Stop()

	// Note: no longer starting updateConfirmationsLoop, confirmation updates are handled uniformly by the confirmationUpdater in the manager
	// This avoids lock contention and blocking caused by multiple updaters operating on the database simultaneously

	logger.Infof("[%s] Start scanning: starting block %d (immediate mode)", l.config.Name, startBlock)

	for {
		select {
		case <-l.ctx.Done():
			logger.Infof("Stop listening %s", l.config.Name)
			return
		case <-blockTicker.C:
			// Check if reorg is being processed
			l.reorgMu.Lock()
			if l.isReorging {
				logger.Debugf("[%s] Processing blockchain reorg, pause scanning new blocks...", l.config.Name)
				l.reorgMu.Unlock()
				continue
			}
			l.reorgMu.Unlock()

			// Check if monitor addresses are being reloaded
			l.reloadMu.Lock()
			if l.isReloadingAddresses {
				logger.Debugf("[%s] Reloading monitor addresses, pause scanning new blocks...", l.config.Name)
				l.reloadMu.Unlock()
				continue
			}
			l.reloadMu.Unlock()

			// Get latest block number on chain
			latestBlock, err := l.client.BlockNumber(l.ctx)
			if err != nil {
				logger.Errorf("Failed to get latest block: %v", err)
				continue
			}

			// Immediate scan: process all new blocks (do not wait for confirmations)
			// Only need to check currentBlock <= latestBlock
			if currentBlock > latestBlock {
				// Already caught up with the latest block on chain, wait for new blocks
				continue
			}

			// Batch process new blocks
			processedCount := 0
			for currentBlock <= latestBlock {
				// Current confirmations = latest block height on chain - current block number
				confirmations := latestBlock - currentBlock

				logger.Debugf("[%s] Immediate scan block %d (current confirmations: %d, chain latest: %d)",
					l.config.Name, currentBlock, confirmations, latestBlock)

				if err := l.processBlock(currentBlock); err != nil {
					logger.Debugf("Processing block error is %v", err)
					errMsg := err.Error()

					// Check if it's a reorg error (format: "reorg:rollback_block_number")
					if strings.Contains(errMsg, "reorg:") {
						idx := strings.Index(errMsg, "reorg:")
						if idx >= 0 {
							rollbackStr := errMsg[idx+6:]
							var numStr string
							for _, ch := range rollbackStr {
								if ch >= '0' && ch <= '9' {
									numStr += string(ch)
								} else {
									break
								}
							}

							rollbackBlock, parseErr := strconv.ParseUint(numStr, 10, 64)
							if parseErr != nil {
								logger.Errorf("Failed to parse reorg rollback block number: %v (original message: %s)", parseErr, errMsg)
								break
							}

							logger.Warnf("Reorg detected, rolled back to block %d", rollbackBlock)

							// Continue scanning from the rolled back block
							currentBlock = rollbackBlock + 1
							logger.Infof("Reorg processing completed, restart scanning from block %d", currentBlock)
							break
						}
					}

					logger.Errorf("Failed to process block %d: %v", currentBlock, err)
					// Other errors: do not advance currentBlock, retry in next loop
					break
				}

				// Only advance to the next block if processing was successful
				processedCount++
				currentBlock++

				// Update status
				l.updateStatus(currentBlock)
			}

			// Batch processing log
			if processedCount > 0 {
				logger.Infof("[%s] Immediately scanned %d blocks (current progress: %d, chain latest: %d)",
					l.config.Name, processedCount, currentBlock-1, latestBlock)
			}
		}
	}
}

// getStartBlock gets the starting block (supports resuming from a checkpoint)
func (l *EVMListener) getStartBlock() (uint64, error) {
	// If storage is available, try to load the last scanned block from the database first
	if l.storage != nil {
		lastBlock, _, err := l.storage.GetLastBlock(l.ctx, appTypes.ChainTypeEVM, l.config.Name)
		if err == nil && lastBlock > 0 {
			logger.Infof("Resume scanning from checkpoint: %s, block %d", l.config.Name, lastBlock)
			return lastBlock + 1, nil // Start from the next block
		}
	}

	// Get the starting block based on configuration
	if l.config.StartBlock == "latest" {
		return l.client.BlockNumber(l.ctx)
	} else if l.config.StartBlock == "earliest" {
		return 0, nil
	} else {
		return strconv.ParseUint(l.config.StartBlock, 10, 64)
	}
}
