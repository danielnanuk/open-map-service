package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/danielnanuk/open-map-service/gateway/internal/fieldmask"
	"github.com/danielnanuk/open-map-service/gateway/internal/gapi"
)

func writeJSON(w http.ResponseWriter, r *http.Request, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	mask := r.Header.Get("X-Goog-FieldMask")
	if mask != "" && status == http.StatusOK {
		raw, _ := json.Marshal(payload)
		var m map[string]any
		json.Unmarshal(raw, &m)
		payload = fieldmask.Apply(m, mask)
	}
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, code int, status, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(gapi.ErrorBody{Error: gapi.ErrorDetail{Code: code, Message: msg, Status: status}})
}

func invalidArgument(w http.ResponseWriter, msg string) {
	writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", msg)
}

func internal(w http.ResponseWriter, err error) {
	writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
}
