package renderer

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"strings"
	"text/template"

	"sigs.k8s.io/yaml"
)

// Render executes the Go template with the provided variables.
// Uses text/template with missingkey=error to catch undefined variables.
func Render(tmplContent string, vars map[string]any) (string, error) {
	t, err := template.New("resource").
		Option("missingkey=error").
		Funcs(funcMap()).
		Parse(tmplContent)
	if err != nil {
		return "", fmt.Errorf("template parse error: %w", err)
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, vars); err != nil {
		return "", fmt.Errorf("template execute error: %w", err)
	}

	return buf.String(), nil
}

// funcMap returns the custom template functions available in templates.
func funcMap() template.FuncMap {
	return template.FuncMap{
		"toYaml":  toYaml,
		"indent":  indent,
		"quote":   quote,
		"default": defaultFunc,
		"b64enc":  b64enc,
	}
}

// toYaml serializes a value to YAML string (without trailing newline for inline use).
func toYaml(v any) (string, error) {
	data, err := yaml.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("toYaml: %w", err)
	}

	return strings.TrimRight(string(data), "\n"), nil
}

// indent prepends each line with N spaces.
func indent(n int, s string) string {
	pad := strings.Repeat(" ", n)
	lines := strings.Split(s, "\n")
	for i := range lines {
		if lines[i] != "" {
			lines[i] = pad + lines[i]
		}
	}

	return strings.Join(lines, "\n")
}

// quote wraps a value in double quotes.
func quote(v any) string {
	return fmt.Sprintf("%q", fmt.Sprint(v))
}

// defaultFunc returns val if non-nil and non-zero, otherwise returns def.
func defaultFunc(def any, val any) any {
	if val == nil {
		return def
	}
	// Check for zero-value strings
	if s, ok := val.(string); ok && s == "" {
		return def
	}

	return val
}

// b64enc base64-encodes a value (converting to string first).
func b64enc(v any) string {
	return base64.StdEncoding.EncodeToString(fmt.Appendf(nil, "%v", v))
}
