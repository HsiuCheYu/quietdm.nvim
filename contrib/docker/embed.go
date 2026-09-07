// Package dockerstack embeds the self-hosting compose stack so `quietdmd
// setup` can materialize it even when quietdmd was installed as a release
// binary or via `go install` and there is no git checkout to read
// contrib/docker from.
package dockerstack

import _ "embed"

// ComposeYAML is the verbatim contents of compose.yaml.
//
//go:embed compose.yaml
var ComposeYAML []byte

// EnvExample is the verbatim contents of .env.example.
//
//go:embed .env.example
var EnvExample []byte
