# bazUSOP runbook

Bu doküman bir olay ya da anormal durum sırasında "şimdi ne yapmalıyım"
sorusuna hızlı cevap vermek içindir. Yapılandırma referansı, release
prosedürü ve Kubernetes kontrol listesi için [operasyon rehberine](OPERATIONS.md)
bakın; mimari arka plan için [mimari dokümana](ARCHITECTURE.md) bakın.

## Hızlı referans

| Kontrol | Komut/uç |
| --- | --- |
| Hub çalışıyor mu | `GET /api/v1/health` |
| Çalışan build kimliği | `GET /api/v1/system/configuration` veya `bazusop-hub --version` |
| Hub logları | stdout'a JSON (`slog`); systemd kurulumunda `journalctl -u bazusop-hub -f` |
| Agent logları | systemd kurulumunda `journalctl -u bazusop-agent -f`; Windows'ta Event Log |
| Aktif alarmlar | `GET /api/v1/incidents` veya arayüzde `/alerts` |
| Filo durumu | `GET /api/v1/instances` veya arayüzde `/fleet` |
| Birleşik denetim izi | `GET /api/v1/audit/events` veya arayüzde `/audit` |

Bu kurulumda resmi bir on-call zinciri veya sayfalama entegrasyonu yoktur;
aşağıdaki adımlar doğrudan operatör tarafından uygulanır.

## Olay playbook'ları

### 1. Hub health/readiness başarısız

**Belirti:** `GET /api/v1/health` yanıt vermiyor veya pod/servis
`CrashLoopBackOff`/`failed` durumunda.

1. Logda `could not initialize PostgreSQL inventory store` var mı kontrol et
   → `DATABASE_URL`, ağ erişimi ve PostgreSQL kullanıcı yetkilerini doğrula.
2. Migration hatası görünüyorsa `internal/storage/postgres/migrations/`
   altındaki sırayı bozan elle yapılmış bir şema değişikliği olup olmadığını
   kontrol et; migration'lar idempotent ve sıra bağımlıdır.
3. `BAZUSOP_TLS_CERT_FILE`/`BAZUSOP_TLS_KEY_FILE` yalnız biri verilmişse hub
   başlangıçta reddeder — ikisi birlikte verilmeli.
4. Kubernetes'te `kubectl rollout status`/`kubectl logs` ile son deploy'un
   sağlık probe'larını geçip geçmediğini doğrula; geçmiyorsa bkz. §8 (geri alma).

### 2. Metrik alarmı (CPU/Bellek/Disk) tetiklendi

**Belirti:** `/alerts` sayfasında veya `GET /api/v1/incidents` çıktısında
`severity: critical` yeni bir olay.

1. İlgili sunucunun `/metrics` sayfasında güncel telemetriyi doğrula — geçici
   bir sıçrama mı, kalıcı mı?
2. Kalıcıysa hedef sunucuda ilgili süreç/servisi araştır; gerekiyorsa `/jobs`
   sayfasından imzalı `service.restart` işi oluştur (operatör token'ı gerekir).
3. Planlı bir yük artışıysa alarmın tekrar açılmasını önlemek için bakım
   penceresi aç (§6).
4. Olayı operatör kimliğiyle onayla (`POST /api/v1/incidents/{id}/acknowledge`);
   koşul normale dönünce olay otomatik çözülür, elle kapatma gerekmez.

### 3. Erişilebilirlik alarmı (agent son görülme eşiğini aştı)

**Belirti:** `reachability` tipi alarm; agent 30 saniyelik değerlendirme
döngüsünde `stale_after_seconds` süresini aştı.

1. Agent'ın süreç/servis olarak çalıştığını doğrula
   (`systemctl status bazusop-agent` / Windows Hizmetler).
2. Agent loglarında hub'a bağlanma hatası ara — `BAZUSOP_AGENT_HUB_URL`,
   `BAZUSOP_AGENT_SERVER_CA_FILE` ve ağ/firewall erişimini kontrol et.
3. Sertifika süresi dolmuş olabilir; agent kendiliğinden yenilemeye çalışır
   (süre dolmadan 1 saat önce). Yenileme başarısız oluyorsa hub tarafında
   agent kimliğinin hâlâ geçerli olduğunu (`agents` tablosu) doğrula.
4. Planlı bakım ise §6'daki bakım penceresi prosedürünü uygula.

### 4. İş (job) `running` durumunda takılı kaldı

**Belirti:** `/jobs` sayfasında bir `service.restart`/`host.reboot` işi uzun
süredir `running`; terminal duruma geçmiyor.

1. Agent'ın canlı olduğunu doğrula (§3). Erişilemiyorsa iş, agent geri
   döndüğünde iki dakikalık koruma penceresi dolduktan sonra otomatik yeniden
   sunulur — elle müdahale gerekmez, beklemek yeterlidir.
2. Agent canlıysa ve iş yine de ilerlemiyorsa agent loglarında
   `job-state.json` okuma/yazma hatası ara; agent belirsiz crash penceresinde
   komutu tekrar etmek yerine işi başarısız kapatacak şekilde tasarlanmıştır.
3. İşin audit kaydını (`GET /api/v1/instances/{agent_id}/jobs/{job_id}/events`
   veya arayüzde ilgili işin üzerine tıklayarak) incele; `claimed` sonrası
   sequence'in kesintisiz ilerleyip ilerlemediğini doğrula.
4. Terminal işler yeniden açılamaz — hatalı kapanan bir işi tekrar denemek
   için yeni bir iş oluştur.

### 5. Log akışı durdu / SSE kayıt gelmiyor

**Belirti:** `/logs` sayfasında canlı akış açık ama yeni kayıt gelmiyor;
geçmiş arama da boş dönüyor.

1. Agent'ın rapor gönderdiğini doğrula (§3 ile aynı canlılık kontrolü).
2. Hub'da `LISTEN/NOTIFY` dinleyici bağlantısının koptuğundan şüpheleniyorsan
   hub loglarında yeniden bağlanma mesajını ara — bağlantı otomatik kurulur;
   kesinti aralığındaki kayıtlar geçmiş log sorgusuyla (`GET
   /api/v1/instances/{agent_id}/logs`) tamamlanabilir, SSE geçmişi telafi
   etmez.
3. Reverse proxy/ingress kullanılıyorsa response buffering'in `text/event-stream`
   için kapalı olduğunu doğrula (`X-Accel-Buffering: no` hub tarafından
   zaten gönderilir, proxy'nin buna saygı gösterdiğini kontrol et).
4. Log batch'i `400` ile reddediliyorsa collector/severity eşlemesini,
   timestamp'i, 1000 kayıt/64 KiB mesaj sınırlarını kontrol et.

### 6. Agent enrollment/renewal başarısız (401/426)

**Belirti:** Yeni agent kaydı veya mevcut agent sertifika yenilemesi
başarısız.

1. `426` dönüyorsa istek TLS olmadan uzaktan geldi — yalnız loopback HTTP
   geliştirme amaçlı açıktır; uzak enrollment mutlaka TLS ister.
2. `401` enrollment'ta geçersiz/tüketilmiş bootstrap token'ı gösterir. Yeni
   tek kullanımlık token üretmek için `BAZUSOP_ENROLLMENT_TOKEN` secret'ını
   değiştirip hub'ları rolling restart et; önceki tüketilmemiş token bu
   sırada otomatik iptal edilir.
3. `401` renewal'da agent kimliğinin artık geçerli olmadığını gösterir —
   agent'ın state dizinini (`/var/lib/bazusop-agent` veya
   `%ProgramData%\bazUSOP\agent`) temizleyip yeni bir bootstrap token'la
   yeniden kaydet.
4. PostgreSQL modunda birden fazla hub replikası aynı token'ı eşzamanlı
   tüketmeye çalışırsa yalnız biri başarılı olur; diğerleri `401` alır, bu
   beklenen davranıştır.

### 7. Politika/bakım/bulut mutasyonu 401/403/503 dönüyor

1. `503` → hub'da ne operator ne admin token yapılandırılmış; yetkili
   mutasyonlar güvenli biçimde kapalı. İlgili secret'ı tanımla.
2. `401` → `Authorization: Bearer ...` değeri bilinmiyor; token'ı kontrol et.
3. `403` → operatör token'ıyla yönetici işlemi (alarm kuralı, bakım
   penceresi, bulut hesabı) deneniyor; admin token kullan. Admin, operator
   yetkilerini de kapsar.

### 8. Kötü bir release yayıldı — geri alma

1. **Linux paket kurulumu:** `scripts/upgrade-hub.sh` (paketteki adıyla
   `/usr/lib/bazusop/upgrade-hub`) restart sonrası sağlık ucu geçmezse
   önceki binary'yi (`.previous`) otomatik geri yükler — genelde elle
   müdahale gerekmez. Gerekiyorsa aracı önceki sürümün imzalı arşivindeki
   binary ve SHA-256 ile tekrar çağır.
2. **Kubernetes/Helm:** `helm rollback bazusop <önceki-revizyon>` veya
   `kubectl rollout undo deployment/bazusop`; rolling update stratejisi ve
   readiness probe'lar sayesinde hatalı pod trafiğe girmeden geri alınır.
3. **Docker Compose:** önceki image tag'ini `compose.yaml`'da belirtip
   `docker compose up -d --build` ile yeniden dağıt.
4. Her durumda geri almadan önce `GET /api/v1/system/configuration`'daki
   sürüm/commit kimliğini not al; sorunun kapsamını (hangi agent'lar,
   hangi zaman aralığı) `/audit` sayfasından doğrula.

## Yedekleme ve kurtarma

- **Kritik veri:** `enrollment_authority` tablosu (agent CA private key'i) ve
  `enrollment_tokens`/`agents`/`jobs` tabloları PostgreSQL'de tutulur. CA
  private key'i kaybedilirse tüm agent'lar yeniden enroll edilmelidir —
  bu yüzden PostgreSQL yedek ve PITR politikası, secret erişimiyle aynı
  sıkılıkta korunmalıdır (bkz. [ARCHITECTURE.md](ARCHITECTURE.md#güven-sınırları)).
- **Geri yükleme sonrası:** PITR sonrası hub restart'ı mevcut agent
  kimliklerini bozmaz; ancak yedekten sonra oluşmuş enrollment token'ları
  veya iş kayıtları kaybolmuş olabilir — kayıp aralığı için filo/iş
  geçmişini `/audit` ve `/fleet` üzerinden karşılaştır.
- **Memory modu (DATABASE_URL yok):** CA ve token durumu yalnızca süreç
  ömrüyle sınırlıdır; hub restart'ı tüm agent kimliklerini geçersiz kılar.
  Üretimde asla memory modunda çalıştırma.

## Bakım penceresi açma

Planlı bir kesinti veya yeniden başlatma öncesi ilgili alarmların
bastırılması için admin token'ıyla bakım penceresi oluştur:

```bash
curl -X POST https://hub.example.com/api/v1/maintenance-windows \
  -H "Authorization: Bearer $BAZUSOP_ADMIN_TOKEN" -H "Content-Type: application/json" \
  -d '{"name":"Planlı bakım","agent_id":"<opsiyonel-tek-agent>","starts_at":"2026-09-20T02:00:00Z","ends_at":"2026-09-20T04:00:00Z","created_by":"gokay"}'
```

`agent_id` boş bırakılırsa pencere tüm filoyu kapsar. Bakım penceresi
yalnız **yeni** olay açılışını bastırır; pencere başlamadan önce zaten açık
olan bir olayı otomatik kapatmaz — gerekiyorsa elle onayla.

## Token rotasyonu

1. Yeni token değerini üret (yüksek entropili, mevcut token'dan farklı).
2. Secret/env değerini güncelle, hub'ları rolling restart et.
3. Eski token'la yapılan istekler restart sonrası reddedilir; her rol için
   yeni token'la kontrollü bir test isteği (ör. bir olayı onaylamak için
   operator, bir alarm kuralı oluşturmak için admin) yap.
4. Enrollment token rotasyonu ayrıca tüketilmemiş eski token'ı otomatik iptal
   eder (bkz. §6.2); operator/admin token rotasyonu mevcut imzalı işleri
   etkilemez.

## Yük/regresyon doğrulaması

Canlı log fan-out'unun regresyon sınırını (1000 kayıtlık tek batch, 32
eşzamanlı abone, <1 saniye) ihlal ettiğinden şüpheleniyorsan:

```bash
GOCACHE=/tmp/bazusop-go-cache go test -race ./internal/logstream ./internal/storage/postgres
```

Compose ortamında uçtan uca smoke testi için `make smoke-compose` —
rastgele proje adı ve portla izole çalışır, bitince tüm container/network/
volume'ü otomatik temizler.
