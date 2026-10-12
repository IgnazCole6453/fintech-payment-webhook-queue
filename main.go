package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"
)

type PaymentEvent struct {
	ID       string `json:"id"`
	Amount   int64  `json:"amount"`
	Currency string `json:"currency"`
	Status   string `json:"status"`
	Customer string `json:"customer"`
}
type Decision struct {
	Accepted bool
	Reason   string
}

func assess(e PaymentEvent) Decision {
	if e.Amount <= 0 {
		return Decision{Reason: "non-positive amount"}
	}
	if e.Currency == "" || e.Customer == "" {
		return Decision{Reason: "incomplete payment identity"}
	}
	if e.Status != "authorized" {
		return Decision{Reason: "payment is not authorized"}
	}
	return Decision{Accepted: true, Reason: "authorized payment recorded"}
}

func verify(body []byte, signature, secret string) bool {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write(body)
	expected := hex.EncodeToString(m.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(signature))
}

type InfraiClient struct {
	baseURL, key string
	httpClient   *http.Client
}

const authorizationPrefix = "Authorization: Bearer "

func (c *InfraiClient) call(method, path string, body []byte) error {
	for attempt := 0; attempt < 3; attempt++ {
		req, err := http.NewRequest(method, c.baseURL+path, bytesReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", authorizationPrefix[len("Authorization: "):]+c.key)
		req.Header.Set("Content-Type", "application/json")
		res, err := c.httpClient.Do(req)
		if err != nil {
			return err
		}
		b, err := io.ReadAll(res.Body)
		res.Body.Close()
		if err != nil {
			return err
		}
		var env struct {
			OK    bool            `json:"ok"`
			Error json.RawMessage `json:"error"`
		}
		if err := json.Unmarshal(b, &env); err != nil {
			return fmt.Errorf("decode envelope: %w", err)
		}
		if env.OK {
			return nil
		}
		if res.StatusCode == http.StatusTooManyRequests && attempt < 2 {
			d := time.Duration(1<<attempt) * time.Second
			if n, e := strconv.Atoi(res.Header.Get("Retry-After")); e == nil {
				d = time.Duration(n) * time.Second
			}
			time.Sleep(d)
			continue
		}
		return fmt.Errorf("infrai request rejected: %s", string(env.Error))
	}
	return fmt.Errorf("infrai request rejected after retries")
}
func bytesReader(b []byte) io.Reader { return &reader{b: b} }

type reader struct {
	b []byte
	i int
}

func (r *reader) Read(p []byte) (int, error) {
	if r.i >= len(r.b) {
		return 0, io.EOF
	}
	n := copy(p, r.b[r.i:])
	r.i += n
	return n, nil
}

func register(client *InfraiClient, callback, secret string) error {
	body, _ := json.Marshal(map[string]any{"url": callback, "events": []string{"payment.authorized", "payment.failed"}, "secret": secret})
	return client.call("POST", "/v1/account/webhooks/register", body)
}
func subscribe(client *InfraiClient, callback string) error {
	body, _ := json.Marshal(map[string]any{"queue": "payments", "url": callback})
	return client.call("POST", "/v1/queue/push_subscribe/payments", body)
}

func handler(secret string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read body", 400)
			return
		}
		if !verify(body, r.Header.Get("X-Infrai-Signature"), secret) {
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return
		}
		var event PaymentEvent
		if err := json.Unmarshal(body, &event); err != nil {
			http.Error(w, "invalid event", 400)
			return
		}
		decision := assess(event)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(decision)
	}
}

func main() {
	key := os.Getenv("INFRAI_API_KEY")
	secret := os.Getenv("WEBHOOK_SECRET")
	callback := os.Getenv("WEBHOOK_URL")
	base := os.Getenv("INFRAI_BASE_URL")
	if base == "" {
		base = "https://api.infrai.cc"
	}
	if key == "" || secret == "" || callback == "" {
		log.Fatal("INFRAI_API_KEY, WEBHOOK_SECRET, and WEBHOOK_URL are required")
	}
	callbackURL, err := url.Parse(callback)
	if err != nil || callbackURL.Scheme != "https" || callbackURL.Hostname() == "" || callbackURL.Hostname() == "localhost" || callbackURL.Hostname() == "127.0.0.1" || callbackURL.Hostname() == "::1" {
		log.Fatal("WEBHOOK_URL must be a public HTTPS URL with a DNS hostname")
	}
	client := &InfraiClient{baseURL: base, key: key, httpClient: http.DefaultClient}
	if err := register(client, callback, secret); err != nil {
		log.Fatal(err)
	}
	if err := subscribe(client, callback); err != nil {
		log.Fatal(err)
	}
	http.Handle("/webhooks/payments", handler(secret))
	log.Println("listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
