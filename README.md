# WorkerGroupClaim Operator

Kubernetes-оператор, управляющий жизненным циклом Worker Group для ClusterAPI. Заменяет Crossplane Composition, обеспечивая immutable-template versioning, rollout tracking и автоматическую очистку устаревших ресурсов.

## Сценарии использования

- Декларативное управление Worker Group через единый ресурс `WorkerGroupClaim`
- Автоматическое версионирование immutable-шаблонов (BegetMachineTemplate, KubeadmConfigTemplate) с hash-именованием
- Контролируемый rollout с отслеживанием завершения и автоматической очисткой старых ревизий

## Быстрый старт

### Установка

```bash
kubectl apply -f config/crd/bases/
kubectl apply -f config/rbac/
kubectl apply -f config/manager/
```

### Создание Worker Group

1. Применить шаблоны (cluster-scoped):

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

2. Создать WorkerGroupClaim:

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
    clusterDNS: "29.64.0.10"
    clusterDomain: "cluster.local"
```

3. Проверить:

```bash
kubectl get workergroupclaim my-workers -n my-cluster-ns
```

## Архитектура

```
WGMachineTemplate (cluster) ──┐
                               ├──► WorkerGroupClaim (namespaced) ──► BMT + KCT + MD ──► Nodes
WGBootstrapTemplate (cluster) ─┘
```

| CRD | Scope | Роль |
|-----|-------|------|
| `WGMachineTemplate` | Cluster | Go-template → BegetMachineTemplate |
| `WGBootstrapTemplate` | Cluster | Go-template → KubeadmConfigTemplate |
| `WorkerGroupClaim` | Namespaced | Переменные + параметры MachineDeployment |

## Конфигурация

### Annotations

| Annotation | Значение | Описание |
|-----------|---------|----------|
| `workergroup.in-cloud.io/paused` | `"true"` | Приостановить reconcile |

### Labels (на создаваемых ресурсах)

| Label | Описание |
|-------|----------|
| `cluster.x-k8s.io/cluster-name` | Имя кластера |
| `workergroup.in-cloud.io/claim-name` | Имя Claim |

### Именование ресурсов

```
MachineDeployment:      {clusterName}-{claimName}
BegetMachineTemplate:   {clusterName}-{claimName}-bmt-{hash8}
KubeadmConfigTemplate:  {clusterName}-{claimName}-kct-{hash8}
```

### Фазы

| Фаза | Описание |
|------|----------|
| `Provisioning` | Первоначальное создание ресурсов |
| `Ready` | Все ресурсы синхронизированы |
| `Updating` | Идёт rollout |
| `Degraded` | Rollout превысил таймаут |
| `Failed` | Ошибка рендеринга |
| `Paused` | Reconcile приостановлен |
| `Deleting` | Удаление ресурсов |

## Документация

| Раздел | Описание |
|--------|----------|
| [Быстрый старт](docs/getting-started.md) | Установка и первый Claim |
| [Концепции](docs/README.md#концепции) | WorkerGroupClaim, шаблоны, пайплайн рендеринга |
| [Руководство](docs/user-guide/README.md) | Создание шаблонов, обновление, pause, мониторинг |
| [API Reference](docs/reference/api.md) | Полное описание всех полей CRD |
| [Примеры](docs/examples/README.md) | Рабочие примеры конфигураций |
| [Troubleshooting](docs/troubleshooting.md) | Типичные проблемы и решения |

## Разработка

```bash
# Запуск тестов
make test

# Линтер
make lint

# Генерация CRD манифестов
make manifests

# Генерация кода (DeepCopy и т.д.)
make generate

# Запуск локально
make run
```

## Лицензия

Apache 2.0
