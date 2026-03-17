# Документация WorkerGroupClaim Operator

## Содержание

### Начало работы

| Документ | Описание |
|----------|----------|
| [Быстрый старт](getting-started.md) | Установка оператора и создание первого WorkerGroupClaim |

### Концепции

| Документ | Описание |
|----------|----------|
| [WorkerGroupClaim](concepts/worker-group-claim.md) | Основной ресурс — набор переменных + параметры MachineDeployment |
| [WGMachineTemplate](concepts/wg-machine-template.md) | Cluster-scoped шаблон для BegetMachineTemplate |
| [WGBootstrapTemplate](concepts/wg-bootstrap-template.md) | Cluster-scoped шаблон для KubeadmConfigTemplate |
| [Пайплайн рендеринга](concepts/rendering-pipeline.md) | Как переменные превращаются в ClusterAPI-ресурсы |

### Руководство пользователя

| Документ | Описание |
|----------|----------|
| [Установка](user-guide/installation.md) | Установка оператора в кластер |
| [Создание шаблонов](user-guide/creating-templates.md) | Написание WGMachineTemplate и WGBootstrapTemplate |
| [Развёртывание Worker Group](user-guide/deploying-worker-groups.md) | Создание WorkerGroupClaim и управление нодами |
| [Обновление и rollout](user-guide/updating-and-rollouts.md) | Изменение переменных, отслеживание rollout, таймауты |
| [Pause / Resume](user-guide/pause-resume.md) | Приостановка и возобновление reconcile |
| [Наблюдаемость](user-guide/monitoring.md) | Events, conditions, статус-поля для мониторинга |

### Справочник

| Документ | Описание |
|----------|----------|
| [API Reference](reference/api.md) | Полное описание всех полей CRD |

### Примеры

| Документ | Описание |
|----------|----------|
| [Примеры](examples/README.md) | Обзор доступных примеров |
| [Базовый Worker Group](examples/basic-worker-group/README.md) | Минимальный рабочий пример с одной Worker Group |

### Диагностика

| Документ | Описание |
|----------|----------|
| [Troubleshooting](troubleshooting.md) | Типичные проблемы и их решение |
