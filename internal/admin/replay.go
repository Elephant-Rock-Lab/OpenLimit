package admin

import (
	"encoding/json"
	"net/http"

	"openlimit/internal/replay"
)

// ReplayHandler returns an HTTP handler for the replay admin endpoint.
// Follows the closure pattern: dataFn is called on each GET request to
// produce the current replay summary.
func ReplayHandler(dataFn func() replay.ReplaySummary) http.HandlerFunc {
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
