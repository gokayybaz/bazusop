# bazUSOP Helm chart

Create the application secret before installing the chart:

```bash
kubectl create namespace bazusop
kubectl -n bazusop create secret generic bazusop-secrets \
  --from-literal=database-url='postgres://user:password@postgres.example/bazusop' \
  --from-literal=enrollment-token='replace-with-a-random-one-time-secret'
helm upgrade --install bazusop . --namespace bazusop
```

For direct hub TLS and agent mTLS, create a TLS secret and enable the chart's TLS
mount:

```bash
kubectl -n bazusop create secret tls bazusop-tls --cert=hub.crt --key=hub.key
helm upgrade --install bazusop . --namespace bazusop \
  --set tls.enabled=true --set tls.existingSecret=bazusop-tls
```

Keep `autoscaling.enabled=false` and one replica until enrollment CA and consumed
bootstrap-token state are shared across replicas. Inventory endpoints are already
stateless when `DATABASE_URL` points to PostgreSQL.

Set `timescale.enabled=true` when the configured database provides the TimescaleDB
extension. The hub then creates the telemetry hypertable and applies a 30-day
retention policy during startup.
