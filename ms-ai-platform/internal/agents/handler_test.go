package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"agentic-platform/ms-ai-platform/internal/llmproviders/llmtest"
	"agentic-platform/ms-ai-platform/internal/orchestrations"
	"agentic-platform/ms-ai-platform/internal/spec"
	"agentic-platform/ms-ai-platform/internal/store"
)

// Needs a Postgres database: DATABASE_URL_TEST=postgres://postgres@localhost:5433/agentic_test?sslmode=disable
func newServer(t *testing.T) (*httptest.Server, *store.Store) {
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
	validator := spec.NewValidator(llmtest.Catalog(llmtest.Gateway("http://127.0.0.1:1/v1", "claude-haiku/claude-haiku-5-5")))
	mux := http.NewServeMux()
	(&Handler{Store: st, Validator: validator}).Routes(mux)
	(&orchestrations.Handler{Store: st, Validator: validator}).Routes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, st
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

func ifMatch(v any) map[string]string { return map[string]string{"If-Match": fmt.Sprintf(`"%v"`, v)} }

// One library agent used by two orchestrations: an edit shows up in both, stale
// edits conflict, and the agent cannot be deleted while it is used.
func TestSharedAgent(t *testing.T) {
	srv, st := newServer(t)
	ctx := context.Background()

	resp, agent := do(t, "POST", srv.URL+"/v1/agents", `{"name":" Weather ","description":"Forecasts","updatedByName":"Ana"}`, nil)
	if resp.StatusCode != http.StatusCreated || agent["name"] != "Weather" || agent["version"].(float64) != 1 {
		t.Fatalf("create: %d %v", resp.StatusCode, agent)
	}
	agentID := agent["id"].(string)
	t.Cleanup(func() { do(t, "DELETE", srv.URL+"/v1/agents/"+agentID, "", nil) })
	if m := agent["model"].(map[string]any); m["asset"] != spec.DefaultLLM {
		t.Fatalf("default model = %v", m)
	}

	// Add the agent under the root of two orchestrations.
	var orchIDs []string
	for _, name := range []string{"Care", "Sales"} {
		_, o := do(t, "POST", srv.URL+"/v1/orchestrations", `{"name":"`+name+`"}`, nil)
		id := o["id"].(string)
		orchIDs = append(orchIDs, id)
		t.Cleanup(func() { do(t, "DELETE", srv.URL+"/v1/orchestrations/"+id, "", nil) })
		_, d := do(t, "GET", srv.URL+"/v1/orchestrations/"+id+"/draft", "", nil)
		g := d["spec"].(map[string]any)
		root := g["rootAgent"].(string)
		g["agents"] = append(g["agents"].([]any), agentID)
		g["edges"] = []any{map[string]any{"from": root, "to": agentID, "mode": "transfer"}}
		body, _ := json.Marshal(map[string]any{"spec": g})
		if resp, out := do(t, "PUT", srv.URL+"/v1/orchestrations/"+id+"/draft", string(body), ifMatch(d["version"])); resp.StatusCode != 200 {
			t.Fatalf("save draft: %d %v", resp.StatusCode, out)
		}
	}

	_, got := do(t, "GET", srv.URL+"/v1/agents/"+agentID, "", nil)
	if used := got["usedBy"].([]any); len(used) != 2 {
		t.Fatalf("usedBy = %v", used)
	}

	edit := `{"name":"Weather","instructions":"Answer with the forecast.","model":{"asset":"core.llm.gateway","config":{}}}`
	if resp, out := do(t, "PUT", srv.URL+"/v1/agents/"+agentID, edit, nil); resp.StatusCode != http.StatusPreconditionRequired {
		t.Fatalf("no If-Match: %d %v", resp.StatusCode, out)
	}
	if resp, out := do(t, "PUT", srv.URL+"/v1/agents/"+agentID, edit, ifMatch(1)); resp.StatusCode != 200 || out["version"].(float64) != 2 {
		t.Fatalf("save agent: %d %v", resp.StatusCode, out)
	}
	resp, conflict := do(t, "PUT", srv.URL+"/v1/agents/"+agentID, edit, ifMatch(1))
	if resp.StatusCode != http.StatusConflict || conflict["agent"].(map[string]any)["version"].(float64) != 2 {
		t.Fatalf("stale save: %d %v", resp.StatusCode, conflict)
	}

	for _, id := range orchIDs {
		o, err := st.ResolveDraft(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		a, ok := o.Agent(agentID)
		if !ok || a.Instructions != "Answer with the forecast." {
			t.Fatalf("%s resolves %+v", id, a)
		}
		if _, result := do(t, "POST", srv.URL+"/v1/orchestrations/"+id+"/draft/validate", `{}`, nil); result["valid"] != true {
			t.Fatalf("resolved draft should be valid: %v", result)
		}
	}

	resp, out := do(t, "DELETE", srv.URL+"/v1/agents/"+agentID, "", nil)
	if resp.StatusCode != http.StatusConflict || !strings.Contains(out["error"].(map[string]any)["message"].(string), "Care") {
		t.Fatalf("delete in use: %d %v", resp.StatusCode, out)
	}

	// A graph may only use agents from the library.
	_, d := do(t, "GET", srv.URL+"/v1/orchestrations/"+orchIDs[0]+"/draft", "", nil)
	g := d["spec"].(map[string]any)
	g["agents"] = append(g["agents"].([]any), "agt_missing")
	body, _ := json.Marshal(map[string]any{"spec": g})
	if resp, out := do(t, "PUT", srv.URL+"/v1/orchestrations/"+orchIDs[0]+"/draft", string(body), ifMatch(d["version"])); resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("unknown agent: %d %v", resp.StatusCode, out)
	}

	// Deleting the orchestrations frees the agent; it stays in the library until deleted.
	for _, id := range orchIDs {
		do(t, "DELETE", srv.URL+"/v1/orchestrations/"+id, "", nil)
	}
	if resp, _ := do(t, "DELETE", srv.URL+"/v1/agents/"+agentID, "", nil); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete unused: %d", resp.StatusCode)
	}
}

func TestValidateAgent(t *testing.T) {
	srv, _ := newServer(t)
	_, out := do(t, "POST", srv.URL+"/v1/agents/validate",
		`{"agent":{"id":"agt_x","name":"","model":{"asset":"core.llm.gateway","config":{"model":"nope/nope"}}}}`, nil)
	problems, _ := json.Marshal(out["problems"])
	if out["valid"] != false || !strings.Contains(string(problems), "name is required") || !strings.Contains(string(problems), "not offered") {
		t.Fatalf("validate: %v", out)
	}
}
