# Обновление и rollout

## Типы обновлений

При изменении `WorkerGroupClaim` оператор разделяет обновления на два типа:

### Обновления с rollout (пересоздание нод)

Изменение hash → новый immutable шаблон → обновление ссылки в MachineDeployment → rolling update.

| Что изменилось | Какой ресурс пересоздаётся |
|---------------|---------------------------|
| `infrastructure.*` (любая переменная) | Новый `BegetMachineTemplate` |
| `bootstrap.*` (любая переменная) | Новый `KubeadmConfigTemplate` |
| `WGMachineTemplate.spec.value` (сам шаблон) | Новый `BegetMachineTemplate` |
| `WGBootstrapTemplate.spec.value` (сам шаблон) | Новый `KubeadmConfigTemplate` |

### In-place обновления (без rollout)

Обновление полей MachineDeployment или KCT напрямую, без пересоздания нод.

| Что изменилось | Что обновляется |
|---------------|----------------|
| `replicas` | MachineDeployment |
| `version` | MachineDeployment |
| `taints` | MachineDeployment |
| `nodeLabels` | MachineDeployment |
| `strategy` | MachineDeployment |
| `deletion.*` | MachineDeployment |
| `deletePolicy` | MachineDeployment |
| `kubeletConfiguration` | KubeadmConfigTemplate (in-place) |

## Сценарий: изменение переменной infrastructure

```bash
# Увеличиваем память
kubectl patch workergroupclaim general-pool -n my-cluster-ns \
  --type merge -p '{"spec":{"infrastructure":{"memory": 8192}}}'
```

Что происходит:
1. Оператор рендерит шаблон с новыми переменными
2. Новый hash ≠ текущий → создаётся новый `BegetMachineTemplate` с именем `...-bmt-{newHash}`
3. Обновляется ссылка в `MachineDeployment.spec.template.spec.infrastructureRef`
4. Старый BMT → `status.pendingDeletion`
5. `phase: Updating`, condition `RolloutComplete=False`
6. ClusterAPI выполняет rolling update нод
7. После завершения: удаление старого BMT, `phase: Ready`

## Отслеживание rollout

Во время rollout:

```bash
kubectl get workergroupclaim general-pool -n my-cluster-ns -o yaml
```

```yaml
status:
  phase: Updating
  currentTemplates:
    begetMachineTemplate: "my-cluster-general-pool-bmt-a1b2c3d4"  # новый
  pendingDeletion:
    - kind: BegetMachineTemplate
      name: "my-cluster-general-pool-bmt-e5f6g7h8"               # старый
  rolloutStartedAt: "2025-01-15T10:30:00Z"
  conditions:
    - type: RolloutComplete
      status: "False"
      reason: RollingOut
```

## Rollout timeout

По умолчанию: **30 минут**. Настраивается через `spec.rolloutTimeout`:

```yaml
spec:
  rolloutTimeout: 1h    # Duration формат
```

При превышении таймаута:
- `phase: Degraded`
- Condition `RolloutTimedOut=True`
- Event `Warning` / `RolloutTimeout`
- Старые шаблоны **НЕ** удаляются
- Автоматического rollback **нет**

### Действия при Degraded

1. **Исследовать причину**: проверить MachineDeployment, Machine, Events
2. **Откатить**: вернуть старые значения переменных — новый rollout с предыдущей конфигурацией
3. **Увеличить таймаут**: если rollout просто медленный

```bash
# Проверить MachineDeployment
kubectl describe machinedeployment my-cluster-general-pool -n my-cluster-ns

# Проверить Machines
kubectl get machines -n my-cluster-ns -l cluster.x-k8s.io/deployment-name=my-cluster-general-pool
```

## Повторное изменение во время rollout

Если переменные изменяются снова, пока предыдущий rollout ещё не завершён:

1. Создаётся новый шаблон с актуальными переменными
2. MachineDeployment обновляется на новый шаблон (перезаписывает текущий rollout)
3. Предыдущий шаблон добавляется в `pendingDeletion`
4. `rolloutStartedAt` сбрасывается

## Сценарий: обновление шаблона

Когда платформенный инженер обновляет `WGMachineTemplate` или `WGBootstrapTemplate`:

1. Оператор через watch обнаруживает изменение
2. Находит **все** `WorkerGroupClaim`, ссылающиеся на шаблон
3. Для каждого Claim: re-render → новый hash → новый ресурс → rollout

> **Внимание:** обновление шаблона триггерит rollout всех использующих его Claim'ов. Для контролируемого обновления рассмотрите [pause/resume](pause-resume.md).

## Следующий шаг

- [Pause / Resume](pause-resume.md) — контролируемые обновления
- [Наблюдаемость](monitoring.md) — мониторинг rollout
