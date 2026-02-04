package confirmation

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/Luckyx-Labs/chainscan-sync/pkg/logger"
	"github.com/ethereum/go-ethereum/ethclient"
)

type Checker struct {
	chainName             string
	client                *ethclient.Client
	requiredConfirmations uint64
	ctx                   context.Context
}

func NewChecker(chainName string, client *ethclient.Client, requiredConfirmations uint64) *Checker {
	return &Checker{
		chainName:             chainName,
		client:                client,
		requiredConfirmations: requiredConfirmations,
		ctx:                   context.Background(),
	}
}

// CheckBlockConfirmations checks the number of confirmations for a given block
func (c *Checker) CheckBlockConfirmations(blockNumber uint64) (uint64, bool, error) {
	// Get the current latest block
	currentBlock, err := c.client.BlockNumber(c.ctx)
	if err != nil {
		return 0, false, fmt.Errorf("failed to get current block: %w", err)
	}

	// Calculate confirmations
	if currentBlock < blockNumber {
		return 0, false, nil
	}

	confirmations := currentBlock - blockNumber

	// Check if the required number of confirmations is reached
	isFinalized := confirmations >= c.requiredConfirmations

	return confirmations, isFinalized, nil
}

// VerifyBlockNotReorged verifies if a block has been reorged
func (c *Checker) VerifyBlockNotReorged(blockNumber uint64, expectedBlockHash string) (bool, error) {
	header, err := c.client.HeaderByNumber(c.ctx, big.NewInt(int64(blockNumber)))
	if err != nil {
		return false, fmt.Errorf("failed to get block header: %w", err)
	}

	currentHash := header.Hash().Hex()
	if currentHash != expectedBlockHash {
		logger.Warnf("[%s] Blockchain reorg detected! Block %d, expected hash: %s, current hash: %s",
			c.chainName, blockNumber, expectedBlockHash, currentHash)
		return false, nil
	}

	return true, nil
}

// WaitForConfirmations waits for the specified number of confirmations (blocking)
func (c *Checker) WaitForConfirmations(blockNumber uint64, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout waiting for block %d to reach %d confirmations", blockNumber, c.requiredConfirmations)
		}

		confirmations, isFinalized, err := c.CheckBlockConfirmations(blockNumber)
		if err != nil {
			return err
		}

		if isFinalized {
			logger.Infof("[%s] Block %d has reached %d confirmations", c.chainName, blockNumber, confirmations)
			return nil
		}

		logger.Debugf("[%s] Block %d current confirmations: %d/%d, waiting...",
			c.chainName, blockNumber, confirmations, c.requiredConfirmations)

		// Wait for one block time
		time.Sleep(2 * time.Second)
	}
}

// GetSafeBlockNumber gets the safe block number (current height - confirmations)
func (c *Checker) GetSafeBlockNumber() (uint64, error) {
	currentBlock, err := c.client.BlockNumber(c.ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to get current block: %w", err)
	}

	if currentBlock < c.requiredConfirmations {
		return 0, nil
	}

	safeBlock := currentBlock - c.requiredConfirmations
	return safeBlock, nil
}

// GetCurrentBlockNumber gets the current block number from the chain
func (c *Checker) GetCurrentBlockNumber() (uint64, error) {
	currentBlock, err := c.client.BlockNumber(c.ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to get current block: %w", err)
	}
	return currentBlock, nil
}

// IsBlockFinalized checks if a block has been finalized
func (c *Checker) IsBlockFinalized(blockNumber uint64) (bool, error) {
	_, isFinalized, err := c.CheckBlockConfirmations(blockNumber)
	return isFinalized, err
}

// GetConfirmationStatus gets the confirmation status of a block
type ConfirmationStatus struct {
	BlockNumber       uint64
	CurrentBlock      uint64
	Confirmations     uint64
	RequiredConfirms  uint64
	IsFinalized       bool
	BlockHash         string
	IsReorged         bool
	EstimatedWaitTime time.Duration
}

func (c *Checker) GetConfirmationStatus(blockNumber uint64, blockHash string) (*ConfirmationStatus, error) {
	currentBlock, err := c.client.BlockNumber(c.ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get current block: %w", err)
	}

	confirmations := uint64(0)
	if currentBlock >= blockNumber {
		confirmations = currentBlock - blockNumber
	}

	isFinalized := confirmations >= c.requiredConfirmations

	// Check if the block has been reorged
	isReorged := false
	if blockHash != "" {
		notReorged, err := c.VerifyBlockNotReorged(blockNumber, blockHash)
		if err != nil {
			logger.Errorf("Failed to verify block hash: %v", err)
		} else {
			isReorged = !notReorged
		}
	}

	// Estimate wait time (assuming 2 seconds per block)
	estimatedWaitTime := time.Duration(0)
	if !isFinalized {
		remainingBlocks := c.requiredConfirmations - confirmations
		estimatedWaitTime = time.Duration(remainingBlocks) * 2 * time.Second
	}

	return &ConfirmationStatus{
		BlockNumber:       blockNumber,
		CurrentBlock:      currentBlock,
		Confirmations:     confirmations,
		RequiredConfirms:  c.requiredConfirmations,
		IsFinalized:       isFinalized,
		BlockHash:         blockHash,
		IsReorged:         isReorged,
		EstimatedWaitTime: estimatedWaitTime,
	}, nil
}
