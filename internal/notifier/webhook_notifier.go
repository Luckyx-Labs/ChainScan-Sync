package notifier

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Luckyx-Labs/chainscan-sync/internal/config"
	"github.com/Luckyx-Labs/chainscan-sync/internal/interfaces"
	"github.com/Luckyx-Labs/chainscan-sync/internal/types"
	"github.com/Luckyx-Labs/chainscan-sync/pkg/logger"
	"github.com/Luckyx-Labs/chainscan-sync/pkg/utils"
)

// WebhookNotifier Webhook notifier
type WebhookNotifier struct {
	config      config.WebhooksConfig
	withdrawEOA string // withdraw EOA address from config
	httpClient  *http.Client
	name        string
	storage     interfaces.Storage
	mu          sync.RWMutex
}

// NewWebhookNotifier creates a new Webhook notifier
func NewWebhookNotifier(cfg config.WebhooksConfig, withdrawEOA string) interfaces.Notifier {
	return &WebhookNotifier{
		config:      cfg,
		withdrawEOA: strings.ToLower(withdrawEOA),
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
			},
		},
		name: "WebhookNotifier",
	}
}

// SetStorage sets the storage (used for recording notification logs)
func (n *WebhookNotifier) SetStorage(storage interfaces.Storage) {
	n.storage = storage
}

// Notify sends a notification
func (n *WebhookNotifier) Notify(ctx context.Context, event *types.Event) error {
	if !n.config.Enabled {
		return nil
	}

	// Serialize event
	eventJSON, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to serialize event: %w", err)
	}

	// Send to all endpoints
	var lastErr error
	for _, endpoint := range n.config.Endpoints {
		if err := n.sendToEndpointWithRetry(ctx, endpoint, eventJSON, event); err != nil {
			logger.Errorf("failed to send to Webhook endpoint %s: %v", endpoint.URL, err)
			lastErr = err
		}
	}

	return lastErr
}

// BatchNotify sends batch notifications
func (n *WebhookNotifier) BatchNotify(ctx context.Context, events []*types.Event) error {
	if !n.config.Enabled || len(events) == 0 {
		return nil
	}

	// Use the first event to determine the event type (events of the same transaction should have the same type)
	firstEvent := events[0]

	// Serialize event list
	eventsJSON, err := json.Marshal(map[string]interface{}{
		"events": events,
		"count":  len(events),
	})
	if err != nil {
		return fmt.Errorf("failed to serialize event list: %w", err)
	}

	// Send to all endpoints that support batch
	var lastErr error
	for _, endpoint := range n.config.Endpoints {
		// Pass the first event to determine routing and signature
		if err := n.sendToEndpointWithRetry(ctx, endpoint, eventsJSON, firstEvent); err != nil {
			logger.Errorf("Failed to batch send to Webhook endpoint %s: %v", endpoint.URL, err)
			lastErr = err
		}
	}

	return lastErr
}

// sendToEndpointWithRetry sends to endpoint with retry
func (n *WebhookNotifier) sendToEndpointWithRetry(ctx context.Context, endpoint config.WebhookEndpoint, data []byte, event *types.Event) error {
	var err error
	maxRetries := 3
	if endpoint.Retry {
		maxRetries = 5
	}

	// Use retry mechanism
	retryConfig := config.RetryConfig{
		MaxAttempts:     maxRetries,
		InitialInterval: 1 * time.Second,
		MaxInterval:     30 * time.Second,
		Multiplier:      2.0,
	}

	err = utils.RetryWithBackoff(ctx, retryConfig, func() error {
		return n.sendToEndpoint(ctx, endpoint, data, event)
	})

	return err
}

// sendToEndpoint Send to a single endpoint
func (n *WebhookNotifier) sendToEndpoint(ctx context.Context, endpoint config.WebhookEndpoint, data []byte, event *types.Event) error {
	startTime := time.Now()
	method_name := ""

	// Construct different requests based on event type
	if event != nil {
		switch event.EventName {
		case "Deposit":
			method_name = "deposit"
		case "ETHDeposit":
			method_name = "ethDeposit"
		case "Withdraw":
			method_name = "tokenWithdraw"
		case "ETHWithdraw":
			method_name = "ethWithdraw"
		case "Transfer":
			// Check if from address is the withdraw EOA address
			if n.withdrawEOA != "" && strings.EqualFold(event.FromAddress, n.withdrawEOA) {
				method_name = "withdraw"
			} else {
				method_name = "transfer"
			}
		case "BatchPayinItem":
			method_name = "batchPayinItem"
		case "BatchPayoutItem":
			method_name = "batchPayoutItem"
		case "NativeTransfer":
			if n.withdrawEOA != "" && strings.EqualFold(event.FromAddress, n.withdrawEOA) {
				method_name = "withdraw"
			} else {
				method_name = "nativeTransfer"
			}
		default:
			// Unsupported event type, skip notification
			logger.Debugf("Unsupported event type for notification: %s, skipping", event.EventName)
			return nil
		}

		var amount int64
		// After JSON deserialization, numeric types are float64
		if amt, ok := event.EventData["amount"].(float64); ok {
			amount = int64(amt)
		} else if amt, ok := event.EventData["receivedAmount"].(float64); ok {
			amount = int64(amt)
		} else if amt, ok := event.EventData["tokens"].(float64); ok {
			amount = int64(amt)
		} else if amt, ok := event.EventData["value"].(float64); ok {
			amount = int64(amt)
		}

		var networkFee *big.Int
		if event.GasPrice != "" {
			gasPrice := new(big.Int)
			if _, ok := gasPrice.SetString(event.GasPrice, 10); ok {
				gasUsed := new(big.Int).SetUint64(event.GasUsed)
				networkFee = new(big.Int).Mul(gasPrice, gasUsed)
			}
		}

		params := map[string]interface{}{
			"method_name":      method_name,
			"address":          event.ToAddress,
			"amount":           amount,
			"contract_address": event.TokenAddress,
			"detected_at":      event.Timestamp.Unix(),
			"network_fee":      networkFee,
			"timestamp":        fmt.Sprintf("%d", time.Now().Unix()),
			"tx_hash":          event.TxHash,
		}

		sign := calculateMD5Sign(params, endpoint.APIKey)

		params["sign"] = sign
		data, _ = json.Marshal(params)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint.URL, bytes.NewBuffer(data))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	// Set request headers
	req.Header.Set("Content-Type", "application/json")

	// Send request
	resp, err := n.httpClient.Do(req)
	if err != nil {
		n.logNotification(event, endpoint.URL, 0, "", err, startTime)
		return fmt.Errorf("send request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read response body (for logging)
	var respBody []byte
	if resp.Body != nil {
		respBody = make([]byte, 1024)
		resp.Body.Read(respBody)
	}

	if resp.StatusCode != http.StatusOK {
		n.logNotification(event, endpoint.URL, resp.StatusCode, string(respBody), fmt.Errorf("HTTP error: %d", resp.StatusCode), startTime)
		return fmt.Errorf("HTTP error: %d", resp.StatusCode)
	}

	n.logNotification(event, endpoint.URL, resp.StatusCode, string(respBody), nil, startTime)
	return nil
}

// calculateMD5Sign calculates the MD5 signature
// Format: md5("k1=v1&k2=v2&...&key=API_KEY")
// Parameters are sorted alphabetically
func calculateMD5Sign(params map[string]interface{}, apiKey string) string {
	// Get all keys and sort them
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// Concatenate parameters in order
	var builder strings.Builder
	for i, k := range keys {
		if i > 0 {
			builder.WriteString("&")
		}
		builder.WriteString(k)
		builder.WriteString("=")
		builder.WriteString(fmt.Sprintf("%v", params[k]))
	}

	builder.WriteString("&")
	builder.WriteString(apiKey)

	// Calculate MD5
	signStr := builder.String()
	hash := md5.Sum([]byte(signStr))
	sign := hex.EncodeToString(hash[:])

	logger.Debugf("Sign string: %s", signStr)
	logger.Debugf("MD5 sign: %s", sign)

	return sign
}

// logNotification logs the notification
func (n *WebhookNotifier) logNotification(event *types.Event, endpoint string, statusCode int, respBody string, err error, startTime time.Time) {
	// If there is no storage, only log to console
	if n.storage == nil {
		if err != nil {
			logger.Warnf("Webhook notification failed: %s -> %s, error: %v", endpoint, getEventID(event), err)
		} else {
			logger.Infof("Webhook notification successful: %s -> %s, status code: %d", endpoint, getEventID(event), statusCode)
		}
		return
	}

}

// getEventID gets the event ID (handles nil case)
func getEventID(event *types.Event) string {
	if event == nil {
		return "batch"
	}
	return event.ID
}

// GetNotifierName gets the notifier name
func (n *WebhookNotifier) GetNotifierName() string {
	return n.name
}

// NotifyRollback sends rollback notifications
// When a blockchain reorganization occurs, notify external services to revoke previous transactions
func (n *WebhookNotifier) NotifyRollback(ctx context.Context, events []*types.Event, rollbackBlockNumber uint64) error {
	if !n.config.Enabled || len(events) == 0 {
		return nil
	}

	logger.Warnf("Sending rollback notifications for %d events (rollback to block %d)", len(events), rollbackBlockNumber)

	var lastErr error
	for _, event := range events {
		if err := n.sendRollbackNotification(ctx, event, rollbackBlockNumber); err != nil {
			logger.Errorf("Failed to send rollback notification for tx %s: %v", event.TxHash, err)
			lastErr = err
		}
	}

	return lastErr
}

// sendRollbackNotification sends a single rollback notification
func (n *WebhookNotifier) sendRollbackNotification(ctx context.Context, event *types.Event, rollbackBlockNumber uint64) error {
	for _, endpoint := range n.config.Endpoints {
		// Build rollback notification parameters
		params := map[string]interface{}{
			"method_name":           "rollback",
			"action":                "rollback",
			"tx_hash":               event.TxHash,
			"block_number":          event.BlockNumber,
			"rollback_block_number": rollbackBlockNumber,
			"original_event":        event.EventName,
			"address":               event.ToAddress,
			"contract_address":      event.TokenAddress,
			"reason":                "blockchain_reorg",
			"timestamp":             fmt.Sprintf("%d", time.Now().Unix()),
		}

		sign := calculateMD5Sign(params, endpoint.APIKey)
		params["sign"] = sign

		data, err := json.Marshal(params)
		if err != nil {
			return fmt.Errorf("failed to serialize rollback notification: %w", err)
		}

		req, err := http.NewRequestWithContext(ctx, "POST", endpoint.URL, bytes.NewBuffer(data))
		if err != nil {
			return fmt.Errorf("create rollback request: %w", err)
		}

		req.Header.Set("Content-Type", "application/json")

		resp, err := n.httpClient.Do(req)
		if err != nil {
			logger.Errorf("Failed to send rollback notification to %s: %v", endpoint.URL, err)
			continue
		}
		resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			logger.Infof("Rollback notification sent successfully: tx=%s, block=%d, endpoint=%s",
				event.TxHash, event.BlockNumber, endpoint.URL)
		} else {
			logger.Warnf("Rollback notification returned status %d: tx=%s, endpoint=%s",
				resp.StatusCode, event.TxHash, endpoint.URL)
		}
	}

	return nil
}
