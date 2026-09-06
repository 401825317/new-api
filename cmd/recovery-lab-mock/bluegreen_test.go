package main

import (
	"bufio"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestBlueGreenLongStreamRelease(t *testing.T) {
	b := newBlueGreenLab("blue", 5*time.Second)
	mux := http.NewServeMux()
	mux.HandleFunc("/stream", func(w http.ResponseWriter, r *http.Request) { b.stream(w, r, "BG_LONG:unit") })
	mux.HandleFunc("/release", b.release)
	s := httptest.NewServer(mux)
	defer s.Close()
	r, err := http.Get(s.URL + "/stream")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	scan := bufio.NewReader(r.Body)
	var first strings.Builder
	for !strings.Contains(first.String(), "BG_blue:1") {
		line, err := scan.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		first.WriteString(line)
	}
	b.mu.Lock()
	active := b.runs["unit"].Active
	b.mu.Unlock()
	if !active {
		t.Fatal("stream not active")
	}
	release, err := http.Post(s.URL+"/release?run=unit", "text/plain", nil)
	if err != nil {
		t.Fatal(err)
	}
	release.Body.Close()
	if release.StatusCode != 204 {
		t.Fatal(release.Status)
	}
	rest, err := io.ReadAll(scan)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(rest), `"type":"response.completed"`) != 1 {
		t.Fatal(string(rest))
	}
}

func TestBlueGreenTimeoutIsNotSuccess(t *testing.T) {
	b := newBlueGreenLab("green", time.Millisecond)
	w := httptest.NewRecorder()
	b.stream(w, httptest.NewRequest("POST", "/", nil), "BG_LONG:timeout")
	if strings.Contains(w.Body.String(), "response.completed") || !strings.Contains(w.Body.String(), "lab_timeout") {
		t.Fatal(w.Body.String())
	}
	if b.runs["timeout"].Active || !b.runs["timeout"].TimedOut {
		t.Fatal("bad terminal state")
	}
}
