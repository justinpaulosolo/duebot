package discord

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

const apiBase = "https://discord.com/api/v10"

type REST struct {
	Token  string
	Client *http.Client
}

func NewREST(token string) *REST {
	return &REST{Token: token, Client: &http.Client{Timeout: 10 * time.Second}}
}

// CreateDMChannel opens (or fetches the existing) DM channel with a user,
// returning its channel ID — you send messages to a channel ID, not a user
// ID directly.
func (r *REST) CreateDMChannel(userID string) (string, error) {
	body, _ := json.Marshal(map[string]string{"recipient_id": userID})
	var result struct {
		ID string `json:"id"`
	}
	if err := r.doWithRetry(http.MethodPost, apiBase+"/users/@me/channels", body, &result, 3); err != nil {
		return "", err
	}
	return result.ID, nil
}

func (r *REST) SendMessage(channelID, content string) error {
	body, _ := json.Marshal(map[string]string{"content": content})
	return r.doWithRetry(http.MethodPost, apiBase+"/channels/"+channelID+"/messages", body, nil, 3)
}

func (r *REST) doWithRetry(method, url string, body []byte, out interface{}, maxAttempts int) error {
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		req, err := http.NewRequest(method, url, bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bot "+r.Token)

		resp, err := r.Client.Do(req)
		if err != nil {
			return err
		}

		if resp.StatusCode == http.StatusTooManyRequests {
			retryAfter := 1 * time.Second
			if v := resp.Header.Get("Retry-After"); v != "" {
				if secs, perr := strconv.ParseFloat(v, 64); perr == nil {
					retryAfter = time.Duration(secs * float64(time.Second))
				}
			}
			resp.Body.Close()
			lastErr = fmt.Errorf("rate limited, retry after %s", retryAfter)
			time.Sleep(retryAfter)
			continue
		}

		defer resp.Body.Close()
		if resp.StatusCode >= 300 {
			return fmt.Errorf("discord API error: status %d", resp.StatusCode)
		}
		if out != nil {
			return json.NewDecoder(resp.Body).Decode(out)
		}
		return nil
	}
	return fmt.Errorf("giving up after %d attempts: %w", maxAttempts, lastErr)
}
