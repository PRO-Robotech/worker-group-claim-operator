# WorkerGroupClaim

## Обзор

- Основной namespaced-ресурс, через который пользователь описывает Worker Group
- Содержит набор переменных для рендеринга шаблонов и параметры MachineDeployment
- Ссылается на cluster-scoped шаблоны `WGMachineTemplate` и `WGBootstrapTemplate`
- Оператор создаёт и управляет `BegetMachineTemplate`, `KubeadmConfigTemplate` и `MachineDeployment` на основе Claim

## Поля Spec

| Поле | Тип | Обязательно | Описание |
|------|-----|:-----------:|----------|
| `clusterName` | `string` | Да | Имя ClusterAPI-кластера, к которому относится Worker Group |
| `replicas` | `*int32` | Нет | Количество нод (default: 1) |
| `version` | `string` | Да | Версия Kubernetes (напр. `v1.30.4`) |
| `machineTemplateRef.name` | `string` | Да | Имя `WGMachineTemplate` (cluster-scoped) |
| `bootstrapTemplateRef.name` | `string` | Да | Имя `WGBootstrapTemplate` (cluster-scoped) |
| `infrastructure` | `map[string]apiextensionsv1.JSON` | Да | Переменные для рендеринга `WGMachineTemplate` |
| `bootstrap` | `map[string]apiextensionsv1.JSON` | Да | Переменные для рендеринга `WGBootstrapTemplate` |
| `taints` | `[]MachineTaint` | Нет | Taints на нодах (передаются в MachineDeployment напрямую) |
| `nodeLabels` | `map[string]string` | Нет | Labels на нодах |
| `strategy` | `MachineDeploymentStrategy` | Нет | Стратегия rolling update |
| `deletePolicy` | `string` | Нет | Порядок удаления Machine при scale-down |
| `deletion` | `MachineDeletionConfig` | Нет | Таймауты удаления Machine (defaults: drain=60s, volumeDetach=60s, nodeDeletion=120s) |
| `rolloutTimeout` | `*metav1.Duration` | Нет | Таймаут rollout (default: 30m) |
| `kubeletConfiguration` | `map[string]interface{}` | Нет | Переопределение kubelet-настроек |

## Переменные (infrastructure / bootstrap)

Каждый ключ в `infrastructure` и `bootstrap` маппится 1:1 на `{{ .ключ }}` в соответствующем шаблоне.

```yaml
infrastructure:
  cpuCount: 4          # → {{ .cpuCount }} в WGMachineTemplate
  memory: 4096         # → {{ .memory }}
  diskSize: 30720      # → {{ .diskSize }}
```

Значения могут быть любого типа: string, number, boolean, array, object.

```yaml
infrastructure:
  sshKeyIds:           # массив — используется через {{ .sshKeyIds | toYaml }}
    - 12345
    - 67890
```

## Status

| Поле | Описание |
|------|----------|
| `phase` | Текущая фаза: `Provisioning`, `Ready`, `Updating`, `Degraded`, `Deleting`, `Failed`, `Paused` |
| `currentTemplates` | Имена текущих BMT и KCT |
| `lastRendered` | Результат последнего рендеринга: hash-суммы и ошибки |
| `pendingDeletion` | Старые шаблоны, ожидающие удаления после rollout |
| `machineDeploymentStatus` | Статус MachineDeployment (replicas, conditions) |
| `rolloutStartedAt` | Время начала текущего rollout |
| `observedGeneration` | Последнее обработанное поколение ресурса |
| `conditions` | Стандартные conditions: `Ready`, `TemplatesRendered`, `RolloutComplete`, `RolloutTimedOut` |

## Жизненный цикл фаз

```
                    ┌──────────┐
        создание ──►│Provisioning│──► Ready
                    └──────────┘       │
                                       │ hash изменился
                                       ▼
                    ┌──────────┐    ┌────────┐
                    │ Degraded │◄───│Updating│
                    │(таймаут) │    └───┬────┘
                    └────┬─────┘       │ rollout завершён
                         │             ▼
                         └──────────► Ready
```

## Лучшие практики

1. **Один Claim = одна Worker Group.** Не пытайтесь объединять разные роли нод в один Claim — используйте отдельные Claim для разных конфигураций
2. **Называйте Claim осмысленно.** Имя Claim входит в имена всех создаваемых ресурсов (`{clusterName}-{claimName}-bmt-...`)
3. **Используйте `rolloutTimeout`.** По умолчанию 30 минут; увеличьте для больших Worker Group, где rolling update занимает больше времени
4. **Batch-обновления через pause.** Если нужно изменить несколько переменных одновременно — поставьте Claim на паузу, внесите изменения, снимите паузу. Это запустит один rollout вместо нескольких

## Связанные ресурсы

- [WGMachineTemplate](wg-machine-template.md) — шаблон для инфраструктуры
- [WGBootstrapTemplate](wg-bootstrap-template.md) — шаблон для bootstrap
- [Пайплайн рендеринга](rendering-pipeline.md) — как переменные превращаются в ресурсы
