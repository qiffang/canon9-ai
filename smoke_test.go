// Package engram9 contains smoke tests that hit a real engram9 server.
//
// Run:
//
//	ENGRAM9_URL=http://localhost:9090 go test -v -run TestSmoke -tags smoke -count=1 ./...
//
// Or against the deployed ALB:
//
//	ENGRAM9_URL=http://k8s-engram9-engram9s-50104033c5-2126112170.ap-southeast-1.elb.amazonaws.com \
//	  go test -v -run TestSmoke -tags smoke -count=1 ./...

//go:build smoke

package engram9

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

var baseURL string

func TestMain(m *testing.M) {
	baseURL = strings.TrimRight(os.Getenv("ENGRAM9_URL"), "/")
	if baseURL == "" {
		fmt.Fprintln(os.Stderr, "ENGRAM9_URL not set, skipping smoke tests")
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// --- helpers ---

type apiResp struct {
	Result string `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

type statusResp struct {
	EventCount      int `json:"event_count"`
	UncompiledCount int `json:"uncompiled_count"`
	WikiPageCount   int `json:"wiki_page_count"`
	ArchivedCount   int `json:"archived_page_count"`
}

func postJSON(t *testing.T, path string, body any) (*http.Response, []byte) {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Post(baseURL+path, "application/json", bytes.NewReader(data))
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	respBody, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp, respBody
}

func getJSON(t *testing.T, path string) (*http.Response, []byte) {
	t.Helper()
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(baseURL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp, body
}

// --- smoke tests ---

func TestSmoke_Status(t *testing.T) {
	resp, body := getJSON(t, "/status")
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d body=%s", resp.StatusCode, body)
	}

	var s statusResp
	if err := json.Unmarshal(body, &s); err != nil {
		t.Fatalf("unmarshal: %v (body=%s)", err, body)
	}
	t.Logf("status: events=%d uncompiled=%d wiki=%d archived=%d",
		s.EventCount, s.UncompiledCount, s.WikiPageCount, s.ArchivedCount)
}

func TestSmoke_RememberAndRecall(t *testing.T) {
	// 1. Remember a unique fact.
	marker := fmt.Sprintf("smoke-test-%d", time.Now().UnixNano())
	fact := fmt.Sprintf("The smoke test marker is %s. This is a deployment verification test.", marker)

	t.Log("remembering:", fact)
	resp, body := postJSON(t, "/remember", map[string]string{"text": fact})
	if resp.StatusCode != 200 {
		t.Fatalf("remember: status=%d body=%s", resp.StatusCode, body)
	}
	var rResp apiResp
	if err := json.Unmarshal(body, &rResp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if rResp.Error != "" {
		t.Fatalf("remember error: %s", rResp.Error)
	}
	if rResp.Result == "" {
		t.Fatal("remember: empty result")
	}
	t.Logf("remember result: %.200s", rResp.Result)

	// 2. Verify event count increased.
	_, statusBody := getJSON(t, "/status")
	var s statusResp
	if err := json.Unmarshal(statusBody, &s); err != nil {
		t.Fatalf("unmarshal status: %v", err)
	}
	if s.EventCount == 0 {
		t.Error("expected event_count > 0 after remember")
	}
	t.Logf("after remember: events=%d wiki=%d", s.EventCount, s.WikiPageCount)

	// 3. Recall the fact.
	t.Log("recalling marker...")
	resp2, body2 := postJSON(t, "/recall", map[string]string{
		"question": fmt.Sprintf("What is the smoke test marker? Look for %s", marker),
	})
	if resp2.StatusCode != 200 {
		t.Fatalf("recall: status=%d body=%s", resp2.StatusCode, body2)
	}
	var qResp apiResp
	if err := json.Unmarshal(body2, &qResp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if qResp.Error != "" {
		t.Fatalf("recall error: %s", qResp.Error)
	}
	if qResp.Result == "" {
		t.Fatal("recall: empty result")
	}
	t.Logf("recall result: %.200s", qResp.Result)
}

func TestSmoke_Compile(t *testing.T) {
	resp, body := postJSON(t, "/compile", map[string]string{})
	if resp.StatusCode != 200 {
		t.Fatalf("compile: status=%d body=%s", resp.StatusCode, body)
	}
	var cResp apiResp
	if err := json.Unmarshal(body, &cResp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if cResp.Error != "" {
		t.Fatalf("compile error: %s", cResp.Error)
	}
	t.Logf("compile result: %.200s", cResp.Result)
}

func TestSmoke_BadRequests(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
		body   string
		want   int
	}{
		{"remember empty text", "POST", "/remember", `{"text":""}`, 400},
		{"recall empty question", "POST", "/recall", `{"question":""}`, 400},
		{"remember bad json", "POST", "/remember", `{bad`, 400},
		{"recall bad json", "POST", "/recall", `{bad`, 400},
		{"remember GET", "GET", "/remember", "", 405},
		{"recall GET", "GET", "/recall", "", 405},
	}

	client := &http.Client{Timeout: 10 * time.Second}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var body io.Reader
			if tt.body != "" {
				body = strings.NewReader(tt.body)
			}
			req, err := http.NewRequest(tt.method, baseURL+tt.path, body)
			if err != nil {
				t.Fatal(err)
			}
			if tt.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			resp, err := client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode != tt.want {
				t.Errorf("status=%d, want %d", resp.StatusCode, tt.want)
			}
		})
	}
}
