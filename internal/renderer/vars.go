package renderer

import (
	"bytes"
	"encoding/json"
	"fmt"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
)

// PrepareVars deserializes map[string]apiextensionsv1.JSON into map[string]any.
// Uses json.Decoder.UseNumber() to preserve integer types (json.Number).
func PrepareVars(raw map[string]apiextensionsv1.JSON) (map[string]any, error) {
	result := make(map[string]any, len(raw))
	for key, val := range raw {
		decoded, err := decodeJSON(val.Raw, key)
		if err != nil {
			return nil, err
		}
		result[key] = decoded
	}

	return result, nil
}

// decodeJSON decodes a raw JSON value using UseNumber() to preserve int64.
func decodeJSON(data []byte, key string) (any, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty JSON value for key %q", key)
	}

	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()

	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, fmt.Errorf("invalid JSON for key %q: %w", key, err)
	}

	return v, nil
}
