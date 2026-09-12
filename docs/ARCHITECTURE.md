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
7. Log batch'i kalıcı depoya toplu yazıldıktan sonra agent'a özel süreç içi SSE
   broker'ına yayınlanır.
8. UI filo, zaman serisi, servis listesi ve logları salt-okunur API uçlarından alır.
9. Operatör bearer token ile izinli bir restart/reboot talebi oluşturur; hub işin
   değişmez alanlarını Ed25519 ile imzalayıp `queued` olarak saklar.
10. Hedef agent işi mTLS ile atomik teslim alır, çıktıyı ve terminal durumu artan
    sequence numaralı audit olayları olarak raporlar.
11. Kabul edilen telemetri örneği etkin metrik kurallarıyla, hub'ın periyodik
    taraması host son-görülme zamanını erişilebilirlik kurallarıyla değerlendirir.
12. İhlal benzersiz aktif olay açar; recovery otomatik çözer, operatör onayı ve
    tüm geçişler olay geçmişine eklenir. Aktif bakım penceresi yeni açılışı bastırır.
13. AWS, Azure veya GCP connector'ı provider instance snapshot'ını gönderir; hub
    doğrulanmış provider agent kimliğini otomatik eşleştirir, hostname/IP sinyalini
    yalnız operatör inceleme adayı olarak işaretler.

## Saklama modeli

`hosts` tablosu agent başına tek normalize edilmiş kayıt tutar. `telemetry_samples`
tablosunun primary key’i agent ve timestamp bileşimidir. Timescale etkinleştirildiğinde
`recorded_at` partition anahtarıyla hypertable’a dönüştürülür ve 30 günlük retention
policy uygulanır.

`services` tablosu `(agent_id, name)` anahtarıyla son bilinen snapshot'ı tutar.
Snapshot yenilenirken aynı agent'ın eski satırları ve yeni satırları tek transaction
içinde değiştirilir; okuyucu kısmi liste görmez. State ve startup type alanları
systemd ile Windows Service Manager farklarını ortak modele indirger.

`log_entries` tablosu `(id, occurred_at)` bileşik anahtarıyla Timescale hypertable
olarak çalışır; agent/zaman ve agent/önem/zaman indeksleri sınırlı geçmiş aramayı
destekler. Timescale etkinse loglar için 14 günlük retention policy uygulanır.
Geliştirme amaçlı memory store agent başına en yeni 10.000 kaydı tutar.

`jobs` tablosu imzalı komutu, son sequence değerini ve yaşam döngüsü durumunu;
`job_events` tablosu onay, teslim, çıktı ve sonucu append-only audit izi olarak
tutar. PostgreSQL `FOR UPDATE SKIP LOCKED`, aynı işin eşzamanlı agent poll'larında
yalnız bir kez teslim edilmesini sağlar. Terminal işler yeniden açılamaz.

`alert_rules` metrik ve erişilebilirlik eşiklerini, `maintenance_windows` zamanlı
bastırmayı, `alert_incidents` güncel yaşam döngüsünü ve `alert_events` append-only
geçiş geçmişini tutar. Kısmi unique indeks, aynı kural/agent çifti için eşzamanlı
yalnız bir aktif olay bulunmasını sağlar.

`cloud_accounts` provider ve harici hesap kimliğini; `cloud_instances` ise her
hesabın son atomik keşif snapshot'ını tutar. Otomatik eşleşme yalnız connector'ın
provider metadata'sından çıkardığı `agent_id_hint` mevcut bir agent kimliğine tam
uyduğunda yapılır. Hostname veya özel IP ile bulunan tekil benzerlik
`candidate_agent_id` olarak saklanır ve agent ilişkisi kurulmaz.

Canlı tail broker'ı hub sürecindedir. Bu nedenle birden fazla hub replikasında SSE
istemcisi yalnız bağlandığı replikanın aldığı yeni kayıtları görür. Production
yatay ölçekleme öncesinde PostgreSQL LISTEN/NOTIFY, NATS veya eşdeğer ortak event
bus eklenmelidir; geçmiş arama tüm replikalarda ortak PostgreSQL'den gelir.

## Ölçekleme

Inventory, telemetry, servis ve geçmiş log handler'ları süreç durumu taşımaz;
ortak PostgreSQL/TimescaleDB kullanan hub replikalarında yatay ölçeklenebilir.
Canlı SSE yayınında yukarıdaki event bus kısıtı geçerlidir. Helm chart HPA,
topology spread, rolling update ve PDB tanımlar.

Erişilebilirlik değerlendirmesi şu anda her hub replikasında çalışabilir; veritabanı
unique kısıtı çift aktif olayı engeller. Çok büyük filolarda tarama işi leader
election veya ayrı scheduler/queue bileşenine taşınmalıdır.

Enrollment CA private key’i ve tüketilmiş bootstrap-token durumu henüz süreç
içindedir. Bu yüzden varsayılan chart tek replika çalışır ve HPA kapalıdır. Güvenli
çoklu replika enrollment için CA’nın secret/KMS üzerinden ortak yüklenmesi ve token
tüketiminin PostgreSQL transaction’ıyla atomik yapılması gerekir.

İş imzalama anahtarı da şu anda hub başlangıcında süreç içinde üretilir ve restart
sonrası değişir. Kalıcı güven kökü ve çoklu replika için private key KMS/Secret'ta
saklanmalı, public key güvenli enrollment/config kanalından agent'a sabitlenmelidir.

## Güven sınırları

- Uzak enrollment plaintext HTTP üzerinden reddedilir.
- Agent yazma uçları doğrulanmış mTLS client sertifikası ister.
- Operasyon oluşturma ayrı bir bearer secret ister; secret yoksa uç kapalıdır.
- Aksiyon allowlist'i yalnız servis restart ve host reboot'u kabul eder; keyfi shell
  çalıştırma desteklenmez.
- Alarm kuralı, bakım penceresi ve olay onayı operatör bearer secret'ıyla korunur.
- Bulut hesabı oluşturma ve provider snapshot yazma operatör bearer secret'ıyla korunur.
- JSON gövdeleri 1 MiB ile sınırlıdır ve bilinmeyen alanlar reddedilir.
- Container non-root ve read-only root filesystem ile çalışmaya uygundur.
- Kubernetes ServiceAccount token’ı varsayılan olarak pod’a bağlanmaz.

Mevcut bearer token kimlik doğrulaması ilk güvenli dikey dilimdir;
`approved_by` alanı token sahibinin beyanıdır. Kullanıcı bazlı RBAC, SSO/MFA,
çift onay ve immutable harici audit sink production yetkilendirme sertleştirmesi
olarak ayrıca ele alınacaktır.
