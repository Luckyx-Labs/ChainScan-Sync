package notifier

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Luckyx-Labs/chainscan-sync/internal/config"
	"github.com/Luckyx-Labs/chainscan-sync/internal/types"
	"github.com/ethereum/go-ethereum/common"
)

// TestWebhookNotifier_IntegrationTest integration test for WebhookNotifier
func TestWebhookNotifier_IntegrationTest(t *testing.T) {
	receivedData := make(chan map[string]interface{}, 10)

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Read request body
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("Failed to read request body: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		// Parse JSON data
		var data map[string]interface{}
		if err := json.Unmarshal(body, &data); err != nil {
			t.Errorf("Failed to unmarshal request body: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		prettyJSON, _ := json.MarshalIndent(data, "", "  ")
		t.Logf("\n=== Received Webhook Data ===\n%s\n", string(prettyJSON))

		receivedData <- data

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok","message":"received"}`))
	}))
	defer mockServer.Close()

	cfg := config.WebhooksConfig{
		Enabled: true,
		Endpoints: []config.WebhookEndpoint{
			{
				URL:   mockServer.URL,
				Retry: true,
			},
		},
	}

	notifier := NewWebhookNotifier(cfg, "")
	ctx := context.Background()

	// define test cases
	testCases := []struct {
		name  string
		event *types.Event
	}{
		{
			name: "Deposit Event - ERC20 Token",
			event: createDepositEvent(
				"0x1234567890abcdef1234567890abcdef12345678901234567890abcdef123456",
				"0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48", // USDC
				"0x742d35Cc6634C0532925a3b844Bc9e7595f0bEb",  // From
				"0x8888888888888888888888888888888888888888", // To
				"1000000000", // 1000 USDC (6 decimals)
			),
		},
		{
			name: "Withdraw Event - Native Token",
			event: createWithdrawEvent(
				"0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
				"0x0", // ETH
				"0x8888888888888888888888888888888888888888",
				"0x742d35Cc6634C0532925a3b844Bc9e7595f0bEb",
				"500000000000000000", // 0.5 ETH
			),
		},
		{
			name: "BatchPayinItem Event",
			event: createBatchPayinItemEvent(
				"0xfedcba0987654321fedcba0987654321fedcba0987654321fedcba0987654321",
				"0xdac17f958d2ee523a2206206994597c13d831ec7", // USDT
				"0x742d35Cc6634C0532925a3b844Bc9e7595f0bEb",
				"2500000000", // 2500 USDT (6 decimals)
			),
		},
		{
			name: "BatchPayoutItem Event",
			event: createBatchPayoutItemEvent(
				"0x111222333444555666777888999aaabbbcccdddeeefff000111222333444555",
				"0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48", // USDC
				"0x9999999999999999999999999999999999999999",
				"750000000", // 750 USDC
			),
		},
		{
			name: "Transfer Event - ERC20",
			event: createTransferEvent(
				"0x555666777888999aaabbbcccdddeeefff000111222333444555666777888999a",
				"0x6b175474e89094c44da98b954eedeac495271d0f", // DAI
				"0x742d35Cc6634C0532925a3b844Bc9e7595f0bEb",
				"0x8888888888888888888888888888888888888888",
				"100000000000000000000", // 100 DAI (18 decimals)
			),
		},
	}

	// Execute tests
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("\n>>> Testing: %s\n", tc.name)

			// Send single event notification
			err := notifier.Notify(ctx, tc.event)
			if err != nil {
				t.Errorf("Failed to send notification: %v", err)
				return
			}

			// Wait for data reception
			select {
			case data := <-receivedData:
				// Validate basic fields
				validateEventData(t, data, tc.event)
			case <-time.After(5 * time.Second):
				t.Error("Timeout waiting for webhook notification")

			}
		})
	}
}

// TestWebhookNotifier_BatchNotify_IntegrationTest batch notification integration test
func TestWebhookNotifier_BatchNotify_IntegrationTest(t *testing.T) {
	receivedData := make(chan map[string]interface{}, 1)

	// Create mock server
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		defer r.Body.Close()

		var data map[string]interface{}
		json.Unmarshal(body, &data)

		// Print batch data
		prettyJSON, _ := json.MarshalIndent(data, "", "  ")
		t.Logf("\n=== Received Batch Webhook Data ===\n%s\n", string(prettyJSON))

		receivedData <- data

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer mockServer.Close()

	cfg := config.WebhooksConfig{
		Enabled: true,
		Endpoints: []config.WebhookEndpoint{
			{
				URL:   mockServer.URL,
				Retry: true,
			},
		},
	}

	notifier := NewWebhookNotifier(cfg, "")
	ctx := context.Background()

	// Create batch events (simulate multiple events in the same transaction)
	txHash := "0xbatch123456789abcdef0123456789abcdef0123456789abcdef0123456789ab"
	events := []*types.Event{
		createDepositEvent(txHash, "0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48",
			"0x742d35Cc6634C0532925a3b844Bc9e7595f0bEb", "0x1111111111111111111111111111111111111111", "1000000000"),
		createDepositEvent(txHash, "0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48",
			"0x742d35Cc6634C0532925a3b844Bc9e7595f0bEb", "0x2222222222222222222222222222222222222222", "2000000000"),
		createDepositEvent(txHash, "0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48",
			"0x742d35Cc6634C0532925a3b844Bc9e7595f0bEb", "0x3333333333333333333333333333333333333333", "3000000000"),
	}

	t.Logf("\n>>> Testing: Batch Notification (3 events in one transaction)\n")

	// Send batch notification
	err := notifier.BatchNotify(ctx, events)
	if err != nil {
		t.Errorf("Failed to send batch notification: %v", err)
		return
	}

	// Validate batch data
	select {
	case data := <-receivedData:
		// Validate batch structure
		if eventsData, ok := data["events"]; !ok {
			t.Error("Missing 'events' field in batch notification")
		} else {
			eventsList, ok := eventsData.([]interface{})
			if !ok {
				t.Error("'events' field is not an array")
			} else if len(eventsList) != 3 {
				t.Errorf("Expected 3 events, got %d", len(eventsList))
			} else {
				t.Logf("Batch notification contains %d events", len(eventsList))
			}
		}

		if count, ok := data["count"]; !ok {
			t.Error("Missing 'count' field in batch notification")
		} else if count != float64(3) {
			t.Errorf("Expected count=3, got %v", count)
		} else {
			t.Logf("Event count is correct: %v", count)
		}
	case <-time.After(5 * time.Second):
		t.Error("Timeout waiting for batch webhook notification")
	}
}

// TestWebhookNotifier_ErrorHandling tests error handling in WebhookNotifier
func TestWebhookNotifier_ErrorHandling(t *testing.T) {
	testCases := []struct {
		name           string
		serverHandler  http.HandlerFunc
		expectError    bool
		errorSubstring string
	}{
		{
			name: "Server Returns 500",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte(`{"error":"internal server error"}`))
			},
			expectError:    true,
			errorSubstring: "HTTP error: 500",
		},
		{
			name: "Server Returns 400",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				w.Write([]byte(`{"error":"bad request"}`))
			},
			expectError:    true,
			errorSubstring: "HTTP error: 400",
		},
		{
			name: "Server Returns 200",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{"status":"success"}`))
			},
			expectError: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			mockServer := httptest.NewServer(tc.serverHandler)
			defer mockServer.Close()

			cfg := config.WebhooksConfig{
				Enabled: true,
				Endpoints: []config.WebhookEndpoint{
					{
						URL:   mockServer.URL,
						Retry: false, // No retry to speed up tests
					},
				},
			}

			notifier := NewWebhookNotifier(cfg, "")
			ctx := context.Background()

			event := createDepositEvent(
				"0xtest123",
				"0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48",
				"0x742d35Cc6634C0532925a3b844Bc9e7595f0bEb",
				"0x8888888888888888888888888888888888888888",
				"1000000000",
			)

			err := notifier.Notify(ctx, event)

			if tc.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				} else if tc.errorSubstring != "" && !contains(err.Error(), tc.errorSubstring) {
					t.Errorf("Expected error containing '%s', got: %v", tc.errorSubstring, err)
				} else {
					t.Logf("Got expected error: %v", err)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error but got: %v", err)
				} else {
					t.Log("Notification succeeded")
				}
			}
		})
	}
}

// createDepositEvent creates a deposit event
func createDepositEvent(txHash, token, from, to, amount string) *types.Event {
	return &types.Event{
		ID:           fmt.Sprintf("%s-0", txHash),
		ChainType:    types.ChainTypeEVM,
		ChainName:    "base",
		BlockNumber:  18000000,
		BlockHash:    "0xblock123456789",
		TxHash:       txHash,
		TxIndex:      0,
		LogIndex:     0,
		ContractAddr: "0xContractAddress123456789",
		EventName:    "Deposit",
		EventData: map[string]interface{}{
			"from":   common.HexToAddress(from),
			"to":     common.HexToAddress(to),
			"token":  common.HexToAddress(token),
			"amount": new(big.Int).SetBytes([]byte(amount)),
		},
		Timestamp: time.Now(),
	}
}

// createWithdrawEvent creates a withdraw event
func createWithdrawEvent(txHash, token, from, to, amount string) *types.Event {
	return &types.Event{
		ID:           fmt.Sprintf("%s-0", txHash),
		ChainType:    types.ChainTypeEVM,
		ChainName:    "base",
		BlockNumber:  18000001,
		BlockHash:    "0xblock123456790",
		TxHash:       txHash,
		TxIndex:      0,
		LogIndex:     0,
		ContractAddr: "0xContractAddress123456789",
		EventName:    "Withdraw",
		EventData: map[string]interface{}{
			"from":   common.HexToAddress(from),
			"to":     common.HexToAddress(to),
			"token":  common.HexToAddress(token),
			"amount": new(big.Int).SetBytes([]byte(amount)),
		},
		Timestamp: time.Now(),
	}
}

// createBatchPayinItemEvent creates a batch payin item event
func createBatchPayinItemEvent(txHash, token, from, amount string) *types.Event {
	return &types.Event{
		ID:           fmt.Sprintf("%s-0", txHash),
		ChainType:    types.ChainTypeEVM,
		ChainName:    "base",
		BlockNumber:  18000002,
		BlockHash:    "0xblock123456791",
		TxHash:       txHash,
		TxIndex:      0,
		LogIndex:     0,
		ContractAddr: "0xContractAddress123456789",
		EventName:    "BatchPayinItem",
		EventData: map[string]interface{}{
			"from":           common.HexToAddress(from),
			"token":          common.HexToAddress(token),
			"receivedAmount": new(big.Int).SetBytes([]byte(amount)),
		},
		Timestamp: time.Now(),
	}
}

// createBatchPayoutItemEvent creates a batch payout item event
func createBatchPayoutItemEvent(txHash, token, recipient, amount string) *types.Event {
	return &types.Event{
		ID:           fmt.Sprintf("%s-0", txHash),
		ChainType:    types.ChainTypeEVM,
		ChainName:    "base",
		BlockNumber:  18000003,
		BlockHash:    "0xblock123456792",
		TxHash:       txHash,
		TxIndex:      0,
		LogIndex:     0,
		ContractAddr: "0xContractAddress123456789",
		EventName:    "BatchPayoutItem",
		EventData: map[string]interface{}{
			"recipient": common.HexToAddress(recipient),
			"token":     common.HexToAddress(token),
			"amount":    new(big.Int).SetBytes([]byte(amount)),
			"timestamp": big.NewInt(time.Now().Unix()),
		},
		Timestamp: time.Now(),
	}
}

// createTransferEvent creates a transfer event
func createTransferEvent(txHash, token, from, to, amount string) *types.Event {
	return &types.Event{
		ID:           fmt.Sprintf("%s-0", txHash),
		ChainType:    types.ChainTypeEVM,
		ChainName:    "base",
		BlockNumber:  18000004,
		BlockHash:    "0xblock123456793",
		TxHash:       txHash,
		TxIndex:      0,
		LogIndex:     0,
		ContractAddr: token,
		EventName:    "Transfer",
		EventData: map[string]interface{}{
			"from":  common.HexToAddress(from),
			"to":    common.HexToAddress(to),
			"value": new(big.Int).SetBytes([]byte(amount)),
		},
		Timestamp: time.Now(),
	}
}

// validateEventData validates the received event data
func validateEventData(t *testing.T, data map[string]interface{}, expectedEvent *types.Event) {
	// Validate basic fields
	if id, ok := data["id"].(string); !ok || id != expectedEvent.ID {
		t.Errorf("ID mismatch: expected %s, got %v", expectedEvent.ID, data["id"])
	} else {
		t.Logf("ID is correct: %s", id)
	}

	if eventName, ok := data["event_name"].(string); !ok || eventName != expectedEvent.EventName {
		t.Errorf("EventName mismatch: expected %s, got %v", expectedEvent.EventName, data["event_name"])
	} else {
		t.Logf("Event name is correct: %s", eventName)
	}

	if txHash, ok := data["tx_hash"].(string); !ok || txHash != expectedEvent.TxHash {
		t.Errorf("TxHash mismatch: expected %s, got %v", expectedEvent.TxHash, data["tx_hash"])
	} else {
		t.Logf("Transaction hash is correct: %s", txHash)
	}

	if chainName, ok := data["chain_name"].(string); !ok || chainName != expectedEvent.ChainName {
		t.Errorf("ChainName mismatch: expected %s, got %v", expectedEvent.ChainName, data["chain_name"])
	} else {
		t.Logf("Chain name is correct: %s", chainName)
	}

	// Validate event_data presence
	if eventData, ok := data["event_data"].(map[string]interface{}); !ok {
		t.Error("event_data field is missing or not a map")
	} else {
		t.Logf("Event data present with %d fields", len(eventData))

		// Print all fields in event_data
		for key, value := range eventData {
			t.Logf("  - %s: %v (type: %T)", key, value, value)
		}
	}
}

// contains checks if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
