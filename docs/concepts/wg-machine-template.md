# WGMachineTemplate

## Обзор

- Cluster-scoped ресурс, содержащий Go-template для генерации `BegetMachineTemplate`
- Один шаблон может использоваться множеством `WorkerGroupClaim` из разных namespace
- Шаблон содержит placeholder'ы `{{ .переменная }}`, которые заполняются из `WorkerGroupClaim.spec.infrastructure`
- При изменении шаблона оператор автоматически пересчитывает все Claim'ы, ссылающиеся на него

## Поля Spec

| Поле | Тип | Обязательно | Описание |
|------|-----|:-----------:|----------|
| `value` | `string` | Да | Go-template, генерирующий YAML `BegetMachineTemplate` (без metadata) |

## Пример

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
          managedBy: system
          networkTag: "{{ .networkTag }}"
          providerID: ""
          serverName: ""
          usePrivateNetwork: {{ .usePrivateNetwork }}
```

## Что происходит при изменении шаблона

1. Оператор через watch обнаруживает изменение `WGMachineTemplate`
2. Находит все `WorkerGroupClaim`, ссылающиеся на этот шаблон
3. Для каждого Claim: пересчитывает rendered YAML → новый hash → создаёт новый `BegetMachineTemplate`
4. Обновляет ссылку в `MachineDeployment`, запускает rollout

## Доступные template-функции

Помимо стандартных Go template функций:

| Функция | Описание | Пример |
|---------|----------|--------|
| `toYaml` | Сериализует значение в YAML | `{{ .sshKeyIds \| toYaml }}` |
| `indent N` | Добавляет отступ в N пробелов | `{{ .block \| indent 8 }}` |
| `quote` | Оборачивает в кавычки | `{{ .val \| quote }}` |
| `default` | Значение по умолчанию | `{{ .val \| default "fallback" }}` |
| `b64enc` | Base64-кодирование | `{{ .cert \| b64enc }}` |

## Лучшие практики

1. **Не включайте `metadata` в шаблон.** Оператор сам добавляет `name`, `namespace`, `labels`, `ownerReferences`
2. **Используйте нативные типы.** `cpuCount: 4` (число), не `cpuCount: "4"` (строка) — шаблон получит правильный тип
3. **Для массивов используйте `toYaml`.** Например, `sshKeyIds: {{ .sshKeyIds | toYaml }}` корректно рендерит `[]int64`

## Связанные ресурсы

- [WorkerGroupClaim](worker-group-claim.md) — ресурс, предоставляющий переменные
- [WGBootstrapTemplate](wg-bootstrap-template.md) — аналогичный шаблон для bootstrap
- [Пайплайн рендеринга](rendering-pipeline.md) — детали процесса рендеринга
