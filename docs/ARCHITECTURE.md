# bazUSOP mimarisi

## Bileşenler

- **Hub:** Go modular monolith; REST API’yi ve gömülü React uygulamasını aynı
  binary’den sunar.
- **Agent:** Ayrı Go binary'si olarak Linux/Windows hostunda çalışır, tüm
  bağlantıları dışarı doğru başlatır. Ed25519 private key ve kısa ömürlü mTLS
  sertifikasını yerel state dizininde korur; envanteri başlangıçta ve periyodik gönderir.
- **PostgreSQL:** agent inventory ve diğer ilişkisel kontrol düzlemi verisinin
  kaynağıdır.
- **TimescaleDB:** telemetry örneklerini PostgreSQL uyumlu hypertable üzerinde
  saklar; etkin değilse aynı şema normal PostgreSQL tablosu olarak çalışır.
- **React UI:** inventory ve telemetry verisini hub API’sinden okuyan operasyon
  yüzeyidir.

## Build ve sürüm kimliği

Hub ve agent'ın sürüm, Git commit ve UTC build tarihi derleme sırasında linker alanlarına
yazılır. Aynı kimlik `--version` CLI çıktısından ve secret içermeyen runtime API
üzerinden okunur; böylece operatör indirilen arşiv ile çalışan pod/binary'yi
karşılaştırabilir. Geliştirme build'leri açıkça `dev/unknown` kimliği taşır.

Release hattı gömülü React çıktısını bir kez üretir ve Go'nun cross-compile
desteğiyle Linux amd64/arm64 ile Windows amd64 hub ve agent binary'lerini oluşturur.
Dağıtım birimi bileşen başına sürümlü arşiv ve ortak SHA-256 manifestidir. Linux deb/rpm paketleri hub binary'si,
systemd unit ve kontrollü yükseltme aracını aynı sürüm biriminde taşır. Windows
MSI major-upgrade sözleşmesiyle eski sürümü yerinde değiştirir ve downgrade'i
engeller; hub Windows Service protokolünü uygulayana kadar yalnız binary kurulumu
yapar.

Tag hattı checksum manifestini ve GHCR image digest'ini GitHub OIDC kimliğiyle
Sigstore Cosign üzerinden keyless imzalar. İmzalı manifest arşiv ve native paket
özetlerinin güven köküdür. Linux yükseltme aracı aday binary'nin SHA-256 ve build
kimliğini hedefe dokunmadan doğrular, tek yükseltme kilidi alır, önceki binary'yi
saklar ve systemd restart sonrası sağlık ucu başarısızsa atomik rollback uygular.

## Veri akışı

1. Agent tek kullanımlık token ve CSR ile enrollment ister.
2. Hub CSR’ı imzalar ve SPIFFE URI SAN taşıyan kısa ömürlü client sertifikası verir.
3. Agent sonraki yazma isteklerini mTLS ile yapar; agent ID gövdeden değil
   sertifikadan çıkarılır.
4. Inventory raporu PostgreSQL’de agent ID üzerinden upsert edilir.
5. Telemetry örneği `(agent_id, recorded_at)` anahtarıyla idempotent yazılır.
6. Servis snapshot'ı ortak durum modeline çevrilir ve önceki snapshot'ı transaction
   içinde atomik olarak değiştirir.
7. Log batch'i ve PostgreSQL notification sinyali atomik commit edilir; her hub
   replikası kalıcı kayıtları agent'a özel SSE abonelerine yayınlar.
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
`recorded_at` partition anahtarıyla hypertable’a dönüştürülür. Telemetri ve log
retention değerleri gün cinsinden yapılandırılır; varsayılanlar sırasıyla 30 ve
14 gündür. Hub, mevcut policy'leri advisory lock altında kaldırıp güncel değerlerle
yeniden kurduğu için rolling başlangıçlarda replikalar birbiriyle yarışmaz.

`services` tablosu `(agent_id, name)` anahtarıyla son bilinen snapshot'ı tutar.
Snapshot yenilenirken aynı agent'ın eski satırları ve yeni satırları tek transaction
içinde değiştirilir; okuyucu kısmi liste görmez. State ve startup type alanları
systemd ile Windows Service Manager farklarını ortak modele indirger.

`log_entries` tablosu `(id, occurred_at)` bileşik anahtarıyla Timescale hypertable
olarak çalışır; agent/zaman ve agent/önem/zaman indeksleri sınırlı geçmiş aramayı
destekler. Timescale etkinse loglar için varsayılan 14 günlük, yapılandırılabilir
retention policy uygulanır. Geliştirme amaçlı memory store agent başına en yeni
10.000 kaydı tutar.

Canlı broker tamponu bir maksimum ingest batch'ini, yani 1000 kaydı taşır. CI yük
kabulü tek batch'i 32 eşzamanlı aboneye bir saniye altında kayıpsız dağıtmayı
doğrular; bu üretim kapasite tahmini değil regresyon sınırıdır.

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

PostgreSQL store, log insert'i ve `pg_notify` sinyalini aynı transaction içinde
commit eder. Bildirimler PostgreSQL'in 8 KiB sınırının altında kimlik gruplarına
bölünür; hassas veya büyük log mesajı notification payload'ına girmez. Her hub
replikası tek bir `LISTEN` bağlantısıyla sinyali alır, kalıcı satırları PostgreSQL'den
okur ve agent bazlı yerel SSE abonelerine dağıtır. Dinleyici bağlantısı koparsa
otomatik yeniden kurulur. Notification kalıcı kuyruk değildir; kısa bağlantı
kesintisinde istemci geçmiş aramayla arayı kapatmalıdır. Bellek store'u geliştirme
modunda süreç içi broker kullanmaya devam eder.

## Ölçekleme

Inventory, telemetry, servis, geçmiş log ve canlı log handler'ları ortak
PostgreSQL/TimescaleDB kullanan hub replikalarında yatay ölçeklenebilir. Helm chart
HPA, topology spread, rolling update ve PDB tanımlar.

Erişilebilirlik değerlendirmesi şu anda her hub replikasında çalışabilir; veritabanı
unique kısıtı çift aktif olayı engeller. Çok büyük filolarda tarama işi leader
election veya ayrı scheduler/queue bileşenine taşınmalıdır.

PostgreSQL modunda enrollment CA tekil satır olarak `INSERT ... ON CONFLICT`
yarışıyla bir kez oluşturulur ve tüm hub replikalarınca paylaşılır. Bootstrap
token'ın yalnız SHA-256 özeti tutulur; koşullu `UPDATE ... WHERE consumed_at IS
NULL` aynı token'ın eşzamanlı yalnız bir istekte tüketilmesini sağlar. Yeni secret
değeri yeni bir tek-kullanımlık token kaydı oluşturur, aynı tüketilmiş değer hub
restart'ında yeniden etkinleşmez. Yeni hash ilk kez eklenirken önceki tüketilmemiş
token'lar aynı transaction içinde iptal edilir. Memory modu bu durumu yalnız süreç ömründe tutar.

CA private key'i PostgreSQL'de bulunduğu için database, yedek ve PITR erişimi
secret sınırındadır. KMS/HSM tabanlı envelope encryption ileri sertleştirme olarak
kalır. Chart'ın varsayılan tek replika/HPA kapalı ayarı enrollment'dan değil,
henüz süreç içinde üretilen iş imzalama anahtarından kaynaklanır.

İş imzalama anahtarı şu anda hub başlangıcında süreç içinde üretilir ve restart
sonrası değişir. Kalıcı güven kökü ve çoklu replika için private key KMS/Secret'ta
saklanmalı, public key güvenli enrollment/config kanalından agent'a sabitlenmelidir.

## Güven sınırları

- Uzak enrollment plaintext HTTP üzerinden reddedilir.
- Agent yazma uçları doğrulanmış mTLS client sertifikası ister.
- İş oluşturma ve olay onayı operator bearer secret'ı; politika, bakım ve bulut
  mutasyonları ayrı admin bearer secret'ı ister. Admin operator yetkilerini kapsar.
- Admin secret tanımlanmamış eski kurulumlarda operator secret yönetici rolüne geri
  düşer; iki secret da yoksa mutasyon uçları kapalıdır.
- Aksiyon allowlist'i yalnız servis restart ve host reboot'u kabul eder; keyfi shell
  çalıştırma desteklenmez.
- JSON gövdeleri 1 MiB ile sınırlıdır ve bilinmeyen alanlar reddedilir.
- Container non-root ve read-only root filesystem ile çalışmaya uygundur.
- Kubernetes ServiceAccount token’ı varsayılan olarak pod’a bağlanmaz.

Mevcut iki rollü bearer token kimlik doğrulaması ilk yetki ayrımı dilimidir;
`approved_by` alanı token sahibinin beyanıdır. Kullanıcı bazlı RBAC, SSO/MFA,
çift onay ve immutable harici audit sink production yetkilendirme sertleştirmesi
olarak ayrıca ele alınacaktır.
