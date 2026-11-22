package orders

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
)

// Lister is what the handler needs from the service.
type Lister interface {
	OrdersWithItems(ctx context.Context, customerID int64) ([]OrderWithItems, error)
}

// Handler serves the orders HTTP endpoints.
type Handler struct {
	lister Lister
}

// NewHandler returns a Handler over lister.
func NewHandler(lister Lister) *Handler {
	return &Handler{lister: lister}
}

// Register mounts the routes on mux.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /customers/{id}/orders", h.listOrders)
}

func (h *Handler) listOrders(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid customer id", http.StatusBadRequest)
		return
	}
	out, err := h.lister.OrdersWithItems(r.Context(), id)
	if err != nil {
		slog.Error("list orders failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}
