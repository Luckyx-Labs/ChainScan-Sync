package evm

import (
	"fmt"
	"math/big"

	storage "github.com/Luckyx-Labs/chainscan-sync/internal/database"
	appTypes "github.com/Luckyx-Labs/chainscan-sync/internal/types"
	"github.com/Luckyx-Labs/chainscan-sync/pkg/logger"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
)

// getERC20Balance get ERC20 token balance
func (l *EVMListener) getERC20Balance(tokenAddress, holderAddress string, blockNumber uint64) (string, error) {
	// ERC20 balanceOf(address) method signature
	// 0x70a08231 = keccak256("balanceOf(address)")[:4]
	methodID := []byte{0x70, 0xa0, 0x82, 0x31}

	// Encode parameter (holder address)
	holder := common.HexToAddress(holderAddress)
	paddedAddress := common.LeftPadBytes(holder.Bytes(), 32)

	// Combine call data
	data := append(methodID, paddedAddress...)

	// Call contract
	token := common.HexToAddress(tokenAddress)
	msg := ethereum.CallMsg{
		To:   &token,
		Data: data,
	}

	logger.Debugf("Query ERC20 balance: token=%s, holder=%s, block=%d", tokenAddress, holderAddress, blockNumber)

	result, err := l.client.CallContract(l.ctx, msg, big.NewInt(int64(blockNumber)))
	if err != nil {
		logger.Warnf("Failed to call balanceOf: token=%s, holder=%s, block=%d, error=%v",
			tokenAddress, holderAddress, blockNumber, err)
		return "", fmt.Errorf("failed to call balanceOf: %w", err)
	}

	// Parse return value (uint256)
	balance := new(big.Int).SetBytes(result)
	logger.Debugf("ERC20 balance query successful: %s", balance.String())
	return balance.String(), nil
}

// updateBalanceFromEvent update balance based on event
func (l *EVMListener) updateBalanceFromEvent(address string, tokenAddress string, blockNumber uint64, txHash string) error {
	pgStorage, ok := l.storage.(*storage.PostgresStorage)
	if !ok {
		return nil
	}

	var balance string
	var err error

	// Distinguish between native token and ERC20
	if tokenAddress == "0x0" || tokenAddress == "" {
		// Native token
		bal, err := l.client.BalanceAt(l.ctx, common.HexToAddress(address), big.NewInt(int64(blockNumber)))
		if err != nil {
			return fmt.Errorf("failed to get native token balance: %w", err)
		}
		balance = bal.String()
		tokenAddress = "0x0"
	} else {
		// ERC20 token
		balance, err = l.getERC20Balance(tokenAddress, address, blockNumber)
		if err != nil {
			return fmt.Errorf("failed to get ERC20 balance: %w", err)
		}
	}

	// Update balance (will update both address_balances and balance_histories tables)
	if err := pgStorage.UpdateAddressBalance(
		l.ctx,
		string(appTypes.ChainTypeEVM),
		l.config.Name,
		address,
		tokenAddress,
		balance,
		blockNumber,
		txHash,
	); err != nil {
		return fmt.Errorf("failed to update address balance: %w", err)
	}

	return nil
}
