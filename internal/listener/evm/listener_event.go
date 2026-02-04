package evm

import (
	"encoding/json"
	"fmt"
	"math/big"
	"time"

	storage "github.com/Luckyx-Labs/chainscan-sync/internal/database"
	appTypes "github.com/Luckyx-Labs/chainscan-sync/internal/types"
	"github.com/Luckyx-Labs/chainscan-sync/pkg/logger"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// processLog Process logs (including complete deposit and withdrawal detection and balance updates)
func (l *EVMListener) processLog(vLog types.Log) error {
	contractInfo, ok := l.contracts[vLog.Address]
	if !ok {
		return nil
	}

	// Parse event
	event, err := l.parseEvent(vLog, contractInfo)
	if err != nil {
		return fmt.Errorf("failed to parse event: %w", err)
	}

	if event != nil {
		// Special handling: Transfer event
		// Priority 1: If from or to is a monitored contract address, skip (corresponding business event exists)
		// Priority 2: If involving monitored addresses, save directly (regardless of contract)
		// Priority 3: If not involving monitored addresses, only save EOA <-> EOA transfers
		if event.EventName == "Transfer" {
			fromAddr, fromOk := event.EventData["from"].(common.Address)
			toAddr, toOk := event.EventData["to"].(common.Address)

			if fromOk && toOk {
				// Priority 1: Check if it involves monitored contract addresses (e.g., AssetPoolManager)
				// If so, it means there are corresponding business events (e.g., BatchPayoutItem), skip Transfer
				fromIsMonitoredContract := l.isMonitoredContract(fromAddr)
				toIsMonitoredContract := l.isMonitoredContract(toAddr)

				if fromIsMonitoredContract || toIsMonitoredContract {
					logger.Debugf("Skip Transfer event: from=%s (monitored_contract=%v) or to=%s (monitored_contract=%v), use business event instead",
						fromAddr.Hex(), fromIsMonitoredContract, toAddr.Hex(), toIsMonitoredContract)
					return nil
				}

				// Priority 2: Check if it involves monitored addresses
				fromMonitored := l.isAddressMonitored(fromAddr)
				toMonitored := l.isAddressMonitored(toAddr)

				if !fromMonitored && !toMonitored {
					// Priority 3: If not involving monitored addresses, check if both are EOAs
					// Use isContractAddress method (supports EIP-7702 recognition)
					fromIsContract := l.isContractAddress(fromAddr)
					toIsContract := l.isContractAddress(toAddr)

					if fromIsContract || toIsContract {
						logger.Debugf("Skip Transfer event (no monitored address): from=%s (contract=%v), to=%s (contract=%v)",
							fromAddr.Hex(), fromIsContract, toAddr.Hex(), toIsContract)
						return nil
					}
				} else {
					// Involving monitored addresses, save directly
					logger.Debugf("Saving Transfer event with monitored address: from=%s (monitored=%v), to=%s (monitored=%v)",
						fromAddr.Hex(), fromMonitored, toAddr.Hex(), toMonitored)
				}
			}
		}

		// 1. Check if it is a deposit or withdrawal related event, requires additional processing
		if l.storage != nil {
			// Get complete transaction information
			tx, isPending, err := l.client.TransactionByHash(l.ctx, vLog.TxHash)
			if err != nil {
				logger.Warnf("Failed to get transaction %s: %v", vLog.TxHash.Hex(), err)
				return nil
			}
			if isPending {
				logger.Debugf("Transaction %s still pending, skipping", vLog.TxHash.Hex())
				return nil
			}

			// Get transaction receipt
			receipt, err := l.client.TransactionReceipt(l.ctx, vLog.TxHash)
			if err != nil {
				logger.Warnf("Failed to get transaction receipt %s: %v", vLog.TxHash.Hex(), err)
				return nil
			}

			// Get block header (use HeaderByNumber to avoid new transaction type unsupported issues)
			header, err := l.client.HeaderByNumber(l.ctx, big.NewInt(int64(vLog.BlockNumber)))
			if err != nil {
				logger.Warnf("Failed to get block header for block %d: %v", vLog.BlockNumber, err)
				return nil
			}
			// Construct BlockInfo (replace *types.Block)
			blockInfo := &BlockInfo{
				Number:    header.Number.Uint64(),
				Hash:      header.Hash(),
				Timestamp: header.Time,
			}

			// Get sender address
			from, err := types.Sender(types.LatestSignerForChainID(new(big.Int).SetUint64(l.config.ChainID)), tx)
			if err != nil {
				logger.Warnf("Failed to get sender address: %v", err)
				return nil
			}
			logger.Infof("event.EventName is: %s", event.EventName)

			switch event.EventName {
			case "Deposit":
				event.TokenAddress = event.EventData["token"].(common.Address).Hex()
				if err := l.handleDepositEvent(tx, receipt, blockInfo, from, event); err != nil {
					logger.Errorf("Failed to process deposit event: %v", err)
				}

			case "Withdraw":
				event.TokenAddress = event.EventData["token"].(common.Address).Hex()
				if err := l.handleWithdrawEvent(tx, receipt, blockInfo, from, event); err != nil {
					logger.Errorf("Failed to process withdraw event: %v", err)
				}

			case "ETHDeposit":
				logger.Infof("start to handle ETHDeposit event")
				event.TokenAddress = "0x0"
				if err := l.handleDepositEvent(tx, receipt, blockInfo, from, event); err != nil {
					logger.Errorf("Failed to process ETH deposit event: %v", err)
				}

			case "ETHWithdraw":
				event.TokenAddress = "0x0"
				if err := l.handleWithdrawEvent(tx, receipt, blockInfo, from, event); err != nil {
					logger.Errorf("Failed to process ETH withdraw event: %v", err)
				}

			case "Transfer":
				event.TokenAddress = event.ContractAddr
				if err := l.handleTransferEvent(tx, receipt, blockInfo, event); err != nil {
					logger.Errorf("Failed to process transfer event: %v", err)
				}

			case "Approval":
				// ERC20 Approval Event, just log for now, no special processing
				logger.Debugf("[Event] Approval: owner=%v, spender=%v",
					event.EventData["owner"], event.EventData["spender"])

			case "BatchPayinItem":
				event.TokenAddress = event.EventData["token"].(common.Address).Hex()
				if err := l.handleBatchPayinItemEvent(tx, receipt, blockInfo, event); err != nil {
					logger.Errorf("Failed to process batch payin item event: %v", err)
				}

			case "BatchPayoutItem":
				event.TokenAddress = event.EventData["token"].(common.Address).Hex()
				if err := l.handleBatchPayoutItemEvent(tx, receipt, blockInfo, event); err != nil {
					logger.Errorf("Failed to process batch payout item event: %v", err)
				}

			default:
				// Other events are only saved to the events table
				logger.Debugf("[Event] %s: saved to events table", event.EventName)
			}

			// Save transaction to transactions table
			if pgStorage, ok := l.storage.(*storage.PostgresStorage); ok {
				txData, err := l.parser.ParseTransactionWithBlockInfo(tx, receipt, blockInfo.Number, blockInfo.Hash.Hex(), blockInfo.Timestamp)
				if err != nil {
					logger.Errorf("Failed to parse transaction: %v", err)
				} else {
					txRecord := &storage.Transaction{
						ChainType:       string(appTypes.ChainTypeEVM),
						ChainName:       l.config.Name,
						BlockNumber:     txData.BlockNumber,
						BlockHash:       txData.BlockHash,
						TxHash:          txData.Hash,
						TxIndex:         uint(receipt.TransactionIndex),
						FromAddress:     txData.From,
						ToAddress:       txData.To,
						Value:           txData.Value,
						GasUsed:         txData.GasUsed,
						GasPrice:        txData.GasPrice,
						GasLimit:        txData.GasLimit,
						Nonce:           txData.Nonce,
						InputData:       txData.InputData,
						Status:          txData.Status,
						ContractAddress: txData.ContractAddress,
						Timestamp:       time.Unix(int64(txData.Timestamp), 0),
						Confirmations:   0,
						IsFinalized:     false,
					}
					if err := pgStorage.SaveTransaction(l.ctx, txRecord); err != nil {
						logger.Errorf("Failed to save transaction: %v", err)
					}
				}

				// 2. Synchronize and save event to events table
				if l.storage != nil {
					// Default to using the transaction's from and to addresses
					event.FromAddress = from.Hex()
					event.ToAddress = tx.To().Hex()

					// Set correct addresses based on event type
					switch event.EventName {
					case "Withdraw", "ETHWithdraw":
						// Withdraw event: contract -> user (user withdraws from contract)
						if user, ok := event.EventData["user"].(common.Address); ok {
							event.FromAddress = event.ContractAddr
							event.ToAddress = user.Hex()
						}
					case "Deposit", "ETHDeposit":
						// Deposit event: user -> contract (user deposits to contract)
						if user, ok := event.EventData["user"].(common.Address); ok {
							event.FromAddress = user.Hex()
							event.ToAddress = event.ContractAddr
						}
					case "Transfer":
						// Transfer event: from -> to
						if fromAddr, ok := event.EventData["from"].(common.Address); ok {
							event.FromAddress = fromAddr.Hex()
						}
						if toAddr, ok := event.EventData["to"].(common.Address); ok {
							event.ToAddress = toAddr.Hex()
						}
					case "BatchPayinItem":
						// BatchPayinItem: from -> contract
						if fromAddr, ok := event.EventData["from"].(common.Address); ok {
							event.FromAddress = fromAddr.Hex()
							event.ToAddress = event.ContractAddr
						}
					case "BatchPayoutItem":
						// BatchPayoutItem: contract -> to
						if toAddr, ok := event.EventData["to"].(common.Address); ok {
							event.FromAddress = event.ContractAddr
							event.ToAddress = toAddr.Hex()
						}
					default:
						// Other events try to extract from event data
						if addr, ok := event.EventData["from"].(common.Address); ok {
							event.FromAddress = addr.Hex()
						}
						if addr, ok := event.EventData["to"].(common.Address); ok {
							event.ToAddress = addr.Hex()
						}
					}

					event.GasLimit = txData.GasLimit
					event.GasPrice = txData.GasPrice
					event.GasUsed = receipt.GasUsed
					if err := l.storage.SaveEvent(l.ctx, event); err != nil {
						logger.Errorf("Failed to save event: %v", err)
						return fmt.Errorf("failed to save event: %w", err)
					}
				}
			}

		}
	}

	return nil
}

// parseEvent Parse event
func (l *EVMListener) parseEvent(vLog types.Log, contractInfo *ContractInfo) (*appTypes.Event, error) {
	if len(vLog.Topics) == 0 {
		return nil, nil
	}

	eventSig := vLog.Topics[0]

	// Find matching event
	var eventName string
	var eventABI abi.Event
	for name, evt := range contractInfo.Events {
		if evt.ID == eventSig {
			eventName = name
			eventABI = evt
			break
		}
	}

	if eventName == "" {
		return nil, nil
	}

	// Parse event data
	eventData := make(map[string]interface{})
	if err := eventABI.Inputs.UnpackIntoMap(eventData, vLog.Data); err != nil {
		return nil, fmt.Errorf("failed to unpack event data: %w", err)
	}

	// Parse indexed parameters
	var indexed abi.Arguments
	for _, arg := range eventABI.Inputs {
		if arg.Indexed {
			indexed = append(indexed, arg)
		}
	}

	if len(vLog.Topics) > 1 {
		if err := abi.ParseTopicsIntoMap(eventData, indexed, vLog.Topics[1:]); err != nil {
			return nil, fmt.Errorf("failed to parse indexed parameters: %w", err)
		}
	}

	// Serialize raw data
	rawData, _ := json.Marshal(vLog)

	event := &appTypes.Event{
		ID:           fmt.Sprintf("%s-%d-%d", vLog.TxHash.Hex(), vLog.Index, vLog.BlockNumber),
		ChainType:    appTypes.ChainTypeEVM,
		ChainName:    l.config.Name,
		BlockNumber:  vLog.BlockNumber,
		BlockHash:    vLog.BlockHash.Hex(),
		TxHash:       vLog.TxHash.Hex(),
		TxIndex:      vLog.TxIndex,
		LogIndex:     vLog.Index,
		ContractAddr: vLog.Address.Hex(),
		EventName:    eventName,
		EventData:    eventData,
		Timestamp:    time.Now(),
		RawData:      rawData,
	}

	logger.WithFields(map[string]interface{}{
		"chain":    l.config.Name,
		"contract": contractInfo.Name,
		"event":    eventName,
		"tx":       vLog.TxHash.Hex(),
		"block":    vLog.BlockNumber,
	}).Info("Detected new event")

	return event, nil
}

// handleDepositEvent Handle deposit event
func (l *EVMListener) handleDepositEvent(tx *types.Transaction, receipt *types.Receipt, blockInfo *BlockInfo, from common.Address, event *appTypes.Event) error {
	// Only process successful transactions
	if receipt.Status != 1 {
		return nil
	}

	// Only record deposits from monitored addresses
	if !l.isAddressMonitored(from) {
		return nil
	}

	// Debug: output event data types
	logger.Debugf("[Deposit event] EventData: %+v", event.EventData)
	for key, value := range event.EventData {
		logger.Debugf("  - %s: %T = %v", key, value, value)
	}

	// Extract amount and token address from event data
	var amount string
	if amt, ok := event.EventData["amount"].(string); ok && amt != "" {
		amount = amt
	} else if val, ok := event.EventData["value"].(string); ok && val != "" {
		amount = val
	} else if amt, ok := event.EventData["amount"].(*big.Int); ok && amt != nil {
		amount = amt.String()
	} else if val, ok := event.EventData["value"].(*big.Int); ok && val != nil {
		amount = val.String()
	}

	if amount == "" {
		logger.Warnf("Deposit event missing amount or value field: %v", event.EventData)
		return nil
	}

	// Extract token address from event data (according to ABI definition, token is indexed address)
	var tokenAddress string
	if token, ok := event.EventData["token"].(common.Address); ok {
		tokenAddress = token.Hex()
		logger.Debugf("Extracted token address: %s", tokenAddress)
	} else {
		// If there is no token field, it may be native token (ETH)
		tokenAddress = "0x0"
		logger.Warnf("Deposit event missing token field, using native token: 0x0, EventData: %+v", event.EventData)
	}

	// Calculate Gas fee
	gasPrice := tx.GasPrice()
	if gasPrice == nil {
		gasPrice = big.NewInt(0)
	}
	gasFee := new(big.Int).Mul(gasPrice, big.NewInt(int64(receipt.GasUsed)))

	// Save to deposits table
	pgStorage, ok := l.storage.(*storage.PostgresStorage)
	if !ok {
		return nil
	}

	deposit := &storage.Deposit{
		ChainType:     string(appTypes.ChainTypeEVM),
		ChainName:     l.config.Name,
		TxHash:        tx.Hash().Hex(),
		BlockNumber:   blockInfo.Number,
		BlockHash:     blockInfo.Hash.Hex(),
		FromAddress:   from.Hex(),
		ToAddress:     tx.To().Hex(),
		TokenAddress:  tokenAddress,
		Amount:        amount,
		GasUsed:       receipt.GasUsed,
		GasFee:        gasFee.String(),
		Status:        uint64(receipt.Status),
		Confirmations: 0,
		IsFinalized:   false,
		Timestamp:     time.Unix(int64(blockInfo.Timestamp), 0),
	}

	if err := pgStorage.SaveDeposit(l.ctx, deposit); err != nil {
		return fmt.Errorf("failed to save deposit record: %w", err)
	}

	logger.Infof("[Event deposit] %s -> %s, amount: %s, token: %s, tx: %s",
		from.Hex(), tx.To().Hex(), amount, tokenAddress, tx.Hash().Hex())

	// Update balance (both native token and ERC20 need to be updated)
	if err := l.updateBalanceFromEvent(from.Hex(), tokenAddress, blockInfo.Number, tx.Hash().Hex()); err != nil {
		logger.Warnf("failed to update deposit balance (continue processing): %v", err)
	}

	return nil
}

// handleWithdrawEvent Handle withdraw event (called from processLog)
func (l *EVMListener) handleWithdrawEvent(tx *types.Transaction, receipt *types.Receipt, blockInfo *BlockInfo, from common.Address, event *appTypes.Event) error {
	// Only process successful transactions
	if receipt.Status != 1 {
		return nil
	}

	// Extract user address from event data (withdraw user)
	var userAddress common.Address
	if user, ok := event.EventData["user"].(common.Address); ok {
		userAddress = user
	} else {
		logger.Warnf("Withdraw event missing user field: %v", event.EventData)
		return nil
	}

	// Withdraw events do not need to check if the user is monitored, process all withdrawals
	logger.Debugf("Processing withdraw event for user: %s", userAddress.Hex())

	// Extract amount from event data
	var amount string
	if amt, ok := event.EventData["amount"].(string); ok && amt != "" {
		amount = amt
	} else if amt, ok := event.EventData["amount"].(*big.Int); ok && amt != nil {
		amount = amt.String()
	}

	if amount == "" {
		logger.Warnf("Withdraw event missing amount field: %v", event.EventData)
		return nil
	}

	// Extract token address from event data (according to ABI definition, token is indexed address)
	var tokenAddress string
	if token, ok := event.EventData["token"].(common.Address); ok {
		tokenAddress = token.Hex()
		logger.Debugf("Extracted token address: %s", tokenAddress)
	} else {
		// If there is no token field, it may be native token (ETH)
		tokenAddress = "0x0"
		logger.Warnf("Withdraw event missing token field, using native token: 0x0, EventData: %+v", event.EventData)
	}

	// Calculate Gas fee
	gasPrice := tx.GasPrice()
	if gasPrice == nil {
		gasPrice = big.NewInt(0)
	}
	gasFee := new(big.Int).Mul(gasPrice, big.NewInt(int64(receipt.GasUsed)))

	// Save to withdraws table
	pgStorage, ok := l.storage.(*storage.PostgresStorage)
	if !ok {
		return nil
	}

	// For Withdraw events:
	// - FromAddress: contract address (AssetPoolManager, source of funds)
	// - ToAddress: user (withdrawal user address, destination of funds)
	// - TokenAddress: token (withdrawn token address)
	withdraw := &storage.Withdraw{
		ChainType:     string(appTypes.ChainTypeEVM),
		ChainName:     l.config.Name,
		TxHash:        tx.Hash().Hex(),
		BlockNumber:   blockInfo.Number,
		BlockHash:     blockInfo.Hash.Hex(),
		FromAddress:   event.ContractAddr,
		ToAddress:     userAddress.Hex(),
		TokenAddress:  tokenAddress,
		Amount:        amount,
		GasUsed:       receipt.GasUsed,
		GasFee:        gasFee.String(),
		Status:        uint64(receipt.Status),
		Confirmations: 0,
		IsFinalized:   false,
		Timestamp:     time.Unix(int64(blockInfo.Timestamp), 0),
	}

	if err := pgStorage.SaveWithdraw(l.ctx, withdraw); err != nil {
		return fmt.Errorf("failed to save withdraw record: %w", err)
	}

	logger.Infof("[Event withdraw] user: %s, contract: %s, token: %s, amount: %s, tx: %s",
		userAddress.Hex(), event.ContractAddr, tokenAddress, amount, tx.Hash().Hex())

	// Update balance (only if user is monitored)
	if l.isAddressMonitored(userAddress) {
		if err := l.updateBalanceFromEvent(userAddress.Hex(), tokenAddress, blockInfo.Number, tx.Hash().Hex()); err != nil {
			logger.Errorf("Failed to update withdraw balance: %v", err)
		}
	} else {
		logger.Debugf("Skip balance update for non-monitored user: %s", userAddress.Hex())
	}

	return nil
}

// handleTransferEvent Handle ERC20 Transfer event
func (l *EVMListener) handleTransferEvent(tx *types.Transaction, receipt *types.Receipt, blockInfo *BlockInfo, event *appTypes.Event) error {
	// Only process successful transactions
	if receipt.Status != 1 {
		return nil
	}

	// Extract from/to/amount
	var fromAddress string
	if addr, ok := event.EventData["from"].(common.Address); ok {
		fromAddress = addr.Hex()
	} else if addr, ok := event.EventData["from"].(string); ok {
		fromAddress = addr
	}

	var toAddress string
	if addr, ok := event.EventData["to"].(common.Address); ok {
		toAddress = addr.Hex()
	} else if addr, ok := event.EventData["to"].(string); ok {
		toAddress = addr
	}

	// Extract transfer amount
	var amount string
	if val, ok := event.EventData["value"].(*big.Int); ok && val != nil {
		amount = val.String()
	} else if val, ok := event.EventData["value"].(string); ok && val != "" {
		amount = val
	} else if val, ok := event.EventData["amount"].(*big.Int); ok && val != nil {
		amount = val.String()
	} else if val, ok := event.EventData["amount"].(string); ok && val != "" {
		amount = val
	}

	// The tokenAddress for Transfer events is the contract address that emitted the event (ERC20 token address)
	tokenAddress := event.ContractAddr
	logger.Debugf("Transfer event: from=%s, to=%s, token=%s, amount=%s", fromAddress, toAddress, tokenAddress, amount)

	if amount == "" {
		logger.Warnf("Transfer event missing amount/value field: %v", event.EventData)
		return nil
	}

	// Calculate Gas fee
	gasPrice := tx.GasPrice()
	if gasPrice == nil {
		gasPrice = big.NewInt(0)
	}
	gasFee := new(big.Int).Mul(gasPrice, big.NewInt(int64(receipt.GasUsed)))

	blockNumber := blockInfo.Number
	txHash := tx.Hash().Hex()
	timestamp := time.Unix(int64(blockInfo.Timestamp), 0)
	blockHash := blockInfo.Hash.Hex()

	pgStorage, ok := l.storage.(*storage.PostgresStorage)
	if !ok {
		return nil
	}

	// Check if from address is monitored → Withdraw
	if fromAddress != "" && l.isAddressMonitored(common.HexToAddress(fromAddress)) {
		withdraw := &storage.Withdraw{
			ChainType:     string(appTypes.ChainTypeEVM),
			ChainName:     l.config.Name,
			TxHash:        txHash,
			BlockNumber:   blockNumber,
			BlockHash:     blockHash,
			FromAddress:   fromAddress,
			ToAddress:     toAddress,
			TokenAddress:  tokenAddress,
			Amount:        amount,
			GasUsed:       receipt.GasUsed,
			GasFee:        gasFee.String(),
			Status:        uint64(receipt.Status),
			Confirmations: 0,
			IsFinalized:   false,
			Timestamp:     timestamp,
		}

		if err := pgStorage.SaveWithdraw(l.ctx, withdraw); err != nil {
			logger.Errorf("Failed to save ERC20 transfer withdraw: %v", err)
		} else {
			logger.Infof("[ERC20 Transfer withdraw] %s -> %s, amount: %s, token: %s, tx: %s",
				fromAddress, toAddress, amount, tokenAddress, txHash)
		}

		// Update from address balance
		if err := l.updateBalanceFromEvent(fromAddress, tokenAddress, blockNumber, txHash); err != nil {
			logger.Errorf("Failed to update from balance: %v", err)
		}
	}

	// Check if to address is monitored → Deposit
	if toAddress != "" && l.isAddressMonitored(common.HexToAddress(toAddress)) {
		deposit := &storage.Deposit{
			ChainType:     string(appTypes.ChainTypeEVM),
			ChainName:     l.config.Name,
			TxHash:        txHash,
			BlockNumber:   blockNumber,
			BlockHash:     blockHash,
			FromAddress:   fromAddress,
			ToAddress:     toAddress,
			TokenAddress:  tokenAddress,
			Amount:        amount,
			GasUsed:       receipt.GasUsed,
			GasFee:        gasFee.String(),
			Status:        uint64(receipt.Status),
			Confirmations: 0,
			IsFinalized:   false,
			Timestamp:     timestamp,
		}

		if err := pgStorage.SaveDeposit(l.ctx, deposit); err != nil {
			logger.Errorf("Failed to save ERC20 transfer deposit: %v", err)
		} else {
			logger.Infof("[ERC20 Transfer deposit] %s -> %s, amount: %s, token: %s, tx: %s",
				fromAddress, toAddress, amount, tokenAddress, txHash)
		}

		// Update to address balance
		if err := l.updateBalanceFromEvent(toAddress, tokenAddress, blockNumber, txHash); err != nil {
			logger.Errorf("Failed to update to balance: %v", err)
		}
	}

	return nil
}

// handleBatchPayinItemEvent Handle batch payin item event
// Each user's transfer triggers a separate BatchPayinItem event
func (l *EVMListener) handleBatchPayinItemEvent(tx *types.Transaction, receipt *types.Receipt, blockInfo *BlockInfo, event *appTypes.Event) error {
	// Only process successful transactions
	if receipt.Status != 1 {
		return nil
	}

	var fromAddress string
	if from, ok := event.EventData["from"].(common.Address); ok {
		fromAddress = from.Hex()
	} else {
		logger.Warnf("BatchPayinItem event missing from field")
		return nil
	}

	var tokenAddress string
	if token, ok := event.EventData["token"].(common.Address); ok {
		tokenAddress = token.Hex()
	} else {
		logger.Warnf("BatchPayinItem event missing token field")
		return nil
	}

	var receivedAmount string
	if amt, ok := event.EventData["receivedAmount"].(*big.Int); ok && amt != nil {
		receivedAmount = amt.String()
	} else if amt, ok := event.EventData["receivedAmount"].(string); ok && amt != "" {
		receivedAmount = amt
	} else {
		logger.Warnf("BatchPayinItem event missing receivedAmount field")
		return nil
	}

	// Only record deposits for monitored addresses
	if !l.isAddressMonitored(common.HexToAddress(fromAddress)) {
		logger.Debugf("Skip batch payin for non-monitored address: from=%s", fromAddress)
		return nil
	}

	// Calculate Gas fee (Note: Gas is shared in batch payin, here we record the total gas)
	gasPrice := tx.GasPrice()
	if gasPrice == nil {
		gasPrice = big.NewInt(0)
	}
	gasFee := new(big.Int).Mul(gasPrice, big.NewInt(int64(receipt.GasUsed)))

	blockNumber := blockInfo.Number
	txHash := tx.Hash().Hex()
	timestamp := time.Unix(int64(blockInfo.Timestamp), 0)

	pgStorage, ok := l.storage.(*storage.PostgresStorage)
	if !ok {
		return nil
	}

	// Save to deposits table
	deposit := &storage.Deposit{
		ChainType:     string(appTypes.ChainTypeEVM),
		ChainName:     l.config.Name,
		TxHash:        txHash,
		BlockNumber:   blockNumber,
		BlockHash:     blockInfo.Hash.Hex(),
		FromAddress:   fromAddress,
		ToAddress:     event.ContractAddr, // AssetPoolManager contract address
		TokenAddress:  tokenAddress,
		Amount:        receivedAmount,
		GasUsed:       receipt.GasUsed,
		GasFee:        gasFee.String(),
		Status:        uint64(receipt.Status),
		Confirmations: 0,
		IsFinalized:   false,
		Timestamp:     timestamp,
	}

	if err := pgStorage.SaveDeposit(l.ctx, deposit); err != nil {
		return fmt.Errorf("failed to save batch payin deposit record: %w", err)
	}

	logger.Infof("[Batch payin] %s -> %s, amount: %s, token: %s, tx: %s",
		fromAddress, event.ContractAddr, receivedAmount, tokenAddress, txHash)

	// Update user balance
	if err := l.updateBalanceFromEvent(fromAddress, tokenAddress, blockNumber, txHash); err != nil {
		logger.Warnf("Failed to update batch payin balance (continue processing): %v", err)
	}

	return nil
}

// handleBatchPayoutItemEvent Handle batch payout item event
// Contract method: batchTransferPayout(address[] recipients, address[] tokens, uint256[] amounts)
// Event: BatchPayoutItem(address indexed recipient, address indexed token, uint256 amount, uint256 timestamp)
func (l *EVMListener) handleBatchPayoutItemEvent(tx *types.Transaction, receipt *types.Receipt, blockInfo *BlockInfo, event *appTypes.Event) error {
	// Only process successful transactions
	if receipt.Status != 1 {
		return nil
	}

	// Extract recipient address from event data (field name: "to" according to Settlement ABI)
	var recipientAddress string
	if recipient, ok := event.EventData["to"].(common.Address); ok {
		recipientAddress = recipient.Hex()
	} else {
		logger.Warnf("BatchPayoutItem event missing 'to' field")
		return nil
	}

	// Extract token address from event data
	var tokenAddress string
	if token, ok := event.EventData["token"].(common.Address); ok {
		tokenAddress = token.Hex()
	} else {
		logger.Warnf("BatchPayoutItem event missing token field")
		return nil
	}

	// Extract amount from event data
	var amount string
	if amt, ok := event.EventData["amount"].(*big.Int); ok && amt != nil {
		amount = amt.String()
	} else if amt, ok := event.EventData["amount"].(string); ok && amt != "" {
		amount = amt
	} else {
		logger.Warnf("BatchPayoutItem event missing amount field")
		return nil
	}

	// Only record payouts for monitored addresses
	if !l.isAddressMonitored(common.HexToAddress(recipientAddress)) {
		logger.Debugf("Skip batch payout for non-monitored address: to=%s", recipientAddress)
		return nil
	}

	// Calculate Gas fee (Note: Gas is shared in batch payout, here we record the total gas)
	gasPrice := tx.GasPrice()
	if gasPrice == nil {
		gasPrice = big.NewInt(0)
	}
	gasFee := new(big.Int).Mul(gasPrice, big.NewInt(int64(receipt.GasUsed)))

	blockNumber := blockInfo.Number
	txHash := tx.Hash().Hex()
	timestamp := time.Unix(int64(blockInfo.Timestamp), 0)

	pgStorage, ok := l.storage.(*storage.PostgresStorage)
	if !ok {
		return nil
	}

	// Save to withdraws table (batch payout is essentially a withdrawal operation)
	// FromAddress is Settlement contract address (event source)
	withdraw := &storage.Withdraw{
		ChainType:     string(appTypes.ChainTypeEVM),
		ChainName:     l.config.Name,
		TxHash:        txHash,
		BlockNumber:   blockNumber,
		BlockHash:     blockInfo.Hash.Hex(),
		FromAddress:   event.ContractAddr, // Settlement contract address
		ToAddress:     recipientAddress,   // Recipient address
		TokenAddress:  tokenAddress,       // Token address
		Amount:        amount,             // Transfer amount
		GasUsed:       receipt.GasUsed,
		GasFee:        gasFee.String(),
		Status:        uint64(receipt.Status),
		Confirmations: 0,
		IsFinalized:   false,
		Timestamp:     timestamp,
	}

	if err := pgStorage.SaveWithdraw(l.ctx, withdraw); err != nil {
		return fmt.Errorf("failed to save batch payout withdraw record: %w", err)
	}

	logger.Infof("[Batch payout] %s -> %s, amount: %s, token: %s, tx: %s",
		event.ContractAddr, recipientAddress, amount, tokenAddress, txHash)

	// Update recipient balance
	if err := l.updateBalanceFromEvent(recipientAddress, tokenAddress, blockNumber, txHash); err != nil {
		logger.Warnf("Failed to update batch payout balance (continue processing): %v", err)
	}

	return nil
}
