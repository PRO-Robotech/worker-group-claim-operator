# Пайплайн рендеринга

## Обзор

Оператор рендерит два шаблона для каждого `WorkerGroupClaim`:

```
WorkerGroupClaim.spec.infrastructure  +  WGMachineTemplate   → BegetMachineTemplate
WorkerGroupClaim.spec.bootstrap       +  WGBootstrapTemplate  → KubeadmConfigTemplate
```

Результат рендеринга — immutable ресурсы с hash в имени. Изменение переменных или шаблона создаёт новый ресурс, а не обновляет существующий.

## Шаги пайплайна

### 1. Загрузка шаблонов

По `spec.machineTemplateRef.name` и `spec.bootstrapTemplateRef.name` загружаются cluster-scoped шаблоны.

Если шаблон не найден → `phase: Failed`, condition `TemplatesRendered=False`, reason `TemplateNotFound`.

### 2. Подготовка переменных

Переменные из `spec.infrastructure` и `spec.bootstrap` десериализуются из `apiextensionsv1.JSON` в нативные Go-типы (`string`, `float64`, `bool`, `[]interface{}`, `map[string]interface{}`).

### 3. Авто-инжект (только для bootstrap)

Три переменные добавляются автоматически перед рендерингом bootstrap-шаблона:

| Переменная | Откуда | Зачем |
|-----------|--------|-------|
| `nodeLabels` | `spec.nodeLabels` (map → sorted string) | Kubelet `--node-labels` flag |
| `machineDeploymentName` | `{clusterName}-{claimName}` | Системный label в cloud-init |
| `__kubeletConfigYaml` | `spec.kubeletConfiguration` (merge с defaults) | Kubelet config файл |

### 4. Go template.Execute

```
template.New("").
    Option("missingkey=error").
    Funcs(funcMap).
    Parse(wgTemplate.Spec.Value)

template.Execute(vars)
```

`missingkey=error` — шаблон обязан иметь значения для всех placeholder'ов. Пропущенная переменная → ошибка рендеринга, ресурсы не создаются.

### 5. Вычисление hash

```
hash = SHA256(rendered_yaml)[:8]
```

Hash считается от **результата рендеринга**, а не от входных переменных. Это значит:
- Изменение переменных → новый rendered YAML → новый hash
- Изменение шаблона → новый rendered YAML → новый hash
- Оба триггера обрабатываются единообразно

**Исключение для KCT:** `__kubeletConfigYaml` подставляется как `""` при вычислении hash. Изменение `spec.kubeletConfiguration` НЕ создаёт новый KCT — обновление происходит in-place.

### 6. Оборачивание в metadata

Оператор добавляет к rendered YAML:

```yaml
metadata:
  name: {clusterName}-{claimName}-bmt-{hash8}   # или -kct-{hash8}
  namespace: {claim.namespace}
  labels:
    cluster.x-k8s.io/cluster-name: {clusterName}
    workergroup.in-cloud.io/claim-name: {claimName}
  ownerReferences:
    - apiVersion: workergroup.in-cloud.io/v1alpha1
      kind: WorkerGroupClaim
      name: {claimName}
      uid: {claimUID}
```

### 7. Сравнение и создание

Если hash отличается от текущего в `status.currentTemplates`:
1. Создать новый immutable ресурс
2. Обновить ссылку в MachineDeployment
3. Старый ресурс → `status.pendingDeletion`
4. Запустить rollout tracking

## Именование ресурсов

```
MachineDeployment:      {clusterName}-{claimName}
BegetMachineTemplate:   {clusterName}-{claimName}-bmt-{hash8}
KubeadmConfigTemplate:  {clusterName}-{claimName}-kct-{hash8}
```

## Что входит в hash

### Hash BMT (infrastructure)

Всё содержимое rendered YAML из `WGMachineTemplate`.

### Hash KCT (bootstrap)

Всё содержимое rendered YAML из `WGBootstrapTemplate`, **кроме** `__kubeletConfigYaml` (подставляется `""`).

### Что НЕ входит ни в один hash

| Поле | Почему | Обновление |
|------|--------|-----------|
| `replicas` | Поле MachineDeployment | In-place MD update |
| `version` | Поле MachineDeployment | In-place MD update |
| `taints` | Поле MachineDeployment | In-place MD update |
| `strategy` | Поле MachineDeployment | In-place MD update |
| `deletion` | Таймауты удаления Machine | In-place MD update |
| `kubeletConfiguration` | In-place KCT update | Обновление без rollout |

## Связанные ресурсы

- [WorkerGroupClaim](worker-group-claim.md) — источник переменных
- [WGMachineTemplate](wg-machine-template.md) — шаблон инфраструктуры
- [WGBootstrapTemplate](wg-bootstrap-template.md) — шаблон bootstrap
