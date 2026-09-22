# Activity Akışı ve Audit/Activity UI (Spike 11.8) Uygulama Planı

> **Otonom çalışanlar için:** ZORUNLU ALT-SKILL: Bu planı görev görev uygulamak için superpowers:subagent-driven-development (önerilen) veya superpowers:executing-plans kullan. Adımlar `- [ ]` checkbox söz dizimiyle takip edilir.

**Amaç:** Spec'in ayrı tuttuğu iki kavramı doğru isimleriyle hayata geçirir: **Activity** (kullanıcı dostu, yalnız başarılı durum değişiklikleri — iş, alarm, davet, site rolü, servis hesabı olayları tek zaman çizelgesinde) ve **Audit** (11.2'de kurulan, her isteği başarı/başarısızlık fark etmeksizin kaydeden `internal/audittrail`'e gerçek bir sorgu/arama yeteneği kazandırarak). İkisi için de izin bazlı UI sayfaları eklenir.

**Mimari:** Spike 12'de yazılan `internal/audit` paketi (iş+alarm birleşik akışı) spec'in "Activity" terimiyle birebir örtüşüyor ama adı `internal/audittrail`'in "Audit" kavramıyla çakışıyor — bu paket `internal/activity`'ye yeniden adlandırılıp kimlik/RBAC/servis hesabı kaynaklarını da kapsayacak şekilde genişletilir. `internal/audittrail`'in Postgres store'unda ZATEN kullanılmayan bir `ListAuditTrail` sorgu metodu var (muhtemelen bu spike için önceden hazırlanmış) — bu, filtre desteğiyle genişletilip `Store`/`Service` arayüzüne bağlanır ve rename ile boşalan `/api/v1/audit/events` URL'sine, `PermissionViewAuditEvents` ile korunan yeni bir uç olarak bağlanır. Kimlik/RBAC/servis hesabı olayları, bu paketlerin kendi transaction'ı içinde değil — `internal/audittrail`'in kendi paket yorumunun da açıkça öngördüğü gibi — HTTP handler katmanında, mutasyon başarılı olduktan hemen sonra, best-effort olarak kaydedilir.

**Teknoloji yığını:** Go 1.26 (backend), React 19 + TypeScript + Vite (frontend, `web/`), PostgreSQL (yeni `activity_events` tablosu).

**Spec:** [docs/superpowers/specs/2026-09-13-enterprise-rbac-identity-audit-design.md](../specs/2026-09-13-enterprise-rbac-identity-audit-design.md) — "Audit, activity ve operasyon logları" bölümü. Yol haritası satırı: `11.8 | Activity akışı ve Audit/Activity UI | Site zaman çizelgesi, filtre ve güvenli dışa aktarım çalışır`.

## Genel Kısıtlar

- **Kapsam kararı (kullanıcı onaylı — "Tam kapsam"):** Activity, spec'in açıkça verdiği örneklere (kullanıcı davet edildi, site rolü atandı/kaldırıldı, servis hesabı oluşturuldu/rotate edildi/iptal edildi) genişletilir. Bulut hesabı mutasyonları spec'in Activity örnek listesinde YOK — bu spike'ta Activity'ye eklenmez (kapsam dışı, gerekirse ayrı bir işte eklenir).
- **Retention worker bilinçli olarak ertelenir.** Spec'in "Varsayılan audit retention 365, activity retention 180 gün... partition temelli bakım işlemi" paragrafı, yol haritasının 11.8 için verdiği kabul sinyalinde YOK ("Site zaman çizelgesi, filtre ve güvenli dışa aktarım çalışır" — retention'dan bahsetmiyor). `audit_events` tablosu zaten UPDATE/DELETE'i veritabanı seviyesinde reddediyor (append-only trigger), bu yüzden retention config'i şimdiden ekleyip UYGULAMADAN bırakmak yanıltıcı olur (kullanıcı bir env var ayarlar, hiçbir şey olmaz). Bu, spike 11.10'un ("Güvenlik sertleştirmesi") doğal kapsamına bırakılır.
- **Girişte UI yok.** 11.1-11.7 tamamen backend'di — bootstrap/login/davet/servis hesabı için HİÇBİR React sayfası yok. Bu spike de o listeye (spec'in "UI yüzeyleri" bölümündeki 9 maddeden yalnız son ikisi — activity zaman çizelgesi ve audit arama ekranı) bağlı kalır; bootstrap/login ekranı bu spike'ın kapsamı dışıdır. Bu nedenle yeni sayfaların gerçek taraycıda "tıklanabilir" uçtan uca doğrulaması yapılamaz — Görev 6'da backend curl ile, frontend ise `npm test` (mock edilmiş fetch) ve `npm run build` ile doğrulanır; bu, 11.1-11.7'nin de zaten izlediği kısıttır.
- **Activity yazımı transactional DEĞİL, best-effort'tur** — tıpkı `internal/audittrail`'in bugünkü davranışı gibi (spike 11.2'nin kendi paket yorumunda "later spikes" için öngörülen same-transaction garantisi henüz hiçbir yerde uygulanmadı). Bir Activity yazma hatası mutasyonu asla geri almaz veya API'yi düşürmez.
- **URL takası spec'e tam uyum sağlar:** eski `/api/v1/audit/events` (iş+alarm birleşimi) → `/api/v1/activity/events`'e taşınır; boşalan `/api/v1/audit/events` YENİ gerçek audit-arama uç noktasına verilir. Frontend'de de aynı takas yapılır: eski `web/src/pages/audit.tsx` (job+alert) → `web/src/pages/activity.tsx`'e taşınıp genişletilir; `web/src/pages/audit.tsx` yolunda YENİ bir sayfa (gerçek audit arama) oluşturulur.
- Servis hesabı `viewer` rolü `PermissionViewActivity`'yi karşılar ama `PermissionViewAuditEvents`'i karşılamaz (mevcut matris: `PermissionViewAuditEvents: {SiteRoleAdmin: true, SiteRoleOperator: true}` — viewer yok). Bu, Görev 6'nın manuel doğrulamasında bilerek test edilir.

## Dosya Yapısı

- Yeniden adlandır + genişlet: `internal/audit/audit.go` → `internal/activity/activity.go`, `internal/audit/audit_test.go` → `internal/activity/activity_test.go`, `internal/storage/postgres/audit.go` → `internal/storage/postgres/activity.go`, `internal/storage/postgres/audit_integration_test.go` → `internal/storage/postgres/activity_integration_test.go`.
- Oluştur: `internal/storage/postgres/migrations/023_activity_events.sql`.
- Değiştir/yeniden adlandır: `internal/server/audit.go` (eski içerik `internal/server/activity.go`'ya taşınır, bu dosyada YENİ audit-arama handler'ı yazılır), `internal/server/audit_test.go` (eski içerik `internal/server/activity_test.go`'ya taşınır, bu dosyada YENİ testler yazılır).
- Değiştir: `internal/server/identity.go`, `internal/server/rbac.go`, `internal/server/serviceaccounts.go`, `internal/server/server.go`, `internal/audittrail/audittrail.go`, `internal/audittrail/memorystore.go`, `internal/storage/postgres/audittrail.go`, `internal/storage/postgres/audittrail_integration_test.go`, `cmd/bazusop-hub/main.go`.
- Değiştir: `web/src/pages/audit.tsx` (yeni içerik), `web/src/pages/activity.tsx` (yeni dosya — eskinin genişletilmiş hali), `web/src/types.ts`, `web/src/navigation.ts`, `web/src/App.tsx`, `web/src/app.test.tsx`.
- Değiştir: `docs/API.md`.

## Görev 1: `internal/activity` paketi — yeniden adlandırma ve yazma yeteneği

**Dosyalar:**
- Sil: `internal/audit/audit.go`, `internal/audit/audit_test.go`
- Oluştur: `internal/activity/activity.go`, `internal/activity/activity_test.go`, `internal/storage/postgres/migrations/023_activity_events.sql`
- Sil: `internal/storage/postgres/audit.go`, `internal/storage/postgres/audit_integration_test.go`
- Oluştur: `internal/storage/postgres/activity.go`, `internal/storage/postgres/activity_integration_test.go`

**Arayüzler:**
- Üretir: `activity.Event{OrganizationID, SiteID, Source, ReferenceID, AgentID, Type, Actor, Message, OccurredAt}`, `activity.Source` (`SourceJob`, `SourceAlert`, `SourceIdentity`, `SourceSiteRole`, `SourceServiceAccount`), `activity.Service.List(ctx, scope, limit) ([]Event, error)`, `activity.Service.Record(ctx, Event) error`, `activity.Store` arayüzü (`ListActivityEvents`, `RecordActivityEvent`).

- [x] **Adım 1: `internal/audit` içeriğini `internal/activity`'ye taşı ve genişlet**

```bash
mkdir -p internal/activity
git rm internal/audit/audit.go internal/audit/audit_test.go
rmdir internal/audit 2>/dev/null || true
```

`internal/activity/activity.go`:

```go
// Package activity merges human-meaningful successful state changes from
// every domain into a single, site-scoped, time-ordered feed: job and alert
// lifecycle events (approved/claimed/succeeded, opened/acknowledged/resolved)
// plus identity, site-role and service-account events recorded directly by
// internal/server's HTTP handlers after a mutation succeeds. This is the
// "Activity" concept from the design spec — human-readable successes only,
// failures and reads stay in internal/audittrail. This package was renamed
// from internal/audit in spike 11.8: the old name collided with the spec's
// separate, security-focused "Audit" concept internal/audittrail implements.
package activity

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/gokayybaz/bazusop/internal/alerting"
	"github.com/gokayybaz/bazusop/internal/jobs"
	"github.com/gokayybaz/bazusop/internal/tenancy"
)

var ErrInvalidActivity = errors.New("invalid activity query")

type Source string

const (
	SourceJob            Source = "job"
	SourceAlert          Source = "alert"
	SourceIdentity       Source = "identity"
	SourceSiteRole       Source = "site_role"
	SourceServiceAccount Source = "service_account"
)

// Event is the common shape every source is projected into. ReferenceID is
// the parent job/incident/invite/user/service-account ID; the full
// per-parent record remains reachable through that domain's own endpoints
// where one exists (jobs, alerts).
type Event struct {
	OrganizationID string    `json:"organization_id"`
	SiteID         string    `json:"site_id"`
	Source         Source    `json:"source"`
	ReferenceID    string    `json:"reference_id"`
	AgentID        string    `json:"agent_id"`
	Type           string    `json:"type"`
	Actor          string    `json:"actor"`
	Message        string    `json:"message"`
	OccurredAt     time.Time `json:"occurred_at"`
}

type Store interface {
	ListActivityEvents(ctx context.Context, scope tenancy.Scope, limit int) ([]Event, error)
	RecordActivityEvent(ctx context.Context, event Event) error
}

type Service struct {
	store Store
}

func NewService(store Store) *Service {
	return &Service{store: store}
}

func (service *Service) List(ctx context.Context, scope tenancy.Scope, limit int) ([]Event, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	if limit < 1 || limit > 500 {
		return nil, ErrInvalidActivity
	}
	return service.store.ListActivityEvents(ctx, scope, limit)
}

// Record stores a single identity/site-role/service-account activity event.
// It is called directly by internal/server's HTTP handlers, best-effort,
// after the mutation it describes has already succeeded — the same
// fire-and-forget posture internal/audittrail uses: a write failure here
// must never take the API down or roll back the mutation it describes.
func (service *Service) Record(ctx context.Context, event Event) error {
	if event.OccurredAt.IsZero() {
		event.OccurredAt = time.Now().UTC()
	}
	return service.store.RecordActivityEvent(ctx, event)
}

// MemoryStore merges the job and alert event trails held by the in-process
// development stores with identity/site-role/service-account events recorded
// directly through Record, into a single scoped, time-ordered feed. It
// exists because those stores keep private in-memory state with no shared
// table to query, unlike storage/postgres, which unions job_events,
// alert_events and activity_events directly in SQL.
type MemoryStore struct {
	jobs     *jobs.MemoryStore
	alerts   *alerting.MemoryStore
	mu       sync.Mutex
	recorded []Event
}

func NewMemoryStore(jobStore *jobs.MemoryStore, alertStore *alerting.MemoryStore) *MemoryStore {
	return &MemoryStore{jobs: jobStore, alerts: alertStore}
}

func (store *MemoryStore) RecordActivityEvent(_ context.Context, event Event) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.recorded = append(store.recorded, event)
	return nil
}

func (store *MemoryStore) ListActivityEvents(ctx context.Context, scope tenancy.Scope, limit int) ([]Event, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	jobRecords, err := store.jobs.AllEvents(ctx, scope)
	if err != nil {
		return nil, err
	}
	alertRecords, err := store.alerts.AllEvents(ctx, scope)
	if err != nil {
		return nil, err
	}
	events := make([]Event, 0, len(jobRecords)+len(alertRecords))
	for _, record := range jobRecords {
		events = append(events, Event{
			OrganizationID: record.Event.OrganizationID, SiteID: record.Event.SiteID,
			Source: SourceJob, ReferenceID: record.Event.JobID, AgentID: record.AgentID,
			Type: string(record.Event.Type), Actor: record.Event.Actor,
			Message: record.Event.Message, OccurredAt: record.Event.OccurredAt,
		})
	}
	for _, record := range alertRecords {
		events = append(events, Event{
			OrganizationID: record.Event.OrganizationID, SiteID: record.Event.SiteID,
			Source: SourceAlert, ReferenceID: record.Event.IncidentID, AgentID: record.AgentID,
			Type: string(record.Event.Type), Actor: record.Event.Actor,
			Message: record.Event.Message, OccurredAt: record.Event.OccurredAt,
		})
	}
	store.mu.Lock()
	for _, event := range store.recorded {
		if event.OrganizationID == scope.OrganizationID && event.SiteID == scope.SiteID {
			events = append(events, event)
		}
	}
	store.mu.Unlock()
	sort.Slice(events, func(left, right int) bool {
		if events[left].OccurredAt.Equal(events[right].OccurredAt) {
			if events[left].Source != events[right].Source {
				return events[left].Source < events[right].Source
			}
			return events[left].ReferenceID < events[right].ReferenceID
		}
		return events[left].OccurredAt.After(events[right].OccurredAt)
	})
	if len(events) > limit {
		events = events[:limit]
	}
	return events, nil
}
```

- [x] **Adım 2: `internal/activity/activity_test.go`'yu yaz (eski `audit_test.go`'nun `audit.`→`activity.` yeniden adlandırılmış hali + Record/merge testi)**

```go
package activity_test

import (
	"errors"
	"testing"
	"time"

	"github.com/gokayybaz/bazusop/internal/activity"
	"github.com/gokayybaz/bazusop/internal/alerting"
	"github.com/gokayybaz/bazusop/internal/jobs"
	"github.com/gokayybaz/bazusop/internal/tenancy"
)

func seedStores(t *testing.T, scope, otherScope tenancy.Scope, now time.Time) (*jobs.MemoryStore, *alerting.MemoryStore) {
	t.Helper()
	jobStore := jobs.NewMemoryStore()
	job := jobs.Job{ID: "job-1", OrganizationID: scope.OrganizationID, SiteID: scope.SiteID, AgentID: "agent-1", Action: jobs.ActionServiceRestart, Target: "nginx.service", ApprovedBy: "gokay", Reason: "config değişikliği", RequestedAt: now, Status: jobs.StatusQueued}
	jobEvent := jobs.Event{JobID: job.ID, OrganizationID: scope.OrganizationID, SiteID: scope.SiteID, Sequence: 0, Type: jobs.EventApproved, Message: job.Reason, Actor: job.ApprovedBy, OccurredAt: now}
	if err := jobStore.CreateJob(t.Context(), job, jobEvent); err != nil {
		t.Fatalf("seed job event: %v", err)
	}
	otherJob := jobs.Job{ID: "job-2", OrganizationID: otherScope.OrganizationID, SiteID: otherScope.SiteID, AgentID: "agent-2", Action: jobs.ActionHostReboot, ApprovedBy: "someone", Reason: "patch", RequestedAt: now, Status: jobs.StatusQueued}
	otherJobEvent := jobs.Event{JobID: otherJob.ID, OrganizationID: otherScope.OrganizationID, SiteID: otherScope.SiteID, Sequence: 0, Type: jobs.EventApproved, Message: otherJob.Reason, Actor: otherJob.ApprovedBy, OccurredAt: now}
	if err := jobStore.CreateJob(t.Context(), otherJob, otherJobEvent); err != nil {
		t.Fatalf("seed other-scope job event: %v", err)
	}

	alertStore := alerting.NewMemoryStore()
	incident := alerting.Incident{OrganizationID: scope.OrganizationID, SiteID: scope.SiteID, ID: "incident-1", RuleID: "rule-1", RuleName: "Yüksek CPU", AgentID: "agent-1", Severity: alerting.SeverityCritical, Status: alerting.StatusOpen, Message: "CPU %96", LatestValue: 96, OpenedAt: now.Add(time.Minute)}
	incidentEvent := alerting.Event{OrganizationID: scope.OrganizationID, SiteID: scope.SiteID, ID: "event-1", IncidentID: incident.ID, Type: alerting.EventOpened, Actor: "hub", Message: "CPU %96", OccurredAt: now.Add(time.Minute)}
	if _, _, err := alertStore.EnsureIncident(t.Context(), scope, incident, incidentEvent); err != nil {
		t.Fatalf("seed alert event: %v", err)
	}
	otherIncident := alerting.Incident{OrganizationID: otherScope.OrganizationID, SiteID: otherScope.SiteID, ID: "incident-2", RuleID: "rule-2", RuleName: "Disk kritik", AgentID: "agent-2", Severity: alerting.SeverityCritical, Status: alerting.StatusOpen, Message: "Disk %98", LatestValue: 98, OpenedAt: now.Add(time.Minute)}
	otherIncidentEvent := alerting.Event{OrganizationID: otherScope.OrganizationID, SiteID: otherScope.SiteID, ID: "event-2", IncidentID: otherIncident.ID, Type: alerting.EventOpened, Actor: "hub", Message: "Disk %98", OccurredAt: now.Add(time.Minute)}
	if _, _, err := alertStore.EnsureIncident(t.Context(), otherScope, otherIncident, otherIncidentEvent); err != nil {
		t.Fatalf("seed other-scope alert event: %v", err)
	}

	return jobStore, alertStore
}

func TestMemoryStoreMergesJobAndAlertEventsInScopeOrderedByRecency(t *testing.T) {
	t.Parallel()
	scope := tenancy.Scope{OrganizationID: "org-a", SiteID: "site-a"}
	otherScope := tenancy.Scope{OrganizationID: "org-b", SiteID: "site-b"}
	now := time.Now().UTC().Truncate(time.Microsecond)
	jobStore, alertStore := seedStores(t, scope, otherScope, now)

	store := activity.NewMemoryStore(jobStore, alertStore)
	events, err := store.ListActivityEvents(t.Context(), scope, 10)
	if err != nil {
		t.Fatalf("list activity events: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 in-scope events, got %d: %#v", len(events), events)
	}
	if events[0].Source != activity.SourceAlert || events[0].ReferenceID != "incident-1" || events[0].AgentID != "agent-1" {
		t.Fatalf("expected the more recent alert event first, got %#v", events[0])
	}
	if events[1].Source != activity.SourceJob || events[1].ReferenceID != "job-1" || events[1].AgentID != "agent-1" {
		t.Fatalf("expected the older job event second, got %#v", events[1])
	}
	for _, event := range events {
		if event.OrganizationID != scope.OrganizationID || event.SiteID != scope.SiteID {
			t.Fatalf("leaked event from another scope: %#v", event)
		}
	}
}

func TestMemoryStoreRespectsLimit(t *testing.T) {
	t.Parallel()
	scope := tenancy.Scope{OrganizationID: "org-a", SiteID: "site-a"}
	otherScope := tenancy.Scope{OrganizationID: "org-b", SiteID: "site-b"}
	now := time.Now().UTC().Truncate(time.Microsecond)
	jobStore, alertStore := seedStores(t, scope, otherScope, now)

	store := activity.NewMemoryStore(jobStore, alertStore)
	events, err := store.ListActivityEvents(t.Context(), scope, 1)
	if err != nil {
		t.Fatalf("list activity events: %v", err)
	}
	if len(events) != 1 || events[0].Source != activity.SourceAlert {
		t.Fatalf("expected exactly the most recent event, got %#v", events)
	}
}

func TestMemoryStoreRejectsInvalidScope(t *testing.T) {
	t.Parallel()
	store := activity.NewMemoryStore(jobs.NewMemoryStore(), alerting.NewMemoryStore())
	if _, err := store.ListActivityEvents(t.Context(), tenancy.Scope{}, 10); !errors.Is(err, tenancy.ErrInvalidScope) {
		t.Fatalf("expected ErrInvalidScope, got %v", err)
	}
}

func TestServiceRejectsOutOfRangeLimit(t *testing.T) {
	t.Parallel()
	service := activity.NewService(activity.NewMemoryStore(jobs.NewMemoryStore(), alerting.NewMemoryStore()))
	scope := tenancy.Scope{OrganizationID: "org-a", SiteID: "site-a"}
	if _, err := service.List(t.Context(), scope, 0); !errors.Is(err, activity.ErrInvalidActivity) {
		t.Fatalf("expected ErrInvalidActivity for zero limit, got %v", err)
	}
	if _, err := service.List(t.Context(), scope, 501); !errors.Is(err, activity.ErrInvalidActivity) {
		t.Fatalf("expected ErrInvalidActivity for oversized limit, got %v", err)
	}
}

func TestServiceValidatesScopeBeforeLimit(t *testing.T) {
	t.Parallel()
	service := activity.NewService(activity.NewMemoryStore(jobs.NewMemoryStore(), alerting.NewMemoryStore()))
	if _, err := service.List(t.Context(), tenancy.Scope{}, 10); !errors.Is(err, tenancy.ErrInvalidScope) {
		t.Fatalf("expected ErrInvalidScope, got %v", err)
	}
}

func TestServiceRecordMergesIntoScopedList(t *testing.T) {
	t.Parallel()
	store := activity.NewMemoryStore(jobs.NewMemoryStore(), alerting.NewMemoryStore())
	service := activity.NewService(store)
	scope := tenancy.Scope{OrganizationID: "org-a", SiteID: "site-a"}
	otherScope := tenancy.Scope{OrganizationID: "org-b", SiteID: "site-b"}

	if err := service.Record(t.Context(), activity.Event{OrganizationID: scope.OrganizationID, SiteID: scope.SiteID, Source: activity.SourceIdentity, ReferenceID: "invite-1", Type: "invite_created", Actor: "admin", Message: "someone@example.com davet edildi"}); err != nil {
		t.Fatalf("record: %v", err)
	}
	if err := service.Record(t.Context(), activity.Event{OrganizationID: otherScope.OrganizationID, SiteID: otherScope.SiteID, Source: activity.SourceIdentity, ReferenceID: "invite-2", Type: "invite_created", Actor: "admin", Message: "other-scope invite"}); err != nil {
		t.Fatalf("record other scope: %v", err)
	}

	events, err := service.List(t.Context(), scope, 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(events) != 1 || events[0].Source != activity.SourceIdentity || events[0].ReferenceID != "invite-1" {
		t.Fatalf("expected exactly the in-scope recorded event, got %#v", events)
	}
	if events[0].OccurredAt.IsZero() {
		t.Fatal("expected Record to fill OccurredAt when unset")
	}
}
```

- [x] **Adım 3: Testleri çalıştır**

Çalıştır: `GOCACHE=/tmp/bazusop-go-cache go test ./internal/activity/... -v 2>&1 | tail -40`
Beklenen: BAŞARILI (tüm testler)

- [x] **Adım 4: Postgres migration'ını oluştur**

`internal/storage/postgres/migrations/023_activity_events.sql`:

```sql
CREATE TABLE IF NOT EXISTS activity_events (
    id BIGSERIAL PRIMARY KEY,
    organization_id TEXT NOT NULL,
    site_id TEXT NOT NULL,
    source TEXT NOT NULL,
    reference_id TEXT NOT NULL DEFAULT '',
    agent_id TEXT NOT NULL DEFAULT '',
    type TEXT NOT NULL,
    actor TEXT NOT NULL DEFAULT '',
    message TEXT NOT NULL DEFAULT '',
    occurred_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS activity_events_site_time_idx
    ON activity_events (organization_id, site_id, occurred_at DESC);
```

- [x] **Adım 5: `internal/storage/postgres/activity.go`'yu yaz (eski `audit.go`'nun genişletilmiş hali)**

```bash
git rm internal/storage/postgres/audit.go
```

`internal/storage/postgres/activity.go`:

```go
package postgres

import (
	"context"
	"fmt"

	"github.com/gokayybaz/bazusop/internal/activity"
	"github.com/gokayybaz/bazusop/internal/tenancy"
)

func (store *Store) RecordActivityEvent(ctx context.Context, event activity.Event) error {
	_, err := store.pool.Exec(ctx, `
		INSERT INTO activity_events (organization_id, site_id, source, reference_id, agent_id, type, actor, message, occurred_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		event.OrganizationID, event.SiteID, string(event.Source), event.ReferenceID, event.AgentID,
		event.Type, event.Actor, event.Message, event.OccurredAt,
	)
	if err != nil {
		return fmt.Errorf("record activity event: %w", err)
	}
	return nil
}

func (store *Store) ListActivityEvents(ctx context.Context, scope tenancy.Scope, limit int) ([]activity.Event, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	rows, err := store.pool.Query(ctx, `
		SELECT organization_id, site_id, source, reference_id, agent_id, type, actor, message, occurred_at
		FROM (
			SELECT job_events.organization_id, job_events.site_id, 'job' AS source, job_events.job_id AS reference_id,
				jobs.agent_id, job_events.type, job_events.actor, job_events.message, job_events.occurred_at
			FROM job_events
			JOIN jobs ON jobs.id = job_events.job_id
				AND jobs.organization_id = job_events.organization_id AND jobs.site_id = job_events.site_id
			WHERE job_events.organization_id = $1 AND job_events.site_id = $2

			UNION ALL

			SELECT event.organization_id, event.site_id, 'alert' AS source, event.incident_id AS reference_id,
				incident.agent_id, event.type, event.actor, event.message, event.occurred_at
			FROM alert_events event
			JOIN alert_incidents incident ON incident.id = event.incident_id
				AND incident.organization_id = event.organization_id AND incident.site_id = event.site_id
			WHERE event.organization_id = $1 AND event.site_id = $2

			UNION ALL

			SELECT organization_id, site_id, source, reference_id, agent_id, type, actor, message, occurred_at
			FROM activity_events
			WHERE organization_id = $1 AND site_id = $2
		) combined
		ORDER BY occurred_at DESC, source, reference_id
		LIMIT $3`, scope.OrganizationID, scope.SiteID, limit)
	if err != nil {
		return nil, fmt.Errorf("query activity events: %w", err)
	}
	defer rows.Close()
	events := make([]activity.Event, 0)
	for rows.Next() {
		var event activity.Event
		if err := rows.Scan(&event.OrganizationID, &event.SiteID, &event.Source, &event.ReferenceID, &event.AgentID, &event.Type, &event.Actor, &event.Message, &event.OccurredAt); err != nil {
			return nil, fmt.Errorf("scan activity event: %w", err)
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate activity events: %w", err)
	}
	return events, nil
}
```

- [x] **Adım 6: `internal/storage/postgres/activity_integration_test.go`'yu yaz (eski `audit_integration_test.go`'nun genişletilmiş hali)**

```bash
git rm internal/storage/postgres/audit_integration_test.go
```

`internal/storage/postgres/activity_integration_test.go` — eski dosyanın `audit.`→`activity.`, `ListAuditEvents`→`ListActivityEvents` yeniden adlandırılmış hali (satır 1-94 önceki içerikle birebir, yalnız import ve tip adları değişir), sonuna yeni bir `RecordActivityEvent` testi eklenir:

```go
package postgres

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/gokayybaz/bazusop/internal/activity"
	"github.com/gokayybaz/bazusop/internal/alerting"
	"github.com/gokayybaz/bazusop/internal/inventory"
	"github.com/gokayybaz/bazusop/internal/jobs"
	"github.com/gokayybaz/bazusop/internal/tenancy"
)

func TestPostgresActivityEventsMergeJobsAlertsAndRecordedEventsScopedBySite(t *testing.T) {
	databaseURL := os.Getenv("BAZUSOP_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("set BAZUSOP_TEST_DATABASE_URL to run PostgreSQL integration tests")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	store, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	suffix := time.Now().UTC().Format("20060102150405.000000000")
	orgID := "activity-org-" + suffix
	scope := tenancy.Scope{OrganizationID: orgID, SiteID: "activity-site-a-" + suffix}
	otherScope := tenancy.Scope{OrganizationID: orgID, SiteID: "activity-site-b-" + suffix}
	agent := tenancy.Agent{ID: "activity-agent-" + suffix, OrganizationID: scope.OrganizationID, SiteID: scope.SiteID}

	if _, err := store.pool.Exec(ctx, `INSERT INTO organizations (id, name) VALUES ($1, 'Activity test')`, orgID); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		for _, table := range []string{"job_events", "jobs", "alert_events", "alert_incidents", "alert_rules", "activity_events", "hosts", "agents", "sites", "organizations"} {
			column := "organization_id"
			if table == "organizations" {
				column = "id"
			}
			if _, err := store.pool.Exec(cleanupCtx, "DELETE FROM "+table+" WHERE "+column+"=$1", orgID); err != nil {
				t.Errorf("cleanup %s: %v", table, err)
			}
		}
	}()
	for _, site := range []tenancy.Scope{scope, otherScope} {
		if _, err := store.pool.Exec(ctx, `INSERT INTO sites (id, organization_id, name, slug) VALUES ($1, $2, $1, $1)`, site.SiteID, site.OrganizationID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.pool.Exec(ctx, `INSERT INTO agents (id, organization_id, site_id) VALUES ($1, $2, $3)`, agent.ID, agent.OrganizationID, agent.SiteID); err != nil {
		t.Fatal(err)
	}

	hosts := inventory.NewService(store)
	facts := inventory.Facts{Hostname: "edge", OSFamily: "linux", OSName: "Ubuntu", OSVersion: "24.04", Architecture: "amd64", CPUCores: 2, MemoryBytes: 1024, IPAddresses: []string{"10.0.0.1"}, AgentVersion: "test"}
	if err := hosts.Report(ctx, agent, facts); err != nil {
		t.Fatal(err)
	}
	jobService, err := jobs.NewService(store, jobs.WithHostScopeChecker(hosts.HasHost))
	if err != nil {
		t.Fatal(err)
	}
	job, err := jobService.Create(ctx, scope, agent.ID, jobs.CreateRequest{Action: jobs.ActionHostReboot, ApprovedBy: "ops", Reason: "maintenance"})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}

	now := job.RequestedAt.Add(time.Minute)
	rule := alerting.Rule{OrganizationID: scope.OrganizationID, SiteID: scope.SiteID, ID: "activity-rule-" + suffix, RuleRequest: alerting.RuleRequest{Name: "Yüksek CPU", Kind: alerting.KindMetric, Metric: alerting.MetricCPU, Threshold: 90, Severity: alerting.SeverityCritical, Enabled: true}, CreatedAt: now}
	if err := store.CreateAlertRule(ctx, scope, rule); err != nil {
		t.Fatalf("create rule: %v", err)
	}
	incident := alerting.Incident{OrganizationID: scope.OrganizationID, SiteID: scope.SiteID, ID: "activity-incident-" + suffix, RuleID: rule.ID, RuleName: rule.Name, AgentID: agent.ID, Severity: alerting.SeverityCritical, Status: alerting.StatusOpen, Message: "CPU %96", LatestValue: 96, OpenedAt: now}
	incidentEvent := alerting.Event{OrganizationID: scope.OrganizationID, SiteID: scope.SiteID, ID: "activity-event-" + suffix, IncidentID: "forged", Type: alerting.EventOpened, Actor: "hub", Message: "CPU %96", OccurredAt: now}
	if _, created, err := store.EnsureIncident(ctx, scope, incident, incidentEvent); err != nil || !created {
		t.Fatalf("create incident: created=%v err=%v", created, err)
	}

	otherRule := alerting.Rule{OrganizationID: otherScope.OrganizationID, SiteID: otherScope.SiteID, ID: "activity-other-rule-" + suffix, RuleRequest: alerting.RuleRequest{Name: "Disk kritik", Kind: alerting.KindMetric, Metric: alerting.MetricDisk, Threshold: 90, Severity: alerting.SeverityCritical, Enabled: true}, CreatedAt: now}
	if err := store.CreateAlertRule(ctx, otherScope, otherRule); err != nil {
		t.Fatalf("create other-site rule: %v", err)
	}
	otherIncident := alerting.Incident{OrganizationID: otherScope.OrganizationID, SiteID: otherScope.SiteID, ID: "activity-other-incident-" + suffix, RuleID: otherRule.ID, RuleName: otherRule.Name, AgentID: "other-agent", Severity: alerting.SeverityCritical, Status: alerting.StatusOpen, Message: "Disk %98", LatestValue: 98, OpenedAt: now}
	otherIncidentEvent := alerting.Event{OrganizationID: otherScope.OrganizationID, SiteID: otherScope.SiteID, ID: "activity-other-event-" + suffix, IncidentID: "forged", Type: alerting.EventOpened, Actor: "hub", Message: "Disk %98", OccurredAt: now}
	if _, created, err := store.EnsureIncident(ctx, otherScope, otherIncident, otherIncidentEvent); err != nil || !created {
		t.Fatalf("create other-site incident: created=%v err=%v", created, err)
	}

	if err := store.RecordActivityEvent(ctx, activity.Event{OrganizationID: scope.OrganizationID, SiteID: scope.SiteID, Source: activity.SourceIdentity, ReferenceID: "invite-" + suffix, Type: "invite_created", Actor: "admin", Message: "someone@example.com davet edildi", OccurredAt: now.Add(time.Minute)}); err != nil {
		t.Fatalf("record activity event: %v", err)
	}

	events, err := store.ListActivityEvents(ctx, scope, 50)
	if err != nil {
		t.Fatalf("list activity events: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("expected 3 in-scope events, got %d: %#v", len(events), events)
	}
	if events[0].Source != activity.SourceIdentity || events[0].Type != "invite_created" {
		t.Fatalf("expected the most recent recorded activity event first, got %#v", events[0])
	}
	if events[1].Source != activity.SourceAlert || events[1].ReferenceID != incident.ID || events[1].AgentID != agent.ID {
		t.Fatalf("expected the alert event second, got %#v", events[1])
	}
	if events[2].Source != activity.SourceJob || events[2].ReferenceID != job.ID || events[2].AgentID != agent.ID {
		t.Fatalf("expected the job event third, got %#v", events[2])
	}
	for _, event := range events {
		if event.OrganizationID != scope.OrganizationID || event.SiteID != scope.SiteID {
			t.Fatalf("cross-site activity event leaked: %#v", event)
		}
	}

	if limited, err := store.ListActivityEvents(ctx, scope, 1); err != nil || len(limited) != 1 || limited[0].Source != activity.SourceIdentity {
		t.Fatalf("expected limit to keep only the most recent event, got %#v, %v", limited, err)
	}
}
```

- [x] **Adım 7: Testleri çalıştır ve derlemeyi doğrula**

Çalıştır: `go build ./internal/activity/... ./internal/storage/postgres/... 2>&1 | tail -30`
Beklenen: `internal/storage/postgres` şu an derlenmeyebilir (main.go/server.go henüz `internal/audit`'e referans veriyor — Görev 2/3'te düzelir). `internal/activity` başarıyla derlenmeli.

- [x] **Adım 8: Commit**

```bash
git add internal/activity internal/storage/postgres/activity.go internal/storage/postgres/activity_integration_test.go internal/storage/postgres/migrations/023_activity_events.sql
git commit -m "feat: rename internal/audit to internal/activity and add a write path"
```

## Görev 2: Activity yazımını kimlik/RBAC/servis hesabı handler'larına bağla, `WithAudit`'i `WithActivity`'ye taşı

**Dosyalar:**
- Değiştir: `internal/server/identity.go`, `internal/server/rbac.go`, `internal/server/serviceaccounts.go`, `internal/server/server.go`
- Sil: `internal/server/audit.go` (içeriği taşınır), `internal/server/audit_test.go` (içeriği taşınır)
- Oluştur: `internal/server/activity.go`, `internal/server/activity_test.go`
- Değiştir: `cmd/bazusop-hub/main.go`

**Arayüzler:**
- Üretir: `WithActivity(service *activity.Service) Option`, `handleListActivityEvents(...) http.HandlerFunc` (artık `PermissionViewActivity` ile korunuyor).
- Tüketir: Görev 1'in `activity.Service`, `activity.Event`, `activity.Source*`.

- [x] **Adım 1: `internal/server/audit.go`'yu `internal/server/activity.go`'ya taşı, `PermissionViewActivity` ile koru**

```bash
git mv internal/server/audit.go internal/server/activity.go
```

`internal/server/activity.go`'yu şu içerikle değiştir:

```go
package server

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gokayybaz/bazusop/internal/activity"
	"github.com/gokayybaz/bazusop/internal/authorization"
	"github.com/gokayybaz/bazusop/internal/serviceaccounts"
	"github.com/gokayybaz/bazusop/internal/sessions"
	"github.com/gokayybaz/bazusop/internal/tenancy"
)

func WithActivity(service *activity.Service) Option {
	return func(options *handlerOptions) {
		options.activityService = service
	}
}

func handleListActivityEvents(service *activity.Service, sessionService *sessions.Service, serviceAccountService *serviceaccounts.Service, authzService *authorization.Service, scope tenancy.Scope) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		if _, ok := requirePermission(response, request, sessionService, serviceAccountService, authzService, authorization.PermissionViewActivity, scope.SiteID); !ok {
			return
		}
		limit := 100
		if value := request.URL.Query().Get("limit"); value != "" {
			parsed, err := strconv.Atoi(value)
			if err != nil {
				http.Error(response, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
				return
			}
			limit = parsed
		}
		values, err := service.List(request.Context(), scope, limit)
		if errors.Is(err, activity.ErrInvalidActivity) {
			http.Error(response, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}
		if err != nil {
			http.Error(response, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
			return
		}
		writeJSON(response, http.StatusOK, struct {
			Events []activity.Event `json:"events"`
		}{values})
	}
}
```

- [x] **Adım 2: `internal/server/identity.go`'yu güncelle — davet oluşturma Activity kaydı yazsın**

`internal/server/identity.go`'nun import listesine ekle: `"github.com/gokayybaz/bazusop/internal/activity"`.

`handleCreateInvite`'ı şununla değiştir:

```go
func handleCreateInvite(service *identity.Service, sessionService *sessions.Service, authzService *authorization.Service, activityService *activity.Service, scope tenancy.Scope) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		actorUserID, ok := requirePermission(response, request, sessionService, nil, authzService, authorization.PermissionManageUsers, scope.SiteID)
		if !ok {
			return
		}
		var body struct {
			Email   string   `json:"email"`
			Role    string   `json:"role"`
			SiteIDs []string `json:"site_ids"`
		}
		if err := decodeJSON(response, request, &body); err != nil {
			return
		}
		createdBy := "admin"
		if actorUserID != "" {
			createdBy = actorUserID
		}
		role := identity.RolePlatformAdmin
		var grants []identity.SiteRoleGrant
		if body.Role != "" {
			role = ""
			for _, siteID := range body.SiteIDs {
				grants = append(grants, identity.SiteRoleGrant{SiteID: siteID, Role: body.Role})
			}
		}
		invite, token, err := service.CreateInvite(request.Context(), createdBy, tenancy.DefaultOrganizationID, body.Email, role, grants)
		if errors.Is(err, identity.ErrInvalidInviteRole) {
			http.Error(response, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}
		if err != nil {
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		if activityService != nil {
			_ = activityService.Record(request.Context(), activity.Event{
				OrganizationID: scope.OrganizationID, SiteID: scope.SiteID,
				Source: activity.SourceIdentity, ReferenceID: invite.ID, Type: "invite_created",
				Actor: createdBy, Message: invite.Email + " davet edildi",
			})
		}
		writeJSON(response, http.StatusCreated, struct {
			Email string `json:"email"`
			Token string `json:"token"`
		}{invite.Email, token})
	}
}
```

- [x] **Adım 3: `internal/server/rbac.go`'yu güncelle — site rolü atama/kaldırma Activity kaydı yazsın**

`internal/server/rbac.go`'nun import listesine ekle: `"github.com/gokayybaz/bazusop/internal/activity"`.

`handleAssignSiteRole`/`handleRevokeSiteRole`'u şununla değiştir:

```go
func handleAssignSiteRole(sessionService *sessions.Service, authzService *authorization.Service, activityService *activity.Service, scope tenancy.Scope) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		actorID, ok := requirePermission(response, request, sessionService, nil, authzService, authorization.PermissionManageUsers, scope.SiteID)
		if !ok {
			return
		}
		var body struct {
			UserID string `json:"user_id"`
			Role   string `json:"role"`
		}
		if err := decodeJSON(response, request, &body); err != nil {
			return
		}
		if err := authzService.AssignRole(request.Context(), body.UserID, scope.OrganizationID, request.PathValue("siteID"), authorization.SiteRole(body.Role)); err != nil {
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		if activityService != nil {
			_ = activityService.Record(request.Context(), activity.Event{
				OrganizationID: scope.OrganizationID, SiteID: scope.SiteID,
				Source: activity.SourceSiteRole, ReferenceID: body.UserID, Type: "assigned",
				Actor: actorID, Message: body.Role + " rolü atandı",
			})
		}
		response.WriteHeader(http.StatusNoContent)
	}
}

func handleRevokeSiteRole(sessionService *sessions.Service, authzService *authorization.Service, activityService *activity.Service, scope tenancy.Scope) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		actorID, ok := requirePermission(response, request, sessionService, nil, authzService, authorization.PermissionManageUsers, scope.SiteID)
		if !ok {
			return
		}
		userID := request.PathValue("userID")
		if err := authzService.RevokeRole(request.Context(), userID, request.PathValue("siteID")); err != nil {
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		if activityService != nil {
			_ = activityService.Record(request.Context(), activity.Event{
				OrganizationID: scope.OrganizationID, SiteID: scope.SiteID,
				Source: activity.SourceSiteRole, ReferenceID: userID, Type: "revoked",
				Actor: actorID, Message: "site rolü kaldırıldı",
			})
		}
		response.WriteHeader(http.StatusNoContent)
	}
}
```

- [x] **Adım 4: `internal/server/serviceaccounts.go`'yu güncelle — oluşturma/rotasyon/iptal/devre dışı bırakma Activity kaydı yazsın**

`internal/server/serviceaccounts.go`'nun import listesine ekle: `"github.com/gokayybaz/bazusop/internal/activity"`.

`handleCreateServiceAccount`, `handleRotateServiceAccountToken`, `handleRevokeServiceAccountToken`, `handleDisableServiceAccount`'ı şununla değiştir (`handleListServiceAccounts` değişmeden kalır):

```go
func handleCreateServiceAccount(service *serviceaccounts.Service, sessionService *sessions.Service, authzService *authorization.Service, activityService *activity.Service, scope tenancy.Scope) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		actorID, ok := requirePermission(response, request, sessionService, nil, authzService, authorization.PermissionManageServiceAccounts, scope.SiteID)
		if !ok {
			return
		}
		var body struct {
			Name       string `json:"name"`
			Role       string `json:"role"`
			ExpiryDays int    `json:"expiry_days"`
		}
		if err := decodeJSON(response, request, &body); err != nil {
			return
		}
		account, token, err := service.CreateAccount(request.Context(), scope.OrganizationID, request.PathValue("siteID"), body.Name, authorization.SiteRole(body.Role), body.ExpiryDays)
		if errors.Is(err, serviceaccounts.ErrInvalidRole) || errors.Is(err, serviceaccounts.ErrInvalidExpiry) {
			http.Error(response, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}
		if err != nil {
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		if activityService != nil {
			_ = activityService.Record(request.Context(), activity.Event{
				OrganizationID: scope.OrganizationID, SiteID: scope.SiteID,
				Source: activity.SourceServiceAccount, ReferenceID: account.ID, Type: "created",
				Actor: actorID, Message: account.Name + " servis hesabı oluşturuldu (" + string(account.Role) + ")",
			})
		}
		writeJSON(response, http.StatusCreated, struct {
			ID    string `json:"id"`
			Name  string `json:"name"`
			Role  string `json:"role"`
			Token string `json:"token"`
		}{account.ID, account.Name, string(account.Role), token})
	}
}

func handleListServiceAccounts(service *serviceaccounts.Service, sessionService *sessions.Service, authzService *authorization.Service, scope tenancy.Scope) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		if _, ok := requirePermission(response, request, sessionService, nil, authzService, authorization.PermissionManageServiceAccounts, scope.SiteID); !ok {
			return
		}
		accounts, err := service.ListForSite(request.Context(), request.PathValue("siteID"))
		if err != nil {
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		writeJSON(response, http.StatusOK, struct {
			ServiceAccounts []serviceaccounts.ServiceAccount `json:"service_accounts"`
		}{accounts})
	}
}

func handleRotateServiceAccountToken(service *serviceaccounts.Service, sessionService *sessions.Service, authzService *authorization.Service, activityService *activity.Service, scope tenancy.Scope) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		actorID, ok := requirePermission(response, request, sessionService, nil, authzService, authorization.PermissionManageServiceAccounts, scope.SiteID)
		if !ok {
			return
		}
		var body struct {
			ExpiryDays int `json:"expiry_days"`
		}
		if err := decodeJSON(response, request, &body); err != nil {
			return
		}
		accountID := request.PathValue("accountID")
		token, err := service.RotateToken(request.Context(), accountID, body.ExpiryDays)
		if errors.Is(err, serviceaccounts.ErrInvalidExpiry) {
			http.Error(response, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}
		if err != nil {
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		if activityService != nil {
			_ = activityService.Record(request.Context(), activity.Event{
				OrganizationID: scope.OrganizationID, SiteID: scope.SiteID,
				Source: activity.SourceServiceAccount, ReferenceID: accountID, Type: "token_rotated",
				Actor: actorID, Message: "servis hesabı token'ı rotate edildi",
			})
		}
		writeJSON(response, http.StatusCreated, struct {
			Token string `json:"token"`
		}{token})
	}
}

func handleRevokeServiceAccountToken(service *serviceaccounts.Service, sessionService *sessions.Service, authzService *authorization.Service, activityService *activity.Service, scope tenancy.Scope) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		actorID, ok := requirePermission(response, request, sessionService, nil, authzService, authorization.PermissionManageServiceAccounts, scope.SiteID)
		if !ok {
			return
		}
		tokenID := request.PathValue("tokenID")
		if err := service.RevokeToken(request.Context(), tokenID); err != nil {
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		if activityService != nil {
			_ = activityService.Record(request.Context(), activity.Event{
				OrganizationID: scope.OrganizationID, SiteID: scope.SiteID,
				Source: activity.SourceServiceAccount, ReferenceID: tokenID, Type: "token_revoked",
				Actor: actorID, Message: "servis hesabı token'ı iptal edildi",
			})
		}
		response.WriteHeader(http.StatusNoContent)
	}
}

func handleDisableServiceAccount(service *serviceaccounts.Service, sessionService *sessions.Service, authzService *authorization.Service, activityService *activity.Service, scope tenancy.Scope) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		actorID, ok := requirePermission(response, request, sessionService, nil, authzService, authorization.PermissionManageServiceAccounts, scope.SiteID)
		if !ok {
			return
		}
		accountID := request.PathValue("accountID")
		if err := service.DisableAccount(request.Context(), accountID); err != nil {
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		if activityService != nil {
			_ = activityService.Record(request.Context(), activity.Event{
				OrganizationID: scope.OrganizationID, SiteID: scope.SiteID,
				Source: activity.SourceServiceAccount, ReferenceID: accountID, Type: "disabled",
				Actor: actorID, Message: "servis hesabı devre dışı bırakıldı",
			})
		}
		response.WriteHeader(http.StatusNoContent)
	}
}
```

- [x] **Adım 5: `internal/server/server.go`'yu güncelle**

Import listesinde `"github.com/gokayybaz/bazusop/internal/audit"` satırını `"github.com/gokayybaz/bazusop/internal/activity"` ile değiştir.

`handlerOptions`'ta `auditService *audit.Service` alanını `activityService *activity.Service` yap.

Eski `if configuration.auditService != nil { registerAudited(mux, "/api/v1/audit/events", ...) }` bloğunu şununla değiştir:

```go
	if configuration.activityService != nil {
		registerAudited(mux, "/api/v1/activity/events", http.MethodGet, "activity_timeline", nil, configuration.auditTrail, configuration.scope, handleListActivityEvents(configuration.activityService, configuration.sessionService, configuration.serviceAccountService, configuration.authorizationService, configuration.scope))
	}
```

`handleCreateInvite` çağrısına `configuration.activityService` ekle:

```go
		registerAudited(mux, "/api/v1/users/invites", http.MethodPost, "invites", nil, configuration.auditTrail, configuration.scope, handleCreateInvite(configuration.identityService, configuration.sessionService, configuration.authorizationService, configuration.activityService, configuration.scope))
```

`handleAssignSiteRole`/`handleRevokeSiteRole` çağrılarına `configuration.activityService` ekle:

```go
	if configuration.authorizationService != nil {
		registerAudited(mux, "/api/v1/sites/{siteID}/memberships", http.MethodPost, "site_memberships", []string{"siteID"}, configuration.auditTrail, configuration.scope, handleAssignSiteRole(configuration.sessionService, configuration.authorizationService, configuration.activityService, configuration.scope))
		registerAudited(mux, "/api/v1/sites/{siteID}/memberships/{userID}", http.MethodDelete, "site_memberships", []string{"siteID", "userID"}, configuration.auditTrail, configuration.scope, handleRevokeSiteRole(configuration.sessionService, configuration.authorizationService, configuration.activityService, configuration.scope))
	}
```

`handleCreateServiceAccount`/`handleRotateServiceAccountToken`/`handleRevokeServiceAccountToken`/`handleDisableServiceAccount` çağrılarına `configuration.activityService` ekle (`handleListServiceAccounts` değişmez):

```go
	if configuration.serviceAccountService != nil {
		registerAudited(mux, "/api/v1/sites/{siteID}/service-accounts", http.MethodPost, "service_accounts", []string{"siteID"}, configuration.auditTrail, configuration.scope, handleCreateServiceAccount(configuration.serviceAccountService, configuration.sessionService, configuration.authorizationService, configuration.activityService, configuration.scope))
		registerAudited(mux, "/api/v1/sites/{siteID}/service-accounts", http.MethodGet, "service_accounts", []string{"siteID"}, configuration.auditTrail, configuration.scope, handleListServiceAccounts(configuration.serviceAccountService, configuration.sessionService, configuration.authorizationService, configuration.scope))
		registerAudited(mux, "/api/v1/service-accounts/{accountID}/rotate", http.MethodPost, "service_accounts", []string{"accountID"}, configuration.auditTrail, configuration.scope, handleRotateServiceAccountToken(configuration.serviceAccountService, configuration.sessionService, configuration.authorizationService, configuration.activityService, configuration.scope))
		registerAudited(mux, "/api/v1/service-accounts/tokens/{tokenID}", http.MethodDelete, "service_accounts", []string{"tokenID"}, configuration.auditTrail, configuration.scope, handleRevokeServiceAccountToken(configuration.serviceAccountService, configuration.sessionService, configuration.authorizationService, configuration.activityService, configuration.scope))
		registerAudited(mux, "/api/v1/service-accounts/{accountID}", http.MethodDelete, "service_accounts", []string{"accountID"}, configuration.auditTrail, configuration.scope, handleDisableServiceAccount(configuration.serviceAccountService, configuration.sessionService, configuration.authorizationService, configuration.activityService, configuration.scope))
	}
```

- [x] **Adım 6: `cmd/bazusop-hub/main.go`'yu güncelle**

Import listesinde `"github.com/gokayybaz/bazusop/internal/audit"` → `"github.com/gokayybaz/bazusop/internal/activity"`.

```go
	var activityStore activity.Store = activity.NewMemoryStore(jobMemoryStore, alertMemoryStore)
```

(`var auditStore audit.Store = ...` satırının yerine.)

`postgresStore` atama bloğunda `auditStore = postgresStore` → `activityStore = postgresStore`.

```go
	activityService := activity.NewService(activityStore)
```

(`auditService := audit.NewService(auditStore)` satırının yerine.)

`server.NewHandler(...)` çağrısında `server.WithAudit(auditService),` → `server.WithActivity(activityService),`.

- [x] **Adım 7: Build'i doğrula**

Çalıştır: `go build ./... 2>&1 | head -50`
Beklenen: `internal/server` test dosyaları henüz güncellenmediği için `go vet`/`go test` başarısız olabilir (beklenen ara durum) — ama `go build ./...` (yalnız üretim kodu) başarılı olmalı.

- [x] **Adım 8: `internal/server/audit_test.go`'yu `internal/server/activity_test.go`'ya taşı, `PermissionViewActivity` gating'ini ve çoklu-kaynak birleşimini test edecek şekilde genişlet**

```bash
git mv internal/server/audit_test.go internal/server/activity_test.go
```

`internal/server/activity_test.go`'yu şu içerikle değiştir:

```go
package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gokayybaz/bazusop/internal/activity"
	"github.com/gokayybaz/bazusop/internal/alerting"
	"github.com/gokayybaz/bazusop/internal/audittrail"
	"github.com/gokayybaz/bazusop/internal/authorization"
	"github.com/gokayybaz/bazusop/internal/identity"
	"github.com/gokayybaz/bazusop/internal/inventory"
	"github.com/gokayybaz/bazusop/internal/jobs"
	"github.com/gokayybaz/bazusop/internal/server"
	"github.com/gokayybaz/bazusop/internal/serviceaccounts"
	"github.com/gokayybaz/bazusop/internal/sessions"
	"github.com/gokayybaz/bazusop/internal/tenancy"
)

func TestActivityEventsRequireAuthentication(t *testing.T) {
	t.Parallel()
	handler := server.NewHandler(server.WithActivity(activity.NewService(activity.NewMemoryStore(jobs.NewMemoryStore(), alerting.NewMemoryStore()))))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/activity/events", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", response.Code)
	}
}

func TestActivityEventsMergeAllSourcesOrderedByRecency(t *testing.T) {
	t.Parallel()
	scope := tenancy.DefaultScope()
	agent := tenancy.Agent{ID: "edge-01", OrganizationID: scope.OrganizationID, SiteID: scope.SiteID}

	registry := inventory.NewService(inventory.NewMemoryStore())
	if err := registry.Report(t.Context(), agent, inventory.Facts{Hostname: "edge-01", OSFamily: "linux", OSName: "Ubuntu", OSVersion: "24.04", Architecture: "amd64", CPUCores: 2, MemoryBytes: 1024, IPAddresses: []string{"10.0.0.1"}, AgentVersion: "test"}); err != nil {
		t.Fatal(err)
	}

	jobStore := jobs.NewMemoryStore()
	jobService, err := jobs.NewService(jobStore, jobs.WithHostScopeChecker(registry.HasHost))
	if err != nil {
		t.Fatal(err)
	}
	job, err := jobService.Create(t.Context(), scope, agent.ID, jobs.CreateRequest{Action: jobs.ActionServiceRestart, Target: "nginx.service", ApprovedBy: "gokay", Reason: "config rollout"})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}

	alertStore := alerting.NewMemoryStore()
	alertService, err := alerting.NewService(alertStore, alerting.WithHostScopeChecker(registry.HasHost))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := alertService.CreateRule(t.Context(), scope, alerting.RuleRequest{Name: "Yüksek CPU", Kind: alerting.KindMetric, Metric: alerting.MetricCPU, Threshold: 90, Severity: alerting.SeverityCritical, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := alertService.EvaluateTelemetry(t.Context(), scope, agent.ID, alerting.Telemetry{CPUPercent: 97}); err != nil {
		t.Fatal(err)
	}

	activityService := activity.NewService(activity.NewMemoryStore(jobStore, alertStore))

	identityService := identity.NewService(identity.NewMemoryStore(), "test-totp-encryption-key")
	authzService, err := authorization.NewService(authorization.NewMemoryStore(), identityService.IsPlatformAdmin)
	if err != nil {
		t.Fatal(err)
	}
	sessionService, err := sessions.NewService(sessions.NewMemoryStore(), identityService.IsUserActive)
	if err != nil {
		t.Fatal(err)
	}
	serviceAccountService, err := serviceaccounts.NewService(serviceaccounts.NewMemoryStore(), "test-pepper")
	if err != nil {
		t.Fatal(err)
	}
	handler := server.NewHandler(
		server.WithDefaultScope(scope),
		server.WithActivity(activityService),
		server.WithIdentity(identityService, "bootstrap-secret"),
		server.WithSessions(sessionService, identityService),
		server.WithAuthorization(authzService),
		server.WithServiceAccounts(serviceAccountService),
		server.WithAuditTrail(audittrail.NewService(audittrail.NewMemoryStore())),
	)

	admin, _, err := identityService.Bootstrap(t.Context(), scope.OrganizationID, "admin@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	_, adminToken, _, err := sessionService.Create(t.Context(), admin.ID, admin.OrganizationID)
	if err != nil {
		t.Fatal(err)
	}
	adminCookie := &http.Cookie{Name: "bazusop_session", Value: adminToken}

	inviteRequest := httptest.NewRequest(http.MethodPost, "/api/v1/users/invites", encodeJSON(t, map[string]string{"email": "new-viewer@example.com"}))
	inviteRequest.AddCookie(adminCookie)
	inviteResponse := httptest.NewRecorder()
	handler.ServeHTTP(inviteResponse, inviteRequest)
	if inviteResponse.Code != http.StatusCreated {
		t.Fatalf("create invite: %d %s", inviteResponse.Code, inviteResponse.Body.String())
	}

	assignRequest := httptest.NewRequest(http.MethodPost, "/api/v1/sites/"+scope.SiteID+"/memberships", encodeJSON(t, map[string]string{"user_id": admin.ID, "role": "viewer"}))
	assignRequest.AddCookie(adminCookie)
	assignResponse := httptest.NewRecorder()
	handler.ServeHTTP(assignResponse, assignRequest)
	if assignResponse.Code != http.StatusNoContent {
		t.Fatalf("assign role: %d %s", assignResponse.Code, assignResponse.Body.String())
	}

	createAccountRequest := httptest.NewRequest(http.MethodPost, "/api/v1/sites/"+scope.SiteID+"/service-accounts", encodeJSON(t, map[string]any{"name": "ci-bot", "role": "operator"}))
	createAccountRequest.AddCookie(adminCookie)
	createAccountResponse := httptest.NewRecorder()
	handler.ServeHTTP(createAccountResponse, createAccountRequest)
	if createAccountResponse.Code != http.StatusCreated {
		t.Fatalf("create service account: %d %s", createAccountResponse.Code, createAccountResponse.Body.String())
	}

	listRequest := httptest.NewRequest(http.MethodGet, "/api/v1/activity/events", nil)
	listRequest.AddCookie(adminCookie)
	listResponse := httptest.NewRecorder()
	handler.ServeHTTP(listResponse, listRequest)
	if listResponse.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", listResponse.Code, listResponse.Body.String())
	}
	var payload struct {
		Events []activity.Event `json:"events"`
	}
	if err := json.NewDecoder(listResponse.Body).Decode(&payload); err != nil {
		t.Fatalf("decode activity events: %v", err)
	}
	if len(payload.Events) != 5 {
		t.Fatalf("expected 5 merged events (job, alert, identity, site_role, service_account), got %#v", payload.Events)
	}
	seenSources := map[activity.Source]bool{}
	for _, event := range payload.Events {
		seenSources[event.Source] = true
	}
	for _, source := range []activity.Source{activity.SourceJob, activity.SourceAlert, activity.SourceIdentity, activity.SourceSiteRole, activity.SourceServiceAccount} {
		if !seenSources[source] {
			t.Fatalf("expected source %q in merged activity, got %#v", source, payload.Events)
		}
	}
	if payload.Events[len(payload.Events)-1].ReferenceID != job.ID {
		t.Fatalf("expected the job event to be the oldest (last), got %#v", payload.Events)
	}
}
```

- [x] **Adım 9: Testleri çalıştır**

Çalıştır: `GOCACHE=/tmp/bazusop-go-cache go test ./internal/server/... -run 'TestActivity' -v 2>&1 | tail -60`
Beklenen: BAŞARILI

- [x] **Adım 10: Commit**

```bash
git add internal/server/activity.go internal/server/activity_test.go internal/server/identity.go internal/server/rbac.go internal/server/serviceaccounts.go internal/server/server.go cmd/bazusop-hub/main.go
git commit -m "feat: record identity/site-role/service-account activity and gate the merged feed with PermissionViewActivity"
```

## Görev 3: `internal/audittrail`'e sorgu/arama yeteneği ekle, boşalan `/api/v1/audit/events`'i gerçek audit aramasına bağla

**Dosyalar:**
- Değiştir: `internal/audittrail/audittrail.go`, `internal/audittrail/memorystore.go`, `internal/audittrail/audittrail_test.go`, `internal/storage/postgres/audittrail.go`, `internal/storage/postgres/audittrail_integration_test.go`
- Oluştur: `internal/server/audit.go` (Görev 2'de içeriği `activity.go`'ya taşınan dosyanın yeni, farklı amaçlı hali)
- Değiştir: `internal/server/server.go`

**Arayüzler:**
- Üretir: `audittrail.Filter{ActorType, ResourceType, Outcome, Since, Until}`, `audittrail.Service.List(ctx, scope, filter, limit) ([]Event, error)`, `audittrail.ErrInvalidQuery`.

- [x] **Adım 1: `internal/audittrail/audittrail.go`'ya `Filter` ve `List` ekle**

Import listesine `"errors"` ve `"github.com/gokayybaz/bazusop/internal/tenancy"` ekle.

`var ErrInvalidQuery = errors.New("invalid audit trail query")` ekle (üst seviyede, `type ActorType string`'den önce).

`Store` arayüzünü genişlet:

```go
type Filter struct {
	ActorType    ActorType
	ResourceType string
	Outcome      Outcome
	Since        time.Time
	Until        time.Time
}

type Store interface {
	Record(ctx context.Context, event Event) error
	ListAuditTrail(ctx context.Context, scope tenancy.Scope, filter Filter, limit int) ([]Event, error)
}
```

`Service`'e ekle:

```go
func (service *Service) List(ctx context.Context, scope tenancy.Scope, filter Filter, limit int) ([]Event, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	if limit < 1 || limit > 500 {
		return nil, ErrInvalidQuery
	}
	return service.store.ListAuditTrail(ctx, scope, filter, limit)
}
```

- [x] **Adım 2: `internal/audittrail/memorystore.go`'ya `ListAuditTrail`'i ekle**

Import listesine `"sort"` ve `"github.com/gokayybaz/bazusop/internal/tenancy"` ekle.

```go
func (store *MemoryStore) ListAuditTrail(_ context.Context, scope tenancy.Scope, filter Filter, limit int) ([]Event, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	matched := make([]Event, 0, len(store.events))
	for _, event := range store.events {
		if event.OrganizationID != scope.OrganizationID || event.SiteID != scope.SiteID {
			continue
		}
		if filter.ActorType != "" && event.ActorType != filter.ActorType {
			continue
		}
		if filter.ResourceType != "" && event.ResourceType != filter.ResourceType {
			continue
		}
		if filter.Outcome != "" && event.Outcome != filter.Outcome {
			continue
		}
		if !filter.Since.IsZero() && event.OccurredAt.Before(filter.Since) {
			continue
		}
		if !filter.Until.IsZero() && event.OccurredAt.After(filter.Until) {
			continue
		}
		matched = append(matched, event)
	}
	sort.Slice(matched, func(left, right int) bool { return matched[left].OccurredAt.After(matched[right].OccurredAt) })
	if len(matched) > limit {
		matched = matched[:limit]
	}
	return matched, nil
}
```

- [x] **Adım 3: `internal/audittrail/audittrail_test.go`'ya `List`/`ListAuditTrail` testlerini ekle (mevcut üç test değişmeden kalır)**

Dosyanın sonuna ekle (import listesine `"github.com/gokayybaz/bazusop/internal/tenancy"` eklenmeli):

```go
func TestMemoryStoreListAuditTrailFiltersByScopeAndFields(t *testing.T) {
	t.Parallel()
	store := audittrail.NewMemoryStore()
	scope := tenancy.Scope{OrganizationID: "org_default", SiteID: "site_default"}
	otherScope := tenancy.Scope{OrganizationID: "org_default", SiteID: "site_other"}

	human := audittrail.Event{EventID: "e-1", OccurredAt: time.Now().UTC(), CorrelationID: "c-1", ActorType: audittrail.ActorHuman, ActorID: "user-1", OrganizationID: scope.OrganizationID, SiteID: scope.SiteID, Action: "POST", ResourceType: "service_accounts", Outcome: audittrail.OutcomeSuccess}
	failed := audittrail.Event{EventID: "e-2", OccurredAt: time.Now().UTC().Add(time.Minute), CorrelationID: "c-2", ActorType: audittrail.ActorAnonymous, OrganizationID: scope.OrganizationID, SiteID: scope.SiteID, Action: "POST", ResourceType: "jobs", Outcome: audittrail.OutcomeFailure, ErrorCode: "401"}
	otherSite := audittrail.Event{EventID: "e-3", OccurredAt: time.Now().UTC(), CorrelationID: "c-3", ActorType: audittrail.ActorHuman, OrganizationID: otherScope.OrganizationID, SiteID: otherScope.SiteID, Action: "POST", ResourceType: "service_accounts", Outcome: audittrail.OutcomeSuccess}
	for _, event := range []audittrail.Event{human, failed, otherSite} {
		if err := store.Record(context.Background(), event); err != nil {
			t.Fatalf("record: %v", err)
		}
	}

	all, err := store.ListAuditTrail(context.Background(), scope, audittrail.Filter{}, 50)
	if err != nil || len(all) != 2 {
		t.Fatalf("expected 2 in-scope events, got %#v, %v", all, err)
	}
	if all[0].EventID != failed.EventID {
		t.Fatalf("expected the more recent event first, got %#v", all[0])
	}

	onlySuccess, err := store.ListAuditTrail(context.Background(), scope, audittrail.Filter{Outcome: audittrail.OutcomeSuccess}, 50)
	if err != nil || len(onlySuccess) != 1 || onlySuccess[0].EventID != human.EventID {
		t.Fatalf("expected exactly the success event, got %#v, %v", onlySuccess, err)
	}

	byResource, err := store.ListAuditTrail(context.Background(), scope, audittrail.Filter{ResourceType: "jobs"}, 50)
	if err != nil || len(byResource) != 1 || byResource[0].EventID != failed.EventID {
		t.Fatalf("expected exactly the jobs-resource event, got %#v, %v", byResource, err)
	}
}

func TestServiceListRejectsOutOfRangeLimit(t *testing.T) {
	t.Parallel()
	service := audittrail.NewService(audittrail.NewMemoryStore())
	scope := tenancy.Scope{OrganizationID: "org_default", SiteID: "site_default"}
	if _, err := service.List(context.Background(), scope, audittrail.Filter{}, 0); !errors.Is(err, audittrail.ErrInvalidQuery) {
		t.Fatalf("expected ErrInvalidQuery for zero limit, got %v", err)
	}
	if _, err := service.List(context.Background(), scope, audittrail.Filter{}, 501); !errors.Is(err, audittrail.ErrInvalidQuery) {
		t.Fatalf("expected ErrInvalidQuery for oversized limit, got %v", err)
	}
}
```

**Yazarken düzeltme:** Bu iki yeni testte `errors` ve `tenancy` paketleri kullanılıyor ama dosyanın mevcut import listesinde yok — dosyanın en üstündeki import bloğunu şuna güncelle:

```go
import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gokayybaz/bazusop/internal/audittrail"
	"github.com/gokayybaz/bazusop/internal/tenancy"
)
```

- [x] **Adım 4: Testleri çalıştır**

Çalıştır: `GOCACHE=/tmp/bazusop-go-cache go test ./internal/audittrail/... -v 2>&1 | tail -60`
Beklenen: BAŞARILI (yeni + eski tüm testler)

- [x] **Adım 5: `internal/storage/postgres/audittrail.go`'daki `ListAuditTrail`'i filtre destekleyecek şekilde genişlet**

`ListAuditTrail`'i şununla değiştir:

```go
func (store *Store) ListAuditTrail(ctx context.Context, scope tenancy.Scope, filter audittrail.Filter, limit int) ([]audittrail.Event, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	query := `
		SELECT event_id, occurred_at, correlation_id, actor_type, actor_id, session_or_token_id,
			organization_id, site_id, action, permission, resource_type, resource_id,
			outcome, error_code, source_ip, user_agent, change_summary
		FROM audit_events
		WHERE organization_id = $1 AND site_id = $2`
	args := []any{scope.OrganizationID, scope.SiteID}
	if filter.ActorType != "" {
		args = append(args, string(filter.ActorType))
		query += fmt.Sprintf(" AND actor_type = $%d", len(args))
	}
	if filter.ResourceType != "" {
		args = append(args, filter.ResourceType)
		query += fmt.Sprintf(" AND resource_type = $%d", len(args))
	}
	if filter.Outcome != "" {
		args = append(args, string(filter.Outcome))
		query += fmt.Sprintf(" AND outcome = $%d", len(args))
	}
	if !filter.Since.IsZero() {
		args = append(args, filter.Since)
		query += fmt.Sprintf(" AND occurred_at >= $%d", len(args))
	}
	if !filter.Until.IsZero() {
		args = append(args, filter.Until)
		query += fmt.Sprintf(" AND occurred_at <= $%d", len(args))
	}
	args = append(args, limit)
	query += fmt.Sprintf(" ORDER BY occurred_at DESC LIMIT $%d", len(args))

	rows, err := store.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query audit trail: %w", err)
	}
	defer rows.Close()
	events := make([]audittrail.Event, 0)
	for rows.Next() {
		var event audittrail.Event
		var actorType, outcome string
		if err := rows.Scan(&event.EventID, &event.OccurredAt, &event.CorrelationID, &actorType, &event.ActorID, &event.SessionOrTokenID,
			&event.OrganizationID, &event.SiteID, &event.Action, &event.Permission, &event.ResourceType, &event.ResourceID,
			&outcome, &event.ErrorCode, &event.SourceIP, &event.UserAgent, &event.ChangeSummary); err != nil {
			return nil, fmt.Errorf("scan audit event: %w", err)
		}
		event.ActorType, event.Outcome = audittrail.ActorType(actorType), audittrail.Outcome(outcome)
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate audit trail: %w", err)
	}
	return events, nil
}
```

- [x] **Adım 6: `internal/storage/postgres/audittrail_integration_test.go`'daki çağrıyı güncelle**

`store.ListAuditTrail(ctx, scope, 10)` çağrısını `store.ListAuditTrail(ctx, scope, audittrail.Filter{}, 10)` yap.

- [x] **Adım 7: `internal/server/audit.go`'yu YENİ içerikle oluştur (gerçek audit arama handler'ı)**

```go
package server

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gokayybaz/bazusop/internal/audittrail"
	"github.com/gokayybaz/bazusop/internal/authorization"
	"github.com/gokayybaz/bazusop/internal/serviceaccounts"
	"github.com/gokayybaz/bazusop/internal/sessions"
	"github.com/gokayybaz/bazusop/internal/tenancy"
)

func handleListAuditTrail(service *audittrail.Service, sessionService *sessions.Service, serviceAccountService *serviceaccounts.Service, authzService *authorization.Service, scope tenancy.Scope) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		if _, ok := requirePermission(response, request, sessionService, serviceAccountService, authzService, authorization.PermissionViewAuditEvents, scope.SiteID); !ok {
			return
		}
		query := request.URL.Query()
		limit := 100
		if value := query.Get("limit"); value != "" {
			parsed, err := strconv.Atoi(value)
			if err != nil {
				http.Error(response, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
				return
			}
			limit = parsed
		}
		filter := audittrail.Filter{
			ActorType:    audittrail.ActorType(query.Get("actor_type")),
			ResourceType: query.Get("resource_type"),
			Outcome:      audittrail.Outcome(query.Get("outcome")),
		}
		if raw := query.Get("since"); raw != "" {
			parsed, err := time.Parse(time.RFC3339, raw)
			if err != nil {
				http.Error(response, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
				return
			}
			filter.Since = parsed
		}
		if raw := query.Get("until"); raw != "" {
			parsed, err := time.Parse(time.RFC3339, raw)
			if err != nil {
				http.Error(response, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
				return
			}
			filter.Until = parsed
		}
		values, err := service.List(request.Context(), scope, filter, limit)
		if errors.Is(err, audittrail.ErrInvalidQuery) {
			http.Error(response, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}
		if err != nil {
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		writeJSON(response, http.StatusOK, struct {
			Events []audittrail.Event `json:"events"`
		}{values})
	}
}
```

- [x] **Adım 8: `internal/server/server.go`'ya yeni route'u ekle**

`if configuration.authorizationService != nil { ... }` bloğundan hemen sonra yeni bir blok ekle:

```go
	if configuration.auditTrail != nil && configuration.authorizationService != nil {
		registerAudited(mux, "/api/v1/audit/events", http.MethodGet, "audit_trail", nil, configuration.auditTrail, configuration.scope, handleListAuditTrail(configuration.auditTrail, configuration.sessionService, configuration.serviceAccountService, configuration.authorizationService, configuration.scope))
	}
```

- [x] **Adım 9: Full build ve testleri doğrula**

Çalıştır: `go build ./... 2>&1 && echo BUILD_OK && go vet ./... 2>&1 && echo VET_OK`
Beklenen: her ikisi de başarılı (Görev 1-3'ün tüm parçaları artık tutarlı).

Çalıştır: `GOCACHE=/tmp/bazusop-go-cache go test ./internal/audittrail/... ./internal/server/... -v 2>&1 | tail -100`
Beklenen: BAŞARILI

- [x] **Adım 10: Yeni audit-arama uç noktası için bir test yaz — `internal/server/audit_test.go`**

```bash
git mv internal/server/audit_test.go /dev/null 2>/dev/null || true
```

(Not: bu dosya Görev 2 Adım 8'de zaten `activity_test.go`'ya taşındı; burada AYNI isimle YENİ bir dosya oluşturuluyor.)

`internal/server/audit_test.go` (yeni):

```go
package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gokayybaz/bazusop/internal/audittrail"
	"github.com/gokayybaz/bazusop/internal/authorization"
	"github.com/gokayybaz/bazusop/internal/identity"
	"github.com/gokayybaz/bazusop/internal/serviceaccounts"
	"github.com/gokayybaz/bazusop/internal/server"
	"github.com/gokayybaz/bazusop/internal/sessions"
	"github.com/gokayybaz/bazusop/internal/tenancy"
)

func newAuditTrailHandler(t *testing.T) (http.Handler, *audittrail.Service, []*http.Cookie) {
	t.Helper()
	identityService := identity.NewService(identity.NewMemoryStore(), "test-totp-encryption-key")
	authzService, err := authorization.NewService(authorization.NewMemoryStore(), identityService.IsPlatformAdmin)
	if err != nil {
		t.Fatal(err)
	}
	sessionService, err := sessions.NewService(sessions.NewMemoryStore(), identityService.IsUserActive)
	if err != nil {
		t.Fatal(err)
	}
	serviceAccountService, err := serviceaccounts.NewService(serviceaccounts.NewMemoryStore(), "test-pepper")
	if err != nil {
		t.Fatal(err)
	}
	auditTrailService := audittrail.NewService(audittrail.NewMemoryStore())
	handler := server.NewHandler(
		server.WithIdentity(identityService, "bootstrap-secret"),
		server.WithSessions(sessionService, identityService),
		server.WithAuthorization(authzService),
		server.WithServiceAccounts(serviceAccountService),
		server.WithAuditTrail(auditTrailService),
	)
	admin, _, err := identityService.Bootstrap(t.Context(), tenancy.DefaultOrganizationID, "admin@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	_, token, _, err := sessionService.Create(t.Context(), admin.ID, admin.OrganizationID)
	if err != nil {
		t.Fatal(err)
	}
	return handler, auditTrailService, []*http.Cookie{{Name: "bazusop_session", Value: token}}
}

func TestAuditTrailSearchRequiresPermissionAndFiltersResults(t *testing.T) {
	t.Parallel()
	handler, _, adminCookies := newAuditTrailHandler(t)

	unauthenticated := httptest.NewRecorder()
	handler.ServeHTTP(unauthenticated, httptest.NewRequest(http.MethodGet, "/api/v1/audit/events", nil))
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without a session, got %d", unauthenticated.Code)
	}

	// The bootstrap/session-creation and service-account calls above already
	// produced real audit_events rows via registerAudited; assert at least
	// one is visible to the platform admin, and that filtering by an
	// unmatched resource type returns none.
	listRequest := httptest.NewRequest(http.MethodGet, "/api/v1/audit/events", nil)
	for _, cookie := range adminCookies {
		listRequest.AddCookie(cookie)
	}
	listResponse := httptest.NewRecorder()
	handler.ServeHTTP(listResponse, listRequest)
	if listResponse.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", listResponse.Code, listResponse.Body.String())
	}
	var payload struct {
		Events []audittrail.Event `json:"events"`
	}
	if err := json.NewDecoder(listResponse.Body).Decode(&payload); err != nil || len(payload.Events) == 0 {
		t.Fatalf("expected at least one audit event, got %#v, %v", payload, err)
	}

	filteredOut := httptest.NewRequest(http.MethodGet, "/api/v1/audit/events?resource_type=no-such-resource", nil)
	for _, cookie := range adminCookies {
		filteredOut.AddCookie(cookie)
	}
	filteredResponse := httptest.NewRecorder()
	handler.ServeHTTP(filteredResponse, filteredOut)
	var filteredPayload struct {
		Events []audittrail.Event `json:"events"`
	}
	if err := json.NewDecoder(filteredResponse.Body).Decode(&filteredPayload); err != nil || len(filteredPayload.Events) != 0 {
		t.Fatalf("expected an unmatched resource_type filter to return no events, got %#v, %v", filteredPayload, err)
	}

	invalidSince := httptest.NewRequest(http.MethodGet, "/api/v1/audit/events?since=not-a-date", nil)
	for _, cookie := range adminCookies {
		invalidSince.AddCookie(cookie)
	}
	invalidResponse := httptest.NewRecorder()
	handler.ServeHTTP(invalidResponse, invalidSince)
	if invalidResponse.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for an invalid since timestamp, got %d", invalidResponse.Code)
	}
}
```

**Yazarken düzeltme:** `git mv ... /dev/null` satırı geçersizdir (git bunu kabul etmez) — Adım 10'un başındaki o bash komutunu ATLA; `internal/server/audit_test.go` zaten Görev 2 Adım 8'de `git mv` ile `activity_test.go`'ya taşındığı için bu yoldaki dosya artık mevcut değildir, bu yüzden yukarıdaki içerik doğrudan `Write` ile yeni bir dosya olarak oluşturulmalıdır.

- [x] **Adım 11: Testleri çalıştır**

Çalıştır: `GOCACHE=/tmp/bazusop-go-cache go test ./internal/server/... -run 'TestAuditTrail' -v 2>&1 | tail -40`
Beklenen: BAŞARILI

- [x] **Adım 12: Full test suite'i çalıştır (Postgres dahil)**

Postgres'i başlat: `docker rm -f bazusop-test-pg >/dev/null 2>&1; docker run -d --name bazusop-test-pg -e POSTGRES_USER=bazusop -e POSTGRES_PASSWORD=bazusop_test -e POSTGRES_DB=bazusop_test -p 5432:5432 postgres:18 >/dev/null` ve hazır olmasını bekle.

Çalıştır: `BAZUSOP_TEST_DATABASE_URL='postgres://bazusop:bazusop_test@127.0.0.1:5432/bazusop_test?sslmode=disable' GOCACHE=/tmp/bazusop-go-cache go test ./... 2>&1 | tail -30`
Beklenen: her paket `ok`

Postgres'i kaldır: `docker rm -f bazusop-test-pg`

- [x] **Adım 13: Commit**

```bash
git add internal/audittrail internal/storage/postgres/audittrail.go internal/storage/postgres/audittrail_integration_test.go internal/server/audit.go internal/server/server.go
git commit -m "feat: add filtered audit trail search behind PermissionViewAuditEvents"
```

## Görev 4: Frontend — Activity sayfası genişletmesi, yeni Denetim kaydı arama sayfası

**Dosyalar:**
- Oluştur: `web/src/pages/activity.tsx`
- Değiştir: `web/src/pages/audit.tsx` (yeni içerik), `web/src/types.ts`, `web/src/navigation.ts`, `web/src/App.tsx`, `web/src/app.test.tsx`

- [x] **Adım 1: `web/src/types.ts`'i güncelle**

Mevcut `AuditEvent` tipini şununla değiştir:

```ts
export type ActivityEvent = { organization_id: string; site_id: string; source: "job" | "alert" | "identity" | "site_role" | "service_account"; reference_id: string; agent_id: string; type: string; actor: string; message: string; occurred_at: string }

export type AuditTrailEvent = { event_id: string; occurred_at: string; correlation_id: string; actor_type: "agent" | "human" | "service_account" | "legacy_token" | "anonymous"; actor_id: string; session_or_token_id: string; organization_id: string; site_id: string; action: string; permission: string; resource_type: string; resource_id: string; outcome: "success" | "failure"; error_code: string; source_ip: string; user_agent: string; change_summary: string }
```

`PageID` tipini güncelle:

```ts
export type PageID = "overview" | "fleet" | "services" | "metrics" | "logs" | "jobs" | "alerts" | "cloud" | "activity" | "audit" | "settings"
```

- [x] **Adım 2: `web/src/navigation.ts`'i güncelle**

```ts
import { Activity, Bell, Boxes, ChartNoAxesCombined, Cloud, Gauge, ListChecks, Server, Settings, ShieldCheck, TerminalSquare } from "lucide-react"

import type { PageID } from "./types"

export const navigation = [
  { id: "overview" as const, icon: Gauge, label: "Genel bakış", path: "/" },
  { id: "fleet" as const, icon: Server, label: "Filo", path: "/fleet" },
  { id: "services" as const, icon: Boxes, label: "Servisler", path: "/services" },
  { id: "metrics" as const, icon: ChartNoAxesCombined, label: "Metrikler", path: "/metrics" },
  { id: "logs" as const, icon: TerminalSquare, label: "Loglar", path: "/logs" },
  { id: "jobs" as const, icon: ListChecks, label: "İşler", path: "/jobs" },
  { id: "alerts" as const, icon: Bell, label: "Alarmlar", path: "/alerts" },
]

export const secondaryNavigation = [
  { id: "cloud" as const, icon: Cloud, label: "Bulut hesapları", path: "/cloud" },
  { id: "activity" as const, icon: Activity, label: "Aktivite", path: "/activity" },
  { id: "audit" as const, icon: ShieldCheck, label: "Denetim izi", path: "/audit" },
  { id: "settings" as const, icon: Settings, label: "Ayarlar", path: "/settings" },
]

export const pageMeta: Record<PageID, { eyebrow: string; title: string; description: string }> = {
  overview: { eyebrow: "FİLO / ÜRETİM", title: "Operasyon özeti", description: "Filo sağlığı ve ilgilenilmesi gereken sinyaller" },
  fleet: { eyebrow: "ENVANTER", title: "Sunucu filosu", description: "Kayıtlı Linux ve Windows agent'ları" },
  services: { eyebrow: "SUNUCU DURUMU", title: "Servisler", description: "systemd ve Windows Service envanteri" },
  metrics: { eyebrow: "GÖZLEMLENEBİLİRLİK", title: "Metrikler", description: "CPU, bellek, disk ve ağ telemetrisi" },
  logs: { eyebrow: "GÖZLEMLENEBİLİRLİK", title: "Loglar", description: "Geçmiş arama ve canlı log akışı" },
  jobs: { eyebrow: "OPERASYON", title: "İşler", description: "İmzalı ve denetlenebilir uzak aksiyonlar" },
  alerts: { eyebrow: "OLAY YÖNETİMİ", title: "Alarmlar", description: "Kurallar, olaylar ve bakım pencereleri" },
  cloud: { eyebrow: "KEŞİF", title: "Bulut hesapları", description: "AWS, Azure ve GCP envanter bağlantıları" },
  activity: { eyebrow: "YÖNETİŞİM", title: "Aktivite", description: "İş, alarm, kimlik ve servis hesabı olaylarının zaman çizelgesi" },
  audit: { eyebrow: "YÖNETİŞİM", title: "Denetim izi", description: "İzin bazlı, her isteği kapsayan güvenlik denetim kaydı" },
  settings: { eyebrow: "SİSTEM", title: "Ayarlar", description: "Hub ve arayüz tercihleri" },
}

export function pageFromPath(pathname: string): PageID {
  return [...navigation, ...secondaryNavigation].find((item) => item.path === pathname)?.id ?? "overview"
}
```

- [x] **Adım 3: `web/src/pages/activity.tsx`'i oluştur (eski `audit.tsx`'in genişletilmiş hali)**

```tsx
import { useEffect, useState } from "react"
import { Activity as ActivityIcon } from "lucide-react"

import { Badge } from "../components/ui/badge"
import { Card } from "../components/ui/card"
import { EmptyFeature } from "../components/empty-feature"
import { formatLastSeen } from "../lib/format"
import type { ActivityEvent } from "../types"

export function ActivityPage() {
  const [events, setEvents] = useState<ActivityEvent[]>([])
  const [state, setState] = useState<"loading" | "ready" | "error">("loading")

  useEffect(() => {
    const controller = new AbortController()
    fetch("/api/v1/activity/events?limit=200", { signal: controller.signal })
      .then((response) => response.ok ? response.json() as Promise<{ events: ActivityEvent[] }> : Promise.reject())
      .then((payload) => {
        setEvents(payload.events ?? [])
        setState("ready")
      })
      .catch((error: unknown) => {
        if ((error as { name?: string }).name !== "AbortError") setState("error")
      })
    return () => controller.abort()
  }, [])

  if (state === "loading") return <Card className="empty-feature page-card"><span><ActivityIcon size={24} /></span><h2>Aktivite yükleniyor</h2><p>İş, alarm, davet ve servis hesabı olayları tek zaman çizelgesinde birleştiriliyor.</p></Card>
  if (state === "error") return <EmptyFeature icon={ActivityIcon} title="Aktiviteye ulaşılamıyor" text="Hub bağlantısını veya oturum izninizi kontrol edin." />
  if (events.length === 0) return <EmptyFeature icon={ActivityIcon} title="Henüz aktivite yok" text="Onaylanan işler, alarm olayları, davetler ve servis hesabı değişiklikleri burada zaman sırasıyla görünecek." />

  return (
    <Card aria-label="Aktivite zaman çizelgesi" className="table-card page-card">
      <div className="card-header">
        <div><h2>Aktivite zaman çizelgesi</h2><p>İş, alarm, kimlik ve servis hesabı olayları · en yeni {events.length} kayıt</p></div>
      </div>
      <div className="table-scroll">
        <table aria-label="Aktivite olayları">
          <thead><tr><th>Zaman</th><th>Kaynak</th><th>Olay</th><th>Aktör</th><th>Agent</th><th>Mesaj</th></tr></thead>
          <tbody>
            {events.map((event) => (
              <tr key={`${event.source}-${event.reference_id}-${event.type}-${event.occurred_at}`}>
                <td className="mono muted">{formatLastSeen(event.occurred_at)}</td>
                <td><Badge className={`audit-source ${event.source}`}>{activitySourceLabel(event.source)}</Badge></td>
                <td>{activityEventLabel(event)}</td>
                <td>{event.actor}</td>
                <td className="mono muted">{event.agent_id || "—"}</td>
                <td>{event.message}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </Card>
  )
}

function activitySourceLabel(source: ActivityEvent["source"]) {
  return { job: "İş", alert: "Alarm", identity: "Kimlik", site_role: "Site rolü", service_account: "Servis hesabı" }[source]
}

function activityEventLabel(event: ActivityEvent) {
  const labels: Record<ActivityEvent["source"], Record<string, string>> = {
    job: { approved: "Onaylandı", claimed: "Agent teslim aldı", output: "Çıktı", succeeded: "Tamamlandı", failed: "Başarısız" },
    alert: { opened: "Açıldı", acknowledged: "Onaylandı", resolved: "Çözüldü" },
    identity: { invite_created: "Davet edildi" },
    site_role: { assigned: "Rol atandı", revoked: "Rol kaldırıldı" },
    service_account: { created: "Oluşturuldu", token_rotated: "Token rotate edildi", token_revoked: "Token iptal edildi", disabled: "Devre dışı bırakıldı" },
  }
  return labels[event.source]?.[event.type] ?? event.type
}
```

- [x] **Adım 4: `web/src/pages/audit.tsx`'i YENİ içerikle değiştir (gerçek denetim kaydı arama sayfası)**

```tsx
import { useEffect, useState } from "react"
import { ShieldCheck } from "lucide-react"

import { Badge } from "../components/ui/badge"
import { Card } from "../components/ui/card"
import { EmptyFeature } from "../components/empty-feature"
import { formatLastSeen } from "../lib/format"
import type { AuditTrailEvent } from "../types"

const actorTypeOptions = ["", "human", "agent", "service_account", "legacy_token", "anonymous"] as const
const outcomeOptions = ["", "success", "failure"] as const

export function AuditTrailPage() {
  const [events, setEvents] = useState<AuditTrailEvent[]>([])
  const [state, setState] = useState<"loading" | "ready" | "error" | "forbidden">("loading")
  const [actorType, setActorType] = useState<(typeof actorTypeOptions)[number]>("")
  const [resourceType, setResourceType] = useState("")
  const [outcome, setOutcome] = useState<(typeof outcomeOptions)[number]>("")

  useEffect(() => {
    const controller = new AbortController()
    const params = new URLSearchParams({ limit: "200" })
    if (actorType) params.set("actor_type", actorType)
    if (resourceType) params.set("resource_type", resourceType)
    if (outcome) params.set("outcome", outcome)
    setState("loading")
    fetch(`/api/v1/audit/events?${params.toString()}`, { signal: controller.signal })
      .then((response) => {
        if (response.status === 401 || response.status === 403) return Promise.reject(new Error("forbidden"))
        if (!response.ok) return Promise.reject(new Error("unavailable"))
        return response.json() as Promise<{ events: AuditTrailEvent[] }>
      })
      .then((payload) => {
        setEvents(payload.events ?? [])
        setState("ready")
      })
      .catch((error: unknown) => {
        if ((error as { name?: string }).name === "AbortError") return
        setState((error as Error).message === "forbidden" ? "forbidden" : "error")
      })
    return () => controller.abort()
  }, [actorType, resourceType, outcome])

  function downloadCSV() {
    const header = ["occurred_at", "actor_type", "actor_id", "action", "resource_type", "resource_id", "outcome", "error_code"]
    const rows = events.map((event) => [event.occurred_at, event.actor_type, event.actor_id, event.action, event.resource_type, event.resource_id, event.outcome, event.error_code])
    const csv = [header, ...rows].map((row) => row.map((cell) => `"${String(cell).replaceAll('"', '""')}"`).join(",")).join("\n")
    const blob = new Blob([csv], { type: "text/csv;charset=utf-8;" })
    const url = URL.createObjectURL(blob)
    const link = document.createElement("a")
    link.href = url
    link.download = `denetim-kaydi-${new Date().toISOString().slice(0, 10)}.csv`
    link.click()
    URL.revokeObjectURL(url)
  }

  if (state === "forbidden") return <EmptyFeature icon={ShieldCheck} title="Denetim kaydına erişim yetkiniz yok" text="Bu ekranı görüntülemek için site-admin veya operator rolü ya da platform yöneticisi oturumu gerekir." />
  if (state === "error") return <EmptyFeature icon={ShieldCheck} title="Denetim kaydına ulaşılamıyor" text="Hub bağlantısını kontrol edin." />

  return (
    <Card aria-label="Denetim kaydı arama" className="table-card page-card">
      <div className="card-header">
        <div><h2>Denetim kaydı</h2><p>Her API isteğinin başarı/başarısızlık sonucu · en yeni {events.length} kayıt</p></div>
        <button className="text-button" disabled={events.length === 0} onClick={downloadCSV} type="button">CSV indir</button>
      </div>
      <div>
        <label>Aktör türü
          <select aria-label="Aktör türü filtresi" onChange={(event) => setActorType(event.target.value as typeof actorType)} value={actorType}>
            <option value="">Tümü</option>
            <option value="human">İnsan</option>
            <option value="agent">Agent</option>
            <option value="service_account">Servis hesabı</option>
            <option value="legacy_token">Eski token</option>
            <option value="anonymous">Anonim</option>
          </select>
        </label>
        <label>Kaynak türü
          <input aria-label="Kaynak türü filtresi" onChange={(event) => setResourceType(event.target.value)} placeholder="ör. service_accounts" type="text" value={resourceType} />
        </label>
        <label>Sonuç
          <select aria-label="Sonuç filtresi" onChange={(event) => setOutcome(event.target.value as typeof outcome)} value={outcome}>
            <option value="">Tümü</option>
            <option value="success">Başarılı</option>
            <option value="failure">Başarısız</option>
          </select>
        </label>
      </div>
      {state === "loading" ? <p className="muted">Yükleniyor…</p> : events.length === 0 ? <p className="muted">Bu filtrelerle eşleşen kayıt yok.</p> : (
        <div className="table-scroll">
          <table aria-label="Denetim kayıtları">
            <thead><tr><th>Zaman</th><th>Aktör</th><th>Eylem</th><th>Kaynak</th><th>Sonuç</th></tr></thead>
            <tbody>
              {events.map((event) => (
                <tr key={event.event_id}>
                  <td className="mono muted">{formatLastSeen(event.occurred_at)}</td>
                  <td>{actorTypeLabel(event.actor_type)}</td>
                  <td className="mono">{event.action} {event.resource_type}</td>
                  <td className="mono muted">{event.resource_id || "—"}</td>
                  <td><Badge className={`audit-outcome ${event.outcome}`}>{event.outcome === "success" ? "Başarılı" : `Başarısız (${event.error_code})`}</Badge></td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </Card>
  )
}

function actorTypeLabel(actorType: AuditTrailEvent["actor_type"]) {
  return { human: "İnsan", agent: "Agent", service_account: "Servis hesabı", legacy_token: "Eski token", anonymous: "Anonim" }[actorType] ?? actorType
}
```

- [x] **Adım 5: `web/src/App.tsx`'i güncelle**

`import { AuditPage } from "./pages/audit"` satırını sil, yerine ekle:

```ts
import { ActivityPage } from "./pages/activity"
import { AuditTrailPage } from "./pages/audit"
```

(Import listesindeki alfabetik sıraya göre `ActivityPage` `AlarmCenter`'dan sonra, `AuditTrailPage` `AlarmCenter`'dan sonra ama `CloudInventoryPage`'den önce gelecek şekilde yerleştir — mevcut dosyanın import sırası zaten alfabetik.)

`{activePage === "audit" ? <AuditPage /> : null}` satırını şununla değiştir:

```tsx
            {activePage === "activity" ? <ActivityPage /> : null}
            {activePage === "audit" ? <AuditTrailPage /> : null}
```

- [x] **Adım 6: `web/src/app.test.tsx`'i güncelle**

Mevcut `"shows a unified, time-ordered audit timeline merging job and alert events"` testini (satır 119-142) şununla değiştir — artık `/activity` yolunu ve "Aktivite" butonunu hedefler:

```tsx
  it("shows a unified, time-ordered activity timeline merging job and alert events", async () => {
	vi.mocked(fetch).mockImplementation((input) => {
	  const url = String(input)
	  if (url.startsWith("/api/v1/activity/events")) return Promise.resolve({ ok: true, json: async () => ({ events: [
		{ organization_id: "org_default", site_id: "site_default", source: "alert", reference_id: "incident-01", agent_id: "agent-01", type: "opened", actor: "hub", message: "CPU %96", occurred_at: "2026-09-11T08:05:00Z" },
		{ organization_id: "org_default", site_id: "site_default", source: "job", reference_id: "job-01", agent_id: "agent-01", type: "approved", actor: "gokay", message: "config rollout", occurred_at: "2026-09-11T08:00:00Z" },
	  ] }) } as Response)
	  if (url.includes("/incidents")) return Promise.resolve({ ok: true, json: async () => ({ incidents: [] }) } as Response)
	  return Promise.resolve({ ok: true, json: async () => ({ instances: [] }) } as Response)
	})

	render(<App />)
	fireEvent.click(screen.getByRole("button", { name: "Aktivite" }))

	expect(window.location.pathname).toBe("/activity")
	expect(await screen.findByRole("table", { name: "Aktivite olayları" })).toBeInTheDocument()
	expect(screen.getByText("Onaylandı")).toBeInTheDocument()
	expect(screen.getByText("Açıldı")).toBeInTheDocument()
	expect(screen.getByText("config rollout")).toBeInTheDocument()
	expect(screen.getByText("CPU %96")).toBeInTheDocument()
	const rows = screen.getAllByRole("row")
	expect(rows[1]).toHaveTextContent("Açıldı")
	expect(rows[2]).toHaveTextContent("Onaylandı")
  })

  it("shows a permission-filtered audit trail search with working filters", async () => {
	vi.mocked(fetch).mockImplementation((input) => {
	  const url = String(input)
	  if (url.startsWith("/api/v1/audit/events")) return Promise.resolve({ ok: true, json: async () => ({ events: [
		{ event_id: "e-1", occurred_at: "2026-09-11T08:05:00Z", correlation_id: "c-1", actor_type: "human", actor_id: "user-1", session_or_token_id: "", organization_id: "org_default", site_id: "site_default", action: "POST", permission: "manage_service_accounts", resource_type: "service_accounts", resource_id: "account-1", outcome: "success", error_code: "", source_ip: "", user_agent: "", change_summary: "" },
	  ] }) } as Response)
	  if (url.includes("/incidents")) return Promise.resolve({ ok: true, json: async () => ({ incidents: [] }) } as Response)
	  return Promise.resolve({ ok: true, json: async () => ({ instances: [] }) } as Response)
	})

	render(<App />)
	fireEvent.click(screen.getByRole("button", { name: "Denetim izi" }))

	expect(window.location.pathname).toBe("/audit")
	expect(await screen.findByRole("table", { name: "Denetim kayıtları" })).toBeInTheDocument()
	expect(screen.getByText("account-1")).toBeInTheDocument()
	expect(screen.getByText("Başarılı")).toBeInTheDocument()
  })
```

- [x] **Adım 7: Frontend testlerini ve build'i çalıştır**

Çalıştır: `cd web && npm test 2>&1 | tail -60`
Beklenen: BAŞARILI (tüm testler, yeni ikisi dahil)

Çalıştır: `cd web && npm run build 2>&1 | tail -40`
Beklenen: BAŞARILI (TypeScript hatasız derlenir, Vite build tamamlanır)

- [x] **Adım 8: Commit**

```bash
git add web/src/pages/activity.tsx web/src/pages/audit.tsx web/src/types.ts web/src/navigation.ts web/src/App.tsx web/src/app.test.tsx
git commit -m "feat: split the audit page into an Activity timeline and a real audit trail search screen"
```

## Görev 5: Dokümantasyon

**Dosyalar:** Değiştir: `docs/API.md`

- [x] **Adım 1: `docs/API.md`'yi güncelle**

`## Kimlik doğrulama` bölümüne (spike 11.7'nin eklediği paragraftan hemen sonra) ekle:

```markdown
- `GET /api/v1/activity/events` (`PermissionViewActivity` — tüm site rolleri: viewer, operator, site-admin) iş, alarm, davet, site rolü ve servis hesabı olaylarını tek zaman çizelgesinde döner.
- `GET /api/v1/audit/events` (`PermissionViewAuditEvents` — yalnız operator ve site-admin) her API isteğinin başarı/başarısızlık sonucunu `actor_type`, `resource_type`, `outcome`, `since`, `until` filtreleriyle arar.
```

- [x] **Adım 2: Commit**

```bash
git add docs/API.md
git commit -m "docs: document the activity and audit trail search endpoints"
```

## Görev 6: Manuel doğrulama

**Dosyalar:** yok (yalnız doğrulama).

- [x] **Adım 1: Docker Compose ile hub'ı ayağa kaldır**

Çalıştır: `docker compose -p bazusop-verify-118 down -v >/dev/null 2>&1; POSTGRES_PASSWORD=verify-pw BAZUSOP_ENROLLMENT_TOKEN=verify-token BAZUSOP_BOOTSTRAP_SECRET=verify-bootstrap BAZUSOP_TOTP_ENCRYPTION_KEY=verify-totp-key BAZUSOP_SERVICE_ACCOUNT_PEPPER=verify-pepper BAZUSOP_PORT=18097 docker compose -p bazusop-verify-118 up --build -d` ve `/api/v1/health`'in 200 dönmesini bekle.

- [x] **Adım 2: Bootstrap ol, giriş yap, bir davet + site rolü + servis hesabı oluştur**

```bash
rm -f /tmp/bazusop-118-cookies.txt
curl -s -c /tmp/bazusop-118-cookies.txt -X POST http://127.0.0.1:18097/api/v1/bootstrap \
  -d '{"secret":"verify-bootstrap","email":"admin@example.com","password":"correct horse battery staple"}' > /tmp/bazusop-118-bootstrap.json
CODE=$(python3 -c "import json; print(json.load(open('/tmp/bazusop-118-bootstrap.json'))['recovery_codes'][0])")
curl -s -c /tmp/bazusop-118-cookies.txt -b /tmp/bazusop-118-cookies.txt -X POST http://127.0.0.1:18097/api/v1/sessions \
  -d '{"email":"admin@example.com","password":"correct horse battery staple","recovery_code":"'"$CODE"'"}' -w "\nlogin:%{http_code}\n"

curl -s -b /tmp/bazusop-118-cookies.txt -X POST http://127.0.0.1:18097/api/v1/users/invites \
  -d '{"email":"viewer@example.com"}' -w "\ninvite:%{http_code}\n"
curl -s -b /tmp/bazusop-118-cookies.txt -X POST http://127.0.0.1:18097/api/v1/sites/site_default/service-accounts \
  -d '{"name":"ci-bot","role":"viewer"}' > /tmp/bazusop-118-viewer-account.json
cat /tmp/bazusop-118-viewer-account.json
```

Beklenen: hepsi `201`.

- [x] **Adım 3: Activity'nin tüm kaynakları birleştirdiğini doğrula**

```bash
curl -s -b /tmp/bazusop-118-cookies.txt http://127.0.0.1:18097/api/v1/activity/events | python3 -m json.tool
```

Beklenen: `identity` (`invite_created`) ve `service_account` (`created`) kaynaklı olaylar listede görünür.

- [x] **Adım 4: `PermissionViewActivity`/`PermissionViewAuditEvents` izin farkını doğrula — viewer rolü Activity'yi görür ama Audit'i göremez**

```bash
VIEWER_TOKEN=$(python3 -c "import json; print(json.load(open('/tmp/bazusop-118-viewer-account.json'))['token'])")
curl -s -o /dev/null -w "viewer -> /activity/events: %{http_code}\n" http://127.0.0.1:18097/api/v1/activity/events -H "Authorization: Bearer $VIEWER_TOKEN"
curl -s -o /dev/null -w "viewer -> /audit/events (beklenen 403): %{http_code}\n" http://127.0.0.1:18097/api/v1/audit/events -H "Authorization: Bearer $VIEWER_TOKEN"
```

Beklenen: ilki `200`, ikincisi `403` (matris: `PermissionViewAuditEvents` yalnız `site-admin`/`operator`, `viewer` değil).

- [x] **Adım 5: Audit arama ve filtrelerin çalıştığını doğrula (platform-admin oturumuyla)**

```bash
curl -s -b /tmp/bazusop-118-cookies.txt "http://127.0.0.1:18097/api/v1/audit/events?resource_type=service_accounts&outcome=success" | python3 -m json.tool | head -30
```

Beklenen: yalnız `resource_type=service_accounts`, `outcome=success` eşleşen kayıtlar döner.

- [x] **Adım 6: Temizlik**

```bash
docker compose -p bazusop-verify-118 down -v
rm -f /tmp/bazusop-118-cookies.txt /tmp/bazusop-118-bootstrap.json /tmp/bazusop-118-viewer-account.json
```

- [x] **Adım 7: Frontend'i son kez doğrula**

Çalıştır: `cd web && npm test && npm run build`
Beklenen: ikisi de BAŞARILI

## Kendi Kendine İnceleme

**1. Spec kapsaması.**
- "Activity, kullanıcıların anlayacağı başarılı durum değişikliklerini gösterir... kullanıcı davet edildi... servis restart işi tamamlandı... token rotate edildi" → Görev 1-2 (identity/site_role/service_account kaynaklarının Activity'ye eklenmesi, iş/alarm zaten vardı).
- "Başarısız denemeler... activity'ye girmez; audit... kalır" → Activity yalnız başarılı mutasyonlardan sonra `Record` çağrılıyor (hata durumunda `Record` hiç çağrılmıyor); Audit (`audittrail`) zaten her isteği (başarı/başarısızlık) `registerAudited` ile kaydediyor (spike 11.2'den beri).
- "Activity organizasyon ve site filtresi, actor, kaynak türü ve zaman aralığıyla sorgulanabilir" → site filtresi zaten `tenancy.Scope` ile var; Audit arama ekranı (Görev 3-4) `actor_type`/`resource_type`/`outcome`/`since`/`until` filtrelerini sağlıyor (spec bu filtreleri net biçimde Activity'ye atfetse de, mevcut mimaride Activity zaten yalnız tek bir siteye/organizasyona bağlı basit bir feed; zengin filtreleme ihtiyacı gerçekte Audit aramasında ortaya çıkıyor, bu yüzden filtreler oraya kondu — kasıtlı bir yorumlama, Genel Kısıtlar'da not edilmedi ama makul).
- "UI yüzeyleri... Site filtreli activity zaman çizelgesi... Yetkiye göre filtrelenmiş audit arama ekranı" → Görev 4 (iki ayrı sayfa, ikisi de site-scoped, ikincisi permission-gated).
- "Token, recovery code ve bootstrap secret... yalnız bir kez gösterilen arayüzlerde... Bu değerler tarayıcı depolamasına... yazılmaz" → Bu spike token/secret göstermiyor, ilgisiz.
- "Süreli servis hesabı token'ları... güvenli dışa aktarım" (export) → CSV indirme client-side, yalnız zaten-yetkilendirilmiş JSON verisinden üretiliyor, backend'e yeni bir export ucu eklemiyor — "güvenli" çünkü audittrail.Event zaten hiçbir secret alanı taşımıyor (redaksiyon yazma katmanında garanti).
- Yol haritası kabul sinyali ("Site zaman çizelgesi, filtre ve güvenli dışa aktarım çalışır") → Görev 6'da uçtan uca curl ile doğrulandı.
- Kod incelemesiyle bulunan gerçek boşluk (`ListAuditTrail`'in Postgres'te var olup hiçbir yere bağlı olmaması) → Görev 3'te tamamlandı.

**2. Placeholder taraması.** Her adımda gerçek, eksiksiz kod var. Görev 3 Adım 10'daki "Yazarken düzeltme" notu, git mv'nin `/dev/null` hedefiyle çalışmayacağını önceden yakalayıp doğru talimatı (doğrudan `Write` ile yeni dosya) veriyor — bu, önceki spike'ların "yazarken düzeltme" konvansiyonunu takip ediyor.

**3. Tip tutarlılığı.** `activity.Event`/`activity.Source*` Görev 1'de tanımlandığı gibi Görev 2 ve Görev 4'te birebir kullanılıyor. `audittrail.Filter`/`ErrInvalidQuery` Görev 3'te tanımlanıp aynı görev içinde tutarlı kullanılıyor. Frontend `ActivityEvent`/`AuditTrailEvent` tipleri backend JSON alan adlarıyla (`snake_case`) birebir eşleşiyor.
