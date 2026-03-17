package renderer

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func TestToYaml_Scalars(t *testing.T) {
	tests := []struct {
		name string
		val  any
		want string
	}{
		{"string", "hello", "hello"},
		{"int", 42, "42"},
		{"bool", true, "true"},
		{"json.Number int", json.Number("4"), "4"},
		{"json.Number float", json.Number("3.14"), "3.14"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := toYaml(tt.val)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("toYaml(%v) = %q, want %q", tt.val, got, tt.want)
			}
		})
	}
}

func TestToYaml_Array(t *testing.T) {
	arr := []any{json.Number("123"), json.Number("456")}
	got, err := toYaml(arr)
	if err != nil {
		t.Fatal(err)
	}
	if got != "- 123\n- 456" {
		t.Errorf("toYaml(arr) = %q, want %q", got, "- 123\n- 456")
	}
}

func TestToYaml_Map(t *testing.T) {
	m := map[string]any{
		"cpu":    json.Number("4"),
		"memory": json.Number("8192"),
	}
	got, err := toYaml(m)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "cpu: 4") {
		t.Errorf("toYaml(map) missing 'cpu: 4', got: %s", got)
	}
	if !strings.Contains(got, "memory: 8192") {
		t.Errorf("toYaml(map) missing 'memory: 8192', got: %s", got)
	}
}

func TestToYaml_NestedStructure(t *testing.T) {
	nested := map[string]any{
		"configuration": map[string]any{
			"cpuCount": json.Number("4"),
			"memory":   json.Number("8192"),
		},
	}
	got, err := toYaml(nested)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "cpuCount: 4") {
		t.Errorf("toYaml(nested) missing 'cpuCount: 4', got: %s", got)
	}
}

func TestIndent(t *testing.T) {
	tests := []struct {
		name string
		n    int
		s    string
		want string
	}{
		{"single line", 4, "hello", "    hello"},
		{"multi line", 2, "a\nb\nc", "  a\n  b\n  c"},
		{"empty lines preserved", 2, "a\n\nb", "  a\n\n  b"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := indent(tt.n, tt.s)
			if got != tt.want {
				t.Errorf("indent(%d, %q) = %q, want %q", tt.n, tt.s, got, tt.want)
			}
		})
	}
}

func TestQuote(t *testing.T) {
	if got := quote("hello"); got != `"hello"` {
		t.Errorf("quote(hello) = %s, want %q", got, `"hello"`)
	}
	if got := quote(42); got != `"42"` {
		t.Errorf("quote(42) = %s, want %q", got, `"42"`)
	}
}

func TestDefault(t *testing.T) {
	if got := defaultFunc("fallback", nil); got != "fallback" {
		t.Errorf("default(fallback, nil) = %v, want fallback", got)
	}
	if got := defaultFunc("fallback", ""); got != "fallback" {
		t.Errorf("default(fallback, '') = %v, want fallback", got)
	}
	if got := defaultFunc("fallback", "actual"); got != "actual" {
		t.Errorf("default(fallback, actual) = %v, want actual", got)
	}
	if got := defaultFunc("fallback", 0); got != 0 {
		t.Errorf("default(fallback, 0) = %v, want 0", got)
	}
}

func TestB64enc(t *testing.T) {
	got := b64enc("hello")
	want := base64.StdEncoding.EncodeToString([]byte("hello"))
	if got != want {
		t.Errorf("b64enc(hello) = %s, want %s", got, want)
	}
}

func TestRender_SimpleTemplate(t *testing.T) {
	tmpl := `name: {{ .name }}
replicas: {{ .replicas }}`
	vars := map[string]any{
		"name":     "test",
		"replicas": json.Number("3"),
	}
	got, err := Render(tmpl, vars)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "name: test") {
		t.Errorf("missing 'name: test' in: %s", got)
	}
	if !strings.Contains(got, "replicas: 3") {
		t.Errorf("missing 'replicas: 3' in: %s", got)
	}
}

func TestRender_MissingKeyError(t *testing.T) {
	tmpl := `value: {{ .undeclaredVar }}`
	vars := map[string]any{"other": "val"}
	_, err := Render(tmpl, vars)
	if err == nil {
		t.Fatal("expected error for missing key, got nil")
	}
	if !strings.Contains(err.Error(), "undeclaredVar") {
		t.Errorf("error should mention undeclaredVar: %v", err)
	}
}

func TestRender_ToYamlInTemplate(t *testing.T) {
	tmpl := `sshKeyIds:
{{ .sshKeyIds | toYaml | indent 2 }}`
	vars := map[string]any{
		"sshKeyIds": []any{json.Number("123"), json.Number("456")},
	}
	got, err := Render(tmpl, vars)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "- 123") || !strings.Contains(got, "- 456") {
		t.Errorf("sshKeyIds not rendered as YAML list: %s", got)
	}
}

func TestRender_JinjaEscaping(t *testing.T) {
	// Go template {{ "{{" }} renders as literal {{
	tmpl := `cloud-init: {{ "{{" }} jinja_var {{ "}}" }}`
	vars := map[string]any{}
	got, err := Render(tmpl, vars)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "{{ jinja_var }}") {
		t.Errorf("jinja escaping failed, got: %s", got)
	}
}

func TestRender_DefaultInTemplate(t *testing.T) {
	tmpl := `value: {{ .myVar | default "fallback" }}`
	vars := map[string]any{
		"myVar": nil,
	}
	got, err := Render(tmpl, vars)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "value: fallback") {
		t.Errorf("default not applied, got: %s", got)
	}
}

func TestRender_B64encInTemplate(t *testing.T) {
	tmpl := `encoded: {{ .secret | b64enc }}`
	vars := map[string]any{
		"secret": "my-password",
	}
	got, err := Render(tmpl, vars)
	if err != nil {
		t.Fatal(err)
	}
	expected := base64.StdEncoding.EncodeToString([]byte("my-password"))
	if !strings.Contains(got, "encoded: "+expected) {
		t.Errorf("b64enc not applied, got: %s", got)
	}
}

func TestRender_QuoteInTemplate(t *testing.T) {
	tmpl := `annotation: {{ .value | quote }}`
	vars := map[string]any{
		"value": "hello world",
	}
	got, err := Render(tmpl, vars)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, `annotation: "hello world"`) {
		t.Errorf("quote not applied, got: %s", got)
	}
}

func TestRender_ParseError(t *testing.T) {
	tmpl := `{{ .unclosed`
	_, err := Render(tmpl, nil)
	if err == nil {
		t.Fatal("expected parse error, got nil")
	}
}
