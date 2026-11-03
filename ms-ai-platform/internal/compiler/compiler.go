// Package compiler turns an orchestration spec into a tree of ADK agents.
package compiler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/agenttool"
	"google.golang.org/adk/tool/mcptoolset"

	"agentic-platform/ms-ai-platform/internal/catalog"
	"agentic-platform/ms-ai-platform/internal/guardrails"
	"agentic-platform/ms-ai-platform/internal/mcpclient"
	"agentic-platform/ms-ai-platform/internal/spec"
	"agentic-platform/ms-ai-platform/internal/store"
)

// ValidationError is returned when the spec has blocking problems.
type ValidationError struct {
	Problems []spec.Problem
}

func (e *ValidationError) Error() string {
	var msgs []string
	for _, p := range e.Problems {
		if p.Severity == spec.SeverityError {
			msgs = append(msgs, fmt.Sprintf("%s: %s", p.Path, p.Message))
		}
	}
	return "the orchestration has problems: " + strings.Join(msgs, "; ")
}

// Compiled is a runnable orchestration.
type Compiled struct {
	Root agent.Agent
	// AgentIDs maps the runtime agent name (event author) to the spec agent id.
	AgentIDs map[string]string
	closers  []io.Closer
}

// Close releases MCP connections opened for this run.
func (c *Compiled) Close() error {
	var errs []error
	for _, cl := range c.closers {
		errs = append(errs, cl.Close())
	}
	return errors.Join(errs...)
}

type Compiler struct {
	Catalog   *catalog.Catalog
	Validator *spec.Validator
}

// Compile validates o and builds the ADK agent tree. skills holds every skill by id.
// Guardrail decisions are sent to run.
func (c *Compiler) Compile(ctx context.Context, o spec.Orchestration, skills map[string]store.Skill, run *guardrails.Run) (*Compiled, error) {
	o.Normalize()
	problems := c.Validator.Validate(&o, func(id string) bool { _, ok := skills[id]; return ok })
	if spec.HasErrors(problems) {
		return nil, &ValidationError{Problems: problems}
	}

	shared, err := c.bindGuardrails(ctx, o.Guardrails)
	if err != nil {
		return nil, fmt.Errorf("orchestration guardrails: %w", err)
	}

	out := &Compiled{AgentIDs: map[string]string{}}
	ok := false
	defer func() {
		if !ok {
			_ = out.Close()
		}
	}()
	built := map[string]agent.Agent{}
	var build func(id string) (agent.Agent, error)
	build = func(id string) (agent.Agent, error) {
		if a, ok := built[id]; ok {
			return a, nil
		}
		a, _ := o.Agent(id)

		var subAgents []agent.Agent
		for _, child := range o.Children(id, spec.EdgeTransfer) {
			sub, err := build(child)
			if err != nil {
				return nil, err
			}
			subAgents = append(subAgents, sub)
		}

		llmAsset, _ := c.Catalog.LLM(a.Model.Asset)
		llm, err := llmAsset.New(ctx, a.Model.Config, c.Catalog.Secrets)
		if err != nil {
			return nil, fmt.Errorf("agent %s model: %w", a.Name, err)
		}

		var tools []tool.Tool
		var toolsets []tool.Toolset
		for _, t := range a.Tools {
			if t.MCP != "" {
				ts, closer, err := c.mcpToolset(t)
				if err != nil {
					return nil, fmt.Errorf("agent %s: %w", a.Name, err)
				}
				out.closers = append(out.closers, closer)
				toolsets = append(toolsets, ts)
				continue
			}
			toolAsset, _ := c.Catalog.Tool(t.Asset)
			tl, err := toolAsset.New(ctx, t.Config, c.Catalog.Secrets)
			if err != nil {
				return nil, fmt.Errorf("agent %s tool %s: %w", a.Name, t.Asset, err)
			}
			tools = append(tools, tl)
		}
		for _, child := range o.Children(id, spec.EdgeTool) {
			sub, err := build(child)
			if err != nil {
				return nil, err
			}
			tools = append(tools, agenttool.New(sub, nil))
		}

		own, err := c.bindGuardrails(ctx, a.Guardrails)
		if err != nil {
			return nil, fmt.Errorf("agent %s guardrails: %w", a.Name, err)
		}
		bounds := append(slices.Clone(shared), own...)

		instruction := buildInstruction(*a, skills)
		cfg := llmagent.Config{
			Name:        spec.RuntimeName(*a),
			Description: a.Description,
			Model:       llm,
			// A provider skips ADK's {placeholder} templating, so braces in instructions are safe.
			InstructionProvider: func(agent.ReadonlyContext) (string, error) { return instruction, nil },
			Tools:               tools,
			Toolsets:            toolsets,
			SubAgents:           subAgents,
		}
		if len(bounds) > 0 {
			cfg.BeforeModelCallbacks = []llmagent.BeforeModelCallback{guardrails.BeforeModel(run, a.ID, bounds)}
			cfg.AfterModelCallbacks = []llmagent.AfterModelCallback{guardrails.AfterModel(run, a.ID, bounds)}
		}
		ag, err := llmagent.New(cfg)
		if err != nil {
			return nil, fmt.Errorf("agent %s: %w", a.Name, err)
		}
		built[id] = ag
		out.AgentIDs[cfg.Name] = a.ID
		return ag, nil
	}

	root, err := build(o.RootAgent)
	if err != nil {
		return nil, err
	}
	out.Root = root
	ok = true
	return out, nil
}

func buildInstruction(a spec.Agent, skills map[string]store.Skill) string {
	var sb strings.Builder
	sb.WriteString(strings.TrimSpace(a.Instructions))
	for _, id := range a.Skills {
		s := skills[id]
		fmt.Fprintf(&sb, "\n\n## Skill: %s\n%s", s.Name, strings.TrimSpace(s.Content))
	}
	return sb.String()
}

func (c *Compiler) mcpToolset(t spec.ToolRef) (tool.Toolset, io.Closer, error) {
	m, _ := c.Catalog.MCP(t.MCP)
	conn, err := m.Connection(t.Config, c.Catalog.Secrets)
	if err != nil {
		return nil, nil, fmt.Errorf("MCP %s: %w", t.MCP, err)
	}
	transport, err := mcpclient.Transport(conn)
	if err != nil {
		return nil, nil, err
	}
	ts, err := mcptoolset.New(mcptoolset.Config{Transport: transport})
	if err != nil {
		return nil, nil, fmt.Errorf("MCP %s: %w", t.MCP, err)
	}
	closer, _ := ts.(io.Closer)
	if closer == nil {
		closer = io.NopCloser(nil)
	}
	var selected []string
	for _, name := range t.Tools {
		if slices.Contains(conn.AllowedTools, name) {
			selected = append(selected, name)
		}
	}
	return tool.FilterToolset(ts, tool.StringPredicate(selected)), closer, nil
}

func (c *Compiler) bindGuardrails(ctx context.Context, refs []spec.AssetRef) ([]guardrails.Bound, error) {
	var out []guardrails.Bound
	for _, ref := range refs {
		g, ok := c.Catalog.Guardrail(ref.Asset)
		if !ok {
			return nil, fmt.Errorf("guardrail %s not found", ref.Asset)
		}
		check, err := g.New(ctx, ref.Config, c.Catalog.Deps())
		if err != nil {
			return nil, fmt.Errorf("guardrail %s: %w", ref.Asset, err)
		}
		m := g.Manifest()
		out = append(out, guardrails.Bound{ID: m.ID, Name: m.Name, Level: g.Level(), Stages: g.Stages(), Check: check})
	}
	return out, nil
}
