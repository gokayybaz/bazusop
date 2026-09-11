# Operasyon rehberi

## Yapılandırma

| Değişken | Zorunluluk | Açıklama |
| --- | --- | --- |
| `BAZUSOP_HTTP_ADDR` | Hayır | Dinleme adresi; varsayılan `:8080` |
| `BAZUSOP_ENROLLMENT_TOKEN` | Üretimde evet | İlk agent için bootstrap secret |
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
BAZUSOP_ENROLLMENT_TOKEN=local-token ./bin/bazusop-hub
```

Tam stack için:

```bash
POSTGRES_PASSWORD=yerel-parola BAZUSOP_ENROLLMENT_TOKEN=yerel-token docker compose up --build
```

Compose TimescaleDB PostgreSQL 18 imajını kullanır, database health bekler ve hub
başlangıcında migration’ları uygular.

## Kubernetes

Chart uygulama secret’ını üretmez. `database-url` ve `enrollment-token` anahtarlarını
taşıyan mevcut bir Secret verilmelidir. TLS etkinse server cert/key ayrıca mevcut
bir TLS Secret’tan read-only mount edilir.

Üretim başlangıç kontrol listesi:

1. PostgreSQL yedekleme ve PITR politikasını doğrula.
2. Timescale extension yetkisini ve 30 günlük retention’ı doğrula.
3. TLS secret rotasyonunu planla.
4. CPU/RAM request-limit değerlerini gerçek yük testine göre ayarla.
5. Enrollment state paylaşılmadan HPA’yı açma.
6. Ingress kullanılıyorsa agent mTLS trafiğinin client sertifikasını hub’a kadar
   koruduğunu doğrula.

## Sağlık ve sorun giderme

- `GET /api/v1/health` liveness ve readiness probe’larının hedefidir.
- Hub başlangıçta database’e ping atamazsa veya migration başarısızsa process
  başlamaz; logdaki `could not initialize PostgreSQL inventory store` mesajını ve
  bağlantı/extension yetkilerini kontrol et.
- Telemetry yazımı `400` dönüyorsa timestamp ve `0–100` yüzde sınırlarını kontrol et.
- Servis snapshot'ı `500` dönüyorsa agent'ın önce envanter raporu gönderdiğini ve
  `hosts` kaydının bulunduğunu kontrol et. `400` için servis state/startup type
  eşlemesini ve 5000 kayıt sınırını kontrol et.
- Agent yazma uçları `401` dönüyorsa client certificate chain, süre ve SPIFFE URI
  SAN değerini kontrol et.
- `426` enrollment yanıtı, uzak isteğin TLS olmadan geldiğini gösterir.
