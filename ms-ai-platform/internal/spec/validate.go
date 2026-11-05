package spec

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"agentic-platform/lib-agent-assets/pkg/assets"
)

// Severity of a validation problem. Errors block runs; warnings do not.
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
)

// Problem is one validation finding, located by a JSON-ish path such as "agents[agt_1].model".
type Problem struct {
	Path     string   `json:"path"`
	AgentID  string   `json:"agentId,omitempty"`
	Message  string   `json:"message"`
	Severity Severity `json:"severity"`
}

// Catalog is what validation needs to know about assets.
type Catalog interface {
	Manifests(kind assets.Kind) []assets.Manifest
}

var agentIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// Validator checks orchestration specs. Manifests are read on every
// validation, since LLM model lists change at runtime; compiled config schemas
// are cached per schema content.
type Validator struct {
	catalog Catalog
	mu      sync.Mutex
	schemas map[string]*jsonschema.Schema // by schema hash
}

func NewValidator(c Catalog) *Validator {
	return &Validator{catalog: c, schemas: map[string]*jsonschema.Schema{}}
}

// HasErrors reports whether any problem is an error.
func HasErrors(ps []Problem) bool {
	return slices.ContainsFunc(ps, func(p Problem) bool { return p.Severity == SeverityError })
}

// Validate checks o. skillExists reports whether a skill id exists.
func (v *Validator) Validate(o *Orchestration, skillExists func(id string) bool) []Problem {
	var ps []Problem
	manifests := map[string]assets.Manifest{}
	for _, m := range v.catalog.Manifests("") {
		manifests[m.ID] = m
	}
	errf := func(path, agentID, format string, args ...any) {
		ps = append(ps, Problem{Path: path, AgentID: agentID, Message: fmt.Sprintf(format, args...), Severity: SeverityError})
	}
	warnf := func(path, agentID, format string, args ...any) {
		ps = append(ps, Problem{Path: path, AgentID: agentID, Message: fmt.Sprintf(format, args...), Severity: SeverityWarning})
	}

	if len(o.Agents) == 0 {
		errf("agents", "", "add at least one agent")
		return ps
	}

	ids := map[string]bool{}
	names := map[string]string{}
	for _, a := range o.Agents {
		p := fmt.Sprintf("agents[%s]", a.ID)
		if !agentIDPattern.MatchString(a.ID) {
			errf(p+".id", a.ID, "agent id %q must be 1-64 letters, digits, '-' or '_'", a.ID)
		}
		if ids[a.ID] {
			errf(p+".id", a.ID, "duplicate agent id %q", a.ID)
		}
		ids[a.ID] = true
		if a.Name == "" {
			errf(p+".name", a.ID, "name is required")
		} else if other, dup := names[RuntimeName(a)]; dup {
			errf(p+".name", a.ID, "name %q is too similar to agent %q; names must be unique", a.Name, other)
		} else {
			names[RuntimeName(a)] = a.Name
		}
		if a.Description == "" {
			warnf(p+".description", a.ID, "add a description so other agents know when to hand off to this one")
		}
		v.checkAsset(manifests, &ps, p+".model", a.ID, a.Model, assets.KindLLM, true)
		for i, s := range a.Skills {
			if skillExists != nil && !skillExists(s) {
				errf(fmt.Sprintf("%s.skills[%d]", p, i), a.ID, "skill %q does not exist", s)
			}
		}
		for i, t := range a.Tools {
			tp := fmt.Sprintf("%s.tools[%d]", p, i)
			switch {
			case t.MCP != "" && t.Asset != "":
				errf(tp, a.ID, "a tool entry is either an asset or an MCP selection, not both")
			case t.MCP != "":
				m, ok := v.checkAsset(manifests, &ps, tp+".mcp", a.ID, AssetRef{Asset: t.MCP}, assets.KindMCP, false)
				if !ok {
					continue
				}
				if len(t.Tools) == 0 {
					errf(tp+".tools", a.ID, "select at least one tool from %s", m.Name)
				}
				allowed, _ := m.Extra["allowedTools"].([]string)
				for _, name := range t.Tools {
					if !slices.Contains(allowed, name) {
						errf(tp+".tools", a.ID, "tool %q is not in the allow-list of %s", name, m.Name)
					}
				}
			default:
				v.checkAsset(manifests, &ps, tp, a.ID, AssetRef{Asset: t.Asset, Config: t.Config}, assets.KindTool, true)
			}
		}
		for i, g := range a.Guardrails {
			v.checkAsset(manifests, &ps, fmt.Sprintf("%s.guardrails[%d]", p, i), a.ID, g, assets.KindGuardrail, true)
		}
	}
	for i, g := range o.Guardrails {
		v.checkAsset(manifests, &ps, fmt.Sprintf("guardrails[%d]", i), "", g, assets.KindGuardrail, true)
	}

	// Graph rules.
	if o.RootAgent == "" {
		errf("rootAgent", "", "choose a root agent")
	} else if !ids[o.RootAgent] {
		errf("rootAgent", "", "root agent %q does not exist", o.RootAgent)
	}
	parents := map[string]string{}
	for i, e := range o.Edges {
		p := fmt.Sprintf("edges[%d]", i)
		if !ids[e.From] || !ids[e.To] {
			errf(p, "", "edge %s -> %s points at a missing agent", e.From, e.To)
			continue
		}
		if e.From == e.To {
			errf(p, e.From, "an agent cannot delegate to itself")
		}
		if e.To == o.RootAgent {
			errf(p, e.To, "the root agent cannot have a parent")
		}
		switch e.Mode {
		case EdgeTransfer:
			if prev, dup := parents[e.To]; dup && prev != e.From {
				errf(p, e.To, "an agent can be handed off to by only one parent (already a sub-agent of %s)", prev)
			}
			parents[e.To] = e.From
		case EdgeTool:
		default:
			errf(p+".mode", "", "edge mode must be %q or %q", EdgeTransfer, EdgeTool)
		}
	}
	if cyc := findCycle(o); cyc != "" {
		errf("edges", cyc, "agents form a cycle through %s", cyc)
	}
	reach := o.Reachable()
	for _, a := range o.Agents {
		if o.RootAgent != "" && !reach[a.ID] {
			warnf(fmt.Sprintf("agents[%s]", a.ID), a.ID, "%s is not connected to the root agent and will not run", a.Name)
		}
	}
	return ps
}

func findCycle(o *Orchestration) string {
	state := map[string]int{} // 1 visiting, 2 done
	var visit func(string) string
	visit = func(id string) string {
		switch state[id] {
		case 1:
			return id
		case 2:
			return ""
		}
		state[id] = 1
		for _, e := range o.Edges {
			if e.From == id {
				if c := visit(e.To); c != "" {
					return c
				}
			}
		}
		state[id] = 2
		return ""
	}
	for _, a := range o.Agents {
		if c := visit(a.ID); c != "" {
			return c
		}
	}
	return ""
}

func (v *Validator) checkAsset(manifests map[string]assets.Manifest, ps *[]Problem, path, agentID string, ref AssetRef, kind assets.Kind, checkConfig bool) (assets.Manifest, bool) {
	add := func(msg string) {
		*ps = append(*ps, Problem{Path: path, AgentID: agentID, Message: msg, Severity: SeverityError})
	}
	if ref.Asset == "" {
		add(fmt.Sprintf("choose a %s", kind))
		return assets.Manifest{}, false
	}
	m, ok := manifests[ref.Asset]
	if !ok {
		add(fmt.Sprintf("asset %q is not in the catalog", ref.Asset))
		return m, false
	}
	if m.Kind != kind {
		add(fmt.Sprintf("asset %q is a %s, expected a %s", ref.Asset, m.Kind, kind))
		return m, false
	}
	if kind == assets.KindLLM {
		if models, ok := m.Extra["models"].([]assets.ModelInfo); ok && len(models) == 0 {
			problem, _ := m.Extra["problem"].(string)
			if problem == "" {
				problem = m.Name + " has no models available"
			}
			add(problem)
			return m, false
		}
		var cfg struct {
			Model string `json:"model"`
		}
		_ = json.Unmarshal(ref.Config, &cfg)
		if models, ok := m.Extra["models"].([]assets.ModelInfo); ok && cfg.Model != "" &&
			!slices.ContainsFunc(models, func(mi assets.ModelInfo) bool { return mi.ID == cfg.Model }) {
			add(fmt.Sprintf("model %q is not offered by %s; choose another model", cfg.Model, m.Name))
			return m, false
		}
	}
	if m.Lifecycle == assets.LifecycleDeprecated {
		*ps = append(*ps, Problem{Path: path, AgentID: agentID, Message: fmt.Sprintf("%s is deprecated", m.Name), Severity: SeverityWarning})
	}
	if checkConfig {
		if err := v.validateConfig(m, ref.Config); err != nil {
			add(fmt.Sprintf("%s config: %v", m.Name, err))
		}
	}
	return m, true
}

func (v *Validator) validateConfig(m assets.Manifest, raw json.RawMessage) error {
	schema, err := v.schema(m)
	if err != nil {
		return err
	}
	if len(bytes.TrimSpace(raw)) == 0 || string(raw) == "null" {
		raw = json.RawMessage(`{}`)
	}
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return err
	}
	if err := schema.Validate(inst); err != nil {
		if ve, ok := err.(*jsonschema.ValidationError); ok {
			var msgs []string
			for _, c := range ve.BasicOutput().Errors {
				if c.Error != nil && c.InstanceLocation != "" {
					msgs = append(msgs, fmt.Sprintf("%s: %s", c.InstanceLocation, c.Error.String()))
				}
			}
			if len(msgs) > 0 {
				return fmt.Errorf("%s", msgs[0])
			}
		}
		return err
	}
	return nil
}

func (v *Validator) schema(m assets.Manifest) (*jsonschema.Schema, error) {
	b, err := json.Marshal(m.ConfigSchema)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(b)
	key := hex.EncodeToString(sum[:])
	v.mu.Lock()
	defer v.mu.Unlock()
	if s, ok := v.schemas[key]; ok {
		return s, nil
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	c := jsonschema.NewCompiler()
	url := "mem://" + m.ID + ".json"
	if err := c.AddResource(url, doc); err != nil {
		return nil, err
	}
	s, err := c.Compile(url)
	if err != nil {
		return nil, err
	}
	v.schemas[key] = s
	return s, nil
}
