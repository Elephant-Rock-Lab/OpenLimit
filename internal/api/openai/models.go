package openaiapi

import (
	"log/slog"
	"net/http"
	"sort"
	"time"

	"openlimit/internal/config"
	openaischema "openlimit/internal/schema/openai"
)

type ModelsHandler struct {
	cfg    config.Config
	logger *slog.Logger
}

func NewModelsHandler(cfg config.Config, logger *slog.Logger) *ModelsHandler {
	return &ModelsHandler{cfg: cfg, logger: logger}
}

func (h *ModelsHandler) Models(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}

	ids := make([]string, 0, len(h.cfg.Models))
	for id := range h.cfg.Models {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	created := time.Now().Unix()
	models := make([]openaischema.ModelInfo, 0, len(ids))
	for _, id := range ids {
		models = append(models, openaischema.ModelInfo{
			ID:      id,
			Object:  "model",
			Created: created,
			OwnedBy: ownerForModel(h.cfg.Models[id]),
		})
	}

	writeJSON(w, http.StatusOK, openaischema.ModelListResponse{
		Object: "list",
		Data:   models,
	})
}

func ownerForModel(model config.ModelConfig) string {
	if len(model.Routes) == 0 {
		return "openlimit"
	}
	return model.Routes[0].Provider
}
