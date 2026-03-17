# Быстрый старт

Руководство по установке оператора и созданию первого Worker Group.

## Предварительные требования

- Kubernetes-кластер с установленным [ClusterAPI](https://cluster-api.sigs.k8s.io/) v1beta2
- [Beget Infrastructure Provider](https://github.com/beget) установлен в management-кластере
- `kubectl` настроен на management-кластер

## 1. Установка оператора

```bash
kubectl apply -f config/crd/bases/
kubectl apply -f config/rbac/
kubectl apply -f config/manager/
```

Проверка:

```bash
kubectl get pods -n worker-group-system
```

## 2. Создание шаблонов

Шаблоны — cluster-scoped ресурсы, общие для всех namespace.

**WGMachineTemplate** — шаблон для BegetMachineTemplate:

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
```

**WGBootstrapTemplate** — шаблон для KubeadmConfigTemplate (сокращённый пример):

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
          joinConfiguration:
            nodeRegistration:
              kubeletExtraArgs:
                - name: node-labels
                  value: "{{ .nodeLabels }}"
```

```bash
kubectl apply -f wg-machine-template.yaml
kubectl apply -f wg-bootstrap-template.yaml
```

## 3. Создание WorkerGroupClaim

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
```

```bash
kubectl apply -f worker-group-claim.yaml
```

## 4. Проверка

```bash
# Статус Claim
kubectl get workergroupclaim my-workers -n my-cluster-ns -o yaml

# Созданные ресурсы
kubectl get begetmachinetemplate -n my-cluster-ns -l workergroup.in-cloud.io/claim-name=my-workers
kubectl get kubeadmconfigtemplate -n my-cluster-ns -l workergroup.in-cloud.io/claim-name=my-workers
kubectl get machinedeployment -n my-cluster-ns my-cluster-my-workers
```

Ожидаемый результат:
- `status.phase: Ready`
- `status.currentTemplates` содержит имена созданных BMT и KCT
- MachineDeployment создан с правильными ссылками

## Дальнейшее чтение

- [Концепции](concepts/worker-group-claim.md) — как устроены ресурсы
- [Обновление и rollout](user-guide/updating-and-rollouts.md) — изменение переменных
- [API Reference](reference/api.md) — полное описание полей
