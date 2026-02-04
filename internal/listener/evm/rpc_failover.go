package evm

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Luckyx-Labs/chainscan-sync/pkg/logger"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
	"golang.org/x/time/rate"
)

// RPCEndpoint RPC endpoint structure
type RPCEndpoint struct {
	URL          string
	Client       *ethclient.Client
	RPCClient    *rpc.Client
	IsAlchemy    bool // Whether it supports Alchemy-specific API
	FailCount    int32
	LastFailTime time.Time
	IsHealthy    bool
	rateLimiter  *rate.Limiter // Rate limiter
}

// FailoverRPCClient RPC client with failover support
type FailoverRPCClient struct {
	endpoints     []*RPCEndpoint
	currentIndex  int32
	mu            sync.RWMutex
	ctx           context.Context
	maxFailCount  int32
	recoveryTime  time.Duration
	healthCheckCh chan struct{}
}

// NewFailoverRPCClient creates a new RPC client with failover support
// rateLimit: requests per second limit, 0 or negative means use default value 30
func NewFailoverRPCClient(ctx context.Context, primaryURL string, backupURLs []string, rateLimit float64) (*FailoverRPCClient, error) {
	// Default rate limit 30 requests/second
	if rateLimit <= 0 {
		rateLimit = 30.0
	}

	client := &FailoverRPCClient{
		endpoints:     make([]*RPCEndpoint, 0),
		currentIndex:  0,
		ctx:           ctx,
		maxFailCount:  3,
		recoveryTime:  30 * time.Second,
		healthCheckCh: make(chan struct{}, 1),
	}

	logger.Infof("RPC rate limit set to %.1f requests/second", rateLimit)

	// Add primary endpoint
	allURLs := append([]string{primaryURL}, backupURLs...)

	for _, url := range allURLs {
		endpoint, err := client.createEndpoint(url)
		if err != nil {
			logger.Warnf("Failed to connect to RPC endpoint %s: %v", url, err)
			continue
		}
		client.endpoints = append(client.endpoints, endpoint)
		logger.Infof("Connected to RPC endpoint: %s (Alchemy: %v)", url, endpoint.IsAlchemy)
	}

	if len(client.endpoints) == 0 {
		return nil, fmt.Errorf("failed to connect to any RPC endpoint")
	}

	// Start health check goroutine
	go client.healthCheckLoop()

	return client, nil
}

// createEndpoint creates a single endpoint
func (c *FailoverRPCClient) createEndpoint(url string) (*RPCEndpoint, error) {
	rpcClient, err := rpc.DialContext(c.ctx, url)
	if err != nil {
		return nil, err
	}

	ethClient := ethclient.NewClient(rpcClient)

	// Detect if it is an Alchemy endpoint
	isAlchemy := strings.Contains(strings.ToLower(url), "alchemy")

	// Set different rate limits based on endpoint type
	// Alchemy free tier: ~330 CU/s, approximately 25-30 requests/second
	// Infura free tier: 10 requests/second
	// Public nodes: conservatively set to 5 requests/second
	var limiter *rate.Limiter
	if isAlchemy {
		limiter = rate.NewLimiter(rate.Limit(25), 30) // 25 requests/second, burst 30
	} else if strings.Contains(strings.ToLower(url), "infura") {
		limiter = rate.NewLimiter(rate.Limit(8), 10) // 8 requests/second, burst 10
	} else {
		limiter = rate.NewLimiter(rate.Limit(5), 10) // 5 requests/second, burst 10
	}

	return &RPCEndpoint{
		URL:         url,
		Client:      ethClient,
		RPCClient:   rpcClient,
		IsAlchemy:   isAlchemy,
		IsHealthy:   true,
		rateLimiter: limiter,
	}, nil
}

// GetCurrentEndpoint gets the current active endpoint
func (c *FailoverRPCClient) GetCurrentEndpoint() *RPCEndpoint {
	c.mu.RLock()
	defer c.mu.RUnlock()

	idx := atomic.LoadInt32(&c.currentIndex)
	if idx >= int32(len(c.endpoints)) {
		idx = 0
	}
	return c.endpoints[idx]
}

// GetClient gets the current ethclient
func (c *FailoverRPCClient) GetClient() *ethclient.Client {
	return c.GetCurrentEndpoint().Client
}

// GetRPCClient gets the current raw RPC client
func (c *FailoverRPCClient) GetRPCClient() *rpc.Client {
	return c.GetCurrentEndpoint().RPCClient
}

// IsAlchemyAvailable checks if the current endpoint supports Alchemy API
func (c *FailoverRPCClient) IsAlchemyAvailable() bool {
	return c.GetCurrentEndpoint().IsAlchemy
}

// GetAlchemyEndpoint gets an available Alchemy endpoint (if any)
func (c *FailoverRPCClient) GetAlchemyEndpoint() *RPCEndpoint {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Prefer the current endpoint (if it is Alchemy)
	currentIdx := atomic.LoadInt32(&c.currentIndex)
	if c.endpoints[currentIdx].IsAlchemy && c.endpoints[currentIdx].IsHealthy {
		return c.endpoints[currentIdx]
	}

	// Otherwise, look for other healthy Alchemy endpoints
	for _, ep := range c.endpoints {
		if ep.IsAlchemy && ep.IsHealthy {
			return ep
		}
	}

	return nil
}

// ReportError reports an error, which may trigger failover
func (c *FailoverRPCClient) ReportError(err error) {
	if err == nil {
		return
	}

	endpoint := c.GetCurrentEndpoint()

	// Increment fail count
	failCount := atomic.AddInt32(&endpoint.FailCount, 1)

	c.mu.Lock()
	endpoint.LastFailTime = time.Now()
	c.mu.Unlock()

	logger.Warnf("RPC endpoint %s error (fail count: %d): %v", endpoint.URL, failCount, err)

	// if fail count exceeds threshold, switch to next endpoint
	if failCount >= c.maxFailCount {
		c.switchToNextEndpoint()
	}
}

// ReportSuccess reports a success and resets the fail count
func (c *FailoverRPCClient) ReportSuccess() {
	endpoint := c.GetCurrentEndpoint()
	atomic.StoreInt32(&endpoint.FailCount, 0)
}

// switchToNextEndpoint switches to the next healthy endpoint
func (c *FailoverRPCClient) switchToNextEndpoint() {
	c.mu.Lock()
	defer c.mu.Unlock()

	currentIdx := atomic.LoadInt32(&c.currentIndex)

	// Mark the current endpoint as unhealthy
	c.endpoints[currentIdx].IsHealthy = false

	// Find the next healthy endpoint
	for i := 1; i < len(c.endpoints); i++ {
		nextIdx := (int(currentIdx) + i) % len(c.endpoints)
		if c.endpoints[nextIdx].IsHealthy {
			atomic.StoreInt32(&c.currentIndex, int32(nextIdx))
			logger.Warnf("Switched RPC endpoint from %s to %s",
				c.endpoints[currentIdx].URL, c.endpoints[nextIdx].URL)

			// Trigger health check to attempt recovery of the failed endpoint
			select {
			case c.healthCheckCh <- struct{}{}:
			default:
			}
			return
		}
	}

	// If no other healthy endpoints, attempt to reconnect to the current endpoint
	logger.Errorf("No healthy RPC endpoint available, attempting to reconnect to %s", c.endpoints[currentIdx].URL)
	c.endpoints[currentIdx].IsHealthy = true
	atomic.StoreInt32(&c.endpoints[currentIdx].FailCount, 0)
}

// healthCheckLoop health check loop
func (c *FailoverRPCClient) healthCheckLoop() {
	ticker := time.NewTicker(c.recoveryTime)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			c.checkUnhealthyEndpoints()
		case <-c.healthCheckCh:
			// Received signal, check immediately
			c.checkUnhealthyEndpoints()
		}
	}
}

// checkUnhealthyEndpoints checks if unhealthy endpoints have recovered
func (c *FailoverRPCClient) checkUnhealthyEndpoints() {
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, endpoint := range c.endpoints {
		if !endpoint.IsHealthy && time.Since(endpoint.LastFailTime) > c.recoveryTime {
			// Attempt health check
			ctx, cancel := context.WithTimeout(c.ctx, 5*time.Second)
			_, err := endpoint.Client.BlockNumber(ctx)
			cancel()

			if err == nil {
				endpoint.IsHealthy = true
				atomic.StoreInt32(&endpoint.FailCount, 0)
				logger.Infof("RPC endpoint %s recovered", endpoint.URL)
			}
		}
	}
}

// closes all connections
func (c *FailoverRPCClient) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, endpoint := range c.endpoints {
		if endpoint.Client != nil {
			endpoint.Client.Close()
		}
	}
	close(c.healthCheckCh)
}

// waitForRateLimit waits for rate limiting
func (c *FailoverRPCClient) waitForRateLimit(ctx context.Context) error {
	endpoint := c.GetCurrentEndpoint()
	if endpoint.rateLimiter != nil {
		return endpoint.rateLimiter.Wait(ctx)
	}
	return nil
}

//	Wrappers for common ethclient methods, automatically handling failover and rate limiting
//
// BlockNumber gets the latest block number
func (c *FailoverRPCClient) BlockNumber(ctx context.Context) (uint64, error) {
	if err := c.waitForRateLimit(ctx); err != nil {
		return 0, err
	}
	blockNum, err := c.GetClient().BlockNumber(ctx)
	if err != nil {
		c.ReportError(err)
		return 0, err
	}
	c.ReportSuccess()
	return blockNum, nil
}

// HeaderByNumber gets the block header
func (c *FailoverRPCClient) HeaderByNumber(ctx context.Context, number *big.Int) (*types.Header, error) {
	if err := c.waitForRateLimit(ctx); err != nil {
		return nil, err
	}
	header, err := c.GetClient().HeaderByNumber(ctx, number)
	if err != nil {
		c.ReportError(err)
		return nil, err
	}
	c.ReportSuccess()
	return header, nil
}

// FilterLogs filters logs
func (c *FailoverRPCClient) FilterLogs(ctx context.Context, query ethereum.FilterQuery) ([]types.Log, error) {
	if err := c.waitForRateLimit(ctx); err != nil {
		return nil, err
	}
	logs, err := c.GetClient().FilterLogs(ctx, query)
	if err != nil {
		c.ReportError(err)
		return nil, err
	}
	c.ReportSuccess()
	return logs, nil
}

// TransactionByHash gets the transaction
func (c *FailoverRPCClient) TransactionByHash(ctx context.Context, hash common.Hash) (*types.Transaction, bool, error) {
	if err := c.waitForRateLimit(ctx); err != nil {
		return nil, false, err
	}
	tx, isPending, err := c.GetClient().TransactionByHash(ctx, hash)
	if err != nil {
		c.ReportError(err)
		return nil, false, err
	}
	c.ReportSuccess()
	return tx, isPending, nil
}

// TransactionReceipt gets the transaction receipt
func (c *FailoverRPCClient) TransactionReceipt(ctx context.Context, txHash common.Hash) (*types.Receipt, error) {
	if err := c.waitForRateLimit(ctx); err != nil {
		return nil, err
	}
	receipt, err := c.GetClient().TransactionReceipt(ctx, txHash)
	if err != nil {
		c.ReportError(err)
		return nil, err
	}
	c.ReportSuccess()
	return receipt, nil
}

// BalanceAt gets the balance
func (c *FailoverRPCClient) BalanceAt(ctx context.Context, account common.Address, blockNumber *big.Int) (*big.Int, error) {
	if err := c.waitForRateLimit(ctx); err != nil {
		return nil, err
	}
	balance, err := c.GetClient().BalanceAt(ctx, account, blockNumber)
	if err != nil {
		c.ReportError(err)
		return nil, err
	}
	c.ReportSuccess()
	return balance, nil
}

// CallContext calls an RPC method (used for Alchemy and other custom APIs)
func (c *FailoverRPCClient) CallContext(ctx context.Context, result interface{}, method string, args ...interface{}) error {
	// Wait for rate limiting
	if err := c.waitForRateLimit(ctx); err != nil {
		return err
	}

	// For Alchemy-specific APIs, try using the Alchemy endpoint
	if strings.HasPrefix(method, "alchemy_") {
		alchemyEndpoint := c.GetAlchemyEndpoint()
		if alchemyEndpoint != nil {
			// Alchemy endpoint also needs to wait for its rate limiter
			if alchemyEndpoint.rateLimiter != nil {
				if err := alchemyEndpoint.rateLimiter.Wait(ctx); err != nil {
					return err
				}
			}
			err := alchemyEndpoint.RPCClient.CallContext(ctx, result, method, args...)
			if err == nil {
				return nil
			}
			// Alchemy API failed, mark endpoint issue
			logger.Warnf("Alchemy API %s failed: %v", method, err)
		}
		// No available Alchemy endpoint, return a specific error to let the caller degrade gracefully
		return fmt.Errorf("alchemy API not available: %s", method)
	}

	// Regular RPC call
	err := c.GetRPCClient().CallContext(ctx, result, method, args...)
	if err != nil {
		c.ReportError(err)
		return err
	}
	c.ReportSuccess()
	return err
}
