# bazUSOP Helm chart

Chart'ı kurmadan önce uygulama Secret'ını oluşturun:

```bash
kubectl create namespace bazusop
kubectl -n bazusop create secret generic bazusop-secrets \
  --from-literal=database-url='postgres://user:password@postgres.example/bazusop' \
  --from-literal=enrollment-token='rastgele-tek-kullanimlik-guclu-bir-secret' \
  --from-literal=bootstrap-secret='rastgele-tek-kullanimlik-guclu-bir-secret-2' \
  --from-literal=totp-encryption-key='rastgele-guclu-bir-sifreleme-anahtari' \
  --from-literal=service-account-pepper='rastgele-guclu-bir-pepper'
helm upgrade --install bazusop . --namespace bazusop
```

`bootstrap-secret`, `totp-encryption-key` ve `service-account-pepper` isteğe
bağlıdır, ama üçü de tanımlanmazsa mutasyon içeren hiçbir API isteği kimlik
doğrulanamaz — bkz. [../../../docs/MIGRATION_v0.4.md](../../../docs/MIGRATION_v0.4.md).

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
Hub başlangıçta hypertable'ları ve retention policy'lerini kurar. Varsayılan
telemetri saklama süresi 30, log saklama süresi 14 gündür; örneğin:

```bash
helm upgrade --install bazusop . --namespace bazusop \
  --set timescale.enabled=true \
  --set retention.telemetryRetentionDays=90 \
  --set retention.logRetentionDays=21
```
