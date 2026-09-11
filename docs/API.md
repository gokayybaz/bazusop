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
- Agent kimliği sertifikadaki `spiffe://bazusop/agent/{agent_id}` URI SAN
  değerinden alınır; istek gövdesinden agent ID kabul edilmez.

## Uçlar

### `GET /api/v1/health`

Liveness/readiness için süreç sağlığını verir:

```json
{"service":"bazusop-hub","status":"ok"}
```

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

### `GET /api/v1/instances/{agent_id}/telemetry`

Sorgu parametreleri:

- `from`: RFC3339 başlangıç; varsayılan son 24 saat.
- `to`: RFC3339 bitiş; varsayılan şu an.
- `limit`: `1–2000`; varsayılan `288`.

En yeni sınırlı pencere kronolojik sırada `samples` alanında, son örnek ayrıca
`latest` alanında döner. Veri yoksa `latest` değeri `null` olur.
