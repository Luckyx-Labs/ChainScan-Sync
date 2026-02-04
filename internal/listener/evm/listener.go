package evm

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Luckyx-Labs/chainscan-sync/internal/config"
	"github.com/Luckyx-Labs/chainscan-sync/internal/interfaces"
	"github.com/Luckyx-Labs/chainscan-sync/internal/parser"
	appTypes "github.com/Luckyx-Labs/chainscan-sync/internal/types"
	"github.com/Luckyx-Labs/chainscan-sync/pkg/logger"
	"github.com/bits-and-blooms/bloom/v3"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
	lru "github.com/hashicorp/golang-lru/v2"
)

// LRU cache size constants
const (
	// Contract address cache capacity (approximately 200,000 addresses, about 50 bytes each, totaling about 10MB)
	ContractCacheSize = 200000
)

// EVMListener EVM chain listener
type EVMListener struct {
	config               config.EVMChainConfig
	retryConfig          config.RetryConfig
	client               *ethclient.Client  // Retained for compatibility (points to the current active endpoint)
	rpcClient            *rpc.Client        // Retained for compatibility (points to the current active endpoint)
	failoverClient       *FailoverRPCClient // Supports failover RPC client
	wsClient             *ethclient.Client
	contracts            map[common.Address]*ContractInfo
	parser               *parser.EVMParser
	errorChan            chan error
	status               *appTypes.ProcessStatus
	ctx                  context.Context
	cancel               context.CancelFunc
	wg                   sync.WaitGroup
	mu                   sync.RWMutex
	storage              interfaces.Storage       // Used for checkpoint resuming
	notifier             interfaces.Notifier      // Used for rollback notifications
	bloomFilter          *bloom.BloomFilter       // Bloom filter (quickly exclude unmonitored addresses)
	addressCache         sync.Map                 // addresses cache (key: string, value: bool)
	contractCache        *lru.Cache[string, bool] // Contract address LRU cache (with capacity limit to avoid memory bloat)
	monitorAddrSize      uint                     // Number of monitored addresses (for logging)
	isReorging           bool                     // Whether reorganization is being processed (used to pause new block scanning)
	reorgMu              sync.Mutex               // Reorganization state lock
	isReloadingAddresses bool                     // Whether addresses are being reloaded (used to pause new block scanning)
	reloadMu             sync.Mutex               // Address reload lock
	withdrawEOA          string                   // Withdraw EOA address from config
}

// ContractInfo Contract information
type ContractInfo struct {
	Address common.Address
	Name    string
	ABI     abi.ABI
	Events  map[string]abi.Event // event name -> event
}

// NewEVMListener creates a new EVMListener
func NewEVMListener(cfg config.EVMChainConfig, retryCfg config.RetryConfig, withdrawEOA string) (interfaces.ChainListener, error) {
	// Create contract address LRU cache
	contractCache, err := lru.New[string, bool](ContractCacheSize)
	if err != nil {
		return nil, fmt.Errorf("failed to create contract cache: %w", err)
	}

	listener := &EVMListener{
		config:        cfg,
		retryConfig:   retryCfg,
		contracts:     make(map[common.Address]*ContractInfo),
		parser:        parser.NewEVMParser(),
		errorChan:     make(chan error, 10),
		contractCache: contractCache,
		withdrawEOA:   strings.ToLower(withdrawEOA),
		status: &appTypes.ProcessStatus{
			ChainType: appTypes.ChainTypeEVM,
			ChainName: cfg.Name,
			IsRunning: false,
		},
	}

	// Load contract abi
	if err := listener.loadContracts(); err != nil {
		return nil, fmt.Errorf("failed to load contracts: %w", err)
	}

	return listener, nil
}

// loadContracts loads contract information
func (l *EVMListener) loadContracts() error {
	for _, contractCfg := range l.config.Contracts {
		address := common.HexToAddress(contractCfg.Address)

		// Read ABI file
		abiData, err := os.ReadFile(contractCfg.ABIPath)
		if err != nil {
			logger.Warnf("Failed to read ABI file %s: %v, skipping contract", contractCfg.ABIPath, err)
			continue
		}

		// Parse ABI
		contractABI, err := abi.JSON(strings.NewReader(string(abiData)))
		if err != nil {
			return fmt.Errorf("failed to parse ABI %s: %w", contractCfg.Name, err)
		}

		// Build event mapping
		events := make(map[string]abi.Event)
		for _, eventName := range contractCfg.Events {
			event, ok := contractABI.Events[eventName]
			if !ok {
				logger.Warnf("Event %s not found in contract %s", eventName, contractCfg.Name)
				continue
			}
			events[eventName] = event
		}

		l.contracts[address] = &ContractInfo{
			Address: address,
			Name:    contractCfg.Name,
			ABI:     contractABI,
			Events:  events,
		}

		logger.Infof("Contract loaded: %s (%s), monitoring events: %v",
			contractCfg.Name, address.Hex(), contractCfg.Events)
	}

	if len(l.contracts) == 0 {
		return fmt.Errorf("no valid contract configuration found")
	}

	return nil
}

// SetStorage sets the storage (used for breakpoint resume)
func (l *EVMListener) SetStorage(storage interfaces.Storage) {
	l.storage = storage
	// Load monitored addresses from the database
	if err := l.loadMonitorAddressesFromDB(); err != nil {
		logger.Errorf("Failed to load monitor addresses from database: %v", err)
	}
}

// SetNotifier sets the notifier (used for rollback notifications)
func (l *EVMListener) SetNotifier(n interfaces.Notifier) {
	l.notifier = n
}

// Start starts the listener
func (l *EVMListener) Start(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.status.IsRunning {
		return fmt.Errorf("listener is already running")
	}

	l.ctx, l.cancel = context.WithCancel(ctx)

	// Connect to RPC node (using failover client)
	if err := l.connect(); err != nil {
		return fmt.Errorf("failed to connect to RPC node: %w", err)
	}

	l.status.IsRunning = true
	l.status.LastUpdated = time.Now()

	// Start listening
	l.wg.Add(1)
	go l.listen()

	logger.Infof("EVM listener started successfully: %s (ChainID: %d)", l.config.Name, l.config.ChainID)
	return nil
}

// Stop stops the listener
func (l *EVMListener) Stop() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if !l.status.IsRunning {
		return nil
	}

	if l.cancel != nil {
		l.cancel()
	}

	l.wg.Wait()

	// Close failover client (will close all endpoint connections)
	if l.failoverClient != nil {
		l.failoverClient.Close()
	}
	if l.wsClient != nil {
		l.wsClient.Close()
	}

	close(l.errorChan)

	l.status.IsRunning = false
	l.status.LastUpdated = time.Now()

	logger.Infof("EVM listener stopped: %s", l.config.Name)
	return nil
}

// GetStatus gets the listener status
func (l *EVMListener) GetStatus() *appTypes.ProcessStatus {
	l.mu.RLock()
	defer l.mu.RUnlock()

	statusCopy := *l.status
	return &statusCopy
}

// GetChainType gets the chain type
func (l *EVMListener) GetChainType() appTypes.ChainType {
	return appTypes.ChainTypeEVM
}

// GetChainName gets the chain name
func (l *EVMListener) GetChainName() string {
	return l.config.Name
}

// GetErrors gets the error channel
func (l *EVMListener) GetErrors() <-chan error {
	return l.errorChan
}

// updateStatus updates the status
func (l *EVMListener) updateStatus(blockNumber uint64) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.status.LastBlock = blockNumber
	l.status.LastUpdated = time.Now()
}
