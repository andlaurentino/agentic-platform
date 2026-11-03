// Package config embeds the default platform configuration files.
package config

import _ "embed"

// LLMs is the default LLM provider configuration (llms.yaml).
//
//go:embed llms.yaml
var LLMs []byte
