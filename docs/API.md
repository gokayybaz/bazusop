# bazUSOP API sözleşmesi

API tabanı `/api/v1` yoludur. JSON yanıtları UTF-8’dir; kimlik ve sertifika
yanıtları önbelleğe alınmaz. Bilinmeyen API yolları SPA’ye düşmek yerine `404`
döner.

## Kimlik doğrulama

- Enrollment, tek kullanımlık bootstrap token ve imzalı PKCS#10 CSR ister.
- Uzak enrollment yalnızca TLS üzerinden kabul edilir. Loopback HTTP geliştirme
  amacıyla açıktır.
- Renewal, inventory ve telemetry yazma uçları hub CA’sının doğruladığı mTLS
  client sertifikasını zorunlu tutar.
- İş oluşturma ve olay onaylama `Authorization: Bearer <operator-token>` ister;
  admin token bu işlemlerde de geçerlidir.
- Alarm kuralı, bakım penceresi ve bulut bağlantısı mutasyonları admin token ister.
  Geçerli operator token bu uçlarda `403`, bilinmeyen token `401` döner.
- Admin token tanımlanmamış eski kurulumlarda operator token iki rolü de taşır.
  Hiçbir yetkili token yapılandırılmamışsa mutasyon uçları `503` döner.
- İş teslim alma ve olay raporlama uçları mTLS agent kimliğini zorunlu tutar.
- Agent kimliği sertifikadaki `spiffe://bazusop/agent/{agent_id}` URI SAN
  değerinden alınır; istek gövdesinden agent ID kabul edilmez.

## Uçlar

### `GET /api/v1/health`

Liveness/readiness için süreç sağlığını verir:

```json
{"service":"bazusop-hub","status":"ok"}
```

### `GET /api/v1/system/configuration`

Secret içermeyen etkin çalışma ve saklama yapılandırmasını döndürür:

```json
{
  "storage": "postgresql",
  "timescale_enabled": true,
  "telemetry_retention_days": 30,
  "log_retention_days": 14,
  "version": "0.3.0",
  "commit": "abc123def456",
  "build_date": "2026-09-12T09:30:00Z"
}
```

Bu uç salt-okunurdur ve Ayarlar sayfasının çalışma modu ile deploy edilen build'i
doğrulamasını sağlar. Secret veya kullanıcı kimlik bilgisi içermez.

### `POST /api/v1/agents/enroll`

```json
{
  "bootstrap_token": "tek-kullanimlik-token",
  "name": "web-01",
  "operating_system": "linux",
  "csr": "-----BEGIN CERTIFICATE REQUEST-----..."
}
```

Başarıda `201` ile `agent_id`, 24 saatlik `certificate`, `ca_certificate` ve
`expires_at` döner. Hatalı token `401`, tüketilmiş token `409`, geçersiz CSR veya
OS ailesi `400` döndürür. Desteklenen OS aileleri `linux` ve `windows` değerleridir.
PostgreSQL modunda token'ın yalnız SHA-256 özeti saklanır ve tüketim replikalar
arasında atomiktir. Yeni kayıt token'ı, hub secret'ı değiştirilip replikalar
rolling restart edilerek kaydedilir; önceki tüketilmemiş token iptal edilir ve
tüketilmiş aynı değer yeniden etkinleşmez.

### `POST /api/v1/agents/renew`

mTLS gerekir. Yeni CSR karşılığında aynı SPIFFE agent ID’sine ait yeni sertifika
döndürür. Kimlik yoksa veya sertifika hub CA’sına doğrulanmıyorsa `401` döner.

### `PUT /api/v1/agents/inventory`

mTLS gerekir. Agent kimliği üzerinden idempotent upsert yapar.

```json
{
  "hostname": "web-01.example.com",
  "os_family": "linux",
  "os_name": "Ubuntu",
  "os_version": "24.04",
  "architecture": "amd64",
  "kernel_version": "6.8.0",
  "cpu_cores": 8,
  "memory_bytes": 17179869184,
  "ip_addresses": ["10.0.0.8"],
  "agent_version": "0.3.0"
}
```

Başarı `204` döner. Mimari adları `amd64`, `arm64` veya `386` olarak normalize
edilir; IP değerleri ayrıştırılır ve tekrarlar kaldırılır.

### `GET /api/v1/instances`

Normalize edilmiş fleet listesini `{ "instances": [...] }` zarfıyla döndürür.
Son raporu iki dakikadan yeni olan instance `connected`, diğerleri `stale` olur.

### `POST /api/v1/agents/telemetry`

mTLS gerekir. Yüzdeler `0–100` aralığında olmalıdır.

```json
{
  "recorded_at": "2026-09-11T04:05:00Z",
  "cpu_percent": 47.8,
  "memory_percent": 63.4,
  "disk_percent": 71.1,
  "network_rx_bytes": 2048,
  "network_tx_bytes": 1024
}
```

Aynı agent ve timestamp yeniden gönderilirse örnek idempotent biçimde güncellenir.
Başarı `204`, geçersiz örnek `400` döner.
Alarm değerlendirmesi yalnız agent'ın en yeni örneğinde yapılır; sonradan gelen
tarihsel örnek aktif olay yaşam döngüsünü geriye götürmez.

### `GET /api/v1/instances/{agent_id}/telemetry`

Sorgu parametreleri:

- `from`: RFC3339 başlangıç; varsayılan son 24 saat.
- `to`: RFC3339 bitiş; varsayılan şu an.
- `limit`: `1–2000`; varsayılan `288`.

En yeni sınırlı pencere kronolojik sırada `samples` alanında, son örnek ayrıca
`latest` alanında döner. Veri yoksa `latest` değeri `null` olur.

### `PUT /api/v1/agents/services`

mTLS gerekir. Her rapor agent'ın önceki servis listesini atomik olarak yeniler.
Agent, envanter raporunu servis snapshot'ından önce göndermelidir.

```json
{
  "observed_at": "2026-09-11T05:00:00Z",
  "services": [
    {
      "name": "nginx.service",
      "display_name": "NGINX Web Server",
      "state": "running",
      "startup_type": "enabled"
    }
  ]
}
```

Linux `active/inactive` ve Windows `started/stopped` durumları ortak
`running/stopped/failed/unknown` modeline çevrilir. `enabled/automatic`,
`manual/static`, `disabled/masked` başlangıç değerleri sırasıyla
`automatic`, `manual`, `disabled` olarak normalize edilir. Bir snapshot en fazla
5000 servis içerebilir. Başarı `204`, geçersiz veri `400` döner.

### `GET /api/v1/instances/{agent_id}/services`

Sunucunun son servis snapshot'ını ada göre sıralı döndürür:

```json
{"services":[{"agent_id":"agent-01","name":"nginx.service","display_name":"NGINX Web Server","state":"running","startup_type":"automatic","observed_at":"2026-09-11T05:00:00Z"}]}
```

İsteğe bağlı `state` parametresi `running`, `stopped`, `failed` veya `unknown`
değerini kabul eder. `q` parametresi servis adı ve görünen adda büyük/küçük harf
duyarsız arama yapar. Geçersiz filtre `400` döner.

### `POST /api/v1/agents/logs`

mTLS gerekir. Tek istekte 1–1000 kayıt kabul edilir; her mesaj en fazla 64 KiB'dir.

```json
{
  "entries": [
    {
      "occurred_at": "2026-09-11T05:10:00Z",
      "collector": "journald",
      "source": "nginx.service",
      "severity": "error",
      "message": "upstream timeout"
    }
  ]
}
```

`collector`; `journald`, `file` veya `windows_event` olur. Önem derecesi
`debug`, `info`, `warn`, `error` veya `critical` ortak değerine normalize edilir.
Hub her kayda benzersiz `id` ve doğrulanmış sertifikadan `agent_id` ekler. Başarı
`204`, geçersiz batch `400` döner.

### `GET /api/v1/instances/{agent_id}/logs`

Varsayılan olarak son bir saatin en yeni 100 kaydını kronolojik sırada döndürür.
Sorgu parametreleri:

- `from`, `to`: RFC3339 zaman aralığı.
- `limit`: `1–500` arası sonuç sınırı.
- `collector`: collector türü.
- `severity`: ortak önem derecesi.
- `source`: kaynak adında büyük/küçük harf duyarsız arama.
- `q`: mesaj metninde büyük/küçük harf duyarsız arama.

Yanıt `{ "entries": [...] }` zarfıdır. Aralık ve limit her sorguda doğrulanır.

### `GET /api/v1/instances/{agent_id}/logs/stream`

`text/event-stream` yanıtıyla canlı tail açar. Bağlantı önce `ready`, ardından her
yeni kayıt için `log` olayı gönderir. Her abonelik yalnız URL'deki agent kimliğinin
kayıtlarını alır. Proxy buffering kapatılmalı, istemci kopunca bağlantı iptal
edilmelidir. Ortak PostgreSQL kullanan hub replikaları yeni kayıt sinyalini
`LISTEN/NOTIFY` ile paylaşır; bildirim yalnız kayıt kimliklerini taşır, log içeriği
kalıcı tablodan okunur. Bellek store'u kullanılan geliştirme modu süreç içidir.

### `POST /api/v1/instances/{agent_id}/jobs`

Operatör veya yönetici bearer token'ı gerekir. İzin verilen aksiyonlar yalnız
`service.restart` ve `host.reboot` değerleridir.

```json
{
  "action": "service.restart",
  "target": "nginx.service",
  "approved_by": "gokay",
  "reason": "yapılandırma dağıtımı"
}
```

Servis restart için `target` zorunludur; reboot için boş olmalıdır. Başarı `201`
ile `queued` durumundaki işi, Ed25519 `signature` ve `signing_public_key`
alanlarıyla döndürür. Geçersiz talep `400` döner.

İmza kanonik v1 payload'ında `id`, `agent_id`, `action`, `target`,
`approved_by`, `reason` ve nanosaniye duyarlıklı RFC3339 `requested_at` alanlarını
kapsar. Agent, public key'i yanıtın kendisinden güvenilir kabul etmemeli; güvenli
enrollment/config kanalıyla sabitlenen hub anahtarıyla eşleştirmelidir.

### `GET /api/v1/instances/{agent_id}/jobs`

En yeni işleri `{ "jobs": [...] }` zarfında döndürür. `limit` değeri `1–200`
arasındadır; varsayılan `50` olur.

### `GET /api/v1/instances/{agent_id}/jobs/{job_id}/events`

İşin `approved`, `claimed`, `output`, `succeeded` veya `failed` olaylarını artan
sequence sırasıyla `{ "events": [...] }` zarfında döndürür. İş/agent eşleşmezse
`404` döner.

### `GET /api/v1/agents/jobs/next`

mTLS gerekir. Sertifikadaki agent için sıradaki işi atomik olarak teslim alır;
işi `running` durumuna geçirip `claimed` audit olayını yazar. İş yoksa `204`
döner.

### `POST /api/v1/agents/jobs/{job_id}/events`

mTLS gerekir. Agent yalnız kendi `running` işine bir sonraki sıralı olayı
ekleyebilir. Teslimden sonraki ilk sequence `2` olmalıdır. `output` işi açık
tutar; `succeeded` ve `failed` terminaldir. Sıra çakışması veya terminal işe
yazma denemesi `409` döner.

```json
{"sequence":2,"type":"output","message":"nginx durduruldu"}
```

### `GET|POST /api/v1/alert-rules`

Listeleme `{ "rules": [...] }` zarfıyla herkese açıktır; oluşturma yönetici bearer
token'ı ister. `metric` kuralları `cpu`, `memory` veya `disk` ve `1–100` eşiği;
`reachability` kuralları `60–86400` saniyelik `stale_after_seconds` değeri kabul
eder. Önem `warning` veya `critical` olur.

```json
{"name":"Yüksek CPU","kind":"metric","metric":"cpu","threshold":90,"stale_after_seconds":0,"severity":"critical","enabled":true}
```

### `GET|POST /api/v1/maintenance-windows`

Listeleme `{ "windows": [...] }` döner; oluşturma yönetici bearer token'ı ister.
`agent_id` boşsa pencere tüm agent'ları, doluysa yalnız ilgili agent'ı bastırır.
Başlangıç dahil, bitiş hariç zaman aralığında yeni olay açılmaz.

```json
{"name":"Kernel bakımı","agent_id":"agent-01","starts_at":"2026-09-11T21:00:00Z","ends_at":"2026-09-11T22:00:00Z","created_by":"gokay"}
```

### `GET /api/v1/incidents`

En yeni olayları `{ "incidents": [...] }` zarfında döndürür. `limit` değeri
`1–500`, varsayılan `100` olur. Durumlar `open`, `acknowledged`, `resolved`;
önemler `warning`, `critical` değerleridir.

### `POST /api/v1/incidents/{incident_id}/acknowledge`

Operatör veya yönetici bearer token'ı ve `{ "actor": "gokay" }` gövdesi ister. Yalnız açık olay
onaylanabilir; terminal/önceden onaylı olay `409`, bilinmeyen olay `404` döner.

### `GET /api/v1/incidents/{incident_id}/events`

Olayın `opened`, `acknowledged`, `resolved` yaşam döngüsünü zaman sırasıyla
`{ "events": [...] }` zarfında verir. Bilinmeyen olay `404` döner.

### `GET|POST /api/v1/cloud/accounts`

Listeleme `{ "accounts": [...] }` zarfıyla salt-okunurdur. Oluşturma yönetici
bearer token'ı ister; provider `aws`, `azure` veya `gcp` olmalıdır.

```json
{"name":"Üretim AWS","provider":"aws","external_id":"123456789012"}
```

Yeni hesap ilk discovery snapshot'ına kadar `pending`, başarılı uzlaştırmadan
sonra `connected` durumundadır. Aynı provider ve harici kimlik PostgreSQL'de
benzersizdir.

### `PUT /api/v1/cloud/accounts/{account_id}/instances`

Connector'ın bir hesap için gördüğü son tam snapshot'ı atomik olarak değiştirir
ve yönetici bearer token'ı ister. Bir snapshot en fazla 10.000 instance içerir.

```json
{
  "instances": [{
    "provider_instance_id": "i-0123",
    "name": "edge-01",
    "region": "eu-central-1",
    "zone": "eu-central-1a",
    "state": "running",
    "os_family": "linux",
    "private_ips": ["10.0.0.8"],
    "public_ips": [],
    "agent_id_hint": "agent-01"
  }]
}
```

`agent_id_hint` mevcut agent kimliğine tam uyuyorsa `verified` eşleşme oluşur.
Bu sinyal yokken tekil hostname veya özel IP benzerliği `candidate` olur ve
otomatik agent bağı kurulmaz. Diğer kayıtlar `unmatched` kalır.

### `GET /api/v1/cloud/instances`

Tüm hesapların son keşif sonucunu `{ "instances": [...] }` zarfıyla döndürür.
Her kayıt provider konumu, ağ adresleri, `match_status`, `match_reason`, doğrulanmış
`agent_id` veya inceleme amaçlı `candidate_agent_id` alanlarını içerir.
