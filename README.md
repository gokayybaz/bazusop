# bazUSOP

**Unified Server Operations Platform**

bazUSOP, Linux ve Windows sunucularını tek kontrol düzleminden izlemek ve
yönetmek için geliştirilen agent–hub platformudur. Ürünün ana dili Türkçedir;
API alanları ve kod tanımlayıcıları geriye dönük uyumluluk için İngilizce tutulur.

## Genel bakış

bazUSOP iki bileşenden oluşur:

- **Hub** — Go ile yazılmış tek bir binary. REST API'yi ve gömülü React
  operasyon konsolunu aynı süreçten sunar; tüm veriyi (envanter, telemetri,
  loglar, işler, alarmlar) PostgreSQL/TimescaleDB'de tutar.
- **Agent** (`bazusop-agent`) — yönetilecek her Linux veya Windows sunucusuna
  kurulan ayrı bir binary. Hub'a **her zaman dışarıya doğru** bağlanır; agent
  tarafında hiçbir inbound port açık olmasına gerek yoktur, bu yüzden
  NAT/firewall arkasındaki sunucular için de uygundur.

Bir agent ilk çalıştığında tek kullanımlık bir bootstrap token ile hub'a
kaydolur, karşılığında kararlı bir mTLS kimliği (SPIFFE ID) alır ve bundan
sonra tüm iletişimini bu kimlikle yapar. Kayıttan sonra periyodik olarak host
envanterini, CPU/bellek/disk/ağ telemetrisini, systemd/Windows Service
durumlarını ve platform loglarını raporlar; kendisine atanmış imzalı
restart/reboot işlerini de aynı döngüde yoklayıp güvenli biçimde çalıştırır.
Operatörler bu veriyi ve aksiyonları gömülü web arayüzünden ya da doğrudan
REST API'den yönetir.

## Mevcut yetenekler

### Envanter ve telemetri

- Ayrı `bazusop-agent` binary'si Linux ve Windows'ta outbound bağlantı kurar;
  tek kullanımlık token'ı kalıcı Ed25519/mTLS kimliğine dönüştürür, sertifikayı
  süresi dolmadan yeniler; host envanteri, CPU/bellek/disk/ağ telemetrisi ve
  systemd/Windows Service snapshot'ını periyodik raporlar.
- Normalize edilmiş host envanteri PostgreSQL'de saklanır.
- CPU, bellek, disk ve ağ telemetrisi PostgreSQL veya TimescaleDB'ye yazılır;
  Timescale etkinse varsayılan 30 günlük, yapılandırılabilir retention uygulanır.
- systemd ve Windows Service snapshot'ları normalize edilerek sunucu bazında
  aranabilir ve durumlarına göre filtrelenebilir.

### Loglar

- Agent, Linux journald ile Windows System/Application Event kayıtlarını ortak
  önem modeline çevirip sınırlı mTLS batch'leri halinde periyodik gönderir.
- journald, dosya ve Windows Event kayıtları ortak log modelinde aranabilir;
  sunucu detayında sınırlı geçmiş ve canlı SSE akışı birlikte izlenebilir.
- PostgreSQL kullanan hub replikaları canlı log olaylarını `LISTEN/NOTIFY` üzerinden
  paylaşır; SSE istemcisi ingest yapan replikaya bağlı olmak zorunda değildir.

### Operasyon işleri

- Servis yeniden başlatma ve host reboot talepleri operatör token'ıyla onaylanır,
  kalıcı enrollment güven köküyle imzalanır; agent imzayı pinlenmiş CA anahtarıyla
  doğruladıktan sonra allowlist aksiyonunu çalıştırır, yürütme durumunu diskte
  korur ve idempotent, sıralı audit olayları gönderir.

### Alarm ve denetim

- CPU, bellek, disk ve agent erişilebilirlik kuralları olay açar; olaylar onaylanır,
  koşul normale dönünce çözülür ve bakım pencerelerinde yeni alarm bastırılır.
- İş ve alarm olayları, sunucu bazlı kendi geçmişlerinin yanında
  `GET /api/v1/audit/events` ile tek bir birleşik, site bazlı zaman
  çizelgesinde de görüntülenebilir.

### Bulut keşfi

- AWS, Azure ve GCP hesaplarıyla gelen instance snapshot'ları PostgreSQL'de tutulur;
  doğrulanmış provider agent kimliği otomatik, hostname/IP benzerliği yalnız aday olarak uzlaştırılır.

### Arayüz

- React arayüzü Go hub binary'sine gömülür; dağıtım için tek çalıştırılabilir
  dosya yeterlidir.
- Arayüz; genel bakış, filo, servisler, metrikler, loglar, işler, alarmlar,
  bulut hesapları, denetim izi ve ayarlar için ayrı, doğrudan açılabilir sayfalar sunar.
- Ayarlar sayfası etkin storage/Timescale modunu ve telemetri-log retention
  değerlerini ve çalışan hub'ın sürüm kimliğini secret bilgisi göstermeden okur.
- Serin nötr açık ve grafit koyu tema arasında geçiş yapılabilir; cihaz tercihi
  tarayıcıda korunur ve tüm operasyon sayfalarına uygulanır.

### Dağıtım

- Docker Compose geliştirme ortamı ve production odaklı Kubernetes/Helm chart'ı
  bulunur.

## Hızlı başlangıç

Gereksinimler: Go 1.26.4+, Node.js 24+ ve npm.

```bash
make test
make build
BAZUSOP_ENROLLMENT_TOKEN="tek-kullanimlik-guclu-bir-secret" \
BAZUSOP_BOOTSTRAP_SECRET="tek-kullanimlik-baska-bir-guclu-secret" \
BAZUSOP_TOTP_ENCRYPTION_KEY="totp-secret-sifreleme-anahtari" \
BAZUSOP_TELEMETRY_RETENTION_DAYS=30 \
BAZUSOP_LOG_RETENTION_DAYS=14 \
./bin/bazusop-hub
```

React uygulaması önce derlenir, sonra Go hub binary'sine gömülür. `make build`
hem `bin/bazusop-hub` hem `bin/bazusop-agent` üretir. Hub varsayılan olarak
`http://127.0.0.1:8080` adresinden erişilebilir.

Kurumsal landing page, operasyon konsolundan ayrı bir statik çıktı olarak
hazırlanır:

```bash
cd web
npm run dev:landing
npm run build:landing
```

`main` dalına landing page kaynaklarını etkileyen bir değişiklik gönderildiğinde
`.github/workflows/pages.yml` çıktıyı otomatik olarak GitHub Pages'e yayınlar.
Depo ayarlarında **Pages → Source** seçeneğinin **GitHub Actions** olması gerekir.

Binary'nin kaynak kimliğini yapılandırma yüklemeden görmek için:

```bash
./bin/bazusop-hub --version
```

Tam geliştirme ortamını TimescaleDB ile başlatmak için:

```bash
POSTGRES_PASSWORD=yerel-parola \
BAZUSOP_ENROLLMENT_TOKEN=yerel-token \
BAZUSOP_BOOTSTRAP_SECRET=yerel-bootstrap-secret \
BAZUSOP_TOTP_ENCRYPTION_KEY=yerel-totp-anahtari \
docker compose up --build
```

`BAZUSOP_PORT`, dışarı açılan hub portunu değiştirir. Compose verisi
`postgres-data` volume'ünde kalıcıdır.

## Yapılandırma özeti

| Değişken | Açıklama |
| --- | --- |
| `BAZUSOP_HTTP_ADDR` | Hub dinleme adresi; varsayılan `:8080` |
| `BAZUSOP_ENROLLMENT_TOKEN` | İlk kayıt için tek kullanımlık bootstrap secret |
| `BAZUSOP_BOOTSTRAP_SECRET` | İlk platform yöneticisini oluşturmak için tek kullanımlık secret |
| `BAZUSOP_TOTP_ENCRYPTION_KEY` | TOTP secret'larını veritabanında şifrelemek için runtime anahtarı |
| `BAZUSOP_SERVICE_ACCOUNT_PEPPER` | Servis hesabı token'larını HMAC ile özetlemek için veritabanı dışı pepper |
| `BAZUSOP_TRUSTED_ORIGINS` | CORS için güvenilir origin listesi (virgülle ayrılmış); varsayılan kapalı |
| `BAZUSOP_TELEMETRY_RETENTION_DAYS` | Timescale telemetri saklama süresi; varsayılan `30`, aralık `1–3650` |
| `BAZUSOP_LOG_RETENTION_DAYS` | Timescale log saklama süresi; varsayılan `14`, aralık `1–3650` |
| `DATABASE_URL` | PostgreSQL/TimescaleDB bağlantı dizesi |
| `BAZUSOP_TIMESCALE_ENABLED` | `true` ise hypertable ve retention yapılandırılır |
| `BAZUSOP_TLS_CERT_FILE` | Hub TLS sertifikasının yolu |
| `BAZUSOP_TLS_KEY_FILE` | Hub TLS private key'inin yolu |

### İlk kurulum

`BAZUSOP_BOOTSTRAP_SECRET` ve `BAZUSOP_TOTP_ENCRYPTION_KEY` ayarlandığında,
ilk platform yöneticisi tek kullanımlık `/api/v1/bootstrap` ucuyla oluşturulur;
ardından `/api/v1/sessions` ile oturum açılır. Mutasyon içeren tüm API'ler
artık yalnız geçerli bir insan oturumu veya servis hesabı token'ı ile
çalışır — ayrıntılar ve otomasyon (CI/CD) geçişi için bkz.
[docs/MIGRATION_v0.4.md](docs/MIGRATION_v0.4.md).

Opsiyonel OIDC girişi platform yöneticisi tarafından `PUT
/api/v1/organization/oidc` ile yapılandırılır; ilk OIDC girişi yalnız
`identity_type: "oidc"` ile oluşturulmuş geçerli bir davetle çalışır. Yerel
giriş her zaman bağımsız çalışmaya devam eder.

Agent değişkenleri:

| Değişken | Açıklama |
| --- | --- |
| `BAZUSOP_AGENT_HUB_URL` | Hub kök URL'si; üretimde `https://...` zorunludur |
| `BAZUSOP_AGENT_ENROLLMENT_TOKEN` | Yalnız ilk kayıtta gereken bootstrap secret |
| `BAZUSOP_AGENT_STATE_DIR` | Kimlik dizini; Linux varsayılanı `/var/lib/bazusop-agent`, Windows varsayılanı `%ProgramData%\\bazUSOP\\agent` |
| `BAZUSOP_AGENT_SERVER_CA_FILE` | Özel hub server CA PEM dosyası; sistem trust store yeterliyse verilmez |
| `BAZUSOP_AGENT_REPORT_INTERVAL` | Envanter, telemetri ve servis periyodu; varsayılan `30s`, aralık `10s–1h` |

TLS cert ve key birlikte verilmelidir. Hub TLS 1.3 kullanır ve kayıtlı client
sertifikalarını mTLS için doğrular. `DATABASE_URL` verilmezse envanter ve telemetri
yalnızca geliştirme amaçlı süreç içi bellekte tutulur.

## Agent kayıt ve veri akışı

`POST /api/v1/agents/enroll`, tek kullanımlık bootstrap token ve imzalı PKCS#10
CSR karşılığında 24 saatlik client sertifikası üretir. Sertifika kararlı bir SPIFFE
agent ID taşır. Yenileme `POST /api/v1/agents/renew` üzerinden mTLS ile yapılır.
Uzak plaintext HTTP kayıt istekleri reddedilir; loopback HTTP yalnızca yerel
geliştirme için açıktır.

Gerçek agent'ı özel CA ile çalışan bir hub'a bağlamak için:

```bash
sudo install -d -m 0700 /var/lib/bazusop-agent
sudo env \
  BAZUSOP_AGENT_HUB_URL=https://hub.example.com \
  BAZUSOP_AGENT_ENROLLMENT_TOKEN='tek-kullanimlik-token' \
  BAZUSOP_AGENT_SERVER_CA_FILE=/etc/bazusop/hub-server-ca.crt \
  ./bin/bazusop-agent
```

Private key Unix'te `0600` izinle atomik yazılır; Windows'ta state dizininin ACL'i
kurulum sırasında yalnız agent servis hesabına sınırlandırılmalıdır. İlk kayıt
tamamlandıktan sonra token state dizinine kaydedilmez ve sonraki başlangıçlarda gerekmez. Agent sertifikası
sona ermeden bir saat önce aynı SPIFFE kimliğiyle otomatik yenilenir. Ayrıntılı
kurulum ve güven modeli için [agent rehberine](docs/AGENT.md) bakın.

Kayıtlı agent'lar envanteri `PUT /api/v1/agents/inventory`, telemetriyi
`POST /api/v1/agents/telemetry`, servisleri `PUT /api/v1/agents/services` ile
raporlar; journald veya Windows Event batch'leri `POST /api/v1/agents/logs`
yolunu kullanır. Agent başarılı log tesliminden sonra cursor'u state dizinindeki
`log-checkpoint.json` dosyasına atomik yazar. Yeniden gönderilen kayıtlar kararlı
kimlikleri sayesinde hub geçmişinde ve canlı akışta çoğalmaz. UI, filo listesini
`GET /api/v1/instances`, zaman serisini
`GET /api/v1/instances/{agent_id}/telemetry`, servisleri
`GET /api/v1/instances/{agent_id}/services` üzerinden okur.
Log geçmişi `GET /api/v1/instances/{agent_id}/logs`, canlı akış ise aynı yolun
`/stream` alt kaynağıdır.

Operatörler `POST /api/v1/instances/{agent_id}/jobs` ile imzalı restart/reboot
işi oluşturur. Agent işi mTLS ile teslim alır ve sıralı çıktı/durum olaylarını
raporlar. İş listesi ve değiştirilemez olay geçmişi sunucu detayında gösterilir.
Bu ve tüm diğer mutasyon uçları yalnız geçerli bir insan oturumu veya servis
hesabı token'ıyla çalışır; hiçbiri yapılandırılmamışsa mutasyonlar güvenli
biçimde kapalı kalır (bkz. [docs/MIGRATION_v0.4.md](docs/MIGRATION_v0.4.md)).
Site-admin rolü site-operator işlemlerini de yapabilir.

Bulut connector'ları hesapları `POST /api/v1/cloud/accounts` ile tanımlar ve
provider snapshot'ını `PUT /api/v1/cloud/accounts/{account_id}/instances` yoluna
gönderir. Salt-okunur hesap ve uzlaştırma görünümü `/cloud` sayfasındadır.

Alarm kuralları ve bakım pencereleri `PermissionManageAlerts` iznini gerektirir
(site-admin rolü veya platform yöneticisi). Telemetri
kuralları her kabul edilen örnekte; erişilebilirlik kuralları 30 saniyede bir
değerlendirilir. Aktif olayların özeti genel bakışta, yaşam döngüsü ise ayrı
`/alerts` sayfasındaki alarm merkezinde görünür.

`DATABASE_URL` verildiğinde agent CA sertifikası/private key'i ve yalnız SHA-256
özeti saklanan bootstrap token'ların tüketim durumu PostgreSQL'de ortaktır. Hub
restart'ı mevcut agent kimliklerini bozmaz; eşzamanlı replikalardan yalnız biri
aynı token'ı tüketebilir. Yeni agent kaydı için secret'taki token değerini
değiştirip hub'ları rolling restart ederek yeni tek-kullanımlık token kaydedilir;
önceki tüketilmemiş token yeniden etkinleştirilemeyecek biçimde iptal edilir.
Memory modu CA ve token durumunu süreç ömrüyle sınırlı tutar.

## Kubernetes ve Helm

Chart, `database-url`, `enrollment-token` ve isteğe bağlı `bootstrap-secret`,
`totp-encryption-key`, `service-account-pepper` anahtarlarını içeren mevcut bir
`bazusop-secrets` Secret'ı bekler:

```bash
helm upgrade --install bazusop deploy/helm/bazusop \
  --namespace bazusop --create-namespace
```

Chart; non-root/read-only container güvenliği, resource request/limit,
readiness/liveness probe, rolling update, topology spread, PDB ve isteğe bağlı
`autoscaling/v2` HPA sağlar. Doğrudan hub TLS'i mevcut bir Secret'tan
`tls.enabled=true` ile bağlanabilir.

## Sürüm arşivleri

SemVer sürümüne ait Linux `amd64`/`arm64` ve Windows `amd64` arşivlerini ve
SHA-256 manifestini yerelde üretmek için:

```bash
make release VERSION=0.3.0
cd dist/bazusop-0.3.0
shasum -a 256 -c checksums.txt
```

`v*` etiketi pushlandığında GitHub Actions önce tüm testleri çalıştırır; Linux
amd64/arm64 için deb/rpm, Windows amd64 için MSI ve hub/agent sürümlü arşivlerini
yayımlar. Her iki binary sürüm, commit ve UTC build tarihini `--version`
çıktısında taşır; hub kimliği ayrıca Ayarlar sayfasında görünür.

Release'in `checksums.txt` manifesti ve çok mimarili GHCR container digest'i
GitHub OIDC üzerinden Sigstore Cosign ile keyless imzalanır. Hub Linux paketi
systemd unit'i ve checksum/sürüm/sağlık kontrollü `upgrade-hub` aracını içerir.
Agent deb/rpm paketleri systemd birimini, Windows agent MSI ise native ve
otomatik başlangıçlı `bazusop-agent` servisini kurar. Her iki Windows MSI
major-upgrade ve downgrade engelleme sözleşmesini kullanır; hub MSI servis kaydı
oluşturmaz.

## Dokümantasyon

- [API sözleşmesi](docs/API.md)
- [Agent kurulum ve güven rehberi](docs/AGENT.md)
- [Mimari](docs/ARCHITECTURE.md)
- [Operasyon rehberi](docs/OPERATIONS.md)
- [Runbook](docs/RUNBOOK.md)
- [Tasarım sistemi](docs/DESIGN.md)
- [Teslimat yol haritası](docs/ROADMAP.md)

Her spike TDD döngüsüyle ilerler: önce kırmızı kabul testi, ardından en küçük
dikey dilim, refactor, tam test/build doğrulaması ve ayrı commit + push.
