package database

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Luckyx-Labs/chainscan-sync/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSaveEvent tests saving an event to the database
func TestSaveEvent(t *testing.T) {
	// Skip test if no database connection is set
	dsn := "host=192.168.1.xxxx port=5432 user=postgres password=xxxx dbname=game sslmode=disable"

	storage, err := NewPostgresStorage(dsn)
	require.NoError(t, err, "failed to create storage")
	defer storage.Close()

	ctx := context.Background()

	t.Run("save new event", func(t *testing.T) {
		event := &types.Event{
			ID:           "test-event-3",
			ChainType:    types.ChainTypeEVM,
			ChainName:    "base-sepolia",
			BlockNumber:  9578290,
			TxHash:       "0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
			ContractAddr: "0x522A377055E02A9054204e766b2EAaB1AB6252FF",
			EventName:    "Transfer",
			EventData: map[string]interface{}{
				"from":   "0xdddd...",
				"to":     "0xbbbb...",
				"amount": "1000000000000000000",
			},
			RawData:   []byte(`{"test": "data"}`),
			Timestamp: time.Now(),
		}

		// Execute save
		err := storage.SaveEvent(ctx, event)
		assert.NoError(t, err, "saving event should succeed")

		// Verify save result
		savedEvent, err := storage.GetEvent(ctx, event.ID)
		fmt.Println("savedEvent: ", savedEvent.ID, savedEvent.EventName, savedEvent.BlockNumber)
		assert.NoError(t, err, "getting event should succeed")
		assert.NotNil(t, savedEvent, "saved event should not be nil")
		assert.Equal(t, event.ID, savedEvent.ID)
		assert.Equal(t, event.ChainName, savedEvent.ChainName)
		assert.Equal(t, event.EventName, savedEvent.EventName)
		assert.Equal(t, event.BlockNumber, savedEvent.BlockNumber)
	})

}

// TestSaveEventConcurrency tests concurrent saving of events to the database
func TestSaveEventConcurrency(t *testing.T) {
	dsn := "host=192.168.1.xxxx port=5432 user=postgres password=xxxx dbname=game_test sslmode=disable"

	storage, err := NewPostgresStorage(dsn)
	require.NoError(t, err, "failed to create storage")
	defer storage.Close()

	ctx := context.Background()

	// Concurrently save the same event (test UPSERT)
	t.Run("concurrent save same event", func(t *testing.T) {
		eventID := "concurrent-event-1"

		// Create 10 goroutines to save the same event concurrently
		done := make(chan bool, 10)
		for i := 0; i < 10; i++ {
			go func(index int) {
				event := &types.Event{
					ID:           eventID,
					ChainType:    types.ChainTypeEVM,
					ChainName:    "base-sepolia",
					BlockNumber:  uint64(9578300 + index),
					TxHash:       "0xeeee...",
					ContractAddr: "0x522A377055E02A9054204e766b2EAaB1AB6252FF",
					EventName:    "Transfer",
					EventData: map[string]interface{}{
						"index": index,
					},
					RawData:   []byte(`{}`),
					Timestamp: time.Now(),
				}

				err := storage.SaveEvent(ctx, event)
				assert.NoError(t, err, "concurrent save should succeed")
				done <- true
			}(i)
		}

		// Wait for all goroutines to complete
		for i := 0; i < 10; i++ {
			<-done
		}

		// Verify only one record exists
		savedEvent, err := storage.GetEvent(ctx, eventID)
		assert.NoError(t, err, "getting event should succeed")
		assert.NotNil(t, savedEvent, "event should exist")
	})
}
