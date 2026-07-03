package generate

import "github.com/goccy/go-yaml"

// yamlUnmarshal keeps the test file free of a direct import cycle of
// concerns: tests parse generated output with the same library production
// marshals with, so "it parses" means the same dialect on both sides.
func yamlUnmarshal(b []byte, v any) error {
	return yaml.Unmarshal(b, v)
}
