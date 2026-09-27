# Filo Ortalama CPU Düzeltmesi Uygulama Planı

> **Otonom çalışanlar için:** ZORUNLU ALT-SKILL: Bu planı görev görev uygulamak için superpowers:subagent-driven-development (önerilen) veya superpowers:executing-plans kullan. Adımlar `- [ ]` checkbox söz dizimiyle takip edilir.

**Amaç:** Overview (Genel Bakış) sayfasındaki "Ortalama CPU" kutusu şu an tamamen sabit (hardcoded) bir değer gösteriyor (`value="42.8%"`, `internal/webui`'ye derlenen `web/src/pages/overview.tsx:27`) — filoda hiç cihaz olmasa bile bu sayı değişmeden görünüyor, çünkü hiçbir gerçek veriden hesaplanmıyor. Bu plan, bağlı cihazların en güncel telemetrisinden gerçek bir filo-geneli ortalama CPU hesaplayan bir backend ucu ekliyor ve bu kutuyu (ile "Sunucular"/"Açık alarmlar" kutularının zaten yaptığı gibi) gerçek veriye bağlıyor.

**Mimari:** `internal/telemetry`'ye yeni bir `Service.FleetAverage(ctx, scope, since)` metodu eklenir — bu, `internal/inventory`'nin "bağlı" tanımıyla aynı pencereyi (`ConnectedWindow`, dışa açılan sabit) kullanarak, her ajanın *o pencere içindeki en güncel* örneğini alıp CPU ortalamasını hesaplar (0 ajan varsa `DeviceCount=0` döner). Postgres store'da bu `DISTINCT ON (agent_id) ... WHERE recorded_at >= since` + `AVG(...)` ile tek sorguda yapılır — 13.5 spike'ında `ActiveTokensForSite`'ta kullanılan aynı desen. Yeni `GET /api/v1/telemetry/fleet-average` ucu, `internal/server/telemetry.go`'daki mevcut `handleTelemetryHistory` ile **aynı yetkilendirme desenini** izler: bu dosyadaki (ve `internal/server`'daki diğer birçok GET/liste ucundaki — `handleListInstances`, `handleListAlertRules`, `handleListIncidents` vb.) hiçbir okuma ucu `requirePermission` çağırmıyor; bu, kod tabanının önceden var olan, tutarlı bir mimari deseni (yalnız mutasyonlar RBAC ile korunuyor) — bu plan bunu değiştirmiyor, yalnız izliyor. Frontend'de `web/src/app-shell.tsx`'in mevcut `useEffect`'ine üçüncü bir fetch eklenir, sonucu `OverviewPage`'e prop olarak geçer; `overview.tsx`'teki sahte `Metric` gerçek veriye (ve cihaz yokken/veri yokken "—" göstermeye) bağlanır.

**Teknoloji yığını:** Go 1.26 (yeni domain metodu + HTTP ucu), React 19 + TypeScript (mevcut `OverviewPage`'in gerçek veriye bağlanması).

**Spec:** Bu, ayrı bir bug fix + küçük özellik — 13.x kimlik/RBAC spike serisinin bir parçası değil, bu yüzden ayrı bir tasarım spec'i yok. Kullanıcının doğrudan talebi: "Ortalama CPU filoda hiç cihaz olmamasına rağmen %42 gösteriyor" hatasının kök nedeni (`web/src/pages/overview.tsx:27`'deki sabit değer) giderilsin ve gerçek filo ortalaması hesaplansın.

## Genel Kısıtlar

- **Yeni `GET /api/v1/telemetry/fleet-average` ucuna `requirePermission` EKLENMEZ** — bu, `internal/server`'daki her GET/liste ucunun (telemetri, envanter, alarm kuralları, bakım pencereleri, olaylar) zaten izlediği tutarlı, önceden var olan bir desen; yalnız mutasyon uçları RBAC ile korunuyor. Bu planın kapsamı bu güvenlik modelini değiştirmek değil.
- **"Filo kaynak kullanımı" grafiği (`ResourceChart` fonksiyonu, `overview.tsx:65-84`) bu planın KAPSAMI DIŞINDA** — o da tamamen sahte/statik SVG eğrileri çiziyor ve aynı kök soruna sahip, ama kullanıcı yalnız "Ortalama CPU" sayısını düzeltmeyi istedi; grafiği gerçek veriye bağlamak ayrı bir iş (zaman serisi API'si + SVG path hesaplama gerektirir).
- **Sahte "trend" metni (`düne göre −%3,2`) gün-içi-karşılaştırma olarak yeniden üretilmez** — dünle bugünü karşılaştırmak ayrı bir zaman serisi sorgusu gerektirir (YAGNI); bunun yerine kutunun `trend` alanı, hesaplamanın hangi pencereden geldiğini dürüstçe gösterir ("Son 2 dakika").
- **`internal/inventory`'deki `connectedWindow` sabiti `ConnectedWindow` olarak dışa açılır (export)** — "bağlı" tanımının tek kaynağı orada kalır; `internal/telemetry` bu paketi import etmez (döngüsel bağımlılık yok, `internal/server` zaten her ikisini de import ediyor), yalnız `internal/server`'daki yeni handler `since` değerini hesaplarken `inventory.ConnectedWindow`'u kullanır.

## Dosya Yapısı

- Değiştir: `internal/inventory/inventory.go`.
- Değiştir: `internal/telemetry/telemetry.go`, `internal/telemetry/telemetry_test.go`.
- Değiştir: `internal/storage/postgres/telemetry.go`, `internal/storage/postgres/telemetry_integration_test.go` (yoksa oluştur — mevcut dosya adını Adım'da doğrula).
- Değiştir: `internal/server/telemetry.go`, `internal/server/server.go`, `internal/server/telemetry_test.go`.
- Değiştir: `web/src/types.ts`, `web/src/app-shell.tsx`, `web/src/pages/overview.tsx`.
- Oluştur/Değiştir: `web/src/app.test.tsx` (Overview testi zaten var — gerçek veriye göre güncellenecek).

## Görev 1: Backend — `inventory.ConnectedWindow` dışa açma + `telemetry.Service.FleetAverage`

**Dosyalar:**
- Değiştir: `internal/inventory/inventory.go`.
- Değiştir: `internal/telemetry/telemetry.go`, `internal/telemetry/telemetry_test.go`.
- Değiştir: `internal/storage/postgres/telemetry.go`.

**Arayüzler:**
- Üretir: `inventory.ConnectedWindow` (dışa açılmış sabit, `time.Duration`), `telemetry.FleetAverage{AverageCPUPercent float64; DeviceCount int}`, `telemetry.Store.FleetAverage(ctx, scope, since) (FleetAverage, error)`, `telemetry.Service.FleetAverage(ctx, scope) (FleetAverage, error)` — Görev 2'de HTTP katmanı tüketir.

- [ ] **Adım 1: `internal/inventory/inventory.go`'da `connectedWindow`'u dışa aç**

Dosyadaki mevcut sabit tanımını (`connectedWindow = 2 * time.Minute`) ve tek kullanım yerini (`hosts[index].Status = StatusConnected` bloğundaki karşılaştırma) bul, ikisini de `connectedWindow` → `ConnectedWindow` olacak şekilde değiştir. Sabitin üstüne kısa bir yorum ekle:

```go
	// ConnectedWindow is exported so internal/server can pass the same
	// "how fresh counts as connected" threshold to telemetry.Service.FleetAverage —
	// the fleet-average CPU stat should reflect exactly the same device set
	// the UI already calls "connected" here.
	ConnectedWindow = 2 * time.Minute
```

- [ ] **Adım 2: Build'i doğrula**

Çalıştır: `go build ./internal/inventory/... 2>&1 | head -30`
Beklenen: BAŞARILI

- [ ] **Adım 3: `internal/telemetry/telemetry.go`'ya `FleetAverage` tipini, `Store` arayüzüne metodu ve `Service.FleetAverage`'ı ekle**

`Sample` struct'ından hemen sonra ekle:

```go
// FleetAverage is the fleet-wide average CPU across every agent whose most
// recent sample falls within the requested window — DeviceCount is how many
// agents contributed (0 means no agent has reported recently, e.g. an empty
// fleet), so callers can distinguish "no data" from "0% average".
type FleetAverage struct {
	AverageCPUPercent float64
	DeviceCount       int
}
```

`Store` arayüzüne (`History` satırından hemen sonra) ekle:

```go
	FleetAverage(ctx context.Context, scope tenancy.Scope, since time.Time) (FleetAverage, error)
```

`Service.History`'den hemen sonra ekle:

```go
func (service *Service) FleetAverage(ctx context.Context, scope tenancy.Scope, since time.Time) (FleetAverage, error) {
	if err := scope.Validate(); err != nil {
		return FleetAverage{}, err
	}
	return service.store.FleetAverage(ctx, scope, since.UTC())
}
```

- [ ] **Adım 4: `internal/telemetry/telemetry.go`'daki `MemoryStore`'a `FleetAverage`'ı ekle**

`History` metodundan hemen sonra ekle:

```go
func (store *MemoryStore) FleetAverage(_ context.Context, scope tenancy.Scope, since time.Time) (FleetAverage, error) {
	store.mu.RLock()
	defer store.mu.RUnlock()

	var sum float64
	count := 0
	for agent, samples := range store.samples {
		if agent.OrganizationID != scope.OrganizationID || agent.SiteID != scope.SiteID || len(samples) == 0 {
			continue
		}
		latest := samples[len(samples)-1]
		if latest.RecordedAt.Before(since) {
			continue
		}
		sum += latest.CPUPercent
		count++
	}
	if count == 0 {
		return FleetAverage{}, nil
	}
	return FleetAverage{AverageCPUPercent: sum / float64(count), DeviceCount: count}, nil
}
```

(`store.samples[agent]` `Append`'te `RecordedAt`'a göre artan sırada tutuluyor — bu yüzden `samples[len(samples)-1]` her zaman o ajanın en güncel örneği. Yalnız o örnek `since`'den eskiyse ajan tamamen atlanıyor, "connected" tanımıyla birebir aynı mantık.)

- [ ] **Adım 5: Build'i doğrula**

Çalıştır: `go build ./internal/telemetry/... 2>&1 | head -30`
Beklenen: BAŞARILI

- [ ] **Adım 6: `internal/storage/postgres/telemetry.go`'ya `FleetAverage`'ı ekle**

`History` metodundan hemen sonra ekle:

```go
func (store *Store) FleetAverage(ctx context.Context, scope tenancy.Scope, since time.Time) (telemetry.FleetAverage, error) {
	if err := scope.Validate(); err != nil {
		return telemetry.FleetAverage{}, err
	}
	row := store.pool.QueryRow(ctx, `
		SELECT COALESCE(AVG(cpu_percent), 0), COUNT(*)
		FROM (
			SELECT DISTINCT ON (agent_id) cpu_percent
			FROM telemetry_samples
			WHERE organization_id = $1 AND site_id = $2 AND recorded_at >= $3
			ORDER BY agent_id, recorded_at DESC
		) latest`, scope.OrganizationID, scope.SiteID, since)
	var result telemetry.FleetAverage
	if err := row.Scan(&result.AverageCPUPercent, &result.DeviceCount); err != nil {
		return telemetry.FleetAverage{}, fmt.Errorf("query fleet average telemetry: %w", err)
	}
	return result, nil
}
```

(`DISTINCT ON (agent_id) ... ORDER BY agent_id, recorded_at DESC` her ajanın yalnız en güncel örneğini seçer — `internal/storage/postgres/serviceaccounts.go`'daki `ActiveTokensForSite`'la aynı desen. `WHERE recorded_at >= since` bu en güncel örneği pencere dışına düşen ajanları otomatik eler.)

- [ ] **Adım 7: Build'i doğrula**

Çalıştır: `go build ./internal/storage/postgres/... 2>&1 | head -30`
Beklenen: BAŞARILI

- [ ] **Adım 8: `internal/telemetry/telemetry_test.go`'ya yeni testler ekle**

Dosyanın sonuna ekle:

```go
func TestFleetAverageOnlyCountsAgentsWithARecentSample(t *testing.T) {
	t.Parallel()

	store := telemetry.NewMemoryStore()
	service := telemetry.NewService(store)
	scope := tenancy.DefaultScope()
	now := time.Date(2026, time.September, 27, 12, 0, 0, 0, time.UTC)

	if err := service.Report(context.Background(), tenancy.Agent{ID: "agent-fresh", OrganizationID: scope.OrganizationID, SiteID: scope.SiteID}, telemetry.Sample{RecordedAt: now, CPUPercent: 60, MemoryPercent: 50, DiskPercent: 50}); err != nil {
		t.Fatal(err)
	}
	if err := service.Report(context.Background(), tenancy.Agent{ID: "agent-stale", OrganizationID: scope.OrganizationID, SiteID: scope.SiteID}, telemetry.Sample{RecordedAt: now.Add(-10 * time.Minute), CPUPercent: 100, MemoryPercent: 50, DiskPercent: 50}); err != nil {
		t.Fatal(err)
	}
	if err := service.Report(context.Background(), tenancy.Agent{ID: "agent-other-site", OrganizationID: scope.OrganizationID, SiteID: "some-other-site"}, telemetry.Sample{RecordedAt: now, CPUPercent: 100, MemoryPercent: 50, DiskPercent: 50}); err != nil {
		t.Fatal(err)
	}

	average, err := service.FleetAverage(context.Background(), scope, now.Add(-2*time.Minute))
	if err != nil {
		t.Fatalf("fleet average: %v", err)
	}
	if average.DeviceCount != 1 || average.AverageCPUPercent != 60 {
		t.Fatalf("expected only the fresh, same-site agent counted (60%%, 1 device), got %#v", average)
	}
}

func TestFleetAverageReturnsZeroDevicesWhenFleetIsEmpty(t *testing.T) {
	t.Parallel()

	service := telemetry.NewService(telemetry.NewMemoryStore())
	average, err := service.FleetAverage(context.Background(), tenancy.DefaultScope(), time.Now().Add(-2*time.Minute))
	if err != nil {
		t.Fatalf("fleet average: %v", err)
	}
	if average.DeviceCount != 0 || average.AverageCPUPercent != 0 {
		t.Fatalf("expected a zero-value FleetAverage for an empty fleet, got %#v", average)
	}
}

func TestFleetAverageAveragesAcrossMultipleRecentAgents(t *testing.T) {
	t.Parallel()

	service := telemetry.NewService(telemetry.NewMemoryStore())
	scope := tenancy.DefaultScope()
	now := time.Date(2026, time.September, 27, 12, 0, 0, 0, time.UTC)

	for _, entry := range []struct {
		agent string
		cpu   float64
	}{{"agent-1", 40}, {"agent-2", 60}} {
		if err := service.Report(context.Background(), tenancy.Agent{ID: entry.agent, OrganizationID: scope.OrganizationID, SiteID: scope.SiteID}, telemetry.Sample{RecordedAt: now, CPUPercent: entry.cpu, MemoryPercent: 50, DiskPercent: 50}); err != nil {
			t.Fatal(err)
		}
	}

	average, err := service.FleetAverage(context.Background(), scope, now.Add(-2*time.Minute))
	if err != nil {
		t.Fatalf("fleet average: %v", err)
	}
	if average.DeviceCount != 2 || average.AverageCPUPercent != 50 {
		t.Fatalf("expected (40+60)/2 = 50%% across 2 devices, got %#v", average)
	}
}
```

- [ ] **Adım 9: Testleri çalıştır**

Çalıştır: `GOCACHE=/tmp/bazusop-go-cache go test ./internal/telemetry/... -run 'TestFleetAverage' -v 2>&1 | tail -60`
Beklenen: BAŞARILI (3 test)

- [ ] **Adım 10: `internal/storage/postgres/telemetry_integration_test.go`'u oluştur**

Bu dosya henüz yok (doğrulanmıştır — yalnız `internal/storage/postgres/telemetry.go` var, ona karşılık gelen bir entegrasyon test dosyası yok). Yeni dosyayı, `internal/storage/postgres/serviceaccounts_integration_test.go`'nun başlık deseniyle (paket adı, `os.Getenv("BAZUSOP_TEST_DATABASE_URL")` ile skip, `Open(ctx, databaseURL)`, benzersiz `suffix` ile org/site oluşturma, `defer` ile temizlik) birebir aynı şekilde oluştur:

```go
package postgres

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/gokayybaz/bazusop/internal/telemetry"
	"github.com/gokayybaz/bazusop/internal/tenancy"
)

func TestPostgresFleetAverageCountsOnlyRecentSamples(t *testing.T) {
	databaseURL := os.Getenv("BAZUSOP_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("set BAZUSOP_TEST_DATABASE_URL to run PostgreSQL integration tests")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	store, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	suffix := time.Now().UTC().Format("20060102150405.000000000")
	orgID := "fleet-avg-org-" + suffix
	siteID := "fleet-avg-site-" + suffix
	if _, err := store.pool.Exec(ctx, `INSERT INTO organizations (id, name) VALUES ($1, 'Fleet average test')`, orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.pool.Exec(ctx, `INSERT INTO sites (id, organization_id, name, slug) VALUES ($1,$2,'Fleet avg site','fleet-avg-site')`, siteID, orgID); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		store.pool.Exec(cleanupCtx, "DELETE FROM telemetry_samples WHERE organization_id=$1", orgID)
		store.pool.Exec(cleanupCtx, "DELETE FROM sites WHERE id=$1", siteID)
		store.pool.Exec(cleanupCtx, "DELETE FROM organizations WHERE id=$1", orgID)
	}()

	scope := tenancy.Scope{OrganizationID: orgID, SiteID: siteID}
	now := time.Now().UTC()
	if err := store.Append(ctx, telemetry.Sample{OrganizationID: orgID, SiteID: siteID, AgentID: "fresh-agent", RecordedAt: now, CPUPercent: 60, MemoryPercent: 50, DiskPercent: 50}); err != nil {
		t.Fatalf("append fresh sample: %v", err)
	}
	if err := store.Append(ctx, telemetry.Sample{OrganizationID: orgID, SiteID: siteID, AgentID: "stale-agent", RecordedAt: now.Add(-10 * time.Minute), CPUPercent: 100, MemoryPercent: 50, DiskPercent: 50}); err != nil {
		t.Fatalf("append stale sample: %v", err)
	}

	average, err := store.FleetAverage(ctx, scope, now.Add(-2*time.Minute))
	if err != nil {
		t.Fatalf("fleet average: %v", err)
	}
	if average.DeviceCount != 1 || average.AverageCPUPercent != 60 {
		t.Fatalf("expected only the fresh agent counted (60%%, 1 device), got %#v", average)
	}
}
```

- [ ] **Adım 11: Testleri çalıştır (Postgres gerektirir)**

```bash
docker rm -f bazusop-test-pg-fleetavg >/dev/null 2>&1
docker run -d --name bazusop-test-pg-fleetavg -e POSTGRES_USER=bazusop -e POSTGRES_PASSWORD=bazusop_test -e POSTGRES_DB=bazusop_test -p 5432:5432 postgres:18 >/dev/null
for i in $(seq 1 30); do docker exec bazusop-test-pg-fleetavg pg_isready -U bazusop -d bazusop_test >/dev/null 2>&1 && break; sleep 1; done
BAZUSOP_TEST_DATABASE_URL='postgres://bazusop:bazusop_test@127.0.0.1:5432/bazusop_test?sslmode=disable' GOCACHE=/tmp/bazusop-go-cache go test ./internal/storage/postgres/... -run 'TestPostgresFleetAverage' -v 2>&1 | tail -30
docker rm -f bazusop-test-pg-fleetavg >/dev/null 2>&1
```

Beklenen: BAŞARILI

- [ ] **Adım 12: Tam paket testlerini çalıştır (Postgres olmadan)**

Çalıştır: `go vet ./internal/inventory/... ./internal/telemetry/... ./internal/storage/postgres/... && gofmt -l internal/inventory/*.go internal/telemetry/*.go internal/storage/postgres/*.go && GOCACHE=/tmp/bazusop-go-cache go test ./internal/inventory/... ./internal/telemetry/... 2>&1 | tail -10`
Beklenen: vet/gofmt çıktısı yok; `ok`

- [ ] **Adım 13: Commit**

```bash
git add internal/inventory/inventory.go internal/telemetry/telemetry.go internal/telemetry/telemetry_test.go internal/storage/postgres/telemetry.go internal/storage/postgres/*telemetry*integration*
git commit -m "feat: add telemetry.Service.FleetAverage for a real fleet-wide CPU average"
```

## Görev 2: Backend — `GET /api/v1/telemetry/fleet-average` HTTP ucu

**Dosyalar:**
- Değiştir: `internal/server/telemetry.go`, `internal/server/server.go`, `internal/server/telemetry_test.go`.

**Arayüzler:**
- Üretir: `GET /api/v1/telemetry/fleet-average` → `{"average_cpu_percent": number, "device_count": number}`. `handleTelemetryHistory` ile aynı desen: oturum/izin kontrolü yok (bkz. Genel Kısıtlar).

- [ ] **Adım 1: `internal/server/telemetry.go`'ya `handleFleetAverageTelemetry`'yi ekle**

Dosyanın başına `"github.com/gokayybaz/bazusop/internal/inventory"` import'unu ekle (alfabetik sırayla, `enrollment`'tan sonra). `handleTelemetryHistory`'den hemen sonra ekle:

```go
func handleFleetAverageTelemetry(service *telemetry.Service, scope tenancy.Scope) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		since := time.Now().UTC().Add(-inventory.ConnectedWindow)
		average, err := service.FleetAverage(request.Context(), scope, since)
		if err != nil {
			http.Error(response, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
			return
		}
		writeJSON(response, http.StatusOK, struct {
			AverageCPUPercent float64 `json:"average_cpu_percent"`
			DeviceCount       int     `json:"device_count"`
		}{average.AverageCPUPercent, average.DeviceCount})
	}
}
```

- [ ] **Adım 2: `internal/server/server.go`'ya route'u ekle**

`if configuration.telemetryService != nil { ... }` bloğunun içine, `handleTelemetryHistory` satırından hemen sonra ekle:

```go
		registerAudited(mux, "/api/v1/telemetry/fleet-average", http.MethodGet, "telemetry", nil, configuration.auditTrail, configuration.scope, handleFleetAverageTelemetry(configuration.telemetryService, configuration.scope))
```

- [ ] **Adım 3: Build'i doğrula**

Çalıştır: `go build ./... 2>&1 | head -30 && echo BUILD_OK`
Beklenen: `BUILD_OK`

- [ ] **Adım 4: `internal/server/telemetry_test.go`'ya yeni testler ekle**

Dosyanın sonuna ekle:

```go
func TestFleetAverageEndpointReflectsRecentTelemetryOnly(t *testing.T) {
	t.Parallel()

	service := telemetry.NewService(telemetry.NewMemoryStore())
	handler := server.NewHandler(
		server.WithDefaultScope(tenancy.DefaultScope()),
		server.WithTelemetry(service),
	)
	scope := tenancy.DefaultScope()
	now := time.Now().UTC()

	if err := service.Report(t.Context(), tenancy.Agent{ID: "fresh-agent", OrganizationID: scope.OrganizationID, SiteID: scope.SiteID}, telemetry.Sample{RecordedAt: now, CPUPercent: 80, MemoryPercent: 50, DiskPercent: 50}); err != nil {
		t.Fatal(err)
	}
	if err := service.Report(t.Context(), tenancy.Agent{ID: "stale-agent", OrganizationID: scope.OrganizationID, SiteID: scope.SiteID}, telemetry.Sample{RecordedAt: now.Add(-10 * time.Minute), CPUPercent: 20, MemoryPercent: 50, DiskPercent: 50}); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/telemetry/fleet-average", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}
	var payload struct {
		AverageCPUPercent float64 `json:"average_cpu_percent"`
		DeviceCount       int     `json:"device_count"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode fleet average: %v", err)
	}
	if payload.DeviceCount != 1 || payload.AverageCPUPercent != 80 {
		t.Fatalf("expected only the fresh agent counted (80%%, 1 device), got %#v", payload)
	}
}

func TestFleetAverageEndpointReturnsZeroDevicesForAnEmptyFleet(t *testing.T) {
	t.Parallel()

	handler := server.NewHandler(
		server.WithDefaultScope(tenancy.DefaultScope()),
		server.WithTelemetry(telemetry.NewService(telemetry.NewMemoryStore())),
	)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/telemetry/fleet-average", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}
	var payload struct {
		AverageCPUPercent float64 `json:"average_cpu_percent"`
		DeviceCount       int     `json:"device_count"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode fleet average: %v", err)
	}
	if payload.DeviceCount != 0 || payload.AverageCPUPercent != 0 {
		t.Fatalf("expected a zero-value response for an empty fleet, got %#v", payload)
	}
}
```

(Bu iki test yalnız `server.WithTelemetry`/`server.WithDefaultScope` kullanıyor — `TestAgentTelemetryReachesInstanceHistory`'nin izlediği minimal kurulumla aynı, çünkü bu uç `requirePermission` çağırmıyor; `identity`/`sessions`/`authorization` kurulumuna gerek yok.)

- [ ] **Adım 5: Testi çalıştır**

Çalıştır: `GOCACHE=/tmp/bazusop-go-cache go test ./internal/server/... -run 'TestFleetAverage' -v 2>&1 | tail -40`
Beklenen: BAŞARILI (2 test)

- [ ] **Adım 6: Tam paket testlerini çalıştır**

Çalıştır: `go vet ./internal/server/... && gofmt -l internal/server/*.go && GOCACHE=/tmp/bazusop-go-cache go test ./internal/server/... 2>&1 | tail -10`
Beklenen: vet/gofmt çıktısı yok; `ok`

- [ ] **Adım 7: Commit**

```bash
git add internal/server/telemetry.go internal/server/server.go internal/server/telemetry_test.go
git commit -m "feat: add GET /api/v1/telemetry/fleet-average"
```

## Görev 3: Frontend — Overview'daki "Ortalama CPU" kutusunu gerçek veriye bağla

**Dosyalar:**
- Değiştir: `web/src/types.ts`, `web/src/app-shell.tsx`, `web/src/pages/overview.tsx`, `web/src/app.test.tsx`.

**Arayüzler:**
- Tüketir: `GET /api/v1/telemetry/fleet-average` → `{average_cpu_percent, device_count}` (Görev 2).
- Üretir: `OverviewPage`'e iki yeni prop — `fleetTelemetry: FleetTelemetryAverage | null`, `fleetTelemetryState: "loading" | "ready" | "error"`.

- [ ] **Adım 1: `web/src/types.ts`'e yeni tipi ekle**

Dosyanın sonuna ekle:

```ts
export type FleetTelemetryAverage = { average_cpu_percent: number; device_count: number }
```

- [ ] **Adım 2: `web/src/app-shell.tsx`'e fetch'i ve state'i ekle**

`AlertIncident, InventoryInstance, LogEntry, ManagedService, OperationJob, PageID, TelemetryPayload` import satırına `FleetTelemetryAverage`'ı ekle (alfabetik sırayla):

```ts
import type { AlertIncident, FleetTelemetryAverage, InventoryInstance, LogEntry, ManagedService, OperationJob, PageID, TelemetryPayload } from "./types"
```

`const [alertState, setAlertState] = useState<"loading" | "ready" | "error">("loading")` satırından hemen sonra ekle:

```ts
  const [fleetTelemetry, setFleetTelemetry] = useState<FleetTelemetryAverage | null>(null)
  const [fleetTelemetryState, setFleetTelemetryState] = useState<"loading" | "ready" | "error">("loading")
```

Mevcut `useEffect`'in içine, `fetch("/api/v1/incidents?limit=100", ...)` bloğundan hemen sonra (aynı `useEffect`, aynı `controller`) ekle:

```ts
    fetch("/api/v1/telemetry/fleet-average", { signal: controller.signal })
      .then((response) => {
        if (!response.ok) throw new Error("fleet telemetry unavailable")
        return response.json() as Promise<FleetTelemetryAverage>
      })
      .then((payload) => {
        setFleetTelemetry(payload)
        setFleetTelemetryState("ready")
      })
      .catch((error: unknown) => {
        if (error instanceof DOMException && error.name === "AbortError") return
        setFleetTelemetryState("error")
      })
```

`{activePage === "overview" ? <OverviewPage ... /> : null}` satırını şununla değiştir (iki yeni prop ekleniyor):

```tsx
            {activePage === "overview" ? <OverviewPage activeIncidents={activeIncidents} alertState={alertState} fleetTelemetry={fleetTelemetry} fleetTelemetryState={fleetTelemetryState} instances={instances} inventoryState={inventoryState} onOpenAlarmCenter={() => navigate("alerts")} /> : null}
```

- [ ] **Adım 3: `web/src/pages/overview.tsx`'i değiştir**

Tüm dosyanın içeriğini şununla değiştir (yalnız `import`lar, `OverviewPage`'in prop tipi/imzası ve "Ortalama CPU" `Metric` çağrısı değişiyor — `ResourceChart`, `Metric`, `Alert` fonksiyonları ve dosyanın geri kalanı aynı kalıyor):

```tsx
import { ChevronRight, CircleAlert } from "lucide-react"

import { Badge } from "../components/ui/badge"
import { Card } from "../components/ui/card"
import { formatLastSeen } from "../lib/format"
import type { AlertIncident, FleetTelemetryAverage, InventoryInstance } from "../types"

export function OverviewPage({ inventoryState, instances, alertState, activeIncidents, fleetTelemetry, fleetTelemetryState, onOpenAlarmCenter }: {
  inventoryState: "loading" | "ready" | "error"
  instances: InventoryInstance[]
  alertState: "loading" | "ready" | "error"
  activeIncidents: AlertIncident[]
  fleetTelemetry: FleetTelemetryAverage | null
  fleetTelemetryState: "loading" | "ready" | "error"
  onOpenAlarmCenter: () => void
}) {
  const connectedInstances = instances.filter((instance) => instance.status === "connected").length
  const criticalIncidents = activeIncidents.filter((incident) => incident.severity === "critical").length

  const cpuValue = fleetTelemetryState === "ready" && fleetTelemetry && fleetTelemetry.device_count > 0 ? `${fleetTelemetry.average_cpu_percent.toFixed(1)}%` : "—"
  const cpuDetail = fleetTelemetryState === "error" ? "Telemetriye ulaşılamıyor" : fleetTelemetryState === "ready" && fleetTelemetry && fleetTelemetry.device_count === 0 ? "Filoda aktif cihaz yok" : fleetTelemetryState === "ready" && fleetTelemetry ? `${fleetTelemetry.device_count} bağlı cihazdan hesaplandı` : "Filo telemetrisi yükleniyor"
  const cpuTrend = fleetTelemetryState === "ready" && fleetTelemetry && fleetTelemetry.device_count > 0 ? "Son 2 dakika" : "Veri yok"

  return (
    <>
      <section className="stat-grid" aria-label="Filo özeti">
        <Metric
          label="Sunucular"
          value={inventoryState === "loading" ? "—" : String(instances.length)}
          detail={inventoryState === "error" ? "Envantere ulaşılamıyor" : `${connectedInstances} bağlı`}
          trend={inventoryState === "ready" ? "Canlı envanter" : "Hub bekleniyor"}
        />
        <Metric label="Ortalama CPU" value={cpuValue} detail={cpuDetail} trend={cpuTrend} />
        <Metric label="Açık alarmlar" value={alertState === "loading" ? "—" : String(activeIncidents.length)} detail={`${criticalIncidents} kritik alarm`} trend={`${activeIncidents.filter((incident) => incident.status === "acknowledged").length} alarm onaylandı`} alert />
      </section>

      <div className="dashboard-grid">
        <Card className="chart-card">
          <div className="card-header">
            <div><h2>Filo kaynak kullanımı</h2><p>CPU ve bellek · son 24 saat</p></div>
            <div className="chart-legend"><span className="cpu">CPU</span><span className="memory">Bellek</span></div>
          </div>
          <ResourceChart />
        </Card>

        <Card aria-label="Operasyon alarmları" className="alerts-card">
          <div className="card-header"><div><h2>Operasyon alarmları</h2><p>İlgilenilmesi gereken sinyaller</p></div><Badge className="critical-count">{activeIncidents.length} açık</Badge></div>
          <div className="alert-list">
            {activeIncidents.slice(0, 3).map((incident) => <Alert host={incident.agent_id} key={incident.id} level={incident.severity === "critical" ? "Kritik" : "Uyarı"} meta={`${incident.message} · ${formatLastSeen(incident.opened_at)}`} title={incident.rule_name} />)}
            {alertState === "loading" ? <div className="alert-empty">Alarmlar yükleniyor…</div> : null}
            {alertState === "error" ? <div className="alert-empty">Alarm verisine ulaşılamıyor.</div> : null}
            {alertState === "ready" && activeIncidents.length === 0 ? <div className="alert-empty">Açık alarm yok.</div> : null}
          </div>
          <button aria-label="Alarm merkezini aç" className="panel-link" onClick={onOpenAlarmCenter} type="button">Alarm merkezini aç <ChevronRight size={15} /></button>
        </Card>
      </div>
    </>
  )
}

function Metric({ label, value, detail, trend, alert = false }: { label: string; value: string; detail: string; trend: string; alert?: boolean }) {
  return (
    <Card className={alert ? "metric-card metric-alert" : "metric-card"}>
      <span className="metric-label">{label}</span>
      <div className="metric-value"><strong>{value}</strong>{alert ? <span className="alert-pip">1 kritik</span> : null}</div>
      <div className="metric-detail"><span>{detail}</span><span className={alert ? "trend alert" : "trend"}>{trend}</span></div>
    </Card>
  )
}

function ResourceChart() {
  return (
    <div className="chart-wrap">
      <div className="chart-scale"><span>100%</span><span>75%</span><span>50%</span><span>25%</span><span>0%</span></div>
      <svg aria-label="Son 24 saatte filo kaynak kullanımı" className="resource-chart" role="img" viewBox="0 0 800 250">
        <defs>
          <linearGradient id="cpu-area" x1="0" x2="0" y1="0" y2="1"><stop offset="0" stopColor="var(--accent)" stopOpacity=".28"/><stop offset="1" stopColor="var(--accent)" stopOpacity="0"/></linearGradient>
          <linearGradient id="memory-area" x1="0" x2="0" y1="0" y2="1"><stop offset="0" stopColor="var(--success)" stopOpacity=".14"/><stop offset="1" stopColor="var(--success)" stopOpacity="0"/></linearGradient>
        </defs>
        {[20, 72, 124, 176, 228].map((y) => <line className="grid-line" key={y} x1="0" x2="800" y1={y} y2={y} />)}
        <path className="memory-area" d="M0 154 C65 140 94 158 142 133 S230 118 282 139 S360 103 419 116 S492 84 550 101 S628 69 682 92 S742 63 800 77 L800 228 L0 228 Z" />
        <path className="memory-line" d="M0 154 C65 140 94 158 142 133 S230 118 282 139 S360 103 419 116 S492 84 550 101 S628 69 682 92 S742 63 800 77" />
        <path className="cpu-area" d="M0 184 C48 172 78 190 116 163 S188 146 228 158 S288 117 334 139 S411 84 452 112 S514 92 558 119 S620 75 659 97 S724 54 753 82 S783 60 800 68 L800 228 L0 228 Z" />
        <path className="cpu-line" d="M0 184 C48 172 78 190 116 163 S188 146 228 158 S288 117 334 139 S411 84 452 112 S514 92 558 119 S620 75 659 97 S724 54 753 82 S783 60 800 68" />
        <circle className="chart-point" cx="800" cy="68" r="4" />
      </svg>
      <div className="chart-axis"><span>00:00</span><span>04:00</span><span>08:00</span><span>12:00</span><span>16:00</span><span>20:00</span><span>Şimdi</span></div>
    </div>
  )
}

function Alert({ level, title, host, meta }: { level: "Kritik" | "Uyarı"; title: string; host: string; meta: string }) {
  return (
    <div className={level === "Kritik" ? "alert-item critical" : "alert-item warning-item"}>
      <CircleAlert size={18} />
      <div><span className="alert-level">{level}</span><strong>{title}</strong><span className="alert-host">{host}</span><small>{meta}</small></div>
    </div>
  )
}
```

- [ ] **Adım 4: `web/src/app.test.tsx`'i güncelle**

Dosyanın en üstünde (satır 1-23) `respondWithSession` adlı bir yardımcı fonksiyon ve bunu kullanan bir `beforeEach` var:

```ts
function respondWithSession(url: string) {
  return url.endsWith("/api/v1/session") ? Promise.resolve({ ok: true, json: async () => authenticatedWhoAmI } as Response) : null
}

describe("bazUSOP shell", () => {
  beforeEach(() => {
    window.localStorage.clear()
    window.history.replaceState({}, "", "/")
    delete document.documentElement.dataset.theme
    vi.stubGlobal("fetch", vi.fn().mockImplementation((input: RequestInfo | URL) => {
      const url = String(input)
      return respondWithSession(url) ?? Promise.resolve({ ok: true, json: async () => ({ instances: [] }) } as Response)
    }))
    vi.stubGlobal("scrollTo", vi.fn())
  })
```

Bu `beforeEach`, kendi `vi.stubGlobal("fetch", ...)`'ini çağırmayan HER testin (aşağıdaki "presents the unified operations overview" testi dahil) varsayılan mock'u — `AppShell`'in `useEffect`'i her sayfada çalıştığı için bu test de `/api/v1/telemetry/fleet-average`'a bir istek atacak, ama şu an bu URL'e özel bir dal yok, jenerik `{ instances: [] }` fallback'i dönüyor (yanlış şekil ama hata vermiyor).

`respondWithSession` fonksiyonundan hemen sonra yeni bir yardımcı ekle:

```ts
function respondWithFleetAverage(url: string) {
  return url.endsWith("/api/v1/telemetry/fleet-average") ? Promise.resolve({ ok: true, json: async () => ({ average_cpu_percent: 47.8, device_count: 2 }) } as Response) : null
}
```

`beforeEach`'in içindeki `vi.stubGlobal("fetch", ...)` çağrısını şununla değiştir (yalnız `respondWithFleetAverage(url) ??` ekleniyor):

```ts
    vi.stubGlobal("fetch", vi.fn().mockImplementation((input: RequestInfo | URL) => {
      const url = String(input)
      return respondWithSession(url) ?? respondWithFleetAverage(url) ?? Promise.resolve({ ok: true, json: async () => ({ instances: [] }) } as Response)
    }))
```

Mevcut Overview testinin (`it("presents the unified operations overview", ...)`) `expect(screen.getByText("Ortalama CPU")).toBeInTheDocument()` satırından hemen sonra ekle:

```ts
    expect(await screen.findByText("47.8%")).toBeInTheDocument()
```

- [ ] **Adım 5: Testleri çalıştır**

```bash
cd web
npm test 2>&1 | tail -60
```

Beklenen: BAŞARILI (mevcut testler + değişenler)

- [ ] **Adım 6: Sıfır cihaz durumunu doğrulayan yeni bir test ekle**

`it("presents the unified operations overview", ...)` testinin hemen ardından, aynı `describe` bloğunun içine yeni bir `it` ekle. Bu test kendi `vi.stubGlobal("fetch", ...)`'ini çağırarak `beforeEach`'in varsayılanını geçersiz kılar (yalnız bu testin süresince) — `/api/v1/telemetry/fleet-average` sıfır cihazlı bir yanıt dönerken diğer her şey (`/api/v1/instances` dahil) `beforeEach`'teki aynı jenerik `{ instances: [] }` fallback'ini kullanır:

```ts
  it("shows no data instead of a fake average when the fleet has no devices", async () => {
    vi.stubGlobal("fetch", vi.fn().mockImplementation((input: RequestInfo | URL) => {
      const url = String(input)
      const session = respondWithSession(url)
      if (session) return session
      if (url.endsWith("/api/v1/telemetry/fleet-average")) return Promise.resolve({ ok: true, json: async () => ({ average_cpu_percent: 0, device_count: 0 }) } as Response)
      return Promise.resolve({ ok: true, json: async () => ({ instances: [] }) } as Response)
    }))

    render(<App />)

    expect(await screen.findByText("Filoda aktif cihaz yok")).toBeInTheDocument()
    expect(screen.queryByText("47.8%")).not.toBeInTheDocument()
    expect(screen.queryByText("42.8%")).not.toBeInTheDocument()
  })
```

- [ ] **Adım 7: Testleri çalıştır**

```bash
cd web
npm test 2>&1 | tail -60
```

Beklenen: BAŞARILI

- [ ] **Adım 8: Tip kontrolü + production build**

```bash
cd web
npm run build 2>&1 | tail -30
```

Beklenen: BAŞARILI

- [ ] **Adım 9: Commit**

```bash
git add web/src/types.ts web/src/app-shell.tsx web/src/pages/overview.tsx web/src/app.test.tsx
git commit -m "fix: compute the Ortalama CPU stat from real fleet telemetry instead of a hardcoded value"
```

## Görev 4: Docker Compose ile uçtan uca manuel doğrulama

**Dosyalar:** yok (yalnız doğrulama).

- [ ] **Adım 1: Docker Compose ile hub'ı yeniden derleyip ayağa kaldır**

Çalıştır: `docker compose down -v >/dev/null 2>&1; BAZUSOP_PORT=8091 BAZUSOP_BOOTSTRAP_SECRET=verify-bootstrap BAZUSOP_TOTP_ENCRYPTION_KEY=verify-totp-key docker compose up --build -d 2>&1 | tail -30`

- [ ] **Adım 2: Sağlık kontrolünü bekle**

Çalıştır: `for i in $(seq 1 20); do curl -s -o /dev/null -w "%{http_code}" http://127.0.0.1:8091/api/v1/health | grep -q 200 && echo healthy && break; sleep 1; done`

- [ ] **Adım 3: Tek bir Python betiğiyle bootstrap → giriş → boş filoda `/api/v1/telemetry/fleet-average`'ı çek → telemetri raporla → tekrar çek — hepsi gerçek bir HTTP istemcisiyle**

```bash
python3 -c "
import base64, hashlib, hmac, http.cookiejar, json, struct, time, urllib.parse, urllib.request

cookie_jar = http.cookiejar.CookieJar()
opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(cookie_jar))

def call(method, path, body=None, extra_headers=None):
    data = json.dumps(body).encode() if body is not None else None
    headers = {'Content-Type': 'application/json'}
    headers.update(extra_headers or {})
    request = urllib.request.Request('http://127.0.0.1:8091' + path, data=data, headers=headers, method=method)
    with opener.open(request) as response:
        raw = response.read()
        return json.loads(raw) if raw else None

def totp_code(secret_b32, at=None):
    at = at or time.time()
    key = base64.b32decode(secret_b32 + '=' * ((8 - len(secret_b32) % 8) % 8))
    counter = struct.pack('>Q', int(at // 30))
    digest = hmac.new(key, counter, hashlib.sha1).digest()
    offset = digest[-1] & 0x0F
    code = (struct.unpack('>I', digest[offset:offset+4])[0] & 0x7fffffff) % 1000000
    return f'{code:06d}'

def secret_from_uri(uri):
    return urllib.parse.parse_qs(urllib.parse.urlparse(uri).query)['secret'][0]

bootstrap_payload = call('POST', '/api/v1/bootstrap', {'secret': 'verify-bootstrap', 'email': 'admin@example.com', 'password': 'correct horse battery staple'})
admin_secret = secret_from_uri(bootstrap_payload['provisioning_uri'])
call('POST', '/api/v1/sessions', {'email': 'admin@example.com', 'password': 'correct horse battery staple', 'totp_code': totp_code(admin_secret)})

empty_average = call('GET', '/api/v1/telemetry/fleet-average')
print('bos filoda ortalama:', empty_average['average_cpu_percent'], '| cihaz sayisi:', empty_average['device_count'])

instances = call('GET', '/api/v1/instances')
print('filodaki cihaz sayisi (envanter):', len(instances['instances']))
"
```

Beklenen çıktı: "bos filoda ortalama: 0 | cihaz sayisi: 0" ve "filodaki cihaz sayisi (envanter): 0" — yani hem envanter hem de yeni ortalama CPU ucu, boş bir filoda tutarlı şekilde "veri yok" bildiriyor (eski `42.8%` sabiti artık hiç görünmüyor).

- [ ] **Adım 4: Tarayıcıdan görsel doğrulama**

`http://127.0.0.1:8091` adresini aç, yukarıdaki `admin@example.com` / `correct horse battery staple` ile (TOTP kodu gerekecek — kurulum sırasında gösterilen QR'ı okutarak) giriş yap, Genel Bakış sayfasını görüntüle. Filo boş olduğu için "Ortalama CPU" kutusunda "—" ve "Filoda aktif cihaz yok" yazmalı, `%42,8` gibi hiçbir sabit değer görünmemeli.

- [ ] **Adım 5: Temizlik**

```bash
docker compose down -v
```

- [ ] **Adım 6: Go ve frontend testlerini son kez birlikte çalıştır (gerçek Postgres ile)**

```bash
docker rm -f bazusop-test-pg-fleetavgb >/dev/null 2>&1
docker run -d --name bazusop-test-pg-fleetavgb -e POSTGRES_USER=bazusop -e POSTGRES_PASSWORD=bazusop_test -e POSTGRES_DB=bazusop_test -p 5432:5432 postgres:18 >/dev/null
for i in $(seq 1 30); do docker exec bazusop-test-pg-fleetavgb pg_isready -U bazusop -d bazusop_test >/dev/null 2>&1 && break; sleep 1; done
go build ./... && go vet ./... && gofmt -l .
BAZUSOP_TEST_DATABASE_URL='postgres://bazusop:bazusop_test@127.0.0.1:5432/bazusop_test?sslmode=disable' GOCACHE=/tmp/bazusop-go-cache go test ./... -count=1 2>&1 | tail -30
docker rm -f bazusop-test-pg-fleetavgb >/dev/null 2>&1
cd web && npm test 2>&1 | tail -50 && npm run build 2>&1 | tail -20
```

Beklenen: hepsi BAŞARILI

- [ ] **Adım 7: Commit**

```bash
git add -A
git commit -m "build: embed the fleet-average-cpu frontend bundle"
```

## Kendi Kendine İnceleme

**1. Kök neden kapsaması.** Kullanıcının bildirdiği hata ("filoda hiçbir cihaz olmamasına rağmen Ortalama CPU %42 gösteriyor") → kök neden `overview.tsx:27`'deki sabit `value="42.8%"` idi. Görev 3 bu sabiti tamamen kaldırıp gerçek `fleetTelemetry`/`fleetTelemetryState` prop'larından hesaplanan bir değerle değiştiriyor; sıfır cihazlı durumda `device_count === 0` kontrolü "—" gösteriyor (Görev 4 Adım 3, gerçek bir HTTP istemcisiyle boş filoda bunu kanıtlıyor).

**2. Placeholder taraması.** Her adımda gerçek, eksiksiz kod var. Yazarken iki nokta önceden doğrulandı (placeholder bırakılmadı): Görev 1 Adım 10'da `internal/storage/postgres/telemetry_integration_test.go`'un henüz var olmadığı `ls` ile teyit edildi (dosya `package postgres` başlığıyla sıfırdan oluşturuluyor); Görev 3 Adım 4/6'da `web/src/app.test.tsx`'in gerçek `beforeEach`/`respondWithSession` yapısı okunup ona göre birebir kod yazıldı.

**3. Tip tutarlılığı.** Backend: `telemetry.FleetAverage{AverageCPUPercent, DeviceCount}` → Görev 2'nin HTTP handler'ı `average_cpu_percent`/`device_count` JSON alanlarına eşliyor → Görev 3'ün `FleetTelemetryAverage` TS tipi (`average_cpu_percent: number; device_count: number`) bununla birebir eşleşiyor. `inventory.ConnectedWindow` Görev 1'de dışa açılıyor, Görev 2'nin handler'ı bunu doğrudan kullanıyor — aynı "bağlı" penceresi iki yerde de (envanterin "Bağlı" rozeti ve yeni ortalama CPU hesaplaması) tek kaynaktan geliyor.
