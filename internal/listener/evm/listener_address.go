package evm

import (
	"context"
	"strings"
	"sync"
	"time"

	appTypes "github.com/Luckyx-Labs/chainscan-sync/internal/types"
	"github.com/Luckyx-Labs/chainscan-sync/pkg/logger"
	"github.com/bits-and-blooms/bloom/v3"
	"github.com/ethereum/go-ethereum/common"
)

// loadMonitorAddressesFromDB load monitored addresses from the database into the bloom filter
// Note: You must acquire the l.mu lock before calling this method
func (l *EVMListener) loadMonitorAddressesFromDB() error {
	if l.storage == nil {
		return nil
	}

	// Clear address cache (important! Otherwise, old cache will cause new addresses to be ineffective)
	l.addressCache = sync.Map{}

	// Get all monitored addresses
	addresses, err := l.storage.GetAllMonitorAddresses(context.Background())
	if err != nil {
		return err
	}

	// Record the number of addresses
	oldSize := l.monitorAddrSize
	l.monitorAddrSize = uint(len(addresses))

	// If there are no monitored addresses, return directly
	if l.monitorAddrSize == 0 {
		if oldSize > 0 {
			logger.Warnf("Monitor addresses cleared: %d -> 0, will process all transactions", oldSize)
		} else {
			logger.Info("No monitor addresses, will process all transactions")
		}
		l.bloomFilter = nil
		return nil
	}

	// Create bloom filter
	// Parameters: number of elements, desired false positive rate (1% = 0.01)
	// 1 million addresses, 1% false positive rate ≈ 1.2MB memory
	// 1 million addresses, 0.1% false positive rate ≈ 1.8MB memory
	l.bloomFilter = bloom.NewWithEstimates(l.monitorAddrSize, 0.001)

	// Load addresses into bloom filter
	validCount := 0
	for _, addr := range addresses {
		if common.IsHexAddress(addr) {
			// Store uniformly in lowercase
			addrLower := strings.ToLower(addr)
			l.bloomFilter.AddString(addrLower)
			validCount++
		} else {
			logger.Warnf("Invalid address format: %s", addr)
		}
	}

	// Calculate actual memory usage
	memoryBytes := l.bloomFilter.Cap()
	memoryMB := float64(memoryBytes) / 1024 / 1024

	if oldSize > 0 {
		// Reloaded
		logger.Infof("Reloaded monitor addresses: %d -> %d (memory: %.2f MB, cache cleared)",
			oldSize, validCount, memoryMB)
	} else {
		// Initialized
		logger.Infof("Loaded %d monitor addresses into bloom filter (memory usage: %.2f MB, false positive rate: 0.1%%)",
			validCount, memoryMB)
	}

	return nil
}

// ReloadMonitorAddresses Safely reload monitored addresses (supports dynamic updates)
// This method will:
// 1. Pause block scanning (to avoid race conditions)
// 2. Wait for the current block to complete
// 3. Clear address cache
// 4. Reload all monitored addresses from the database
// 5. Rebuild bloom filter
// 6. Resume block scanning
func (l *EVMListener) ReloadMonitorAddresses() error {
	logger.Infof("[%s] Starting safe reload of monitor addresses...", l.config.Name)

	// 1. Set reload flag, pause scanning of new blocks
	l.reloadMu.Lock()
	l.isReloadingAddresses = true
	l.reloadMu.Unlock()

	logger.Debugf("[%s] Block scanning paused, waiting for current block to complete...", l.config.Name)

	// 2. Wait a short time to ensure the current block processing is complete
	// Note: Using a short wait here to avoid long blocking
	time.Sleep(100 * time.Millisecond)

	// 3. Acquire write lock and reload addresses
	l.mu.Lock()
	err := l.loadMonitorAddressesFromDB()
	l.mu.Unlock()

	// 4. Clear reload flag, resume block scanning
	l.reloadMu.Lock()
	l.isReloadingAddresses = false
	l.reloadMu.Unlock()

	if err != nil {
		logger.Errorf("[%s] Failed to reload monitor addresses: %v", l.config.Name, err)
		return err
	}

	logger.Infof("[%s] Block scanning resumed", l.config.Name)
	return nil
}

// shouldProcessTransaction Determine whether to process the transaction (address filtering)
func (l *EVMListener) shouldProcessTransaction(from common.Address, to *common.Address) bool {
	// Acquire read lock to protect bloomFilter reading
	l.mu.RLock()
	bloomFilter := l.bloomFilter
	l.mu.RUnlock()

	// If no monitored addresses are set, process all transactions
	if bloomFilter == nil {
		return true
	}

	// Check if the from address is monitored
	if l.isAddressMonitored(from) {
		return true
	}

	// Check if the to address is monitored
	if to != nil && l.isAddressMonitored(*to) {
		return true
	}

	return false
}

// isAddressMonitored Check if a single address is monitored (optimized with bloom filter)
func (l *EVMListener) isAddressMonitored(addr common.Address) bool {
	// Acquire read lock to protect bloomFilter reading
	l.mu.RLock()
	bloomFilter := l.bloomFilter
	l.mu.RUnlock()

	// If no bloom filter is set, all addresses are monitored
	if bloomFilter == nil {
		return true
	}

	// Convert to lowercase uniformly
	addrLower := strings.ToLower(addr.Hex())

	// 1. Check cache first (to avoid repeated database queries)
	if cached, ok := l.addressCache.Load(addrLower); ok {
		return cached.(bool)
	}

	// 2. Use bloom filter for quick exclusion (100% not present)
	if !bloomFilter.TestString(addrLower) {
		l.addressCache.Store(addrLower, false)
		return false
	}

	// 3. Possible presence (0.01% false positive), check database to confirm
	exists, err := l.storage.IsAddressMonitored(context.Background(), addrLower)
	if err != nil {
		logger.Errorf("Failed to query if address is monitored %s: %v", addrLower, err)
		// Conservatively handle errors by returning true to avoid data loss
		return true
	}

	// Cache the result
	l.addressCache.Store(addrLower, exists)
	return exists
}

// isContractAddress Check if an address is a contract address (with LRU cache)
// Cache capacity is limited to 100,000 addresses, automatically evicting the least recently used entries when exceeded
// Note: EIP-7702 authorized EOA addresses have delegated code (0xef0100 + 20-byte address), but are still considered EOAs
func (l *EVMListener) isContractAddress(addr common.Address) bool {
	addrLower := strings.ToLower(addr.Hex())

	// 1. Check LRU cache first to avoid repeated RPC calls
	if cached, ok := l.contractCache.Get(addrLower); ok {
		return cached
	}

	// 2. Use client.CodeAt to check if the address has contract code
	// If the returned bytecode length is greater than 0, it indicates a contract address
	code, err := l.client.CodeAt(l.ctx, addr, nil) // nil means using the latest block
	if err != nil {
		logger.Debugf("Failed to get code at address %s: %v", addr.Hex(), err)
		// If the query fails, assume it is a contract address for safety (to avoid misprocessing)
		// Do not cache the failure result, retry next time
		return true
	}

	var isContract bool

	// EIP-7702: Check if it is authorized delegation code
	// Format: 0xef0100 + 20-byte address = 23 bytes
	if len(code) == 23 && code[0] == 0xef && code[1] == 0x01 && code[2] == 0x00 {
		logger.Debugf("Address %s has EIP-7702 delegation code (delegated to %s), treating as EOA",
			addr.Hex(), common.BytesToAddress(code[3:]).Hex())
		isContract = false // EIP-7702 delegated addresses are still EOAs
	} else {
		// If there is bytecode, it is a contract; otherwise, it is an EOA
		isContract = len(code) > 0
	}

	// Add to LRU cache (automatically evicting the least recently used entries)
	l.contractCache.Add(addrLower, isContract)
	logger.Debugf("Address %s isContract: %v (code length: %d, cached, cache size: %d)",
		addr.Hex(), isContract, len(code), l.contractCache.Len())

	return isContract
}

// isMonitoredContract Check if an address is a monitored contract address
// Used to identify business contracts like AssetPoolManager, whose Transfer events should be skipped
func (l *EVMListener) isMonitoredContract(addr common.Address) bool {
	l.mu.RLock()
	defer l.mu.RUnlock()

	_, exists := l.contracts[addr]
	return exists
}

// isEventRelatedToMonitoredAddresses Check if event data contains monitored addresses
func (l *EVMListener) isEventRelatedToMonitoredAddresses(event *appTypes.Event) bool {
	// Special handling: Withdraw and ETHWithdraw events
	// These events do not require checking if the user is in the monitored list; process all withdrawals directly
	if event.EventName == "Withdraw" || event.EventName == "ETHWithdraw" {
		logger.Debugf("Processing %s event without user address check", event.EventName)
		return true
	}

	// Special handling: ERC20 Transfer events
	// Priority 1: If a monitored address is involved, process directly (regardless of whether the counterparty is a contract)
	// Priority 2: If no monitored address is involved, only process transfers between EOAs
	if event.EventName == "Transfer" {
		var fromAddr, toAddr common.Address
		var hasFrom, hasTo bool

		// Extract from and to addresses from event data
		if addr, ok := event.EventData["from"].(common.Address); ok {
			fromAddr = addr
			hasFrom = true
		}
		if addr, ok := event.EventData["to"].(common.Address); ok {
			toAddr = addr
			hasTo = true
		}

		// Both from and to addresses must be present
		if !hasFrom || !hasTo {
			return false
		}

		// Priority 1: Check if a monitored address is involved
		fromMonitored := l.isAddressMonitored(fromAddr)
		toMonitored := l.isAddressMonitored(toAddr)

		// If a monitored address is involved, process directly (regardless of whether the counterparty is an EOA or a contract)
		if fromMonitored || toMonitored {
			logger.Infof("Found Transfer with monitored address: from=%s (monitored=%v), to=%s (monitored=%v)",
				fromAddr.Hex(), fromMonitored, toAddr.Hex(), toMonitored)
			return true
		}

		// Priority 2: If no monitored address is involved, only process transfers between EOAs
		fromIsContract := l.isContractAddress(fromAddr)
		toIsContract := l.isContractAddress(toAddr)

		// Exclude transfers involving contract addresses
		if fromIsContract || toIsContract {
			logger.Debugf("Skipping Transfer event (no monitored address): from=%s (contract=%v), to=%s (contract=%v)",
				fromAddr.Hex(), fromIsContract, toAddr.Hex(), toIsContract)
			return false
		}

		logger.Debugf("Found ERC20 Transfer between EOAs (no monitored address): from=%s, to=%s", fromAddr.Hex(), toAddr.Hex())
		return true
	}

	// Other events: Check all address fields in event data
	addressFieldsToCheck := []string{
		"from", "to", "sender", "recipient", "user", "owner", "spender",
		"account", "operator", "approved", "beneficiary", "player",
	}

	logger.Debugf("Checking event '%s' with data fields: %+v", event.EventName, event.EventData)

	for _, fieldName := range addressFieldsToCheck {
		if addr, ok := event.EventData[fieldName].(common.Address); ok {
			logger.Debugf("Checking field '%s': %s, monitored=%v",
				fieldName, addr.Hex(), l.isAddressMonitored(addr))
			if l.isAddressMonitored(addr) {
				logger.Infof("Found monitored address in event '%s' field '%s': %s",
					event.EventName, fieldName, addr.Hex())
				return true
			}
		}
	}

	// Check the event's own FromAddress and ToAddress fields
	if event.FromAddress != "" {
		logger.Debugf("Checking event.FromAddress: %s, monitored=%v",
			event.FromAddress, l.isAddressMonitored(common.HexToAddress(event.FromAddress)))
		if l.isAddressMonitored(common.HexToAddress(event.FromAddress)) {
			logger.Infof("Found monitored address in event.FromAddress: %s", event.FromAddress)
			return true
		}
	}

	if event.ToAddress != "" {
		logger.Debugf("Checking event.ToAddress: %s, monitored=%v",
			event.ToAddress, l.isAddressMonitored(common.HexToAddress(event.ToAddress)))
		if l.isAddressMonitored(common.HexToAddress(event.ToAddress)) {
			logger.Infof("Found monitored address in event.ToAddress: %s", event.ToAddress)
			return true
		}
	}

	logger.Debugf("Event '%s' not related to any monitored address", event.EventName)
	return false
}
