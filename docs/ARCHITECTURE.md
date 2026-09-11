# bazUSOP mimarisi

## Bileşenler

- **Hub:** Go modular monolith; REST API’yi ve gömülü React uygulamasını aynı
  binary’den sunar.
- **Agent:** Linux/Windows hostunda çalışır, tüm bağlantıları dışarı doğru başlatır.
- **PostgreSQL:** agent inventory ve diğer ilişkisel kontrol düzlemi verisinin
  kaynağıdır.
- **TimescaleDB:** telemetry örneklerini PostgreSQL uyumlu hypertable üzerinde
  saklar; etkin değilse aynı şema normal PostgreSQL tablosu olarak çalışır.
- **React UI:** inventory ve telemetry verisini hub API’sinden okuyan operasyon
  yüzeyidir.

## Veri akışı

1. Agent tek kullanımlık token ve CSR ile enrollment ister.
2. Hub CSR’ı imzalar ve SPIFFE URI SAN taşıyan kısa ömürlü client sertifikası verir.
3. Agent sonraki yazma isteklerini mTLS ile yapar; agent ID gövdeden değil
   sertifikadan çıkarılır.
4. Inventory raporu PostgreSQL’de agent ID üzerinden upsert edilir.
5. Telemetry örneği `(agent_id, recorded_at)` anahtarıyla idempotent yazılır.
6. Servis snapshot'ı ortak durum modeline çevrilir ve önceki snapshot'ı transaction
   içinde atomik olarak değiştirir.
7. UI filo, zaman serisi ve servis listesini salt-okunur API uçlarından alır.

## Saklama modeli

`hosts` tablosu agent başına tek normalize edilmiş kayıt tutar. `telemetry_samples`
tablosunun primary key’i agent ve timestamp bileşimidir. Timescale etkinleştirildiğinde
`recorded_at` partition anahtarıyla hypertable’a dönüştürülür ve 30 günlük retention
policy uygulanır.

`services` tablosu `(agent_id, name)` anahtarıyla son bilinen snapshot'ı tutar.
Snapshot yenilenirken aynı agent'ın eski satırları ve yeni satırları tek transaction
içinde değiştirilir; okuyucu kısmi liste görmez. State ve startup type alanları
systemd ile Windows Service Manager farklarını ortak modele indirger.

## Ölçekleme

Inventory ve telemetry handler’ları süreç durumu taşımaz; ortak PostgreSQL/
TimescaleDB kullanan hub replikalarında yatay ölçeklenebilir. Helm chart HPA,
topology spread, rolling update ve PDB tanımlar.

Enrollment CA private key’i ve tüketilmiş bootstrap-token durumu henüz süreç
içindedir. Bu yüzden varsayılan chart tek replika çalışır ve HPA kapalıdır. Güvenli
çoklu replika enrollment için CA’nın secret/KMS üzerinden ortak yüklenmesi ve token
tüketiminin PostgreSQL transaction’ıyla atomik yapılması gerekir.

## Güven sınırları

- Uzak enrollment plaintext HTTP üzerinden reddedilir.
- Agent yazma uçları doğrulanmış mTLS client sertifikası ister.
- JSON gövdeleri 1 MiB ile sınırlıdır ve bilinmeyen alanlar reddedilir.
- Container non-root ve read-only root filesystem ile çalışmaya uygundur.
- Kubernetes ServiceAccount token’ı varsayılan olarak pod’a bağlanmaz.
