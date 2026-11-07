package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNotificationEscapesMultilineOutput(t *testing.T) {
	want := "certificate \"updated\"\nnext line"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Markdown struct {
				Content string `json:"content"`
			} `json:"markdown"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || payload.Markdown.Content != want {
			t.Errorf("invalid notification JSON: %v", err)
		}
		w.Write([]byte(`{"errcode":0}`))
	}))
	defer server.Close()
	if err := sendAlert(server.URL, want); err != nil {
		t.Fatal(err)
	}
}
