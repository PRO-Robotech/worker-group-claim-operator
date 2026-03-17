# Примеры

| Пример | Описание |
|--------|----------|
| [Базовый Worker Group](basic-worker-group/README.md) | Минимальный рабочий пример: один шаблон, один Claim |

## Паттерны

### Базовый Worker Group

Один `WGMachineTemplate` + один `WGBootstrapTemplate` + один `WorkerGroupClaim`:

```
WGMachineTemplate (cluster) ──┐
                               ├──► WorkerGroupClaim (namespaced) ──► BMT + KCT + MD
WGBootstrapTemplate (cluster) ─┘
```

### Несколько Worker Group в одном кластере

Разные Claim'ы с разными конфигурациями, одни шаблоны:

```
WGMachineTemplate (cluster) ──┬──► WorkerGroupClaim "general" ──► BMT + KCT + MD (3 replicas)
                               │
WGBootstrapTemplate (cluster) ─┼──► WorkerGroupClaim "gpu" ──► BMT + KCT + MD (2 replicas)
                               │
                               └──► WorkerGroupClaim "highmem" ──► BMT + KCT + MD (1 replica)
```

### Разные шаблоны для разных ролей

Отдельные шаблоны для Worker Group с особыми требованиями:

```
WGMachineTemplate "standard" ──► WorkerGroupClaim "general"
WGMachineTemplate "gpu" ──────► WorkerGroupClaim "gpu-pool"

WGBootstrapTemplate "default" ──► оба Claim'а
```
