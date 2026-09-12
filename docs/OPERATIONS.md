# Operasyon rehberi

## Yapılandırma

| Değişken | Zorunluluk | Açıklama |
| --- | --- | --- |
| `BAZUSOP_HTTP_ADDR` | Hayır | Dinleme adresi; varsayılan `:8080` |
| `BAZUSOP_ENROLLMENT_TOKEN` | Üretimde evet | İlk agent için bootstrap secret |
| `BAZUSOP_OPERATOR_TOKEN` | Uzak aksiyon için evet | İş oluşturma ve olay onaylama bearer secret'ı |
| `BAZUSOP_ADMIN_TOKEN` | Üretimde evet | Alarm politikası, bakım ve bulut bağlantısı yönetim secret'ı; yoksa operator token kullanılır |
| `DATABASE_URL` | Üretimde evet | PostgreSQL bağlantı dizesi |
| `BAZUSOP_TIMESCALE_ENABLED` | Hayır | `true` ise extension, hypertable ve retention kurulur |
| `BAZUSOP_TELEMETRY_RETENTION_DAYS` | Hayır | Telemetri saklama günü; varsayılan `30`, aralık `1–3650` |
| `BAZUSOP_LOG_RETENTION_DAYS` | Hayır | Log saklama günü; varsayılan `14`, aralık `1–3650` |
| `BAZUSOP_TLS_CERT_FILE` | Uzak agent için evet | Hub server sertifikası |
| `BAZUSOP_TLS_KEY_FILE` | Uzak agent için evet | Hub server private key’i |

TLS cert ve key birlikte verilmelidir. `DATABASE_URL` yoksa inventory ve telemetry
süreç içi bellekte tutulur; restart sonrası kaybolur.

Agent yapılandırması ve ilk kayıt prosedürü [agent rehberinde](AGENT.md) bulunur.
Release arşivleri her hedef için hem `bazusop-hub` hem `bazusop-agent` binary'sini
ayrı arşivler halinde üretir.

## Yerel geliştirme

```bash
make test
make build
BAZUSOP_ENROLLMENT_TOKEN=local-token BAZUSOP_OPERATOR_TOKEN=local-operator-token BAZUSOP_ADMIN_TOKEN=local-admin-token ./bin/bazusop-hub
```

Tam stack için:

```bash
POSTGRES_PASSWORD=yerel-parola BAZUSOP_ENROLLMENT_TOKEN=yerel-token BAZUSOP_OPERATOR_TOKEN=yerel-operator-token BAZUSOP_ADMIN_TOKEN=yerel-yonetici-token docker compose up --build
```

Compose TimescaleDB PostgreSQL 18 imajını kullanır, database health bekler ve hub
başlangıcında migration’ları uygular.

## Sürüm üretimi ve doğrulama

SemVer sürüm arşivlerini üret:

```bash
make release VERSION=0.3.0
```

Komut `dist/bazusop-0.3.0/` altında Linux amd64/arm64 için `tar.gz`, Windows
amd64 için `zip` ve tüm arşivleri kapsayan `checksums.txt` üretir. Var olan bir
sürüm dizininin üzerine yazmaz. İndirilen dosyayı Linux/macOS üzerinde doğrula:

```bash
cd dist/bazusop-0.3.0
shasum -a 256 -c checksums.txt
```

Çalışan build kimliği `bazusop-hub --version` veya `bazusop-agent --version`,
`GET /api/v1/system/configuration` veya Ayarlar sayfasından karşılaştırılabilir.
Release build'inde sürüm, kaynak commit'i ve UTC build tarihi linker üzerinden
binary'ye yazılır. `v*` Git etiketi workflow'u testten geçmeyen sürümü yayımlamaz.

Release varlıkları arşivlere ek olarak Linux amd64/arm64 deb/rpm ve Windows amd64
hub ve agent MSI'ları içerir. `checksums.txt.sigstore.json`, checksum manifestinin GitHub Actions
OIDC kimliğiyle üretilen Sigstore bundle'ıdır. İmzayı doğrula:

```bash
cosign verify-blob checksums.txt \
  --bundle checksums.txt.sigstore.json \
  --certificate-identity=https://github.com/gokayybaz/bazusop/.github/workflows/release.yml@refs/tags/v0.3.0 \
  --certificate-oidc-issuer=https://token.actions.githubusercontent.com
shasum -a 256 -c checksums.txt
```

Container doğrulamasını tag yerine immutable digest ile yap:

```bash
cosign verify ghcr.io/gokayybaz/bazusop@sha256:<digest> \
  --certificate-identity=https://github.com/gokayybaz/bazusop/.github/workflows/release.yml@refs/tags/v0.3.0 \
  --certificate-oidc-issuer=https://token.actions.githubusercontent.com
```

Linux paketi kurulduktan sonra `/etc/bazusop/hub.env` dosyasını `root:bazusop`
ve `0640` izinleriyle oluşturup `systemctl start bazusop-hub` çalıştır. Paket,
secret içermeyen bu dizini ve servisi kurar ancak eksik yapılandırmayla servisi
başlatmaz.

Agent deb/rpm paketi `/etc/bazusop/agent.env` mevcut değilken servisi başlatmaz,
ancak boot için etkinleştirir. Dosyayı root sahipliğinde `0600` izinle hazırlayıp
`systemctl start bazusop-agent` çalıştırın. Agent state dizini `0700` olarak
korunur ve paket kaldırıldığında silinmez. Kimlik dosyalarıyla birlikte
`log-checkpoint.json` da restart sonrası log devamlılığı için korunmalı ve başka
bir agent kimliğiyle paylaşılmamalıdır. Windows agent MSI otomatik başlangıçlı
`bazusop-agent` servisini kaydeder; makine kapsamlı agent ortam değişkenleri
tanımlandıktan sonra servis elle ilk kez başlatılır.

Manuel kontrollü yükseltmede önce imzalı manifesti ve arşivi doğrula, binary'yi
çıkar ve kendi özetini yükseltme aracına ver:

```bash
candidate=/tmp/bazusop-hub
candidate_sha=$(sha256sum "$candidate" | awk '{print $1}')
sudo /usr/lib/bazusop/upgrade-hub \
  --candidate "$candidate" \
  --target /usr/bin/bazusop-hub \
  --version 0.3.0 \
  --sha256 "$candidate_sha"
```

Araç eşzamanlı yükseltmeyi kilitler, önceki binary'yi `.previous` olarak saklar
ve restart sonrasında sağlık ucu geçmezse geri yükler. Hub ve agent Windows
MSI'ları yeni major sürümü yerinde yükseltir ve eski sürüm kurulumunu engeller.
Hub MSI servis kaydı oluşturmaz; agent MSI native Windows Service yaşam döngüsünü
yönetir.

İki bağımsız store üzerinden gerçek `LISTEN/NOTIFY` ve ortak enrollment state
entegrasyon testlerini çalıştırmak için:

```bash
BAZUSOP_TEST_DATABASE_URL='postgres://bazusop:parola@127.0.0.1:5432/bazusop?sslmode=disable' \
  GOCACHE=/tmp/bazusop-go-cache go test ./internal/storage/postgres -run TestPostgres
```

## Kubernetes

Chart uygulama secret’ını üretmez. `database-url`, `enrollment-token`, `operator-token`
ve önerilen `admin-token` anahtarlarını taşıyan mevcut bir Secret verilmelidir.
`admin-token` yoksa geriye uyumluluk için operator token yönetici rolünü de taşır.
TLS etkinse server cert/key ayrıca mevcut
bir TLS Secret’tan read-only mount edilir.

Üretim başlangıç kontrol listesi:

1. PostgreSQL yedekleme ve PITR politikasını doğrula.
2. Timescale extension yetkisini ve retention değerlerini doğrula. Varsayılan
   olarak telemetri 30 gün, loglar 14 gün saklanır. Değer değişikliği hub
   başlangıcında advisory lock altında mevcut policy'yi güvenle yeniler.
3. TLS secret rotasyonunu planla.
4. Enrollment, operator ve admin token'larını ayrı, yüksek entropili değerlerle oluştur;
   secret erişimini sınırla ve rotasyon prosedürünü test et.
5. CPU/RAM request-limit değerlerini gerçek yük testine göre ayarla.
6. PostgreSQL enrollment state'ini ve yedek şifrelemesini doğrula; HPA öncesinde
   tüm replikaların aynı enrollment/iş imza güven kökünü kullandığını test et.
7. Ingress kullanılıyorsa agent mTLS trafiğinin client sertifikasını hub’a kadar
   koruduğunu doğrula.
8. Kritik CPU/bellek/disk ve erişilebilirlik eşiklerini gerçek baseline'a göre
   ayarla; planlı çalışmadan önce kapsamı doğru bakım penceresini oluştur.

## Sağlık ve sorun giderme

- `GET /api/v1/health` liveness ve readiness probe’larının hedefidir.
- Hub başlangıçta database’e ping atamazsa veya migration başarısızsa process
  başlamaz; logdaki `could not initialize PostgreSQL inventory store` mesajını ve
  bağlantı/extension yetkilerini kontrol et.
- Telemetry yazımı `400` dönüyorsa timestamp ve `0–100` yüzde sınırlarını kontrol et.
- Servis snapshot'ı `500` dönüyorsa agent'ın önce envanter raporu gönderdiğini ve
  `hosts` kaydının bulunduğunu kontrol et. `400` için servis state/startup type
  eşlemesini ve 5000 kayıt sınırını kontrol et.
- Log batch'i `400` dönüyorsa collector/severity eşlemesini, timestamp'i, 1000
  kayıt batch sınırını ve 64 KiB mesaj sınırını kontrol et.
- SSE bağlantısı açılıyor fakat kayıt gelmiyorsa reverse proxy buffering'i ve
  PostgreSQL bağlantısını kontrol et. Hub `LISTEN/NOTIFY` bağlantısını otomatik
  yeniler; kesinti aralığındaki kayıtları geçmiş log sorgusuyla tamamla.
- Agent yazma uçları `401` dönüyorsa client certificate chain, süre ve SPIFFE URI
  SAN değerini kontrol et.
- İş oluşturma `401` dönüyorsa `Authorization: Bearer ...` değerini; `503`
  dönüyorsa hub'da `BAZUSOP_OPERATOR_TOKEN` yapılandırmasını kontrol et. UI token'ı
  kalıcı depolamaz ve başarılı oluşturmadan sonra bellekten temizler.
- Politika, bakım veya bulut mutasyonu `403` dönüyorsa operator yerine admin token
  kullanın. `401` bilinmeyen token'ı, `503` ise hiçbir yetkili token'ın
  yapılandırılmadığını gösterir.
- İş `running` durumunda kalıyorsa agent event sequence'inin teslimden sonra 2 ile
  başlayıp kesintisiz arttığını ve terminal olay gönderdiğini kontrol et.
- Metrik alarmı açılmıyorsa kuralın etkin olduğunu, metric adını ve agent'ın yeni
  telemetri gönderdiğini kontrol et. Erişilebilirlik kuralları 30 saniyede bir
  değerlendirilir.
- Beklenen olay bakım sırasında görünmüyorsa aktif global veya agent kapsamlı
  pencereyi kontrol et. Bakım penceresi yalnız yeni açılışı bastırır; önceden açık
  olayı otomatik kapatmaz.
- Bulut hesabı `pending` kalıyorsa connector'ın ilgili hesap için tam snapshot
  gönderdiğini kontrol et. Hostname/IP eşleşmesi bilerek yalnız inceleme adayıdır;
  otomatik doğrulama için provider metadata'sında bazUSOP agent kimliği bulunmalıdır.
- `426` enrollment yanıtı, uzak isteğin TLS olmadan geldiğini gösterir.

Token rotasyonu sırasında eski token'la yeni mutasyonları durdurun, Secret/env
değerini değiştirip hub pod'larını yeniden başlatın ve her rolle kontrollü bir test
işlemi yapın. Mevcut imzalı işler rotasyondan etkilenmez.

## Yük kabul hedefi

CI testi, yasal maksimum 1000 kayıtlık tek log batch'inin süreç-içi broker'da 32
eşzamanlı aboneye kayıpsız ve bir saniyeden kısa sürede fan-out edilmesini bekler.
Her abone tamponu bir maksimum batch'i taşır; daha yavaş istemciler geçmiş arama
ucuyla arayı kapatır. Bu eşik kapasite planlama benchmark'ı değil, regresyon
korumasıdır. Yarış koşulu kontrolü için:

```bash
GOCACHE=/tmp/bazusop-go-cache go test -race ./internal/logstream ./internal/storage/postgres
```
