package hash

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// Resource type constants for naming.
const (
	TypeBMT = "bmt"
	TypeKCT = "kct"
)

// ComputeHash returns the first 8 hex characters of SHA256(renderedYAML).
func ComputeHash(renderedYAML string) string {
	h := sha256.Sum256([]byte(renderedYAML))

	return hex.EncodeToString(h[:])[:8]
}

// ResourceName generates a Kubernetes resource name in the format:
// {clusterName}-{claimName}-{resourceType}-{hash8}
func ResourceName(clusterName, claimName, resourceType, hash string) string {
	name := fmt.Sprintf("%s-%s-%s-%s", clusterName, claimName, resourceType, hash)
	// Kubernetes name max length is 253
	if len(name) > 253 {
		name = name[:253]
	}

	return name
}
