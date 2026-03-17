# Создание шаблонов

Шаблоны — cluster-scoped ресурсы, определяющие структуру целевых ClusterAPI-ресурсов. Один шаблон используется множеством `WorkerGroupClaim` из разных namespace.

## WGMachineTemplate

Определяет структуру `BegetMachineTemplate`. Placeholder'ы `{{ .переменная }}` заполняются из `WorkerGroupClaim.spec.infrastructure`.

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

### Работа с массивами

Для массивов используйте `toYaml`:

```yaml
spec:
  value: |
    apiVersion: infrastructure.cluster.x-k8s.io/v1beta2
    kind: BegetMachineTemplate
    spec:
      template:
        spec:
          sshKeyIds:
          {{ .sshKeyIds | toYaml | indent 10 }}
```

В `WorkerGroupClaim`:

```yaml
infrastructure:
  sshKeyIds:
    - 12345
    - 67890
```

## WGBootstrapTemplate

Определяет структуру `KubeadmConfigTemplate`. Placeholder'ы заполняются из `WorkerGroupClaim.spec.bootstrap` плюс [автоматически инжектируемые переменные](../concepts/wg-bootstrap-template.md#автоматически-инжектируемые-переменные).

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
            - path: /etc/default/containerd/download.env
              owner: root:root
              permissions: "0644"
              content: |
                COMPONENT_VERSION="{{ .containerdVersion }}"

          joinConfiguration:
            nodeRegistration:
              kubeletExtraArgs:
                - name: node-labels
                  value: "{{ .nodeLabels }}"
              taints: []
```

### Экранирование cloud-init Jinja

KubeadmConfigTemplate часто содержит cloud-init Jinja-выражения. Чтобы Go template engine их не интерпретировал:

```yaml
# Так пишем в шаблоне:
content: |
  KUBELET_EXTRA_ARGS="--provider-id=beget:///{{ "{{" }} ds.meta_data.instance_id.split(':')[0] {{ "}}" }}"

# Результат после рендеринга:
content: |
  KUBELET_EXTRA_ARGS="--provider-id=beget:///{{ ds.meta_data.instance_id.split(':')[0] }}"
```

## Правила написания шаблонов

1. **Не включайте `metadata`** — оператор добавляет `name`, `namespace`, `labels`, `ownerReferences` автоматически

2. **Все переменные обязательны** — шаблон рендерится с `missingkey=error`. Если в Claim не указана переменная, которая используется в шаблоне, рендеринг завершится ошибкой

3. **Доступные функции**: `toYaml`, `indent`, `quote`, `default`, `b64enc` — помимо стандартных Go template

4. **Статические значения оставляйте как есть** — не нужно шаблонизировать значения, которые одинаковы для всех Claim

## Применение

```bash
kubectl apply -f wg-machine-template.yaml
kubectl apply -f wg-bootstrap-template.yaml

# Проверка
kubectl get wgmachinetemplates
kubectl get wgbootstraptemplates
```

## Обновление шаблона

При обновлении шаблона оператор автоматически:
1. Находит все `WorkerGroupClaim`, ссылающиеся на шаблон
2. Пересчитывает rendered YAML → новый hash → новый ресурс
3. Обновляет ссылки в MachineDeployment → rollout

> **Внимание:** обновление шаблона триггерит rollout **всех** Claim'ов, использующих этот шаблон.

## Следующий шаг

- [Развёртывание Worker Group](deploying-worker-groups.md)
