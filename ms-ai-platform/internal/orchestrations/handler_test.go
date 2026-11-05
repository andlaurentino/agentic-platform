package orchestrations

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"agentic-platform/ms-ai-platform/internal/llmproviders/llmtest"
	"agentic-platform/ms-ai-platform/internal/spec"
	"agentic-platform/ms-ai-platform/internal/store"
)

// Needs a Postgres database: DATABASE_URL_TEST=postgres://postgres@localhost:5433/agentic_test?sslmode=disable
func newServer(t *testing.T) *httptest.Server {
	t.Helper()
	url := os.Getenv("DATABASE_URL_TEST")
	if url == "" {
		t.Skip("DATABASE_URL_TEST not set")
	}
	st, err := store.Open(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	mux := http.NewServeMux()
	(&Handler{Store: st, Validator: spec.NewValidator(llmtest.Catalog(llmtest.Gateway("http://127.0.0.1:1/v1", "claude-haiku/claude-haiku-5-5")))}).Routes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func do(t *testing.T, method, url, body string, headers map[string]string) (*http.Response, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest(method, url, strings.NewReader(body))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp, out
}

func TestDraftOptimisticConcurrency(t *testing.T) {
	srv := newServer(t)
	resp, orch := do(t, "POST", srv.URL+"/v1/orchestrations", `{"name":"Customer Care"}`, nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create: %d %v", resp.StatusCode, orch)
	}
	id := orch["id"].(string)
	t.Cleanup(func() { do(t, "DELETE", srv.URL+"/v1/orchestrations/"+id, "", nil) })

	resp, draft := do(t, "GET", srv.URL+"/v1/orchestrations/"+id+"/draft", "", nil)
	if resp.StatusCode != 200 || resp.Header.Get("ETag") != `"1"` {
		t.Fatalf("get draft: %d etag=%s", resp.StatusCode, resp.Header.Get("ETag"))
	}
	specJSON, _ := json.Marshal(draft["spec"])
	body := `{"spec":` + string(specJSON) + `,"updatedByName":"Ana"}`

	if resp, _ := do(t, "PUT", srv.URL+"/v1/orchestrations/"+id+"/draft", body, nil); resp.StatusCode != http.StatusPreconditionRequired {
		t.Fatalf("no If-Match: got %d, want 428", resp.StatusCode)
	}
	resp, saved := do(t, "PUT", srv.URL+"/v1/orchestrations/"+id+"/draft", body, map[string]string{"If-Match": `"1"`})
	if resp.StatusCode != 200 || saved["version"].(float64) != 2 {
		t.Fatalf("first save: %d %v", resp.StatusCode, saved)
	}
	// A second editor still holding version 1 gets a conflict with the current draft.
	resp, conflict := do(t, "PUT", srv.URL+"/v1/orchestrations/"+id+"/draft", body, map[string]string{"If-Match": `"1"`})
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("stale save: got %d, want 409", resp.StatusCode)
	}
	if d := conflict["draft"].(map[string]any); d["version"].(float64) != 2 || d["updatedByName"] != "Ana" {
		t.Fatalf("conflict should return current draft: %v", d)
	}
	if !strings.Contains(conflict["error"].(map[string]any)["message"].(string), "Ana") {
		t.Fatalf("conflict message should name the editor: %v", conflict["error"])
	}

	resp, result := do(t, "POST", srv.URL+"/v1/orchestrations/"+id+"/draft/validate", `{}`, nil)
	if resp.StatusCode != 200 || result["valid"] != true {
		t.Fatalf("starter draft should be valid: %v", result)
	}

	resp, list := do(t, "GET", srv.URL+"/v1/orchestrations/"+id, "", nil)
	agents := list["agents"].([]any)
	if resp.StatusCode != 200 || len(agents) != 1 || agents[0].(map[string]any)["root"] != true {
		t.Fatalf("orchestration summary: %v", list)
	}
}
