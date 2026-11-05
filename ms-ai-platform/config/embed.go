// Package config embeds the default platform configuration files.
package config

import _ "embed"

// LLMProviders is the default list of LLM providers (llm-providers.yaml).
//
//go:embed llm-providers.yaml
var LLMProviders []byte
