# Руководство пользователя

## Содержание

| Раздел | Описание |
|--------|----------|
| [Установка](installation.md) | Установка оператора в management-кластер |
| [Создание шаблонов](creating-templates.md) | Написание WGMachineTemplate и WGBootstrapTemplate |
| [Развёртывание Worker Group](deploying-worker-groups.md) | Создание WorkerGroupClaim и управление нодами |
| [Обновление и rollout](updating-and-rollouts.md) | Изменение переменных, rollout tracking, таймауты |
| [Pause / Resume](pause-resume.md) | Приостановка и возобновление reconcile |
| [Наблюдаемость](monitoring.md) | Events, conditions, статус, orphan cleanup |

## Краткий справочник

### Частые команды

```bash
# Статус Claim
kubectl get workergroupclaim -n <ns>
kubectl describe workergroupclaim <name> -n <ns>

# Созданные ресурсы
kubectl get begetmachinetemplate -n <ns> -l workergroup.in-cloud.io/claim-name=<name>
kubectl get kubeadmconfigtemplate -n <ns> -l workergroup.in-cloud.io/claim-name=<name>
kubectl get machinedeployment -n <ns> <clusterName>-<claimName>

# Шаблоны (cluster-scoped)
kubectl get wgmachinetemplates
kubectl get wgbootstraptemplates

# Pause/resume
kubectl annotate workergroupclaim <name> -n <ns> workergroup.in-cloud.io/paused=true
kubectl annotate workergroupclaim <name> -n <ns> workergroup.in-cloud.io/paused-

# Events
kubectl get events -n <ns> --field-selector involvedObject.name=<name>
```

### Conditions

| Condition | True | False |
|-----------|------|-------|
| `Ready` | Все ресурсы синхронизированы | Есть проблемы |
| `TemplatesRendered` | Шаблоны успешно отрендерены | Ошибка рендеринга или шаблон не найден |
| `RolloutComplete` | Rollout завершён | Rollout в процессе |
| `RolloutTimedOut` | Rollout превысил `rolloutTimeout` | Rollout в пределах таймаута |
| `Paused` | Reconcile приостановлен | Reconcile активен |

### Фазы

| Фаза | Описание |
|------|----------|
| `Provisioning` | Первоначальное создание ресурсов |
| `Ready` | Все ресурсы синхронизированы, rollout завершён |
| `Updating` | Идёт rollout после изменения hash |
| `Degraded` | Rollout превысил `rolloutTimeout` |
| `Deleting` | Claim удаляется, ресурсы зачищаются |
| `Failed` | Ошибка рендеринга или шаблон не найден |
| `Paused` | Reconcile приостановлен аннотацией |
