package embedded

import (
	_ "embed"
)

// DefaultConfigTemplate contains the embedded config.example.yaml template from project root.
//
//go:embed config.example.yaml
var DefaultConfigTemplate []byte
