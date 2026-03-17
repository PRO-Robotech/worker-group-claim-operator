# Troubleshooting

## Проверка статуса

```bash
# Обзор всех Claim
kubectl get workergroupclaim -A

# Подробный статус
kubectl describe workergroupclaim <name> -n <ns>

# Events
kubectl get events -n <ns> --field-selector involvedObject.name=<name> --sort-by='.lastTimestamp'

# Логи оператора
kubectl logs -n worker-group-system deployment/worker-group-controller-manager -f
```

## Типичные проблемы

### 1. Phase: Failed, TemplatesRendered=False, reason: TemplateNotFound

**Симптом:**

```yaml
status:
  phase: Failed
  conditions:
    - type: TemplatesRendered
      status: "False"
      reason: TemplateNotFound
      message: "WGMachineTemplate 'default-machine' not found"
```

**Причина:** `spec.machineTemplateRef.name` или `spec.bootstrapTemplateRef.name` указывает на несуществующий шаблон.

**Решение:**

```bash
# Проверить существующие шаблоны
kubectl get wgmachinetemplates
kubectl get wgbootstraptemplates

# Исправить ссылку
kubectl patch workergroupclaim <name> -n <ns> \
  --type merge -p '{"spec":{"machineTemplateRef":{"name":"correct-template-name"}}}'
```

### 2. Phase: Failed, TemplatesRendered=False, reason: RenderError

**Симптом:**

```yaml
status:
  phase: Failed
  lastRendered:
    errors:
      - "template: missing variable 'newVar' in infrastructure"
```

**Причина:** шаблон содержит `{{ .newVar }}`, но в `spec.infrastructure` или `spec.bootstrap` нет ключа `newVar`.

**Решение:** добавить недостающую переменную в Claim:

```bash
kubectl patch workergroupclaim <name> -n <ns> \
  --type merge -p '{"spec":{"infrastructure":{"newVar": "value"}}}'
```

### 3. Phase: Degraded, RolloutTimedOut=True

**Симптом:**

```yaml
status:
  phase: Degraded
  conditions:
    - type: RolloutTimedOut
      status: "True"
```

**Причина:** rolling update MachineDeployment не завершился за `spec.rolloutTimeout` (default 30m).

**Решение:**

```bash
# 1. Проверить статус MachineDeployment
kubectl describe machinedeployment <clusterName>-<claimName> -n <ns>

# 2. Проверить отдельные Machines
kubectl get machines -n <ns> \
  -l cluster.x-k8s.io/deployment-name=<clusterName>-<claimName>

# 3. Проверить причину зависания (обычно Machine не переходит в Ready)
kubectl describe machine <machine-name> -n <ns>

# 4. Варианты:
# - Исправить причину и дождаться завершения
# - Откатить: вернуть старые значения переменных
# - Увеличить таймаут
kubectl patch workergroupclaim <name> -n <ns> \
  --type merge -p '{"spec":{"rolloutTimeout":"1h"}}'
```

### 4. Phase: Ready, но pendingDeletion не пуст

**Симптом:** Claim в фазе `Updating` или `Ready`, но `status.pendingDeletion` содержит старые шаблоны.

**Причина:** оператор проверяет перед удалением, что ни один MachineSet не ссылается на старый шаблон. Если старый MS ещё существует — шаблон остаётся в `pendingDeletion` до удаления MS.

**Решение:** обычно это нормальное поведение — подождать. Если MS завис:

```bash
# Проверить MachineSets
kubectl get machinesets -n <ns> \
  -l cluster.x-k8s.io/deployment-name=<clusterName>-<claimName>

# Проверить, какой KCT/BMT ссылается MS
kubectl get machineset <ms-name> -n <ns> \
  -o jsonpath='{.spec.template.spec.bootstrap.configRef.name}'
```

### 5. Phase: Updating, rollout не завершается

**Симптом:** Claim в фазе `Updating` дольше ожидаемого, но таймаут ещё не достигнут.

**Решение:**

```bash
# Проверить прогресс rollout
kubectl get machinedeployment <clusterName>-<claimName> -n <ns> \
  -o jsonpath='{.status.replicas}/{.status.upToDateReplicas}/{.status.readyReplicas}'

# Проверить, есть ли проблемные Machines
kubectl get machines -n <ns> \
  -l cluster.x-k8s.io/deployment-name=<clusterName>-<claimName> \
  --sort-by='.metadata.creationTimestamp'
```

### 6. Orphaned ресурсы (BMT/KCT без Claim)

**Симптом:** BegetMachineTemplate или KubeadmConfigTemplate с label `workergroup.in-cloud.io/claim-name`, но без соответствующего Claim.

**Причина:** обычно возникает при crash оператора во время rollout. Оператор автоматически чистит orphans каждые 5 минут (ресурсы старше 10 минут).

**Решение:** дождаться автоматической очистки или удалить вручную:

```bash
# Найти orphans
kubectl get begetmachinetemplate -n <ns> \
  -l workergroup.in-cloud.io/claim-name=<claim-name>

# Удалить если нужно
kubectl delete begetmachinetemplate <name> -n <ns>
```

### 7. Claim не реагирует на изменения

**Симптом:** изменили spec, но ничего не происходит.

**Проверить:**

```bash
# Не на паузе ли?
kubectl get workergroupclaim <name> -n <ns> \
  -o jsonpath='{.metadata.annotations.workergroup\.in-cloud\.io/paused}'

# Если "true" — снять паузу
kubectl annotate workergroupclaim <name> -n <ns> \
  workergroup.in-cloud.io/paused-
```

### 8. Несколько Claim'ов ссылаются на один шаблон — неожиданный rollout

**Симптом:** обновили шаблон для одного Claim, но rollout пошёл на всех.

**Причина:** шаблоны cluster-scoped и разделяемые. Изменение шаблона триггерит пересчёт **всех** ссылающихся Claim'ов.

**Решение:** для разных конфигураций используйте разные шаблоны, или используйте [pause](user-guide/pause-resume.md) для поэтапного rollout.
