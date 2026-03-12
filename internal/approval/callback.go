package approval

import (
	"log"
	"net/http"
)

// CallbackHandler handles HTTP callbacks for approval actions.
type CallbackHandler struct {
	store *Store
}

// NewCallbackHandler creates a handler for approval callback URLs.
func NewCallbackHandler(store *Store) *CallbackHandler {
	return &CallbackHandler{store: store}
}

// ServeHTTP handles GET /callback/approval?id=xxx&action=approve|reject&token=xxx
func (h *CallbackHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	action := r.URL.Query().Get("action")
	token := r.URL.Query().Get("token")

	if id == "" || (action != "approve" && action != "reject") {
		http.Error(w, "invalid parameters: id and action (approve|reject) required", http.StatusBadRequest)
		return
	}

	if token == "" || !h.store.ValidateToken(id, token) {
		log.Printf("[callback] invalid token for approval %s", id)
		http.Error(w, "invalid or missing token", http.StatusForbidden)
		return
	}

	approved := action == "approve"
	ok := h.store.Resolve(id, approved)

	if !ok {
		http.Error(w, "request not found or already resolved", http.StatusNotFound)
		return
	}

	log.Printf("[callback] approval %s: %s", id, action)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if approved {
		_, _ = w.Write([]byte(`<!DOCTYPE html><html><body><h2>Approved</h2><p>The operation will be executed.</p></body></html>`))
	} else {
		_, _ = w.Write([]byte(`<!DOCTYPE html><html><body><h2>Rejected</h2><p>The operation has been rejected.</p></body></html>`))
	}
}
