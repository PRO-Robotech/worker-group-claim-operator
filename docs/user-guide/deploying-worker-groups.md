# Развёртывание Worker Group

## Создание WorkerGroupClaim

```yaml
apiVersion: workergroup.in-cloud.io/v1alpha1
kind: WorkerGroupClaim
metadata:
  name: general-pool
  namespace: my-cluster-ns
spec:
  clusterName: my-cluster
  replicas: 3
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
    workload-type: general
```

```bash
kubectl apply -f worker-group-claim.yaml
```

## Что создаёт оператор

При применении `WorkerGroupClaim` оператор создаёт три ресурса:

| Ресурс | Имя | Источник |
|--------|-----|----------|
| `BegetMachineTemplate` | `my-cluster-general-pool-bmt-{hash8}` | Рендер `WGMachineTemplate` + `infrastructure` |
| `KubeadmConfigTemplate` | `my-cluster-general-pool-kct-{hash8}` | Рендер `WGBootstrapTemplate` + `bootstrap` |
| `MachineDeployment` | `my-cluster-general-pool` | Формируется оператором напрямую |

## Проверка статуса

```bash
kubectl get workergroupclaim general-pool -n my-cluster-ns
```

```
NAME           PHASE   REPLICAS   READY   UP-TO-DATE   AGE
general-pool   Ready   3          3       3            5m
```

Подробный статус:

```bash
kubectl get workergroupclaim general-pool -n my-cluster-ns -o yaml
```

Ключевые поля в `status`:
- `phase: Ready` — всё синхронизировано
- `currentTemplates` — имена текущих BMT и KCT
- `conditions` — детальное состояние

## Несколько Worker Group в одном кластере

Можно создать несколько `WorkerGroupClaim` для одного `clusterName`:

```yaml
# general-pool: 4 CPU, 4GB RAM
apiVersion: workergroup.in-cloud.io/v1alpha1
kind: WorkerGroupClaim
metadata:
  name: general-pool
  namespace: my-cluster-ns
spec:
  clusterName: my-cluster
  replicas: 3
  infrastructure:
    cpuCount: 4
    memory: 4096
    # ...
---
# gpu-pool: 8 CPU, 16GB RAM
apiVersion: workergroup.in-cloud.io/v1alpha1
kind: WorkerGroupClaim
metadata:
  name: gpu-pool
  namespace: my-cluster-ns
spec:
  clusterName: my-cluster
  replicas: 2
  infrastructure:
    cpuCount: 8
    memory: 16384
    # ...
  taints:
    - key: gpu
      value: "true"
      effect: NoSchedule
      propagation: Always
```

Каждый Claim создаёт свой набор ресурсов с уникальными именами.

## Taints и Node Labels

**Taints** передаются в `MachineDeployment` напрямую и обновляются in-place (без rollout):

```yaml
spec:
  taints:
    - key: dedicated
      value: gpu
      effect: NoSchedule
      propagation: Always       # Always или OnInitialization
```

**Node Labels** используются двумя способами:
1. Устанавливаются как labels на MachineDeployment template
2. Автоматически конвертируются в строку `"k=v,k=v"` и инжектятся в bootstrap как `nodeLabels` для kubelet `--node-labels`

```yaml
spec:
  nodeLabels:
    environment: production     # → label на MD + строка для kubelet
    workload-type: general
```

## Стратегия обновления

```yaml
spec:
  strategy:
    type: RollingUpdate
    rollingUpdate:
      maxSurge: 1
      maxUnavailable: 0       # zero-downtime rollout
```

## Таймауты удаления Machine

По умолчанию оператор устанавливает таймауты для всех MachineDeployment:

```yaml
# Defaults (применяются автоматически)
deletion:
  nodeDrainTimeoutSeconds: 60
  nodeVolumeDetachTimeoutSeconds: 60
  nodeDeletionTimeoutSeconds: 120
```

Можно переопределить любое подмножество:

```yaml
spec:
  deletion:
    nodeDrainTimeoutSeconds: 300    # увеличить для тяжёлых workloads
```

Изменение `spec.deletion` — in-place обновление (без rollout).

## Удаление Worker Group

```bash
kubectl delete workergroupclaim general-pool -n my-cluster-ns
```

Оператор через finalizer:
1. Удаляет MachineDeployment (ClusterAPI удалит Machine → Node)
2. Удаляет текущие BMT и KCT
3. Удаляет ресурсы из `pendingDeletion`
4. Снимает finalizer

## Следующий шаг

- [Обновление и rollout](updating-and-rollouts.md)
