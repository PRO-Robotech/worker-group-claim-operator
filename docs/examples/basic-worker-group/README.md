# Базовый Worker Group

Минимальный рабочий пример: создание Worker Group из 2 нод с 4 CPU / 4GB RAM.

## Ресурсы

### 1. WGMachineTemplate

```yaml
apiVersion: workergroup.in-cloud.io/v1alpha1
kind: WGMachineTemplate
metadata:
  name: default-machine
spec:
  value: |
    apiVersion: infrastructure.cluster.x-k8s.io/v1beta2
    kind: BegetMachineTemplate
    spec:
      template:
        spec:
          configuration:
            cpuCount: {{ .cpuCount }}
            diskSize: {{ .diskSize }}
            memory: {{ .memory }}
          image: "{{ .image }}"
          managedBy: system
          networkTag: "{{ .networkTag }}"
          providerID: ""
          serverName: ""
          usePrivateNetwork: {{ .usePrivateNetwork }}
          sshKeyIds:
          {{ .sshKeyIds | toYaml | indent 10 }}
```

### 2. WGBootstrapTemplate

```yaml
apiVersion: workergroup.in-cloud.io/v1alpha1
kind: WGBootstrapTemplate
metadata:
  name: default-bootstrap
spec:
  value: |
    apiVersion: bootstrap.cluster.x-k8s.io/v1beta2
    kind: KubeadmConfigTemplate
    spec:
      template:
        spec:
          format: cloud-config
          files:
            - path: /etc/containerd/config.toml
              owner: root:root
              permissions: "0644"
              content: |
                version = 2
                imports = ["/etc/containerd/conf.d/*.toml"]

            - path: /etc/default/containerd/download.env
              owner: root:root
              permissions: "0644"
              content: |
                COMPONENT_VERSION="{{ .containerdVersion }}"
                REPOSITORY="https://github.com/containerd/containerd/releases/download"

            - path: /etc/default/kubelet/extra-args.env
              owner: root:root
              permissions: "0644"
              content: |
                KUBELET_EXTRA_ARGS="--provider-id=beget:///{{ "{{" }} ds.meta_data.instance_id.split(':')[0] {{ "}}" }}"

          joinConfiguration:
            nodeRegistration:
              imagePullPolicy: IfNotPresent
              kubeletExtraArgs:
                - name: cloud-provider
                  value: external
                - name: cluster-dns
                  value: "{{ .clusterDNS }}"
                - name: cluster-domain
                  value: "{{ .clusterDomain }}"
                - name: node-labels
                  value: "{{ .nodeLabels }}"
              taints: []
```

### 3. WorkerGroupClaim

```yaml
apiVersion: workergroup.in-cloud.io/v1alpha1
kind: WorkerGroupClaim
metadata:
  name: my-workers
  namespace: my-cluster-ns
spec:
  clusterName: my-cluster
  replicas: 2
  version: v1.30.4

  machineTemplateRef:
    name: default-machine
  bootstrapTemplateRef:
    name: default-bootstrap

  infrastructure:
    cpuCount: 4
    memory: 4096
    diskSize: 30720
    image: "k8s-customer:latest"
    usePrivateNetwork: true
    networkTag: "vps"
    sshKeyIds:
      - 12345
      - 67890

  bootstrap:
    containerdVersion: "1.7.19"
    runcVersion: "v1.1.12"
    crictlVersion: "v1.30.0"
    kubeadmVersion: "v1.30.4"
    kubectlVersion: "v1.30.4"
    kubeletVersion: "v1.30.4"
    clusterDNS: "29.64.0.10"
    clusterDomain: "cluster.local"
    registryMirrorAddress: "100.87.0.13"

  nodeLabels:
    environment: production
    workload-type: general

  strategy:
    type: RollingUpdate
    rollingUpdate:
      maxSurge: 1
      maxUnavailable: 0
```

## Развёртывание

```bash
# 1. Шаблоны (cluster-scoped)
kubectl apply -f wg-machine-template.yaml
kubectl apply -f wg-bootstrap-template.yaml

# 2. Claim
kubectl apply -f worker-group-claim.yaml
```

## Проверка

```bash
# Статус Claim
kubectl get workergroupclaim my-workers -n my-cluster-ns

# Созданные ресурсы
kubectl get begetmachinetemplate -n my-cluster-ns
kubectl get kubeadmconfigtemplate -n my-cluster-ns
kubectl get machinedeployment -n my-cluster-ns

# Events
kubectl describe workergroupclaim my-workers -n my-cluster-ns
```

Ожидаемый результат:

```
NAME         PHASE   REPLICAS   READY   UP-TO-DATE   AGE
my-workers   Ready   2          2       2            5m
```

## Обновление

Увеличение памяти (triggers rollout):

```bash
kubectl patch workergroupclaim my-workers -n my-cluster-ns \
  --type merge -p '{"spec":{"infrastructure":{"memory": 8192}}}'
```

Scale up (in-place, без rollout):

```bash
kubectl patch workergroupclaim my-workers -n my-cluster-ns \
  --type merge -p '{"spec":{"replicas": 4}}'
```

## Очистка

```bash
kubectl delete workergroupclaim my-workers -n my-cluster-ns
kubectl delete wgmachinetemplate default-machine
kubectl delete wgbootstraptemplate default-bootstrap
```
