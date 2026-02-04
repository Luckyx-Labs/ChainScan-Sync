package evm

import (
	"fmt"
	"math/big"
	"strings"
	"time"

	storage "github.com/Luckyx-Labs/chainscan-sync/internal/database"
	appTypes "github.com/Luckyx-Labs/chainscan-sync/internal/types"
	"github.com/Luckyx-Labs/chainscan-sync/pkg/logger"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
)

// ========== Alchemy API Struct ==========

// AlchemyAssetTransfersParams Alchemy API request parameters
type AlchemyAssetTransfersParams struct {
	FromBlock        string   `json:"fromBlock"`
	ToBlock          string   `json:"toBlock"`
	Category         []string `json:"category"`
	WithMetadata     bool     `json:"withMetadata"`
	ExcludeZeroValue bool     `json:"excludeZeroValue"`
	MaxCount         string   `json:"maxCount,omitempty"`
}

// AlchemyAssetTransfersResult Alchemy API response result
type AlchemyAssetTransfersResult struct {
	Transfers []AlchemyTransfer `json:"transfers"`
	PageKey   string            `json:"pageKey,omitempty"`
}

// AlchemyTransfer Single transfer record
type AlchemyTransfer struct {
	BlockNum    string                  `json:"blockNum"`
	Hash        string                  `json:"hash"`
	From        string                  `json:"from"`
	To          string                  `json:"to"`
	Value       float64                 `json:"value"`
	Asset       string                  `json:"asset"`
	Category    string                  `json:"category"`
	RawContract AlchemyRawContract      `json:"rawContract"`
	Metadata    AlchemyTransferMetadata `json:"metadata"`
}

// RawTransaction legacy transaction structure
type RawTransaction struct {
	Hash        string   `json:"hash"`
	From        string   `json:"from"`
	To          string   `json:"to"`
	Value       *big.Int `json:"-"`
	ValueHex    string   `json:"value"`
	Input       []byte   `json:"-"`
	InputHex    string   `json:"input"`
	Gas         uint64   `json:"-"`
	GasHex      string   `json:"gas"`
	GasPrice    *big.Int `json:"-"`
	GasPriceHex string   `json:"gasPrice"`
	Nonce       uint64   `json:"-"`
	NonceHex    string   `json:"nonce"`
	Type        uint8    `json:"-"`
	TypeHex     string   `json:"type"`
}

// AlchemyRawContract Original contract information
type AlchemyRawContract struct {
	Value   string `json:"value"`
	Address string `json:"address"`
	Decimal string `json:"decimal"`
}

// AlchemyTransferMetadata Transfer metadata
type AlchemyTransferMetadata struct {
	BlockTimestamp string `json:"blockTimestamp"`
}

// BlockReceiptsResult Block receipts cache
type BlockReceiptsResult struct {
	Receipts map[string]*types.Receipt  // txHash -> receipt
	RawTxMap map[string]*RawTransaction // txHash -> raw transaction (compatible with new transaction types)
}

// getBlockReceiptsAndTxs Batch get all receipts and transaction information of a block
// Use raw JSON-RPC calls to avoid new transaction type parsing issues
func (l *EVMListener) getBlockReceiptsAndTxs(blockNumber uint64) (*BlockReceiptsResult, error) {
	result := &BlockReceiptsResult{
		Receipts: make(map[string]*types.Receipt),
		RawTxMap: make(map[string]*RawTransaction),
	}

	blockHex := hexutil.EncodeUint64(blockNumber)

	// Use eth_getBlockReceipts (supported by Alchemy/Infura/Geth 1.13+)
	var receipts []*types.Receipt
	err := l.rpcClient.CallContext(l.ctx, &receipts, "eth_getBlockReceipts", blockHex)
	if err != nil {
		// If eth_getBlockReceipts is not supported, log a warning but do not return an error
		logger.Debugf("eth_getBlockReceipts not supported or failed: %v, gas info will be empty", err)
	} else {
		// Build receipts map
		for _, receipt := range receipts {
			if receipt != nil {
				result.Receipts[receipt.TxHash.Hex()] = receipt
			}
		}
	}

	// Get block transactions - Use raw JSON-RPC calls to avoid new transaction type parsing issues
	var blockData struct {
		Transactions []*RawTransaction `json:"transactions"`
	}
	err = l.rpcClient.CallContext(l.ctx, &blockData, "eth_getBlockByNumber", blockHex, true)
	if err != nil {
		// report error to failover client if exists
		if l.failoverClient != nil {
			l.failoverClient.ReportError(err)
		}
		logger.Warnf("Failed to get block %d transactions: %v", blockNumber, err)
		return result, fmt.Errorf("failed to get block: %w", err)
	}

	// report success to failover client if exists
	if l.failoverClient != nil {
		l.failoverClient.ReportSuccess()
	}

	for _, tx := range blockData.Transactions {
		if tx != nil && tx.Hash != "" {
			// Parse ValueHex to Value (big.Int)
			if tx.ValueHex != "" {
				tx.Value = new(big.Int)
				tx.Value.SetString(tx.ValueHex[2:], 16) // Remove "0x" prefix and parse as hex
			}
			result.RawTxMap[tx.Hash] = tx
		}
	}

	logger.Debugf("Block %d: getBlockReceiptsAndTxs got %d transactions", blockNumber, len(result.RawTxMap))

	return result, nil
}

// processNativeTransfersWithStandardRPC Process native token transfers using standard RPC API
// Use raw JSON-RPC calls to avoid new transaction type parsing issues
func (l *EVMListener) processNativeTransfersWithStandardRPC(blockNumber uint64, header *types.Header) error {
	logger.Debugf("Block %d: Scanning native transfers with standard RPC", blockNumber)

	// Get all receipts and transaction information of the block
	blockReceipts, err := l.getBlockReceiptsAndTxs(blockNumber)
	if err != nil {
		return fmt.Errorf("failed to get block receipts: %w", err)
	}

	logger.Debugf("Block %d: Got %d transactions from standard RPC", blockNumber, len(blockReceipts.RawTxMap))

	if len(blockReceipts.RawTxMap) == 0 {
		return nil
	}

	blockTime := time.Unix(int64(header.Time), 0)
	processedCount := 0

	// Iterate over all transactions to find native token transfers
	for _, tx := range blockReceipts.RawTxMap {
		// Check if it is a native token transfer (value > 0)
		if tx.Value == nil || tx.Value.Sign() <= 0 {
			continue
		}

		// Check if To address exists (exclude contract creation transactions)
		if tx.To == "" {
			continue
		}

		from := common.HexToAddress(tx.From)
		to := common.HexToAddress(tx.To)

		// fromMonitored := l.isAddressMonitored(from)
		isWithdraw := l.withdrawEOA != "" && strings.EqualFold(from.Hex(), l.withdrawEOA)
		// Check if it is related to monitored addresses
		toMonitored := l.isAddressMonitored(to)

		if !isWithdraw && !toMonitored {
			continue
		}

		processedCount++

		// Construct AlchemyTransfer struct to reuse save logic
		transfer := AlchemyTransfer{
			Hash: tx.Hash,
			From: tx.From,
			To:   tx.To,
		}

		// Save to database
		if err := l.saveNativeTransferFromAlchemy(
			transfer, tx.Value, header, blockTime,
			isWithdraw, toMonitored,
		); err != nil {
			logger.Errorf("Failed to save native transfer: %v", err)
		}
	}

	if processedCount > 0 {
		logger.Infof("Block %d: Processed %d native transfers via standard RPC", blockNumber, processedCount)
	}

	return nil
}

// saveNativeTransferFromAlchemy Save native token transfers obtained through Alchemy API
func (l *EVMListener) saveNativeTransferFromAlchemy(
	transfer AlchemyTransfer,
	amount *big.Int,
	header *types.Header,
	blockTime time.Time,
	isWithdraw, toMonitored bool,
) error {
	pgStorage, ok := l.storage.(*storage.PostgresStorage)
	if !ok {
		return nil
	}

	blockNumber := header.Number.Uint64()
	blockHash := header.Hash().Hex()
	txHash := transfer.Hash
	tokenAddress := "0x0" // Native token

	// Native token transfer gas information
	// - Standard EOA transfer gas limit and gas used are fixed at 21000
	// - Gas price is obtained from the block header's BaseFee (after EIP-1559)
	var gasUsed uint64 = 21000
	var gasLimit uint64 = 21000
	var gasPrice string = "0"
	var gasFee string = "0"

	// Get base fee from block header as gas price
	if header.BaseFee != nil {
		gasPrice = header.BaseFee.String()
		gasFee = new(big.Int).Mul(big.NewInt(int64(gasUsed)), header.BaseFee).String()
	}

	// If from is the configured withdraw EOA address → Withdraw
	// Changed from: fromMonitored (any monitored address)
	// Changed to: only the configured withdraw_eoa address can trigger withdraw
	if isWithdraw {
		withdraw := &storage.Withdraw{
			ChainType:     string(appTypes.ChainTypeEVM),
			ChainName:     l.config.Name,
			TxHash:        txHash,
			BlockNumber:   blockNumber,
			BlockHash:     blockHash,
			FromAddress:   transfer.From,
			ToAddress:     transfer.To,
			TokenAddress:  tokenAddress,
			Amount:        amount.String(),
			GasUsed:       gasUsed,
			GasFee:        gasFee,
			Status:        1,
			Confirmations: 0,
			IsFinalized:   false,
			Timestamp:     blockTime,
		}

		if err := pgStorage.SaveWithdraw(l.ctx, withdraw); err != nil {
			logger.Errorf("Failed to save native withdraw: %v", err)
		} else {
			logger.Infof("[Native withdraw] %s -> %s, amount: %s ETH, tx: %s",
				transfer.From, transfer.To, amount.String(), txHash)
		}

		// Update balance
		if err := l.updateBalanceFromEvent(transfer.From, tokenAddress, blockNumber, txHash); err != nil {
			logger.Warnf("Failed to update from balance: %v", err)
		}
	}

	// If to is a monitored address → Deposit
	if toMonitored {
		deposit := &storage.Deposit{
			ChainType:     string(appTypes.ChainTypeEVM),
			ChainName:     l.config.Name,
			TxHash:        txHash,
			BlockNumber:   blockNumber,
			BlockHash:     blockHash,
			FromAddress:   transfer.From,
			ToAddress:     transfer.To,
			TokenAddress:  tokenAddress,
			Amount:        amount.String(),
			GasUsed:       gasUsed,
			GasFee:        gasFee,
			Status:        1,
			Confirmations: 0,
			IsFinalized:   false,
			Timestamp:     blockTime,
		}

		if err := pgStorage.SaveDeposit(l.ctx, deposit); err != nil {
			logger.Errorf("Failed to save native deposit: %v", err)
		} else {
			logger.Infof("[Native deposit] %s -> %s, amount: %s ETH, tx: %s",
				transfer.From, transfer.To, amount.String(), txHash)
		}

		// Update balance
		if err := l.updateBalanceFromEvent(transfer.To, tokenAddress, blockNumber, txHash); err != nil {
			logger.Warnf("Failed to update to balance: %v", err)
		}
	}

	// Save to events table (only one record)
	// Use unified ID format: txHash-logIndex-blockNumber
	// NativeTransfer has no logIndex, use 0
	event := &appTypes.Event{
		ID:           fmt.Sprintf("%s-%d-%d", txHash, 0, blockNumber),
		ChainType:    appTypes.ChainTypeEVM,
		ChainName:    l.config.Name,
		BlockNumber:  blockNumber,
		BlockHash:    blockHash,
		TxHash:       txHash,
		TxIndex:      0,
		LogIndex:     0,
		ContractAddr: tokenAddress,
		EventName:    "NativeTransfer",
		FromAddress:  transfer.From,
		ToAddress:    transfer.To,
		TokenAddress: tokenAddress,
		GasUsed:      gasUsed,
		GasLimit:     gasLimit,
		GasPrice:     gasPrice,
		Timestamp:    blockTime,
		EventData: map[string]interface{}{
			"from":  transfer.From,
			"to":    transfer.To,
			"value": amount,
		},
	}
	if err := pgStorage.SaveEvent(l.ctx, event); err != nil {
		logger.Errorf("Failed to save native transfer event: %v", err)
	}

	// Save to transactions table
	txRecord := &storage.Transaction{
		ChainType:   string(appTypes.ChainTypeEVM),
		ChainName:   l.config.Name,
		BlockNumber: blockNumber,
		BlockHash:   blockHash,
		TxHash:      txHash,
		TxIndex:     0,
		FromAddress: transfer.From,
		ToAddress:   transfer.To,
		Value:       amount.String(),
		GasUsed:     gasUsed,
		GasPrice:    gasPrice,
		GasLimit:    gasLimit,
		Nonce:       0,
		InputData:   "",
		Status:      1,
		Timestamp:   blockTime,
	}
	if err := pgStorage.SaveTransaction(l.ctx, txRecord); err != nil {
		logger.Errorf("Failed to save native transfer transaction: %v", err)
	}

	return nil
}
