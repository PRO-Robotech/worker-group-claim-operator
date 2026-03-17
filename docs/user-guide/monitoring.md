# Наблюдаемость

## Events

Оператор записывает события на `WorkerGroupClaim`:

| Event | Type | Reason | Когда |
|-------|------|--------|-------|
| Шаблон отрендерен | Normal | `TemplateRendered` | Успешный render |
| Создан BMT | Normal | `InfrastructureTemplateCreated` | Новый BegetMachineTemplate |
| Создан KCT | Normal | `BootstrapTemplateCreated` | Новый KubeadmConfigTemplate |
| Обновлён MD | Normal | `MachineDeploymentUpdated` | Обновлены ссылки или параметры |
| Rollout начат | Normal | `RolloutStarted` | MD начал rolling update |
| Rollout завершён | Normal | `RolloutComplete` | MD завершил rolling update |
| Удалён старый шаблон | Normal | `StaleTemplateDeleted` | Очистка после rollout |
| Ошибка рендеринга | Warning | `RenderError` | Шаблон не отрендерился |
| Шаблон не найден | Warning | `TemplateNotFound` | Ref указывает на несуществующий ресурс |
| Rollout timeout | Warning | `RolloutTimeout` | Превышен `rolloutTimeout` |
| Paused | Normal | `Paused` | Claim поставлен на паузу |
| Resumed | Normal | `Resumed` | Пауза снята |

```bash
# Все события Claim
kubectl get events -n <ns> --field-selector involvedObject.name=<claim-name> --sort-by='.lastTimestamp'

# Или через describe
kubectl describe workergroupclaim <name> -n <ns>
```

## Conditions

| Condition | Описание |
|-----------|----------|
| `Ready` | `True` когда все ресурсы синхронизированы и rollout завершён |
| `TemplatesRendered` | `True` когда оба шаблона успешно отрендерены |
| `RolloutComplete` | `True` когда `upToDateReplicas == replicas` (нет активного rollout) |
| `RolloutTimedOut` | `True` когда rollout превысил `spec.rolloutTimeout` |
| `Paused` | `True` когда reconcile приостановлен аннотацией |

Пример чтения conditions:

```bash
kubectl get workergroupclaim <name> -n <ns> -o jsonpath='{.status.conditions}' | jq .
```

## Status-поля

```yaml
status:
  phase: Ready                           # текущая фаза
  observedGeneration: 5                  # последнее обработанное поколение

  currentTemplates:
    begetMachineTemplate: "...-bmt-a1b2c3d4"
    kubeadmConfigTemplate: "...-kct-e5f6g7h8"

  lastRendered:
    infrastructureHash: "a1b2c3d4"
    bootstrapHash: "e5f6g7h8"

  pendingDeletion:                       # пусто когда всё ок
    - kind: BegetMachineTemplate
      name: "...-bmt-oldhash1"

  rolloutStartedAt: "2025-01-15T10:30:00Z"   # null когда нет rollout

  machineDeploymentStatus:               # зеркало статуса MD
    name: "my-cluster-general-pool"
    replicas: 3
    readyReplicas: 3
    upToDateReplicas: 3
```

## Мониторинг rollout

Во время rollout полезно отслеживать:

```bash
# Фаза Claim
kubectl get workergroupclaim -n <ns> -w

# Статус MachineDeployment
kubectl get machinedeployment -n <ns> -w

# Machines (отдельные ноды)
kubectl get machines -n <ns> -l cluster.x-k8s.io/deployment-name=<md-name> -w
```

### Признаки проблем

| Симптом | Возможная причина |
|---------|------------------|
| `phase: Failed`, condition `TemplatesRendered=False` | Ошибка рендеринга — проверьте переменные |
| `phase: Degraded`, condition `RolloutTimedOut=True` | Rollout завис — проверьте Machines |
| `phase: Updating` долго | Rollout в процессе — проверьте `upToDateReplicas` |
| `pendingDeletion` не пуст при `phase: Ready` | Обычно временное — MachineSet ещё ссылается на старый шаблон. Оператор дождётся удаления MS |

## Orphan Cleanup (сбор мусора)

Оператор автоматически очищает «осиротевшие» шаблоны — BMT и KCT, которые не привязаны ни к одному WorkerGroupClaim. Такие ресурсы могут остаться при crash оператора во время rollout (создал шаблон, но не успел обновить status).

### Как работает

- **Интервал**: каждые 5 минут (на leader'е)
- **Grace period**: 10 минут после создания

Алгоритм:
1. Найти все BMT и KCT с label `workergroup.in-cloud.io/claim-name`
2. Собрать reference set — все имена из `status.currentTemplates` и `status.pendingDeletion` всех Claim'ов
3. Если шаблон **НЕ** в reference set **И** старше 10 минут → удалить
4. Event `OrphanTemplateDeleted`

### Что НЕ удаляется

- Шаблоны в `status.currentTemplates` (актуальные)
- Шаблоны в `status.pendingDeletion` (ожидают завершения rollout)
- Шаблоны младше 10 минут (grace period — reconcile мог ещё не обновить status)
- Шаблоны без label `workergroup.in-cloud.io/claim-name` (не созданы оператором)

### MachineSet safety check

Дополнительно, при плановом удалении шаблонов из `pendingDeletion` (после завершения rollout), оператор проверяет, что ни один MachineSet не ссылается на шаблон. Если старый MS ещё существует — удаление откладывается до удаления MS.

```bash
# Посмотреть MachineSets, привязанные к MD
kubectl get machinesets -n <ns> \
  -l cluster.x-k8s.io/deployment-name=<clusterName>-<claimName>

# Проверить, на какой KCT ссылается MS
kubectl get machineset <ms-name> -n <ns> \
  -o jsonpath='{.spec.template.spec.bootstrap.configRef.name}'
```

### Диагностика

```bash
# Найти потенциальные orphans
kubectl get begetmachinetemplate -n <ns> \
  -l workergroup.in-cloud.io/claim-name=<name>

kubectl get kubeadmconfigtemplate -n <ns> \
  -l workergroup.in-cloud.io/claim-name=<name>

# Логи orphan cleaner
kubectl logs -n worker-group-system deployment/worker-group-controller-manager \
  | grep orphan-cleaner
```
