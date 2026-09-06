package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

type blueGreenRun struct {
	Active       bool `json:"active"`
	Completed    bool `json:"completed"`
	Disconnected bool `json:"disconnected"`
	TimedOut     bool `json:"timed_out"`
	Chunks       int  `json:"chunks"`
	release      chan struct{}
	released     bool
}

type blueGreenLab struct {
	mu       sync.Mutex
	color    string
	deadline time.Duration
	runs     map[string]*blueGreenRun
}

func newBlueGreenLab(color string, deadline time.Duration) *blueGreenLab {
	if color != "green" {
		color = "blue"
	}
	return &blueGreenLab{color: color, deadline: deadline, runs: map[string]*blueGreenRun{}}
}

func (b *blueGreenLab) state(w http.ResponseWriter, r *http.Request) {
	b.mu.Lock()
	defer b.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"color": b.color, "runs": b.runs})
}

func (b *blueGreenLab) release(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(405)
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	run := b.runs[r.URL.Query().Get("run")]
	if run == nil {
		http.NotFound(w, r)
		return
	}
	if !run.released {
		close(run.release)
		run.released = true
	}
	w.WriteHeader(http.StatusNoContent)
}

var blueGreenRunID = regexp.MustCompile(`^[a-z0-9-]{1,80}$`)

func (b *blueGreenLab) stream(w http.ResponseWriter, r *http.Request, input string) bool {
	long := strings.HasPrefix(input, "BG_LONG:")
	if !long && !strings.HasPrefix(input, "BG_QUICK:") {
		return false
	}
	id := strings.SplitN(input, ":", 2)[1]
	if !blueGreenRunID.MatchString(id) {
		http.Error(w, "invalid lab run", 400)
		return true
	}
	run := &blueGreenRun{Active: true, release: make(chan struct{})}
	b.mu.Lock()
	if _, exists := b.runs[id]; exists || len(b.runs) >= 1000 {
		b.mu.Unlock()
		http.Error(w, "duplicate run or lab limit", 409)
		return true
	}
	b.runs[id] = run
	b.mu.Unlock()
	defer func() { b.mu.Lock(); run.Active = false; b.mu.Unlock() }()
	w.Header().Set("Content-Type", "text/event-stream")
	seq := 0
	send := func(event map[string]any) bool {
		event["sequence_number"] = seq
		seq++
		payload, _ := json.Marshal(event)
		if _, err := fmt.Fprintf(w, "data: %s\n\n", payload); err != nil {
			return false
		}
		return http.NewResponseController(w).Flush() == nil
	}
	responseID := "resp_bg_" + b.color + "_" + id
	disconnect := func() { b.mu.Lock(); run.Disconnected = true; b.mu.Unlock() }
	if !send(map[string]any{"type": "response.created", "response": map[string]any{"id": responseID, "status": "in_progress", "output": []any{}}}) {
		disconnect()
		return true
	}
	delta := func() bool {
		b.mu.Lock()
		run.Chunks++
		n := run.Chunks
		b.mu.Unlock()
		return send(map[string]any{"type": "response.output_text.delta", "delta": fmt.Sprintf("BG_%s:%d", b.color, n), "output_index": 0, "content_index": 0})
	}
	if !delta() {
		disconnect()
		return true
	}
	if long {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		timer := time.NewTimer(b.deadline)
		defer timer.Stop()
	wait:
		for {
			select {
			case <-run.release:
				break wait
			case <-r.Context().Done():
				disconnect()
				return true
			case <-timer.C:
				b.mu.Lock()
				run.TimedOut = true
				b.mu.Unlock()
				send(map[string]any{"type": "error", "error": map[string]string{"code": "lab_timeout", "message": "lab release deadline reached"}})
				return true
			case <-ticker.C:
				if !delta() {
					disconnect()
					return true
				}
			}
		}
	}
	b.mu.Lock()
	n := run.Chunks
	b.mu.Unlock()
	ok := send(map[string]any{"type": "response.completed", "response": map[string]any{"id": responseID, "object": "response", "model": "gpt-5.6-terra", "status": "completed", "output": []any{}, "usage": map[string]int{"input_tokens": 10, "output_tokens": n, "total_tokens": 10 + n}}})
	b.mu.Lock()
	run.Completed = ok
	run.Disconnected = !ok
	b.mu.Unlock()
	return true
}
