package handler

import (
	"context"
	"encoding/json"

	"github.com/Luckyx-Labs/chainscan-sync/internal/interfaces"
	"github.com/Luckyx-Labs/chainscan-sync/internal/types"
	"github.com/Luckyx-Labs/chainscan-sync/pkg/logger"
)

type LoggerHandler struct {
	name string
}

func NewLoggerHandler() interfaces.EventHandler {
	return &LoggerHandler{
		name: "LoggerHandler",
	}
}

func (h *LoggerHandler) HandleEvent(ctx context.Context, event *types.Event) error {
	eventJSON, err := json.MarshalIndent(event, "", "  ")
	if err != nil {
		return err
	}

	logger.WithFields(map[string]interface{}{
		"handler": h.name,
		"chain":   event.ChainName,
		"event":   event.EventName,
		"tx":      event.TxHash,
	}).Infof("handled event:\n%s", string(eventJSON))

	return nil
}

func (h *LoggerHandler) GetHandlerName() string {
	return h.name
}

func (h *LoggerHandler) CanHandle(event *types.Event) bool {
	return true
}
