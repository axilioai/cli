// Package api is a minimal HTTP client for the axilio REST API.
//
// TEMPORARY: this hand-written client covers only the handful of endpoints the
// CLI needs today. It is a placeholder for the Fern-generated Go SDK
// (github.com/axilioai/axilio-go), which will replace it once the go-sdk
// generation group is wired up. Command code talks to this via small methods,
// so swapping in the generated SDK is a localized change.
package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const defaultBaseURL = "https://api.axilio.ai"

// Client talks to the axilio backend with an axl_ API key.
type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

// New builds a client. baseURL is the host (the /api/v1 prefix is added here).
func New(apiKey, baseURL string) *Client {
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	return &Client{
		baseURL: baseURL + "/api/v1",
		apiKey:  apiKey,
		http:    &http.Client{Timeout: 60 * time.Second},
	}
}

// BaseURL is the resolved API host (with the /api/v1 prefix).
func (c *Client) BaseURL() string { return c.baseURL }

// Error is an API error carrying the status and the server's message.
type Error struct {
	StatusCode int
	Message    string
	Body       string
}

func (e *Error) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("HTTP %d: %s", e.StatusCode, e.Message)
	}
	return fmt.Sprintf("HTTP %d: %s", e.StatusCode, e.Body)
}

func (c *Client) do(method, path string, body, out any) ([]byte, error) {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.baseURL+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Axilio-Api-Key", c.apiKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		e := &Error{StatusCode: resp.StatusCode, Body: string(raw)}
		var em struct {
			Detail  string `json:"detail"`
			Message string `json:"message"`
			ErrMsg  string `json:"error"`
		}
		if json.Unmarshal(raw, &em) == nil {
			for _, m := range []string{em.Detail, em.Message, em.ErrMsg} {
				if m != "" {
					e.Message = m
					break
				}
			}
		}
		return raw, e
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			return raw, err
		}
	}
	return raw, nil
}

// --- typed responses (subset; the generated Go SDK will supersede these) ---

type Balance struct {
	BalanceDisplay string `json:"balance_display"`
}

type AllocateResponse struct {
	SessionID    string `json:"session_id"`
	PhoneID      string `json:"phone_id"`
	Region       string `json:"region"`
	ControlURL   string `json:"control_url"`
	LiveViewURL  string `json:"live_view_url"`
	TelemetryURL string `json:"telemetry_url"`
}

type ActiveSession struct {
	SessionID    string `json:"session_id"`
	PhoneID      string `json:"phone_id"`
	PhoneType    string `json:"phone_type"`
	ModelName    string `json:"model_name"`
	WorkflowName string `json:"workflow_name"`
}

type ActiveSessions struct {
	Sessions []ActiveSession `json:"sessions"`
	Total    int             `json:"total"`
}

type Phone struct {
	PhoneID   string `json:"phone_id"`
	PhoneType string `json:"phone_type"`
	ModelName string `json:"model_name"`
	Status    string `json:"status"`
}

type AvailablePhones struct {
	Phones []Phone `json:"phones"`
}

// AllocateRequest is the body for POST /phones/allocate.
type AllocateRequest struct {
	PhoneType  string `json:"phone_type"`
	PhoneID    string `json:"phone_id,omitempty"`
	WorkflowID string `json:"workflow_id,omitempty"`
}

// --- methods ---

func (c *Client) GetBalance() ([]byte, *Balance, error) {
	var b Balance
	raw, err := c.do(http.MethodGet, "/billing/balance", nil, &b)
	return raw, &b, err
}

func (c *Client) AvailablePhones() ([]byte, *AvailablePhones, error) {
	var r AvailablePhones
	raw, err := c.do(http.MethodGet, "/phones/available", nil, &r)
	return raw, &r, err
}

func (c *Client) ActiveSessions() ([]byte, *ActiveSessions, error) {
	var r ActiveSessions
	raw, err := c.do(http.MethodGet, "/phones/sessions/active", nil, &r)
	return raw, &r, err
}

func (c *Client) Allocate(req AllocateRequest) ([]byte, *AllocateResponse, error) {
	var r AllocateResponse
	raw, err := c.do(http.MethodPost, "/phones/allocate", req, &r)
	return raw, &r, err
}

func (c *Client) Deallocate(phoneID string) ([]byte, error) {
	return c.do(http.MethodPost, "/phones/deallocate", map[string]string{"phone_id": phoneID}, nil)
}
