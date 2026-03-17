# WorkerGroupClaim Operator: Полное описание реализации

> Цель документа: детальное описание каждого этапа работы оператора с привязкой к секциям ТЗ (Readme.md). Описывает что реализовано, как работает, и где реализация отличается от оригинального ТЗ.

---

## Содержание

1. [Обзор архитектуры](#1-обзор-архитектуры)
2. [CRD: WGMachineTemplate](#2-crd-wgmachinetemplate)
3. [CRD: WGBootstrapTemplate](#3-crd-wgbootstraptemplate)
4. [CRD: WorkerGroupClaim](#4-crd-workergroupclaim)
5. [Пайплайн рендеринга](#5-пайплайн-рендеринга)
6. [Вычисление hash и именование ресурсов](#6-вычисление-hash-и-именование-ресурсов)
7. [Создание управляемых ресурсов](#7-создание-управляемых-ресурсов)
8. [Reconcile: основной цикл](#8-reconcile-основной-цикл)
9. [Rollout: отслеживание и таймаут](#9-rollout-отслеживание-и-таймаут)
10. [In-place обновления (без rollout)](#10-in-place-обновления-без-rollout)
11. [Pause / Resume](#11-pause--resume)
12. [Удаление (Finalizer)](#12-удаление-finalizer)
13. [Orphan Cleanup](#13-orphan-cleanup)
14. [Watches: стратегия наблюдения](#14-watches-стратегия-наблюдения)
15. [Статус и Conditions](#15-статус-и-conditions)
16. [Events (наблюдаемость)](#16-events-наблюдаемость)
17. [Сводная таблица отклонений от ТЗ](#17-сводная-таблица-отклонений-от-тз)

---

## 1. Обзор архитектуры

### Что говорит ТЗ (секции 1.1–1.2)

Dedicated Kubernetes-оператор, заменяющий Crossplane Composition. Три CRD:

| CRD | Scope | Роль |
|-----|-------|------|
| `WGBootstrapTemplate` | Cluster | Go-template → KubeadmConfigTemplate |
| `WGMachineTemplate` | Cluster | Go-template → BegetMachineTemplate |
| `WorkerGroupClaim` | Namespaced | Переменные + параметры MachineDeployment |

### Что реализовано

Полностью совпадает с ТЗ. Три CRD в группе `workergroup.in-cloud.io/v1alpha1`.

**Отклонений нет.**

---

## 2. CRD: WGMachineTemplate

### Что говорит ТЗ (секция 3)

Cluster-scoped. Единственное поле `spec.value` — строка с Go-template, который при рендеринге выдаёт YAML `BegetMachineTemplate`. Один шаблон используется множеством Claims из любых namespace'ов.

### Что реализовано

Одно поле `spec.value` (строка). Шаблон содержит `{{ .cpuCount }}`, `{{ .memory }}` и т.д. — placeholder'ы, которые маппятся 1:1 на ключи из `WorkerGroupClaim.spec.infrastructure`.

**Отклонений нет.**

---

## 3. CRD: WGBootstrapTemplate

### Что говорит ТЗ (секция 2)

Cluster-scoped. Единственное поле `spec.value` — Go-template для `KubeadmConfigTemplate`. Содержит cloud-init файлы, `joinConfiguration`, `preKubeadmCommands`. Шаблон **не содержит** `metadata` — оператор добавляет его при создании.

### Что реализовано

Одно поле `spec.value` (строка). Metadata добавляется оператором при создании ресурса.

**Отклонений нет.**

---

## 4. CRD: WorkerGroupClaim

### Что говорит ТЗ (секция 4)

Namespaced ресурс с плоскими переменными + параметры MachineDeployment.

### 4.1. Spec — отклонения от ТЗ

| Поле | ТЗ (секция 4.2) | Реализация | Причина изменения |
|------|-----------------|------------|-------------------|
| `infrastructure` | `map[string]string` | `map[string]apiextensionsv1.JSON` | Нативные типы (string, int, bool, array, object). `sshKeyIds` как `[]int64` вместо строки |
| `bootstrap` | `map[string]string` | `map[string]apiextensionsv1.JSON` | Аналогично |
| `strategy` | `spec.strategy` | `spec.strategy` (в CRD), маппится на `md.Spec.Rollout.Strategy` | ClusterAPI v1beta2 использует путь `spec.rollout.strategy`, не `spec.strategy` |
| `deletion` | `nodeDrainTimeout: Duration` | `spec.deletion` (3 поля: `nodeDrainTimeoutSeconds`, `nodeVolumeDetachTimeoutSeconds`, `nodeDeletionTimeoutSeconds`) | ClusterAPI v1beta2: `spec.template.spec.deletion.*` — `int32` секунды. Хардкод defaults: 60/60/120 |
| `taints[].propagation` | Не упомянут явно | `MachineTaintPropagation` (Always/OnInitialization) | Подтверждено как реальное поле ClusterAPI `MachineTaint` |
| `rolloutTimeout` | `progressDeadlineSeconds` (секция 9.2) | `spec.rolloutTimeout` (Duration, default 30m) | `progressDeadlineSeconds` deprecated в CAPI v1beta2 |
| `kubeletConfiguration` | Отсутствует в ТЗ | `spec.kubeletConfiguration` (map) | Добавлено: merge defaults + inject в bootstrap vars. Функциональность перенесена из существовавшего Crossplane pipeline |
| `deletePolicy` | Отсутствует в ТЗ | `spec.deletePolicy` → `md.Spec.Deletion.Order` | Отдельное поле `MachineSetDeletionOrder` в CAPI v1beta2 |

### 4.2. Status — отклонения от ТЗ

| Поле | ТЗ (секция 4.4) | Реализация | Причина изменения |
|------|-----------------|------------|-------------------|
| `phase` | Ready, Provisioning, Updating, Deleting, Failed | + **Paused**, **Degraded** | Paused — полная фаза (не флаг). Degraded — rollout timeout без автоотката |
| `lastRendered.errors` | `[]string` | `lastRendered.error` (string) | Упрощено: одна ошибка за раз достаточна |
| `rolloutStartedAt` | Не упомянут | `*metav1.Time` | Необходим для timeout tracking |
| `observedGeneration` | Не упомянут | `int64` | Стандартная практика controller-runtime |
| `machineDeploymentStatus` | Описан | Реализован: name, replicas, readyReplicas, upToDateReplicas | `upToDateReplicas` (v1beta2), не `updatedReplicas` (v1beta1) |

### 4.3. Переменные infrastructure/bootstrap — ключевое отклонение

**ТЗ секция 4.3**: "Тип `map[string]any`". Значения "могут быть string/bool/array/object".

**Реализация**: `map[string]apiextensionsv1.JSON` с `x-kubernetes-preserve-unknown-fields: true`.

**Почему**: CRD OpenAPI v3 schema не поддерживает `map[string]any` напрямую. `apiextensionsv1.JSON` хранит raw JSON bytes, что позволяет:
- `cpuCount: 4` → нативный `int64`
- `sshKeyIds: [123, 456]` → нативный массив `[]int64`
- `image: "k8s-customer:latest"` → нативный `string`

---

## 5. Пайплайн рендеринга

### Что говорит ТЗ (секция 5)

```
WorkerGroupClaim.spec.infrastructure → WGMachineTemplate.spec.value → BegetMachineTemplate
WorkerGroupClaim.spec.bootstrap      → WGBootstrapTemplate.spec.value → KubeadmConfigTemplate
```

Шаги: загрузить шаблоны → подготовить переменные → Go template.Execute → валидация → hash → обернуть metadata → сравнить с текущим.

### Что реализовано

Реализация следует ТЗ, но добавляет промежуточные шаги для разрешения конфликтов ТЗ и сохранения привычного поведения при миграции с Crossplane.

### 5.1. Шаг 1: Загрузить шаблоны

По `spec.machineTemplateRef.name` и `spec.bootstrapTemplateRef.name` загружаются cluster-scoped шаблоны. Если шаблон не найден → `PhaseFailed`, condition `TemplatesRendered=False`, reason `TemplateNotFound`.

**Совпадает с ТЗ секция 5.2 шаг 1, секция 9.6.**

### 5.2. Шаг 2: Подготовить переменные

Каждое значение из `map[string]apiextensionsv1.JSON` десериализуется из raw JSON в нативный Go-тип. Используется `UseNumber()` для сохранения точности `int64` (важно для `sshKeyIds`).

**Отклонение от ТЗ**: ТЗ подразумевает `map[string]string`. Реализация — `map[string]interface{}` с нативными типами, потому что `sshKeyIds` из CRD Beget-провайдера — это `[]int64`, и рендерить массив из строки неудобно.

### 5.3. Шаг 3: Авто-инжект переменных (не в ТЗ)

Три переменные инжектятся автоматически **до** рендеринга:

| Переменная | Значение | Куда | Зачем |
|-----------|---------|------|-------|
| `nodeLabels` | `"k1=v1,k2=v2"` (sorted) | bootstrap vars | Для kubelet `--node-labels` flag. ТЗ секция 4.2 показывает `{{ .nodeLabels }}` в шаблоне, но `spec.nodeLabels` — это map. Оператор конвертирует автоматически |
| `machineDeploymentName` | `{clusterName}-{claimName}` | bootstrap vars | Для системного label `node-group.beget.com/name` в cloud-init. Ранее это обеспечивалось отдельным pipeline step в Crossplane |
| `__kubeletConfigYaml` | YAML kubelet config (или `""` для hash) | bootstrap vars | Оператор мержит `spec.kubeletConfiguration` с defaults, сериализует в YAML. Шаблон вставляет как cloud-init файл |

**Зачем `nodeLabels`**: ТЗ секция 4.2 содержит конфликт — `spec.nodeLabels` это `map[string]string` для MD labels, но bootstrap-шаблон ожидает строку `"k=v,k=v"` для kubelet. Оператор разрешает конфликт: автоматически конвертирует map в sorted string и инжектит. Пользователю не нужно указывать labels дважды.

### 5.4. Шаг 4: Выполнить Go template

Шаблон парсится и выполняется с опцией `missingkey=error` — если шаблон содержит `{{ .foo }}`, а переменной `foo` нет, рендеринг падает с ошибкой. Без этой опции Go template подставляет `<no value>`, создавая невалидный YAML.

Доступны пользовательские template-функции (FuncMap).

**Совпадает с ТЗ секция 5.2 шаги 3–5, секция 9.1.**

### 5.5. Template-функции

| Функция | ТЗ (секция 5.3) | Реализация | Отличие |
|---------|-----------------|------------|---------|
| `toYamlList` | Comma-separated → YAML list | **`toYaml`** — любое значение → YAML | Расширено: обрабатывает любой тип, включая массивы и объекты |
| `indent N` | Есть | Есть | — |
| `quote` | Есть | Есть | — |
| `default` | Есть | Есть | — |
| `b64enc` | Есть | Есть | — |

**Отклонение**: `toYamlList` заменён на универсальный `toYaml`. Для `sshKeyIds: [123, 456]` рендерится корректный YAML-массив через `{{ .sshKeyIds | toYaml }}`. Это работает с любым типом данных, не только comma-separated строками.

### 5.6. Cloud-init Jinja экранирование

**ТЗ секция 5.4**: `{{ "{{" }}` → `{{`.

Стандартный механизм Go template. Работает без дополнительного кода.

**Совпадает с ТЗ.**

---

## 6. Вычисление hash и именование ресурсов

### Что говорит ТЗ (секция 7.1, 8.2)

```
MachineDeployment:      {clusterName}-{claimName}
BegetMachineTemplate:   {clusterName}-{claimName}-bmt-{hash8}
KubeadmConfigTemplate:  {clusterName}-{claimName}-kct-{hash8}
```

Hash = SHA256 от rendered YAML (секция 8.2).

> Примечание: ТЗ секция 4.3 ошибочно указывает "SHA256 от содержимого map" — это противоречит секции 8.2. Реализовано по секции 8.2 (от rendered YAML), т.к. это единственно корректный вариант: изменение шаблона при тех же переменных тоже должно давать новый hash.

### Что реализовано

Hash вычисляется как первые 8 символов SHA256 от rendered YAML строки. Имя ресурса формируется как `{clusterName}-{claimName}-{тип}-{hash8}`.

### 6.1. Что входит в hash BMT

```
SHA256( rendered YAML из WGMachineTemplate )[:8]
```

Входят: все `spec.infrastructure` переменные + содержимое `WGMachineTemplate.spec.value`.

Изменение **любой** infrastructure-переменной ИЛИ шаблона → новый hash → новый BMT.

### 6.2. Что входит в hash KCT

```
SHA256( rendered YAML из WGBootstrapTemplate с __kubeletConfigYaml="" )[:8]
```

Входят: все `spec.bootstrap` переменные + `nodeLabels` (auto-inject) + `machineDeploymentName` (auto-inject) + содержимое `WGBootstrapTemplate.spec.value`.

**НЕ входит**: `__kubeletConfigYaml` — при вычислении hash подставляется пустая строка `""`. Это значит:
- Изменение `spec.kubeletConfiguration` **НЕ** создаёт новый KCT (нет rollout)
- KCT обновляется in-place (см. раздел 10)

Такое поведение сохраняет привычную для пользователей логику: kubelet config обновляется in-place без пересоздания нод (так же работало в заменяемой Crossplane composition).

### 6.3. Что НЕ входит ни в один hash

| Поле | Почему не в hash | Обновление |
|------|-----------------|------------|
| `taints` | Идёт напрямую в MD, не через rendering pipeline | In-place MD update |
| `replicas` | Поле MachineDeployment, не шаблона | In-place MD update |
| `version` | Поле MachineDeployment | In-place MD update |
| `strategy` | Поле MachineDeployment | In-place MD update |
| `deletion.*` | Таймауты удаления Machine (defaults: 60/60/120) | In-place MD update |
| `kubeletConfiguration` | Осознанное решение: сохранение поведения при миграции | In-place KCT update |

---

## 7. Создание управляемых ресурсов

### Что говорит ТЗ (секция 7)

| Ресурс | Immutable | Стратегия обновления |
|--------|-----------|---------------------|
| BegetMachineTemplate | **Да** | Create-new → update-ref → delete-old |
| KubeadmConfigTemplate | **Да** | Create-new → update-ref → delete-old |
| MachineDeployment | Нет | In-place update |

### Что реализовано

#### 7.1. BegetMachineTemplate (BMT)

Создаётся через unstructured-клиент — **без Go-зависимости** на Beget provider. Оператор не импортирует типы провайдера, а работает с generic YAML.

Процесс:
1. Парсит rendered YAML
2. Извлекает `spec`
3. Устанавливает metadata: name (с hash), namespace, labels, ownerReference
4. Создаёт через unstructured Create

**Immutability**: если BMT с таким именем уже существует — skip (идемпотентно). Новые переменные = новый hash = новое имя = новый ресурс.

#### 7.2. KubeadmConfigTemplate (KCT)

Создаётся через типизированный клиент.

**Отличие от BMT**: KCT может обновляться in-place (при изменении `kubeletConfiguration` — hash не меняется, но содержимое отличается). См. раздел 10.

#### 7.3. MachineDeployment (MD)

Имя: `{clusterName}-{claimName}` (без hash — мутабельный ресурс).

Маппинг полей из WorkerGroupClaim в MachineDeployment:

| Claim поле | MD путь (CAPI v1beta2) | ТЗ путь | Отклонение |
|-----------|----------------------|---------|------------|
| `clusterName` | `spec.clusterName` | `spec.clusterName` | — |
| `replicas` | `spec.replicas` | `spec.replicas` | — |
| `version` | `spec.template.spec.version` | `spec.template.spec.version` | — |
| `strategy` | **`spec.rollout.strategy`** | `spec.strategy` | Путь изменён: CAPI v1beta2 |
| `deletePolicy` | **`spec.deletion.order`** | Не в ТЗ | Добавлено: CAPI v1beta2 |
| `deletion` | **`spec.template.spec.deletion`** (3 поля) | `spec.template.spec.nodeDrainTimeout` | Расширено: drain + volumeDetach + nodeDeletion. Хардкод defaults: 60/60/120 |
| `taints` | `spec.template.spec.taints` | `spec.template.spec.taints` | — |
| `nodeLabels` | `spec.template.metadata.labels` | `spec.template.metadata.labels` | — |
| BMT ref | `spec.template.spec.infrastructureRef` | Совпадает | — |
| KCT ref | `spec.template.spec.bootstrap.configRef` | Совпадает | — |

**Selector**:
- ТЗ: `matchLabels: {cluster.x-k8s.io/cluster-name: X}` — слишком широкий, конфликтует при нескольких Claims в одном кластере
- Реализация: `matchLabels: {cluster.x-k8s.io/cluster-name: X, workergroup.in-cloud.io/claim-name: Y}` — уникальный для каждого Claim

**Labels на Machine template** (spec.template.metadata.labels):
```yaml
labels:
  cluster.x-k8s.io/cluster-name: {clusterName}
  workergroup.in-cloud.io/claim-name: {claimName}
  node-group.beget.com/name: {clusterName}-{claimName}  # системный
  {user nodeLabels...}                                    # пользовательские
```

#### 7.4. Owner References

Все managed-ресурсы (BMT, KCT, MD) получают `ownerReference` на `WorkerGroupClaim` с `blockOwnerDeletion: true` и `controller: true`.

Шаблоны (`WGBootstrapTemplate`, `WGMachineTemplate`) — **NOT owned**. Они cluster-scoped и разделяемые.

**Совпадает с ТЗ секция 7.2.**

---

## 8. Reconcile: основной цикл

### Что говорит ТЗ (секция 8.1)

```
1. Загрузить шаблоны
2. Render vars → templates
3. Hash rendered YAML, сравнить с текущим
4. Если изменился: create new → update MD ref → old → pendingDeletion
5. Ждать rollout completion
6. Удалить pendingDeletion → Ready
```

### Что реализовано

Полный порядок шагов:

```
 ┌─ 1. Загрузить claim (exit если удалён)
 ├─ 2. Проверить DeletionTimestamp → reconcileDelete()
 ├─ 3. Проверить pause аннотацию → reconcilePaused()
 ├─ 4. Resume из Paused (если аннотация снята)
 ├─ 5. Ensure finalizer
 ├─ 6. Validate nodeLabels (зарезервированный префикс)
 ├─ 7. renderTemplates():
 │   ├─ Загрузить шаблоны по ref
 │   ├─ Десериализовать переменные (JSON → native types)
 │   ├─ Авто-инжект: nodeLabels, machineDeploymentName, kubeletConfigYaml
 │   ├─ Render infrastructure vars → BMT YAML
 │   └─ Render bootstrap vars → KCT YAML
 ├─ 8. Вычислить hash BMT и KCT
 ├─ 9. Сформировать имена ресурсов
 ├─ 10. Определить тип изменения:
 │   ├─ isFirstProvision = (currentTemplates == nil)
 │   └─ hashChanged = (old hash ≠ new hash)
 ├─ 11. Создать BMT (если не существует)
 ├─ 12. Создать или in-place обновить KCT
 ├─ 13. Если hashChanged: переместить старые шаблоны в pendingDeletion
 ├─ 14. Создать или обновить MachineDeployment (diff-based)
 ├─ 15. Обновить status: currentTemplates, lastRendered, TemplatesRendered
 ├─ 16. Скопировать MD status в claim (replicas/readyReplicas/upToDateReplicas)
 ├─ 17. Установить observedGeneration
 └─ 18. Переход фазы:
     ├─ hashChanged → startRollout() [PhaseUpdating, requeue 30s]
     ├─ pendingDeletion не пуст → trackRollout() [PhaseUpdating/Degraded]
     └─ иначе → setReady() [PhaseReady, без requeue]
```

### 8.1. Маппинг шагов ТЗ → реализация

| ТЗ шаг (секция 8.1) | Реализация | Шаги |
|---------------------|------------|------|
| Загрузить шаблоны | Загрузка по machineTemplateRef / bootstrapTemplateRef | 7 |
| Render vars → templates | Go template с missingkey=error | 7 |
| Hash rendered YAML | SHA256[:8] от rendered output | 8 |
| Сравнить с текущим | Сравнение имён (имя включает hash) | 10 |
| Create new, update MD ref | Создание BMT/KCT, обновление MD refs | 11–14 |
| Old → pendingDeletion | Добавление в status.pendingDeletion | 13 |
| Ждать rollout | trackRollout() + requeue 30s | 18 |
| Удалить pendingDeletion | completeRollout() внутри trackRollout | 18 |

### 8.2. Что добавлено сверх ТЗ

| Этап | Зачем |
|------|-------|
| Pause проверка (шаг 3) | Заморозка reconcile по аннотации (ТЗ секция 18) |
| NodeLabels validation (шаг 6) | Запрет зарезервированного префикса `workergroup.in-cloud.io/` |
| Auto-inject vars (шаг 7) | nodeLabels, machineDeploymentName, kubeletConfig — разрешение конфликтов ТЗ и сохранение привычного поведения при миграции |
| In-place KCT update (шаг 12) | kubeletConfiguration без rollout |
| MD diff (шаг 14) | Разделение ref-changes (вызывают rollout) и in-place changes (не вызывают) |
| mirrorMDStatus (шаг 16) | Пробросить MD status в claim для удобства мониторинга |
| observedGeneration (шаг 17) | Стандарт controller-runtime |

---

## 9. Rollout: отслеживание и таймаут

### Что говорит ТЗ (секция 8.1, 9.2)

- Ждать `upToDateReplicas == replicas`
- При зависании rollout — `RolloutComplete=False`, `ProgressDeadlineExceeded`
- Пользователь может откатить, вернув старые переменные

### Что реализовано

### 9.1. Запуск rollout

Вызывается когда hash изменился (новый BMT и/или KCT).

Действия:
1. Phase → `Updating`
2. Запоминается `status.rolloutStartedAt = now`
3. Conditions: `RolloutComplete=False`, `RolloutTimedOut=False`, `Ready=False`
4. Event: `RolloutStarted`
5. Requeue через 30 секунд

### 9.2. Отслеживание

Вызывается каждые 30 секунд (requeue), пока `pendingDeletion` не пуст.

```
1. Загрузить MachineDeployment
2. Rollout завершён?
   ├─ Да → completeRollout()
   └─ Нет → проверить timeout
       ├─ Время превышено?
       │   ├─ Да → Phase=Degraded, RolloutTimedOut=True, requeue 60s
       │   └─ Нет → Phase=Updating (без изменений), requeue 30s
```

### 9.3. Определение завершения

Двухрежимная проверка для совместимости с CAPI v1beta1 и v1beta2:

1. **v1beta2 путь** (primary): condition `RollingOut=False` И `upToDateReplicas == replicas`
2. **v1beta1 fallback** (deprecated): `updatedReplicas == replicas` И `unavailableReplicas == 0`

**Отклонение от ТЗ**: ТЗ упоминает только `upToDateReplicas == replicas`. Реализация дополнительно использует v1beta2 conditions как primary indicator и v1beta1 как fallback.

### 9.4. Завершение

1. **MachineSet safety check**: для каждого ресурса в `pendingDeletion` оператор ищет MachineSets по label `cluster.x-k8s.io/deployment-name` и проверяет, что ни один MS не ссылается на этот шаблон (`configRef.name` для KCT, `infrastructureRef.name` для BMT)
2. Если MS ещё ссылается → шаблон остаётся в `pendingDeletion`, requeue через 30s
3. Если MS не ссылается → удалить ресурс, event `StaleTemplateDeleted`
4. Когда все `pendingDeletion` удалены: очистить `pendingDeletion` и `rolloutStartedAt`
5. Phase → `Ready`
6. Conditions: `RolloutComplete=True`, `Ready=True`
7. Event `RolloutComplete`
8. Без requeue — стабильное состояние

### 9.5. Таймаут

**ТЗ**: `progressDeadlineSeconds` (default 300s).
**Реализация**: `spec.rolloutTimeout` (Duration, default 30m).

**Отклонение**: `progressDeadlineSeconds` deprecated в CAPI v1beta2. Заменён на собственное поле с более длинным default (30m vs 5m). При таймауте:
- Phase → `Degraded`
- Condition `RolloutTimedOut=True`
- **Без автоотката** — оператор продолжает ждать завершения с requeue 60s
- Пользователь может либо дождаться, либо вернуть старые переменные

### 9.6. Повторное изменение во время rollout

**ТЗ секция 9.3**: создать новый шаблон, обновить ref, добавить предыдущий в pendingDeletion.

**Реализация**: полностью совпадает. На каждом reconcile:
- Рендер с актуальными переменными → новый hash
- Если hash изменился снова → новый BMT/KCT, старый (включая "промежуточный") в pendingDeletion
- `rolloutStartedAt` сбрасывается (новый timeout отсчёт)
- `pendingDeletion` накапливает записи — все удалятся после завершения последнего rollout

---

## 10. In-place обновления (без rollout)

### Что говорит ТЗ (секция 8.4)

| Изменение | BMT | KCT | MD |
|-----------|-----|-----|----|
| `replicas` | — | — | in-place |
| `version` | — | — | in-place |
| `taints` | — | — | in-place |
| `labels` | — | — | in-place |
| `strategy` | — | — | in-place |

### Что реализовано

При обновлении MD оператор вычисляет diff, разделяя изменения на два типа:

- **RefsChanged**: изменился `infrastructureRef` или `bootstrap.configRef` → triggers rollout
- **InPlaceChanged**: изменились `replicas`, `version`, `strategy`, `taints`, `templateLabels`, `deletion` → in-place MD update, без rollout

Если `RefsChanged == false` и `InPlaceChanged == true` → обновить MD in-place, **без** создания новых шаблонов.

**Совпадает с ТЗ секция 8.4.**

### 10.1. Дополнение: kubeletConfiguration in-place

**Не в ТЗ.** Добавлено для сохранения привычного поведения при миграции с Crossplane.

При изменении `spec.kubeletConfiguration`:
1. Hash KCT **не меняется** (hash считается с `__kubeletConfigYaml=""`)
2. Имя KCT **то же**
3. Но rendered content отличается (с реальным kubelet YAML)
4. Оператор сравнивает spec существующего и desired KCT
5. Если отличается → in-place update существующего KCT
6. **Без rollout** — kubelet config применяется через cloud-init только при создании новых нод

---

## 11. Pause / Resume

### Что говорит ТЗ (секция 18)

Аннотация `workergroup.in-cloud.io/paused: "true"`:
- При paused: пропускает reconcile, ничего не создаёт/обновляет/удаляет
- При resume: полный reconcile, применяет накопленные изменения

### Что реализовано

**Pause**:
1. Detect: аннотация `workergroup.in-cloud.io/paused` равна `"true"`
2. Phase → `Paused`
3. Conditions: `Paused=True`, `Ready=False`
4. Event: `Paused`
5. Без requeue — reconcile запустится только при внешнем триггере (изменение аннотации)

**Resume**:
1. При следующем reconcile: аннотация снята И phase == Paused
2. Condition: `Paused=False`, reason `Resumed`
3. Event: `Resumed`
4. Продолжается нормальный reconcile
5. Если за время паузы переменные изменились → рендер → новый hash → rollout

**Совпадает с ТЗ секция 18.1–18.2.**

**Дополнение**: `Paused` — полноценная фаза (не просто флаг). Это предотвращает случайные обновления при гонках между снятием аннотации и reconcile.

---

## 12. Удаление (Finalizer)

### Что говорит ТЗ (секция 8.5)

```
1. phase: Deleting
2. Удалить MachineDeployment
3. Удалить текущие BMT и KCT
4. Удалить pendingDeletion
5. Снять finalizer
```

### Что реализовано

```
Step 1: Phase → Deleting (если ещё не)

Step 2: Удалить MachineDeployment
  ├─ MD существует → delete + requeue 10s (ждём cascade delete Machine → Node)
  └─ MD не найден → продолжить

Step 3: Удалить current templates (BMT + KCT)
  └─ Log errors, но продолжить (graceful degradation)

Step 4: Удалить pendingDeletion templates
  └─ Log errors, но продолжить

Step 5: Remove finalizer
  └─ Event: "Deleted"
  └─ Return (API server удалит объект)
```

**Совпадает с ТЗ секция 8.5.**

**Нюанс**: MD удаляется **первым**, потому что MD ссылается на BMT/KCT. Удаление BMT/KCT при живом MD вызвало бы ошибки в CAPI контроллере.

**Нюанс**: requeue 10s на шаге 2 — ждём, пока CAPI обработает cascade delete (MD → MachineSet → Machine → Node). Только после исчезновения MD переходим к удалению шаблонов.

---

## 13. Orphan Cleanup

### Что говорит ТЗ (секция 9.5)

> Периодический reconcile (каждые 5 минут) ищет BMT/KCT с label `workergroup.in-cloud.io/claim-name`, не упоминаемые ни в `currentTemplates`, ни в `pendingDeletion`, старше 10 минут.

### Что реализовано

Отдельный периодический процесс (запускается на leader'е):

- **Интервал**: каждые 5 минут
- **Grace period**: 10 минут

Алгоритм каждого прохода:
1. Список всех labeled BMT и KCT (по label `claim-name`)
2. Список всех Claims → построить reference set (currentTemplates + pendingDeletion)
3. Для каждого BMT/KCT: если НЕ в reference set И `creationTimestamp` старше 10 минут → удалить
4. Event: `OrphanTemplateDeleted`

**Совпадает с ТЗ секция 9.5.**

**Сценарий защиты**: оператор создал BMT → крэш до обновления status → перезапуск через 15 минут → orphan cleaner удалит BMT → следующий reconcile воссоздаст. Это ожидаемое поведение — лишний churn, но самовосстановление.

---

## 14. Watches: стратегия наблюдения

### Что говорит ТЗ (секция 8.3)

Оператор watch'ит:
1. `WorkerGroupClaim` — основной триггер
2. `WGBootstrapTemplate` — re-render при изменении шаблона
3. `WGMachineTemplate` — аналогично
4. `MachineDeployment` — rollout status

### Что реализовано

Четыре watch-источника:

| # | Ресурс | Тип | Что происходит при изменении |
|---|--------|-----|------------------------------|
| 1 | `WorkerGroupClaim` | Primary | Стандартный reconcile |
| 2 | `MachineDeployment` | Owned | Автоматический enqueue владельца (для trackRollout) |
| 3 | `WGBootstrapTemplate` | Watch + map | Enqueue **всех** Claims, ссылающихся на шаблон |
| 4 | `WGMachineTemplate` | Watch + map | Enqueue **всех** Claims, ссылающихся на шаблон |

**Fan-out**: при изменении `WGBootstrapTemplate` → оператор находит все Claims, ссылающиеся на этот шаблон → каждый Claim проходит полный reconcile → re-render → hash compare → возможно новый KCT.

**Совпадает с ТЗ секция 8.3, 13.3.**

**Дополнение**: `MaxConcurrentReconciles: 5` — ограничение параллелизма при обновлении shared-шаблона. Если 100 Claims ссылаются на один шаблон — обработка пакетами по 5, а не все 100 одновременно.

---

## 15. Статус и Conditions

### Фазы (state machine)

```
                    ┌──────────────────────────────────────────────┐
                    │                                              │
                    ▼                                              │
              Provisioning ──────► Ready ◄────────────────────────┤
                    │                │                              │
                    │                │ hash changed                 │
                    │                ▼                              │
                    │           Updating ──────► Ready             │
                    │                │              (completeRollout)
                    │                │ timeout
                    │                ▼
                    │           Degraded ──────► Ready
                    │                              (eventual completion)
                    │
              (render/validation error)
                    │
                    ▼
                Failed ──────► Provisioning/Ready (после исправления)

   Любая фаза + pause annotation → Paused → (resume) → предыдущий flow
   Любая фаза + DeletionTimestamp → Deleting → (cleanup) → удалён
```

### Conditions

| Condition | True | False |
|-----------|------|-------|
| `Ready` | Все ресурсы актуальны, rollout нет | Rollout в процессе, ошибка, или пауза |
| `TemplatesRendered` | Рендеринг успешен | Ошибка рендеринга или шаблон не найден |
| `RolloutComplete` | Rollout завершён | Rollout в процессе |
| `RolloutTimedOut` | Timeout превышен | Нет таймаута |
| `Paused` | Claim на паузе | Активен |

### Requeue стратегия

| Фаза | Requeue | Комментарий |
|------|---------|-------------|
| Provisioning | Нет | Стабилизируется в Ready |
| Ready | Нет | Стабильное состояние, ждём watch event |
| Updating | 30s | Polling rollout status |
| Degraded | 60s | Ждём завершение или manual intervention |
| Failed | 60s | Retry (пользователь мог исправить шаблон/переменные) |
| Paused | Нет | Ждём снятия аннотации (watch event) |
| Deleting | 10s | Ждём cascade delete MD |

---

## 16. Events (наблюдаемость)

### Что говорит ТЗ (секция 10.1)

| Event | Type | Reason |
|-------|------|--------|
| Шаблон отрендерен | Normal | TemplateRendered |
| Создан BMT | Normal | InfrastructureTemplateCreated |
| Создан KCT | Normal | BootstrapTemplateCreated |
| Обновлён MD | Normal | MachineDeploymentUpdated |
| Rollout начат | Normal | RolloutStarted |
| Rollout завершён | Normal | RolloutComplete |
| Удалён старый шаблон | Normal | StaleTemplateDeleted |
| Ошибка рендеринга | Warning | RenderError |
| Шаблон не найден | Warning | TemplateNotFound |
| Rollout timeout | Warning | RolloutTimeout |

### Что реализовано

Все events из ТЗ реализованы. Дополнительно:

| Event | Type | Reason | Когда |
|-------|------|--------|-------|
| Паузa | Normal | Paused | Annotation установлена |
| Возобновление | Normal | Resumed | Annotation снята |
| Удалён claim | Normal | Deleted | Finalizer обработан |
| Orphan удалён | Normal | OrphanTemplateDeleted | Periodic cleanup |
| KCT in-place update | Normal | BootstrapTemplateUpdated | kubelet config change |

---

## 17. Сводная таблица отклонений от ТЗ

### Критические отклонения (изменяют API или поведение)

| # | ТЗ секция | Что было в ТЗ | Что реализовано | Причина |
|---|-----------|---------------|-----------------|---------|
| 1 | 4.2, 4.3 | `map[string]string` | `map[string]apiextensionsv1.JSON` | Нативные типы для `sshKeyIds: []int64` и `usePrivateNetwork: bool` из CRD Beget-провайдера |
| 2 | 4.3 vs 8.2 | Hash от map contents (4.3) | Hash от rendered YAML (8.2) | Противоречие в ТЗ. Rendered YAML — единственно правильный вариант: изменение шаблона тоже должно давать новый hash |
| 3 | 6.3 | Selector только по `cluster-name` | + `claim-name` label | Без этого несколько Claims в одном кластере конфликтуют по selector |
| 4 | 6.3 | `spec.strategy` | `spec.rollout.strategy` (CAPI v1beta2) | ClusterAPI v1beta2 использует другой путь API |
| 5 | 6.3 | `nodeDrainTimeout: Duration` | `spec.deletion` (3 поля, int32 секунды, defaults: 60/60/120) | ClusterAPI v1beta2: расширено до 3 таймаутов (drain, volumeDetach, nodeDeletion) |
| 6 | 9.2 | `progressDeadlineSeconds` (300s) | `spec.rolloutTimeout` (30m), phase `Degraded` | `progressDeadlineSeconds` deprecated в CAPI v1beta2. Собственное поле с большим default |
| 7 | 5.3 | `toYamlList` | `toYaml` (универсальный) | Обрабатывает любой тип данных, не только comma-separated строки |

### Дополнения (отсутствуют в ТЗ, добавлены при реализации)

| # | Что добавлено | Зачем |
|---|---------------|-------|
| 1 | `spec.kubeletConfiguration` | Merge defaults + inject в bootstrap vars. Сохраняет привычное поведение при миграции с Crossplane |
| 2 | `spec.deletePolicy` | ClusterAPI v1beta2 поле `spec.deletion.order` |
| 3 | Auto-inject `nodeLabels` | Авто-конвертация `map[string]string` → `"k=v"` строка для kubelet `--node-labels`. Разрешение конфликта в ТЗ секция 4.2 |
| 4 | Auto-inject `machineDeploymentName` | Системный label `node-group.beget.com/name` в cloud-init |
| 5 | Auto-inject `__kubeletConfigYaml` | Kubelet config как cloud-init файл |
| 6 | Phase `Paused` (полная фаза) | Предотвращение гонок при снятии аннотации |
| 7 | Phase `Degraded` | Rollout timeout как отдельная фаза (без автоотката) |
| 8 | `status.observedGeneration` | Стандарт controller-runtime |
| 9 | `status.rolloutStartedAt` | Необходим для timeout tracking |
| 10 | Orphan cleanup periodic | Safety net для потерянных шаблонов при crash оператора |
| 11 | Unstructured BMT client | Zero dependency на Go-модуль Beget provider |

### Принятые ограничения

| Ограничение | Статус | Комментарий |
|-------------|--------|-------------|
| Версионирование шаблонов (pin version) | Не реализовано | Claim всегда использует текущую версию шаблона |
| ValidatingWebhook | Не реализовано | Валидация на уровне оператора (runtime), CEL rules для immutability |
| MutatingWebhook (defaulting) | Не реализовано | Defaults в коде оператора |
| Миграция с Crossplane | Требует доработки | Hash-стратегия отличается, selector immutability в MD |

---

## Приложение A: Полный flow — от создания Claim до Ready

```
Пользователь:
  kubectl apply -f workergroupclaim.yaml

┌─────────────────────────────────────────────────────────────────────┐
│ 1. API Server принимает WorkerGroupClaim                            │
│    - CRD schema validation (OpenAPI v3)                             │
│    - CEL rules: clusterName immutable (self == oldSelf)             │
│    - Enqueue в workqueue оператора                                  │
└──────────────────────────────┬──────────────────────────────────────┘
                               │
┌──────────────────────────────▼──────────────────────────────────────┐
│ 2. Reconcile() начинается                                          │
│    Phase: "" → Provisioning                                         │
│    Добавить finalizer workergroup.in-cloud.io/finalizer       │
└──────────────────────────────┬──────────────────────────────────────┘
                               │
┌──────────────────────────────▼──────────────────────────────────────┐
│ 3. renderTemplates()                                                │
│                                                                     │
│    a) Загрузить WGMachineTemplate "default-machine" (cluster-scoped)│
│    b) Загрузить WGBootstrapTemplate "default-bootstrap"             │
│                                                                     │
│    c) Десериализовать claim.spec.infrastructure:                    │
│       { cpuCount: 4, memory: 4096, diskSize: 30720,                │
│         image: "k8s-customer:latest",                               │
│         sshKeyIds: [123, 456], ... }                                │
│                                                                     │
│    d) Десериализовать claim.spec.bootstrap:                         │
│       { containerdVersion: "1.7.19", clusterDNS: "29.64.0.10", ... }│
│                                                                     │
│    e) Авто-инжект в bootstrap vars:                                 │
│       nodeLabels = "environment=production,workload-type=general"   │
│       machineDeploymentName = "c9b2e5-client-c5ce2e"               │
│       __kubeletConfigYaml = "" (для hash) / реальный YAML (для     │
│       content)                                                      │
│                                                                     │
│    f) Render infrastructure vars через WGMachineTemplate → bmtYAML  │
│    g) Render bootstrap vars через WGBootstrapTemplate → kctYAML     │
└──────────────────────────────┬──────────────────────────────────────┘
                               │
┌──────────────────────────────▼──────────────────────────────────────┐
│ 4. Hash + Naming                                                    │
│                                                                     │
│    bmtHash = SHA256(bmtYAML)[:8]  →  "a3f8c1d2"                    │
│    kctHash = SHA256(kctYAML)[:8]  →  "dae26c58"                    │
│                                                                     │
│    bmtName = "c9b2e5-client-c5ce2e-bmt-a3f8c1d2"                   │
│    kctName = "c9b2e5-client-c5ce2e-kct-dae26c58"                   │
│    mdName  = "c9b2e5-client-c5ce2e"                                 │
└──────────────────────────────┬──────────────────────────────────────┘
                               │
┌──────────────────────────────▼──────────────────────────────────────┐
│ 5. Создать ресурсы                                                  │
│                                                                     │
│    a) BMT: unstructured Create → BegetMachineTemplate в кластере    │
│       Event: InfrastructureTemplateCreated                          │
│                                                                     │
│    b) KCT: typed Create → KubeadmConfigTemplate в кластере          │
│       Event: BootstrapTemplateCreated                               │
│                                                                     │
│    c) MD: Create MachineDeployment                                  │
│       - infrastructureRef → bmtName                                 │
│       - bootstrap.configRef → kctName                               │
│       - replicas, version, taints, nodeLabels, strategy             │
│       Event: MachineDeploymentUpdated                               │
└──────────────────────────────┬──────────────────────────────────────┘
                               │
┌──────────────────────────────▼──────────────────────────────────────┐
│ 6. Обновить status                                                  │
│                                                                     │
│    currentTemplates:                                                │
│      begetMachineTemplate: "c9b2e5-client-c5ce2e-bmt-a3f8c1d2"     │
│      kubeadmConfigTemplate: "c9b2e5-client-c5ce2e-kct-dae26c58"    │
│                                                                     │
│    lastRendered:                                                    │
│      infrastructureHash: "a3f8c1d2"                                 │
│      bootstrapHash: "dae26c58"                                      │
│                                                                     │
│    conditions:                                                      │
│      TemplatesRendered=True, Ready=True, RolloutComplete=True       │
│                                                                     │
│    phase: Ready                                                     │
│    observedGeneration: 1                                            │
└─────────────────────────────────────────────────────────────────────┘
```

## Приложение B: Flow обновления переменной

```
Пользователь:
  kubectl patch workergroupclaim c5ce2e -n godjee \
    --type=merge -p '{"spec":{"infrastructure":{"memory":"8192"}}}'

┌─────────────────────────────────────────────────────────────────────┐
│ 1. Reconcile() запускается (generation changed)                     │
│    Phase: Ready → (пока не меняем)                                  │
│                                                                     │
│ 2. renderTemplates() с memory=8192                                  │
│    → bmtYAML содержит "memory: 8192" вместо "memory: 4096"         │
│    → kctYAML не изменился                                           │
│                                                                     │
│ 3. Hash:                                                            │
│    bmtHash → "b7e2f4a1" (НОВЫЙ)                                    │
│    kctHash → "dae26c58" (тот же)                                    │
│                                                                     │
│ 4. hashChanged = true                                               │
│                                                                     │
│ 5. Создать новый BMT "...bmt-b7e2f4a1"                             │
│    Старый KCT "...kct-dae26c58" уже существует → skip               │
│                                                                     │
│ 6. Переместить старый BMT в pendingDeletion:                        │
│    - name: "c9b2e5-client-c5ce2e-bmt-a3f8c1d2"                     │
│                                                                     │
│ 7. Обновить MD: infrastructureRef → "...bmt-b7e2f4a1"              │
│    → ClusterAPI начинает rolling update                             │
│                                                                     │
│ 8. startRollout():                                                  │
│    Phase → Updating, rolloutStartedAt = now                         │
│    Event: RolloutStarted                                            │
│    Requeue 30s                                                      │
└──────────────────────────────┬──────────────────────────────────────┘
                               │ (30 секунд спустя)
┌──────────────────────────────▼──────────────────────────────────────┐
│ 9. trackRollout()                                                   │
│    MD.status: RollingOut=True → rollout ещё идёт                    │
│    Timeout не превышен                                              │
│    Requeue 30s                                                      │
└──────────────────────────────┬──────────────────────────────────────┘
                               │ (через N минут, rollout завершён)
┌──────────────────────────────▼──────────────────────────────────────┐
│ 10. trackRollout() → isRolloutComplete = true                       │
│                                                                     │
│ 11. completeRollout():                                              │
│     Удалить старый BMT "...bmt-a3f8c1d2"                            │
│     Event: StaleTemplateDeleted                                     │
│     Очистить pendingDeletion, rolloutStartedAt                      │
│     Phase → Ready                                                   │
│     Event: RolloutComplete                                          │
│     Без requeue — стабильное состояние                              │
└─────────────────────────────────────────────────────────────────────┘

Итого:
  - Старый BMT удалён ТОЛЬКО ПОСЛЕ завершения rollout
  - Новые ноды создаются с memory=8192
  - Старые ноды gracefully заменяются (RollingUpdate strategy)
```

## Приложение C: Flow обновления шаблона (fan-out)

```
Платформенный инженер:
  kubectl edit wgbootstraptemplate default-bootstrap
  → Добавил новый cloud-init файл в spec.value

┌─────────────────────────────────────────────────────────────────────┐
│ 1. Watch на WGBootstrapTemplate срабатывает                         │
│    Оператор находит все Claims с bootstrapTemplateRef.name          │
│    == "default-bootstrap"                                           │
│    Результат: [claim-A, claim-B, claim-C]                           │
│    → Enqueue все 3 в workqueue                                     │
│                                                                     │
│ 2. MaxConcurrentReconciles: 5 — все 3 обрабатываются параллельно   │
│                                                                     │
│ 3. Для каждого Claim:                                               │
│    - renderTemplates() с обновлённым шаблоном                       │
│    - kctYAML изменился → новый hash → новый KCT                     │
│    - bmtYAML тот же → hash тот же → skip                            │
│    - Обновить MD: bootstrap.configRef → новый KCT                   │
│    - Старый KCT → pendingDeletion                                   │
│    - startRollout()                                                 │
│                                                                     │
│ Результат: 3 параллельных rollout'а (по одному на Claim)            │
└─────────────────────────────────────────────────────────────────────┘
```