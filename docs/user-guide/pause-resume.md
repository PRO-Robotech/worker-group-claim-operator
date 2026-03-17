# Pause / Resume

Аннотация для приостановки reconcile отдельного `WorkerGroupClaim`.

## Включение паузы

```bash
kubectl annotate workergroupclaim <name> -n <ns> \
  workergroup.in-cloud.io/paused=true
```

## Снятие паузы

```bash
kubectl annotate workergroupclaim <name> -n <ns> \
  workergroup.in-cloud.io/paused-
```

## Поведение при паузе

- Оператор **пропускает** весь reconcile loop для этого Claim
- Никакие ресурсы не создаются, не обновляются, не удаляются
- `pendingDeletion` не обрабатывается — старые шаблоны сохраняются
- `phase: Paused`
- Condition `Paused=True`
- Event `Normal` / `Paused`

## Поведение при снятии паузы

- Оператор выполняет полный reconcile: загрузка шаблонов → рендеринг → сравнение hash
- Если за время паузы переменные или шаблоны изменились → создание новых ресурсов, rollout
- Обрабатываются накопленные `pendingDeletion`
- Event `Normal` / `Resumed`

## Сценарии использования

### Batch-обновление

Изменение нескольких переменных одним rollout вместо нескольких:

```bash
# 1. Пауза
kubectl annotate workergroupclaim general-pool -n my-cluster-ns \
  workergroup.in-cloud.io/paused=true

# 2. Несколько изменений
kubectl patch workergroupclaim general-pool -n my-cluster-ns \
  --type merge -p '{"spec":{"infrastructure":{"memory": 8192, "cpuCount": 8}}}'

# 3. Снятие паузы → один rollout
kubectl annotate workergroupclaim general-pool -n my-cluster-ns \
  workergroup.in-cloud.io/paused-
```

### Экстренная остановка

При проблемах с rollout — пауза предотвращает дальнейшие изменения:

```bash
kubectl annotate workergroupclaim general-pool -n my-cluster-ns \
  workergroup.in-cloud.io/paused=true
```

### Контролируемое обновление шаблона

При обновлении shared-шаблона — пауза на конкретных Claim'ах для поэтапного rollout:

```bash
# Пауза на всех Claim'ах кроме canary
kubectl get workergroupclaim -A -o name | \
  grep -v canary | \
  xargs -I{} kubectl annotate {} workergroup.in-cloud.io/paused=true

# Обновить шаблон — rollout только на canary
kubectl apply -f wg-bootstrap-template.yaml

# После проверки — снять паузу с остальных
```
