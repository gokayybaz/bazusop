# bazUSOP

**Unified Server Operations Platform**

bazUSOP, Linux ve Windows sunucularını tek kontrol düzleminden izlemek ve
yönetmek için geliştirilen agent–hub platformudur. Ürünün ana dili Türkçedir;
API alanları ve kod tanımlayıcıları geriye dönük uyumluluk için İngilizce tutulur.

## Mevcut yetenekler

- React arayüzü Go hub binary'sine gömülür; dağıtım için tek çalıştırılabilir
  dosya yeterlidir.
- Linux ve Windows agent'ları tek kullanımlık token ile kaydolur, hub CA'sının
  imzaladığı kısa ömürlü mTLS kimliğiyle haberleşir.
- Normalize edilmiş host envanteri PostgreSQL'de saklanır.
- CPU, bellek, disk ve ağ telemetrisi PostgreSQL veya TimescaleDB'ye yazılır;
  Timescale etkinse 30 günlük retention policy uygulanır.
- systemd ve Windows Service snapshot'ları normalize edilerek sunucu bazında
  aranabilir ve durumlarına göre filtrelenebilir.
- journald, dosya ve Windows Event kayıtları ortak log modelinde aranabilir;
  sunucu detayında sınırlı geçmiş ve canlı SSE akışı birlikte izlenebilir.
- PostgreSQL kullanan hub replikaları canlı log olaylarını `LISTEN/NOTIFY` üzerinden
  paylaşır; SSE istemcisi ingest yapan replikaya bağlı olmak zorunda değildir.
- Servis yeniden başlatma ve host reboot talepleri operatör token'ıyla onaylanır,
  Ed25519 ile imzalanır ve sıralı audit olaylarıyla uçtan uca izlenir.
- CPU, bellek, disk ve agent erişilebilirlik kuralları olay açar; olaylar onaylanır,
  koşul normale dönünce çözülür ve bakım pencerelerinde yeni alarm bastırılır.
- Arayüz; genel bakış, filo, servisler, metrikler, loglar, işler, alarmlar,
  bulut hesapları, denetim izi ve ayarlar için ayrı, doğrudan açılabilir sayfalar sunar.
- Serin nötr açık ve grafit koyu tema arasında geçiş yapılabilir; cihaz tercihi
  tarayıcıda korunur ve tüm operasyon sayfalarına uygulanır.
- AWS, Azure ve GCP hesaplarıyla gelen instance snapshot'ları PostgreSQL'de tutulur;
  doğrulanmış provider agent kimliği otomatik, hostname/IP benzerliği yalnız aday olarak uzlaştırılır.
- Docker Compose geliştirme ortamı ve production odaklı Kubernetes/Helm chart'ı
  bulunur.

## Hızlı başlangıç

Gereksinimler: Go 1.26+, Node.js 24+ ve npm.

```bash
make test
make build
BAZUSOP_ENROLLMENT_TOKEN="tek-kullanimlik-guclu-bir-secret" \
BAZUSOP_OPERATOR_TOKEN="ayri-guclu-bir-operator-secret" \
BAZUSOP_ADMIN_TOKEN="ayri-guclu-bir-yonetici-secret" \
./bin/bazusop-hub
```

React uygulaması önce derlenir, sonra Go hub binary'sine gömülür. Hub varsayılan
olarak `http://127.0.0.1:8080` adresinden erişilebilir.

Tam geliştirme ortamını TimescaleDB ile başlatmak için:

```bash
POSTGRES_PASSWORD=yerel-parola \
BAZUSOP_ENROLLMENT_TOKEN=yerel-token \
BAZUSOP_OPERATOR_TOKEN=yerel-operator-token \
BAZUSOP_ADMIN_TOKEN=yerel-yonetici-token \
docker compose up --build
```

`BAZUSOP_PORT`, dışarı açılan hub portunu değiştirir. Compose verisi
`postgres-data` volume'ünde kalıcıdır.

## Yapılandırma özeti

| Değişken | Açıklama |
| --- | --- |
| `BAZUSOP_HTTP_ADDR` | Hub dinleme adresi; varsayılan `:8080` |
| `BAZUSOP_ENROLLMENT_TOKEN` | İlk kayıt için tek kullanımlık bootstrap secret |
| `BAZUSOP_OPERATOR_TOKEN` | İş oluşturma ve olay onaylama yetkisi veren bearer secret |
| `BAZUSOP_ADMIN_TOKEN` | Politika, bakım ve bulut bağlantısı yönetme yetkisi; yoksa operator token'a geri düşer |
| `DATABASE_URL` | PostgreSQL/TimescaleDB bağlantı dizesi |
| `BAZUSOP_TIMESCALE_ENABLED` | `true` ise hypertable ve retention yapılandırılır |
| `BAZUSOP_TLS_CERT_FILE` | Hub TLS sertifikasının yolu |
| `BAZUSOP_TLS_KEY_FILE` | Hub TLS private key'inin yolu |

TLS cert ve key birlikte verilmelidir. Hub TLS 1.3 kullanır ve kayıtlı client
sertifikalarını mTLS için doğrular. `DATABASE_URL` verilmezse envanter ve telemetri
yalnızca geliştirme amaçlı süreç içi bellekte tutulur.

## Agent kayıt ve veri akışı

`POST /api/v1/agents/enroll`, tek kullanımlık bootstrap token ve imzalı PKCS#10
CSR karşılığında 24 saatlik client sertifikası üretir. Sertifika kararlı bir SPIFFE
agent ID taşır. Yenileme `POST /api/v1/agents/renew` üzerinden mTLS ile yapılır.
Uzak plaintext HTTP kayıt istekleri reddedilir; loopback HTTP yalnızca yerel
geliştirme için açıktır.

Kayıtlı agent'lar envanteri `PUT /api/v1/agents/inventory`, telemetriyi
`POST /api/v1/agents/telemetry`, servisleri `PUT /api/v1/agents/services` ile
raporlar; log batch'leri `POST /api/v1/agents/logs` yolunu kullanır. UI, filo listesini
`GET /api/v1/instances`, zaman serisini
`GET /api/v1/instances/{agent_id}/telemetry`, servisleri
`GET /api/v1/instances/{agent_id}/services` üzerinden okur.
Log geçmişi `GET /api/v1/instances/{agent_id}/logs`, canlı akış ise aynı yolun
`/stream` alt kaynağıdır.

Operatörler `POST /api/v1/instances/{agent_id}/jobs` ile imzalı restart/reboot
işi oluşturur. Agent işi mTLS ile teslim alır ve sıralı çıktı/durum olaylarını
raporlar. İş listesi ve değiştirilemez olay geçmişi sunucu detayında gösterilir.
Operator ve admin token'larının ikisi de ayarlanmadığında yetkili mutasyonlar
güvenli biçimde kapatılır. Admin rolü operator işlemlerini de yapabilir.

Bulut connector'ları hesapları `POST /api/v1/cloud/accounts` ile tanımlar ve
provider snapshot'ını `PUT /api/v1/cloud/accounts/{account_id}/instances` yoluna
gönderir. Salt-okunur hesap ve uzlaştırma görünümü `/cloud` sayfasındadır.

Alarm kuralları ve bakım pencereleri ayrı admin token'ıyla yönetilir. Telemetri
kuralları her kabul edilen örnekte; erişilebilirlik kuralları 30 saniyede bir
değerlendirilir. Aktif olayların özeti genel bakışta, yaşam döngüsü ise ayrı
`/alerts` sayfasındaki alarm merkezinde görünür.

Şu anda CA private key'i ve tüketilmiş bootstrap-token durumu hub sürecindedir.
Bu nedenle enrollment trafiği için tek replika kullanılmalıdır; ortak KMS/Secret
ve PostgreSQL tabanlı token durumu sonraki ölçek sertleştirmesinde ele alınacaktır.

## Kubernetes ve Helm

Chart, `database-url`, `enrollment-token`, `operator-token` ve önerilen
`admin-token` anahtarlarını içeren mevcut bir `bazusop-secrets` Secret'ı bekler:

```bash
helm upgrade --install bazusop deploy/helm/bazusop \
  --namespace bazusop --create-namespace
```

Chart; non-root/read-only container güvenliği, resource request/limit,
readiness/liveness probe, rolling update, topology spread, PDB ve isteğe bağlı
`autoscaling/v2` HPA sağlar. Doğrudan hub TLS'i mevcut bir Secret'tan
`tls.enabled=true` ile bağlanabilir.

## Dokümantasyon

- [API sözleşmesi](docs/API.md)
- [Mimari](docs/ARCHITECTURE.md)
- [Operasyon rehberi](docs/OPERATIONS.md)
- [Tasarım sistemi](docs/DESIGN.md)
- [Teslimat yol haritası](docs/ROADMAP.md)

Her spike TDD döngüsüyle ilerler: önce kırmızı kabul testi, ardından en küçük
dikey dilim, refactor, tam test/build doğrulaması ve ayrı commit + push.
