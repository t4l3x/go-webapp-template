package response_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/t4l3x/go-webapp-template/internal/platform/httpserver/response"
)

func TestJSON(t *testing.T) {
	rec := httptest.NewRecorder()

	response.JSON(
		rec,
		http.StatusCreated,
		map[string]string{"id": "123"},
	)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}

	want := "application/json; charset=utf-8"
	if got := rec.Header().Get("Content-Type"); got != want {
		t.Fatalf("Content-Type = %q, want %q", got, want)
	}

	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}

	if body["id"] != "123" {
		t.Fatalf("body[id] = %q, want %q", body["id"], "123")
	}
}
