/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package utils

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"

	. "github.com/onsi/ginkgo/v2" //nolint:staticcheck
)

const (
	certmanagerVersion = "v1.19.1"
	certmanagerURLTmpl = "https://github.com/cert-manager/cert-manager/releases/download/%s/cert-manager.yaml"

	defaultKindBinary  = "kind"
	defaultKindCluster = "kind"
)

func warnError(err error) {
	_, _ = fmt.Fprintf(GinkgoWriter, "warning: %v\n", err)
}

// Run executes the provided command within this context
func Run(cmd *exec.Cmd) (string, error) {
	dir, _ := GetProjectDir()
	cmd.Dir = dir

	if err := os.Chdir(cmd.Dir); err != nil {
		_, _ = fmt.Fprintf(GinkgoWriter, "chdir dir: %q\n", err)
	}

	cmd.Env = append(os.Environ(), "GO111MODULE=on")
	command := strings.Join(cmd.Args, " ")
	_, _ = fmt.Fprintf(GinkgoWriter, "running: %q\n", command)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("%q failed with error %q: %w", command, string(output), err)
	}

	return string(output), nil
}

// UninstallCertManager uninstalls the cert manager
func UninstallCertManager() {
	url := fmt.Sprintf(certmanagerURLTmpl, certmanagerVersion)
	cmd := exec.Command("kubectl", "delete", "-f", url)
	if _, err := Run(cmd); err != nil {
		warnError(err)
	}

	// Delete leftover leases in kube-system (not cleaned by default)
	kubeSystemLeases := []string{
		"cert-manager-cainjector-leader-election",
		"cert-manager-controller",
	}
	for _, lease := range kubeSystemLeases {
		cmd = exec.Command("kubectl", "delete", "lease", lease,
			"-n", "kube-system", "--ignore-not-found", "--force", "--grace-period=0")
		if _, err := Run(cmd); err != nil {
			warnError(err)
		}
	}
}

// InstallCertManager installs the cert manager bundle.
func InstallCertManager() error {
	url := fmt.Sprintf(certmanagerURLTmpl, certmanagerVersion)
	cmd := exec.Command("kubectl", "apply", "-f", url)
	if _, err := Run(cmd); err != nil {
		return err
	}
	// Wait for cert-manager-webhook to be ready, which can take time if cert-manager
	// was re-installed after uninstalling on a cluster.
	cmd = exec.Command("kubectl", "wait", "deployment.apps/cert-manager-webhook",
		"--for", "condition=Available",
		"--namespace", "cert-manager",
		"--timeout", "5m",
	)

	_, err := Run(cmd)

	return err
}

// IsCertManagerCRDsInstalled checks if any Cert Manager CRDs are installed
// by verifying the existence of key CRDs related to Cert Manager.
func IsCertManagerCRDsInstalled() bool {
	// List of common Cert Manager CRDs
	certManagerCRDs := []string{
		"certificates.cert-manager.io",
		"issuers.cert-manager.io",
		"clusterissuers.cert-manager.io",
		"certificaterequests.cert-manager.io",
		"orders.acme.cert-manager.io",
		"challenges.acme.cert-manager.io",
	}

	// Execute the kubectl command to get all CRDs
	cmd := exec.Command("kubectl", "get", "crds")
	output, err := Run(cmd)
	if err != nil {
		return false
	}

	// Check if any of the Cert Manager CRDs are present
	crdList := GetNonEmptyLines(output)
	for _, crd := range certManagerCRDs {
		for _, line := range crdList {
			if strings.Contains(line, crd) {
				return true
			}
		}
	}

	return false
}

// LoadImageToKindClusterWithName loads a local docker image to the kind cluster
func LoadImageToKindClusterWithName(name string) error {
	cluster := defaultKindCluster
	if v, ok := os.LookupEnv("KIND_CLUSTER"); ok {
		cluster = v
	}
	kindOptions := []string{"load", "docker-image", name, "--name", cluster}
	kindBinary := defaultKindBinary
	if v, ok := os.LookupEnv("KIND"); ok {
		kindBinary = v
	}
	cmd := exec.Command(kindBinary, kindOptions...)
	_, err := Run(cmd)

	return err
}

// GetNonEmptyLines converts given command output string into individual objects
// according to line breakers, and ignores the empty elements in it.
func GetNonEmptyLines(output string) []string {
	var res []string
	for element := range strings.SplitSeq(output, "\n") {
		if element != "" {
			res = append(res, element)
		}
	}

	return res
}

// GetProjectDir will return the directory where the project is
func GetProjectDir() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return wd, fmt.Errorf("failed to get current working directory: %w", err)
	}
	wd = strings.ReplaceAll(wd, "/test/e2e", "")

	return wd, nil
}

// InstallCAPI installs ClusterAPI core, kubeadm bootstrap, and kubeadm control-plane providers.
func InstallCAPI() error {
	if _, err := exec.LookPath("clusterctl"); err != nil {
		return fmt.Errorf("clusterctl not found in PATH: %w", err)
	}
	cmd := exec.Command("clusterctl", "init", "--wait-providers")
	_, err := Run(cmd)

	return err
}

// EnableCAPIFeatureGate patches the CAPI controller-manager deployment to enable a feature gate
// and waits for the rollout to complete.
func EnableCAPIFeatureGate(gate string) error {
	patch := fmt.Sprintf(
		`[{"op":"add","path":"/spec/template/spec/containers/0/args/-","value":"--feature-gates=%s=true"}]`,
		gate,
	)
	cmd := exec.Command("kubectl", "patch", "deployment", "capi-controller-manager",
		"-n", "capi-system", "--type=json", "-p", patch)
	if _, err := Run(cmd); err != nil {
		return err
	}
	cmd = exec.Command("kubectl", "rollout", "status", "deployment/capi-controller-manager",
		"-n", "capi-system", "--timeout=2m")
	_, err := Run(cmd)

	return err
}

// UninstallCAPI uninstalls all ClusterAPI providers and CRDs.
func UninstallCAPI() {
	cmd := exec.Command("clusterctl", "delete", "--all", "--include-crd")
	if _, err := Run(cmd); err != nil {
		warnError(err)
	}
}

// KubectlApply applies a YAML file using kubectl.
func KubectlApply(file string) (string, error) {
	cmd := exec.Command("kubectl", "apply", "-f", file)

	return Run(cmd)
}

// KubectlGet retrieves a resource field using jsonpath.
func KubectlGet(resource, name, ns, jsonpath string) (string, error) {
	args := []string{"get", resource, name, "-o", "jsonpath=" + jsonpath}
	if ns != "" {
		args = append(args, "-n", ns)
	}
	cmd := exec.Command("kubectl", args...)

	return Run(cmd)
}

// GetPhase returns the current phase of a WorkerGroupClaim.
func GetPhase(name, ns string) string {
	output, err := KubectlGet("workergroupclaim", name, ns, "{.status.phase}")
	if err != nil {
		return ""
	}

	return output
}

// ResetMDStatus resets a MachineDeployment's status to simulate a rollout in-progress.
// This ensures the operator sees the rollout as incomplete after a template ref change.
func ResetMDStatus(name, ns string) error {
	patch := `{"status":{"replicas":0,"readyReplicas":0,"upToDateReplicas":0,"conditions":[]}}`
	cmd := exec.Command("kubectl", "patch", "machinedeployments.cluster.x-k8s.io", name,
		"-n", ns, "--subresource=status", "--type=merge", "-p", patch)
	_, err := Run(cmd)

	return err
}

// PatchMDStatus patches a MachineDeployment's status to simulate rollout completion.
// Uses v1beta2 metav1.Condition format (requires message field).
func PatchMDStatus(name, ns string, replicas int32) error {
	patch := fmt.Sprintf(
		`{"status":{"replicas":%d,"readyReplicas":%d,"upToDateReplicas":%d,`+
			`"conditions":[{"type":"RollingOut","status":"False","reason":"Complete",`+
			`"message":"Rollout complete","lastTransitionTime":"2026-01-01T00:00:00Z"}]}}`,
		replicas, replicas, replicas,
	)
	cmd := exec.Command("kubectl", "patch", "machinedeployments.cluster.x-k8s.io", name,
		"-n", ns, "--subresource=status", "--type=merge", "-p", patch)
	_, err := Run(cmd)

	return err
}

// UncommentCode searches for target in the file and remove the comment prefix
// of the target content. The target content may span multiple lines.
func UncommentCode(filename, target, prefix string) error {
	content, err := os.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("failed to read file %q: %w", filename, err)
	}
	strContent := string(content)

	idx := strings.Index(strContent, target)
	if idx < 0 {
		return fmt.Errorf("unable to find the code %q to be uncomment", target)
	}

	out := new(bytes.Buffer)
	_, err = out.Write(content[:idx])
	if err != nil {
		return fmt.Errorf("failed to write to output: %w", err)
	}

	scanner := bufio.NewScanner(bytes.NewBufferString(target))
	if !scanner.Scan() {
		return nil
	}
	for {
		if _, err = out.WriteString(strings.TrimPrefix(scanner.Text(), prefix)); err != nil {
			return fmt.Errorf("failed to write to output: %w", err)
		}
		// Avoid writing a newline in case the previous line was the last in target.
		if !scanner.Scan() {
			break
		}
		if _, err = out.WriteString("\n"); err != nil {
			return fmt.Errorf("failed to write to output: %w", err)
		}
	}

	if _, err = out.Write(content[idx+len(target):]); err != nil {
		return fmt.Errorf("failed to write to output: %w", err)
	}

	if err = os.WriteFile(filename, out.Bytes(), 0644); err != nil {
		return fmt.Errorf("failed to write file %q: %w", filename, err)
	}

	return nil
}
