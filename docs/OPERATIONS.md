# Operasyon rehberi

## Yapılandırma

| Değişken | Zorunluluk | Açıklama |
| --- | --- | --- |
| `BAZUSOP_HTTP_ADDR` | Hayır | Dinleme adresi; varsayılan `:8080` |
| `BAZUSOP_ENROLLMENT_TOKEN` | Üretimde evet | İlk agent için bootstrap secret |
| `BAZUSOP_OPERATOR_TOKEN` | Uzak aksiyon için evet | İş oluşturma bearer secret'ı |
| `DATABASE_URL` | Üretimde evet | PostgreSQL bağlantı dizesi |
| `BAZUSOP_TIMESCALE_ENABLED` | Hayır | `true` ise extension, hypertable ve retention kurulur |
| `BAZUSOP_TLS_CERT_FILE` | Uzak agent için evet | Hub server sertifikası |
| `BAZUSOP_TLS_KEY_FILE` | Uzak agent için evet | Hub server private key’i |

TLS cert ve key birlikte verilmelidir. `DATABASE_URL` yoksa inventory ve telemetry
süreç içi bellekte tutulur; restart sonrası kaybolur.

## Yerel geliştirme

```bash
make test
make build
BAZUSOP_ENROLLMENT_TOKEN=local-token BAZUSOP_OPERATOR_TOKEN=local-operator-token ./bin/bazusop-hub
```

Tam stack için:

```bash
POSTGRES_PASSWORD=yerel-parola BAZUSOP_ENROLLMENT_TOKEN=yerel-token BAZUSOP_OPERATOR_TOKEN=yerel-operator-token docker compose up --build
```

Compose TimescaleDB PostgreSQL 18 imajını kullanır, database health bekler ve hub
başlangıcında migration’ları uygular.

## Kubernetes

Chart uygulama secret’ını üretmez. `database-url`, `enrollment-token` ve `operator-token` anahtarlarını
taşıyan mevcut bir Secret verilmelidir. TLS etkinse server cert/key ayrıca mevcut
bir TLS Secret’tan read-only mount edilir.

Üretim başlangıç kontrol listesi:

1. PostgreSQL yedekleme ve PITR politikasını doğrula.
2. Timescale extension yetkisini ve 30 günlük retention'ı doğrula.
   Telemetri 30 gün, loglar 14 gün saklanır.
3. TLS secret rotasyonunu planla.
4. Enrollment ve operator token'larını ayrı, yüksek entropili değerlerle oluştur;
   secret erişimini sınırla ve rotasyon prosedürünü test et.
5. CPU/RAM request-limit değerlerini gerçek yük testine göre ayarla.
6. Enrollment state ve iş imza anahtarı paylaşılmadan HPA’yı açma.
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
- SSE bağlantısı açılıyor fakat kayıt gelmiyorsa reverse proxy buffering'i kapat;
  çoklu hub replikasında ortak event bus henüz bulunmadığını hesaba kat.
- Agent yazma uçları `401` dönüyorsa client certificate chain, süre ve SPIFFE URI
  SAN değerini kontrol et.
- İş oluşturma `401` dönüyorsa `Authorization: Bearer ...` değerini; `503`
  dönüyorsa hub'da `BAZUSOP_OPERATOR_TOKEN` yapılandırmasını kontrol et. UI token'ı
  kalıcı depolamaz ve başarılı oluşturmadan sonra bellekten temizler.
- İş `running` durumunda kalıyorsa agent event sequence'inin teslimden sonra 2 ile
  başlayıp kesintisiz arttığını ve terminal olay gönderdiğini kontrol et.
- Metrik alarmı açılmıyorsa kuralın etkin olduğunu, metric adını ve agent'ın yeni
  telemetri gönderdiğini kontrol et. Erişilebilirlik kuralları 30 saniyede bir
  değerlendirilir.
- Beklenen olay bakım sırasında görünmüyorsa aktif global veya agent kapsamlı
  pencereyi kontrol et. Bakım penceresi yalnız yeni açılışı bastırır; önceden açık
  olayı otomatik kapatmaz.
- `426` enrollment yanıtı, uzak isteğin TLS olmadan geldiğini gösterir.

Operator token rotasyonu sırasında eski token'la yeni iş oluşturmayı durdurun,
Secret/env değerini değiştirip hub pod'larını yeniden başlatın ve yeni token'la
kontrollü bir test işi oluşturun. Mevcut imzalı işler rotasyondan etkilenmez.
