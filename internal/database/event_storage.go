package database

import (
	"context"
	"encoding/json"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/Luckyx-Labs/chainscan-sync/internal/types"
	appLogger "github.com/Luckyx-Labs/chainscan-sync/pkg/logger"
)

func (s *PostgresStorage) SaveEvent(ctx context.Context, event *types.Event) error {

	eventDataJSON, err := json.Marshal(event.EventData)
	if err != nil {
		return fmt.Errorf("failed to serialize event data: %w", err)
	}

	eventDataStr := string(eventDataJSON)
	var rawDataStr *string
	if len(event.RawData) > 0 {
		raw := string(event.RawData)
		rawDataStr = &raw
	}

	dbEvent := Event{
		ID:              event.ID,
		ChainType:       string(event.ChainType),
		ChainName:       event.ChainName,
		BlockNumber:     event.BlockNumber,
		BlockHash:       event.BlockHash,
		TxHash:          event.TxHash,
		TxIndex:         event.TxIndex,
		LogIndex:        event.LogIndex,
		ContractAddress: event.ContractAddr,
		EventName:       event.EventName,
		FromAddress:     event.FromAddress,
		ToAddress:       event.ToAddress,
		TokenAddress:    event.TokenAddress,
		GasUsed:         event.GasUsed,
		GasLimit:        event.GasLimit,
		GasPrice:        event.GasPrice,
		EventData:       &eventDataStr,
		RawData:         rawDataStr,
		Timestamp:       event.Timestamp,
		Confirmations:   event.Confirmations,
		IsFinalized:     event.IsFinalized,
		Processed:       false,
		NotifyStatus:    NotifyStatusPending,
	}

	// use transaction to ensure data integrity
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Save or update event
		result := tx.Where("id = ?", event.ID).FirstOrCreate(&dbEvent)

		if result.Error != nil {
			return fmt.Errorf("failed to save event: %w", result.Error)
		}

		return nil
	})

	if err != nil {
		return err
	}

	appLogger.Debugf("Event saved: %s [%s/%s]", event.ID, event.ChainName, event.EventName)
	return nil
}

// GetEvent gets an event by its ID
func (s *PostgresStorage) GetEvent(ctx context.Context, eventID string) (*types.Event, error) {
	var dbEvent Event

	result := s.db.WithContext(ctx).Model(&Event{}).Where("id = ?", eventID).First(&dbEvent)
	if result.Error == gorm.ErrRecordNotFound {
		return nil, fmt.Errorf("event not found: %s", eventID)
	}
	if result.Error != nil {
		return nil, fmt.Errorf("failed to get event: %w", result.Error)
	}

	// Convert to application layer event object
	return s.dbEventToAppEvent(&dbEvent)
}

// IsEventProcessed checks if an event has been processed
func (s *PostgresStorage) IsEventProcessed(ctx context.Context, eventID string) (bool, error) {
	var dbEvent Event

	result := s.db.WithContext(ctx).
		Select("processed").
		Where("id = ?", eventID).
		First(&dbEvent)

	if result.Error == gorm.ErrRecordNotFound {
		return false, nil // Event not found, consider as not processed
	}
	if result.Error != nil {
		return false, fmt.Errorf("failed to query event status: %w", result.Error)
	}

	return dbEvent.Processed, nil
}

// MarkEventAsProcessed marks an event as processed
func (s *PostgresStorage) MarkEventAsProcessed(ctx context.Context, eventID string) error {
	result := s.db.WithContext(ctx).
		Model(&Event{}).
		Where("id = ?", eventID).
		Update("processed", true)

	if result.Error != nil {
		return fmt.Errorf("failed to mark event as processed: %w", result.Error)
	}

	if result.RowsAffected == 0 {
		return fmt.Errorf("event not found: %s", eventID)
	}

	return nil
}

// GetEventsByChain gets events for a specific chain
func (s *PostgresStorage) GetEventsByChain(ctx context.Context, chainType types.ChainType, chainName string, limit int) ([]*types.Event, error) {
	var dbEvents []Event

	result := s.db.WithContext(ctx).
		Where("chain_type = ? AND chain_name = ?", chainType, chainName).
		Order("timestamp DESC").
		Limit(limit).
		Find(&dbEvents)

	if result.Error != nil {
		return nil, fmt.Errorf("failed to query chain events: %w", result.Error)
	}

	return s.dbEventsToAppEvents(dbEvents)
}

// GetUnprocessedEvents gets unprocessed events
func (s *PostgresStorage) GetUnprocessedEvents(ctx context.Context, limit int) ([]*types.Event, error) {
	var dbEvents []Event

	result := s.db.WithContext(ctx).
		Where("processed = ?", false).
		Order("timestamp ASC").
		Limit(limit).
		Find(&dbEvents)

	if result.Error != nil {
		return nil, fmt.Errorf("failed to query unprocessed events: %w", result.Error)
	}

	return s.dbEventsToAppEvents(dbEvents)
}

// GetEventsByContract gets events for a specific contract
func (s *PostgresStorage) GetEventsByContract(ctx context.Context, contractAddr string, limit int) ([]*types.Event, error) {
	var dbEvents []Event

	result := s.db.WithContext(ctx).
		Where("contract_address = ?", contractAddr).
		Order("timestamp DESC").
		Limit(limit).
		Find(&dbEvents)

	if result.Error != nil {
		return nil, fmt.Errorf("failed to query contract events: %w", result.Error)
	}

	return s.dbEventsToAppEvents(dbEvents)
}

// BatchSaveEvents batch saves multiple events
func (s *PostgresStorage) BatchSaveEvents(ctx context.Context, events []*types.Event) error {
	if len(events) == 0 {
		return nil
	}

	dbEvents := make([]Event, 0, len(events))
	for _, event := range events {
		eventDataJSON, _ := json.Marshal(event.EventData)
		rawDataJSON := event.RawData
		if rawDataJSON == nil {
			rawDataJSON = []byte("{}")
		}

		// Convert to string pointers
		eventDataStr := string(eventDataJSON)
		rawDataStr := string(rawDataJSON)

		dbEvents = append(dbEvents, Event{
			ID:              event.ID,
			ChainType:       string(event.ChainType),
			ChainName:       event.ChainName,
			BlockNumber:     event.BlockNumber,
			TxHash:          event.TxHash,
			ContractAddress: event.ContractAddr,
			EventName:       event.EventName,
			EventData:       &eventDataStr,
			RawData:         &rawDataStr,
			Timestamp:       event.Timestamp,
			Processed:       false,
		})
	}

	// Use OnConflict Do Nothing to avoid duplicate entries
	result := s.db.WithContext(ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		Create(&dbEvents)

	if result.Error != nil {
		return fmt.Errorf("failed to batch save events: %w", result.Error)
	}

	appLogger.Infof("Successfully saved %d events in batch", len(events))
	return nil
}

// GetEventsByTxHash gets all events for a specific transaction
func (s *PostgresStorage) GetEventsByTxHash(ctx context.Context, txHash string) ([]*types.Event, error) {
	var dbEvents []Event
	result := s.db.WithContext(ctx).
		Where("tx_hash = ?", txHash).
		Order("log_index ASC").
		Find(&dbEvents)

	if result.Error != nil {
		return nil, fmt.Errorf("failed to get events by tx hash: %w", result.Error)
	}

	// Convert to types.Event
	events := make([]*types.Event, 0, len(dbEvents))
	for i := range dbEvents {
		event, err := s.dbEventToAppEvent(&dbEvents[i])
		if err != nil {
			appLogger.Warnf("Failed to convert event %s: %v", dbEvents[i].ID, err)
			continue
		}
		events = append(events, event)
	}

	return events, nil
}

// dbEventToAppEvent converts a database event to an application layer event
func (s *PostgresStorage) dbEventToAppEvent(dbEvent *Event) (*types.Event, error) {
	var eventData map[string]interface{}
	if dbEvent.EventData != nil {
		if err := json.Unmarshal([]byte(*dbEvent.EventData), &eventData); err != nil {
			appLogger.Warnf("failed to deserialize event data: %v", err)
			eventData = make(map[string]interface{})
		}
	} else {
		eventData = make(map[string]interface{})
	}

	var rawData []byte
	if dbEvent.RawData != nil {
		rawData = []byte(*dbEvent.RawData)
	}

	return &types.Event{
		ID:           dbEvent.ID,
		ChainType:    types.ChainType(dbEvent.ChainType),
		ChainName:    dbEvent.ChainName,
		BlockNumber:  dbEvent.BlockNumber,
		BlockHash:    dbEvent.BlockHash,
		TxHash:       dbEvent.TxHash,
		TxIndex:      dbEvent.TxIndex,
		LogIndex:     dbEvent.LogIndex,
		ContractAddr: dbEvent.ContractAddress,
		EventName:    dbEvent.EventName,
		FromAddress:  dbEvent.FromAddress,
		ToAddress:    dbEvent.ToAddress,
		TokenAddress: dbEvent.TokenAddress,
		EventData:    eventData,
		RawData:      rawData,
		GasUsed:      dbEvent.GasUsed,
		GasLimit:     dbEvent.GasLimit,
		GasPrice:     dbEvent.GasPrice,
		Timestamp:    dbEvent.Timestamp,
	}, nil
}

// dbEventsToAppEvents batch converts database events to application layer events
func (s *PostgresStorage) dbEventsToAppEvents(dbEvents []Event) ([]*types.Event, error) {
	events := make([]*types.Event, 0, len(dbEvents))
	for i := range dbEvents {
		event, err := s.dbEventToAppEvent(&dbEvents[i])
		if err != nil {
			appLogger.Warnf("Failed to convert event: %v", err)
			continue
		}
		events = append(events, event)
	}
	return events, nil
}
