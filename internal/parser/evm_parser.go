package parser

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// EVMParser EVM transaction parser
type EVMParser struct{}

// NewEVMParser creates a new EVM parser
func NewEVMParser() *EVMParser {
	return &EVMParser{}
}

// ParseTransaction parses basic transaction information
func (p *EVMParser) ParseTransaction(tx *types.Transaction, receipt *types.Receipt, block *types.Block) (*TransactionData, error) {
	if block != nil {
		return p.ParseTransactionWithBlockInfo(tx, receipt, block.NumberU64(), block.Hash().Hex(), block.Time())
	}
	return p.ParseTransactionWithBlockInfo(tx, receipt, 0, "", 0)
}

// ParseTransactionWithBlockInfo parses basic transaction information (using block info parameters to avoid BlockByHash calls)
func (p *EVMParser) ParseTransactionWithBlockInfo(tx *types.Transaction, receipt *types.Receipt, blockNumber uint64, blockHash string, blockTime uint64) (*TransactionData, error) {
	txData := &TransactionData{
		Hash:        tx.Hash().Hex(),
		Nonce:       tx.Nonce(),
		GasLimit:    tx.Gas(),
		Value:       tx.Value().String(),
		InputData:   hex.EncodeToString(tx.Data()),
		BlockNumber: blockNumber,
		BlockHash:   blockHash,
		Timestamp:   blockTime,
	}

	// From address
	from, err := types.Sender(types.LatestSignerForChainID(tx.ChainId()), tx)
	if err == nil {
		txData.From = from.Hex()
	}

	// To address
	if tx.To() != nil {
		txData.To = tx.To().Hex()
	}

	// Gas price
	if tx.GasPrice() != nil {
		txData.GasPrice = tx.GasPrice().String()
	} else if tx.GasTipCap() != nil {
		txData.GasPrice = tx.GasTipCap().String()
	}

	// If receipt is available, extract more information
	if receipt != nil {
		txData.Status = receipt.Status
		txData.GasUsed = receipt.GasUsed
		txData.ContractAddress = receipt.ContractAddress.Hex()

		// Parse logs
		logs, err := p.ParseLogs(receipt.Logs)
		if err == nil {
			txData.Logs = logs
		}
	}

	return txData, nil
}

// ParseLogs parses transaction logs
func (p *EVMParser) ParseLogs(logs []*types.Log) ([]*LogData, error) {
	result := make([]*LogData, 0, len(logs))

	for _, log := range logs {
		logData := &LogData{
			Address: log.Address.Hex(),
			Topics:  make([]string, 0, len(log.Topics)),
			Data:    hex.EncodeToString(log.Data),
			Index:   log.Index,
		}

		for _, topic := range log.Topics {
			logData.Topics = append(logData.Topics, topic.Hex())
		}

		result = append(result, logData)
	}

	return result, nil
}

// DecodeEventLog decodes event logs (using ABI)
func (p *EVMParser) DecodeEventLog(contractABI abi.ABI, eventName string, log *types.Log) (map[string]interface{}, error) {
	event, ok := contractABI.Events[eventName]
	if !ok {
		return nil, fmt.Errorf("The event %s was not found", eventName)
	}

	// Verify event signature
	if len(log.Topics) == 0 || log.Topics[0] != event.ID {
		return nil, fmt.Errorf("Event signature does not match")
	}

	// Parse event data
	result := make(map[string]interface{})

	// Parse indexed parameters (from topics)
	topicIndex := 1
	for _, input := range event.Inputs {
		if input.Indexed {
			if topicIndex >= len(log.Topics) {
				return nil, fmt.Errorf("indexed parameter count does not match")
			}

			value, err := p.decodeTopicValue(input.Type, log.Topics[topicIndex])
			if err != nil {
				return nil, fmt.Errorf("failed to decode indexed parameter %s: %w", input.Name, err)
			}
			result[input.Name] = value
			topicIndex++
		}
	}

	// Parse non-indexed parameters (from data)
	nonIndexedInputs := make(abi.Arguments, 0)
	for _, input := range event.Inputs {
		if !input.Indexed {
			nonIndexedInputs = append(nonIndexedInputs, input)
		}
	}

	if len(nonIndexedInputs) > 0 && len(log.Data) > 0 {
		values, err := nonIndexedInputs.Unpack(log.Data)
		if err != nil {
			return nil, fmt.Errorf("failed to decode data: %w", err)
		}

		for i, input := range nonIndexedInputs {
			if i < len(values) {
				result[input.Name] = p.formatValue(values[i])
			}
		}
	}

	return result, nil
}

// decodeTopicValue decodes topic values
func (p *EVMParser) decodeTopicValue(typ abi.Type, topic common.Hash) (interface{}, error) {
	switch typ.T {
	case abi.AddressTy:
		return common.BytesToAddress(topic.Bytes()).Hex(), nil
	case abi.UintTy, abi.IntTy:
		return new(big.Int).SetBytes(topic.Bytes()).String(), nil
	case abi.BoolTy:
		return topic.Big().Uint64() != 0, nil
	case abi.BytesTy, abi.FixedBytesTy:
		return topic.Hex(), nil
	case abi.StringTy:
		return topic.Hex(), nil
	default:
		return topic.Hex(), nil
	}
}

// formatValue formats the decoded ABI value
func (p *EVMParser) formatValue(value interface{}) interface{} {
	switch v := value.(type) {
	case *big.Int:
		return v.String()
	case common.Address:
		return v.Hex()
	case []byte:
		return hex.EncodeToString(v)
	case [32]byte:
		return hex.EncodeToString(v[:])
	default:
		return v
	}
}

// DecodeInputData decodes transaction input data (function call)
func (p *EVMParser) DecodeInputData(contractABI abi.ABI, data []byte) (string, map[string]interface{}, error) {
	if len(data) < 4 {
		return "", nil, fmt.Errorf("input data is too short")
	}

	// Get function signature (first 4 bytes)
	methodSig := data[:4]

	// Find matching method
	method, err := contractABI.MethodById(methodSig)
	if err != nil {
		return "", nil, fmt.Errorf("method not found: %w", err)
	}

	// Decode parameters
	values, err := method.Inputs.Unpack(data[4:])
	if err != nil {
		return method.Name, nil, fmt.Errorf("failed to decode parameters: %w", err)
	}

	// Build parameter map
	params := make(map[string]interface{})
	for i, input := range method.Inputs {
		if i < len(values) {
			params[input.Name] = p.formatValue(values[i])
		}
	}

	return method.Name, params, nil
}

// ParseERC20Transfer parses ERC20 Transfer events
func (p *EVMParser) ParseERC20Transfer(log *types.Log) (*ERC20TransferData, error) {
	if len(log.Topics) != 3 {
		return nil, fmt.Errorf("Transfer event topics count is incorrect")
	}

	transfer := &ERC20TransferData{
		Token: log.Address.Hex(),
		From:  common.BytesToAddress(log.Topics[1].Bytes()).Hex(),
		To:    common.BytesToAddress(log.Topics[2].Bytes()).Hex(),
	}

	// Parse amount from data
	if len(log.Data) >= 32 {
		amount := new(big.Int).SetBytes(log.Data)
		transfer.Amount = amount.String()
	}

	return transfer, nil
}

// IsERC20Transfer determines if the event is an ERC20 Transfer event
func (p *EVMParser) IsERC20Transfer(log *types.Log) bool {
	// Transfer(address,address,uint256) signature
	transferSig := common.HexToHash("0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef")
	return len(log.Topics) == 3 && log.Topics[0] == transferSig
}

// Contract method signatures (first 4 bytes)
var (
	// deposit(address token, uint256 amount)
	MethodSigDeposit = []byte{0x47, 0xe7, 0xef, 0x24} // keccak256("deposit(address,uint256)")[:4]

	// withdraw(address token, uint256 amount)
	MethodSigWithdraw = []byte{0xf3, 0xfd, 0xb1, 0x5a} // keccak256("withdraw(address,uint256)")[:4]

	// batchTransferPayin(address[] calldata froms, address[] calldata tokens, uint256[] calldata amounts)
	MethodSigBatchPayin = []byte{0xa8, 0x6f, 0x17, 0xd7} // keccak256("batchTransferPayin(address[],address[],uint256[])")[:4]

	// batchTransferPayout(address[] calldata recipients, address[] calldata tokens, uint256[] calldata amounts)
	MethodSigBatchPayout = []byte{0xe6, 0x35, 0x67, 0x3d} // keccak256("batchTransferPayout(address[],address[],uint256[])")[:4]

	// depositETH() payable
	MethodSigDepositETH = []byte{0xf6, 0x32, 0x6f, 0xb3} // keccak256("depositETH()")[:4]

	// withdrawETH(uint256 amount)
	MethodSigWithdrawETH = []byte{0xf1, 0x4f, 0xcb, 0xc8} // keccak256("withdrawETH(uint256)")[:4]

	// transfer(address to, uint256 amount) - ERC20 standard transfer
	MethodSigERC20Transfer = []byte{0xa9, 0x05, 0x9c, 0xbb} // keccak256("transfer(address,uint256)")[:4]
)

// ContractCallType contract call types
type ContractCallType string

const (
	ContractCallEOATransfer ContractCallType = "eoa_transfer"          // EOA ordinary transfer
	ContractCallDeposit     ContractCallType = "contract_deposit"      // Contract deposit (ERC20)
	ContractCallWithdraw    ContractCallType = "contract_withdraw"     // Contract withdraw (ERC20)
	ContractCallBatchPayin  ContractCallType = "contract_batch_payin"  // Contract batch payin
	ContractCallBatchPayout ContractCallType = "contract_batch_payout" // Contract batch payout
	ContractCallDepositETH  ContractCallType = "contract_deposit_eth"  // Contract deposit (ETH)
	ContractCallWithdrawETH ContractCallType = "contract_withdraw_eth" // Contract withdraw (ETH)
	ContractCallOther       ContractCallType = "contract_other"        // Other contract calls
)

// ContractCallData contract call data
type ContractCallData struct {
	CallType     ContractCallType `json:"call_type"`
	TokenAddress string           `json:"token_address,omitempty"` // Token address
	Amount       string           `json:"amount,omitempty"`        // Single amount
	Recipients   []string         `json:"recipients,omitempty"`    // Batch recipients
	Froms        []string         `json:"froms,omitempty"`         // Batch senders
	Tokens       []string         `json:"tokens,omitempty"`        // Batch tokens
	Amounts      []string         `json:"amounts,omitempty"`       // Batch amounts
	RawMethodSig string           `json:"raw_method_sig"`          // Raw method signature
}

// parseERC20Transfer parse transfer(address to, uint256 amount) - ERC20 transfer classified as EOA transfer
func (p *EVMParser) parseERC20Transfer(tx *types.Transaction, data []byte, callData *ContractCallData) (*ContractCallData, error) {
	if len(data) < 64 {
		return nil, fmt.Errorf("transfer parameters length is insufficient")
	}

	callData.CallType = ContractCallEOATransfer
	// ERC20 contract address is tx.To()
	callData.TokenAddress = tx.To().Hex()
	// amount (last 32 bytes)
	callData.Amount = new(big.Int).SetBytes(data[32:64]).String()

	return callData, nil
}

// parseDeposit parse deposit(address token, uint256 amount)
func (p *EVMParser) parseDeposit(data []byte, callData *ContractCallData) (*ContractCallData, error) {
	if len(data) < 64 {
		return nil, fmt.Errorf("deposit parameters length is insufficient")
	}

	callData.CallType = ContractCallDeposit
	// token address (first 32 bytes, take last 20 bytes)
	callData.TokenAddress = common.BytesToAddress(data[12:32]).Hex()
	// amount (last 32 bytes)
	callData.Amount = new(big.Int).SetBytes(data[32:64]).String()

	return callData, nil
}

// parseBatchPayout parse batchTransferPayout(address[] recipients, address[] tokens, uint256[] amounts)
func (p *EVMParser) parseBatchPayout(data []byte, callData *ContractCallData) (*ContractCallData, error) {
	callData.CallType = ContractCallBatchPayout

	// Parsing dynamic arrays requires more complex logic
	// TODO: Use proper ABI decoding

	return callData, nil
}

// parseDepositETH parse depositETH() - payable function
func (p *EVMParser) parseDepositETH(tx *types.Transaction, callData *ContractCallData) (*ContractCallData, error) {
	callData.CallType = ContractCallDepositETH
	callData.TokenAddress = "0x0" // ETH
	callData.Amount = tx.Value().String()

	return callData, nil
}

// parseWithdrawETH parse withdrawETH(uint256 amount)
func (p *EVMParser) parseWithdrawETH(data []byte, callData *ContractCallData) (*ContractCallData, error) {
	if len(data) < 32 {
		return nil, fmt.Errorf("withdrawETH parameters length is insufficient")
	}

	callData.CallType = ContractCallWithdrawETH
	callData.TokenAddress = "0x0" // ETH
	callData.Amount = new(big.Int).SetBytes(data[:32]).String()

	return callData, nil
}

// TransactionData transaction data structure
type TransactionData struct {
	Hash            string     `json:"hash"`
	From            string     `json:"from"`
	To              string     `json:"to"`
	Value           string     `json:"value"`
	GasLimit        uint64     `json:"gas_limit"`
	GasUsed         uint64     `json:"gas_used"`
	GasPrice        string     `json:"gas_price"`
	Nonce           uint64     `json:"nonce"`
	InputData       string     `json:"input_data"`
	Status          uint64     `json:"status"`
	ContractAddress string     `json:"contract_address,omitempty"`
	BlockNumber     uint64     `json:"block_number"`
	BlockHash       string     `json:"block_hash"`
	Timestamp       uint64     `json:"timestamp"`
	Logs            []*LogData `json:"logs,omitempty"`
}

// LogData log data structure
type LogData struct {
	Address string   `json:"address"`
	Topics  []string `json:"topics"`
	Data    string   `json:"data"`
	Index   uint     `json:"index"`
}

// ERC20TransferData ERC20 transfer data
type ERC20TransferData struct {
	Token  string `json:"token"`
	From   string `json:"from"`
	To     string `json:"to"`
	Amount string `json:"amount"`
}

// ToJSON convert to JSON string
func (td *TransactionData) ToJSON() (string, error) {
	data, err := json.Marshal(td)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// ParseEventSignature parse event signature string
func ParseEventSignature(signature string) (common.Hash, error) {
	signature = strings.TrimSpace(signature)
	if strings.HasPrefix(signature, "0x") {
		return common.HexToHash(signature), nil
	}
	// If it's a function signature form, need to compute Keccak256
	return common.Hash{}, fmt.Errorf("please provide the full event hash")
}
