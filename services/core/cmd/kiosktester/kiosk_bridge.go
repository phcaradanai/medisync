package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// kioskCommand is delivered only to browser sessions authenticated as the
// selected immutable kiosk code. The tester and kiosk need no opener link.
type kioskCommand struct {
	ID        uint64 `json:"id"`
	KioskCode string `json:"kioskCode"`
	Type      string `json:"type"`
	Code      string `json:"code,omitempty"`
	CardToken string `json:"cardToken,omitempty"`
	SentAt    string `json:"sentAt"`
}

type kioskCommandBroker struct {
	mu      sync.Mutex
	nextID  uint64
	clients map[string]map[chan kioskCommand]struct{}
}

func newKioskCommandBroker() *kioskCommandBroker {
	return &kioskCommandBroker{clients: make(map[string]map[chan kioskCommand]struct{})}
}

func (b *kioskCommandBroker) subscribe(kioskCode string) (<-chan kioskCommand, func()) {
	ch := make(chan kioskCommand, 16)
	b.mu.Lock()
	if b.clients[kioskCode] == nil {
		b.clients[kioskCode] = make(map[chan kioskCommand]struct{})
	}
	b.clients[kioskCode][ch] = struct{}{}
	b.mu.Unlock()

	return ch, func() {
		b.mu.Lock()
		delete(b.clients[kioskCode], ch)
		if len(b.clients[kioskCode]) == 0 {
			delete(b.clients, kioskCode)
		}
		b.mu.Unlock()
	}
}

func (b *kioskCommandBroker) publish(cmd kioskCommand) (kioskCommand, int) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.nextID++
	cmd.ID = b.nextID
	cmd.SentAt = time.Now().UTC().Format(time.RFC3339Nano)
	delivered := 0
	for ch := range b.clients[cmd.KioskCode] {
		select {
		case ch <- cmd:
			delivered++
		default:
			// A stalled browser must not block commands for healthy kiosks.
		}
	}
	return cmd, delivered
}

func (b *kioskCommandBroker) clientCount(kioskCode string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.clients[kioskCode])
}

func (b *kioskCommandBroker) handleStatus(w http.ResponseWriter, req *http.Request) {
	setKioskBridgeCORS(w)
	if req.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if req.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	kioskCode := strings.TrimSpace(req.URL.Query().Get("kioskCode"))
	if !validBridgeKioskCode(kioskCode) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "kioskCode must be exactly 8 digits"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"kioskCode": kioskCode, "connected": b.clientCount(kioskCode)})
}

func (b *kioskCommandBroker) handleEvents(w http.ResponseWriter, req *http.Request) {
	setKioskBridgeCORS(w)
	if req.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if req.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	kioskCode := strings.TrimSpace(req.URL.Query().Get("kioskCode"))
	if !validBridgeKioskCode(kioskCode) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "kioskCode must be exactly 8 digits"})
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	events, unsubscribe := b.subscribe(kioskCode)
	defer unsubscribe()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()

	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-req.Context().Done():
			return
		case cmd := <-events:
			payload, err := json.Marshal(cmd)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "id: %d\ndata: %s\n\n", cmd.ID, payload)
			flusher.Flush()
		case <-heartbeat.C:
			fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}

func (b *kioskCommandBroker) handleCommand(w http.ResponseWriter, req *http.Request) {
	setKioskBridgeCORS(w)
	if req.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var cmd kioskCommand
	if err := json.NewDecoder(req.Body).Decode(&cmd); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	cmd.Type = strings.TrimSpace(cmd.Type)
	cmd.KioskCode = strings.TrimSpace(cmd.KioskCode)
	cmd.Code = strings.TrimSpace(cmd.Code)
	cmd.CardToken = strings.TrimSpace(cmd.CardToken)
	if !validBridgeKioskCode(cmd.KioskCode) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "kioskCode must be exactly 8 digits"})
		return
	}
	switch cmd.Type {
	case "scan_sticker":
		if cmd.Code == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "scan_sticker requires code"})
			return
		}
	case "scan_card":
		if cmd.CardToken == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "scan_card requires cardToken"})
			return
		}
	default:
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "type must be scan_sticker or scan_card"})
		return
	}

	published, recipients := b.publish(cmd)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"id":         published.ID,
		"recipients": recipients,
	})
}

func validBridgeKioskCode(code string) bool {
	if len(code) != 8 || code[:4] == "0000" || code[4:] == "0000" {
		return false
	}
	for _, ch := range code {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}

func setKioskBridgeCORS(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
}
