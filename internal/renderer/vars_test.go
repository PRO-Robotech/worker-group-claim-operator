package renderer

import (
	"encoding/json"
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
)

func rawJSON(v string) apiextensionsv1.JSON {
	return apiextensionsv1.JSON{Raw: []byte(v)}
}

func TestPrepareVars_String(t *testing.T) {
	vars, err := PrepareVars(map[string]apiextensionsv1.JSON{
		"name": rawJSON(`"hello"`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if s, ok := vars["name"].(string); !ok || s != "hello" {
		t.Errorf("name = %v (%T), want string 'hello'", vars["name"], vars["name"])
	}
}

func TestPrepareVars_Number(t *testing.T) {
	vars, err := PrepareVars(map[string]apiextensionsv1.JSON{
		"cpuCount": rawJSON(`4`),
	})
	if err != nil {
		t.Fatal(err)
	}
	n, ok := vars["cpuCount"].(json.Number)
	if !ok {
		t.Fatalf("cpuCount type = %T, want json.Number", vars["cpuCount"])
	}
	if n.String() != "4" {
		t.Errorf("cpuCount = %s, want 4", n.String())
	}
}

func TestPrepareVars_Boolean(t *testing.T) {
	vars, err := PrepareVars(map[string]apiextensionsv1.JSON{
		"enabled": rawJSON(`true`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if b, ok := vars["enabled"].(bool); !ok || !b {
		t.Errorf("enabled = %v (%T), want bool true", vars["enabled"], vars["enabled"])
	}
}

func TestPrepareVars_Array(t *testing.T) {
	vars, err := PrepareVars(map[string]apiextensionsv1.JSON{
		"sshKeyIds": rawJSON(`[123, 456]`),
	})
	if err != nil {
		t.Fatal(err)
	}
	arr, ok := vars["sshKeyIds"].([]any)
	if !ok {
		t.Fatalf("sshKeyIds type = %T, want []any", vars["sshKeyIds"])
	}
	if len(arr) != 2 {
		t.Fatalf("sshKeyIds len = %d, want 2", len(arr))
	}
	if n, ok := arr[0].(json.Number); !ok || n.String() != "123" {
		t.Errorf("sshKeyIds[0] = %v (%T), want json.Number 123", arr[0], arr[0])
	}
}

func TestPrepareVars_Object(t *testing.T) {
	vars, err := PrepareVars(map[string]apiextensionsv1.JSON{
		"config": rawJSON(`{"cpu": 4, "memory": 8192}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	m, ok := vars["config"].(map[string]any)
	if !ok {
		t.Fatalf("config type = %T, want map[string]any", vars["config"])
	}
	if n, ok := m["cpu"].(json.Number); !ok || n.String() != "4" {
		t.Errorf("config.cpu = %v, want 4", m["cpu"])
	}
}

func TestPrepareVars_EmptyMap(t *testing.T) {
	vars, err := PrepareVars(map[string]apiextensionsv1.JSON{})
	if err != nil {
		t.Fatal(err)
	}
	if vars == nil {
		t.Fatal("result should not be nil for empty input")
	}
	if len(vars) != 0 {
		t.Errorf("len = %d, want 0", len(vars))
	}
}

func TestPrepareVars_InvalidJSON(t *testing.T) {
	_, err := PrepareVars(map[string]apiextensionsv1.JSON{
		"bad": rawJSON(`{invalid}`),
	})
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestPrepareVars_NumberPreservedInToYaml(t *testing.T) {
	vars, err := PrepareVars(map[string]apiextensionsv1.JSON{
		"count": rawJSON(`4`),
	})
	if err != nil {
		t.Fatal(err)
	}
	// toYaml should render json.Number("4") as "4", not "4.0"
	yaml, err := toYaml(vars["count"])
	if err != nil {
		t.Fatal(err)
	}
	if yaml != "4" {
		t.Errorf("toYaml(json.Number(4)) = %q, want %q", yaml, "4")
	}
}

func TestPrepareVars_ArrayRenderedAsYaml(t *testing.T) {
	vars, err := PrepareVars(map[string]apiextensionsv1.JSON{
		"ids": rawJSON(`[123, 456]`),
	})
	if err != nil {
		t.Fatal(err)
	}
	yaml, err := toYaml(vars["ids"])
	if err != nil {
		t.Fatal(err)
	}
	if yaml != "- 123\n- 456" {
		t.Errorf("toYaml(ids) = %q, want '- 123\\n- 456'", yaml)
	}
}
