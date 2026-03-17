# API Reference

API Group: `workergroup.in-cloud.io/v1alpha1`

## WorkerGroupClaim

**Scope:** Namespaced

Основной ресурс, описывающий Worker Group. Содержит переменные для рендеринга шаблонов и параметры MachineDeployment.

### Spec

| Поле | Тип | Обязательно | Default | Описание |
|------|-----|:-----------:|---------|----------|
| `clusterName` | `string` | Да | — | Имя ClusterAPI-кластера |
| `replicas` | `*int32` | Нет | `1` | Количество нод |
| `version` | `string` | Да | — | Версия Kubernetes (напр. `v1.30.4`) |
| `machineTemplateRef.name` | `string` | Да | — | Имя `WGMachineTemplate` |
| `bootstrapTemplateRef.name` | `string` | Да | — | Имя `WGBootstrapTemplate` |
| `infrastructure` | `map[string]apiextensionsv1.JSON` | Да | — | Переменные для `WGMachineTemplate` |
| `bootstrap` | `map[string]apiextensionsv1.JSON` | Да | — | Переменные для `WGBootstrapTemplate` |
| `taints` | `[]MachineTaint` | Нет | `[]` | Taints на нодах |
| `nodeLabels` | `map[string]string` | Нет | `{}` | Labels на нодах |
| `strategy` | `MachineDeploymentStrategy` | Нет | `RollingUpdate` | Стратегия rolling update |
| `deletePolicy` | `string` | Нет | — | Порядок удаления: `Random`, `Newest`, `Oldest` |
| `deletion` | `MachineDeletionConfig` | Нет | см. ниже | Таймауты удаления Machine (drain, volume detach, node deletion) |
| `rolloutTimeout` | `*metav1.Duration` | Нет | `30m` | Таймаут rollout |
| `kubeletConfiguration` | `map[string]interface{}` | Нет | `{}` | Переопределение kubelet-настроек |

### MachineTaint

| Поле | Тип | Обязательно | Описание |
|------|-----|:-----------:|----------|
| `key` | `string` | Да | Ключ taint |
| `value` | `string` | Нет | Значение taint |
| `effect` | `corev1.TaintEffect` | Да | `NoSchedule`, `PreferNoSchedule`, `NoExecute` |
| `propagation` | `MachineTaintPropagation` | Нет | `Always` (default) или `OnInitialization` |

### MachineDeletionConfig

Таймауты удаления Machine. Если не указано — применяются дефолты.

| Поле | Тип | Обязательно | Default | Описание |
|------|-----|:-----------:|---------|----------|
| `nodeDrainTimeoutSeconds` | `*int32` | Нет | `60` | Таймаут drain ноды (секунды) |
| `nodeVolumeDetachTimeoutSeconds` | `*int32` | Нет | `60` | Таймаут ожидания detach volumes (секунды) |
| `nodeDeletionTimeoutSeconds` | `*int32` | Нет | `120` | Таймаут удаления Node объекта (секунды) |

### MachineDeploymentStrategy

| Поле | Тип | Обязательно | Описание |
|------|-----|:-----------:|----------|
| `type` | `string` | Нет | `RollingUpdate` (default) или `OnDelete` |
| `rollingUpdate.maxSurge` | `intOrString` | Нет | Max нод сверх replicas при update |
| `rollingUpdate.maxUnavailable` | `intOrString` | Нет | Max недоступных нод при update |

### Status

| Поле | Тип | Описание |
|------|-----|----------|
| `phase` | `string` | `Provisioning`, `Ready`, `Updating`, `Degraded`, `Deleting`, `Failed`, `Paused` |
| `observedGeneration` | `int64` | Последнее обработанное `metadata.generation` |
| `currentTemplates.begetMachineTemplate` | `string` | Имя текущего BMT |
| `currentTemplates.kubeadmConfigTemplate` | `string` | Имя текущего KCT |
| `lastRendered.infrastructureHash` | `string` | Hash текущего rendered BMT |
| `lastRendered.bootstrapHash` | `string` | Hash текущего rendered KCT |
| `lastRendered.errors` | `[]string` | Ошибки последнего рендеринга |
| `pendingDeletion` | `[]ResourceRef` | Старые шаблоны, ожидающие удаления |
| `rolloutStartedAt` | `*metav1.Time` | Время начала текущего rollout |
| `machineDeploymentStatus.name` | `string` | Имя MachineDeployment |
| `machineDeploymentStatus.replicas` | `*int32` | Желаемое количество реплик |
| `machineDeploymentStatus.readyReplicas` | `*int32` | Готовые реплики |
| `machineDeploymentStatus.upToDateReplicas` | `*int32` | Реплики на актуальной версии |
| `conditions` | `[]metav1.Condition` | Стандартные conditions |

### Conditions

| Type | Описание |
|------|----------|
| `Ready` | Все ресурсы синхронизированы, rollout завершён |
| `TemplatesRendered` | Оба шаблона успешно отрендерены |
| `RolloutComplete` | `upToDateReplicas == replicas` |
| `RolloutTimedOut` | Rollout превысил `rolloutTimeout` |
| `Paused` | Reconcile приостановлен аннотацией |

### Annotations

| Annotation | Значение | Описание |
|-----------|---------|----------|
| `workergroup.in-cloud.io/paused` | `"true"` | Приостановить reconcile для этого Claim |

### Labels (на создаваемых ресурсах)

| Label | Значение | На каких ресурсах |
|-------|---------|-------------------|
| `cluster.x-k8s.io/cluster-name` | `spec.clusterName` | BMT, KCT, MD |
| `workergroup.in-cloud.io/claim-name` | Имя Claim | BMT, KCT, MD |
| `cluster.x-k8s.io/deployment-name` | `{clusterName}-{claimName}` | MD |

---

## WGMachineTemplate

**Scope:** Cluster

Go-template для генерации `BegetMachineTemplate`.

### Spec

| Поле | Тип | Обязательно | Описание |
|------|-----|:-----------:|----------|
| `value` | `string` | Да | Go-template, генерирующий YAML `BegetMachineTemplate` (без metadata) |

### Пример

```yaml
apiVersion: workergroup.in-cloud.io/v1alpha1
kind: WGMachineTemplate
metadata:
  name: default-machine
spec:
  value: |
    apiVersion: infrastructure.cluster.x-k8s.io/v1beta2
    kind: BegetMachineTemplate
    spec:
      template:
        spec:
          configuration:
            cpuCount: {{ .cpuCount }}
            diskSize: {{ .diskSize }}
            memory: {{ .memory }}
          image: "{{ .image }}"
```

---

## WGBootstrapTemplate

**Scope:** Cluster

Go-template для генерации `KubeadmConfigTemplate`.

### Spec

| Поле | Тип | Обязательно | Описание |
|------|-----|:-----------:|----------|
| `value` | `string` | Да | Go-template, генерирующий YAML `KubeadmConfigTemplate` (без metadata) |

### Автоматически инжектируемые переменные

Доступны в шаблоне, но не указываются в `WorkerGroupClaim.spec.bootstrap`:

| Переменная | Тип | Описание |
|-----------|-----|----------|
| `nodeLabels` | `string` | `"k1=v1,k2=v2"` (sorted). Из `spec.nodeLabels` |
| `machineDeploymentName` | `string` | `{clusterName}-{claimName}` |
| `__kubeletConfigYaml` | `string` | YAML kubelet config. Из `spec.kubeletConfiguration` |

---

## Template-функции

Доступны в шаблонах помимо стандартных Go template функций:

| Функция | Описание | Пример | Результат |
|---------|----------|--------|-----------|
| `toYaml` | Сериализация в YAML | `{{ .list \| toYaml }}` | `- item1\n- item2` |
| `indent N` | Отступ N пробелов | `{{ .block \| indent 8 }}` | Каждая строка с отступом |
| `quote` | Оборачивание в кавычки | `{{ .val \| quote }}` | `"value"` |
| `default` | Значение по умолчанию | `{{ .val \| default "fb" }}` | `fb` если пусто |
| `b64enc` | Base64-кодирование | `{{ .cert \| b64enc }}` | Base64 строка |

## Именование ресурсов

```
MachineDeployment:      {clusterName}-{claimName}
BegetMachineTemplate:   {clusterName}-{claimName}-bmt-{SHA256[:8]}
KubeadmConfigTemplate:  {clusterName}-{claimName}-kct-{SHA256[:8]}
```

Hash вычисляется от rendered YAML. Для KCT `__kubeletConfigYaml` подставляется как `""` при вычислении hash.
