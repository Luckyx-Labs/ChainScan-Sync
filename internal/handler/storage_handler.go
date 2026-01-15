package handler

import (
	"context"

	"github.com/Luckyx-Labs/chainscan-sync/internal/interfaces"
	"github.com/Luckyx-Labs/chainscan-sync/internal/types"
	"github.com/Luckyx-Labs/chainscan-sync/pkg/logger"
)

type StorageHandler struct {
	name    string
	storage interfaces.Storage
}

func NewStorageHandler(storage interfaces.Storage) interfaces.EventHandler {
	return &StorageHandler{
		name:    "StorageHandler",
		storage: storage,
	}
}

func (h *StorageHandler) HandleEvent(ctx context.Context, event *types.Event) error {
	processed, err := h.storage.IsEventProcessed(ctx, event.ID)
	if err != nil {
		logger.Errorf("Failed to check event status: %v", err)
	}

	if processed {
		logger.Warnf("Event already processed, skipping duplicate save: %s", event.ID)
		return nil
	}

	if err := h.storage.SaveEvent(ctx, event); err != nil {
		return err
	}

	logger.WithFields(map[string]interface{}{
		"handler": h.name,
		"chain":   event.ChainName,
		"event":   event.EventName,
		"tx":      event.TxHash,
		"block":   event.BlockNumber,
	}).Info("the event has been stored successfully")

	return nil
}

func (h *StorageHandler) GetHandlerName() string {
	return h.name
}

func (h *StorageHandler) CanHandle(event *types.Event) bool {
	return true
}
