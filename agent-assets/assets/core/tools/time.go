// Package tools holds native tool assets.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"

	"agentic-platform/agent-assets/sdk"
)

const CurrentTimeID = "core.tool.current-time"

// CurrentTimeConfig lets builders pick the timezone used when the model does not give one.
type CurrentTimeConfig struct {
	DefaultTimezone string `json:"defaultTimezone,omitempty" jsonschema:"title=Default timezone,default=UTC" jsonschema_description:"IANA timezone, for example Europe/Lisbon."`
}

type CurrentTime struct{}

func (CurrentTime) Manifest() sdk.Manifest {
	return sdk.Manifest{
		ID:           CurrentTimeID,
		Kind:         sdk.KindTool,
		Domain:       "core",
		Name:         "Current time",
		Description:  "Returns the current date and time in a timezone.",
		Owner:        "@ai-platform",
		Tags:         []string{"utility"},
		Lifecycle:    sdk.LifecycleStable,
		ConfigSchema: sdk.SchemaFor[CurrentTimeConfig](),
		Extra:        map[string]any{"sideEffects": "read"},
	}
}

type currentTimeArgs struct {
	Timezone string `json:"timezone,omitempty" jsonschema:"IANA timezone, for example Europe/Lisbon. Optional."`
}

type currentTimeResult struct {
	Timezone string `json:"timezone"`
	Time     string `json:"time"`
	Weekday  string `json:"weekday"`
}

func (CurrentTime) New(_ context.Context, raw json.RawMessage, _ sdk.Secrets) (tool.Tool, error) {
	cfg, err := sdk.DecodeConfig(raw, CurrentTimeConfig{DefaultTimezone: "UTC"})
	if err != nil {
		return nil, err
	}
	if _, err := time.LoadLocation(cfg.DefaultTimezone); err != nil {
		return nil, fmt.Errorf("defaultTimezone: %w", err)
	}
	return functiontool.New(functiontool.Config{
		Name:        "current_time",
		Description: "Get the current date and time in a timezone.",
	}, func(_ agent.ToolContext, args currentTimeArgs) (currentTimeResult, error) {
		tz := args.Timezone
		if tz == "" {
			tz = cfg.DefaultTimezone
		}
		loc, err := time.LoadLocation(tz)
		if err != nil {
			return currentTimeResult{}, fmt.Errorf("unknown timezone %q", tz)
		}
		now := time.Now().In(loc)
		return currentTimeResult{Timezone: tz, Time: now.Format(time.RFC3339), Weekday: now.Weekday().String()}, nil
	})
}
