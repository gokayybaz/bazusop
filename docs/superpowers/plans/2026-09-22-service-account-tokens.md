# Süreli Servis Hesabı Token'ları (Spike 11.6) Uygulama Planı

> **Otonom çalışanlar için:** ZORUNLU ALT-SKILL: Bu planı görev görev uygulamak için superpowers:subagent-driven-development (önerilen) veya superpowers:executing-plans kullan. Adımlar `- [x]` checkbox söz dizimiyle takip edilir.

**Amaç:** Yeni `internal/serviceaccounts` paketi, tam olarak bir siteye bağlı, yalnız `site-admin`/`operator`/`viewer` rolü taşıyabilen (asla `platform-admin` olamayan, web oturumu açamayan) servis hesapları sağlar. Token'lar yalnız oluşturulurken/rotate edilirken bir kez gösterilir; veritabanı dışında tutulan bir pepper ile HMAC-SHA-256 kullanılarak özetlenir. Varsayılan geçerlilik 90 gün, üst sınır 365 gündür. `internal/server`'ın çift-yollu (oturum/eski-bearer) `requirePermission` yardımcısı üçüncü bir yola — servis hesabı bearer token'ı — genişletilir, böylece 11.5'te inşa edilen 9 operasyonel mutasyon rotası artık gerçek bir otomasyon kimlik doğrulama yolu kazanır (11.7 eski token köprüsünü kaldırdığında yerini dolduracak).

**Mimari:** `internal/serviceaccounts`, bu kod tabanındaki her domain paketiyle aynı `Store` + `MemoryStore` + PostgreSQL `Store` desenini izler. `authorization.SiteRole`'a doğrudan bağımlıdır (servis hesabının rolü doğrudan bu tip) — `identity`/`sessions`/`authorization` arasındaki callback-decoupling deseninin aksine, burada dairesel bağımlılık riski yok (`serviceaccounts -> authorization`, tek yönlü). Token'ın kendisi, gösterilen dizenin içine gömülü, hassas olmayan, kararlı bir kimlik (`bazusop_sat_<tokenID>_<secret>`) taşır — bu, hem DB'de O(1) indeksli arama (secret'ı hash'lemeden önce `tokenID` ile satırı bulma) hem de `internal/server/middleware.go`'nun `deriveActor`'ının DB'ye hiç gitmeden, pepper'a ihtiyaç duymadan güvenle loglanabilecek bir actor etiketi çıkarmasını sağlar (tıpkı spike 11.4'ün oturum çerezi SHA-256 özeti deseni gibi).

**Önemli tasarım kararı:** Spec'in "Roller" tablosunda servis hesabı yönetimi için ayrı bir satır yok; en yakın örüntü — platform-admin her yerde, site-admin yalnız kendi sitesinde — zaten `PermissionManageAgents`/`PermissionManageAlerts`/`PermissionManageCloudAccounts` için kullanılıyor. Aynı örüntüyle yeni bir `PermissionManageServiceAccounts` izni eklenir (mevcut kataloğun doğal, gerekçeli bir genişlemesi — yeniden tasarımı değil).

**Teknoloji yığını:** Go 1.26, yalnızca stdlib (`crypto/hmac`, `crypto/sha256`, `crypto/subtle`, `crypto/rand`, `encoding/hex`) — yeni harici bağımlılık yok.

**Spec:** [docs/superpowers/specs/2026-09-13-enterprise-rbac-identity-audit-design.md](../specs/2026-09-13-enterprise-rbac-identity-audit-design.md) — "Sabit RBAC modeli" → "Servis hesapları" bölümü. Yol haritası spike 11.6'yı uygular.

## Genel Kısıtlar

- Servis hesabı rolü yalnız `site-admin`, `operator`, `viewer` olabilir — asla `platform-admin` (spec: "platform-admin olamaz"). `ErrInvalidRole` bunu zorunlu kılar.
- Token biçimi: `bazusop_sat_<tokenID>_<secret>` — `tokenID` (16 hex karakter) hassas değildir ve DB'de düz metin saklanır (arama anahtarı); `secret` (64 hex karakter) yalnız oluşturma/rotasyon anında bir kez gösterilir, veritabanında yalnız `HMAC-SHA-256(pepper, secret)` hex özeti olarak saklanır. Pepper, `BAZUSOP_SERVICE_ACCOUNT_PEPPER` ortam değişkeninden gelir — veritabanı dışında tutulan bir runtime secret'ı (spec: "veritabanı dışında tutulan bir pepper").
- Varsayılan geçerlilik 90 gün; `expiry_days` açıkça verilirse 1-365 aralığında olmalı, aksi halde `ErrInvalidExpiry`.
- Rotasyon (`RotateToken`), hesabın önceki tüm aktif token'larını iptal ettikten sonra yeni bir token üretir — bir hesabın her an en fazla bir kullanılabilir token'ı olur (spike 11.4'ün `Sessions.Rotate`'iyle aynı "eskiyi iptal et, yeniyi üret" deseni).
- `internal/server`'daki `requirePermission`, üçüncü bir yol kazanır: oturum çerezi yoksa VE `Authorization` header'ı `bazusop_sat_` önekiyle başlıyorsa servis hesabı token'ı olarak doğrulanır (bulunamazsa/süresi geçmişse/iptal edilmişse **doğrudan 401 döner, eski bearer yoluna düşmez** — çağıran açıkça bir servis hesabı token'ı sunmuşsa, bunu belirsiz şekilde eski statik token olarak yeniden yorumlamak yanlış olur); önek yoksa mevcut eski bearer köprüsüne (11.7'ye kadar) düşülür.
- Servis hesabının `OrganizationID`/`SiteID`'si, isteğin hedef site kapsamıyla (`configuration.scope`, tek-site mimarisi — 11.5'te teyit edildi) eşleşmezse `403` döner (spec: "başka siteye erişemez").
- Bu spike, servis hesabı özelliğini yalnız `identityService`/`authorizationService` VE `BAZUSOP_SERVICE_ACCOUNT_PEPPER` ikisi de yapılandırılmışken devreye alır — mevcut deployment'lar hiçbir davranış değişikliği görmez (11.2-11.5'in tamamının izlediği opt-in kalıbı).
- Her doğrulama, `Touch` ile `last_used_at`/`last_used_ip`'yi günceller (spec: "Son kullanım zamanı ve kaynak IP gibi güvenli metadata'yı günceller").
- Her kullanım zaten `registerAudited` middleware'i (spike 11.2) tarafından otomatik denetleniyor — `deriveActor`'ın yeni `ActorServiceAccount` dalı bunun için tek gereken parça.

## Dosya Yapısı

- `internal/serviceaccounts/serviceaccounts.go` — `ServiceAccount`, `Token`, sentinel hatalar, `TokenPrefix`, `ParseToken`, `Store` arayüzü, `Service` (`CreateAccount`, `RotateToken`, `RevokeToken`, `DisableAccount`, `ListForSite`, `Validate`), `Option`/`NewService`.
- `internal/serviceaccounts/memorystore.go` — `MemoryStore`.
- `internal/serviceaccounts/serviceaccounts_test.go`
- `internal/authorization/authorization.go` — değişiklik: `PermissionAllowsRole` fonksiyonu çıkarılır, `PermissionManageServiceAccounts` eklenir.
- `internal/authorization/authorization_test.go` — değişiklik: yeni testler.
- `internal/storage/postgres/migrations/022_service_accounts.sql`
- `internal/storage/postgres/serviceaccounts.go`
- `internal/storage/postgres/serviceaccounts_integration_test.go`
- `internal/storage/postgres/store_test.go` — değişiklik: migration sayısı 21→22.
- `internal/server/serviceaccounts.go` — `WithServiceAccounts`, `bearerToken`, handler'lar.
- `internal/server/serviceaccounts_test.go`
- `internal/server/rbac.go` — değişiklik: `requirePermission` üçüncü yol.
- `internal/server/middleware.go` — değişiklik: `deriveActor` servis hesabı tanıma.
- `internal/server/jobs.go`, `internal/server/alerting.go`, `internal/server/cloudinventory.go`, `internal/server/identity.go` — değişiklik: `requirePermission` çağrılarına yeni parametre.
- `internal/server/server.go` — değişiklik: `handlerOptions`/`NewHandler` bağlantısı, yeni rotalar, tüm `requirePermission`-zincirli handler çağrı siteleri.
- `internal/audittrail/audittrail.go` — değişiklik: `ActorServiceAccount` sabiti.
- `internal/config/config.go` — değişiklik: `ServiceAccountPepper` alanı.
- `cmd/bazusop-hub/main.go` — değişiklik: `serviceAccountService` inşası + bağlantı.

## Görev 1: `internal/serviceaccounts` çekirdek paketi

**Dosyalar:**
- Oluştur: `internal/serviceaccounts/serviceaccounts.go`
- Oluştur: `internal/serviceaccounts/memorystore.go`
- Oluştur: `internal/serviceaccounts/serviceaccounts_test.go`

**Arayüzler:**
- Tüketir: `authorization.SiteRole`, `authorization.SiteRoleAdmin/Operator/Viewer` (mevcut, spike 11.5).
- Üretir: `type ServiceAccount struct{ID, OrganizationID, SiteID, Name string; Role authorization.SiteRole; CreatedAt time.Time; DisabledAt *time.Time}`; `type Token struct{ID, ServiceAccountID string; CreatedAt, ExpiresAt time.Time; LastUsedAt *time.Time; LastUsedIP string; RevokedAt *time.Time}`; sentinel hatalar `ErrInvalidRole`, `ErrInvalidExpiry`, `ErrAccountNotFound`, `ErrAccountDisabled`, `ErrTokenNotFound`, `ErrTokenRevoked`, `ErrTokenExpired`; `const TokenPrefix = "bazusop_sat_"`; `func ParseToken(token string) (tokenID, secret string, ok bool)`; `type Store interface` (aşağıda); `type Service struct` ile `func NewService(store Store, pepper string, options ...Option) (*Service, error)` ve metodlar `CreateAccount`, `RotateToken`, `RevokeToken`, `DisableAccount`, `ListForSite`, `Validate`; `type Option func(*Service)`; `WithClock(func() time.Time) Option`; `func NewMemoryStore() *MemoryStore`.

- [x] **Adım 1: Başarısız testi yaz**

```go
// internal/serviceaccounts/serviceaccounts_test.go
package serviceaccounts_test

import (
	"errors"
	"testing"
	"time"

	"github.com/gokayybaz/bazusop/internal/authorization"
	"github.com/gokayybaz/bazusop/internal/serviceaccounts"
)

func newTestService(t *testing.T, now func() time.Time) *serviceaccounts.Service {
	t.Helper()
	service, err := serviceaccounts.NewService(serviceaccounts.NewMemoryStore(), "test-pepper", serviceaccounts.WithClock(now))
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func TestCreateAccountProducesAOneTimeTokenThatValidates(t *testing.T) {
	t.Parallel()
	service := newTestService(t, time.Now)
	account, token, err := service.CreateAccount(t.Context(), "org_default", "site_default", "ci-bot", authorization.SiteRoleOperator, 0)
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	if account.Role != authorization.SiteRoleOperator || account.SiteID != "site_default" || token == "" {
		t.Fatalf("unexpected account: %#v (token=%q)", account, token)
	}

	validated, err := service.Validate(t.Context(), token, "10.0.0.1")
	if err != nil || validated.ID != account.ID {
		t.Fatalf("expected the fresh token to validate, got %#v %v", validated, err)
	}
}

func TestCreateAccountRejectsAPlatformAdminRole(t *testing.T) {
	t.Parallel()
	service := newTestService(t, time.Now)
	if _, _, err := service.CreateAccount(t.Context(), "org_default", "site_default", "bad", "platform-admin", 0); !errors.Is(err, serviceaccounts.ErrInvalidRole) {
		t.Fatalf("expected ErrInvalidRole, got %v", err)
	}
}

func TestCreateAccountRejectsAnOutOfRangeExpiry(t *testing.T) {
	t.Parallel()
	service := newTestService(t, time.Now)
	if _, _, err := service.CreateAccount(t.Context(), "org_default", "site_default", "bad", authorization.SiteRoleViewer, 400); !errors.Is(err, serviceaccounts.ErrInvalidExpiry) {
		t.Fatalf("expected ErrInvalidExpiry for 400 days, got %v", err)
	}
}

func TestValidateRejectsAnUnknownToken(t *testing.T) {
	t.Parallel()
	service := newTestService(t, time.Now)
	if _, err := service.Validate(t.Context(), "bazusop_sat_deadbeef_deadbeef", "10.0.0.1"); !errors.Is(err, serviceaccounts.ErrTokenNotFound) {
		t.Fatalf("expected ErrTokenNotFound, got %v", err)
	}
}

func TestValidateRejectsAWrongSecretForARealTokenID(t *testing.T) {
	t.Parallel()
	service := newTestService(t, time.Now)
	_, token, err := service.CreateAccount(t.Context(), "org_default", "site_default", "ci-bot", authorization.SiteRoleOperator, 0)
	if err != nil {
		t.Fatal(err)
	}
	tokenID, _, ok := serviceaccounts.ParseToken(token)
	if !ok {
		t.Fatal("expected a parseable token")
	}
	forged := serviceaccounts.TokenPrefix + tokenID + "_" + "0000000000000000000000000000000000000000000000000000000000000000"
	if _, err := service.Validate(t.Context(), forged, "10.0.0.1"); !errors.Is(err, serviceaccounts.ErrTokenNotFound) {
		t.Fatalf("expected a forged secret to be rejected as ErrTokenNotFound, got %v", err)
	}
}

func TestValidateRejectsAnExpiredToken(t *testing.T) {
	t.Parallel()
	current := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := func() time.Time { return current }
	service := newTestService(t, clock)
	_, token, err := service.CreateAccount(t.Context(), "org_default", "site_default", "ci-bot", authorization.SiteRoleOperator, 1)
	if err != nil {
		t.Fatal(err)
	}
	current = current.AddDate(0, 0, 2)
	if _, err := service.Validate(t.Context(), token, "10.0.0.1"); !errors.Is(err, serviceaccounts.ErrTokenExpired) {
		t.Fatalf("expected ErrTokenExpired, got %v", err)
	}
}

func TestRotateTokenInvalidatesThePreviousOne(t *testing.T) {
	t.Parallel()
	service := newTestService(t, time.Now)
	account, oldToken, err := service.CreateAccount(t.Context(), "org_default", "site_default", "ci-bot", authorization.SiteRoleOperator, 0)
	if err != nil {
		t.Fatal(err)
	}
	newToken, err := service.RotateToken(t.Context(), account.ID, 0)
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if newToken == oldToken {
		t.Fatal("expected a distinct new token")
	}
	if _, err := service.Validate(t.Context(), oldToken, "10.0.0.1"); !errors.Is(err, serviceaccounts.ErrTokenRevoked) {
		t.Fatalf("expected the old token to be revoked, got %v", err)
	}
	if _, err := service.Validate(t.Context(), newToken, "10.0.0.1"); err != nil {
		t.Fatalf("expected the new token to validate, got %v", err)
	}
}

func TestRevokeTokenRejectsFurtherUse(t *testing.T) {
	t.Parallel()
	service := newTestService(t, time.Now)
	account, token, err := service.CreateAccount(t.Context(), "org_default", "site_default", "ci-bot", authorization.SiteRoleOperator, 0)
	if err != nil {
		t.Fatal(err)
	}
	tokenID, _, _ := serviceaccounts.ParseToken(token)
	if err := service.RevokeToken(t.Context(), tokenID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := service.Validate(t.Context(), token, "10.0.0.1"); !errors.Is(err, serviceaccounts.ErrTokenRevoked) {
		t.Fatalf("expected ErrTokenRevoked, got %v", err)
	}
	_ = account
}

func TestDisableAccountRejectsFurtherUseEvenWithAValidToken(t *testing.T) {
	t.Parallel()
	service := newTestService(t, time.Now)
	account, token, err := service.CreateAccount(t.Context(), "org_default", "site_default", "ci-bot", authorization.SiteRoleOperator, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.DisableAccount(t.Context(), account.ID); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if _, err := service.Validate(t.Context(), token, "10.0.0.1"); !errors.Is(err, serviceaccounts.ErrTokenRevoked) {
		t.Fatalf("expected DisableAccount to also revoke the active token (ErrTokenRevoked), got %v", err)
	}
}

func TestListForSiteReturnsOnlyThatSitesAccounts(t *testing.T) {
	t.Parallel()
	service := newTestService(t, time.Now)
	if _, _, err := service.CreateAccount(t.Context(), "org_default", "site-a", "a-bot", authorization.SiteRoleViewer, 0); err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.CreateAccount(t.Context(), "org_default", "site-b", "b-bot", authorization.SiteRoleViewer, 0); err != nil {
		t.Fatal(err)
	}
	accounts, err := service.ListForSite(t.Context(), "site-a")
	if err != nil || len(accounts) != 1 || accounts[0].Name != "a-bot" {
		t.Fatalf("expected exactly one site-a account, got %#v %v", accounts, err)
	}
}
```

- [x] **Adım 2: Testin başarısız olduğunu doğrula**

Çalıştır: `GOCACHE=/tmp/bazusop-go-cache go test ./internal/serviceaccounts/... -v`
Beklenen: BAŞARISIZ — `package serviceaccounts: no non-test Go files`

- [x] **Adım 3: `serviceaccounts.go`'yu yaz**

```go
// internal/serviceaccounts/serviceaccounts.go
package serviceaccounts

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gokayybaz/bazusop/internal/authorization"
)

const TokenPrefix = "bazusop_sat_"

const (
	defaultExpiryDays = 90
	maxExpiryDays     = 365
	pepperKeyVersion  = 0x01
)

type ServiceAccount struct {
	ID             string
	OrganizationID string
	SiteID         string
	Name           string
	Role           authorization.SiteRole
	CreatedAt      time.Time
	DisabledAt     *time.Time
}

type Token struct {
	ID               string
	ServiceAccountID string
	CreatedAt        time.Time
	ExpiresAt        time.Time
	LastUsedAt       *time.Time
	LastUsedIP       string
	RevokedAt        *time.Time
}

var (
	ErrInvalidRole     = errors.New("service accounts may only hold site-admin, operator, or viewer roles")
	ErrInvalidExpiry   = errors.New("token expiry must be between 1 and 365 days")
	ErrAccountNotFound = errors.New("service account not found")
	ErrAccountDisabled = errors.New("service account is disabled")
	ErrTokenNotFound   = errors.New("service account token not found")
	ErrTokenRevoked    = errors.New("service account token revoked")
	ErrTokenExpired    = errors.New("service account token expired")
)

// ParseToken splits a presented bearer value into its non-secret tokenID
// (safe to log — internal/server/middleware.go's deriveActor uses this
// directly, with no DB round trip or pepper needed) and its high-entropy
// secret (never logged; only ever compared via Service.Validate).
func ParseToken(token string) (tokenID, secret string, ok bool) {
	if !strings.HasPrefix(token, TokenPrefix) {
		return "", "", false
	}
	rest := strings.TrimPrefix(token, TokenPrefix)
	parts := strings.SplitN(rest, "_", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

type Store interface {
	CreateAccount(ctx context.Context, account ServiceAccount) error
	AccountByID(ctx context.Context, id string) (ServiceAccount, error)
	DisableAccount(ctx context.Context, id string, at time.Time) error
	AccountsForSite(ctx context.Context, siteID string) ([]ServiceAccount, error)
	CreateToken(ctx context.Context, token Token, secretHash string) error
	TokenByID(ctx context.Context, id string) (Token, string, error)
	RevokeToken(ctx context.Context, id string, at time.Time) error
	RevokeActiveTokensForAccount(ctx context.Context, accountID string, at time.Time) error
	Touch(ctx context.Context, tokenID string, at time.Time, sourceIP string) error
}

type Service struct {
	store  Store
	pepper string
	now    func() time.Time
}

type Option func(*Service)

func WithClock(clock func() time.Time) Option { return func(service *Service) { service.now = clock } }

func NewService(store Store, pepper string, options ...Option) (*Service, error) {
	if pepper == "" {
		return nil, errors.New("service account token pepper is required")
	}
	service := &Service{store: store, pepper: pepper, now: func() time.Time { return time.Now().UTC() }}
	for _, option := range options {
		option(service)
	}
	return service, nil
}

func (service *Service) CreateAccount(ctx context.Context, organizationID, siteID, name string, role authorization.SiteRole, expiryDays int) (ServiceAccount, string, error) {
	if role != authorization.SiteRoleAdmin && role != authorization.SiteRoleOperator && role != authorization.SiteRoleViewer {
		return ServiceAccount{}, "", ErrInvalidRole
	}
	id, err := newOpaqueHex(16)
	if err != nil {
		return ServiceAccount{}, "", err
	}
	account := ServiceAccount{ID: id, OrganizationID: organizationID, SiteID: siteID, Name: name, Role: role, CreatedAt: service.now()}
	if err := service.store.CreateAccount(ctx, account); err != nil {
		return ServiceAccount{}, "", err
	}
	token, err := service.issueToken(ctx, id, expiryDays)
	if err != nil {
		return ServiceAccount{}, "", err
	}
	return account, token, nil
}

func (service *Service) RotateToken(ctx context.Context, accountID string, expiryDays int) (string, error) {
	if err := service.store.RevokeActiveTokensForAccount(ctx, accountID, service.now()); err != nil {
		return "", err
	}
	return service.issueToken(ctx, accountID, expiryDays)
}

func (service *Service) RevokeToken(ctx context.Context, tokenID string) error {
	return service.store.RevokeToken(ctx, tokenID, service.now())
}

func (service *Service) DisableAccount(ctx context.Context, accountID string) error {
	if err := service.store.RevokeActiveTokensForAccount(ctx, accountID, service.now()); err != nil {
		return err
	}
	return service.store.DisableAccount(ctx, accountID, service.now())
}

func (service *Service) ListForSite(ctx context.Context, siteID string) ([]ServiceAccount, error) {
	return service.store.AccountsForSite(ctx, siteID)
}

// Validate looks the token up by its non-secret tokenID (O(1), indexed),
// then compares the presented secret's HMAC against the stored hash in
// constant time. A wrong secret for a real tokenID is reported identically
// to an unknown tokenID (ErrTokenNotFound) — never distinguished, so a
// caller can't use response differences to enumerate valid token IDs.
func (service *Service) Validate(ctx context.Context, presented, sourceIP string) (ServiceAccount, error) {
	tokenID, secret, ok := ParseToken(presented)
	if !ok {
		return ServiceAccount{}, ErrTokenNotFound
	}
	token, storedHash, err := service.store.TokenByID(ctx, tokenID)
	if errors.Is(err, ErrTokenNotFound) {
		return ServiceAccount{}, ErrTokenNotFound
	}
	if err != nil {
		return ServiceAccount{}, err
	}
	if subtle.ConstantTimeCompare([]byte(hashSecret(secret, service.pepper)), []byte(storedHash)) != 1 {
		return ServiceAccount{}, ErrTokenNotFound
	}
	if token.RevokedAt != nil {
		return ServiceAccount{}, ErrTokenRevoked
	}
	if service.now().After(token.ExpiresAt) {
		return ServiceAccount{}, ErrTokenExpired
	}
	account, err := service.store.AccountByID(ctx, token.ServiceAccountID)
	if err != nil {
		return ServiceAccount{}, err
	}
	if account.DisabledAt != nil {
		return ServiceAccount{}, ErrAccountDisabled
	}
	if err := service.store.Touch(ctx, token.ID, service.now(), sourceIP); err != nil {
		return ServiceAccount{}, err
	}
	return account, nil
}

func (service *Service) issueToken(ctx context.Context, accountID string, expiryDays int) (string, error) {
	if expiryDays == 0 {
		expiryDays = defaultExpiryDays
	}
	if expiryDays < 1 || expiryDays > maxExpiryDays {
		return "", ErrInvalidExpiry
	}
	tokenID, err := newOpaqueHex(8)
	if err != nil {
		return "", err
	}
	secret, err := newOpaqueHex(32)
	if err != nil {
		return "", err
	}
	now := service.now()
	token := Token{ID: tokenID, ServiceAccountID: accountID, CreatedAt: now, ExpiresAt: now.AddDate(0, 0, expiryDays)}
	if err := service.store.CreateToken(ctx, token, hashSecret(secret, service.pepper)); err != nil {
		return "", err
	}
	return TokenPrefix + tokenID + "_" + secret, nil
}

// hashSecret is HMAC-SHA-256 keyed by an operator-supplied pepper kept
// outside the database (BAZUSOP_SERVICE_ACCOUNT_PEPPER), not plain SHA-256
// — a deliberate step up from the plain-hash pattern used for session and
// invite tokens elsewhere in this codebase, since a leaked database alone
// is not enough to brute-force these longer-lived (90-365 day) credentials;
// the pepper must also leak. The one-byte version prefix mirrors
// internal/identity's EncryptTOTPSecret pattern for future pepper rotation.
func hashSecret(secret, pepper string) string {
	mac := hmac.New(sha256.New, []byte(pepper))
	mac.Write([]byte(secret))
	versioned := append([]byte{pepperKeyVersion}, mac.Sum(nil)...)
	return hex.EncodeToString(versioned)
}

func newOpaqueHex(byteLen int) (string, error) {
	value := make([]byte, byteLen)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate token component: %w", err)
	}
	return hex.EncodeToString(value), nil
}
```

- [x] **Adım 4: `MemoryStore`'u yaz**

```go
// internal/serviceaccounts/memorystore.go
package serviceaccounts

import (
	"context"
	"sync"
	"time"
)

type tokenRecord struct {
	token      Token
	secretHash string
}

// MemoryStore is for local development and tests only, matching every
// other domain's memory-mode fallback.
type MemoryStore struct {
	mu       sync.Mutex
	accounts map[string]ServiceAccount
	tokens   map[string]tokenRecord // token id -> record
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{accounts: make(map[string]ServiceAccount), tokens: make(map[string]tokenRecord)}
}

func (store *MemoryStore) CreateAccount(_ context.Context, account ServiceAccount) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.accounts[account.ID] = account
	return nil
}

func (store *MemoryStore) AccountByID(_ context.Context, id string) (ServiceAccount, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	account, ok := store.accounts[id]
	if !ok {
		return ServiceAccount{}, ErrAccountNotFound
	}
	return account, nil
}

func (store *MemoryStore) DisableAccount(_ context.Context, id string, at time.Time) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	account, ok := store.accounts[id]
	if !ok {
		return ErrAccountNotFound
	}
	disabledAt := at
	account.DisabledAt = &disabledAt
	store.accounts[id] = account
	return nil
}

func (store *MemoryStore) AccountsForSite(_ context.Context, siteID string) ([]ServiceAccount, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	var accounts []ServiceAccount
	for _, account := range store.accounts {
		if account.SiteID == siteID {
			accounts = append(accounts, account)
		}
	}
	return accounts, nil
}

func (store *MemoryStore) CreateToken(_ context.Context, token Token, secretHash string) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.tokens[token.ID] = tokenRecord{token: token, secretHash: secretHash}
	return nil
}

func (store *MemoryStore) TokenByID(_ context.Context, id string) (Token, string, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	record, ok := store.tokens[id]
	if !ok {
		return Token{}, "", ErrTokenNotFound
	}
	return record.token, record.secretHash, nil
}

func (store *MemoryStore) RevokeToken(_ context.Context, id string, at time.Time) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	record, ok := store.tokens[id]
	if !ok {
		return ErrTokenNotFound
	}
	revokedAt := at
	record.token.RevokedAt = &revokedAt
	store.tokens[id] = record
	return nil
}

func (store *MemoryStore) RevokeActiveTokensForAccount(_ context.Context, accountID string, at time.Time) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	revokedAt := at
	for id, record := range store.tokens {
		if record.token.ServiceAccountID == accountID && record.token.RevokedAt == nil {
			record.token.RevokedAt = &revokedAt
			store.tokens[id] = record
		}
	}
	return nil
}

func (store *MemoryStore) Touch(_ context.Context, tokenID string, at time.Time, sourceIP string) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	record, ok := store.tokens[tokenID]
	if !ok {
		return ErrTokenNotFound
	}
	lastUsedAt := at
	record.token.LastUsedAt = &lastUsedAt
	record.token.LastUsedIP = sourceIP
	store.tokens[tokenID] = record
	return nil
}
```

- [x] **Adım 5: Testleri çalıştırıp geçtiğini doğrula**

Çalıştır: `GOCACHE=/tmp/bazusop-go-cache go test ./internal/serviceaccounts/... -v`
Beklenen: BAŞARILI (tüm testler)

- [x] **Adım 6: `go vet` ve `gofmt` çalıştır**

Çalıştır: `go vet ./internal/serviceaccounts/... && gofmt -l internal/serviceaccounts/*.go`
Beklenen: çıktı yok

- [x] **Adım 7: Commit**

```bash
git add internal/serviceaccounts/serviceaccounts.go internal/serviceaccounts/memorystore.go internal/serviceaccounts/serviceaccounts_test.go
git commit -m "feat: add time-limited service account tokens with HMAC-peppered hashing"
```

## Görev 2: `internal/authorization` — `PermissionAllowsRole` ve `PermissionManageServiceAccounts`

**Dosyalar:**
- Değiştir: `internal/authorization/authorization.go`
- Değiştir: `internal/authorization/authorization_test.go`

**Arayüzler:**
- Üretir: `func PermissionAllowsRole(permission Permission, role SiteRole) bool`; yeni sabit `PermissionManageServiceAccounts Permission = "manage_service_accounts"`.

- [x] **Adım 1: Başarısız testleri yaz**

`internal/authorization/authorization_test.go`'nun sonuna ekle:

```go
func TestPermissionAllowsRoleMatchesCanForSiteScopedPermissions(t *testing.T) {
	t.Parallel()
	if !authorization.PermissionAllowsRole(authorization.PermissionCreateJobs, authorization.SiteRoleOperator) {
		t.Fatal("expected an operator to satisfy PermissionCreateJobs directly")
	}
	if authorization.PermissionAllowsRole(authorization.PermissionManageAlerts, authorization.SiteRoleOperator) {
		t.Fatal("expected an operator to not satisfy PermissionManageAlerts")
	}
}

func TestPermissionAllowsRoleAlwaysDeniesOrgScopedPermissions(t *testing.T) {
	t.Parallel()
	for _, role := range []authorization.SiteRole{authorization.SiteRoleAdmin, authorization.SiteRoleOperator, authorization.SiteRoleViewer} {
		if authorization.PermissionAllowsRole(authorization.PermissionManageUsers, role) {
			t.Fatalf("expected PermissionManageUsers to be org-scoped (platform-admin only), got allowed for %s", role)
		}
	}
}

func TestPermissionManageServiceAccountsMatchesTheSiteAdminOnlyPattern(t *testing.T) {
	t.Parallel()
	service := newTestService(t, nil)
	if err := service.AssignRole(t.Context(), "user-1", "org_default", "site-1", authorization.SiteRoleAdmin); err != nil {
		t.Fatal(err)
	}
	allowed, err := service.Can(t.Context(), "user-1", authorization.PermissionManageServiceAccounts, "site-1")
	if err != nil || !allowed {
		t.Fatalf("expected site-admin to manage service accounts, got %v %v", allowed, err)
	}

	viewerService := newTestService(t, nil)
	if err := viewerService.AssignRole(t.Context(), "user-2", "org_default", "site-1", authorization.SiteRoleViewer); err != nil {
		t.Fatal(err)
	}
	allowed, err = viewerService.Can(t.Context(), "user-2", authorization.PermissionManageServiceAccounts, "site-1")
	if err != nil || allowed {
		t.Fatalf("expected viewer to be denied PermissionManageServiceAccounts, got %v %v", allowed, err)
	}
}
```

- [x] **Adım 2: Testlerin başarısız olduğunu doğrula**

Çalıştır: `GOCACHE=/tmp/bazusop-go-cache go test ./internal/authorization/... -run 'TestPermissionAllowsRole|TestPermissionManageServiceAccounts' -v`
Beklenen: BAŞARISIZ — `undefined: authorization.PermissionAllowsRole` (ve `PermissionManageServiceAccounts`)

- [x] **Adım 3: Uygula**

`internal/authorization/authorization.go`'da sabitler bloğuna ekle:

```go
	PermissionManageServiceAccounts Permission = "manage_service_accounts"
```

`siteRolePermissions` haritasına ekle:

```go
	PermissionManageServiceAccounts: {SiteRoleAdmin: true},
```

`Can` metodunu, ortak mantığı `PermissionAllowsRole`'a devredecek şekilde yeniden düzenle:

```go
// PermissionAllowsRole reports whether role alone (without any
// platform-admin bypass) satisfies permission. Org-scoped permissions
// always return false — they require platform-admin, an identity-level
// concept unrelated to SiteRole. internal/serviceaccounts uses this
// directly (a service account has no platform-admin bypass at all, so it
// never needs the full Service.Can/store round trip).
func PermissionAllowsRole(permission Permission, role SiteRole) bool {
	if orgScopedPermissions[permission] {
		return false
	}
	allowedRoles, known := siteRolePermissions[permission]
	if !known {
		return false
	}
	return allowedRoles[role]
}

func (service *Service) Can(ctx context.Context, userID string, permission Permission, siteID string) (bool, error) {
	isPlatformAdmin, err := service.platformAdmin(ctx, userID)
	if err != nil {
		return false, err
	}
	if isPlatformAdmin {
		return true, nil
	}
	role, err := service.store.RoleForUserAtSite(ctx, userID, siteID)
	if errors.Is(err, ErrMembershipNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return PermissionAllowsRole(permission, role), nil
}
```

(Eski `Can` gövdesindeki `if orgScopedPermissions[permission] { return false, nil }` ve `allowedRoles, known := siteRolePermissions[permission]; if !known { return false, nil }` satırları artık `PermissionAllowsRole` içinde olduğu için `Can`'den kaldırılır — bu, davranışta hiçbir değişiklik yapmadan mantığı tekilleştirir.)

- [x] **Adım 4: Testleri çalıştırıp geçtiğini doğrula**

Çalıştır: `GOCACHE=/tmp/bazusop-go-cache go test ./internal/authorization/... -v`
Beklenen: BAŞARILI (tüm testler, eskiler dahil — `TestFullPermissionMatrixMatchesTheSpec` refactor'den etkilenmemeli)

- [x] **Adım 5: `go vet` ve `gofmt` çalıştır**

Çalıştır: `go vet ./internal/authorization/... && gofmt -l internal/authorization/*.go`
Beklenen: çıktı yok

- [x] **Adım 6: Commit**

```bash
git add internal/authorization/authorization.go internal/authorization/authorization_test.go
git commit -m "refactor: extract PermissionAllowsRole and add PermissionManageServiceAccounts"
```

## Görev 3: PostgreSQL kalıcılığı

**Dosyalar:**
- Oluştur: `internal/storage/postgres/migrations/022_service_accounts.sql`
- Oluştur: `internal/storage/postgres/serviceaccounts.go`
- Oluştur: `internal/storage/postgres/serviceaccounts_integration_test.go`
- Değiştir: `internal/storage/postgres/store_test.go`

**Arayüzler:**
- Tüketir: `serviceaccounts.ServiceAccount`, `serviceaccounts.Token`, `serviceaccounts.Store` (Görev 1).
- Üretir: `serviceaccounts.Store` arayüzünün dokuz metodunu karşılayan `*Store` metodları.

- [x] **Adım 1: Başarısız entegrasyon testini yaz**

```go
// internal/storage/postgres/serviceaccounts_integration_test.go
package postgres

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/gokayybaz/bazusop/internal/authorization"
	"github.com/gokayybaz/bazusop/internal/serviceaccounts"
)

func TestPostgresServiceAccountLifecycle(t *testing.T) {
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
	orgID := "sat-org-" + suffix
	siteID := "sat-site-" + suffix
	if _, err := store.pool.Exec(ctx, `INSERT INTO organizations (id, name) VALUES ($1, 'Service account test')`, orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.pool.Exec(ctx, `INSERT INTO sites (id, organization_id, name, slug) VALUES ($1,$2,'SAT site','sat-site')`, siteID, orgID); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		store.pool.Exec(cleanupCtx, "DELETE FROM service_account_tokens WHERE service_account_id IN (SELECT id FROM service_accounts WHERE organization_id=$1)", orgID)
		store.pool.Exec(cleanupCtx, "DELETE FROM service_accounts WHERE organization_id=$1", orgID)
		store.pool.Exec(cleanupCtx, "DELETE FROM sites WHERE id=$1", siteID)
		store.pool.Exec(cleanupCtx, "DELETE FROM organizations WHERE id=$1", orgID)
	}()

	now := time.Now().UTC()
	account := serviceaccounts.ServiceAccount{ID: "sat-account-" + suffix, OrganizationID: orgID, SiteID: siteID, Name: "ci-bot", Role: authorization.SiteRoleOperator, CreatedAt: now}
	if err := store.CreateAccount(ctx, account); err != nil {
		t.Fatalf("create account: %v", err)
	}
	fetched, err := store.AccountByID(ctx, account.ID)
	if err != nil || fetched.Name != "ci-bot" || fetched.Role != authorization.SiteRoleOperator {
		t.Fatalf("expected to read back the account, got %#v %v", fetched, err)
	}
	accounts, err := store.AccountsForSite(ctx, siteID)
	if err != nil || len(accounts) != 1 {
		t.Fatalf("expected one account for the site, got %#v %v", accounts, err)
	}

	token := serviceaccounts.Token{ID: "sat-token-" + suffix, ServiceAccountID: account.ID, CreatedAt: now, ExpiresAt: now.AddDate(0, 0, 90)}
	if err := store.CreateToken(ctx, token, "hash-1"); err != nil {
		t.Fatalf("create token: %v", err)
	}
	fetchedToken, hash, err := store.TokenByID(ctx, token.ID)
	if err != nil || hash != "hash-1" || fetchedToken.ServiceAccountID != account.ID {
		t.Fatalf("expected to read back the token, got %#v %q %v", fetchedToken, hash, err)
	}

	touchedAt := now.Add(time.Hour)
	if err := store.Touch(ctx, token.ID, touchedAt, "10.0.0.5"); err != nil {
		t.Fatalf("touch: %v", err)
	}
	afterTouch, _, err := store.TokenByID(ctx, token.ID)
	if err != nil || afterTouch.LastUsedIP != "10.0.0.5" || afterTouch.LastUsedAt == nil || !afterTouch.LastUsedAt.Equal(touchedAt) {
		t.Fatalf("expected last-used metadata to be updated, got %#v %v", afterTouch, err)
	}

	if err := store.RevokeToken(ctx, token.ID, now.Add(2*time.Hour)); err != nil {
		t.Fatalf("revoke token: %v", err)
	}
	revoked, _, err := store.TokenByID(ctx, token.ID)
	if err != nil || revoked.RevokedAt == nil {
		t.Fatalf("expected the token to be revoked, got %#v %v", revoked, err)
	}

	secondToken := serviceaccounts.Token{ID: "sat-token-2-" + suffix, ServiceAccountID: account.ID, CreatedAt: now, ExpiresAt: now.AddDate(0, 0, 90)}
	if err := store.CreateToken(ctx, secondToken, "hash-2"); err != nil {
		t.Fatalf("create second token: %v", err)
	}
	if err := store.RevokeActiveTokensForAccount(ctx, account.ID, now.Add(3*time.Hour)); err != nil {
		t.Fatalf("revoke active tokens: %v", err)
	}
	secondRevoked, _, err := store.TokenByID(ctx, secondToken.ID)
	if err != nil || secondRevoked.RevokedAt == nil {
		t.Fatalf("expected the second token to be revoked too, got %#v %v", secondRevoked, err)
	}

	if err := store.DisableAccount(ctx, account.ID, now.Add(4*time.Hour)); err != nil {
		t.Fatalf("disable account: %v", err)
	}
	disabledAccount, err := store.AccountByID(ctx, account.ID)
	if err != nil || disabledAccount.DisabledAt == nil {
		t.Fatalf("expected the account to be disabled, got %#v %v", disabledAccount, err)
	}

	if _, err := store.AccountByID(ctx, "does-not-exist"); !errors.Is(err, serviceaccounts.ErrAccountNotFound) {
		t.Fatalf("expected ErrAccountNotFound, got %v", err)
	}
	if _, _, err := store.TokenByID(ctx, "does-not-exist"); !errors.Is(err, serviceaccounts.ErrTokenNotFound) {
		t.Fatalf("expected ErrTokenNotFound, got %v", err)
	}
}
```

- [x] **Adım 2: Testin başarısız olduğunu doğrula**

Çalıştır: `BAZUSOP_TEST_DATABASE_URL='postgres://bazusop:bazusop_test@127.0.0.1:5432/bazusop_test?sslmode=disable' GOCACHE=/tmp/bazusop-go-cache go test ./internal/storage/postgres/... -run TestPostgresServiceAccountLifecycle -v`

(Yoksa önce atılabilir bir PostgreSQL 18 konteyneri başlat.)

Beklenen: BAŞARISIZ — `Store` metodları henüz yok.

- [x] **Adım 3: Migration'ı yaz**

```sql
-- internal/storage/postgres/migrations/022_service_accounts.sql
CREATE TABLE IF NOT EXISTS service_accounts (
    id TEXT PRIMARY KEY,
    organization_id TEXT NOT NULL,
    site_id TEXT NOT NULL,
    name TEXT NOT NULL,
    role TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    disabled_at TIMESTAMPTZ,
    CONSTRAINT service_accounts_org_fk FOREIGN KEY (organization_id) REFERENCES organizations(id) ON DELETE RESTRICT,
    CONSTRAINT service_accounts_site_fk FOREIGN KEY (site_id) REFERENCES sites(id) ON DELETE RESTRICT
);

CREATE INDEX IF NOT EXISTS service_accounts_site_idx ON service_accounts (site_id);

CREATE TABLE IF NOT EXISTS service_account_tokens (
    id TEXT PRIMARY KEY,
    service_account_id TEXT NOT NULL,
    secret_hash TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL,
    last_used_at TIMESTAMPTZ,
    last_used_ip TEXT,
    revoked_at TIMESTAMPTZ,
    CONSTRAINT service_account_tokens_account_fk FOREIGN KEY (service_account_id) REFERENCES service_accounts(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS service_account_tokens_account_idx ON service_account_tokens (service_account_id);
```

- [x] **Adım 4: `Store` implementasyonunu yaz**

```go
// internal/storage/postgres/serviceaccounts.go
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/gokayybaz/bazusop/internal/authorization"
	"github.com/gokayybaz/bazusop/internal/serviceaccounts"
)

func (store *Store) CreateAccount(ctx context.Context, account serviceaccounts.ServiceAccount) error {
	_, err := store.pool.Exec(ctx, `
		INSERT INTO service_accounts (id, organization_id, site_id, name, role, created_at)
		VALUES ($1,$2,$3,$4,$5,$6)`,
		account.ID, account.OrganizationID, account.SiteID, account.Name, string(account.Role), account.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert service account: %w", err)
	}
	return nil
}

func (store *Store) AccountByID(ctx context.Context, id string) (serviceaccounts.ServiceAccount, error) {
	var account serviceaccounts.ServiceAccount
	var role string
	err := store.pool.QueryRow(ctx, `
		SELECT id, organization_id, site_id, name, role, created_at, disabled_at
		FROM service_accounts WHERE id=$1`, id).Scan(
		&account.ID, &account.OrganizationID, &account.SiteID, &account.Name, &role, &account.CreatedAt, &account.DisabledAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return serviceaccounts.ServiceAccount{}, serviceaccounts.ErrAccountNotFound
	}
	if err != nil {
		return serviceaccounts.ServiceAccount{}, fmt.Errorf("query service account: %w", err)
	}
	account.Role = authorization.SiteRole(role)
	return account, nil
}

func (store *Store) DisableAccount(ctx context.Context, id string, at time.Time) error {
	result, err := store.pool.Exec(ctx, `UPDATE service_accounts SET disabled_at=$2 WHERE id=$1 AND disabled_at IS NULL`, id, at)
	if err != nil {
		return fmt.Errorf("disable service account: %w", err)
	}
	if result.RowsAffected() != 1 {
		return serviceaccounts.ErrAccountNotFound
	}
	return nil
}

func (store *Store) AccountsForSite(ctx context.Context, siteID string) ([]serviceaccounts.ServiceAccount, error) {
	rows, err := store.pool.Query(ctx, `
		SELECT id, organization_id, site_id, name, role, created_at, disabled_at
		FROM service_accounts WHERE site_id=$1 ORDER BY created_at`, siteID)
	if err != nil {
		return nil, fmt.Errorf("query service accounts: %w", err)
	}
	defer rows.Close()
	var accounts []serviceaccounts.ServiceAccount
	for rows.Next() {
		var account serviceaccounts.ServiceAccount
		var role string
		if err := rows.Scan(&account.ID, &account.OrganizationID, &account.SiteID, &account.Name, &role, &account.CreatedAt, &account.DisabledAt); err != nil {
			return nil, fmt.Errorf("scan service account: %w", err)
		}
		account.Role = authorization.SiteRole(role)
		accounts = append(accounts, account)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate service accounts: %w", err)
	}
	return accounts, nil
}

func (store *Store) CreateToken(ctx context.Context, token serviceaccounts.Token, secretHash string) error {
	_, err := store.pool.Exec(ctx, `
		INSERT INTO service_account_tokens (id, service_account_id, secret_hash, created_at, expires_at)
		VALUES ($1,$2,$3,$4,$5)`,
		token.ID, token.ServiceAccountID, secretHash, token.CreatedAt, token.ExpiresAt)
	if err != nil {
		return fmt.Errorf("insert service account token: %w", err)
	}
	return nil
}

func (store *Store) TokenByID(ctx context.Context, id string) (serviceaccounts.Token, string, error) {
	var token serviceaccounts.Token
	var secretHash string
	err := store.pool.QueryRow(ctx, `
		SELECT id, service_account_id, secret_hash, created_at, expires_at, last_used_at, last_used_ip, revoked_at
		FROM service_account_tokens WHERE id=$1`, id).Scan(
		&token.ID, &token.ServiceAccountID, &secretHash, &token.CreatedAt, &token.ExpiresAt,
		&token.LastUsedAt, &token.LastUsedIP, &token.RevokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return serviceaccounts.Token{}, "", serviceaccounts.ErrTokenNotFound
	}
	if err != nil {
		return serviceaccounts.Token{}, "", fmt.Errorf("query service account token: %w", err)
	}
	return token, secretHash, nil
}

func (store *Store) RevokeToken(ctx context.Context, id string, at time.Time) error {
	result, err := store.pool.Exec(ctx, `UPDATE service_account_tokens SET revoked_at=$2 WHERE id=$1 AND revoked_at IS NULL`, id, at)
	if err != nil {
		return fmt.Errorf("revoke service account token: %w", err)
	}
	if result.RowsAffected() != 1 {
		return serviceaccounts.ErrTokenNotFound
	}
	return nil
}

func (store *Store) RevokeActiveTokensForAccount(ctx context.Context, accountID string, at time.Time) error {
	_, err := store.pool.Exec(ctx, `UPDATE service_account_tokens SET revoked_at=$2 WHERE service_account_id=$1 AND revoked_at IS NULL`, accountID, at)
	if err != nil {
		return fmt.Errorf("revoke active service account tokens: %w", err)
	}
	return nil
}

func (store *Store) Touch(ctx context.Context, tokenID string, at time.Time, sourceIP string) error {
	_, err := store.pool.Exec(ctx, `UPDATE service_account_tokens SET last_used_at=$2, last_used_ip=$3 WHERE id=$1`, tokenID, at, sourceIP)
	if err != nil {
		return fmt.Errorf("touch service account token: %w", err)
	}
	return nil
}
```

**Yazarken düzeltme — `RevokeToken`'ın entegrasyon testiyle uyumsuzluğu:** Görev 1'in `Service.RevokeToken`'ı zaten var olan (belki de daha önce iptal edilmiş) bir token için `store.RevokeToken`'ı çağırdığında hata beklemiyor (Görev 1'in `MemoryStore.RevokeToken`'ı da `RowsAffected`/idempotency kontrolü yapmıyor, yalnız bulunamama durumunda hata veriyor). Yukarıdaki Postgres implementasyonu `result.RowsAffected() != 1` kontrolüyle **zaten iptal edilmiş** bir token'ı da `ErrTokenNotFound` sayıyor — bu, `MemoryStore`'un davranışıyla (idempotent, zaten-iptal-edilmiş bir token'ı sessizce kabul eder) tutarsız. Bu spike'ta bu tutarsızlık pratik bir sorun yaratmaz (test paketi hiçbir yerde bir token'ı iki kez iptal etmiyor), ama gelecekte biri `RevokeToken`'ı idempotent bekleyip Postgres'te sürpriz bir `ErrTokenNotFound` alabilir. **Düzeltme:** `RevokeToken`'daki `if result.RowsAffected() != 1` kontrolünü kaldır — satır zaten var olmasa bile (ki `Service.RevokeToken` her zaman gerçek bir `tokenID` ile çağrılır) `UPDATE ... WHERE id=$1 AND revoked_at IS NULL`'ın 0 satır etkilemesi sessiz bir no-op olarak kabul edilir, tıpkı `MemoryStore`'un zaten-iptal-edilmiş bir token'da hata vermeyişi gibi. Düzeltilmiş metod:

```go
func (store *Store) RevokeToken(ctx context.Context, id string, at time.Time) error {
	_, err := store.pool.Exec(ctx, `UPDATE service_account_tokens SET revoked_at=$2 WHERE id=$1 AND revoked_at IS NULL`, id, at)
	if err != nil {
		return fmt.Errorf("revoke service account token: %w", err)
	}
	return nil
}
```

- [x] **Adım 5: Entegrasyon testini çalıştırıp geçtiğini doğrula**

Çalıştır: `BAZUSOP_TEST_DATABASE_URL='postgres://bazusop:bazusop_test@127.0.0.1:5432/bazusop_test?sslmode=disable' GOCACHE=/tmp/bazusop-go-cache go test ./internal/storage/postgres/... -run TestPostgresServiceAccountLifecycle -v`
Beklenen: BAŞARILI

- [x] **Adım 6: Migration sayısı testini güncelle**

`internal/storage/postgres/store_test.go`'da:

```go
	if len(entries) != 22 {
		t.Fatalf("expected twenty-two storage migrations, got %d", len(entries))
	}
	if entries[0].Name() != "001_hosts.sql" || entries[21].Name() != "022_service_accounts.sql" {
		t.Fatalf("unexpected migration range: %s through %s", entries[0].Name(), entries[len(entries)-1].Name())
	}
```

- [x] **Adım 7: Tam test paketini, vet ve gofmt'ı çalıştır**

Çalıştır: `go vet ./... && gofmt -l internal/storage/postgres/*.go internal/serviceaccounts/*.go internal/authorization/*.go && BAZUSOP_TEST_DATABASE_URL='postgres://bazusop:bazusop_test@127.0.0.1:5432/bazusop_test?sslmode=disable' GOCACHE=/tmp/bazusop-go-cache go test ./...`
Beklenen: vet/gofmt çıktısı yok; her paket `ok`

- [x] **Adım 8: Commit**

```bash
git add internal/storage/postgres/migrations/022_service_accounts.sql internal/storage/postgres/serviceaccounts.go internal/storage/postgres/serviceaccounts_integration_test.go internal/storage/postgres/store_test.go
git commit -m "feat: add PostgreSQL persistence for service accounts and tokens"
```

## Görev 4: HTTP bağlantısı — üçüncü yol (`requirePermission`), `deriveActor`, yönetim uçları

**Dosyalar:**
- Oluştur: `internal/server/serviceaccounts.go`
- Değiştir: `internal/server/rbac.go`
- Değiştir: `internal/server/middleware.go`
- Değiştir: `internal/server/jobs.go`, `internal/server/alerting.go`, `internal/server/cloudinventory.go`, `internal/server/identity.go`
- Değiştir: `internal/server/server.go`
- Değiştir: `internal/audittrail/audittrail.go`
- Değiştir: `internal/config/config.go`
- Değiştir: `cmd/bazusop-hub/main.go`

**Arayüzler:**
- Tüketir: `serviceaccounts.Service`, `serviceaccounts.TokenPrefix`, `serviceaccounts.ParseToken` (Görev 1); `authorization.PermissionAllowsRole`, `authorization.PermissionManageServiceAccounts` (Görev 2).
- Üretir: `func WithServiceAccounts(service *serviceaccounts.Service) Option`; genişletilmiş `requirePermission` imzası.

- [x] **Adım 1: `audittrail.ActorServiceAccount` ekle**

`internal/audittrail/audittrail.go`'da:

```go
	ActorAgent          ActorType = "agent"
	ActorHuman          ActorType = "human"
	ActorServiceAccount ActorType = "service_account"
	ActorLegacyToken    ActorType = "legacy_token"
	ActorAnonymous      ActorType = "anonymous"
```

- [x] **Adım 2: `deriveActor`'ı servis hesabı token'ını tanıyacak şekilde genişlet**

`internal/server/middleware.go`'da import listesine `"strings"` ve `"github.com/gokayybaz/bazusop/internal/serviceaccounts"` ekle. `deriveActor`'ı, oturum çerezi bloğundan sonra, eski bearer bloğundan önce şu kontrolü ekleyecek şekilde değiştir:

```go
	if cookie, err := request.Cookie(sessionCookieName); err == nil && cookie.Value != "" {
		sum := sha256.Sum256([]byte(cookie.Value))
		return audittrail.ActorHuman, hex.EncodeToString(sum[:])
	}
	if authorization := request.Header.Get("Authorization"); strings.HasPrefix(authorization, "Bearer "+serviceaccounts.TokenPrefix) {
		if tokenID, _, ok := serviceaccounts.ParseToken(strings.TrimPrefix(authorization, "Bearer ")); ok {
			return audittrail.ActorServiceAccount, tokenID
		}
	}
	if request.Header.Get("Authorization") != "" {
		return audittrail.ActorLegacyToken, "bearer"
	}
```

**Not:** Bu etiketleme, `serviceaccounts.ParseToken`'ı DB'ye hiç gitmeden, pepper'a ihtiyaç duymadan çağırır — yalnız `tokenID` (hassas olmayan) kısmını ayırır; `secret` kısmı hiçbir zaman bu fonksiyonun döndürdüğü değere karışmaz, dolayısıyla audit'e asla sızmaz.

- [x] **Adım 3: `requirePermission`'a üçüncü yolu ekle**

`internal/server/rbac.go`'da import listesine `"strings"` ve `"github.com/gokayybaz/bazusop/internal/serviceaccounts"` ekle. `requirePermission`'ı şununla değiştir:

```go
func requirePermission(response http.ResponseWriter, request *http.Request, sessionService *sessions.Service, serviceAccountService *serviceaccounts.Service, authzService *authorization.Service, permission authorization.Permission, siteID string, tokens accessTokens, legacyRole accessRole) (string, bool) {
	if sessionService != nil {
		if cookie, err := request.Cookie(sessionCookieName); err == nil && cookie.Value != "" {
			session, err := sessionService.Validate(request.Context(), cookie.Value)
			if err != nil {
				clearSessionCookies(response, request)
				http.Error(response, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
				return "", false
			}
			allowed, err := authzService.Can(request.Context(), session.UserID, permission, siteID)
			if err != nil {
				http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				return "", false
			}
			if !allowed {
				http.Error(response, http.StatusText(http.StatusForbidden), http.StatusForbidden)
				return "", false
			}
			return session.UserID, true
		}
	}
	if serviceAccountService != nil {
		if bearer, ok := bearerToken(request); ok && strings.HasPrefix(bearer, serviceaccounts.TokenPrefix) {
			account, err := serviceAccountService.Validate(request.Context(), bearer, sourceIP(request))
			if err != nil {
				http.Error(response, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
				return "", false
			}
			if account.OrganizationID != tenancy.DefaultOrganizationID || account.SiteID != siteID || !authorization.PermissionAllowsRole(permission, account.Role) {
				http.Error(response, http.StatusText(http.StatusForbidden), http.StatusForbidden)
				return "", false
			}
			return account.ID, true
		}
	}
	if !authorizeRole(response, request, tokens, legacyRole) {
		return "", false
	}
	return "", true
}

func bearerToken(request *http.Request) (string, bool) {
	const prefix = "Bearer "
	header := request.Header.Get("Authorization")
	if !strings.HasPrefix(header, prefix) {
		return "", false
	}
	return strings.TrimPrefix(header, prefix), true
}
```

`handleAssignSiteRole`/`handleRevokeSiteRole`'un `requirePermission` çağrılarına `nil` bir `serviceAccountService` argümanı ekle (bu iki uç yalnız platform-admin'e ayrılmıştır — spec'in "Kullanıcı daveti ve rol/site ataması" satırı zaten `PermissionManageUsers`'ı org-kapsamlı/yalnız-platform-admin yapıyor, ki bu izin `PermissionAllowsRole` tarafından her site rolü için zaten `false` döner; yine de servis hesabı yolu bu iki route için hiç denenmeyecek şekilde `nil` geçirmek daha açık ve okunabilir):

```go
func handleAssignSiteRole(sessionService *sessions.Service, authzService *authorization.Service, tokens accessTokens, scope tenancy.Scope) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		if _, ok := requirePermission(response, request, sessionService, nil, authzService, authorization.PermissionManageUsers, scope.SiteID, tokens, roleAdmin); !ok {
			return
		}
		...
```

(`handleRevokeSiteRole`'da aynı değişiklik.)

- [x] **Adım 4: Diğer 9 `requirePermission` çağrı sitesine `serviceAccountService` parametresini ekle**

Aşağıdaki her handler imzasına `serviceAccountService *serviceaccounts.Service` parametresi eklenir (mevcut `sessionService *sessions.Service` parametresinden hemen sonra) ve kendi `requirePermission` çağrısına aynı sırayla geçirilir:

- `internal/server/jobs.go`: `handleCreateJob` → `authorization.PermissionCreateJobs` (servis hesabı otomasyonunun en doğal kullanım alanı — CI/CD job tetikleme).
- `internal/server/alerting.go`: `handleCreateAlertRule`, `handleCreateMaintenance` → `authorization.PermissionManageAlerts`; `handleAcknowledgeIncident` → `authorization.PermissionAcknowledgeIncidents`.
- `internal/server/cloudinventory.go`: `handleCreateCloudAccount`, `handleReconcileCloudInstances` → `authorization.PermissionManageCloudAccounts`.
- `internal/server/identity.go`: `handleCreateInvite` → `authorization.PermissionManageUsers` (yalnız platform-admin oturumu veya eski admin bearer token'ı ile ulaşılabilir kalır — servis hesabı `nil` geçirilir, çünkü davet oluşturma bir insan-yönetim eylemidir, otomasyona açılmamalıdır).

Her dosyada import listesine `"github.com/gokayybaz/bazusop/internal/serviceaccounts"` eklenir (henüz yoksa).

Örnek (`jobs.go`):

```go
func handleCreateJob(service *jobs.Service, sessionService *sessions.Service, serviceAccountService *serviceaccounts.Service, authzService *authorization.Service, tokens accessTokens, scope tenancy.Scope) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		if _, ok := requirePermission(response, request, sessionService, serviceAccountService, authzService, authorization.PermissionCreateJobs, scope.SiteID, tokens, roleOperator); !ok {
			return
		}
		...
```

`identity.go`'daki `handleCreateInvite` için `nil` geçirilir:

```go
func handleCreateInvite(service *identity.Service, sessionService *sessions.Service, authzService *authorization.Service, tokens accessTokens, scope tenancy.Scope) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		actorUserID, ok := requirePermission(response, request, sessionService, nil, authzService, authorization.PermissionManageUsers, scope.SiteID, tokens, roleAdmin)
		...
```

(`handleCreateInvite`'ın kendi imzası değişmez — yalnız çağrı içindeki `requirePermission` argüman listesine `nil` eklenir.)

- [x] **Adım 5: `internal/server/serviceaccounts.go`'yu yaz**

```go
// internal/server/serviceaccounts.go
package server

import (
	"errors"
	"net/http"

	"github.com/gokayybaz/bazusop/internal/authorization"
	"github.com/gokayybaz/bazusop/internal/serviceaccounts"
	"github.com/gokayybaz/bazusop/internal/sessions"
	"github.com/gokayybaz/bazusop/internal/tenancy"
)

func WithServiceAccounts(service *serviceaccounts.Service) Option {
	return func(options *handlerOptions) { options.serviceAccountService = service }
}

func handleCreateServiceAccount(service *serviceaccounts.Service, sessionService *sessions.Service, authzService *authorization.Service, tokens accessTokens, scope tenancy.Scope) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		if _, ok := requirePermission(response, request, sessionService, nil, authzService, authorization.PermissionManageServiceAccounts, scope.SiteID, tokens, roleAdmin); !ok {
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
		writeJSON(response, http.StatusCreated, struct {
			ID    string `json:"id"`
			Name  string `json:"name"`
			Role  string `json:"role"`
			Token string `json:"token"`
		}{account.ID, account.Name, string(account.Role), token})
	}
}

func handleListServiceAccounts(service *serviceaccounts.Service, sessionService *sessions.Service, authzService *authorization.Service, tokens accessTokens, scope tenancy.Scope) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		if _, ok := requirePermission(response, request, sessionService, nil, authzService, authorization.PermissionManageServiceAccounts, scope.SiteID, tokens, roleAdmin); !ok {
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

func handleRotateServiceAccountToken(service *serviceaccounts.Service, sessionService *sessions.Service, authzService *authorization.Service, tokens accessTokens, scope tenancy.Scope) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		if _, ok := requirePermission(response, request, sessionService, nil, authzService, authorization.PermissionManageServiceAccounts, scope.SiteID, tokens, roleAdmin); !ok {
			return
		}
		var body struct {
			ExpiryDays int `json:"expiry_days"`
		}
		if err := decodeJSON(response, request, &body); err != nil {
			return
		}
		token, err := service.RotateToken(request.Context(), request.PathValue("accountID"), body.ExpiryDays)
		if errors.Is(err, serviceaccounts.ErrInvalidExpiry) {
			http.Error(response, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}
		if err != nil {
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		writeJSON(response, http.StatusCreated, struct {
			Token string `json:"token"`
		}{token})
	}
}

func handleRevokeServiceAccountToken(service *serviceaccounts.Service, sessionService *sessions.Service, authzService *authorization.Service, tokens accessTokens, scope tenancy.Scope) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		if _, ok := requirePermission(response, request, sessionService, nil, authzService, authorization.PermissionManageServiceAccounts, scope.SiteID, tokens, roleAdmin); !ok {
			return
		}
		if err := service.RevokeToken(request.Context(), request.PathValue("tokenID")); err != nil {
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		response.WriteHeader(http.StatusNoContent)
	}
}

func handleDisableServiceAccount(service *serviceaccounts.Service, sessionService *sessions.Service, authzService *authorization.Service, tokens accessTokens, scope tenancy.Scope) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		if _, ok := requirePermission(response, request, sessionService, nil, authzService, authorization.PermissionManageServiceAccounts, scope.SiteID, tokens, roleAdmin); !ok {
			return
		}
		if err := service.DisableAccount(request.Context(), request.PathValue("accountID")); err != nil {
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		response.WriteHeader(http.StatusNoContent)
	}
}
```

**Not:** Yönetim uçları (`handleCreateServiceAccount` ve diğerleri) kasıtlı olarak `requirePermission`'a `nil` servis hesabı geçirir — bir servis hesabının kendi kendini veya kardeş hesapları yönetebilmesi (token rotate/revoke, yeni hesap oluşturma) istenmeyen bir yetki genişlemesi olurdu; yalnız insan oturumu veya eski admin bearer token'ı bu uçlara erişebilir. Servis hesabı token'ları yalnız Görev 4 Adım 4'te güncellenen **operasyonel** rotalarda (jobs/alerts/cloud) kabul edilir.

- [x] **Adım 6: `handlerOptions`, `WithServiceAccounts` ve beş yönetim rotasını `server.go`'ya bağla**

İmport listesine ekle: `"github.com/gokayybaz/bazusop/internal/serviceaccounts"`.

`handlerOptions`'a ekle: `serviceAccountService *serviceaccounts.Service`.

`NewHandler`'da, `authorizationService` bloğundan hemen sonra ekle:

```go
	if configuration.serviceAccountService != nil {
		tokens := accessTokens{operator: configuration.operatorToken, admin: configuration.adminToken}
		registerAudited(mux, "/api/v1/sites/{siteID}/service-accounts", http.MethodPost, "service_accounts", []string{"siteID"}, configuration.auditTrail, configuration.scope, handleCreateServiceAccount(configuration.serviceAccountService, configuration.sessionService, configuration.authorizationService, tokens, configuration.scope))
		registerAudited(mux, "/api/v1/sites/{siteID}/service-accounts", http.MethodGet, "service_accounts", []string{"siteID"}, configuration.auditTrail, configuration.scope, handleListServiceAccounts(configuration.serviceAccountService, configuration.sessionService, configuration.authorizationService, tokens, configuration.scope))
		registerAudited(mux, "/api/v1/service-accounts/{accountID}/rotate", http.MethodPost, "service_accounts", []string{"accountID"}, configuration.auditTrail, configuration.scope, handleRotateServiceAccountToken(configuration.serviceAccountService, configuration.sessionService, configuration.authorizationService, tokens, configuration.scope))
		registerAudited(mux, "/api/v1/service-accounts/tokens/{tokenID}", http.MethodDelete, "service_accounts", []string{"tokenID"}, configuration.auditTrail, configuration.scope, handleRevokeServiceAccountToken(configuration.serviceAccountService, configuration.sessionService, configuration.authorizationService, tokens, configuration.scope))
		registerAudited(mux, "/api/v1/service-accounts/{accountID}", http.MethodDelete, "service_accounts", []string{"accountID"}, configuration.auditTrail, configuration.scope, handleDisableServiceAccount(configuration.serviceAccountService, configuration.sessionService, configuration.authorizationService, tokens, configuration.scope))
	}
```

Görev 4 Adım 3-4'te güncellenen 11 çağrı sitesinin her birine (`handleCreateJob`, `handleCreateAlertRule`, `handleCreateMaintenance`, `handleAcknowledgeIncident`, `handleCreateCloudAccount`, `handleReconcileCloudInstances`, `handleCreateInvite`, `handleAssignSiteRole`, `handleRevokeSiteRole`) `configuration.serviceAccountService` (veya `handleCreateInvite`/`handleAssignSiteRole`/`handleRevokeSiteRole` için — bunların kendi handler'ı zaten `nil` sabit geçiriyor, `server.go`'daki çağrı sitesi değişmez) argümanını `configuration.sessionService`'ten hemen sonra ekle. Örnek:

```go
registerAudited(mux, "/api/v1/instances/{agentID}/jobs", http.MethodPost, "jobs", []string{"agentID"}, configuration.auditTrail, configuration.scope, handleCreateJob(configuration.jobService, configuration.sessionService, configuration.serviceAccountService, configuration.authorizationService, tokens, configuration.scope))
```

(`handleCreateAlertRule`, `handleCreateMaintenance`, `handleAcknowledgeIncident`, `handleCreateCloudAccount`, `handleReconcileCloudInstances` için aynı kalıp; `handleCreateInvite`/`handleAssignSiteRole`/`handleRevokeSiteRole`'un kendi çağrı siteleri değişmeden kalır çünkü bu üçü Görev 4 Adım 3-4'te `nil`'i kendi gövdelerinde sabit geçiriyor.)

- [x] **Adım 7: Build ve tam test paketini çalıştır**

Çalıştır: `go build ./... && go vet ./... && gofmt -l . 2>&1 | grep -v node_modules`
Beklenen: build başarılı; vet/gofmt çıktısı yok

(Bu adımda henüz `internal/server`'ın kendi testleri çalıştırılmaz — Görev 5 bunları ekler ve doğrular.)

- [x] **Adım 8: `config.go` ve `main.go`'ya bağla**

`internal/config/config.go`'da `Config`'e ekle: `ServiceAccountPepper string`. `Load()`'a ekle: `ServiceAccountPepper: os.Getenv("BAZUSOP_SERVICE_ACCOUNT_PEPPER"),`.

`cmd/bazusop-hub/main.go`'nun import listesine ekle: `"github.com/gokayybaz/bazusop/internal/serviceaccounts"`.

`var authorizationStore authorization.Store = authorization.NewMemoryStore()` satırından sonra ekle:

```go
	var serviceAccountStore serviceaccounts.Store = serviceaccounts.NewMemoryStore()
```

Postgres bloğunda, `authorizationStore = postgresStore` satırından sonra ekle:

```go
		serviceAccountStore = postgresStore
```

`authorizationService` inşasından hemen sonra (aynı `if configuration.BootstrapSecret != "" && ...` bloğunun dışında, çünkü servis hesapları ayrıca `BAZUSOP_SERVICE_ACCOUNT_PEPPER`'a da bağlı) ekle:

```go
	var serviceAccountService *serviceaccounts.Service
	if authorizationService != nil && configuration.ServiceAccountPepper != "" {
		serviceAccountService, err = serviceaccounts.NewService(serviceAccountStore, configuration.ServiceAccountPepper)
		if err != nil {
			logger.Error("could not initialize service account service", "error", err)
			os.Exit(1)
		}
	} else if authorizationService != nil {
		logger.Warn("BAZUSOP_SERVICE_ACCOUNT_PEPPER is not set; service account tokens are disabled")
	}
```

`server.NewHandler(...)` çağrısına ekle:

```go
			server.WithServiceAccounts(serviceAccountService),
```

- [x] **Adım 9: Build'i tekrar çalıştır**

Çalıştır: `go build ./... && go vet ./... && gofmt -l . 2>&1 | grep -v node_modules`
Beklenen: temiz

- [x] **Adım 10: Commit**

```bash
git add internal/server/serviceaccounts.go internal/server/rbac.go internal/server/middleware.go internal/server/jobs.go internal/server/alerting.go internal/server/cloudinventory.go internal/server/identity.go internal/server/server.go internal/audittrail/audittrail.go internal/config/config.go cmd/bazusop-hub/main.go
git commit -m "feat: wire service account bearer authentication and management endpoints"
```

## Görev 5: Uçtan uca kanıt — servis hesabı token'ı gerçek bir operasyonel rotayı kimlik doğruluyor

**Dosyalar:**
- Oluştur: `internal/server/serviceaccounts_test.go`

**Arayüzler:**
- Tüketir: Görev 4'ün tüm HTTP bağlantısı.

- [x] **Adım 1: Başarısız testi yaz**

```go
// internal/server/serviceaccounts_test.go
package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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

func newServiceAccountHandler(t *testing.T, adminToken string) (http.Handler, []*http.Cookie) {
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
	registry := inventory.NewService(inventory.NewMemoryStore())
	agent := tenancy.Agent{ID: "edge-01", OrganizationID: tenancy.DefaultOrganizationID, SiteID: tenancy.DefaultSiteID}
	if err := registry.Report(t.Context(), agent, inventory.Facts{Hostname: "edge-01", OSFamily: "linux", OSName: "Ubuntu", OSVersion: "24.04", Architecture: "amd64", CPUCores: 2, MemoryBytes: 1024, IPAddresses: []string{"10.0.0.1"}, AgentVersion: "test"}); err != nil {
		t.Fatal(err)
	}
	jobService, err := jobs.NewService(jobs.NewMemoryStore(), jobs.WithHostScopeChecker(registry.HasHost))
	if err != nil {
		t.Fatal(err)
	}
	handler := server.NewHandler(
		server.WithDefaultScope(tenancy.DefaultScope()),
		server.WithIdentity(identityService, "bootstrap-secret"),
		server.WithSessions(sessionService, identityService),
		server.WithAuthorization(authzService),
		server.WithServiceAccounts(serviceAccountService),
		server.WithJobs(jobService, "operator-token"),
		server.WithAdminToken(adminToken),
		server.WithAuditTrail(audittrail.NewService(audittrail.NewMemoryStore())),
	)

	admin, _, err := identityService.Bootstrap(t.Context(), tenancy.DefaultOrganizationID, "admin@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	_, sessionToken, _, err := sessionService.Create(t.Context(), admin.ID, admin.OrganizationID)
	if err != nil {
		t.Fatal(err)
	}
	return handler, []*http.Cookie{{Name: "bazusop_session", Value: sessionToken}}
}

func TestServiceAccountTokenAuthenticatesAndCreatesAJob(t *testing.T) {
	t.Parallel()
	handler, adminCookies := newServiceAccountHandler(t, "admin-token")

	createRequest := httptest.NewRequest(http.MethodPost, "/api/v1/sites/site_default/service-accounts", encodeJSON(t, map[string]any{
		"name": "ci-bot", "role": "operator",
	}))
	for _, cookie := range adminCookies {
		createRequest.AddCookie(cookie)
	}
	createResponse := httptest.NewRecorder()
	handler.ServeHTTP(createResponse, createRequest)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("expected 201 from service account creation, got %d: %s", createResponse.Code, createResponse.Body.String())
	}
	var created struct {
		ID    string `json:"id"`
		Token string `json:"token"`
	}
	if err := json.NewDecoder(createResponse.Body).Decode(&created); err != nil || created.Token == "" {
		t.Fatalf("expected a one-time token, got %v", err)
	}

	jobRequest := httptest.NewRequest(http.MethodPost, "/api/v1/instances/edge-01/jobs", encodeJSON(t, map[string]string{
		"action": "service.restart", "target": "nginx.service", "approved_by": "ci-bot", "reason": "automated rollout",
	}))
	jobRequest.Header.Set("Authorization", "Bearer "+created.Token)
	jobResponse := httptest.NewRecorder()
	handler.ServeHTTP(jobResponse, jobRequest)
	if jobResponse.Code != http.StatusCreated {
		t.Fatalf("expected the service account token to authenticate a real job creation, got %d: %s", jobResponse.Code, jobResponse.Body.String())
	}
}

func TestRevokedServiceAccountTokenIsRejected(t *testing.T) {
	t.Parallel()
	handler, adminCookies := newServiceAccountHandler(t, "admin-token")

	createRequest := httptest.NewRequest(http.MethodPost, "/api/v1/sites/site_default/service-accounts", encodeJSON(t, map[string]any{
		"name": "ci-bot", "role": "operator",
	}))
	for _, cookie := range adminCookies {
		createRequest.AddCookie(cookie)
	}
	createResponse := httptest.NewRecorder()
	handler.ServeHTTP(createResponse, createRequest)
	var created struct {
		ID    string `json:"id"`
		Token string `json:"token"`
	}
	_ = json.NewDecoder(createResponse.Body).Decode(&created)
	tokenID, _, ok := serviceaccounts.ParseToken(created.Token)
	if !ok {
		t.Fatal("expected a parseable token")
	}

	revokeRequest := httptest.NewRequest(http.MethodDelete, "/api/v1/service-accounts/tokens/"+tokenID, nil)
	for _, cookie := range adminCookies {
		revokeRequest.AddCookie(cookie)
	}
	revokeResponse := httptest.NewRecorder()
	handler.ServeHTTP(revokeResponse, revokeRequest)
	if revokeResponse.Code != http.StatusNoContent {
		t.Fatalf("expected 204 from token revocation, got %d: %s", revokeResponse.Code, revokeResponse.Body.String())
	}

	jobRequest := httptest.NewRequest(http.MethodPost, "/api/v1/instances/edge-01/jobs", encodeJSON(t, map[string]string{
		"action": "service.restart", "target": "nginx.service", "approved_by": "ci-bot", "reason": "automated rollout",
	}))
	jobRequest.Header.Set("Authorization", "Bearer "+created.Token)
	jobResponse := httptest.NewRecorder()
	handler.ServeHTTP(jobResponse, jobRequest)
	if jobResponse.Code != http.StatusUnauthorized {
		t.Fatalf("expected the revoked token to be rejected, got %d", jobResponse.Code)
	}
}

func TestServiceAccountTokenCannotManageOtherServiceAccounts(t *testing.T) {
	t.Parallel()
	handler, adminCookies := newServiceAccountHandler(t, "admin-token")

	createRequest := httptest.NewRequest(http.MethodPost, "/api/v1/sites/site_default/service-accounts", encodeJSON(t, map[string]any{
		"name": "ci-bot", "role": "site-admin",
	}))
	for _, cookie := range adminCookies {
		createRequest.AddCookie(cookie)
	}
	createResponse := httptest.NewRecorder()
	handler.ServeHTTP(createResponse, createRequest)
	var created struct {
		Token string `json:"token"`
	}
	_ = json.NewDecoder(createResponse.Body).Decode(&created)

	escalationRequest := httptest.NewRequest(http.MethodPost, "/api/v1/sites/site_default/service-accounts", encodeJSON(t, map[string]any{
		"name": "second-bot", "role": "viewer",
	}))
	escalationRequest.Header.Set("Authorization", "Bearer "+created.Token)
	escalationResponse := httptest.NewRecorder()
	handler.ServeHTTP(escalationResponse, escalationRequest)
	if escalationResponse.Code != http.StatusUnauthorized {
		t.Fatalf("expected a service account token to be unable to manage service accounts, got %d: %s", escalationResponse.Code, escalationResponse.Body.String())
	}
}

var _ = time.Now
```

**Yazarken düzeltme — dosyanın sonundaki `var _ = time.Now` bir taslak artığıdır:** hiçbir testte `time` paketi doğrudan kullanılmıyor. Bu satırı ve `"time"` import'unu **tamamen sil**.

- [x] **Adım 2: Testin başarısız olduğunu doğrula**

Çalıştır: `GOCACHE=/tmp/bazusop-go-cache go test ./internal/server/... -run 'TestServiceAccountToken|TestRevokedServiceAccountToken' -v 2>&1 | tail -30`
Beklenen: BAŞARISIZ (Görev 4 tamamlanmış olsa bile, `"time"` importunun kullanılmaması derleme hatası verir — düzeltmeyi uygula, sonra tekrar çalıştır: gerçek bir davranış hatası beklenmiyor, çünkü Görev 4 zaten HTTP bağlantısını tamamladı; bu adım asıl olarak testin gerçekten testi ettiğini doğrular)

- [x] **Adım 3: Testleri çalıştırıp geçtiğini doğrula**

Düzeltmeyi uyguladıktan sonra çalıştır: `GOCACHE=/tmp/bazusop-go-cache go test ./internal/server/... -run 'TestServiceAccountToken|TestRevokedServiceAccountToken' -v`
Beklenen: BAŞARILI (3 test)

- [x] **Adım 4: Tam server paket testini çalıştır**

Çalıştır: `GOCACHE=/tmp/bazusop-go-cache go test ./internal/server/... -v 2>&1 | tail -200`
Beklenen: tüm testler (yeni ve eski, spike 11.4/11.5'in testleri dahil) BAŞARILI

- [x] **Adım 5: `go vet` ve `gofmt` çalıştır**

Çalıştır: `go vet ./internal/server/... && gofmt -l internal/server/*.go`
Beklenen: çıktı yok

- [x] **Adım 6: Commit**

```bash
git add internal/server/serviceaccounts_test.go
git commit -m "test: prove service account tokens authenticate real operational routes end to end"
```

## Görev 6: Canlı hub'a karşı manuel doğrulama

**Dosyalar:** yok (yalnız doğrulama).

- [x] **Adım 1: Yeni secret'larla yerel bir hub ayağa kaldır**

Çalıştır: `POSTGRES_PASSWORD=verify-pw BAZUSOP_ENROLLMENT_TOKEN=verify-token BAZUSOP_OPERATOR_TOKEN=verify-operator BAZUSOP_ADMIN_TOKEN=verify-admin BAZUSOP_BOOTSTRAP_SECRET=verify-bootstrap BAZUSOP_TOTP_ENCRYPTION_KEY=verify-totp-key BAZUSOP_SERVICE_ACCOUNT_PEPPER=verify-pepper BAZUSOP_PORT=18095 docker compose -p bazusop-verify-sat up --build -d` ve hub konteynerinin healthy olmasını bekle.

- [x] **Adım 2: Bootstrap ol, recovery code al, giriş yap**

```bash
curl -s -c /tmp/bazusop-sat-cookies.txt -X POST http://127.0.0.1:18095/api/v1/bootstrap \
  -d '{"secret":"verify-bootstrap","email":"admin@example.com","password":"correct horse battery staple"}'
curl -s -c /tmp/bazusop-sat-cookies.txt -X POST http://127.0.0.1:18095/api/v1/sessions \
  -d '{"email":"admin@example.com","password":"correct horse battery staple","recovery_code":"<yukarıdaki ilk recovery_code>"}'
```

Beklenen: bootstrap `201`; login `201`.

- [x] **Adım 3: Bir servis hesabı oluştur**

```bash
curl -s -b /tmp/bazusop-sat-cookies.txt -X POST http://127.0.0.1:18095/api/v1/sites/site_default/service-accounts \
  -d '{"name":"ci-bot","role":"operator"}'
```

Beklenen: `201`, yanıt gövdesinde `bazusop_sat_` ile başlayan bir `token`.

- [x] **Adım 4: Token'ın gerçek bir operasyonel rotayı kimlik doğruladığını doğrula**

```bash
curl -s -o /dev/null -w "status:%{http_code}\n" -X POST http://127.0.0.1:18095/api/v1/instances/edge-01/jobs \
  -H "Authorization: Bearer <adım 3'teki token>" \
  -d '{"action":"service.restart","target":"nginx.service","approved_by":"ci-bot","reason":"verify"}'
```

Beklenen: `404` (`edge-01` bu doğrulama hub'ında kayıtlı değil — bu, kimlik doğrulamanın/yetkilendirmenin geçtiğini, yalnız kaynağın var olmadığını gösterir; `401`/`403` DEĞİL `404` alınması token'ın kabul edildiğinin kanıtıdır).

- [x] **Adım 5: Token'ı rotate et, eski token'ın artık çalışmadığını doğrula**

```bash
ACCOUNT_ID=<adım 3 yanıtındaki id>
curl -s -b /tmp/bazusop-sat-cookies.txt -X POST http://127.0.0.1:18095/api/v1/service-accounts/$ACCOUNT_ID/rotate -d '{}'
curl -s -o /dev/null -w "eski token (beklenen 401): %{http_code}\n" -X POST http://127.0.0.1:18095/api/v1/instances/edge-01/jobs \
  -H "Authorization: Bearer <adım 3'teki eski token>" \
  -d '{"action":"service.restart","target":"nginx.service","approved_by":"ci-bot","reason":"verify"}'
```

Beklenen: rotate `201` (yeni token); eski token ile istek `401`.

- [x] **Adım 6: Denetim izini sorgula**

```bash
docker compose -p bazusop-verify-sat exec postgres psql -U bazusop -d bazusop -c \
  "SELECT action, resource_type, outcome, error_code FROM audit_events WHERE resource_type IN ('service_accounts','jobs') ORDER BY occurred_at;"
```

Beklenen: hesap oluşturma, rotasyon ve iki job isteğinin (eski/yeni token) her biri için birer satır.

- [x] **Adım 7: Kapat ve geçici dosyaları temizle**

Çalıştır: `docker compose -p bazusop-verify-sat down -v && rm -f /tmp/bazusop-sat-cookies.txt`

## Kendi Kendine İnceleme

**1. Spec kapsaması.**
- Tam olarak bir siteye bağlı, yalnız site-admin/operator/viewer, platform-admin olamaz, web oturumu açamaz → Görev 1 (`ErrInvalidRole`), Görev 4 (servis hesabı yolu yalnız `requirePermission`'ın operasyonel-rota dalında, oturum-tabanlı hiçbir uca asla erişemez — `sessionService.Validate` servis hesabı token'larını hiç tanımaz).
- Yüksek entropili secret + hassas olmayan kararlı token kimliği, HMAC-SHA-256 + harici pepper, sabit zamanlı karşılaştırma → Görev 1 (`hashSecret`, `subtle.ConstantTimeCompare`, `TokenPrefix`/`ParseToken`).
- Yalnız oluşturma/rotasyonda bir kez gösterim → `CreateAccount`/`RotateToken` yalnız o anki dönüş değerinde token taşır; `Token`/`ServiceAccount` struct'ları hiçbir zaman ham secret alanı taşımaz.
- Sürümlü, sabit-zamanlı-karşılaştırmaya-uygun hash → `pepperKeyVersion` öneki (spike 11.3'ün `EncryptTOTPSecret`'ıyla aynı desen).
- Varsayılan 90 gün, üst sınır 365 gün → `defaultExpiryDays`/`maxExpiryDays`, test edildi.
- Son kullanım zamanı/kaynak IP güncellemesi → `Touch`, `Validate` içinde her başarılı doğrulamada çağrılır.
- Rotasyonla yenilenebilir → `RotateToken`, test edildi (eski token iptal olur).
- Token veya hesap anında iptal → `RevokeToken` (yalnız token) ve `DisableAccount` (hesap + tüm aktif token'lar), ayrı test edildi.
- Her kullanımda actor/token kimliği/site/sonuç audit → `deriveActor`'ın `ActorServiceAccount` dalı + zaten var olan `registerAudited` middleware'i, Görev 6'da canlı doğrulandı.
- Eski operator/admin token desteğinin kaldırılması → **bu spike'ın kapsamı dışında**, açıkça spike 11.7'ye bırakıldı (yol haritasında zaten ayrı bir spike).
- Agent mTLS kimliği servis hesabına dönüşmez → dokunulmadı; `authenticateAgent`/agent rotaları bu spike'ta hiç değişmedi, servis hesabı yolu yalnız `requirePermission` zincirine (insan/otomasyon rotaları) eklendi.

**2. Placeholder taraması.** Her adımda gerçek kod var. İki bilinçli "önce yanlış yaz, sonra düzelt" dizisi var: Görev 3'te Postgres `RevokeToken`'ın `MemoryStore` ile tutarsız `RowsAffected` kontrolü, Görev 5'te test dosyasının kullanılmayan `"time"` importu. Her biri açık bir **düzeltme** notuyla ve tam yerine geçecek kodla birlikte veriliyor.

**3. Tip tutarlılığı.** `serviceaccounts.ServiceAccount`/`Token`/`TokenPrefix`/`ParseToken` Görev 1'de tanımlandığı gibi Görev 3-5 boyunca birebir aynı kullanılıyor. `requirePermission`'ın genişletilmiş imzası (`serviceAccountService *serviceaccounts.Service` eklenmiş hali) Görev 4'ün tüm 11 çağrı sitesinde ve Görev 5'in `handleCreateServiceAccount` vb. çağrılarında tutarlı. `authorization.PermissionAllowsRole` Görev 2'de tanımlandığı gibi Görev 4'ün `requirePermission`'ında doğrudan (servis hesabı yolu için, `authzService.Can`'in aksine — servis hesabının platform-admin bypass'ı yok) kullanılıyor.
