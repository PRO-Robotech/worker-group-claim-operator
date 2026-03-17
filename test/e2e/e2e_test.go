//go:build e2e

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

package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/pointpu/worker-group-claim-operator/test/utils"
)

// namespace where the project is deployed in
const namespace = "worker-group-claim-operator-system"

// serviceAccountName created for the project
const serviceAccountName = "worker-group-claim-operator-controller-manager"

// metricsServiceName is the name of the metrics service of the project
const metricsServiceName = "worker-group-claim-operator-controller-manager-metrics-service"

// metricsRoleBindingName is the name of the RBAC that will be created to allow get the metrics data
const metricsRoleBindingName = "worker-group-claim-operator-metrics-binding"

// testNS is the namespace where test WorkerGroupClaim resources are created
const testNS = "e2e-test"

// clusterName is the CAPI Cluster name used in tests
const clusterName = "e2e-cluster"

var _ = Describe("Manager", Ordered, func() {
	var controllerPodName string
	var projectDir string
	var testdataDir string

	BeforeAll(func() {
		var err error
		projectDir, err = utils.GetProjectDir()
		Expect(err).NotTo(HaveOccurred())
		testdataDir = filepath.Join(projectDir, "test", "e2e", "testdata")

		By("creating manager namespace (idempotent)")
		cmd := exec.Command("kubectl", "create", "ns", namespace)
		_, _ = utils.Run(cmd) // ignore error if namespace already exists

		By("labeling the namespace to enforce the restricted security policy")
		cmd = exec.Command("kubectl", "label", "--overwrite", "ns", namespace,
			"pod-security.kubernetes.io/enforce=restricted")
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to label namespace with restricted policy")

		By("installing CRDs")
		cmd = exec.Command("make", "install")
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to install CRDs")

		if !skipCAPIInstall {
			By("installing ClusterAPI controllers")
			Expect(utils.InstallCAPI()).To(Succeed(), "Failed to install CAPI")

			By("enabling MachineTaintPropagation feature gate on CAPI controller")
			Expect(utils.EnableCAPIFeatureGate("MachineTaintPropagation")).To(Succeed())
		}

		By("installing BegetMachineTemplate CRD")
		_, err = utils.KubectlApply(filepath.Join(projectDir, "testdata", "crds", "begetmachinetemplate.yaml"))
		Expect(err).NotTo(HaveOccurred(), "Failed to install BMT CRD")

		By("deploying the controller-manager")
		cmd = exec.Command("make", "deploy", "IMG="+projectImage)
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to deploy the controller-manager")

		By("creating test namespace (idempotent)")
		cmd = exec.Command("kubectl", "create", "ns", testNS)
		_, _ = utils.Run(cmd) // ignore error if namespace already exists

		By("applying test Cluster object (paused)")
		_, err = utils.KubectlApply(filepath.Join(testdataDir, "cluster.yaml"))
		Expect(err).NotTo(HaveOccurred(), "Failed to apply Cluster")

		By("applying WGMachineTemplate")
		_, err = utils.KubectlApply(filepath.Join(testdataDir, "wgmachinetemplate.yaml"))
		Expect(err).NotTo(HaveOccurred(), "Failed to apply WGMachineTemplate")

		By("applying WGBootstrapTemplate")
		_, err = utils.KubectlApply(filepath.Join(testdataDir, "wgbootstraptemplate.yaml"))
		Expect(err).NotTo(HaveOccurred(), "Failed to apply WGBootstrapTemplate")
	})

	AfterAll(func() {
		By("cleaning up the curl pod for metrics")
		cmd := exec.Command("kubectl", "delete", "pod", "curl-metrics",
			"-n", namespace, "--ignore-not-found")
		_, _ = utils.Run(cmd)

		By("cleaning up metrics ClusterRoleBinding")
		cmd = exec.Command("kubectl", "delete", "clusterrolebinding", metricsRoleBindingName, "--ignore-not-found")
		_, _ = utils.Run(cmd)

		By("removing finalizers from all WorkerGroupClaims (prevents hanging on deletion)")
		cmd = exec.Command("kubectl", "get", "workergroupclaims", "-n", testNS,
			"-o", "jsonpath={.items[*].metadata.name}")
		wgcNames, _ := utils.Run(cmd)
		for _, name := range strings.Fields(wgcNames) {
			cmd = exec.Command("kubectl", "patch", "workergroupclaim", name, "-n", testNS,
				"--type=merge", "-p", `{"metadata":{"finalizers":[]}}`)
			_, _ = utils.Run(cmd)
		}
		cmd = exec.Command("kubectl", "delete", "workergroupclaims", "--all", "-n", testNS,
			"--ignore-not-found", "--timeout=30s")
		_, _ = utils.Run(cmd)

		By("removing finalizers from MachineDeployments (paused cluster blocks CAPI cleanup)")
		cmd = exec.Command("kubectl", "get", "machinedeployments.cluster.x-k8s.io", "-n", testNS,
			"-o", "jsonpath={.items[*].metadata.name}")
		mdNames, _ := utils.Run(cmd)
		for _, name := range strings.Fields(mdNames) {
			cmd = exec.Command("kubectl", "patch", "machinedeployments.cluster.x-k8s.io", name,
				"-n", testNS, "--type=merge", "-p", `{"metadata":{"finalizers":[]}}`)
			_, _ = utils.Run(cmd)
		}

		By("removing CAPI Cluster finalizers (paused cluster won't self-cleanup)")
		cmd = exec.Command("kubectl", "patch", "cluster", clusterName, "-n", testNS,
			"--type=merge", "-p", `{"metadata":{"finalizers":[]}}`)
		_, _ = utils.Run(cmd)

		By("deleting test namespace")
		cmd = exec.Command("kubectl", "delete", "ns", testNS, "--ignore-not-found", "--timeout=60s")
		_, _ = utils.Run(cmd)

		By("deleting cluster-scoped test templates")
		cmd = exec.Command("kubectl", "delete", "wgmachinetemplate", "e2e-machine-tmpl", "--ignore-not-found")
		_, _ = utils.Run(cmd)
		cmd = exec.Command("kubectl", "delete", "wgbootstraptemplate", "e2e-bootstrap-tmpl", "--ignore-not-found")
		_, _ = utils.Run(cmd)

		By("undeploying the controller-manager")
		cmd = exec.Command("make", "undeploy")
		_, _ = utils.Run(cmd)

		By("uninstalling CRDs")
		cmd = exec.Command("make", "uninstall")
		_, _ = utils.Run(cmd)

		if !skipCAPIInstall {
			By("uninstalling ClusterAPI")
			utils.UninstallCAPI()
		}

		By("removing BegetMachineTemplate CRD")
		cmd = exec.Command("kubectl", "delete", "-f",
			filepath.Join(projectDir, "testdata", "crds", "begetmachinetemplate.yaml"),
			"--ignore-not-found")
		_, _ = utils.Run(cmd)

		By("removing manager namespace")
		cmd = exec.Command("kubectl", "delete", "ns", namespace, "--ignore-not-found")
		_, _ = utils.Run(cmd)
	})

	AfterEach(func() {
		specReport := CurrentSpecReport()
		if specReport.Failed() {
			By("Fetching controller manager pod logs")
			cmd := exec.Command("kubectl", "logs", controllerPodName, "-n", namespace)
			controllerLogs, err := utils.Run(cmd)
			if err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Controller logs:\n %s", controllerLogs)
			} else {
				_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get Controller logs: %s", err)
			}

			By("Fetching Kubernetes events")
			cmd = exec.Command("kubectl", "get", "events", "-n", namespace, "--sort-by=.lastTimestamp")
			eventsOutput, err := utils.Run(cmd)
			if err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Kubernetes events:\n%s", eventsOutput)
			} else {
				_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get Kubernetes events: %s", err)
			}

			By("Fetching test namespace events")
			cmd = exec.Command("kubectl", "get", "events", "-n", testNS, "--sort-by=.lastTimestamp")
			eventsOutput, err = utils.Run(cmd)
			if err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Test namespace events:\n%s", eventsOutput)
			} else {
				_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get test namespace events: %s", err)
			}

			By("Fetching controller manager pod description")
			cmd = exec.Command("kubectl", "describe", "pod", controllerPodName, "-n", namespace)
			podDescription, err := utils.Run(cmd)
			if err == nil {
				fmt.Println("Pod description:\n", podDescription)
			} else {
				fmt.Println("Failed to describe controller pod")
			}
		}
	})

	SetDefaultEventuallyTimeout(2 * time.Minute)
	SetDefaultEventuallyPollingInterval(time.Second)

	// ===== Scenario 1: Operator Health & Startup =====
	It("should run successfully", func() {
		By("validating that the controller-manager pod is running as expected")
		verifyControllerUp := func(g Gomega) {
			cmd := exec.Command("kubectl", "get",
				"pods", "-l", "control-plane=controller-manager",
				"-o", "go-template={{ range .items }}"+
					"{{ if not .metadata.deletionTimestamp }}"+
					"{{ .metadata.name }}"+
					"{{ \"\\n\" }}{{ end }}{{ end }}",
				"-n", namespace,
			)
			podOutput, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred(), "Failed to retrieve controller-manager pod information")
			podNames := utils.GetNonEmptyLines(podOutput)
			g.Expect(podNames).To(HaveLen(1), "expected 1 controller pod running")
			controllerPodName = podNames[0]
			g.Expect(controllerPodName).To(ContainSubstring("controller-manager"))

			cmd = exec.Command("kubectl", "get",
				"pods", controllerPodName, "-o", "jsonpath={.status.phase}",
				"-n", namespace,
			)
			output, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(output).To(Equal("Running"), "Incorrect controller-manager pod status")
		}
		Eventually(verifyControllerUp).Should(Succeed())

		By("verifying controller pod is Ready")
		Eventually(func(g Gomega) {
			cmd := exec.Command("kubectl", "get", "pod", controllerPodName, "-n", namespace,
				"-o", "jsonpath={.status.conditions[?(@.type=='Ready')].status}")
			output, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(output).To(Equal("True"), "Controller pod not ready")
		}, 3*time.Minute, time.Second).Should(Succeed())

		By("checking controller logs for successful startup")
		Eventually(func(g Gomega) {
			cmd := exec.Command("kubectl", "logs", controllerPodName, "-n", namespace)
			output, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(output).To(ContainSubstring("Starting workers"))
		}, 2*time.Minute, time.Second).Should(Succeed())

		By("verifying health endpoints via pod conditions (distroless has no curl)")
		// Liveness probe checks /healthz, readiness probe checks /readyz.
		// If both conditions are True, the health endpoints are responding 200.
		Eventually(func(g Gomega) {
			cmd := exec.Command("kubectl", "get", "pod", controllerPodName, "-n", namespace,
				"-o", "jsonpath={.status.containerStatuses[0].ready}")
			output, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(output).To(Equal("true"), "container should be ready (healthz/readyz probes passing)")
		}, time.Minute, time.Second).Should(Succeed())
	})

	// ===== Scenario 2: Metrics Endpoint =====
	It("should ensure the metrics endpoint is serving metrics", func() {
		By("ensuring ClusterRoleBinding for metrics access exists")
		cmd := exec.Command("kubectl", "delete", "clusterrolebinding", metricsRoleBindingName, "--ignore-not-found")
		_, _ = utils.Run(cmd)
		cmd = exec.Command("kubectl", "create", "clusterrolebinding", metricsRoleBindingName,
			"--clusterrole=worker-group-claim-operator-metrics-reader",
			fmt.Sprintf("--serviceaccount=%s:%s", namespace, serviceAccountName),
		)
		_, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to create ClusterRoleBinding")

		By("validating that the metrics service is available")
		cmd = exec.Command("kubectl", "get", "service", metricsServiceName, "-n", namespace)
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Metrics service should exist")

		By("getting the service account token")
		token, err := serviceAccountToken()
		Expect(err).NotTo(HaveOccurred())
		Expect(token).NotTo(BeEmpty())

		By("ensuring the metrics server is started")
		Eventually(func(g Gomega) {
			cmd := exec.Command("kubectl", "logs", controllerPodName, "-n", namespace)
			output, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(output).To(ContainSubstring("Serving metrics server"),
				"Metrics server not yet started")
		}, 3*time.Minute, time.Second).Should(Succeed())

		// +kubebuilder:scaffold:e2e-metrics-webhooks-readiness

		By("creating the curl-metrics pod to access the metrics endpoint")
		cmd = exec.Command("kubectl", "run", "curl-metrics", "--restart=Never",
			"--namespace", namespace,
			"--image=curlimages/curl:latest",
			"--overrides",
			fmt.Sprintf(`{
				"spec": {
					"containers": [{
						"name": "curl",
						"image": "curlimages/curl:latest",
						"command": ["/bin/sh", "-c"],
						"args": ["curl -v -k -H 'Authorization: Bearer %s' https://%s.%s.svc.cluster.local:8443/metrics"],
						"securityContext": {
							"readOnlyRootFilesystem": true,
							"allowPrivilegeEscalation": false,
							"capabilities": {
								"drop": ["ALL"]
							},
							"runAsNonRoot": true,
							"runAsUser": 1000,
							"seccompProfile": {
								"type": "RuntimeDefault"
							}
						}
					}],
					"serviceAccountName": "%s"
				}
			}`, token, metricsServiceName, namespace, serviceAccountName))
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to create curl-metrics pod")

		By("waiting for the curl-metrics pod to complete.")
		verifyCurlUp := func(g Gomega) {
			cmd := exec.Command("kubectl", "get", "pods", "curl-metrics",
				"-o", "jsonpath={.status.phase}",
				"-n", namespace)
			output, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(output).To(Equal("Succeeded"), "curl pod in wrong status")
		}
		Eventually(verifyCurlUp, 5*time.Minute).Should(Succeed())

		By("getting the metrics by checking curl-metrics logs")
		verifyMetricsAvailable := func(g Gomega) {
			metricsOutput, err := getMetricsOutput()
			g.Expect(err).NotTo(HaveOccurred(), "Failed to retrieve logs from curl pod")
			g.Expect(metricsOutput).NotTo(BeEmpty())
			g.Expect(metricsOutput).To(ContainSubstring("< HTTP/1.1 200 OK"))
		}
		Eventually(verifyMetricsAvailable, 2*time.Minute).Should(Succeed())
	})

	// +kubebuilder:scaffold:e2e-webhooks-checks

	// ===== Scenario 3: CRD Schema Validation =====
	It("should reject invalid claims via CRD schema validation", func() {
		By("rejecting a claim without required clusterName")
		output, err := kubectlApplyLiteral(`
apiVersion: workergroup.in-cloud.io/v1alpha1
kind: WorkerGroupClaim
metadata:
  name: invalid-no-cluster
  namespace: e2e-test
spec:
  replicas: 2
  version: "v1.30.1"
  machineTemplateRef:
    name: e2e-machine-tmpl
  bootstrapTemplateRef:
    name: e2e-bootstrap-tmpl
`)
		Expect(err).To(HaveOccurred(), "should reject missing clusterName")
		Expect(output).To(ContainSubstring("clusterName"))

		By("rejecting a claim with negative replicas")
		output, err = kubectlApplyLiteral(`
apiVersion: workergroup.in-cloud.io/v1alpha1
kind: WorkerGroupClaim
metadata:
  name: invalid-neg-replicas
  namespace: e2e-test
spec:
  clusterName: e2e-cluster
  replicas: -1
  version: "v1.30.1"
  machineTemplateRef:
    name: e2e-machine-tmpl
  bootstrapTemplateRef:
    name: e2e-bootstrap-tmpl
`)
		Expect(err).To(HaveOccurred(), "should reject negative replicas")
		Expect(output).To(ContainSubstring("replicas"))

		By("rejecting a claim with empty machineTemplateRef.name")
		output, err = kubectlApplyLiteral(`
apiVersion: workergroup.in-cloud.io/v1alpha1
kind: WorkerGroupClaim
metadata:
  name: invalid-empty-ref
  namespace: e2e-test
spec:
  clusterName: e2e-cluster
  replicas: 2
  version: "v1.30.1"
  machineTemplateRef:
    name: ""
  bootstrapTemplateRef:
    name: e2e-bootstrap-tmpl
`)
		Expect(err).To(HaveOccurred(), "should reject empty machineTemplateRef.name")
		Expect(output).To(ContainSubstring("machineTemplateRef"))

		By("rejecting a claim with invalid taint effect")
		output, err = kubectlApplyLiteral(`
apiVersion: workergroup.in-cloud.io/v1alpha1
kind: WorkerGroupClaim
metadata:
  name: invalid-taint
  namespace: e2e-test
spec:
  clusterName: e2e-cluster
  replicas: 2
  version: "v1.30.1"
  machineTemplateRef:
    name: e2e-machine-tmpl
  bootstrapTemplateRef:
    name: e2e-bootstrap-tmpl
  taints:
    - key: dedicated
      value: gpu
      effect: Invalid
      propagation: Always
`)
		Expect(err).To(HaveOccurred(), "should reject invalid taint effect")
		Expect(output).To(ContainSubstring("effect"))

		By("accepting a valid claim")
		_, err = kubectlApplyLiteral(`
apiVersion: workergroup.in-cloud.io/v1alpha1
kind: WorkerGroupClaim
metadata:
  name: valid-schema-test
  namespace: e2e-test
spec:
  clusterName: e2e-cluster
  replicas: 1
  version: "v1.30.1"
  machineTemplateRef:
    name: e2e-machine-tmpl
  bootstrapTemplateRef:
    name: e2e-bootstrap-tmpl
`)
		Expect(err).NotTo(HaveOccurred(), "valid claim should be accepted")

		By("cleaning up schema test claim")
		cmd := exec.Command("kubectl", "delete", "workergroupclaim", "valid-schema-test",
			"-n", testNS, "--timeout=60s")
		_, _ = utils.Run(cmd)
	})

	// ===== Scenario 4: CRD Immutability (clusterName CEL) =====
	It("should enforce clusterName immutability via CEL", func() {
		By("creating a claim for immutability testing")
		_, err := kubectlApplyLiteral(`
apiVersion: workergroup.in-cloud.io/v1alpha1
kind: WorkerGroupClaim
metadata:
  name: e2e-pool-immutable
  namespace: e2e-test
spec:
  clusterName: e2e-cluster
  replicas: 1
  version: "v1.30.1"
  machineTemplateRef:
    name: e2e-machine-tmpl
  bootstrapTemplateRef:
    name: e2e-bootstrap-tmpl
  infrastructure:
    cpuCount: 2
    memory: 4096
`)
		Expect(err).NotTo(HaveOccurred(), "Failed to create claim for immutability test")

		By("waiting for the claim to be created")
		Eventually(func() error {
			cmd := exec.Command("kubectl", "get", "workergroupclaim", "e2e-pool-immutable", "-n", testNS)
			_, err := utils.Run(cmd)

			return err
		}).Should(Succeed())

		By("attempting to change clusterName (should be rejected)")
		cmd := exec.Command("kubectl", "patch", "workergroupclaim", "e2e-pool-immutable",
			"-n", testNS, "--type=merge",
			"-p", `{"spec":{"clusterName":"other-cluster"}}`)
		output, err := utils.Run(cmd)
		Expect(err).To(HaveOccurred(), "clusterName change should be rejected")
		Expect(output).To(ContainSubstring("immutable"),
			"error should mention immutability")

		By("cleaning up immutability test claim")
		cmd = exec.Command("kubectl", "delete", "workergroupclaim", "e2e-pool-immutable",
			"-n", testNS, "--timeout=60s")
		_, _ = utils.Run(cmd)
	})

	// ===== Scenario 5: Full Provisioning via kubectl =====
	It("should provision all resources via kubectl", func() {
		mdName := clusterName + "-e2e-pool"

		By("applying WorkerGroupClaim")
		_, err := utils.KubectlApply(filepath.Join(testdataDir, "workergroupclaim.yaml"))
		Expect(err).NotTo(HaveOccurred(), "Failed to apply WorkerGroupClaim")

		By("waiting for MachineDeployment to be created")
		Eventually(func() error {
			cmd := exec.Command("kubectl", "get", "machinedeployments.cluster.x-k8s.io",
				mdName, "-n", testNS)
			_, err := utils.Run(cmd)

			return err
		}, 2*time.Minute, time.Second).Should(Succeed(), "MachineDeployment should be created")

		By("verifying BMT exists with correct naming")
		Eventually(func(g Gomega) {
			bmtName, err := utils.KubectlGet("workergroupclaim", "e2e-pool", testNS,
				"{.status.currentTemplates.bmt}")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(bmtName).To(MatchRegexp(`^e2e-cluster-e2e-pool-bmt-[a-f0-9]{8}$`),
				"BMT name should match {cluster}-{claim}-bmt-{hash8}")
		}).Should(Succeed())

		By("verifying KCT exists with correct naming")
		Eventually(func(g Gomega) {
			kctName, err := utils.KubectlGet("workergroupclaim", "e2e-pool", testNS,
				"{.status.currentTemplates.kct}")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(kctName).To(MatchRegexp(`^e2e-cluster-e2e-pool-kct-[a-f0-9]{8}$`),
				"KCT name should match {cluster}-{claim}-kct-{hash8}")
		}).Should(Succeed())

		By("verifying MD has correct replicas and version")
		replicas, err := utils.KubectlGet("machinedeployments.cluster.x-k8s.io",
			mdName, testNS, "{.spec.replicas}")
		Expect(err).NotTo(HaveOccurred())
		Expect(replicas).To(Equal("2"))

		version, err := utils.KubectlGet("machinedeployments.cluster.x-k8s.io",
			mdName, testNS, "{.spec.template.spec.version}")
		Expect(err).NotTo(HaveOccurred())
		Expect(version).To(Equal("v1.30.1"))

		By("patching MD status to simulate rollout completion")
		Expect(utils.PatchMDStatus(mdName, testNS, 2)).To(Succeed())

		By("waiting for Phase=Ready")
		Eventually(func() string {
			return utils.GetPhase("e2e-pool", testNS)
		}, 2*time.Minute, time.Second).Should(Equal("Ready"))

		By("verifying status fields")
		observedGen, err := utils.KubectlGet("workergroupclaim", "e2e-pool", testNS,
			"{.status.observedGeneration}")
		Expect(err).NotTo(HaveOccurred())
		Expect(observedGen).NotTo(BeEmpty())

		bmtHash, err := utils.KubectlGet("workergroupclaim", "e2e-pool", testNS,
			"{.status.lastRendered.bmtHash}")
		Expect(err).NotTo(HaveOccurred())
		Expect(bmtHash).To(HaveLen(8))

		kctHash, err := utils.KubectlGet("workergroupclaim", "e2e-pool", testNS,
			"{.status.lastRendered.kctHash}")
		Expect(err).NotTo(HaveOccurred())
		Expect(kctHash).To(HaveLen(8))

		By("verifying events on the claim")
		cmd := exec.Command("kubectl", "describe", "workergroupclaim", "e2e-pool", "-n", testNS)
		output, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred())
		Expect(output).To(ContainSubstring("TemplateRendered"))
	})

	// ===== Scenario 5b: Reconcile Stability — no infinite loops =====
	It("should not reconcile continuously after reaching Ready", func() {
		By("waiting for reconciliation to settle (15s)")
		time.Sleep(15 * time.Second)

		By("counting 'Reconcile complete' log lines for the claim")
		initialCount := countReconcileLogs(controllerPodName, "e2e-pool")
		_, _ = fmt.Fprintf(GinkgoWriter, "Initial reconcile count: %d\n", initialCount)

		By("verifying reconcile count stays stable for 30 seconds")
		Consistently(func() int {
			return countReconcileLogs(controllerPodName, "e2e-pool")
		}, 30*time.Second, 5*time.Second).Should(
			BeNumerically("<=", initialCount+1),
			"reconcile count should not grow — indicates infinite reconcile loop")
	})

	// ===== Scenario 6: Printer Columns =====
	It("should show correct kubectl printer columns", func() {
		By("getting workergroupclaim with kubectl")
		cmd := exec.Command("kubectl", "get", "workergroupclaim", "-n", testNS)
		output, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred())

		By("verifying column headers")
		lines := utils.GetNonEmptyLines(output)
		Expect(lines).NotTo(BeEmpty())
		header := lines[0]
		Expect(header).To(ContainSubstring("NAME"))
		Expect(header).To(ContainSubstring("CLUSTER"))
		Expect(header).To(ContainSubstring("PHASE"))
		Expect(header).To(ContainSubstring("REPLICAS"))
		Expect(header).To(ContainSubstring("AGE"))

		By("verifying claim row values")
		Expect(len(lines)).To(BeNumerically(">=", 2), "should have at least header + 1 row")
		row := lines[1]
		Expect(row).To(ContainSubstring("e2e-pool"))
		Expect(row).To(ContainSubstring("e2e-cluster"))
		Expect(row).To(ContainSubstring("Ready"))
	})

	// ===== Scenario 7: Variable Update -> Rollout =====
	It("should handle variable update and rollout", func() {
		mdName := clusterName + "-e2e-pool"

		By("recording current BMT name")
		oldBMT, err := utils.KubectlGet("workergroupclaim", "e2e-pool", testNS,
			"{.status.currentTemplates.bmt}")
		Expect(err).NotTo(HaveOccurred())
		Expect(oldBMT).NotTo(BeEmpty())

		By("resetting MD status so operator sees rollout as in-progress after ref change")
		Expect(utils.ResetMDStatus(mdName, testNS)).To(Succeed())

		By("patching infrastructure.cpuCount to trigger hash change")
		cmd := exec.Command("kubectl", "patch", "workergroupclaim", "e2e-pool",
			"-n", testNS, "--type=merge",
			"-p", `{"spec":{"infrastructure":{"cpuCount":8}}}`)
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred())

		By("waiting for Phase=Updating")
		Eventually(func() string {
			return utils.GetPhase("e2e-pool", testNS)
		}, 2*time.Minute, time.Second).Should(Equal("Updating"))

		By("verifying new BMT was created with different name")
		Eventually(func(g Gomega) {
			newBMT, err := utils.KubectlGet("workergroupclaim", "e2e-pool", testNS,
				"{.status.currentTemplates.bmt}")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(newBMT).NotTo(Equal(oldBMT), "BMT name should change after var update")
			g.Expect(newBMT).To(MatchRegexp(`^e2e-cluster-e2e-pool-bmt-[a-f0-9]{8}$`))
		}).Should(Succeed())

		By("verifying old BMT is in pendingDeletion")
		Eventually(func(g Gomega) {
			pending, err := utils.KubectlGet("workergroupclaim", "e2e-pool", testNS,
				"{.status.pendingDeletion}")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(pending).To(ContainSubstring(oldBMT))
		}).Should(Succeed())

		By("patching MD status to simulate rollout completion")
		Expect(utils.PatchMDStatus(mdName, testNS, 2)).To(Succeed())

		By("waiting for Phase=Ready")
		Eventually(func() string {
			return utils.GetPhase("e2e-pool", testNS)
		}, 2*time.Minute, time.Second).Should(Equal("Ready"))

		By("verifying pendingDeletion is empty and old BMT deleted")
		Eventually(func(g Gomega) {
			pending, err := utils.KubectlGet("workergroupclaim", "e2e-pool", testNS,
				"{.status.pendingDeletion}")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(pending).To(BeEmpty())
		}).Should(Succeed())
	})

	// ===== Scenario 7b: Reconcile Stability after rollout =====
	It("should not reconcile continuously after rollout completion", func() {
		By("waiting for reconciliation to settle after rollout (15s)")
		time.Sleep(15 * time.Second)

		By("counting reconcile log lines after rollout")
		postRolloutCount := countReconcileLogs(controllerPodName, "e2e-pool")
		_, _ = fmt.Fprintf(GinkgoWriter, "Post-rollout reconcile count: %d\n", postRolloutCount)

		By("verifying reconcile count stays stable for 30 seconds (post-rollout)")
		Consistently(func() int {
			return countReconcileLogs(controllerPodName, "e2e-pool")
		}, 30*time.Second, 5*time.Second).Should(
			BeNumerically("<=", postRolloutCount+1),
			"reconcile count should not grow after rollout — indicates loop in trackRollout→setReady path")
	})

	// ===== Scenario 8: In-Place Updates =====
	It("should handle in-place updates without rollout", func() {
		mdName := clusterName + "-e2e-pool"

		By("recording current BMT and KCT names")
		currentBMT, err := utils.KubectlGet("workergroupclaim", "e2e-pool", testNS,
			"{.status.currentTemplates.bmt}")
		Expect(err).NotTo(HaveOccurred())
		currentKCT, err := utils.KubectlGet("workergroupclaim", "e2e-pool", testNS,
			"{.status.currentTemplates.kct}")
		Expect(err).NotTo(HaveOccurred())

		By("patching replicas to 5 (in-place)")
		cmd := exec.Command("kubectl", "patch", "workergroupclaim", "e2e-pool",
			"-n", testNS, "--type=merge",
			"-p", `{"spec":{"replicas":5}}`)
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred())

		By("verifying MD replicas updated to 5")
		Eventually(func(g Gomega) {
			replicas, err := utils.KubectlGet("machinedeployments.cluster.x-k8s.io",
				mdName, testNS, "{.spec.replicas}")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(replicas).To(Equal("5"))
		}).Should(Succeed())

		By("verifying BMT unchanged after replicas change")
		bmtAfterReplicas, err := utils.KubectlGet("workergroupclaim", "e2e-pool", testNS,
			"{.status.currentTemplates.bmt}")
		Expect(err).NotTo(HaveOccurred())
		Expect(bmtAfterReplicas).To(Equal(currentBMT))

		By("verifying phase is still Ready (no rollout)")
		Expect(utils.GetPhase("e2e-pool", testNS)).To(Equal("Ready"))

		By("adding a taint (in-place)")
		cmd = exec.Command("kubectl", "patch", "workergroupclaim", "e2e-pool",
			"-n", testNS, "--type=merge",
			"-p", `{"spec":{"taints":[{"key":"dedicated","value":"gpu","effect":"NoSchedule","propagation":"Always"}]}}`)
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred())

		By("verifying MD has the taint")
		Eventually(func(g Gomega) {
			taintKey, err := utils.KubectlGet("machinedeployments.cluster.x-k8s.io",
				mdName, testNS, "{.spec.template.spec.taints[0].key}")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(taintKey).To(Equal("dedicated"))
		}).Should(Succeed())

		By("verifying BMT and KCT unchanged after taint")
		bmtAfterTaint, err := utils.KubectlGet("workergroupclaim", "e2e-pool", testNS,
			"{.status.currentTemplates.bmt}")
		Expect(err).NotTo(HaveOccurred())
		Expect(bmtAfterTaint).To(Equal(currentBMT))

		kctAfterTaint, err := utils.KubectlGet("workergroupclaim", "e2e-pool", testNS,
			"{.status.currentTemplates.kct}")
		Expect(err).NotTo(HaveOccurred())
		Expect(kctAfterTaint).To(Equal(currentKCT))

		Expect(utils.GetPhase("e2e-pool", testNS)).To(Equal("Ready"))
	})

	// ===== Scenario 9: Pause and Resume =====
	It("should handle pause and resume", func() {
		mdName := clusterName + "-e2e-pool"

		By("pausing the claim via annotation")
		cmd := exec.Command("kubectl", "annotate", "workergroupclaim", "e2e-pool",
			"-n", testNS, "workergroup.in-cloud.io/paused=true")
		_, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred())

		By("waiting for Phase=Paused")
		Eventually(func() string {
			return utils.GetPhase("e2e-pool", testNS)
		}, 2*time.Minute, time.Second).Should(Equal("Paused"))

		By("verifying Paused condition is True")
		Eventually(func(g Gomega) {
			cond, err := utils.KubectlGet("workergroupclaim", "e2e-pool", testNS,
				`{.status.conditions[?(@.type=="Paused")].status}`)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(cond).To(Equal("True"))
		}).Should(Succeed())

		By("changing infrastructure var while paused")
		cmd = exec.Command("kubectl", "patch", "workergroupclaim", "e2e-pool",
			"-n", testNS, "--type=merge",
			"-p", `{"spec":{"infrastructure":{"cpuCount":12}}}`)
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred())

		By("verifying phase stays Paused after var change")
		Consistently(func() string {
			return utils.GetPhase("e2e-pool", testNS)
		}, 5*time.Second, time.Second).Should(Equal("Paused"))

		By("resetting MD status so operator sees rollout as in-progress after ref change")
		Expect(utils.ResetMDStatus(clusterName+"-e2e-pool", testNS)).To(Succeed())

		By("removing pause annotation")
		cmd = exec.Command("kubectl", "annotate", "workergroupclaim", "e2e-pool",
			"-n", testNS, "workergroup.in-cloud.io/paused-")
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred())

		By("waiting for Phase=Updating (accumulated changes applied)")
		Eventually(func() string {
			return utils.GetPhase("e2e-pool", testNS)
		}, 2*time.Minute, time.Second).Should(Equal("Updating"))

		By("patching MD status to simulate rollout completion")
		Expect(utils.PatchMDStatus(mdName, testNS, 5)).To(Succeed())

		By("waiting for Phase=Ready")
		Eventually(func() string {
			return utils.GetPhase("e2e-pool", testNS)
		}, 2*time.Minute, time.Second).Should(Equal("Ready"))

		By("verifying Paused condition is False after resume")
		cond, err := utils.KubectlGet("workergroupclaim", "e2e-pool", testNS,
			`{.status.conditions[?(@.type=="Paused")].status}`)
		Expect(err).NotTo(HaveOccurred())
		Expect(cond).To(Equal("False"))
	})

	// ===== Scenario 10: Deletion with Finalizer =====
	It("should delete resources with finalizer", func() {
		mdName := clusterName + "-e2e-pool"

		By("recording BMT, KCT, MD names before deletion")
		bmtName, err := utils.KubectlGet("workergroupclaim", "e2e-pool", testNS,
			"{.status.currentTemplates.bmt}")
		Expect(err).NotTo(HaveOccurred())
		kctName, err := utils.KubectlGet("workergroupclaim", "e2e-pool", testNS,
			"{.status.currentTemplates.kct}")
		Expect(err).NotTo(HaveOccurred())

		By("initiating WorkerGroupClaim deletion (async — paused cluster blocks MD cleanup)")
		cmd := exec.Command("kubectl", "delete", "workergroupclaim", "e2e-pool",
			"-n", testNS, "--wait=false")
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to initiate WorkerGroupClaim deletion")

		By("waiting for operator to initiate MD deletion")
		Eventually(func(g Gomega) {
			ts, err := utils.KubectlGet("machinedeployments.cluster.x-k8s.io",
				mdName, testNS, "{.metadata.deletionTimestamp}")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(ts).NotTo(BeEmpty(), "MD should have deletionTimestamp")
		}, time.Minute, time.Second).Should(Succeed())

		By("removing CAPI finalizers from MD (paused cluster won't process deletion)")
		cmd = exec.Command("kubectl", "patch", "machinedeployments.cluster.x-k8s.io", mdName,
			"-n", testNS, "--type=merge", "-p", `{"metadata":{"finalizers":[]}}`)
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred())

		By("verifying MachineDeployment is fully deleted")
		Eventually(func() bool {
			cmd := exec.Command("kubectl", "get", "machinedeployments.cluster.x-k8s.io",
				mdName, "-n", testNS)
			_, err := utils.Run(cmd)

			return err != nil
		}, time.Minute, time.Second).Should(BeTrue(), "MD should be deleted")

		By("verifying claim is gone")
		Eventually(func() bool {
			cmd := exec.Command("kubectl", "get", "workergroupclaim", "e2e-pool", "-n", testNS)
			_, err := utils.Run(cmd)

			return err != nil
		}, 2*time.Minute, time.Second).Should(BeTrue(), "claim should be deleted")

		By("verifying BMT is deleted")
		Eventually(func() bool {
			cmd := exec.Command("kubectl", "get", "begetmachinetemplates.infrastructure.cluster.x-k8s.io",
				bmtName, "-n", testNS)
			_, err := utils.Run(cmd)

			return err != nil
		}, time.Minute, time.Second).Should(BeTrue(), "BMT should be deleted")

		By("verifying KCT is deleted")
		Eventually(func() bool {
			cmd := exec.Command("kubectl", "get", "kubeadmconfigtemplates.bootstrap.cluster.x-k8s.io",
				kctName, "-n", testNS)
			_, err := utils.Run(cmd)

			return err != nil
		}, time.Minute, time.Second).Should(BeTrue(), "KCT should be deleted")
	})

	// ===== Scenario 11: Operator Pod Restart Recovery =====
	It("should recover after operator pod restart", func() {
		mdName := clusterName + "-e2e-pool-restart"

		By("creating a claim for restart test")
		_, err := kubectlApplyLiteral(`
apiVersion: workergroup.in-cloud.io/v1alpha1
kind: WorkerGroupClaim
metadata:
  name: e2e-pool-restart
  namespace: e2e-test
spec:
  clusterName: e2e-cluster
  replicas: 2
  version: "v1.30.1"
  machineTemplateRef:
    name: e2e-machine-tmpl
  bootstrapTemplateRef:
    name: e2e-bootstrap-tmpl
  infrastructure:
    cpuCount: 4
    memory: 8192
`)
		Expect(err).NotTo(HaveOccurred())

		By("waiting for MachineDeployment to be created")
		Eventually(func() error {
			cmd := exec.Command("kubectl", "get", "machinedeployments.cluster.x-k8s.io",
				mdName, "-n", testNS)
			_, err := utils.Run(cmd)

			return err
		}, 2*time.Minute, time.Second).Should(Succeed())

		By("patching MD status to get to Ready")
		Expect(utils.PatchMDStatus(mdName, testNS, 2)).To(Succeed())

		By("waiting for Phase=Ready")
		Eventually(func() string {
			return utils.GetPhase("e2e-pool-restart", testNS)
		}, 2*time.Minute, time.Second).Should(Equal("Ready"))

		By("recording resource state before restart")
		bmtBefore, err := utils.KubectlGet("workergroupclaim", "e2e-pool-restart", testNS,
			"{.status.currentTemplates.bmt}")
		Expect(err).NotTo(HaveOccurred())

		By("deleting the controller pod to force restart")
		cmd := exec.Command("kubectl", "delete", "pod", "-l", "control-plane=controller-manager",
			"-n", namespace)
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred())

		By("waiting for new controller pod to be running")
		Eventually(func(g Gomega) {
			cmd := exec.Command("kubectl", "get", "pods", "-l", "control-plane=controller-manager",
				"-o", "go-template={{ range .items }}"+
					"{{ if not .metadata.deletionTimestamp }}"+
					"{{ .metadata.name }}"+
					"{{ \"\\n\" }}{{ end }}{{ end }}",
				"-n", namespace)
			output, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred())
			podNames := utils.GetNonEmptyLines(output)
			g.Expect(podNames).To(HaveLen(1))
			controllerPodName = podNames[0]

			cmd = exec.Command("kubectl", "get", "pod", controllerPodName,
				"-o", "jsonpath={.status.phase}", "-n", namespace)
			phase, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(phase).To(Equal("Running"))
		}, 3*time.Minute, time.Second).Should(Succeed())

		By("verifying claim resources are unchanged after restart")
		bmtAfter, err := utils.KubectlGet("workergroupclaim", "e2e-pool-restart", testNS,
			"{.status.currentTemplates.bmt}")
		Expect(err).NotTo(HaveOccurred())
		Expect(bmtAfter).To(Equal(bmtBefore), "BMT should be unchanged after restart")

		By("verifying reconciliation works after restart — update replicas")
		cmd = exec.Command("kubectl", "patch", "workergroupclaim", "e2e-pool-restart",
			"-n", testNS, "--type=merge",
			"-p", `{"spec":{"replicas":3}}`)
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred())

		Eventually(func(g Gomega) {
			replicas, err := utils.KubectlGet("machinedeployments.cluster.x-k8s.io",
				mdName, testNS, "{.spec.replicas}")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(replicas).To(Equal("3"))
		}, 2*time.Minute, time.Second).Should(Succeed())

		By("cleaning up restart test claim")
		cmd = exec.Command("kubectl", "delete", "workergroupclaim", "e2e-pool-restart",
			"-n", testNS, "--timeout=60s")
		_, _ = utils.Run(cmd)
	})

	// ===== Scenario 12: Shared Template Fan-out =====
	It("should handle shared template fan-out", func() {
		mdNameA := clusterName + "-e2e-pool-fan-a"
		mdNameB := clusterName + "-e2e-pool-fan-b"

		By("creating two claims referencing the same templates")
		_, err := kubectlApplyLiteral(`
apiVersion: workergroup.in-cloud.io/v1alpha1
kind: WorkerGroupClaim
metadata:
  name: e2e-pool-fan-a
  namespace: e2e-test
spec:
  clusterName: e2e-cluster
  replicas: 1
  version: "v1.30.1"
  machineTemplateRef:
    name: e2e-machine-tmpl
  bootstrapTemplateRef:
    name: e2e-bootstrap-tmpl
  infrastructure:
    cpuCount: 2
    memory: 4096
`)
		Expect(err).NotTo(HaveOccurred())

		_, err = kubectlApplyLiteral(`
apiVersion: workergroup.in-cloud.io/v1alpha1
kind: WorkerGroupClaim
metadata:
  name: e2e-pool-fan-b
  namespace: e2e-test
spec:
  clusterName: e2e-cluster
  replicas: 1
  version: "v1.30.1"
  machineTemplateRef:
    name: e2e-machine-tmpl
  bootstrapTemplateRef:
    name: e2e-bootstrap-tmpl
  infrastructure:
    cpuCount: 4
    memory: 8192
`)
		Expect(err).NotTo(HaveOccurred())

		By("waiting for both MDs to exist")
		Eventually(func() error {
			cmd := exec.Command("kubectl", "get", "machinedeployments.cluster.x-k8s.io",
				mdNameA, "-n", testNS)
			_, err := utils.Run(cmd)

			return err
		}, 2*time.Minute, time.Second).Should(Succeed())
		Eventually(func() error {
			cmd := exec.Command("kubectl", "get", "machinedeployments.cluster.x-k8s.io",
				mdNameB, "-n", testNS)
			_, err := utils.Run(cmd)

			return err
		}, 2*time.Minute, time.Second).Should(Succeed())

		By("patching both MD statuses")
		Expect(utils.PatchMDStatus(mdNameA, testNS, 1)).To(Succeed())
		Expect(utils.PatchMDStatus(mdNameB, testNS, 1)).To(Succeed())

		By("waiting for both claims to be Ready")
		Eventually(func() string {
			return utils.GetPhase("e2e-pool-fan-a", testNS)
		}, 2*time.Minute, time.Second).Should(Equal("Ready"))
		Eventually(func() string {
			return utils.GetPhase("e2e-pool-fan-b", testNS)
		}, 2*time.Minute, time.Second).Should(Equal("Ready"))

		By("recording KCT names before template update")
		kctA, err := utils.KubectlGet("workergroupclaim", "e2e-pool-fan-a", testNS,
			"{.status.currentTemplates.kct}")
		Expect(err).NotTo(HaveOccurred())
		kctB, err := utils.KubectlGet("workergroupclaim", "e2e-pool-fan-b", testNS,
			"{.status.currentTemplates.kct}")
		Expect(err).NotTo(HaveOccurred())

		By("updating WGBootstrapTemplate content")
		_, err = kubectlApplyLiteral(`
apiVersion: workergroup.in-cloud.io/v1alpha1
kind: WGBootstrapTemplate
metadata:
  name: e2e-bootstrap-tmpl
spec:
  value: |
    apiVersion: bootstrap.cluster.x-k8s.io/v1beta2
    kind: KubeadmConfigTemplate
    spec:
      template:
        spec:
          joinConfiguration:
            nodeRegistration:
              name: '{{ "{{ ds.meta_data.local_hostname }}" }}'
              criSocket: /var/run/containerd/containerd.sock
`)
		Expect(err).NotTo(HaveOccurred())

		By("waiting for both claims to get new KCTs")
		Eventually(func(g Gomega) {
			newKctA, err := utils.KubectlGet("workergroupclaim", "e2e-pool-fan-a", testNS,
				"{.status.currentTemplates.kct}")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(newKctA).NotTo(Equal(kctA), "fan-a KCT should change")
		}, 2*time.Minute, time.Second).Should(Succeed())

		Eventually(func(g Gomega) {
			newKctB, err := utils.KubectlGet("workergroupclaim", "e2e-pool-fan-b", testNS,
				"{.status.currentTemplates.kct}")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(newKctB).NotTo(Equal(kctB), "fan-b KCT should change")
		}, 2*time.Minute, time.Second).Should(Succeed())

		By("patching MD statuses for rollout completion")
		Expect(utils.PatchMDStatus(mdNameA, testNS, 1)).To(Succeed())
		Expect(utils.PatchMDStatus(mdNameB, testNS, 1)).To(Succeed())

		By("waiting for both to be Ready")
		Eventually(func() string {
			return utils.GetPhase("e2e-pool-fan-a", testNS)
		}, 2*time.Minute, time.Second).Should(Equal("Ready"))
		Eventually(func() string {
			return utils.GetPhase("e2e-pool-fan-b", testNS)
		}, 2*time.Minute, time.Second).Should(Equal("Ready"))

		By("verifying each claim has unique KCT names")
		finalKctA, _ := utils.KubectlGet("workergroupclaim", "e2e-pool-fan-a", testNS,
			"{.status.currentTemplates.kct}")
		finalKctB, _ := utils.KubectlGet("workergroupclaim", "e2e-pool-fan-b", testNS,
			"{.status.currentTemplates.kct}")
		// KCTs should be different because claims have different clusterName-claimName prefixes
		Expect(finalKctA).NotTo(Equal(finalKctB))

		By("cleaning up fan-out test claims")
		cmd := exec.Command("kubectl", "delete", "workergroupclaim",
			"e2e-pool-fan-a", "e2e-pool-fan-b", "-n", testNS, "--timeout=60s")
		_, _ = utils.Run(cmd)
	})

	// ===== Scenario 13: Reconcile Metrics =====
	It("should show reconcile metrics after successful reconciliation", func() {
		By("deleting stale curl-metrics pod (its metrics snapshot is from test start)")
		cmd := exec.Command("kubectl", "delete", "pod", "curl-metrics",
			"-n", namespace, "--ignore-not-found")
		_, _ = utils.Run(cmd)

		By("getting a fresh service account token")
		token, err := serviceAccountToken()
		Expect(err).NotTo(HaveOccurred())
		Expect(token).NotTo(BeEmpty())

		By("creating a fresh curl-metrics pod to capture current metrics")
		cmd = exec.Command("kubectl", "run", "curl-metrics", "--restart=Never",
			"--namespace", namespace,
			"--image=curlimages/curl:latest",
			"--overrides",
			fmt.Sprintf(`{
				"spec": {
					"containers": [{
						"name": "curl",
						"image": "curlimages/curl:latest",
						"command": ["/bin/sh", "-c"],
						"args": ["curl -v -k -H 'Authorization: Bearer %s' https://%s.%s.svc.cluster.local:8443/metrics"],
						"securityContext": {
							"readOnlyRootFilesystem": true,
							"allowPrivilegeEscalation": false,
							"capabilities": {
								"drop": ["ALL"]
							},
							"runAsNonRoot": true,
							"runAsUser": 1000,
							"seccompProfile": {
								"type": "RuntimeDefault"
							}
						}
					}],
					"serviceAccountName": "%s"
				}
			}`, token, metricsServiceName, namespace, serviceAccountName))
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to create fresh curl-metrics pod")

		By("waiting for the fresh curl-metrics pod to complete")
		Eventually(func(g Gomega) {
			cmd := exec.Command("kubectl", "get", "pods", "curl-metrics",
				"-o", "jsonpath={.status.phase}", "-n", namespace)
			output, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(output).To(Equal("Succeeded"))
		}, 2*time.Minute, time.Second).Should(Succeed())

		By("getting fresh metrics output")
		metricsOutput, err := getMetricsOutput()
		Expect(err).NotTo(HaveOccurred())

		By("verifying reconcile success counter exists")
		Expect(metricsOutput).To(ContainSubstring(
			`controller_runtime_reconcile_total{controller="workergroupclaim"`))

		By("verifying workqueue metrics exist")
		Expect(metricsOutput).To(ContainSubstring(
			`workqueue_adds_total{controller="workergroupclaim"`))
	})
})

// kubectlApplyLiteral applies YAML content from a string using a temp file.
func kubectlApplyLiteral(yamlContent string) (string, error) {
	tmpFile, err := os.CreateTemp("", "kubectl-e2e-*.yaml")
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(yamlContent); err != nil {
		return "", fmt.Errorf("write temp file: %w", err)
	}
	tmpFile.Close()

	cmd := exec.Command("kubectl", "apply", "-f", tmpFile.Name())

	return utils.Run(cmd)
}

// serviceAccountToken returns a token for the specified service account in the given namespace.
func serviceAccountToken() (string, error) {
	const tokenRequestRawString = `{
		"apiVersion": "authentication.k8s.io/v1",
		"kind": "TokenRequest"
	}`

	secretName := serviceAccountName + "-token-request"
	tokenRequestFile := filepath.Join("/tmp", secretName)
	err := os.WriteFile(tokenRequestFile, []byte(tokenRequestRawString), os.FileMode(0o644))
	if err != nil {
		return "", err
	}

	var out string
	verifyTokenCreation := func(g Gomega) {
		cmd := exec.Command("kubectl", "create", "--raw", fmt.Sprintf(
			"/api/v1/namespaces/%s/serviceaccounts/%s/token",
			namespace,
			serviceAccountName,
		), "-f", tokenRequestFile)

		output, err := cmd.CombinedOutput()
		g.Expect(err).NotTo(HaveOccurred())

		var token tokenRequest
		err = json.Unmarshal(output, &token)
		g.Expect(err).NotTo(HaveOccurred())

		out = token.Status.Token
	}
	Eventually(verifyTokenCreation).Should(Succeed())

	return out, err
}

// getMetricsOutput retrieves and returns the logs from the curl pod used to access the metrics endpoint.
func getMetricsOutput() (string, error) {
	By("getting the curl-metrics logs")
	cmd := exec.Command("kubectl", "logs", "curl-metrics", "-n", namespace)

	return utils.Run(cmd)
}

// countReconcileLogs counts "Reconcile complete" log lines that contain the given claim identifier.
// Uses the BMT/KCT name prefix (which embeds claim name) to filter for specific claims.
//
//nolint:unparam // claimID is parameterized for reuse across different claim names
func countReconcileLogs(podName, claimID string) int {
	cmd := exec.Command("kubectl", "logs", podName, "-n", namespace)
	output, err := utils.Run(cmd)
	if err != nil {
		return -1
	}
	count := 0
	for line := range strings.SplitSeq(output, "\n") {
		if strings.Contains(line, "Reconcile complete") && strings.Contains(line, claimID) {
			count++
		}
	}

	return count
}

// tokenRequest is a simplified representation of the Kubernetes TokenRequest API response.
type tokenRequest struct {
	Status struct {
		Token string `json:"token"`
	} `json:"status"`
}
