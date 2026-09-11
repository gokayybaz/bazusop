# bazUSOP Helm chart

Chart'ı kurmadan önce uygulama Secret'ını oluşturun:

```bash
kubectl create namespace bazusop
kubectl -n bazusop create secret generic bazusop-secrets \
  --from-literal=database-url='postgres://user:password@postgres.example/bazusop' \
  --from-literal=enrollment-token='rastgele-tek-kullanimlik-guclu-bir-secret' \
  --from-literal=operator-token='rastgele-guclu-bir-operator-secret'
helm upgrade --install bazusop . --namespace bazusop
```

Doğrudan hub TLS'i ve agent mTLS'i için bir TLS Secret oluşturup mount'u
etkinleştirin:

```bash
kubectl -n bazusop create secret tls bazusop-tls --cert=hub.crt --key=hub.key
helm upgrade --install bazusop . --namespace bazusop \
  --set tls.enabled=true --set tls.existingSecret=bazusop-tls
```

Enrollment CA ve tüketilmiş bootstrap-token durumu replikalar arasında
paylaşılana kadar `autoscaling.enabled=false` ve tek replika kullanın.
`DATABASE_URL` PostgreSQL'e işaret ettiğinde envanter ve telemetri handler'ları
süreç durumu taşımaz.

Veritabanı TimescaleDB extension'ı sağlıyorsa `timescale.enabled=true` ayarlayın.
Hub başlangıçta telemetri hypertable'ını ve 30 günlük retention policy'yi kurar.
