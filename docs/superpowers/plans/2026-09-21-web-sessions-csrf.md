# Sunucu Taraflı Web Oturumları ve CSRF (Spike 11.4) Uygulama Planı

> **Otonom çalışanlar için:** ZORUNLU ALT-SKILL: Bu planı görev görev uygulamak için superpowers:subagent-driven-development (önerilen) veya superpowers:executing-plans kullan. Adımlar `- [x]` checkbox söz dizimiyle takip edilir.

**Amaç:** `internal/sessions` paketi, yerel kullanıcılar için opaque cookie tabanlı sunucu taraflı oturumlar (login/logout/whoami), boşta kalma (30dk) ve mutlak (8sa) süre aşımı, girişte rotasyon ve toplu iptal sağlar. Oturumun kendi mutasyon uçları (logout, admin toplu iptal hariç — o bearer token ile korunur) CSRF token doğrulaması ister. Ayrıca varsayılan kapalı, açıkça yapılandırılabilir bir CORS güvenilir-origin listesi eklenir.

**Mimari:** `internal/sessions` bu kod tabanındaki her domain paketiyle aynı `Store` arayüzü + `MemoryStore` + PostgreSQL `Store` deseni izler (bkz. `internal/identity`, `internal/jobs`). Oturum kimliği (`Session.ID`) audit'te güvenle loglanabilecek, sırrı içermeyen ayrı bir alandır; tarayıcıya verilen çerez değeri yalnız SHA-256 hash'i olarak saklanır — tıpkı davet token'ları ve recovery code'lar gibi. `internal/server/sessions.go` dört yeni rotayı (`POST /api/v1/sessions`, `GET /api/v1/session`, `DELETE /api/v1/sessions`, admin toplu iptal uçları) mevcut `registerAudited` sarmalayıcısı üzerinden açar; bu yüzden her oturum olayı otomatik olarak denetlenir. `internal/server/middleware.go`'daki `deriveActor`, oturum çerezinin SHA-256 özetini (DB'ye gitmeden, salt etiketleme amaçlı) `human` actor türü olarak işaretleyecek şekilde genişletilir. CORS, `NewHandler`'ın döndürdüğü `http.Handler`'ı saran ayrı, bağımsız bir middleware'dir (`internal/server/cors.go`) ve session'lardan habersizdir.

**Teknoloji yığını:** Go 1.26, yalnızca stdlib (`crypto/rand`, `crypto/sha256`, `crypto/subtle`, `encoding/hex`, `net/http` çerezleri) — yeni harici bağımlılık yok, bu kod tabanının asgari-bağımlılık felsefesiyle tutarlı.

**Spec:** [docs/superpowers/specs/2026-09-13-enterprise-rbac-identity-audit-design.md](../specs/2026-09-13-enterprise-rbac-identity-audit-design.md) — "Web oturumları" bölümü. Yol haritası spike 11.4'ü uygular.

## Genel Kısıtlar

- RBAC (spike 11.5) henüz yok. Spec'in "yetki seviyesi değiştiğinde rotate edilir" ve "rol/site üyeliği değiştiğinde ... oturumlar geçersiz olur" maddeleri, gerçek bir rol/site üyelik modeli olmadan HTTP'den tetiklenemez. Bunun yerine `Service.Rotate` doğrudan servis katmanında test edilen, yeniden kullanılabilir bir primitive olarak inşa edilir; 11.5 bunu rol/site değişikliğinde çağıracak. Bu spike'ta hiçbir HTTP handler `Rotate`'i çağırmaz — yalnızca girişte yeni bir oturum (`Create`) üretilir, ki bu da spec'in "girişte rotate" maddesini karşılar (önceden var olan anonim bir oturum kavramı olmadığından, "girişte rotasyon" ile "girişte taze session/token/CSRF üçlüsü üretmek" eş anlamlıdır).
- MFA sıfırlama endpoint'i henüz yok (spike 11.3'ün self-review'ünde de belirtildi). "MFA sıfırlandığında oturumlar geçersiz olur" maddesi bu yüzden bu spike'ta uygulanamaz — ileriki bir spike'a ertelenir.
- Kullanıcı devre dışı bırakma (disable) HTTP endpoint'i henüz yok — `identity.User.DisabledAt` alanı zaten var ama onu ayarlayan bir API yok (bu da RBAC/kullanıcı yönetimi kapsamına daha uygun, muhtemelen 11.5 veya sonrası). Buna rağmen `sessions.Service.Validate`, her doğrulamada kullanıcının aktif olup olmadığını enjekte edilen bir `UserActiveChecker` ile kontrol eder — böylece disable endpoint'i ileride eklendiğinde oturum iptali otomatik ve zaten test edilmiş olur.
- Admin toplu iptal uçları (`DELETE /api/v1/users/{userID}/sessions`, `DELETE /api/v1/sessions/all`), spike 11.2/11.3'te kurulan mevcut `BAZUSOP_ADMIN_TOKEN` bearer secret köprüsüyle korunur (`authorizeRole(..., roleAdmin)`) — RBAC gelene kadar geçici, bilinen bir köprü.
- CSRF token, oturum çerezinin (`bazusop_session`, HttpOnly) yanında JS tarafından okunabilir ikinci bir çerez (`bazusop_csrf`, HttpOnly=false) olarak sunulur ve login yanıt gövdesinde de döndürülür. Sunucu tarafında session kaydının kendi `CSRFToken` alanıyla `X-CSRF-Token` header'ı sabit-zamanlı karşılaştırılır. CSRF token, tek başına (oturum çerezi olmadan) hiçbir kaynağa erişim sağlamadığı için — davet token'ı veya recovery code'un aksine — veritabanında düz metin saklanır; hash'lenmesi ek güvenlik sağlamaz.
- CSRF doğrulaması yalnız **çerez tabanlı** mutasyonlar için gerekir: `DELETE /api/v1/sessions` (logout). `POST /api/v1/sessions` (login) henüz oturum yokken çalıştığı için CSRF'siz kalır — girişte rotasyonla (her login taze bir session/CSRF üretir) session fixation zaten engellenir. Admin toplu iptal uçları bearer token ile korunduğundan (tarayıcı bunu otomatik eklemez) CSRF kapsamı dışındadır.
- Oturum ID'si (audit'te loglanan `session_id`) ile tarayıcıya verilen çerez değeri (bearer sır) **farklı** değerlerdir: `Session.ID` sıra dışı rastgele ama gizli olmayan bir kimlik; çerez değeri ayrı, yalnız SHA-256 hash'i saklanan bir sırdır. Bu ayrım, audit olaylarına asla sır sızdırmamayı garanti eder (spec'in redaksiyon kuralı: "cookie ... hiçbir event veya log alanına yazılmaz").
- Her doğrulanan istekte `LastSeenAt` güncellenir (sliding idle window) — bu her başarılı `Validate` çağrısında bir DB yazımı demektir. Bilinçli bir ilk-dilim ödünleşimi; optimizasyon (örn. yalnız N saniyede bir touch) ileriki bir sertleştirme spike'ına (11.10) bırakılır.
- CORS varsayılan olarak **kapalı**: `BAZUSOP_TRUSTED_ORIGINS` boşsa hiçbir origin için `Access-Control-Allow-Origin` header'ı üretilmez. CORS, isteğin sunucuya ulaşmasını engellemez (tarayıcılar zaten böyle çalışmaz) — yalnız cross-origin JS'in yanıtı okuyabilmesini kontrol eder. Asıl CSRF koruması `X-CSRF-Token` doğrulamasıdır; CORS ikinci bir savunma katmanıdır.
- Bu spike hiçbir UI eklemez (spike 11.2 ve 11.3 ile aynı yaklaşım) — yalnız API + testler. Login/TOTP/recovery-code UI akışları ileriki bir UI spike'ına bırakılır.

## Dosya Yapısı

- `internal/sessions/sessions.go` — `Session`, `Store` arayüzü, sentinel hatalar, `Service` (`Create`/`Validate`/`Rotate`/`Revoke`/`RevokeAllForUser`/`RevokeAllForOrganization`), `UserActiveChecker`, `Option`/`NewService`.
- `internal/sessions/memorystore.go` — `MemoryStore`.
- `internal/sessions/sessions_test.go` — servis birim testleri.
- `internal/identity/identity.go` — değişiklik: `UserByID` (public) ve `IsUserActive` eklenir.
- `internal/identity/identity_test.go` — değişiklik: iki yeni test.
- `internal/storage/postgres/migrations/020_sessions.sql` — `sessions` tablosu.
- `internal/storage/postgres/sessions.go` — `Store` implementasyonu.
- `internal/storage/postgres/sessions_integration_test.go`
- `internal/storage/postgres/store_test.go` — değişiklik: migration sayısı 19→20.
- `internal/server/cors.go` — CORS middleware + güvenilir origin ayrıştırma.
- `internal/server/cors_test.go`
- `internal/server/sessions.go` — `WithSessions` option, çerez yardımcıları, `handleLogin`/`handleWhoAmI`/`handleLogout`/`handleRevokeUserSessions`/`handleRevokeAllSessions`.
- `internal/server/sessions_test.go` — HTTP kabul testleri.
- `internal/server/middleware.go` — değişiklik: `deriveActor` oturum çerezini `human` olarak etiketler.
- `internal/server/server.go` — değişiklik: `handlerOptions`/`NewHandler` bağlantısı, CORS sarmalama.
- `internal/audittrail/audittrail.go` — değişiklik: `ActorHuman` sabiti eklenir.
- `internal/config/config.go` — değişiklik: `TrustedOrigins []string` alanı + ayrıştırma.
- `internal/config/config_test.go` — değişiklik: yeni test.
- `cmd/bazusop-hub/main.go` — değişiklik: `sessionService` inşası + `WithSessions`/`WithTrustedOrigins` bağlantısı.
- `compose.yaml` — değişiklik: `BAZUSOP_TRUSTED_ORIGINS`'i hub konteynerine geçir.

## Görev 1: `internal/sessions` çekirdek paketi

**Dosyalar:**
- Oluştur: `internal/sessions/sessions.go`
- Oluştur: `internal/sessions/memorystore.go`
- Oluştur: `internal/sessions/sessions_test.go`

**Arayüzler:**
- Üretir: `type Session struct{ID, UserID, OrganizationID, CSRFToken string; CreatedAt, LastSeenAt, AbsoluteExpiresAt time.Time; RevokedAt *time.Time}`; `type UserActiveChecker func(context.Context, string) (bool, error)`; sentinel hatalar `ErrSessionNotFound`, `ErrSessionRevoked`, `ErrSessionExpired`, `ErrSessionIdle`, `ErrUserInactive`; `type Store interface` (aşağıda); `type Service struct` ile `func NewService(store Store, userActive UserActiveChecker, options ...Option) (*Service, error)` ve metodlar `Create`, `Validate`, `Rotate`, `Revoke`, `RevokeAllForUser`, `RevokeAllForOrganization`; `type Option func(*Service)`; `WithClock(func() time.Time) Option`; `WithIdleTimeout(time.Duration) Option`; `WithAbsoluteTimeout(time.Duration) Option`; `func NewMemoryStore() *MemoryStore`.

- [x] **Adım 1: Başarısız testi yaz**

```go
// internal/sessions/sessions_test.go
package sessions_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gokayybaz/bazusop/internal/sessions"
)

func alwaysActive(context.Context, string) (bool, error) { return true, nil }

func newTestService(t *testing.T, now func() time.Time, options ...sessions.Option) *sessions.Service {
	t.Helper()
	allOptions := append([]sessions.Option{sessions.WithClock(now)}, options...)
	service, err := sessions.NewService(sessions.NewMemoryStore(), alwaysActive, allOptions...)
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func TestCreateProducesAValidatableSession(t *testing.T) {
	t.Parallel()
	service := newTestService(t, time.Now)
	session, token, csrfToken, err := service.Create(t.Context(), "user-1", "org_default")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if session.UserID != "user-1" || session.OrganizationID != "org_default" || token == "" || csrfToken == "" {
		t.Fatalf("unexpected session: %#v (token=%q csrf=%q)", session, token, csrfToken)
	}
	if session.CSRFToken != csrfToken {
		t.Fatalf("expected the returned CSRF token to match the session's stored CSRF token")
	}

	validated, err := service.Validate(t.Context(), token)
	if err != nil || validated.ID != session.ID {
		t.Fatalf("expected the fresh token to validate, got %#v %v", validated, err)
	}
}

func TestValidateRejectsAnUnknownToken(t *testing.T) {
	t.Parallel()
	service := newTestService(t, time.Now)
	if _, err := service.Validate(t.Context(), "does-not-exist"); !errors.Is(err, sessions.ErrSessionNotFound) {
		t.Fatalf("expected ErrSessionNotFound, got %v", err)
	}
}

func TestValidateRejectsARevokedSession(t *testing.T) {
	t.Parallel()
	service := newTestService(t, time.Now)
	_, token, _, err := service.Create(t.Context(), "user-1", "org_default")
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Revoke(t.Context(), token); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := service.Validate(t.Context(), token); !errors.Is(err, sessions.ErrSessionRevoked) {
		t.Fatalf("expected ErrSessionRevoked, got %v", err)
	}
}

func TestValidateRejectsAfterAbsoluteTimeout(t *testing.T) {
	t.Parallel()
	current := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := func() time.Time { return current }
	service := newTestService(t, clock, sessions.WithAbsoluteTimeout(time.Hour))
	_, token, _, err := service.Create(t.Context(), "user-1", "org_default")
	if err != nil {
		t.Fatal(err)
	}
	current = current.Add(61 * time.Minute)
	if _, err := service.Validate(t.Context(), token); !errors.Is(err, sessions.ErrSessionExpired) {
		t.Fatalf("expected ErrSessionExpired, got %v", err)
	}
}

func TestValidateRejectsAfterIdleTimeoutAndSlidesTheWindowForwardOtherwise(t *testing.T) {
	t.Parallel()
	current := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := func() time.Time { return current }
	service := newTestService(t, clock, sessions.WithIdleTimeout(10*time.Minute), sessions.WithAbsoluteTimeout(8*time.Hour))
	_, token, _, err := service.Create(t.Context(), "user-1", "org_default")
	if err != nil {
		t.Fatal(err)
	}

	current = current.Add(5 * time.Minute)
	if _, err := service.Validate(t.Context(), token); err != nil {
		t.Fatalf("expected a well-within-idle-window validation to succeed: %v", err)
	}

	// The prior Validate call above slid LastSeenAt forward to `current`, so
	// another 5 minutes here is still within the 10-minute idle window
	// measured from the *slid* timestamp, proving the window moved.
	current = current.Add(5 * time.Minute)
	if _, err := service.Validate(t.Context(), token); err != nil {
		t.Fatalf("expected the idle window to have slid forward: %v", err)
	}

	current = current.Add(11 * time.Minute)
	if _, err := service.Validate(t.Context(), token); !errors.Is(err, sessions.ErrSessionIdle) {
		t.Fatalf("expected ErrSessionIdle, got %v", err)
	}
}

func TestValidateRejectsWhenTheUserIsNoLongerActive(t *testing.T) {
	t.Parallel()
	inactive := func(context.Context, string) (bool, error) { return false, nil }
	service, err := sessions.NewService(sessions.NewMemoryStore(), inactive)
	if err != nil {
		t.Fatal(err)
	}
	_, token, _, err := service.Create(t.Context(), "user-1", "org_default")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Validate(t.Context(), token); !errors.Is(err, sessions.ErrUserInactive) {
		t.Fatalf("expected ErrUserInactive, got %v", err)
	}
}

func TestRotateIssuesAFreshSessionAndRevokesTheOld(t *testing.T) {
	t.Parallel()
	service := newTestService(t, time.Now)
	session, token, csrfToken, err := service.Create(t.Context(), "user-1", "org_default")
	if err != nil {
		t.Fatal(err)
	}
	newSession, newToken, newCSRFToken, err := service.Rotate(t.Context(), token)
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if newSession.ID == session.ID || newToken == token || newCSRFToken == csrfToken {
		t.Fatalf("expected rotation to produce a fresh session id, token, and CSRF token")
	}
	if newSession.UserID != session.UserID || newSession.OrganizationID != session.OrganizationID {
		t.Fatalf("expected rotation to preserve the same user and organization")
	}
	if _, err := service.Validate(t.Context(), token); !errors.Is(err, sessions.ErrSessionRevoked) {
		t.Fatalf("expected the old token to be revoked after rotation, got %v", err)
	}
	if _, err := service.Validate(t.Context(), newToken); err != nil {
		t.Fatalf("expected the new token to validate: %v", err)
	}
}

func TestRevokeAllForUserInvalidatesEveryOneOfTheirSessionsButNotOthers(t *testing.T) {
	t.Parallel()
	service := newTestService(t, time.Now)
	_, tokenA1, _, err := service.Create(t.Context(), "user-a", "org_default")
	if err != nil {
		t.Fatal(err)
	}
	_, tokenA2, _, err := service.Create(t.Context(), "user-a", "org_default")
	if err != nil {
		t.Fatal(err)
	}
	_, tokenB, _, err := service.Create(t.Context(), "user-b", "org_default")
	if err != nil {
		t.Fatal(err)
	}
	if err := service.RevokeAllForUser(t.Context(), "user-a"); err != nil {
		t.Fatalf("revoke all for user: %v", err)
	}
	if _, err := service.Validate(t.Context(), tokenA1); !errors.Is(err, sessions.ErrSessionRevoked) {
		t.Fatalf("expected user-a's first session revoked, got %v", err)
	}
	if _, err := service.Validate(t.Context(), tokenA2); !errors.Is(err, sessions.ErrSessionRevoked) {
		t.Fatalf("expected user-a's second session revoked, got %v", err)
	}
	if _, err := service.Validate(t.Context(), tokenB); err != nil {
		t.Fatalf("expected user-b's session to remain valid, got %v", err)
	}
}

func TestRevokeAllForOrganizationInvalidatesEverySessionInThatOrganization(t *testing.T) {
	t.Parallel()
	service := newTestService(t, time.Now)
	_, tokenA, _, err := service.Create(t.Context(), "user-a", "org_default")
	if err != nil {
		t.Fatal(err)
	}
	_, tokenB, _, err := service.Create(t.Context(), "user-b", "org_default")
	if err != nil {
		t.Fatal(err)
	}
	_, tokenOther, _, err := service.Create(t.Context(), "user-c", "org_other")
	if err != nil {
		t.Fatal(err)
	}
	if err := service.RevokeAllForOrganization(t.Context(), "org_default"); err != nil {
		t.Fatalf("revoke all for organization: %v", err)
	}
	if _, err := service.Validate(t.Context(), tokenA); !errors.Is(err, sessions.ErrSessionRevoked) {
		t.Fatalf("expected org_default session A revoked, got %v", err)
	}
	if _, err := service.Validate(t.Context(), tokenB); !errors.Is(err, sessions.ErrSessionRevoked) {
		t.Fatalf("expected org_default session B revoked, got %v", err)
	}
	if _, err := service.Validate(t.Context(), tokenOther); err != nil {
		t.Fatalf("expected the other organization's session to remain valid, got %v", err)
	}
}

func TestNewServiceRequiresAUserActiveChecker(t *testing.T) {
	t.Parallel()
	if _, err := sessions.NewService(sessions.NewMemoryStore(), nil); err == nil {
		t.Fatal("expected NewService to reject a nil UserActiveChecker")
	}
}
```

- [x] **Adım 2: Testin başarısız olduğunu doğrula**

Çalıştır: `GOCACHE=/tmp/bazusop-go-cache go test ./internal/sessions/... -v`
Beklenen: BAŞARISIZ — `package sessions: no non-test Go files`

- [x] **Adım 3: `sessions.go`'yu yaz**

```go
// internal/sessions/sessions.go
package sessions

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

type Session struct {
	ID                string
	UserID            string
	OrganizationID    string
	CSRFToken         string
	CreatedAt         time.Time
	LastSeenAt        time.Time
	AbsoluteExpiresAt time.Time
	RevokedAt         *time.Time
}

var (
	ErrSessionNotFound = errors.New("session not found")
	ErrSessionRevoked  = errors.New("session revoked")
	ErrSessionExpired  = errors.New("session expired")
	ErrSessionIdle     = errors.New("session idle timeout")
	ErrUserInactive    = errors.New("user is disabled")
)

// UserActiveChecker reports whether userID may still hold a valid session —
// false once a user is disabled. internal/identity provides the production
// implementation (Service.IsUserActive); this package stays decoupled from
// identity's concrete types, matching the jobs.HostScopeChecker convention
// already used elsewhere in this codebase.
type UserActiveChecker func(context.Context, string) (bool, error)

type Store interface {
	CreateSession(ctx context.Context, session Session, tokenHash string) error
	SessionByTokenHash(ctx context.Context, tokenHash string) (Session, error)
	Touch(ctx context.Context, sessionID string, lastSeenAt time.Time) error
	RevokeSession(ctx context.Context, sessionID string, at time.Time) error
	RevokeAllForUser(ctx context.Context, userID string, at time.Time) error
	RevokeAllForOrganization(ctx context.Context, organizationID string, at time.Time) error
}

type Service struct {
	store           Store
	userActive      UserActiveChecker
	now             func() time.Time
	idleTimeout     time.Duration
	absoluteTimeout time.Duration
}

type Option func(*Service)

func WithClock(clock func() time.Time) Option { return func(service *Service) { service.now = clock } }
func WithIdleTimeout(duration time.Duration) Option {
	return func(service *Service) { service.idleTimeout = duration }
}
func WithAbsoluteTimeout(duration time.Duration) Option {
	return func(service *Service) { service.absoluteTimeout = duration }
}

// NewService requires a non-nil userActive so that disabling a user always
// has a real, already-tested path to invalidating their sessions, even
// before any HTTP endpoint sets DisabledAt.
func NewService(store Store, userActive UserActiveChecker, options ...Option) (*Service, error) {
	service := &Service{
		store: store, userActive: userActive, now: func() time.Time { return time.Now().UTC() },
		idleTimeout: 30 * time.Minute, absoluteTimeout: 8 * time.Hour,
	}
	for _, option := range options {
		option(service)
	}
	if service.userActive == nil {
		return nil, errors.New("session user-active checker is required")
	}
	return service, nil
}

func (service *Service) Create(ctx context.Context, userID, organizationID string) (Session, string, string, error) {
	id, err := newOpaqueID()
	if err != nil {
		return Session{}, "", "", err
	}
	token, err := newOpaqueID()
	if err != nil {
		return Session{}, "", "", err
	}
	csrfToken, err := newOpaqueID()
	if err != nil {
		return Session{}, "", "", err
	}
	now := service.now()
	session := Session{
		ID: id, UserID: userID, OrganizationID: organizationID, CSRFToken: csrfToken,
		CreatedAt: now, LastSeenAt: now, AbsoluteExpiresAt: now.Add(service.absoluteTimeout),
	}
	if err := service.store.CreateSession(ctx, session, hashToken(token)); err != nil {
		return Session{}, "", "", err
	}
	return session, token, csrfToken, nil
}

func (service *Service) Validate(ctx context.Context, token string) (Session, error) {
	session, err := service.store.SessionByTokenHash(ctx, hashToken(token))
	if err != nil {
		return Session{}, err
	}
	if session.RevokedAt != nil {
		return Session{}, ErrSessionRevoked
	}
	now := service.now()
	if now.After(session.AbsoluteExpiresAt) {
		return Session{}, ErrSessionExpired
	}
	if now.Sub(session.LastSeenAt) > service.idleTimeout {
		return Session{}, ErrSessionIdle
	}
	active, err := service.userActive(ctx, session.UserID)
	if err != nil {
		return Session{}, err
	}
	if !active {
		return Session{}, ErrUserInactive
	}
	if err := service.store.Touch(ctx, session.ID, now); err != nil {
		return Session{}, err
	}
	session.LastSeenAt = now
	return session, nil
}

// Rotate validates token, then issues a brand-new session for the same
// user/organization and revokes the old one. Spike 11.5 will call this
// whenever a user's role or site membership changes; this spike calls it
// from no HTTP handler yet, but it is fully exercised at the service layer.
func (service *Service) Rotate(ctx context.Context, token string) (Session, string, string, error) {
	session, err := service.Validate(ctx, token)
	if err != nil {
		return Session{}, "", "", err
	}
	newSession, newToken, newCSRFToken, err := service.Create(ctx, session.UserID, session.OrganizationID)
	if err != nil {
		return Session{}, "", "", err
	}
	if err := service.store.RevokeSession(ctx, session.ID, service.now()); err != nil {
		return Session{}, "", "", err
	}
	return newSession, newToken, newCSRFToken, nil
}

func (service *Service) Revoke(ctx context.Context, token string) error {
	session, err := service.store.SessionByTokenHash(ctx, hashToken(token))
	if err != nil {
		return err
	}
	return service.store.RevokeSession(ctx, session.ID, service.now())
}

func (service *Service) RevokeAllForUser(ctx context.Context, userID string) error {
	return service.store.RevokeAllForUser(ctx, userID, service.now())
}

func (service *Service) RevokeAllForOrganization(ctx context.Context, organizationID string) error {
	return service.store.RevokeAllForOrganization(ctx, organizationID, service.now())
}

func newOpaqueID() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate session id: %w", err)
	}
	return hex.EncodeToString(value), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

var _ = subtle.ConstantTimeCompare // used by internal/server's CSRF check, kept imported here for future in-package use if needed
```

**Yazarken düzeltme:** Son satırdaki `var _ = subtle.ConstantTimeCompare` satırı gereksiz bir placeholder'dır — bu paket `subtle`'ı hiçbir yerde kullanmıyor (CSRF karşılaştırması `internal/server` katmanında yapılıyor). Bu satırı ve `"crypto/subtle"` import'unu **tamamen sil**; aksi halde `go vet` "unused variable pattern"a benzer bir kod kokusu bırakır ve derlenir ama anlamsızdır. Gerçek dosyada import listesi yalnız `context`, `crypto/rand`, `crypto/sha256`, `encoding/hex`, `errors`, `fmt`, `time` olmalıdır.

- [x] **Adım 4: `MemoryStore`'u yaz**

```go
// internal/sessions/memorystore.go
package sessions

import (
	"context"
	"sync"
	"time"
)

type sessionRecord struct {
	session   Session
	tokenHash string
}

// MemoryStore is for local development and tests only, matching every
// other domain's memory-mode fallback.
type MemoryStore struct {
	mu             sync.Mutex
	byTokenHash    map[string]sessionRecord
	byID           map[string]string // session id -> token hash
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		byTokenHash: make(map[string]sessionRecord),
		byID:        make(map[string]string),
	}
}

func (store *MemoryStore) CreateSession(_ context.Context, session Session, tokenHash string) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.byTokenHash[tokenHash] = sessionRecord{session: session, tokenHash: tokenHash}
	store.byID[session.ID] = tokenHash
	return nil
}

func (store *MemoryStore) SessionByTokenHash(_ context.Context, tokenHash string) (Session, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	record, ok := store.byTokenHash[tokenHash]
	if !ok {
		return Session{}, ErrSessionNotFound
	}
	return record.session, nil
}

func (store *MemoryStore) Touch(_ context.Context, sessionID string, lastSeenAt time.Time) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	tokenHash, ok := store.byID[sessionID]
	if !ok {
		return ErrSessionNotFound
	}
	record := store.byTokenHash[tokenHash]
	record.session.LastSeenAt = lastSeenAt
	store.byTokenHash[tokenHash] = record
	return nil
}

func (store *MemoryStore) RevokeSession(_ context.Context, sessionID string, at time.Time) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	tokenHash, ok := store.byID[sessionID]
	if !ok {
		return ErrSessionNotFound
	}
	record := store.byTokenHash[tokenHash]
	revokedAt := at
	record.session.RevokedAt = &revokedAt
	store.byTokenHash[tokenHash] = record
	return nil
}

func (store *MemoryStore) RevokeAllForUser(_ context.Context, userID string, at time.Time) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	revokedAt := at
	for tokenHash, record := range store.byTokenHash {
		if record.session.UserID == userID && record.session.RevokedAt == nil {
			record.session.RevokedAt = &revokedAt
			store.byTokenHash[tokenHash] = record
		}
	}
	return nil
}

func (store *MemoryStore) RevokeAllForOrganization(_ context.Context, organizationID string, at time.Time) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	revokedAt := at
	for tokenHash, record := range store.byTokenHash {
		if record.session.OrganizationID == organizationID && record.session.RevokedAt == nil {
			record.session.RevokedAt = &revokedAt
			store.byTokenHash[tokenHash] = record
		}
	}
	return nil
}
```

- [x] **Adım 5: Testleri çalıştırıp geçtiğini doğrula**

Çalıştır: `GOCACHE=/tmp/bazusop-go-cache go test ./internal/sessions/... -v`
Beklenen: BAŞARILI (tüm testler)

- [x] **Adım 6: `go vet` ve `gofmt` çalıştır**

Çalıştır: `go vet ./internal/sessions/... && gofmt -l internal/sessions/*.go`
Beklenen: her iki komuttan da çıktı yok

- [x] **Adım 7: Commit**

```bash
git add internal/sessions/sessions.go internal/sessions/memorystore.go internal/sessions/sessions_test.go
git commit -m "feat: add web session lifecycle (create, validate, rotate, revoke)"
```

## Görev 2: `internal/identity`'ye `UserByID` ve `IsUserActive` ekle

**Dosyalar:**
- Değiştir: `internal/identity/identity.go`
- Değiştir: `internal/identity/identity_test.go`

**Arayüzler:**
- Üretir: `func (service *Service) UserByID(ctx context.Context, id string) (User, error)`; `func (service *Service) IsUserActive(ctx context.Context, id string) (bool, error)` — `sessions.UserActiveChecker` imzasına tam uyar, `main.go`'da doğrudan geçirilebilir.

- [x] **Adım 1: Başarısız testleri yaz**

`internal/identity/identity_test.go`'nun sonuna ekle:

```go
func TestUserByIDReturnsTheStoredUser(t *testing.T) {
	t.Parallel()
	service := newTestService()
	created, _, err := service.Bootstrap(t.Context(), "org_default", "admin@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	fetched, err := service.UserByID(t.Context(), created.ID)
	if err != nil || fetched.Email != "admin@example.com" {
		t.Fatalf("expected to read back the bootstrapped user, got %#v %v", fetched, err)
	}
}

func TestIsUserActiveReflectsTheStoredDisabledAtField(t *testing.T) {
	t.Parallel()
	store := identity.NewMemoryStore()
	service := identity.NewService(store, "test-totp-encryption-key")
	user, _, err := service.Bootstrap(t.Context(), "org_default", "admin@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	active, err := service.IsUserActive(t.Context(), user.ID)
	if err != nil || !active {
		t.Fatalf("expected a freshly bootstrapped user to be active, got %v %v", active, err)
	}

	disabledAt := time.Now().UTC()
	user.DisabledAt = &disabledAt
	if err := store.UpdateUser(t.Context(), user); err != nil {
		t.Fatal(err)
	}
	active, err = service.IsUserActive(t.Context(), user.ID)
	if err != nil || active {
		t.Fatalf("expected a disabled user to be inactive, got %v %v", active, err)
	}
}

func TestIsUserActiveReturnsFalseForAnUnknownUserWithoutError(t *testing.T) {
	t.Parallel()
	service := newTestService()
	active, err := service.IsUserActive(t.Context(), "does-not-exist")
	if err != nil {
		t.Fatalf("expected no error for an unknown user, got %v", err)
	}
	if active {
		t.Fatal("expected an unknown user to be reported inactive")
	}
}
```

- [x] **Adım 2: Testin başarısız olduğunu doğrula**

Çalıştır: `GOCACHE=/tmp/bazusop-go-cache go test ./internal/identity/... -run 'TestUserByID|TestIsUserActive' -v`
Beklenen: BAŞARISIZ — `undefined: identity.Service.UserByID` (ve `IsUserActive`)

- [x] **Adım 3: Uygula**

`internal/identity/identity.go`'da, `VerifyCredentialsWithRecoveryCode` metodundan hemen sonra ekle:

```go
func (service *Service) UserByID(ctx context.Context, id string) (User, error) {
	return service.store.UserByID(ctx, id)
}

// IsUserActive reports whether id exists and is not disabled. An unknown
// user is reported inactive (not an error) — this lets sessions.Service
// treat "user vanished" and "user disabled" identically: no valid session.
func (service *Service) IsUserActive(ctx context.Context, id string) (bool, error) {
	user, err := service.store.UserByID(ctx, id)
	if errors.Is(err, ErrInvalidCredentials) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return user.DisabledAt == nil, nil
}
```

- [x] **Adım 4: Testleri çalıştırıp geçtiğini doğrula**

Çalıştır: `GOCACHE=/tmp/bazusop-go-cache go test ./internal/identity/... -v`
Beklenen: BAŞARILI (tüm testler)

- [x] **Adım 5: `go vet` ve `gofmt` çalıştır**

Çalıştır: `go vet ./internal/identity/... && gofmt -l internal/identity/*.go`
Beklenen: çıktı yok

- [x] **Adım 6: Commit**

```bash
git add internal/identity/identity.go internal/identity/identity_test.go
git commit -m "feat: expose UserByID and IsUserActive from internal/identity"
```

## Görev 3: Oturumlar için PostgreSQL store

**Dosyalar:**
- Oluştur: `internal/storage/postgres/migrations/020_sessions.sql`
- Oluştur: `internal/storage/postgres/sessions.go`
- Oluştur: `internal/storage/postgres/sessions_integration_test.go`
- Değiştir: `internal/storage/postgres/store_test.go`

**Arayüzler:**
- Tüketir: `sessions.Session`, `sessions.Store` (Görev 1).
- Üretir: `sessions.Store` arayüzünün altı metodunu karşılayan `*Store` metodları.

- [x] **Adım 1: Başarısız entegrasyon testini yaz**

```go
// internal/storage/postgres/sessions_integration_test.go
package postgres

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/gokayybaz/bazusop/internal/sessions"
)

func TestPostgresSessionLifecycle(t *testing.T) {
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
	orgID := "session-org-" + suffix
	userID := "session-user-" + suffix
	if _, err := store.pool.Exec(ctx, `INSERT INTO organizations (id, name) VALUES ($1, 'Session test')`, orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.pool.Exec(ctx, `INSERT INTO users (id, organization_id, email, role, password_hash, created_at) VALUES ($1,$2,'session-test@example.com','platform-admin','hash', now())`, userID, orgID); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, err := store.pool.Exec(cleanupCtx, "DELETE FROM sessions WHERE organization_id=$1", orgID); err != nil {
			t.Errorf("cleanup sessions: %v", err)
		}
		if _, err := store.pool.Exec(cleanupCtx, "DELETE FROM users WHERE id=$1", userID); err != nil {
			t.Errorf("cleanup users: %v", err)
		}
		if _, err := store.pool.Exec(cleanupCtx, "DELETE FROM organizations WHERE id=$1", orgID); err != nil {
			t.Errorf("cleanup organizations: %v", err)
		}
	}()

	now := time.Now().UTC()
	session := sessions.Session{
		ID: "session-" + suffix, UserID: userID, OrganizationID: orgID, CSRFToken: "csrf-" + suffix,
		CreatedAt: now, LastSeenAt: now, AbsoluteExpiresAt: now.Add(8 * time.Hour),
	}
	tokenHash := "token-hash-" + suffix
	if err := store.CreateSession(ctx, session, tokenHash); err != nil {
		t.Fatalf("create session: %v", err)
	}

	fetched, err := store.SessionByTokenHash(ctx, tokenHash)
	if err != nil || fetched.ID != session.ID || fetched.CSRFToken != session.CSRFToken {
		t.Fatalf("expected to read back the session, got %#v %v", fetched, err)
	}

	touchedAt := now.Add(5 * time.Minute)
	if err := store.Touch(ctx, session.ID, touchedAt); err != nil {
		t.Fatalf("touch: %v", err)
	}
	afterTouch, err := store.SessionByTokenHash(ctx, tokenHash)
	if err != nil || !afterTouch.LastSeenAt.Equal(touchedAt) {
		t.Fatalf("expected LastSeenAt to be updated, got %#v %v", afterTouch, err)
	}

	if err := store.RevokeSession(ctx, session.ID, now.Add(10*time.Minute)); err != nil {
		t.Fatalf("revoke session: %v", err)
	}
	revoked, err := store.SessionByTokenHash(ctx, tokenHash)
	if err != nil || revoked.RevokedAt == nil {
		t.Fatalf("expected the session to be marked revoked, got %#v %v", revoked, err)
	}

	secondTokenHash := "second-token-hash-" + suffix
	secondSession := session
	secondSession.ID = "second-session-" + suffix
	if err := store.CreateSession(ctx, secondSession, secondTokenHash); err != nil {
		t.Fatalf("create second session: %v", err)
	}
	if err := store.RevokeAllForUser(ctx, userID, now.Add(15*time.Minute)); err != nil {
		t.Fatalf("revoke all for user: %v", err)
	}
	secondRevoked, err := store.SessionByTokenHash(ctx, secondTokenHash)
	if err != nil || secondRevoked.RevokedAt == nil {
		t.Fatalf("expected the second session to be revoked too, got %#v %v", secondRevoked, err)
	}

	thirdTokenHash := "third-token-hash-" + suffix
	thirdSession := session
	thirdSession.ID = "third-session-" + suffix
	if err := store.CreateSession(ctx, thirdSession, thirdTokenHash); err != nil {
		t.Fatalf("create third session: %v", err)
	}
	if err := store.RevokeAllForOrganization(ctx, orgID, now.Add(20*time.Minute)); err != nil {
		t.Fatalf("revoke all for organization: %v", err)
	}
	thirdRevoked, err := store.SessionByTokenHash(ctx, thirdTokenHash)
	if err != nil || thirdRevoked.RevokedAt == nil {
		t.Fatalf("expected the third session to be revoked too, got %#v %v", thirdRevoked, err)
	}
}
```

- [x] **Adım 2: Testin başarısız olduğunu doğrula**

Çalıştır: `BAZUSOP_TEST_DATABASE_URL='postgres://bazusop:bazusop_test@127.0.0.1:5432/bazusop_test?sslmode=disable' GOCACHE=/tmp/bazusop-go-cache go test ./internal/storage/postgres/... -run TestPostgresSessionLifecycle -v`

(Yoksa önce atılabilir bir PostgreSQL 18 konteyneri başlat — bu paketteki her entegrasyon testiyle aynı desen.)

Beklenen: BAŞARISIZ — `Store` metodları henüz yok.

- [x] **Adım 3: Migration'ı yaz**

```sql
-- internal/storage/postgres/migrations/020_sessions.sql
CREATE TABLE IF NOT EXISTS sessions (
    id TEXT PRIMARY KEY,
    token_hash TEXT NOT NULL UNIQUE,
    user_id TEXT NOT NULL,
    organization_id TEXT NOT NULL,
    csrf_token TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at TIMESTAMPTZ NOT NULL,
    absolute_expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    CONSTRAINT sessions_user_fk FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    CONSTRAINT sessions_org_fk FOREIGN KEY (organization_id) REFERENCES organizations(id) ON DELETE RESTRICT
);

CREATE INDEX IF NOT EXISTS sessions_user_idx ON sessions (user_id);
CREATE INDEX IF NOT EXISTS sessions_org_idx ON sessions (organization_id);
```

- [x] **Adım 4: `Store` implementasyonunu yaz**

```go
// internal/storage/postgres/sessions.go
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/gokayybaz/bazusop/internal/sessions"
)

func (store *Store) CreateSession(ctx context.Context, session sessions.Session, tokenHash string) error {
	_, err := store.pool.Exec(ctx, `
		INSERT INTO sessions (id, token_hash, user_id, organization_id, csrf_token, created_at, last_seen_at, absolute_expires_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		session.ID, tokenHash, session.UserID, session.OrganizationID, session.CSRFToken,
		session.CreatedAt, session.LastSeenAt, session.AbsoluteExpiresAt)
	if err != nil {
		return fmt.Errorf("insert session: %w", err)
	}
	return nil
}

func (store *Store) SessionByTokenHash(ctx context.Context, tokenHash string) (sessions.Session, error) {
	var session sessions.Session
	err := store.pool.QueryRow(ctx, `
		SELECT id, user_id, organization_id, csrf_token, created_at, last_seen_at, absolute_expires_at, revoked_at
		FROM sessions WHERE token_hash=$1`, tokenHash).Scan(
		&session.ID, &session.UserID, &session.OrganizationID, &session.CSRFToken,
		&session.CreatedAt, &session.LastSeenAt, &session.AbsoluteExpiresAt, &session.RevokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return sessions.Session{}, sessions.ErrSessionNotFound
	}
	if err != nil {
		return sessions.Session{}, fmt.Errorf("query session: %w", err)
	}
	return session, nil
}

func (store *Store) Touch(ctx context.Context, sessionID string, lastSeenAt time.Time) error {
	_, err := store.pool.Exec(ctx, `UPDATE sessions SET last_seen_at=$2 WHERE id=$1`, sessionID, lastSeenAt)
	if err != nil {
		return fmt.Errorf("touch session: %w", err)
	}
	return nil
}

func (store *Store) RevokeSession(ctx context.Context, sessionID string, at time.Time) error {
	_, err := store.pool.Exec(ctx, `UPDATE sessions SET revoked_at=$2 WHERE id=$1 AND revoked_at IS NULL`, sessionID, at)
	if err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	return nil
}

func (store *Store) RevokeAllForUser(ctx context.Context, userID string, at time.Time) error {
	_, err := store.pool.Exec(ctx, `UPDATE sessions SET revoked_at=$2 WHERE user_id=$1 AND revoked_at IS NULL`, userID, at)
	if err != nil {
		return fmt.Errorf("revoke sessions for user: %w", err)
	}
	return nil
}

func (store *Store) RevokeAllForOrganization(ctx context.Context, organizationID string, at time.Time) error {
	_, err := store.pool.Exec(ctx, `UPDATE sessions SET revoked_at=$2 WHERE organization_id=$1 AND revoked_at IS NULL`, organizationID, at)
	if err != nil {
		return fmt.Errorf("revoke sessions for organization: %w", err)
	}
	return nil
}
```

- [x] **Adım 5: Entegrasyon testini çalıştırıp geçtiğini doğrula**

Çalıştır: `BAZUSOP_TEST_DATABASE_URL='postgres://bazusop:bazusop_test@127.0.0.1:5432/bazusop_test?sslmode=disable' GOCACHE=/tmp/bazusop-go-cache go test ./internal/storage/postgres/... -run TestPostgresSessionLifecycle -v`
Beklenen: BAŞARILI

- [x] **Adım 6: Migration sayısı testini güncelle**

`internal/storage/postgres/store_test.go`'da `TestStorageMigrationsAreEmbeddedInOrder`:

```go
	if len(entries) != 20 {
		t.Fatalf("expected twenty storage migrations, got %d", len(entries))
	}
	if entries[0].Name() != "001_hosts.sql" || entries[19].Name() != "020_sessions.sql" {
		t.Fatalf("unexpected migration range: %s through %s", entries[0].Name(), entries[len(entries)-1].Name())
	}
```

- [x] **Adım 7: Tam test paketini, vet ve gofmt'ı çalıştır**

Çalıştır: `go vet ./... && gofmt -l internal/storage/postgres/*.go internal/sessions/*.go internal/identity/*.go && BAZUSOP_TEST_DATABASE_URL='postgres://bazusop:bazusop_test@127.0.0.1:5432/bazusop_test?sslmode=disable' GOCACHE=/tmp/bazusop-go-cache go test ./...`
Beklenen: vet/gofmt çıktısı yok; her paket `ok`

- [x] **Adım 8: Commit**

```bash
git add internal/storage/postgres/migrations/020_sessions.sql internal/storage/postgres/sessions.go internal/storage/postgres/sessions_integration_test.go internal/storage/postgres/store_test.go
git commit -m "feat: add PostgreSQL store for web sessions"
```

## Görev 4: CORS middleware ve güvenilir origin yapılandırması

**Dosyalar:**
- Oluştur: `internal/server/cors.go`
- Oluştur: `internal/server/cors_test.go`
- Değiştir: `internal/config/config.go`
- Değiştir: `internal/config/config_test.go`

**Arayüzler:**
- Üretir: `func newTrustedOrigins(raw string) map[string]bool`; `func corsMiddleware(trustedOrigins map[string]bool, next http.Handler) http.Handler`.

- [x] **Adım 1: Başarısız testi yaz**

```go
// internal/server/cors_test.go
package server_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gokayybaz/bazusop/internal/audittrail"
	"github.com/gokayybaz/bazusop/internal/server"
)

func TestCORSOmitsHeadersWhenNoTrustedOriginsAreConfigured(t *testing.T) {
	t.Parallel()
	handler := server.NewHandler(server.WithAuditTrail(audittrail.NewService(audittrail.NewMemoryStore())))

	request := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	request.Header.Set("Origin", "https://anywhere.example")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected the request to still succeed, got %d", response.Code)
	}
	if response.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("expected no CORS header when no trusted origins are configured")
	}
}

func TestCORSAllowsAConfiguredTrustedOrigin(t *testing.T) {
	t.Parallel()
	handler := server.NewHandler(
		server.WithAuditTrail(audittrail.NewService(audittrail.NewMemoryStore())),
		server.WithTrustedOrigins([]string{"https://ui.example"}),
	)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	request.Header.Set("Origin", "https://ui.example")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Header().Get("Access-Control-Allow-Origin") != "https://ui.example" {
		t.Fatalf("expected the trusted origin to be echoed back, got %q", response.Header().Get("Access-Control-Allow-Origin"))
	}
	if response.Header().Get("Access-Control-Allow-Credentials") != "true" {
		t.Fatal("expected credentials to be allowed for a trusted origin")
	}
}

func TestCORSRejectsAPreflightFromAnUntrustedOrigin(t *testing.T) {
	t.Parallel()
	handler := server.NewHandler(
		server.WithAuditTrail(audittrail.NewService(audittrail.NewMemoryStore())),
		server.WithTrustedOrigins([]string{"https://ui.example"}),
	)

	request := httptest.NewRequest(http.MethodOptions, "/api/v1/health", nil)
	request.Header.Set("Origin", "https://evil.example")
	request.Header.Set("Access-Control-Request-Method", "GET")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for a preflight from an untrusted origin, got %d", response.Code)
	}
}

func TestCORSHandlesAPreflightFromATrustedOrigin(t *testing.T) {
	t.Parallel()
	handler := server.NewHandler(
		server.WithAuditTrail(audittrail.NewService(audittrail.NewMemoryStore())),
		server.WithTrustedOrigins([]string{"https://ui.example"}),
	)

	request := httptest.NewRequest(http.MethodOptions, "/api/v1/health", nil)
	request.Header.Set("Origin", "https://ui.example")
	request.Header.Set("Access-Control-Request-Method", "POST")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for a trusted preflight, got %d", response.Code)
	}
	if response.Header().Get("Access-Control-Allow-Methods") == "" {
		t.Fatal("expected Access-Control-Allow-Methods to be set")
	}
}
```

- [x] **Adım 2: Testin başarısız olduğunu doğrula**

Çalıştır: `GOCACHE=/tmp/bazusop-go-cache go test ./internal/server/... -run TestCORS -v`
Beklenen: BAŞARISIZ — `undefined: server.WithTrustedOrigins`

- [x] **Adım 3: `cors.go`'yu yaz**

```go
// internal/server/cors.go
package server

import "net/http"

func newTrustedOrigins(origins []string) map[string]bool {
	trusted := make(map[string]bool, len(origins))
	for _, origin := range origins {
		if origin != "" {
			trusted[origin] = true
		}
	}
	return trusted
}

// corsMiddleware never blocks a request from reaching next — browsers
// already withhold the response body from cross-origin JS unless
// Access-Control-Allow-Origin matches, so omitting that header for an
// untrusted origin is sufficient for GET-style requests. The one case this
// middleware actively rejects is a CORS preflight (OPTIONS with
// Access-Control-Request-Method) from an untrusted origin, since answering
// it at all would let the browser proceed with the real cross-origin
// request. State-changing same-site requests are separately defended by
// the CSRF token check in internal/server/sessions.go — CORS here is
// defense in depth, not the primary control.
func corsMiddleware(trustedOrigins map[string]bool, next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		origin := request.Header.Get("Origin")
		if origin == "" {
			next.ServeHTTP(response, request)
			return
		}
		isPreflight := request.Method == http.MethodOptions && request.Header.Get("Access-Control-Request-Method") != ""
		if !trustedOrigins[origin] {
			if isPreflight {
				http.Error(response, http.StatusText(http.StatusForbidden), http.StatusForbidden)
				return
			}
			next.ServeHTTP(response, request)
			return
		}
		response.Header().Set("Access-Control-Allow-Origin", origin)
		response.Header().Set("Access-Control-Allow-Credentials", "true")
		response.Header().Set("Vary", "Origin")
		if isPreflight {
			response.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			response.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-CSRF-Token, Authorization")
			response.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(response, request)
	})
}
```

`internal/server/server.go`'da `handlerOptions`'a ekle: `trustedOrigins []string`. Ve şu option'ı ekle:

```go
func WithTrustedOrigins(origins []string) Option {
	return func(options *handlerOptions) { options.trustedOrigins = origins }
}
```

`NewHandler`'ın en sonunda, `mux.Handle("/", webui.Handler())` satırından sonraki `return mux` satırını şununla değiştir:

```go
	return corsMiddleware(newTrustedOrigins(configuration.trustedOrigins), mux)
```

- [x] **Adım 4: `internal/config`'e alan ekle**

`internal/config/config.go`'da `Config`'e ekle: `TrustedOrigins []string`.

`Load()`'a ekle:

```go
		TrustedOrigins: trustedOrigins(os.Getenv("BAZUSOP_TRUSTED_ORIGINS")),
```

Dosyanın sonuna yardımcı fonksiyonu ekle:

```go
func trustedOrigins(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	origins := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			origins = append(origins, trimmed)
		}
	}
	return origins
}
```

`internal/config/config_test.go`'ya ekle:

```go
func TestTrustedOrigins(t *testing.T) {
	t.Setenv("BAZUSOP_TRUSTED_ORIGINS", "https://ui.example, https://admin.example")

	origins := config.Load().TrustedOrigins
	if len(origins) != 2 || origins[0] != "https://ui.example" || origins[1] != "https://admin.example" {
		t.Fatalf("expected two trimmed trusted origins, got %#v", origins)
	}
}

func TestTrustedOriginsDefaultsToEmpty(t *testing.T) {
	t.Setenv("BAZUSOP_TRUSTED_ORIGINS", "")

	if origins := config.Load().TrustedOrigins; len(origins) != 0 {
		t.Fatalf("expected no trusted origins by default, got %#v", origins)
	}
}
```

- [x] **Adım 5: Testleri çalıştırıp geçtiğini doğrula**

Çalıştır: `GOCACHE=/tmp/bazusop-go-cache go test ./internal/server/... ./internal/config/... -v 2>&1 | tail -60`
Beklenen: tüm testler BAŞARILI

- [x] **Adım 6: `go vet` ve `gofmt` çalıştır**

Çalıştır: `go vet ./internal/server/... ./internal/config/... && gofmt -l internal/server/*.go internal/config/*.go`
Beklenen: çıktı yok

- [x] **Adım 7: Commit**

```bash
git add internal/server/cors.go internal/server/cors_test.go internal/server/server.go internal/config/config.go internal/config/config_test.go
git commit -m "feat: add default-closed CORS trusted-origin allowlist"
```

## Görev 5: HTTP bağlantısı — login, whoami, logout, admin toplu iptal

**Dosyalar:**
- Oluştur: `internal/server/sessions.go`
- Oluştur: `internal/server/sessions_test.go`
- Değiştir: `internal/server/middleware.go`
- Değiştir: `internal/server/server.go`
- Değiştir: `internal/audittrail/audittrail.go`
- Değiştir: `cmd/bazusop-hub/main.go`
- Değiştir: `compose.yaml`

**Arayüzler:**
- Tüketir: `identity.Service.VerifyCredentials`/`VerifyCredentialsWithRecoveryCode`/`UserByID` (Görev 2), `sessions.Service` (Görev 1).
- Üretir: `func WithSessions(sessionService *sessions.Service, identityService *identity.Service) Option`.

- [x] **Adım 1: `audittrail.ActorHuman` ekle**

`internal/audittrail/audittrail.go`'da:

```go
	ActorAgent       ActorType = "agent"
	ActorHuman       ActorType = "human"
	ActorLegacyToken ActorType = "legacy_token"
	ActorAnonymous   ActorType = "anonymous"
```

- [x] **Adım 2: `deriveActor`'ı oturum çerezini etiketleyecek şekilde genişlet**

`internal/server/middleware.go`'da import listesine `"crypto/sha256"` ekle. `deriveActor`'ı şu şekilde değiştir (agent bloğundan sonra, bearer bloğundan önce):

```go
func deriveActor(request *http.Request) (audittrail.ActorType, string) {
	if request.TLS != nil && len(request.TLS.PeerCertificates) > 0 {
		cert := request.TLS.PeerCertificates[0]
		if len(cert.URIs) > 0 {
			return audittrail.ActorAgent, cert.URIs[0].String()
		}
		return audittrail.ActorAgent, cert.Subject.CommonName
	}
	if cookie, err := request.Cookie(sessionCookieName); err == nil && cookie.Value != "" {
		sum := sha256.Sum256([]byte(cookie.Value))
		return audittrail.ActorHuman, hex.EncodeToString(sum[:])
	}
	if request.Header.Get("Authorization") != "" {
		return audittrail.ActorLegacyToken, "bearer"
	}
	return audittrail.ActorAnonymous, ""
}
```

**Not:** Bu etiketleme doğrulama yapmaz (fonksiyonun kendi doc yorumunda zaten belirtildiği gibi) — DB'ye gitmeden çerezin SHA-256 özetini loglar. Bu özet, `internal/storage/postgres/sessions.go`'daki `token_hash` sütunuyla aynı değerdir, bu yüzden gerekirse ilgili session satırıyla çapraz referans kurulabilir; çerezin kendisi (bearer sır) hiçbir audit alanına yazılmaz.

- [x] **Adım 3: Başarısız kabul testini yaz**

```go
// internal/server/sessions_test.go
package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gokayybaz/bazusop/internal/audittrail"
	"github.com/gokayybaz/bazusop/internal/identity"
	"github.com/gokayybaz/bazusop/internal/server"
	"github.com/gokayybaz/bazusop/internal/sessions"
)

func newSessionHandler(t *testing.T, adminToken string) (http.Handler, *identity.Service) {
	t.Helper()
	identityService := identity.NewService(identity.NewMemoryStore(), "test-totp-encryption-key")
	sessionService, err := sessions.NewService(sessions.NewMemoryStore(), identityService.IsUserActive)
	if err != nil {
		t.Fatal(err)
	}
	handler := server.NewHandler(
		server.WithIdentity(identityService, "bootstrap-secret"),
		server.WithSessions(sessionService, identityService),
		server.WithAdminToken(adminToken),
		server.WithAuditTrail(audittrail.NewService(audittrail.NewMemoryStore())),
	)
	return handler, identityService
}

func bootstrapAdminWithTOTP(t *testing.T, handler http.Handler) (email, password string) {
	t.Helper()
	email, password = "admin@example.com", "correct horse battery staple"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/bootstrap", encodeJSON(t, map[string]string{
		"secret": "bootstrap-secret", "email": email, "password": password,
	})))
	if response.Code != http.StatusCreated {
		t.Fatalf("bootstrap failed: %d %s", response.Code, response.Body.String())
	}
	return email, password
}

func TestLoginRequiresAValidTOTPCode(t *testing.T) {
	t.Parallel()
	handler, _ := newSessionHandler(t, "admin-token")
	email, password := bootstrapAdminWithTOTP(t, handler)

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/sessions", encodeJSON(t, map[string]string{
		"email": email, "password": password, "totp_code": "000000",
	})))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for a wrong TOTP code, got %d: %s", response.Code, response.Body.String())
	}
}

func TestLoginWhoAmILogoutEndToEnd(t *testing.T) {
	t.Parallel()
	handler, identityService := newSessionHandler(t, "admin-token")
	email, password := bootstrapAdminWithTOTP(t, handler)

	user, err := identityService.UserByID(t.Context(), "")
	_ = user
	_ = err

	// Recover a valid TOTP code the same way TestBootstrapInviteConsumeAndConfirmTOTPEndToEnd
	// would — VerifyCredentials needs a real code, so drive it through the
	// service layer directly here instead of decrypting via HTTP (bootstrap's
	// HTTP response never returns the raw secret, only the provisioning URI).
	t.Fatal("see Adım 3'ün düzeltme notu: gerçek bir TOTP kodu üretmek için servis katmanına doğrudan erişim gerekir")
}
```

**Yazarken düzeltme:** `TestLoginWhoAmILogoutEndToEnd` taslağı çözülemeyen bir soruna işaret ediyor: `POST /api/v1/bootstrap`'ın HTTP yanıtı yalnız `provisioning_uri` döndürür, ham TOTP secret'ını asla döndürmez (spec'in redaksiyon kuralı gereği — bu doğru davranış). Bu yüzden salt HTTP çağrılarıyla geçerli bir TOTP kodu üretmenin yolu yok. Çözüm: testte `identity.NewService`'i doğrudan kullanarak `Bootstrap`'ı servis katmanından çağır (HTTP değil), dönen secret'ı `identity.GenerateTOTPCode` ile kodla — tıpkı `internal/identity/identity_test.go`'daki `TestInviteLifecycleCreateConsumeConfirmTOTP`'ın yaptığı gibi. Ama `Bootstrap`'ın döndürdüğü `TOTPEnrollment` de ham secret'ı taşımıyor (bkz. Görev 4/spike 11.3 — `TOTPEnrollment` bilinçli olarak yalnız `ProvisioningURI` ve `RecoveryCodes` taşır). Gerçek çözüm: testte bootstrap'ı **HTTP üzerinden** yap (yukarıdaki gibi), sonra login'i **recovery code ile** dene — `VerifyCredentialsWithRecoveryCode` TOTP kodu gerektirmez, yalnız e-posta+parola+recovery code ister ve bootstrap'ın HTTP yanıtı `recovery_codes`'u zaten döndürür. Bu hem gerçek bir API akışını test eder hem de ham secret'a erişim ihtiyacını ortadan kaldırır. `TestLoginWhoAmILogoutEndToEnd`'i şu şekilde yeniden yaz (yukarıdaki taslağın tamamının yerine geçer):

```go
func TestLoginWhoAmILogoutEndToEnd(t *testing.T) {
	t.Parallel()
	handler, _ := newSessionHandler(t, "admin-token")

	email, password := "admin@example.com", "correct horse battery staple"
	bootstrapResponse := httptest.NewRecorder()
	handler.ServeHTTP(bootstrapResponse, httptest.NewRequest(http.MethodPost, "/api/v1/bootstrap", encodeJSON(t, map[string]string{
		"secret": "bootstrap-secret", "email": email, "password": password,
	})))
	if bootstrapResponse.Code != http.StatusCreated {
		t.Fatalf("bootstrap failed: %d %s", bootstrapResponse.Code, bootstrapResponse.Body.String())
	}
	var bootstrapPayload struct {
		RecoveryCodes []string `json:"recovery_codes"`
	}
	if err := json.NewDecoder(bootstrapResponse.Body).Decode(&bootstrapPayload); err != nil || len(bootstrapPayload.RecoveryCodes) == 0 {
		t.Fatalf("expected recovery codes from bootstrap: %v", err)
	}

	loginResponse := httptest.NewRecorder()
	handler.ServeHTTP(loginResponse, httptest.NewRequest(http.MethodPost, "/api/v1/sessions", encodeJSON(t, map[string]string{
		"email": email, "password": password, "recovery_code": bootstrapPayload.RecoveryCodes[0],
	})))
	if loginResponse.Code != http.StatusCreated {
		t.Fatalf("expected 201 from login, got %d: %s", loginResponse.Code, loginResponse.Body.String())
	}
	var loginPayload struct {
		UserID    string `json:"user_id"`
		CSRFToken string `json:"csrf_token"`
	}
	if err := json.NewDecoder(loginResponse.Body).Decode(&loginPayload); err != nil || loginPayload.UserID == "" || loginPayload.CSRFToken == "" {
		t.Fatalf("expected a user id and CSRF token from login: %v", err)
	}
	cookies := loginResponse.Result().Cookies()
	if len(cookies) < 2 {
		t.Fatalf("expected both the session and CSRF cookies to be set, got %d cookies", len(cookies))
	}

	whoAmIRequest := httptest.NewRequest(http.MethodGet, "/api/v1/session", nil)
	for _, cookie := range cookies {
		whoAmIRequest.AddCookie(cookie)
	}
	whoAmIResponse := httptest.NewRecorder()
	handler.ServeHTTP(whoAmIResponse, whoAmIRequest)
	if whoAmIResponse.Code != http.StatusOK {
		t.Fatalf("expected 200 from whoami with a valid session cookie, got %d: %s", whoAmIResponse.Code, whoAmIResponse.Body.String())
	}

	logoutWithoutCSRF := httptest.NewRequest(http.MethodDelete, "/api/v1/sessions", nil)
	for _, cookie := range cookies {
		logoutWithoutCSRF.AddCookie(cookie)
	}
	logoutWithoutCSRFResponse := httptest.NewRecorder()
	handler.ServeHTTP(logoutWithoutCSRFResponse, logoutWithoutCSRF)
	if logoutWithoutCSRFResponse.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for logout without a CSRF header, got %d", logoutWithoutCSRFResponse.Code)
	}

	logoutRequest := httptest.NewRequest(http.MethodDelete, "/api/v1/sessions", nil)
	for _, cookie := range cookies {
		logoutRequest.AddCookie(cookie)
	}
	logoutRequest.Header.Set("X-CSRF-Token", loginPayload.CSRFToken)
	logoutResponse := httptest.NewRecorder()
	handler.ServeHTTP(logoutResponse, logoutRequest)
	if logoutResponse.Code != http.StatusNoContent {
		t.Fatalf("expected 204 from logout, got %d: %s", logoutResponse.Code, logoutResponse.Body.String())
	}

	whoAmIAfterLogout := httptest.NewRequest(http.MethodGet, "/api/v1/session", nil)
	for _, cookie := range cookies {
		whoAmIAfterLogout.AddCookie(cookie)
	}
	whoAmIAfterLogoutResponse := httptest.NewRecorder()
	handler.ServeHTTP(whoAmIAfterLogoutResponse, whoAmIAfterLogout)
	if whoAmIAfterLogoutResponse.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 from whoami after logout, got %d", whoAmIAfterLogoutResponse.Code)
	}
}

func TestAdminCanRevokeAllSessionsForAUser(t *testing.T) {
	t.Parallel()
	handler, _ := newSessionHandler(t, "admin-token")
	email, password := "admin@example.com", "correct horse battery staple"
	bootstrapResponse := httptest.NewRecorder()
	handler.ServeHTTP(bootstrapResponse, httptest.NewRequest(http.MethodPost, "/api/v1/bootstrap", encodeJSON(t, map[string]string{
		"secret": "bootstrap-secret", "email": email, "password": password,
	})))
	var bootstrapPayload struct {
		RecoveryCodes []string `json:"recovery_codes"`
	}
	_ = json.NewDecoder(bootstrapResponse.Body).Decode(&bootstrapPayload)

	loginResponse := httptest.NewRecorder()
	handler.ServeHTTP(loginResponse, httptest.NewRequest(http.MethodPost, "/api/v1/sessions", encodeJSON(t, map[string]string{
		"email": email, "password": password, "recovery_code": bootstrapPayload.RecoveryCodes[0],
	})))
	var loginPayload struct {
		UserID string `json:"user_id"`
	}
	_ = json.NewDecoder(loginResponse.Body).Decode(&loginPayload)
	cookies := loginResponse.Result().Cookies()

	revokeRequest := httptest.NewRequest(http.MethodDelete, "/api/v1/users/"+loginPayload.UserID+"/sessions", nil)
	revokeRequest.Header.Set("Authorization", "Bearer admin-token")
	revokeResponse := httptest.NewRecorder()
	handler.ServeHTTP(revokeResponse, revokeRequest)
	if revokeResponse.Code != http.StatusNoContent {
		t.Fatalf("expected 204 from admin revoke, got %d: %s", revokeResponse.Code, revokeResponse.Body.String())
	}

	whoAmIRequest := httptest.NewRequest(http.MethodGet, "/api/v1/session", nil)
	for _, cookie := range cookies {
		whoAmIRequest.AddCookie(cookie)
	}
	whoAmIResponse := httptest.NewRecorder()
	handler.ServeHTTP(whoAmIResponse, whoAmIRequest)
	if whoAmIResponse.Code != http.StatusUnauthorized {
		t.Fatalf("expected the revoked session to be rejected, got %d", whoAmIResponse.Code)
	}
}
```

- [x] **Adım 4: Testin başarısız olduğunu doğrula**

Çalıştır: `GOCACHE=/tmp/bazusop-go-cache go test ./internal/server/... -run 'TestLogin|TestAdminCanRevoke' -v`
Beklenen: BAŞARISIZ — `undefined: server.WithSessions`

- [x] **Adım 5: `internal/server/sessions.go`'yu yaz**

```go
// internal/server/sessions.go
package server

import (
	"crypto/subtle"
	"errors"
	"net/http"

	"github.com/gokayybaz/bazusop/internal/identity"
	"github.com/gokayybaz/bazusop/internal/sessions"
	"github.com/gokayybaz/bazusop/internal/tenancy"
)

const (
	sessionCookieName = "bazusop_session"
	csrfCookieName    = "bazusop_csrf"
)

func WithSessions(sessionService *sessions.Service, identityService *identity.Service) Option {
	return func(options *handlerOptions) {
		options.sessionService = sessionService
		options.sessionIdentityService = identityService
	}
}

func setSessionCookies(response http.ResponseWriter, request *http.Request, token, csrfToken string) {
	secure := request.TLS != nil
	http.SetCookie(response, &http.Cookie{Name: sessionCookieName, Value: token, Path: "/api", HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode})
	http.SetCookie(response, &http.Cookie{Name: csrfCookieName, Value: csrfToken, Path: "/api", HttpOnly: false, Secure: secure, SameSite: http.SameSiteLaxMode})
}

func clearSessionCookies(response http.ResponseWriter, request *http.Request) {
	secure := request.TLS != nil
	http.SetCookie(response, &http.Cookie{Name: sessionCookieName, Value: "", Path: "/api", HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode, MaxAge: -1})
	http.SetCookie(response, &http.Cookie{Name: csrfCookieName, Value: "", Path: "/api", HttpOnly: false, Secure: secure, SameSite: http.SameSiteLaxMode, MaxAge: -1})
}

// authenticateSession resolves and validates the caller's session cookie.
// On any failure it writes a 401 itself and returns ok=false — callers just
// check ok and return.
func authenticateSession(response http.ResponseWriter, request *http.Request, sessionService *sessions.Service) (sessions.Session, string, bool) {
	cookie, err := request.Cookie(sessionCookieName)
	if err != nil || cookie.Value == "" {
		http.Error(response, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
		return sessions.Session{}, "", false
	}
	session, err := sessionService.Validate(request.Context(), cookie.Value)
	if err != nil {
		clearSessionCookies(response, request)
		http.Error(response, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
		return sessions.Session{}, "", false
	}
	return session, cookie.Value, true
}

func requireMatchingCSRFToken(response http.ResponseWriter, request *http.Request, session sessions.Session) bool {
	header := request.Header.Get("X-CSRF-Token")
	if header == "" || subtle.ConstantTimeCompare([]byte(header), []byte(session.CSRFToken)) != 1 {
		http.Error(response, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		return false
	}
	return true
}

func handleLogin(identityService *identity.Service, sessionService *sessions.Service) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		var body struct {
			Email        string `json:"email"`
			Password     string `json:"password"`
			TOTPCode     string `json:"totp_code"`
			RecoveryCode string `json:"recovery_code"`
		}
		if err := decodeJSON(response, request, &body); err != nil {
			return
		}
		var user identity.User
		var err error
		if body.RecoveryCode != "" {
			user, err = identityService.VerifyCredentialsWithRecoveryCode(request.Context(), tenancy.DefaultOrganizationID, body.Email, body.Password, body.RecoveryCode)
		} else {
			user, err = identityService.VerifyCredentials(request.Context(), tenancy.DefaultOrganizationID, body.Email, body.Password, body.TOTPCode)
		}
		if err != nil {
			http.Error(response, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}
		session, token, csrfToken, err := sessionService.Create(request.Context(), user.ID, user.OrganizationID)
		if err != nil {
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		setSessionCookies(response, request, token, csrfToken)
		writeJSON(response, http.StatusCreated, struct {
			UserID    string `json:"user_id"`
			Email     string `json:"email"`
			Role      string `json:"role"`
			CSRFToken string `json:"csrf_token"`
			ExpiresAt string `json:"expires_at"`
		}{user.ID, user.Email, string(user.Role), csrfToken, session.AbsoluteExpiresAt.Format(timeLayout)})
	}
}

const timeLayout = "2006-01-02T15:04:05Z07:00"

func handleWhoAmI(sessionService *sessions.Service, identityService *identity.Service) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		session, _, ok := authenticateSession(response, request, sessionService)
		if !ok {
			return
		}
		user, err := identityService.UserByID(request.Context(), session.UserID)
		if err != nil {
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		writeJSON(response, http.StatusOK, struct {
			UserID    string `json:"user_id"`
			Email     string `json:"email"`
			Role      string `json:"role"`
			ExpiresAt string `json:"expires_at"`
		}{user.ID, user.Email, string(user.Role), session.AbsoluteExpiresAt.Format(timeLayout)})
	}
}

func handleLogout(sessionService *sessions.Service) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		session, token, ok := authenticateSession(response, request, sessionService)
		if !ok {
			return
		}
		if !requireMatchingCSRFToken(response, request, session) {
			return
		}
		if err := sessionService.Revoke(request.Context(), token); err != nil {
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		clearSessionCookies(response, request)
		response.WriteHeader(http.StatusNoContent)
	}
}

func handleRevokeUserSessions(sessionService *sessions.Service, tokens accessTokens) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		if !authorizeRole(response, request, tokens, roleAdmin) {
			return
		}
		if err := sessionService.RevokeAllForUser(request.Context(), request.PathValue("userID")); err != nil {
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		response.WriteHeader(http.StatusNoContent)
	}
}

func handleRevokeAllSessions(sessionService *sessions.Service, tokens accessTokens) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		if !authorizeRole(response, request, tokens, roleAdmin) {
			return
		}
		if err := sessionService.RevokeAllForOrganization(request.Context(), tenancy.DefaultOrganizationID); err != nil {
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		response.WriteHeader(http.StatusNoContent)
	}
}

var _ = errors.Is // corrected below: unused in this file once handlers only compare via sessionService's own sentinel errors internally; see the correction note after this code block.
```

**Yazarken düzeltme:** Yukarıdaki son satır (`var _ = errors.Is`) bir taslak artığıdır — bu dosyada `errors.Is` hiçbir yerde gerçekten kullanılmıyor (hata ayrımı `sessions.Service`'in kendi içinde yapılıyor; HTTP katmanı yalnız `err != nil` kontrolü yapıp 401/500 döndürüyor). Bu satırı ve `"errors"` import'unu **tamamen sil**. Gerçek dosyanın import listesi: `crypto/subtle`, `net/http`, `github.com/gokayybaz/bazusop/internal/identity`, `github.com/gokayybaz/bazusop/internal/sessions`, `github.com/gokayybaz/bazusop/internal/tenancy`.

- [x] **Adım 6: `handlerOptions`, `WithSessions` ve beş rotayı `server.go`'ya bağla**

`internal/server/server.go`'nun import listesine ekle: `"github.com/gokayybaz/bazusop/internal/sessions"`.

`handlerOptions`'a ekle:

```go
	sessionService          *sessions.Service
	sessionIdentityService  *identity.Service
```

`NewHandler`'da, `identityService` bloğundan hemen sonra ekle:

```go
	if configuration.sessionService != nil {
		registerAudited(mux, "/api/v1/sessions", http.MethodPost, "sessions", nil, configuration.auditTrail, configuration.scope, handleLogin(configuration.sessionIdentityService, configuration.sessionService))
		registerAudited(mux, "/api/v1/session", http.MethodGet, "sessions", nil, configuration.auditTrail, configuration.scope, handleWhoAmI(configuration.sessionService, configuration.sessionIdentityService))
		registerAudited(mux, "/api/v1/sessions", http.MethodDelete, "sessions", nil, configuration.auditTrail, configuration.scope, handleLogout(configuration.sessionService))
		tokens := accessTokens{operator: configuration.operatorToken, admin: configuration.adminToken}
		registerAudited(mux, "/api/v1/users/{userID}/sessions", http.MethodDelete, "sessions", []string{"userID"}, configuration.auditTrail, configuration.scope, handleRevokeUserSessions(configuration.sessionService, tokens))
		registerAudited(mux, "/api/v1/sessions/all", http.MethodDelete, "sessions", nil, configuration.auditTrail, configuration.scope, handleRevokeAllSessions(configuration.sessionService, tokens))
	}
```

- [x] **Adım 7: Testleri çalıştırıp geçtiğini doğrula**

Çalıştır: `GOCACHE=/tmp/bazusop-go-cache go test ./internal/server/... -v 2>&1 | tail -120`
Beklenen: tüm testler (yeni ve eski) BAŞARILI

- [x] **Adım 8: `main.go`'ya bağla**

`cmd/bazusop-hub/main.go`'nun import listesine ekle: `"github.com/gokayybaz/bazusop/internal/sessions"`.

`var identityStore identity.Store = identity.NewMemoryStore()` satırından sonra ekle:

```go
	var sessionStore sessions.Store = sessions.NewMemoryStore()
```

Postgres bloğunda, `identityStore = postgresStore` satırından sonra ekle:

```go
		sessionStore = postgresStore
```

`identityService` inşasından hemen sonra ekle:

```go
	var sessionService *sessions.Service
	if identityService != nil {
		sessionService, err = sessions.NewService(sessionStore, identityService.IsUserActive)
		if err != nil {
			logger.Error("could not initialize session service", "error", err)
			os.Exit(1)
		}
	}
```

`server.NewHandler(...)` çağrısına ekle:

```go
			server.WithSessions(sessionService, identityService),
			server.WithTrustedOrigins(configuration.TrustedOrigins),
```

**Not:** `sessionService` yalnız `identityService != nil` iken inşa edilir, bu yüzden `server.WithSessions(sessionService, identityService)` her zaman ya ikisi de `nil` ya da ikisi de dolu geçirir — `NewHandler`'ın `if configuration.sessionService != nil` kontrolü tutarlıdır, `handleLogin` içinde asla nil `identityService`'e erişilmez.

- [x] **Adım 9: `compose.yaml`'a `BAZUSOP_TRUSTED_ORIGINS`'i ekle**

`internal/config` bölümünde spike 11.3'ün eklediği iki satırdan sonra:

```yaml
      BAZUSOP_TRUSTED_ORIGINS: "${BAZUSOP_TRUSTED_ORIGINS:-}"
```

- [x] **Adım 10: Build ve tam test paketini çalıştır**

Çalıştır: `go build ./... && go vet ./... && gofmt -l . 2>&1 | grep -v node_modules && BAZUSOP_TEST_DATABASE_URL='postgres://bazusop:bazusop_test@127.0.0.1:5432/bazusop_test?sslmode=disable' GOCACHE=/tmp/bazusop-go-cache go test ./...`
Beklenen: build başarılı; vet/gofmt çıktısı yok; her paket `ok`

- [x] **Adım 11: Commit**

```bash
git add internal/server/sessions.go internal/server/sessions_test.go internal/server/middleware.go internal/server/server.go internal/audittrail/audittrail.go cmd/bazusop-hub/main.go compose.yaml
git commit -m "feat: wire login, whoami, logout, and admin bulk-revoke endpoints"
```

## Görev 6: Canlı hub'a karşı manuel doğrulama

**Dosyalar:** yok (yalnız doğrulama).

- [x] **Adım 1: Yeni secret'larla yerel bir hub ayağa kaldır**

Çalıştır: `POSTGRES_PASSWORD=verify-pw BAZUSOP_ENROLLMENT_TOKEN=verify-token BAZUSOP_OPERATOR_TOKEN=verify-operator BAZUSOP_ADMIN_TOKEN=verify-admin BAZUSOP_BOOTSTRAP_SECRET=verify-bootstrap BAZUSOP_TOTP_ENCRYPTION_KEY=verify-totp-key BAZUSOP_PORT=18093 docker compose -p bazusop-verify-sessions up --build -d` ve hub konteynerinin healthy olmasını bekle.

- [x] **Adım 2: Bootstrap ol ve recovery code'ları al**

```bash
curl -s -c /tmp/bazusop-cookies.txt -X POST http://127.0.0.1:18093/api/v1/bootstrap \
  -d '{"secret":"verify-bootstrap","email":"admin@example.com","password":"correct horse battery staple"}'
```

Beklenen: `201`, yanıt gövdesinde `recovery_codes` dizisi.

- [x] **Adım 3: Recovery code ile giriş yap**

```bash
curl -s -i -c /tmp/bazusop-cookies.txt -X POST http://127.0.0.1:18093/api/v1/sessions \
  -d '{"email":"admin@example.com","password":"correct horse battery staple","recovery_code":"<yukarıdaki ilk recovery_code>"}'
```

Beklenen: `201`, `Set-Cookie: bazusop_session=...` ve `Set-Cookie: bazusop_csrf=...` header'ları, yanıt gövdesinde `csrf_token`.

- [x] **Adım 4: whoami ile oturumu doğrula**

```bash
curl -s -b /tmp/bazusop-cookies.txt http://127.0.0.1:18093/api/v1/session
```

Beklenen: `200`, `user_id`/`email`/`role` alanları.

- [x] **Adım 5: CSRF token olmadan logout'un reddedildiğini doğrula, sonra doğru token ile çıkış yap**

```bash
curl -s -o /dev/null -w "status:%{http_code}\n" -b /tmp/bazusop-cookies.txt -X DELETE http://127.0.0.1:18093/api/v1/sessions
curl -s -o /dev/null -w "status:%{http_code}\n" -b /tmp/bazusop-cookies.txt -X DELETE http://127.0.0.1:18093/api/v1/sessions \
  -H "X-CSRF-Token: <adım 3'teki csrf_token>"
curl -s -o /dev/null -w "status:%{http_code}\n" -b /tmp/bazusop-cookies.txt http://127.0.0.1:18093/api/v1/session
```

Beklenen: sırasıyla `403`, `204`, `401`.

- [x] **Adım 6: Denetim izini sorgula**

```bash
docker compose -p bazusop-verify-sessions exec postgres psql -U bazusop -d bazusop -c \
  "SELECT action, resource_type, outcome, error_code FROM audit_events WHERE resource_type = 'sessions' ORDER BY occurred_at;"
```

Beklenen: login/whoami/logout denemelerinin her biri için birer satır (başarısız CSRF'siz logout `outcome=failure`/`error_code=403` olarak görünür).

- [x] **Adım 7: Kapat ve geçici dosyaları temizle**

Çalıştır: `docker compose -p bazusop-verify-sessions down -v && rm -f /tmp/bazusop-cookies.txt`

## Kendi Kendine İnceleme

**1. Spec kapsaması.**
- Opaque cookie, HttpOnly, TLS altında Secure, SameSite=Lax, daraltılmış path/host kapsamı → Görev 5 (`setSessionCookies`: `Path: "/api"`, `Domain` ayarlanmıyor → host-only, `Secure` request.TLS'e göre koşullu).
- Yetki/site üyeliği istemci token'ına gömülmez, her istek sunucu taraflı güncel durumu kullanır → `Validate` her çağrıda store'dan taze okur, hiçbir yetki bilgisi cookie içinde taşınmaz (yalnız opak token).
- Girişte rotasyon → her `Create` taze session/token/CSRF üretir (Genel Kısıtlar'da gerekçelendirildi); yetki seviyesi değişince rotasyon → `Service.Rotate` primitive'i inşa edildi ve test edildi, HTTP'den çağrılması 11.5'e bırakıldı (açıkça not edildi).
- 30dk boşta / 8sa mutlak süre aşımı → Görev 1, `WithIdleTimeout`/`WithAbsoluteTimeout` varsayılanları, deterministik sahte-saat testleriyle doğrulandı.
- Toplu iptal (kullanıcı bazlı ve organizasyon geneli) → Görev 1 (`RevokeAllForUser`/`RevokeAllForOrganization`) + Görev 5 (admin bridge token ile HTTP uçları) + Görev 6 (canlı doğrulama).
- Kullanıcı devre dışı bırakıldığında oturumlar geçersiz olur → `UserActiveChecker` ile `Validate`'e gömülü; gerçek bir disable endpoint'i olmadan bile test edilebilir (Görev 2'nin testleri).
- CSRF, durum değiştiren cookie tabanlı isteklerde doğrulanır → Görev 5 (`requireMatchingCSRFToken`, yalnız `handleLogout`'ta).
- CORS varsayılan kapalı, güvenilir origin listesi açık yapılandırma gerektirir → Görev 4.
- MFA sıfırlandığında oturum iptali → **bu spike'ta yok**, açıkça ertelendi (Genel Kısıtlar): MFA sıfırlama endpoint'i henüz mevcut değil (spike 11.3'ün kendi self-review'ünde de aynı gerekçeyle ertelenmişti).

**2. Placeholder taraması.** Her adımda gerçek kod var. İki bilinçli "önce yanlış yaz, sonra düzelt" dizisi var (Görev 1'deki gereksiz `subtle` import'u, Görev 5'teki gereksiz `errors.Is` satırı) — her biri açık bir **düzeltme** notuyla ve tam yerine geçecek kodla birlikte veriliyor, çözümsüz bir `TODO` olarak bırakılmıyor.

**3. Tip tutarlılığı.** `sessions.Session`, `sessions.Store`, `sessions.UserActiveChecker` imzaları Görev 1'de tanımlandığı gibi Görev 2, 3 ve 5 boyunca birebir aynı kullanılıyor. `identity.Service.IsUserActive`'in imzası (`func(context.Context, string) (bool, error)`) `sessions.UserActiveChecker` ile bire bir eşleşiyor, bu yüzden `main.go`'da `identityService.IsUserActive` hiçbir adaptör olmadan doğrudan geçirilebiliyor.
