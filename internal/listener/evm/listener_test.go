package evm

import (
	"context"
	"math/big"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Luckyx-Labs/chainscan-sync/internal/config"
	storage "github.com/Luckyx-Labs/chainscan-sync/internal/database"
	appTypes "github.com/Luckyx-Labs/chainscan-sync/internal/types"
	"github.com/bits-and-blooms/bloom/v3"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
)

// MockStorage mock storage for testing
type MockStorage struct {
	addresses []string
}

func (m *MockStorage) GetAllMonitorAddresses(ctx context.Context) ([]string, error) {
	return m.addresses, nil
}

// Implement other required interface methods (empty implementations)
func (m *MockStorage) SaveEvent(ctx context.Context, event *appTypes.Event) error {
	return nil
}

func (m *MockStorage) GetLastBlock(ctx context.Context, chainType appTypes.ChainType, chainName string) (uint64, string, error) {
	return 0, "", nil
}

func (m *MockStorage) RollbackToBlock(ctx context.Context, chainType appTypes.ChainType, chainName string, blockNumber uint64) error {
	return nil
}

func (m *MockStorage) IsAddressMonitored(ctx context.Context, address string) (bool, error) {
	addrLower := strings.ToLower(address)
	for _, addr := range m.addresses {
		if strings.ToLower(addr) == addrLower {
			return true, nil
		}
	}
	return false, nil
}

func (m *MockStorage) UpdateAddressBalance(ctx context.Context, chainType, chainName, address, tokenAddress, balance string, blockNumber uint64, txHash string) error {
	return nil
}

func (m *MockStorage) GetAddressBalance(ctx context.Context, chainType, chainName, address, tokenAddress string) (string, error) {
	return "0", nil
}

func (m *MockStorage) GetAddressInfo(ctx context.Context, chainName, address string) (interface{}, error) {
	return nil, nil
}

func (m *MockStorage) GetEvent(ctx context.Context, eventID string) (*appTypes.Event, error) {
	return nil, nil
}

func (m *MockStorage) IsEventProcessed(ctx context.Context, eventID string) (bool, error) {
	return false, nil
}

func (m *MockStorage) GetEventsByChain(ctx context.Context, chainType appTypes.ChainType, chainName string, limit int) ([]*appTypes.Event, error) {
	return nil, nil
}

func (m *MockStorage) GetUnprocessedEvents(ctx context.Context, limit int) ([]*appTypes.Event, error) {
	return nil, nil
}

func (m *MockStorage) GetEventsByContract(ctx context.Context, contractAddr string, limit int) ([]*appTypes.Event, error) {
	return nil, nil
}

func (m *MockStorage) SaveWithdraw(ctx context.Context, withdraw interface{}) error {
	return nil
}

func (m *MockStorage) SaveDeposit(ctx context.Context, deposit interface{}) error {
	return nil
}

func (m *MockStorage) UpdateWithdrawNotifyStatus(ctx context.Context, txHash, status string, notifyError string) error {
	return nil
}

func (m *MockStorage) UpdateDepositNotifyStatus(ctx context.Context, txHash, status string, notifyError string) error {
	return nil
}

func (m *MockStorage) GetPendingWithdraws(ctx context.Context, limit int) ([]interface{}, error) {
	return nil, nil
}

func (m *MockStorage) GetPendingDeposits(ctx context.Context, limit int) ([]interface{}, error) {
	return nil, nil
}

func (m *MockStorage) GetPendingNotifyTxHashes(ctx context.Context, limit int) ([]string, error) {
	return nil, nil
}

func (m *MockStorage) GetEventsByTxHash(ctx context.Context, txHash string) ([]*appTypes.Event, error) {
	return nil, nil
}

func (m *MockStorage) UpdateEventsNotifyStatus(ctx context.Context, txHash, status string, notifyError string) error {
	return nil
}

func (m *MockStorage) GetFailedNotifyTxHashes(ctx context.Context, limit int) ([]string, error) {
	return nil, nil
}

func (m *MockStorage) QueryBlocks(ctx context.Context, params map[string]interface{}, page, pageSize int) ([]interface{}, int64, error) {
	return nil, 0, nil
}

func (m *MockStorage) QueryTransactions(ctx context.Context, params map[string]interface{}, page, pageSize int) ([]interface{}, int64, error) {
	return nil, 0, nil
}

func (m *MockStorage) QueryEvents(ctx context.Context, params map[string]interface{}, page, pageSize int) ([]interface{}, int64, error) {
	return nil, 0, nil
}

func (m *MockStorage) QueryAddresses(ctx context.Context, params map[string]interface{}, page, pageSize int) ([]interface{}, int64, error) {
	return nil, 0, nil
}

func (m *MockStorage) QueryBalances(ctx context.Context, params map[string]interface{}, page, pageSize int) ([]interface{}, int64, error) {
	return nil, 0, nil
}

func (m *MockStorage) QueryBalanceHistory(ctx context.Context, address, tokenAddress string, params map[string]interface{}, page, pageSize int) ([]interface{}, int64, error) {
	return nil, 0, nil
}

func (m *MockStorage) GetBlockByNumber(ctx context.Context, chainName string, blockNumber uint64) (interface{}, error) {
	return nil, nil
}

func (m *MockStorage) GetTransactionByHash(ctx context.Context, txHash string) (interface{}, error) {
	return nil, nil
}

func (m *MockStorage) GetStatistics(ctx context.Context) (map[string]interface{}, error) {
	return nil, nil
}

func (m *MockStorage) GetChainStatistics(ctx context.Context, chainName string) (map[string]interface{}, error) {
	return nil, nil
}

func (m *MockStorage) Close() error {
	return nil
}

func (m *MockStorage) GetNotifiedEventsAfterBlock(ctx context.Context, chainType appTypes.ChainType, chainName string, blockNumber uint64) ([]*appTypes.Event, error) {
	return nil, nil
}

func (m *MockStorage) UpdateUnconfirmedBlocksConfirmations(ctx context.Context, chainType string, chainName string, latestBlockNumber uint64, requiredConfirmations uint64) (int64, error) {
	return 0, nil
}

// TestLoadMonitorAddressesFromDB tests loading monitored addresses from the database
func TestLoadMonitorAddressesFromDB(t *testing.T) {
	// Test address list (including target address)
	testAddresses := []string{
		"0x03fB5a438cEa45Ca0D438d863F6829012F6e430B", // Target test address
		"0x742d35Cc6634C0532925a3b844Bc9e7595f0bEb",
		"0xdAC17F958D2ee523a2206206994597C13D831ec7",
	}

	// Create mock storage
	mockStorage := &MockStorage{
		addresses: testAddresses,
	}

	// Create test listener
	listener := &EVMListener{
		storage: mockStorage,
	}

	// Test loading monitored addresses
	t.Run("load monitor addresses", func(t *testing.T) {
		err := listener.loadMonitorAddressesFromDB()
		if err != nil {
			t.Fatalf("Failed to load monitored addresses: %v", err)
		}

		// Verify address count
		if listener.monitorAddrSize != uint(len(testAddresses)) {
			t.Errorf("Incorrect number of monitored addresses: expected %d, got %d", len(testAddresses), listener.monitorAddrSize)
		}

		// Verify bloom filter is created
		if listener.bloomFilter == nil {
			t.Fatal("Bloom filter not created")
		}

		t.Logf("Successfully loaded %d monitored addresses", listener.monitorAddrSize)
	})

	// Test if specific address 0x03fB5a438cEa45Ca0D438d863F6829012F6e430B is in the bloom filter
	t.Run("verify 0x03fB5a438cEa45Ca0D438d863F6829012F6e430B is loaded", func(t *testing.T) {
		targetAddress := "0x03fB5a438cEa45Ca0D438d863F6829012F6e430B"
		targetAddressLower := strings.ToLower(targetAddress)

		// Directly test bloom filter
		if !listener.bloomFilter.TestString(targetAddressLower) {
			t.Fatalf("Address %s not found in bloom filter", targetAddress)
		}

		t.Logf("Address %s successfully loaded into bloom filter", targetAddress)
		// Also test using common.Address type
		addr := common.HexToAddress(targetAddress)
		addrHex := strings.ToLower(addr.Hex())
		if !listener.bloomFilter.TestString(addrHex) {
			t.Errorf("Address converted through common.Address not found in bloom filter")
		}
	})

	// Test if all addresses are in the bloom filter
	// t.Run("verify all addresses are loaded", func(t *testing.T) {
	// 	for _, addr := range testAddresses {
	// 		addrLower := strings.ToLower(addr)
	// 		if !listener.bloomFilter.TestString(addrLower) {
	// 			t.Errorf("Address %s not found in bloom filter", addr)
	// 		}
	// 	}
	// 	t.Logf("All %d addresses successfully loaded", len(testAddresses))
	// })

}

// TestBloomFilterMemoryUsage tests bloom filter memory usage
func TestBloomFilterMemoryUsage(t *testing.T) {
	testCases := []struct {
		name          string
		addressCount  int
		expectedMaxMB float64
	}{
		{"100 addresses", 100, 0.1},
		{"1000 addresses", 1000, 0.5},
		{"10000 addresses", 10000, 2.0},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			addresses := make([]string, tc.addressCount)
			for i := 0; i < tc.addressCount; i++ {
				addresses[i] = common.HexToAddress(strings.Repeat("0", 38) + string(rune('a'+i%26)) + string(rune('0'+i%10))).Hex()
			}

			mockStorage := &MockStorage{addresses: addresses}
			listener := &EVMListener{storage: mockStorage}

			err := listener.loadMonitorAddressesFromDB()
			if err != nil {
				t.Fatalf("Failed to load monitored addresses: %v", err)
			}

			memoryBytes := listener.bloomFilter.Cap()
			memoryMB := float64(memoryBytes) / 1024 / 1024

			t.Logf("Memory usage for %d addresses: %.3f MB", tc.addressCount, memoryMB)

			if memoryMB > tc.expectedMaxMB {
				t.Logf("⚠ Memory usage %.3f MB exceeds expected %.3f MB", memoryMB, tc.expectedMaxMB)
			}
		})
	}
}

// BenchmarkBloomFilterLookup Benchmark test: bloom filter lookup performance
func BenchmarkBloomFilterLookup(b *testing.B) {
	// Create bloom filter and add 1000000 addresses
	bf := bloom.NewWithEstimates(1000, 0.001)
	for i := 0; i < 1000000; i++ {
		addr := strings.ToLower(common.HexToAddress(strings.Repeat("0", 38) + string(rune('a'+i%26)) + string(rune('0'+i%10))).Hex())
		bf.AddString(addr)
	}

	testAddr := strings.ToLower("0x03fB5a438cEa45Ca0D438d863F6829012F6e430B")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bf.TestString(testAddr)
	}
	// Test result: 43081628                27.97 ns/op            0 B/op          0 allocs/op
}

// ====================================================================  Testing native token transfer handling =============================================================

// MockPostgresStorage simulates PostgresStorage (for testing handleNativeTransfer)
type MockPostgresStorage struct {
	MockStorage
	SavedWithdraws []*MockWithdraw
	SavedDeposits  []*MockDeposit
}

type MockWithdraw struct {
	ChainType     string
	ChainName     string
	TxHash        string
	BlockNumber   uint64
	BlockHash     string
	FromAddress   string
	ToAddress     string
	TokenAddress  string
	Amount        string
	GasUsed       uint64
	GasFee        string
	Status        uint64
	Confirmations uint64
	IsFinalized   bool
}

type MockDeposit struct {
	ChainType     string
	ChainName     string
	TxHash        string
	BlockNumber   uint64
	BlockHash     string
	FromAddress   string
	ToAddress     string
	TokenAddress  string
	Amount        string
	GasUsed       uint64
	GasFee        string
	Status        uint64
	Confirmations uint64
	IsFinalized   bool
}

func (m *MockPostgresStorage) SaveWithdraw(ctx context.Context, withdraw interface{}) error {
	m.SavedWithdraws = append(m.SavedWithdraws, &MockWithdraw{
		TxHash:        "mock_withdraw",
		Amount:        "1000000000000000000", // 1 ETH
		TokenAddress:  "0x0",
		FromAddress:   "0x03fB5a438cEa45Ca0D438d863F6829012F6e430B",
		ToAddress:     "0x742d35Cc6634C0532925a3b844Bc9e7595f0bEb",
		Confirmations: 0,
		IsFinalized:   false,
	})
	return nil
}

func (m *MockPostgresStorage) SaveDeposit(ctx context.Context, deposit interface{}) error {
	m.SavedDeposits = append(m.SavedDeposits, &MockDeposit{
		TxHash:       "mock_deposit",
		Amount:       "1000000000000000000", // 1 ETH
		TokenAddress: "0x0",
		FromAddress:  "0x742d35Cc6634C0532925a3b844Bc9e7595f0bEb",
		ToAddress:    "0x03fB5a438cEa45Ca0D438d863F6829012F6e430B",

		Confirmations: 0,
		IsFinalized:   false,
	})
	return nil
}

// NOTE: TestHandleNativeTransfer has been removed because handleNativeTransfer function
// was deprecated and replaced by processNativeTransfersWithStandardRPC in listener_native_transfer.go

func TestProcessBlockTest(t *testing.T) {

	// Create test context
	ctx := context.Background()

	// Get database connection string from environment variable
	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		dsn = "host=192.168.1.xxxx user=postgres password=xxxx dbname=game port=5432 sslmode=disable TimeZone=Asia/Shanghai"
	}

	// Create real database storage
	storageInterface, err := storage.NewPostgresStorage(dsn)
	if err != nil {
		t.Fatalf("Failed to connect to database: %v", err)
	}
	defer storageInterface.Close()

	// Get RPC URL
	rpcURL := "https://base-sepolia.g.alchemy.com/v2/xxxx"

	// Initialize EVMListener - test contract event listener
	cfg := config.EVMChainConfig{
		Name:          "base-sepolia",
		ChainID:       84532, // Base Sepolia testnet
		RPCURL:        rpcURL,
		Confirmations: 12,
		Contracts: []config.ContractConfig{
			{
				Address: "0x3c18534C24055Ce2622ed77C0C0307c0D68c608e",
				Name:    "AssetPoolManager",
				ABIPath: "./AssetPoolManager.json",
				Events: []string{
					"Deposit",
					"Withdraw",
					"ETHDeposit",
					"ETHWithdraw",
					"BatchPayinItem",
				},
			},
			{
				Address: "0x522A377055E02A9054204e766b2EAaB1AB6252FF",
				Name:    "USDT",
				ABIPath: "./ERC20.json",
				Events: []string{
					"Transfer",
					"Approval",
				},
			},
		},
	}

	retryCfg := config.RetryConfig{
		MaxAttempts:     3,
		InitialInterval: time.Second,
		MaxInterval:     time.Second * 10,
		Multiplier:      1.5,
	}

	// create EVM listener
	listener, err := NewEVMListener(cfg, retryCfg)
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}

	evmListener := listener.(*EVMListener)
	evmListener.SetStorage(storageInterface)
	evmListener.ctx = ctx

	// connect to RPC node
	if err := evmListener.connect(); err != nil {
		t.Fatalf("Failed to connect to RPC node: %v", err)
	}

	// load monitored addresses
	if err := evmListener.loadMonitorAddressesFromDB(); err != nil {
		t.Fatalf("Failed to load monitored addresses: %v", err)

	}

	t.Logf("EVMListener initialized successfully")
	t.Logf("  - Chain Name: %s", cfg.Name)
	t.Logf("  - Chain ID: %d", cfg.ChainID)
	t.Logf("  - RPC URL: %s", rpcURL)
	t.Logf("  - Monitored Addresses Count: %d", evmListener.monitorAddrSize)

	// Get latest block number
	latestBlock, err := evmListener.client.BlockNumber(ctx)
	if err != nil {
		t.Fatalf("Failed to get latest block: %v", err)
	}
	t.Logf("  - Latest Block on Chain: %d", latestBlock)

	// Test processing a specific block
	t.Run("Process Single Block", func(t *testing.T) {
		// blockNumber := 9606919 // deposit event block 				✔
		// blockNumber := 9619584 // withdraw event block 				✔
		// blockNumber := 9660476 // ETHDeposit event block        	✔
		// blockNumber := 9619771 // ETHWithdraw event block       	✔
		// blockNumber := 9620240 // BatchPayinItem event block           ✔
		// blockNumber := 9620000 // BatchTransfer payout event block   ✔
		blockNumber := 9674329 // USDT Transfer event block       	✔

		err := evmListener.processBlock(uint64(blockNumber))
		if err != nil {
			errMsg := err.Error()
			t.Logf("Failed to process block: %v", err)

			// Check if it's a reorg error
			if strings.Contains(errMsg, "reorg:") {
				t.Logf("Detected reorg error, manually fetching block details and processing")

				// Get full block data
				block, err := evmListener.client.BlockByNumber(ctx, big.NewInt(int64(blockNumber)))
				if err != nil {

					t.Fatalf("Failed to get block details: %v", err)
				}

				t.Logf("Block details: Number=%d, Hash=%s, ParentHash=%s, TxCount=%d",
					block.Number().Uint64(), block.Hash().Hex(), block.ParentHash().Hex(), len(block.Transactions()))

				// 1. Process transactions in the block
				// for _, tx := range block.Transactions() {
				// 	txHash := tx.Hash().Hex()
				// 	from, _ := types.Sender(types.LatestSignerForChainID(new(big.Int).SetUint64(11155111)), tx)

				// 	t.Logf("  Transaction: %s, from=%s, to=%v", txHash, from.Hex(), tx.To())

				// 	if evmListener.shouldProcessTransaction(from, tx.To()) {
				// 		t.Logf("    → Relevant transaction, processing...")
				// 		if err := evmListener.processTransaction(tx, block); err != nil {
				// 			t.Errorf("    Failed to process transaction: %v", err)
				// 		} else {
				// 			t.Logf("    Transaction processed successfully")
				// 		}
				// 	}
				// }

				// 2. Query and process contract event logs
				if len(evmListener.contracts) > 0 {
					t.Logf("Querying contract events...")
					addresses := make([]common.Address, 0, len(evmListener.contracts))
					for addr := range evmListener.contracts {
						addresses = append(addresses, addr)
						t.Logf("  Monitoring contract: %s", addr.Hex())
					}

					query := ethereum.FilterQuery{
						FromBlock: big.NewInt(int64(blockNumber)),
						ToBlock:   big.NewInt(int64(blockNumber)),
						Addresses: addresses,
					}

					logs, err := evmListener.client.FilterLogs(ctx, query)
					if err != nil {
						t.Errorf("Failed to query logs: %v", err)
					} else {
						t.Logf("  Found %d event logs", len(logs))
						for i, vLog := range logs {
							t.Logf("  [%d] TxHash=%s, LogIndex=%d, Address=%s, Topics=%d",
								i, vLog.TxHash.Hex(), vLog.Index, vLog.Address.Hex(), len(vLog.Topics))

							if err := evmListener.processLog(vLog); err != nil {
								t.Errorf("    Failed to process log: %v", err)
							} else {
								t.Logf("    Log processed successfully")
							}
						}
					}
				}
			} else {
				t.Errorf("Failed to process block: %v", err)
			}
		} else {
			t.Logf("Block %d processed successfully", blockNumber)
		}
	})
}
