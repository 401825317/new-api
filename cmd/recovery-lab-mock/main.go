// A loopback-only fault injector for the isolated recovery lab image.
package main

import (
	"fmt"
	"github.com/QuantumNous/new-api/common"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

func main() {
	var mu sync.Mutex
	counts := map[string]int{}
	send := func(w http.ResponseWriter, v any) {
		b, _ := common.Marshal(v)
		fmt.Fprintf(w, "data: %s\n\n", b)
		w.(http.Flusher).Flush()
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/stats", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		b, _ := common.Marshal(counts)
		w.Header().Set("Content-Type", "application/json")
		w.Write(b)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		mode := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")[0]
		if r.Method != "POST" || !strings.HasSuffix(r.URL.Path, "/v1/responses") {
			http.NotFound(w, r)
			return
		}
		mu.Lock()
		counts[mode]++
		mu.Unlock()
		failure := map[string]any{"code": "server_error", "message": "RECOVERY_LAB simulated upstream overload", "type": "service_unavailable_error"}
		if mode == "http503" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(503)
			b, _ := common.Marshal(map[string]any{"error": failure})
			w.Write(b)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		send(w, map[string]any{"type": "response.created", "sequence_number": 0, "response": map[string]any{"id": "resp_lab_start", "status": "in_progress", "output": []any{}}})
		if mode == "eof" {
			return
		}
		if mode == "partial" || mode == "ok" {
			send(w, map[string]any{"type": "response.output_text.delta", "sequence_number": 1, "output_index": 0, "content_index": 0, "delta": "RECOVERY_LAB_OK"})
		}
		if mode == "ok" {
			send(w, map[string]any{"type": "response.completed", "sequence_number": 2, "response": map[string]any{"id": "resp_lab_ok", "object": "response", "model": "gpt-5.6-terra", "status": "completed", "output": []any{}, "usage": map[string]int{"input_tokens": 10, "output_tokens": 2, "total_tokens": 12}}})
			return
		}
		if mode == "failed" {
			send(w, map[string]any{"type": "response.failed", "sequence_number": 4, "response": map[string]any{"status": "failed", "error": failure}})
			return
		}
		send(w, map[string]any{"type": "error", "sequence_number": 4, "error": failure})
	})
	server := &http.Server{Addr: "127.0.0.1:3101", Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	log.Fatal(server.ListenAndServe())
}
