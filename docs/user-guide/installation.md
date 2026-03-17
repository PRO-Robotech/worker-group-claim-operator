# Установка

## Предварительные требования

- Kubernetes кластер (management cluster)
- [ClusterAPI](https://cluster-api.sigs.k8s.io/) v1beta2 (>= v1.12)
- [Beget Infrastructure Provider](https://github.com/beget) установлен
- `kubectl` с доступом cluster-admin

## Установка CRD

```bash
kubectl apply -f config/crd/bases/
```

Проверка CRD:

```bash
kubectl get crd | grep workergroup
```

Ожидаемый результат:

```
wgbootstraptemplates.workergroup.in-cloud.io
wgmachinetemplates.workergroup.in-cloud.io
workergroupclaims.workergroup.in-cloud.io
```

## Установка RBAC и контроллера

```bash
kubectl apply -f config/rbac/
kubectl apply -f config/manager/
```

## Проверка

```bash
kubectl get pods -n worker-group-system
```

Контроллер должен быть в статусе `Running`.

```bash
kubectl logs -n worker-group-system deployment/worker-group-controller-manager -f
```

## Обновление

Для обновления оператора повторите те же команды — CRD и контроллер будут обновлены:

```bash
kubectl apply -f config/crd/bases/
kubectl apply -f config/rbac/
kubectl apply -f config/manager/
```

## Удаление

```bash
kubectl delete -f config/manager/
kubectl delete -f config/rbac/
kubectl delete -f config/crd/bases/
```

> **Внимание:** удаление CRD удалит все `WorkerGroupClaim`, `WGMachineTemplate` и `WGBootstrapTemplate`. Убедитесь, что это намеренное действие.

## Следующий шаг

- [Создание шаблонов](creating-templates.md)
