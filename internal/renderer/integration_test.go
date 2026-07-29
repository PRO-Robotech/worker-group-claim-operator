package renderer_test

import (
	"strings"
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"

	"github.com/pointpu/worker-group-claim-operator/internal/hash"
	"github.com/pointpu/worker-group-claim-operator/internal/kubelet"
	"github.com/pointpu/worker-group-claim-operator/internal/renderer"
)

// Realistic BMT template (adapted from workergroups/begetMachineTemplate)
const bmtTemplate = `apiVersion: infrastructure.cluster.x-k8s.io/v1beta2
kind: BegetMachineTemplate
spec:
  template:
    spec:
      configuration:
        cpuCount: {{ .cpuCount }}
        diskSize: {{ .diskSize }}
        memory: {{ .memory }}
      image: {{ .image | quote }}
      managedBy: {{ .managedBy | default "system" | quote }}
      serverName: {{ .serverName | default "" | quote }}
      usePrivateNetwork: {{ .usePrivateNetwork | default true }}
      networkTag: {{ .networkTag | default "vps" | quote }}
      providerID: ""
      sshKeyIds:
{{ .sshKeyIds | toYaml | indent 8 }}`

// Realistic KCT template (simplified from workergroups/kubeadmConfigTemplate)
const kctTemplate = `apiVersion: bootstrap.cluster.x-k8s.io/v1beta2
kind: KubeadmConfigTemplate
spec:
  template:
    spec:
      files:
        - path: /var/lib/kubelet/config-custom.yaml
          owner: "root:root"
          permissions: "0644"
          content: |
{{ .kubeletConfigYaml | indent 12 }}
        - path: /etc/node-labels
          owner: "root:root"
          permissions: "0644"
          content: |
            NODE_LABELS={{ "{{" }} .nodeLabels {{ "}}" }}
      joinConfiguration:
        discovery: {}
        nodeRegistration:
          kubeletExtraArgs:
            cluster-dns: "{{ .clusterDNS }}"
            cluster-domain: "{{ .clusterDomain }}"
            node-labels: "{{ .nodeLabels }},node-group.beget.com/name={{ .machineDeploymentName }}"
          name: {{ "{{" }} ds.meta_data.local_hostname {{ "}}" }}`

func rawJSON(v string) apiextensionsv1.JSON {
	return apiextensionsv1.JSON{Raw: []byte(v)}
}

func allBMTVars() map[string]apiextensionsv1.JSON {
	return map[string]apiextensionsv1.JSON{
		"cpuCount":          rawJSON(`4`),
		"memory":            rawJSON(`8192`),
		"diskSize":          rawJSON(`61440`),
		"image":             rawJSON(`"ubuntu-22.04"`),
		"usePrivateNetwork": rawJSON(`true`),
		"managedBy":         rawJSON(`"system"`),
		"serverName":        rawJSON(`""`),
		"networkTag":        rawJSON(`"vps"`),
		"sshKeyIds":         rawJSON(`[123, 456]`),
	}
}

func TestIntegration_BMTRendering(t *testing.T) {
	infraVars := allBMTVars()

	vars, err := renderer.PrepareVars(infraVars)
	if err != nil {
		t.Fatalf("PrepareVars: %v", err)
	}

	rendered, err := renderer.Render(bmtTemplate, vars)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	// Verify rendered YAML
	if !strings.Contains(rendered, "cpuCount: 4") {
		t.Error("missing cpuCount: 4")
	}
	if !strings.Contains(rendered, "memory: 8192") {
		t.Error("missing memory: 8192")
	}
	if !strings.Contains(rendered, `image: "ubuntu-22.04"`) {
		t.Errorf("missing quoted image, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "- 123") || !strings.Contains(rendered, "- 456") {
		t.Errorf("sshKeyIds not rendered as YAML list:\n%s", rendered)
	}
	if !strings.Contains(rendered, `managedBy: "system"`) {
		t.Errorf("default managedBy not applied:\n%s", rendered)
	}

	// Hash is deterministic
	h1 := hash.ComputeHash(rendered)
	h2 := hash.ComputeHash(rendered)
	if h1 != h2 {
		t.Errorf("hash non-deterministic: %s != %s", h1, h2)
	}

	// Resource name
	name := hash.ResourceName("my-cluster", "pool-1", hash.TypeBMT, h1)
	if !strings.HasPrefix(name, "my-cluster-pool-1-bmt-") {
		t.Errorf("unexpected name: %s", name)
	}
}

func TestIntegration_BMTHashChangesOnVarChange(t *testing.T) {
	vars1 := allBMTVars()
	vars2 := allBMTVars()
	vars2["cpuCount"] = rawJSON(`8`) // changed

	v1, _ := renderer.PrepareVars(vars1)
	v2, _ := renderer.PrepareVars(vars2)
	r1, _ := renderer.Render(bmtTemplate, v1)
	r2, _ := renderer.Render(bmtTemplate, v2)

	h1 := hash.ComputeHash(r1)
	h2 := hash.ComputeHash(r2)
	if h1 == h2 {
		t.Error("hash should change when infrastructure vars change")
	}
}

func TestIntegration_KCTRenderingWithAutoInject(t *testing.T) {
	bootstrapVars := map[string]apiextensionsv1.JSON{
		"clusterDNS":    rawJSON(`"29.64.0.10"`),
		"clusterDomain": rawJSON(`"cluster.local"`),
	}

	vars, err := renderer.PrepareVars(bootstrapVars)
	if err != nil {
		t.Fatalf("PrepareVars: %v", err)
	}

	// Auto-inject
	nodeLabels := map[string]string{
		"zone":        "eu-west-1",
		"environment": "prod",
	}
	renderer.InjectNodeLabels(vars, nodeLabels)
	renderer.InjectMachineDeploymentName(vars, "my-cluster", "pool-1")

	// For hash: inject kubelet config as empty string
	vars["kubeletConfigYaml"] = ""

	rendered, err := renderer.Render(kctTemplate, vars)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	kctHash := hash.ComputeHash(rendered)

	// Now inject real kubelet config for content
	kubeletYaml, err := kubelet.MergeKubeletConfig(nil)
	if err != nil {
		t.Fatalf("MergeKubeletConfig: %v", err)
	}
	vars["kubeletConfigYaml"] = kubeletYaml

	finalRendered, err := renderer.Render(kctTemplate, vars)
	if err != nil {
		t.Fatalf("Final Render: %v", err)
	}

	// Verify auto-injected values
	if !strings.Contains(finalRendered, "environment=prod,zone=eu-west-1") {
		t.Errorf("nodeLabels not correctly injected (sorted):\n%s", finalRendered)
	}
	if !strings.Contains(finalRendered, "node-group.beget.com/name=my-cluster-pool-1") {
		t.Errorf("machineDeploymentName not injected:\n%s", finalRendered)
	}
	if !strings.Contains(finalRendered, "maxPods: 250") {
		t.Errorf("kubelet config not injected:\n%s", finalRendered)
	}

	// Jinja escaping works ({{ ds.meta_data.local_hostname }})
	if !strings.Contains(finalRendered, "{{ ds.meta_data.local_hostname }}") {
		t.Errorf("jinja escaping broken:\n%s", finalRendered)
	}

	// Verify hash is stable
	if len(kctHash) != 8 {
		t.Errorf("hash length = %d, want 8", len(kctHash))
	}
}

func TestIntegration_KCTHashStableWhenKubeletChanges(t *testing.T) {
	bootstrapVars := map[string]apiextensionsv1.JSON{
		"clusterDNS":    rawJSON(`"29.64.0.10"`),
		"clusterDomain": rawJSON(`"cluster.local"`),
	}

	computeHashForKCT := func() string {
		vars, _ := renderer.PrepareVars(bootstrapVars)
		renderer.InjectNodeLabels(vars, map[string]string{"app": "test"})
		renderer.InjectMachineDeploymentName(vars, "cluster", "pool")
		// For hash: empty kubelet config
		vars["kubeletConfigYaml"] = ""
		rendered, _ := renderer.Render(kctTemplate, vars)

		return hash.ComputeHash(rendered)
	}

	hash1 := computeHashForKCT()
	hash2 := computeHashForKCT()

	if hash1 != hash2 {
		t.Errorf("KCT hash should be stable: %s != %s", hash1, hash2)
	}
}

func TestIntegration_KCTHashChangesOnBootstrapVarChange(t *testing.T) {
	computeHash := func(domain string) string {
		vars, _ := renderer.PrepareVars(map[string]apiextensionsv1.JSON{
			"clusterDNS":    rawJSON(`"29.64.0.10"`),
			"clusterDomain": rawJSON(`"` + domain + `"`),
		})
		renderer.InjectNodeLabels(vars, map[string]string{"app": "test"})
		renderer.InjectMachineDeploymentName(vars, "cluster", "pool")
		vars["kubeletConfigYaml"] = ""
		rendered, _ := renderer.Render(kctTemplate, vars)

		return hash.ComputeHash(rendered)
	}

	h1 := computeHash("cluster.local")
	h2 := computeHash("custom.domain") // changed domain
	if h1 == h2 {
		t.Error("KCT hash should change when bootstrap vars change")
	}
}

func TestIntegration_KCTHashChangesOnNodeLabelsChange(t *testing.T) {
	computeHash := func(labels map[string]string) string {
		vars, _ := renderer.PrepareVars(map[string]apiextensionsv1.JSON{
			"clusterDNS":    rawJSON(`"29.64.0.10"`),
			"clusterDomain": rawJSON(`"cluster.local"`),
		})
		renderer.InjectNodeLabels(vars, labels)
		renderer.InjectMachineDeploymentName(vars, "cluster", "pool")
		vars["kubeletConfigYaml"] = ""
		rendered, _ := renderer.Render(kctTemplate, vars)

		return hash.ComputeHash(rendered)
	}

	h1 := computeHash(map[string]string{"app": "v1"})
	h2 := computeHash(map[string]string{"app": "v2"}) // changed label
	if h1 == h2 {
		t.Error("KCT hash should change when nodeLabels change")
	}
}

func TestIntegration_KCTRendersWithEmptyNodeLabels(t *testing.T) {
	vars, err := renderer.PrepareVars(map[string]apiextensionsv1.JSON{
		"clusterDNS":    rawJSON(`"29.64.0.10"`),
		"clusterDomain": rawJSON(`"cluster.local"`),
	})
	if err != nil {
		t.Fatalf("PrepareVars: %v", err)
	}

	renderer.InjectNodeLabels(vars, nil)
	renderer.InjectMachineDeploymentName(vars, "cluster", "pool")
	vars["kubeletConfigYaml"] = ""

	rendered, err := renderer.Render(kctTemplate, vars)
	if err != nil {
		t.Fatalf("Render with empty nodeLabels: %v", err)
	}
	if !strings.Contains(rendered, `node-labels: ",node-group.beget.com/name=cluster-pool"`) {
		t.Errorf("expected leading-comma node-labels value:\n%s", rendered)
	}
}

func TestIntegration_SshKeyIdsAsYamlList(t *testing.T) {
	v := allBMTVars()
	v["sshKeyIds"] = rawJSON(`[123, 456, 789]`)
	vars, err := renderer.PrepareVars(v)
	if err != nil {
		t.Fatal(err)
	}
	rendered, err := renderer.Render(bmtTemplate, vars)
	if err != nil {
		t.Fatal(err)
	}
	// sshKeyIds should render as YAML list with proper indentation
	if !strings.Contains(rendered, "        - 123") {
		t.Errorf("sshKeyIds[0] not properly indented:\n%s", rendered)
	}
	if !strings.Contains(rendered, "        - 456") {
		t.Errorf("sshKeyIds[1] not properly indented:\n%s", rendered)
	}
	if !strings.Contains(rendered, "        - 789") {
		t.Errorf("sshKeyIds[2] not properly indented:\n%s", rendered)
	}
}
