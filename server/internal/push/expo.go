package push

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const DefaultExpoURL = "https://exp.host/--/api/v2/push/send"

// maxBatch — ограничение Expo на число сообщений в одном запросе.
const maxBatch = 100

type Message struct {
	To        string         `json:"to"`
	Title     string         `json:"title"`
	Body      string         `json:"body"`
	Sound     string         `json:"sound,omitempty"`
	ChannelID string         `json:"channelId,omitempty"`
	Data      map[string]any `json:"data,omitempty"`
}

// Result — ответ Expo на одно сообщение (в том же порядке, что и отправленные).
type Result struct {
	Status string `json:"status"` // "ok" или "error"
	// Error заполнен при status=error; DeviceNotRegistered значит, что токен больше не действует.
	Error string
}

// Sender отправляет push-уведомления. В тестах подменяется.
type Sender interface {
	Send(ctx context.Context, msgs []Message) ([]Result, error)
}

// ExpoSender шлёт уведомления через Expo Push API (https://docs.expo.dev/push-notifications/sending-notifications/).
type ExpoSender struct {
	URL         string // по умолчанию DefaultExpoURL
	AccessToken string // нужен, только если в проекте Expo включена защита push-токенов
	Client      *http.Client
}

func (s ExpoSender) Send(ctx context.Context, msgs []Message) ([]Result, error) {
	url := s.URL
	if url == "" {
		url = DefaultExpoURL
	}
	client := s.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}

	results := make([]Result, 0, len(msgs))
	for start := 0; start < len(msgs); start += maxBatch {
		batch := msgs[start:min(start+maxBatch, len(msgs))]
		got, err := s.sendBatch(ctx, client, url, batch)
		if err != nil {
			return results, err
		}
		results = append(results, got...)
	}
	return results, nil
}

func (s ExpoSender) sendBatch(ctx context.Context, client *http.Client, url string, batch []Message) ([]Result, error) {
	body, err := json.Marshal(batch)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if s.AccessToken != "" {
		req.Header.Set("Authorization", "Bearer "+s.AccessToken)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("expo push: status %d: %.200s", resp.StatusCode, raw)
	}

	var parsed struct {
		Data []struct {
			Status  string `json:"status"`
			Message string `json:"message"`
			Details struct {
				Error string `json:"error"`
			} `json:"details"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("expo push: bad response: %w", err)
	}
	if len(parsed.Data) != len(batch) {
		return nil, fmt.Errorf("expo push: got %d results for %d messages", len(parsed.Data), len(batch))
	}
	out := make([]Result, len(parsed.Data))
	for i, d := range parsed.Data {
		out[i] = Result{Status: d.Status}
		if d.Status != "ok" {
			out[i].Error = d.Details.Error
			if out[i].Error == "" {
				out[i].Error = d.Message
			}
		}
	}
	return out, nil
}
