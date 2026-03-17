# WGBootstrapTemplate

## Обзор

- Cluster-scoped ресурс, содержащий Go-template для генерации `KubeadmConfigTemplate`
- Один шаблон может использоваться множеством `WorkerGroupClaim` из разных namespace
- Шаблон содержит placeholder'ы `{{ .переменная }}`, которые заполняются из `WorkerGroupClaim.spec.bootstrap`
- Оператор автоматически инжектит дополнительные переменные (`nodeLabels`, `machineDeploymentName`, `__kubeletConfigYaml`) перед рендерингом

## Поля Spec

| Поле | Тип | Обязательно | Описание |
|------|-----|:-----------:|----------|
| `value` | `string` | Да | Go-template, генерирующий YAML `KubeadmConfigTemplate` (без metadata) |

## Пример

```yaml
apiVersion: workergroup.in-cloud.io/v1alpha1
kind: WGBootstrapTemplate
metadata:
  name: default-bootstrap
spec:
  value: |
    apiVersion: bootstrap.cluster.x-k8s.io/v1beta2
    kind: KubeadmConfigTemplate
    spec:
      template:
        spec:
          format: cloud-config
          files:
            - path: /etc/containerd/config.toml
              owner: root:root
              permissions: "0644"
              content: |
                version = 2
                imports = ["/etc/containerd/conf.d/*.toml"]

            - path: /etc/default/containerd/download.env
              owner: root:root
              permissions: "0644"
              content: |
                COMPONENT_VERSION="{{ .containerdVersion }}"

            - path: /etc/default/kubelet/extra-args.env
              owner: root:root
              permissions: "0644"
              content: |
                KUBELET_EXTRA_ARGS="--provider-id=beget:///{{ "{{" }} ds.meta_data.instance_id.split(':')[0] {{ "}}" }}"

          joinConfiguration:
            nodeRegistration:
              kubeletExtraArgs:
                - name: node-labels
                  value: "{{ .nodeLabels }}"
              taints: []
```

## Автоматически инжектируемые переменные

Оператор добавляет эти переменные в bootstrap vars **до** рендеринга шаблона. Их не нужно указывать в `WorkerGroupClaim.spec.bootstrap`.

| Переменная | Значение | Назначение |
|-----------|---------|-----------|
| `nodeLabels` | `"k1=v1,k2=v2"` (sorted) | Kubelet `--node-labels`. Конвертируется из `spec.nodeLabels` (map) |
| `machineDeploymentName` | `{clusterName}-{claimName}` | Системный label `node-group.beget.com/name` в cloud-init |
| `__kubeletConfigYaml` | YAML kubelet config | Kubelet config файл для cloud-init. Мержится из `spec.kubeletConfiguration` с defaults |

## Экранирование cloud-init Jinja

KubeadmConfigTemplate может содержать cloud-init Jinja-шаблоны, которые НЕ должны интерпретироваться Go template engine:

```yaml
# В шаблоне:
content: |
  KUBELET_EXTRA_ARGS="--provider-id=beget:///{{ "{{" }} ds.meta_data.instance_id.split(':')[0] {{ "}}" }}"

# После рендеринга:
content: |
  KUBELET_EXTRA_ARGS="--provider-id=beget:///{{ ds.meta_data.instance_id.split(':')[0] }}"
```

## Лучшие практики

1. **Не включайте `metadata` в шаблон.** Оператор добавит сам
2. **Используйте `nodeLabels` из авто-инжекта**, а не добавляйте в `spec.bootstrap` вручную
3. **Экранируйте Jinja** через `{{ "{{" }}` для cloud-init выражений

## Связанные ресурсы

- [WorkerGroupClaim](worker-group-claim.md) — ресурс, предоставляющий переменные
- [WGMachineTemplate](wg-machine-template.md) — аналогичный шаблон для инфраструктуры
- [Пайплайн рендеринга](rendering-pipeline.md) — детали процесса рендеринга
