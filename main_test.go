package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAssessPayment(t *testing.T) {
	tests := []struct {
		name     string
		event    PaymentEvent
		accepted bool
	}{
		{"authorized", PaymentEvent{Amount: 1250, Currency: "USD", Customer: "cus_1", Status: "authorized"}, true},
		{"zero amount", PaymentEvent{Amount: 0, Currency: "USD", Customer: "cus_1", Status: "authorized"}, false},
		{"pending", PaymentEvent{Amount: 1250, Currency: "USD", Customer: "cus_1", Status: "pending"}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := assess(tc.event).Accepted; got != tc.accepted {
				t.Fatalf("accepted=%v, want %v", got, tc.accepted)
			}
		})
	}
}

func TestRegisterAndSubscribeRequests(t *testing.T) {
	var requests []struct {
		path string
		body map[string]any
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		var decoded map[string]any
		if err := json.Unmarshal(body, &decoded); err != nil {
			t.Fatal(err)
		}
		requests = append(requests, struct {
			path string
			body map[string]any
		}{r.URL.Path, decoded})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	client := &InfraiClient{baseURL: server.URL, key: "key", httpClient: server.Client()}
	callback := "https://payments.example.com/webhooks/payments"
	if err := register(client, callback, "secret"); err != nil {
		t.Fatal(err)
	}
	if err := subscribe(client, callback); err != nil {
		t.Fatal(err)
	}

	if got := requests[0].body["url"]; got != callback {
		t.Fatalf("register url=%v, want %q", got, callback)
	}
	if got := requests[1].path; got != "/v1/queue/push_subscribe/payments" {
		t.Fatalf("subscribe path=%q", got)
	}
	if got := requests[1].body["queue"]; got != "payments" {
		t.Fatalf("subscribe queue=%v", got)
	}
	if got := requests[1].body["url"]; got != callback {
		t.Fatalf("subscribe url=%v, want %q", got, callback)
	}
}
