package runtime

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"polymarket-signal/internal/connectors/polymarket"
)

type APIServer struct {
	gamma polymarket.GammaClient
	clob  polymarket.CLOBClient
	mux   *http.ServeMux
}

func NewAPIServer(gamma polymarket.GammaClient, clob polymarket.CLOBClient) *APIServer {
	s := &APIServer{
		gamma: gamma,
		clob:  clob,
		mux:   http.NewServeMux(),
	}
	s.routes()
	return s
}

func (s *APIServer) Handler() http.Handler {
	return s.mux
}

func (s *APIServer) routes() {
	s.mux.HandleFunc("/health", s.handleHealth)
	s.mux.HandleFunc("/markets", s.handleMarkets)
	s.mux.HandleFunc("/events", s.handleEvents)
	s.mux.HandleFunc("/orderbook", s.handleOrderBook)
	s.mux.HandleFunc("/trades", s.handleTrades)
}

func (s *APIServer) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *APIServer) handleMarkets(w http.ResponseWriter, r *http.Request) {
	activeOnly := !strings.EqualFold(r.URL.Query().Get("active"), "false")
	markets, err := s.gamma.GetMarkets(r.Context(), activeOnly)
	if err != nil {
		writeError(w, http.StatusBadGateway, fmt.Errorf("fetch markets: %w", err))
		return
	}
	writeJSON(w, http.StatusOK, markets)
}

func (s *APIServer) handleEvents(w http.ResponseWriter, r *http.Request) {
	events, err := s.gamma.GetEvents(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, fmt.Errorf("fetch events: %w", err))
		return
	}
	writeJSON(w, http.StatusOK, events)
}

func (s *APIServer) handleOrderBook(w http.ResponseWriter, r *http.Request) {
	tokenID := strings.TrimSpace(r.URL.Query().Get("token_id"))
	if tokenID == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("token_id is required"))
		return
	}

	book, err := s.clob.GetOrderBook(r.Context(), tokenID)
	if err != nil {
		writeError(w, http.StatusBadGateway, fmt.Errorf("fetch orderbook: %w", err))
		return
	}
	writeJSON(w, http.StatusOK, book)
}

func (s *APIServer) handleTrades(w http.ResponseWriter, r *http.Request) {
	tokenID := strings.TrimSpace(r.URL.Query().Get("token_id"))
	if tokenID == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("token_id is required"))
		return
	}

	since, err := parseSince(r.URL.Query().Get("since"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	trades, err := s.clob.GetTradesSince(r.Context(), tokenID, since)
	if err != nil {
		writeError(w, http.StatusBadGateway, fmt.Errorf("fetch trades: %w", err))
		return
	}
	writeJSON(w, http.StatusOK, trades)
}

func parseSince(value string) (time.Time, error) {
	if strings.TrimSpace(value) == "" {
		return time.Now().Add(-5 * time.Minute).UTC(), nil
	}
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid since (expect RFC3339): %w", err)
	}
	return t.UTC(), nil
}

func writeJSON(w http.ResponseWriter, statusCode int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, statusCode int, err error) {
	writeJSON(w, statusCode, map[string]string{"error": err.Error()})
}
