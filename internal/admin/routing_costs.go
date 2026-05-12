package admin

import (
	"encoding/json"
	"net/http"
)

type CostEntryJSON struct {
	Provider    string  `json:"provider"`
	Model       string  `json:"model"`
	InputPer1M  float64 `json:"input_per_1m"`
	OutputPer1M float64 `json:"output_per_1m"`
}

type RoutingCostsResponse struct {
	Models   []CostEntryJSON `json:"models"`
	Strategy string          `json:"strategy"`
	Weights  CostWeightsJSON `json:"weights"`
}

type CostWeightsJSON struct {
	Cost    float64 `json:"cost"`
	Latency float64 `json:"latency"`
	Health  float64 `json:"health"`
}

func RoutingCostsHandler(dataFn func() RoutingCostsResponse) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeAdminError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
			return
		}
		resp := dataFn()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}
