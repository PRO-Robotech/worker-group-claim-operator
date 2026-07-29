package validation

import (
	"reflect"
	"strings"
	"testing"
)

func TestSanitizeNodeLabels(t *testing.T) {
	tests := map[string]struct {
		labels       map[string]string
		wantAccepted map[string]string
		wantRejected []string
	}{
		"all valid": {
			labels: map[string]string{
				"app":             "nginx",
				"environment":     "production",
				"custom.io/label": "value",
			},
			wantAccepted: map[string]string{
				"app":             "nginx",
				"environment":     "production",
				"custom.io/label": "value",
			},
		},
		"nil": {},
		"empty": {
			labels: map[string]string{},
		},
		"empty value is valid": {
			labels:       map[string]string{"app": ""},
			wantAccepted: map[string]string{"app": ""},
		},
		"single reserved dropped": {
			labels:       map[string]string{"cluster.x-k8s.io/cluster-name": "my-cluster"},
			wantAccepted: map[string]string{},
			wantRejected: []string{"cluster.x-k8s.io/cluster-name"},
		},
		"reserved dropped, rest kept, sorted": {
			labels: map[string]string{
				"workergroup.in-cloud.io/claim-name":       "val",
				"cluster.x-k8s.io/cluster-name":            "val",
				"node-group.beget.com/name":                "val",
				"node.cluster.x-k8s.io/managed":            "val",
				"node-restriction.kubernetes.io/protected": "val",
				"valid-label": "val",
			},
			wantAccepted: map[string]string{"valid-label": "val"},
			wantRejected: []string{
				"cluster.x-k8s.io/cluster-name",
				"node-group.beget.com/name",
				"node-restriction.kubernetes.io/protected",
				"node.cluster.x-k8s.io/managed",
				"workergroup.in-cloud.io/claim-name",
			},
		},
		"node-role is not reserved": {
			labels:       map[string]string{"node-role.kubernetes.io/worker": "true"},
			wantAccepted: map[string]string{"node-role.kubernetes.io/worker": "true"},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			accepted, rejected, err := SanitizeNodeLabels(tt.labels)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantAccepted == nil && accepted != nil && len(accepted) != 0 {
				t.Errorf("accepted = %v, want empty", accepted)
			}
			if tt.wantAccepted != nil && !reflect.DeepEqual(accepted, tt.wantAccepted) {
				t.Errorf("accepted = %v, want %v", accepted, tt.wantAccepted)
			}
			if !reflect.DeepEqual(rejected, tt.wantRejected) {
				t.Errorf("rejected = %v, want %v", rejected, tt.wantRejected)
			}
		})
	}
}

func TestSanitizeNodeLabels_InvalidSyntax(t *testing.T) {
	tests := map[string]struct {
		labels   map[string]string
		wantHint string
	}{
		"bad key charset":   {labels: map[string]string{"bad key": "val"}, wantHint: "bad key"},
		"two slashes":       {labels: map[string]string{"a/b/c": "val"}, wantHint: "a/b/c"},
		"key too long":      {labels: map[string]string{strings.Repeat("a", 64): "val"}, wantHint: "must be no more than 63"},
		"bad value charset": {labels: map[string]string{"app": "bad value"}, wantHint: "app=bad value"},
		"value too long":    {labels: map[string]string{"app": strings.Repeat("v", 64)}, wantHint: "must be no more than 63"},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			accepted, rejected, err := SanitizeNodeLabels(tt.labels)
			if err == nil {
				t.Fatal("expected error for invalid label")
			}
			if !strings.Contains(err.Error(), tt.wantHint) {
				t.Errorf("error = %v, want it to mention %q", err, tt.wantHint)
			}
			if accepted != nil || rejected != nil {
				t.Errorf("accepted/rejected must be nil on error, got %v / %v", accepted, rejected)
			}
		})
	}
}

func TestSanitizeNodeLabels_InvalidWinsOverReserved(t *testing.T) {
	_, _, err := SanitizeNodeLabels(map[string]string{
		"cluster.x-k8s.io/name": "val",
		"bad key":               "val",
	})
	if err == nil {
		t.Fatal("expected error when an invalid key is present")
	}
}
