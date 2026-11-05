// Package config embeds the default gateway configuration.
package config

import _ "embed"

// LLMs is the default upstream provider configuration (llms.yaml).
//
//go:embed llms.yaml
var LLMs []byte
