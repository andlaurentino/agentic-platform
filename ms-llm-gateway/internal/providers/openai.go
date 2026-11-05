package providers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"

	"agentic-platform/ms-llm-gateway/internal/wire"
)

// openaiChat forwards the request to an OpenAI-compatible upstream. The body is
// passed through with the model id rewritten and the fields the upstream may
// not accept removed; responses stream back unchanged.
func (p *Provider) openaiChat(ctx context.Context, c Call, out Output) (Result, *wire.Error) {
	body, werr := p.openaiBody(c)
	if werr != nil {
		return Result{}, werr
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSuffix(p.Cfg.BaseURL, "/")+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Result{}, wire.Errorf(http.StatusInternalServerError, "server_error", "", "build upstream request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	p.setOpenAIHeaders(req)
	resp, err := p.http.Do(req)
	if err != nil {
		return Result{}, p.unreachable(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		return Result{}, p.upstreamError(resp.StatusCode, upstreamMessage(raw), c.Model.ID)
	}

	mediaType, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if !c.Req.Stream {
		raw, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
		if err != nil {
			return Result{}, p.unreachable(err)
		}
		var parsed struct {
			Usage wire.Usage `json:"usage"`
		}
		if err := json.Unmarshal(raw, &parsed); err != nil {
			return Result{}, wire.Errorf(http.StatusBadGateway, "upstream_error", "", "%s returned an invalid response: %v", p.Cfg.Name, err)
		}
		out.W.Header().Set("Content-Type", "application/json")
		_, _ = out.W.Write(raw)
		return Result{Usage: parsed.Usage}, nil
	}
	if mediaType != "text/event-stream" {
		return Result{}, wire.Errorf(http.StatusBadGateway, "upstream_error", "", "%s answered a streaming request with %q instead of an event stream", p.Cfg.Name, mediaType)
	}

	var res Result
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64<<10), 8<<20)
	for sc.Scan() {
		data, ok := bytes.CutPrefix(sc.Bytes(), []byte("data:"))
		if !ok {
			continue // event names, comments and blank separators
		}
		data = bytes.TrimSpace(data)
		if bytes.Contains(data, []byte(`"usage"`)) {
			var chunk struct {
				Usage *wire.Usage `json:"usage"`
			}
			if json.Unmarshal(data, &chunk) == nil && chunk.Usage != nil {
				res.Usage = *chunk.Usage
			}
		}
		if err := out.SSE.Raw(data); err != nil {
			return res, nil // client went away
		}
	}
	if err := sc.Err(); err != nil && !errors.Is(err, context.Canceled) {
		return res, p.unreachable(err)
	}
	return res, nil
}

// openaiBody rewrites the client's request for the upstream.
func (p *Provider) openaiBody(c Call) ([]byte, *wire.Error) {
	dec := json.NewDecoder(bytes.NewReader(c.Raw))
	dec.UseNumber() // keep numbers exactly as the client sent them
	var body map[string]any
	if err := dec.Decode(&body); err != nil {
		return nil, wire.BadRequest("invalid JSON body: %v", err)
	}
	body["model"] = c.Model.ID
	if msgs, ok := body["messages"].([]any); ok {
		for _, m := range msgs {
			if msg, ok := m.(map[string]any); ok {
				// Reasoning extension: Claude-only, unknown to other upstreams.
				delete(msg, "thinking_blocks")
				delete(msg, "reasoning_content")
			}
		}
	}
	if !p.Cfg.StreamUsageEnabled() {
		delete(body, "stream_options")
	}
	if !c.Model.Effort {
		delete(body, "reasoning_effort")
	}
	b, err := json.Marshal(body)
	if err != nil {
		return nil, wire.BadRequest("invalid JSON body: %v", err)
	}
	return b, nil
}

// setOpenAIHeaders adds the API key and the configured headers.
func (p *Provider) setOpenAIHeaders(req *http.Request) {
	if key := p.Cfg.APIKey(); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	for k, v := range p.Cfg.ExpandedHeaders() {
		req.Header.Set(k, v)
	}
}

func (p *Provider) unreachable(err error) *wire.Error {
	if errors.Is(err, context.Canceled) {
		return wire.Errorf(499, "request_cancelled", "", "the client cancelled the request")
	}
	return wire.Errorf(http.StatusBadGateway, "upstream_unavailable", "", "%s is unreachable: %v", p.Cfg.Name, err)
}
