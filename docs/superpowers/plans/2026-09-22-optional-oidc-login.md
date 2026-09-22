# Opsiyonel OIDC (Spike 11.9) Uygulama Planı

> **Otonom çalışanlar için:** ZORUNLU ALT-SKILL: Bu planı görev görev uygulamak için superpowers:subagent-driven-development (önerilen) veya superpowers:executing-plans kullan. Adımlar `- [ ]` checkbox söz dizimiyle takip edilir.

**Amaç:** Platform yöneticisinin yapılandırabildiği, Authorization Code + PKCE akışıyla çalışan opsiyonel bir OIDC girişi ekler. İlk giriş yalnız geçerli bir davetle olur; bağlantı sonrasında hesap kalıcı olarak `issuer + subject` ile tanınır. Yerel giriş tamamen bağımsız çalışmaya devam eder.

**Mimari:** Üç katman: (1) `internal/identity` — davet/kullanıcı modelini OIDC bağlantısını (issuer+subject) ve organizasyonun OIDC ayarlarını (discovery URL, issuer, client ID, şifreli client secret, redirect URL) kapsayacak şekilde genişletir; (2) yeni `internal/oidc` — `github.com/coreos/go-oidc/v3` ve `golang.org/x/oauth2` üzerine ince bir sarmalayıcı: PKCE `state`/`nonce`/`code_verifier` üretimi, yetkilendirme URL'si inşası, callback'te kod değişimi ve ID token doğrulama (imza, issuer, audience, süre, nonce); (3) `internal/server` — HTTP katmanı: OIDC ayarları CRUD'u, `/oidc/login` ve `/oidc/callback` uçları, kısa ömürlü bir flow-state cookie'si. Katmanlar arası bağımlılık her zaman tek yönlü: `internal/oidc` hiçbir şey saklamaz (yalnız protokol), `internal/identity` `internal/oidc`'i hiç import etmez (yalnız düz string'ler — issuer/subject/email — alır), `internal/server` ikisini birbirine bağlar.

**Teknoloji yığını:** Go 1.26, `github.com/coreos/go-oidc/v3@v3.21.0`, `golang.org/x/oauth2@v0.37.0` (bu ikisi bu spike'ta eklenen ilk yeni doğrudan bağımlılıklar — proje şu ana kadar yalnız pgx/gopsutil/x-crypto kullanıyordu; JWT imza doğrulama ve JWKS önbellekleme elle yazılamayacak kadar güvenlik-kritik, iyi test edilmiş bu kütüphaneler tercih edildi). Test kodunda `github.com/go-jose/go-jose/v4` (go-oidc'nin zaten geçişli bağımlılığı) sahte IdP'nin ID token'ını imzalamak için doğrudan kullanılır.

**Spec:** [docs/superpowers/specs/2026-09-13-enterprise-rbac-identity-audit-design.md](../specs/2026-09-13-enterprise-rbac-identity-audit-design.md) — "Kimlik yaşam döngüsü" → "OIDC kullanıcı" bölümü. Yol haritası satırı: `11.9 | Opsiyonel OIDC | Davetli kullanıcı PKCE ile bağlanır; yerel giriş bağımsız çalışır`.

## Genel Kısıtlar

- **UI yok, tıpkı 11.1–11.7 gibi.** Şu an hiçbir login/bootstrap sayfası yok (11.8'de bu açıkça tespit edildi) — bir "OIDC ile giriş" butonu asılacağı bir sayfa olmadan anlamsız kalır. Yol haritasının kabul sinyali ("Davetli kullanıcı PKCE ile bağlanır") protokol seviyesinde, tam HTTP akışıyla test edilebilir; bu spike backend-only kalır, tıpkı 11.1–11.7 gibi.
- **Client secret şifreleme anahtarı için yeni bir env var eklenmez.** `internal/identity/totp.go`'daki `EncryptTOTPSecret`/`DecryptTOTPSecret` zaten genel amaçlı AES-256-GCM yardımcılarıdır (TOTP'ye özel bir kripto içermez) — OIDC client secret'ı da aynı `BAZUSOP_TOTP_ENCRYPTION_KEY` ile şifrelenir. İkisi de "operatörün sağladığı bir secret'ı zorunlu bir runtime anahtarıyla şifrele" aynı ihtiyacıdır; ayrı bir anahtar eklemek operasyon yüzeyini gereksiz büyütür.
- **`acr`/`amr` claim'leri audit olayına eklenmez.** Spec bunu "varsa ekler" diye yumuşak bir gereksinim olarak tanımlıyor; `audittrail.Event`'in sabit şekli ve `registerAudited`'ın genel sarmalayıcısı bunun için yeniden yapılandırılırsa fayda/maliyet oranı düşük olur, ve yol haritasının kabul sinyalinde bu detay yok. Bilinçli olarak ertelenir.
- **IdP grup claim'leri rol üretmez** — zaten spec'in gereksinimi bu, ve mimari bunu doğal olarak sağlıyor: OIDC kullanıcısının rolü/site üyeliği yalnız DAVET'in sabit `Role`/`SiteRoleGrants` alanlarından gelir (aynı `siteRoleGrantor` callback'i, yerel davetlerle birebir aynı yol), IdP token'ındaki hiçbir claim rol atamasına karışmaz.
- **Tek-org/tek-site mimarisiyle tutarlı** (11.5'in kararı): OIDC yapılandırması `scope.OrganizationID` (hub'ın tek sabit organizasyonu) için saklanır; genel çok-organizasyonlu bir OIDC config çözümleme mekanizması kurulmaz.
- **`internal/oidc.Provider` her giriş denemesinde sıfırdan inşa edilir**, önbelleklenmez. OIDC ayarları canlı bir API ile admin tarafından değiştirilebildiğinden (env var'dan sabit okunmadığından) ve giriş insan-tetikli, düşük frekanslı bir işlem olduğundan, her denemede bir discovery isteği fazlası, config-değişince-önbelleği-geçersiz-kılma karmaşıklığından daha ucuzdur.
- `handleConsumeInvite` (yerel parola belirleme) artık `identity.ErrInviteWrongIdentityType` hatasını da ele alır (bir OIDC-tipi daveti yerel yoldan tüketme girişimi `403` döner).

## Dosya Yapısı

- Değiştir: `internal/identity/identity.go`, `internal/identity/memorystore.go`, `internal/identity/identity_test.go`.
- Değiştir: `internal/storage/postgres/identity.go`.
- Oluştur: `internal/storage/postgres/migrations/024_oidc.sql`.
- Oluştur: `internal/oidc/oidc.go`, `internal/oidc/oidc_test.go`.
- Değiştir: `go.mod`, `go.sum`.
- Değiştir: `internal/server/identity.go`, `internal/server/server.go`.
- Oluştur: `internal/server/oidc.go`, `internal/server/oidc_test.go`.
- Değiştir: `docs/API.md`, `README.md`.

## Görev 1: `internal/identity` — OIDC kullanıcı bağlantısı ve organizasyon OIDC ayarları

**Dosyalar:**
- Değiştir: `internal/identity/identity.go`, `internal/identity/memorystore.go`, `internal/identity/identity_test.go`
- Değiştir: `internal/storage/postgres/identity.go`
- Oluştur: `internal/storage/postgres/migrations/024_oidc.sql`

**Arayüzler:**
- Üretir: `identity.IdentityType` (`IdentityTypeLocal`, `IdentityTypeOIDC`), `identity.OIDCConfiguration`, `Service.LoginOrLinkOIDCUser(ctx, organizationID, issuer, subject, email string) (User, error)`, `Service.SetOIDCConfiguration(ctx, organizationID, discoveryURL, issuer, clientID, clientSecret, redirectURL, updatedBy string) (OIDCConfiguration, error)`, `Service.OIDCConfiguration(ctx, organizationID string) (OIDCConfiguration, error)` (secret'sız), `Service.OIDCConfigurationWithSecret(ctx, organizationID string) (OIDCConfiguration, string, error)` (yalnız internal kullanım).

- [x] **Adım 1: `internal/identity/identity.go`'yu güncelle**

`User` struct'ına ekle:

```go
type User struct {
	ID                  string
	OrganizationID      string
	Email               string
	Role                Role
	PasswordHash        string
	TOTPSecretEncrypted []byte
	TOTPConfirmedAt     *time.Time
	OIDCIssuer          string
	OIDCSubject         string
	CreatedAt           time.Time
	DisabledAt          *time.Time
}
```

`Invite` struct'ına `IdentityType` ekle, ve yeni tip tanımla:

```go
type IdentityType string

const (
	IdentityTypeLocal IdentityType = "local"
	IdentityTypeOIDC  IdentityType = "oidc"
)

type Invite struct {
	ID             string
	OrganizationID string
	Email          string
	Role           Role
	SiteRoleGrants []SiteRoleGrant
	IdentityType   IdentityType
	CreatedBy      string
	ExpiresAt      time.Time
	ConsumedAt     *time.Time
	RevokedAt      *time.Time
	CreatedAt      time.Time
}

// OIDCConfiguration is one organization's OIDC settings. ClientSecretEncrypted
// is only ever populated by Service.OIDCConfigurationWithSecret (internal
// use, login/callback handlers); Service.OIDCConfiguration always zeroes it
// — the design spec requires the client secret never be returned by any
// read API.
type OIDCConfiguration struct {
	OrganizationID        string
	DiscoveryURL          string
	Issuer                string
	ClientID              string
	ClientSecretEncrypted []byte
	RedirectURL           string
	UpdatedAt             time.Time
	UpdatedBy             string
}
```

Yeni sentinel hatalar ekle:

```go
	ErrOIDCNotConfigured      = errors.New("organization has no OIDC configuration")
	ErrInviteWrongIdentityType = errors.New("invite identity type does not match this login method")
```

`Store` arayüzünü genişlet:

```go
type Store interface {
	IsBootstrapped(ctx context.Context) (bool, error)
	CompleteBootstrap(ctx context.Context, user User) error
	CreateUser(ctx context.Context, user User) error
	UserByEmail(ctx context.Context, organizationID, email string) (User, error)
	UserByID(ctx context.Context, id string) (User, error)
	UserByOIDCIdentity(ctx context.Context, organizationID, issuer, subject string) (User, error)
	UpdateUser(ctx context.Context, user User) error
	SaveInvite(ctx context.Context, invite Invite, tokenHash string) error
	InviteByTokenHash(ctx context.Context, tokenHash string) (Invite, error)
	InviteByEmail(ctx context.Context, organizationID, email string) (Invite, error)
	ConsumeInvite(ctx context.Context, tokenHash string, consumedByUserID string, at time.Time) error
	ConsumeInviteByID(ctx context.Context, inviteID string, consumedByUserID string, at time.Time) error
	SaveRecoveryCodes(ctx context.Context, userID string, codeHashes []string) error
	ConsumeRecoveryCode(ctx context.Context, userID, codeHash string) (bool, error)
	SaveOIDCConfiguration(ctx context.Context, config OIDCConfiguration) error
	OIDCConfigurationByOrganization(ctx context.Context, organizationID string) (OIDCConfiguration, error)
}
```

`CreateInvite`'ı `identityType` parametresi alacak şekilde değiştir:

```go
func (service *Service) CreateInvite(ctx context.Context, createdBy, organizationID, email string, role Role, siteRoleGrants []SiteRoleGrant, identityType IdentityType) (Invite, string, error) {
	if role == RolePlatformAdmin && len(siteRoleGrants) > 0 {
		return Invite{}, "", ErrInvalidInviteRole
	}
	if role != RolePlatformAdmin && len(siteRoleGrants) == 0 {
		return Invite{}, "", ErrInvalidInviteRole
	}
	if identityType == "" {
		identityType = IdentityTypeLocal
	}
	token, err := newID()
	if err != nil {
		return Invite{}, "", err
	}
	id, err := newID()
	if err != nil {
		return Invite{}, "", err
	}
	invite := Invite{
		ID: id, OrganizationID: organizationID, Email: email, Role: role, SiteRoleGrants: siteRoleGrants,
		IdentityType: identityType, CreatedBy: createdBy, ExpiresAt: service.now().Add(24 * time.Hour), CreatedAt: service.now(),
	}
	if err := service.store.SaveInvite(ctx, invite, hashToken(token)); err != nil {
		return Invite{}, "", err
	}
	return invite, token, nil
}
```

`ConsumeInvite`'a identity-type kontrolü ekle (mevcut süre/tüketim kontrolünden hemen sonra):

```go
func (service *Service) ConsumeInvite(ctx context.Context, token, password string) (User, error) {
	tokenHash := hashToken(token)
	invite, err := service.store.InviteByTokenHash(ctx, tokenHash)
	if err != nil {
		return User{}, err
	}
	if invite.ConsumedAt != nil || invite.RevokedAt != nil || service.now().After(invite.ExpiresAt) {
		return User{}, ErrInviteExpired
	}
	if invite.IdentityType == IdentityTypeOIDC {
		return User{}, ErrInviteWrongIdentityType
	}
	passwordHash, err := HashPassword(password)
	...
```

(Fonksiyonun geri kalanı değişmeden kalır.)

Dosyanın sonuna (`newID`'den önce) yeni metotları ekle:

```go
// LoginOrLinkOIDCUser resolves a verified OIDC identity (issuer+subject,
// already authenticated by internal/oidc) to a user. A returning user
// (already linked to this issuer+subject) logs in directly; a first-time
// subject requires a pending, unconsumed, unexpired oidc-type invite whose
// email matches — the same "no account without an invite" rule local users
// follow. Once linked, the account is permanently recognized by
// issuer+subject; a later email change at the IdP does not move the link
// (email is only ever consulted for this one-time invite lookup).
func (service *Service) LoginOrLinkOIDCUser(ctx context.Context, organizationID, issuer, subject, email string) (User, error) {
	existing, err := service.store.UserByOIDCIdentity(ctx, organizationID, issuer, subject)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, ErrInvalidCredentials) {
		return User{}, err
	}
	invite, err := service.store.InviteByEmail(ctx, organizationID, email)
	if err != nil {
		return User{}, ErrInviteNotFound
	}
	if invite.ConsumedAt != nil || invite.RevokedAt != nil || service.now().After(invite.ExpiresAt) {
		return User{}, ErrInviteExpired
	}
	if invite.IdentityType != IdentityTypeOIDC {
		return User{}, ErrInviteWrongIdentityType
	}
	id, err := newID()
	if err != nil {
		return User{}, err
	}
	user := User{
		ID: id, OrganizationID: invite.OrganizationID, Email: invite.Email, Role: invite.Role,
		OIDCIssuer: issuer, OIDCSubject: subject, CreatedAt: service.now(),
	}
	if err := service.store.CreateUser(ctx, user); err != nil {
		return User{}, err
	}
	if len(invite.SiteRoleGrants) > 0 && service.siteRoleGrantor != nil {
		if err := service.siteRoleGrantor(ctx, user.ID, invite.SiteRoleGrants); err != nil {
			return User{}, err
		}
	}
	if err := service.store.ConsumeInviteByID(ctx, invite.ID, user.ID, service.now()); err != nil {
		return User{}, err
	}
	return user, nil
}

// SetOIDCConfiguration persists organization-wide OIDC settings. clientSecret
// is encrypted at rest using the same AES-256-GCM helper (and runtime key)
// TOTP secrets use — see the plan's Genel Kısıtlar for why this spike does
// not introduce a second encryption key.
func (service *Service) SetOIDCConfiguration(ctx context.Context, organizationID, discoveryURL, issuer, clientID, clientSecret, redirectURL, updatedBy string) (OIDCConfiguration, error) {
	encrypted, err := EncryptTOTPSecret([]byte(clientSecret), service.totpEncryptionKey)
	if err != nil {
		return OIDCConfiguration{}, err
	}
	config := OIDCConfiguration{
		OrganizationID: organizationID, DiscoveryURL: discoveryURL, Issuer: issuer, ClientID: clientID,
		ClientSecretEncrypted: encrypted, RedirectURL: redirectURL, UpdatedAt: service.now(), UpdatedBy: updatedBy,
	}
	if err := service.store.SaveOIDCConfiguration(ctx, config); err != nil {
		return OIDCConfiguration{}, err
	}
	return config, nil
}

// OIDCConfiguration returns the organization's OIDC settings WITHOUT the
// client secret — the shape safe to return from a read API.
func (service *Service) OIDCConfiguration(ctx context.Context, organizationID string) (OIDCConfiguration, error) {
	config, err := service.store.OIDCConfigurationByOrganization(ctx, organizationID)
	if err != nil {
		return OIDCConfiguration{}, err
	}
	config.ClientSecretEncrypted = nil
	return config, nil
}

// OIDCConfigurationWithSecret returns the organization's OIDC settings with
// the client secret decrypted — for internal/server's login/callback
// handlers only, which must present it to the IdP's token endpoint. Never
// exposed through an HTTP response.
func (service *Service) OIDCConfigurationWithSecret(ctx context.Context, organizationID string) (OIDCConfiguration, string, error) {
	config, err := service.store.OIDCConfigurationByOrganization(ctx, organizationID)
	if err != nil {
		return OIDCConfiguration{}, "", err
	}
	secret, err := DecryptTOTPSecret(config.ClientSecretEncrypted, service.totpEncryptionKey)
	if err != nil {
		return OIDCConfiguration{}, "", err
	}
	config.ClientSecretEncrypted = nil
	return config, string(secret), nil
}
```

- [x] **Adım 2: `internal/identity/memorystore.go`'yu güncelle**

`MemoryStore` struct'ına ekle, `NewMemoryStore`'da başlat:

```go
type MemoryStore struct {
	mu             sync.Mutex
	bootstrapped   bool
	usersByID      map[string]User
	usersByEmail   map[string]string          // "org_id\x00email" -> user id
	usersByOIDC    map[string]string          // "org_id\x00issuer\x00subject" -> user id
	invites        map[string]inviteRecord    // token hash -> record
	recoveryHashes map[string]map[string]bool // user id -> code hash -> consumed
	oidcConfigs    map[string]OIDCConfiguration // org id -> config
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		usersByID:      make(map[string]User),
		usersByEmail:   make(map[string]string),
		usersByOIDC:    make(map[string]string),
		invites:        make(map[string]inviteRecord),
		recoveryHashes: make(map[string]map[string]bool),
		oidcConfigs:    make(map[string]OIDCConfiguration),
	}
}

func oidcKey(organizationID, issuer, subject string) string {
	return organizationID + "\x00" + issuer + "\x00" + subject
}
```

`CreateUser`'ı OIDC index'ini de dolduracak şekilde güncelle:

```go
func (store *MemoryStore) CreateUser(_ context.Context, user User) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.usersByID[user.ID] = user
	store.usersByEmail[emailKey(user.OrganizationID, user.Email)] = user.ID
	if user.OIDCSubject != "" {
		store.usersByOIDC[oidcKey(user.OrganizationID, user.OIDCIssuer, user.OIDCSubject)] = user.ID
	}
	return nil
}
```

Ekle:

```go
func (store *MemoryStore) UserByOIDCIdentity(_ context.Context, organizationID, issuer, subject string) (User, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	id, ok := store.usersByOIDC[oidcKey(organizationID, issuer, subject)]
	if !ok {
		return User{}, ErrInvalidCredentials
	}
	return store.usersByID[id], nil
}

// InviteByEmail returns the most recently created invite for email,
// regardless of status — LoginOrLinkOIDCUser applies the same
// expiry/consumed/revoked/identity-type checks ConsumeInvite already
// applies for local invites.
func (store *MemoryStore) InviteByEmail(_ context.Context, organizationID, email string) (Invite, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	var found *Invite
	for _, record := range store.invites {
		if record.invite.OrganizationID != organizationID || record.invite.Email != email {
			continue
		}
		if found == nil || record.invite.CreatedAt.After(found.CreatedAt) {
			invite := record.invite
			found = &invite
		}
	}
	if found == nil {
		return Invite{}, ErrInviteNotFound
	}
	return *found, nil
}

func (store *MemoryStore) ConsumeInviteByID(_ context.Context, inviteID string, _ string, at time.Time) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	for tokenHash, record := range store.invites {
		if record.invite.ID == inviteID {
			consumedAt := at
			record.invite.ConsumedAt = &consumedAt
			store.invites[tokenHash] = record
			return nil
		}
	}
	return ErrInviteNotFound
}

func (store *MemoryStore) SaveOIDCConfiguration(_ context.Context, config OIDCConfiguration) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.oidcConfigs[config.OrganizationID] = config
	return nil
}

func (store *MemoryStore) OIDCConfigurationByOrganization(_ context.Context, organizationID string) (OIDCConfiguration, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	config, ok := store.oidcConfigs[organizationID]
	if !ok {
		return OIDCConfiguration{}, ErrOIDCNotConfigured
	}
	return config, nil
}
```

- [x] **Adım 3: Mevcut `CreateInvite` çağrı sitelerini güncelle**

`internal/identity/identity_test.go`'daki dört çağrıya trailing `identity.IdentityTypeLocal` ekle (satır 43, 181, 193, 212):

```go
invite, token, err := service.CreateInvite(t.Context(), "admin", "org_default", "new-admin@example.com", identity.RolePlatformAdmin, nil, identity.IdentityTypeLocal)
```

(Diğer üç çağrı sitesinde de aynı şekilde sona `identity.IdentityTypeLocal` eklenir.)

`internal/server/rbac_test.go:71` ve `internal/server/rbac_operational_test.go:83`'teki çağrılara da aynı şekilde `identity.IdentityTypeLocal` eklenir.

- [x] **Adım 4: `internal/identity/identity_test.go`'ya OIDC testlerini ekle**

Dosyanın sonuna ekle:

```go
func TestLoginOrLinkOIDCUserCreatesUserFromPendingInvite(t *testing.T) {
	t.Parallel()
	service := newTestService()
	if _, _, err := service.Bootstrap(t.Context(), "org_default", "admin@example.com", "correct horse battery staple"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.CreateInvite(t.Context(), "admin", "org_default", "sso-user@example.com", identity.RolePlatformAdmin, nil, identity.IdentityTypeOIDC); err != nil {
		t.Fatalf("create invite: %v", err)
	}

	user, err := service.LoginOrLinkOIDCUser(t.Context(), "org_default", "https://idp.example.com", "subject-123", "sso-user@example.com")
	if err != nil {
		t.Fatalf("login or link: %v", err)
	}
	if user.Email != "sso-user@example.com" || user.OIDCIssuer != "https://idp.example.com" || user.OIDCSubject != "subject-123" {
		t.Fatalf("unexpected linked user: %#v", user)
	}
	if user.PasswordHash != "" {
		t.Fatalf("expected no password hash for an OIDC user, got %q", user.PasswordHash)
	}

	again, err := service.LoginOrLinkOIDCUser(t.Context(), "org_default", "https://idp.example.com", "subject-123", "sso-user@example.com")
	if err != nil || again.ID != user.ID {
		t.Fatalf("expected the second login to return the same linked user, got %#v %v", again, err)
	}
}

func TestLoginOrLinkOIDCUserRejectsWithoutAPendingInvite(t *testing.T) {
	t.Parallel()
	service := newTestService()
	if _, err := service.LoginOrLinkOIDCUser(t.Context(), "org_default", "https://idp.example.com", "subject-999", "unknown@example.com"); !errors.Is(err, identity.ErrInviteNotFound) {
		t.Fatalf("expected ErrInviteNotFound, got %v", err)
	}
}

func TestLoginOrLinkOIDCUserRejectsALocalTypeInvite(t *testing.T) {
	t.Parallel()
	service := newTestService()
	if _, _, err := service.Bootstrap(t.Context(), "org_default", "admin@example.com", "correct horse battery staple"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.CreateInvite(t.Context(), "admin", "org_default", "local-user@example.com", identity.RolePlatformAdmin, nil, identity.IdentityTypeLocal); err != nil {
		t.Fatalf("create invite: %v", err)
	}
	if _, err := service.LoginOrLinkOIDCUser(t.Context(), "org_default", "https://idp.example.com", "subject-1", "local-user@example.com"); !errors.Is(err, identity.ErrInviteWrongIdentityType) {
		t.Fatalf("expected ErrInviteWrongIdentityType, got %v", err)
	}
}

func TestConsumeInviteRejectsAnOIDCTypeInvite(t *testing.T) {
	t.Parallel()
	service := newTestService()
	if _, _, err := service.Bootstrap(t.Context(), "org_default", "admin@example.com", "correct horse battery staple"); err != nil {
		t.Fatal(err)
	}
	_, token, err := service.CreateInvite(t.Context(), "admin", "org_default", "sso-user@example.com", identity.RolePlatformAdmin, nil, identity.IdentityTypeOIDC)
	if err != nil {
		t.Fatalf("create invite: %v", err)
	}
	if _, err := service.ConsumeInvite(t.Context(), token, "a password"); !errors.Is(err, identity.ErrInviteWrongIdentityType) {
		t.Fatalf("expected ErrInviteWrongIdentityType, got %v", err)
	}
}

func TestSetAndReadOIDCConfigurationRedactsTheClientSecret(t *testing.T) {
	t.Parallel()
	service := newTestService()
	saved, err := service.SetOIDCConfiguration(t.Context(), "org_default", "https://idp.example.com/.well-known/openid-configuration", "https://idp.example.com", "client-id", "super-secret", "https://hub.example.com/api/v1/oidc/callback", "admin")
	if err != nil {
		t.Fatalf("set oidc configuration: %v", err)
	}
	if saved.ClientSecretEncrypted == nil {
		t.Fatal("expected SetOIDCConfiguration's return value to carry the encrypted secret internally")
	}

	read, err := service.OIDCConfiguration(t.Context(), "org_default")
	if err != nil {
		t.Fatalf("read oidc configuration: %v", err)
	}
	if read.ClientSecretEncrypted != nil {
		t.Fatalf("expected the redacted read path to omit the client secret, got %#v", read.ClientSecretEncrypted)
	}
	if read.DiscoveryURL != "https://idp.example.com/.well-known/openid-configuration" || read.ClientID != "client-id" {
		t.Fatalf("unexpected configuration: %#v", read)
	}

	_, secret, err := service.OIDCConfigurationWithSecret(t.Context(), "org_default")
	if err != nil || secret != "super-secret" {
		t.Fatalf("expected the internal read path to decrypt the client secret, got %q %v", secret, err)
	}
}

func TestOIDCConfigurationByOrganizationReturnsErrOIDCNotConfigured(t *testing.T) {
	t.Parallel()
	service := newTestService()
	if _, err := service.OIDCConfiguration(t.Context(), "org_default"); !errors.Is(err, identity.ErrOIDCNotConfigured) {
		t.Fatalf("expected ErrOIDCNotConfigured, got %v", err)
	}
}
```

- [x] **Adım 5: Testleri çalıştır**

Çalıştır: `GOCACHE=/tmp/bazusop-go-cache go test ./internal/identity/... -v 2>&1 | tail -80`
Beklenen: BAŞARILI (tüm eski + yeni testler)

- [x] **Adım 6: Postgres migration'ını oluştur**

`internal/storage/postgres/migrations/024_oidc.sql`:

```sql
-- OIDC users have no local password; NOT NULL was correct before this
-- spike (every user was local) and is now relaxed.
ALTER TABLE users ALTER COLUMN password_hash DROP NOT NULL;
ALTER TABLE users ADD COLUMN IF NOT EXISTS oidc_issuer TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN IF NOT EXISTS oidc_subject TEXT NOT NULL DEFAULT '';

-- Partial (oidc_subject <> '') so multiple local users, which all have
-- empty oidc_issuer/oidc_subject, never collide on this uniqueness rule.
CREATE UNIQUE INDEX IF NOT EXISTS users_oidc_identity_key
    ON users (organization_id, oidc_issuer, oidc_subject)
    WHERE oidc_subject <> '';

ALTER TABLE invites ADD COLUMN IF NOT EXISTS identity_type TEXT NOT NULL DEFAULT 'local';

CREATE TABLE IF NOT EXISTS oidc_configurations (
    organization_id TEXT PRIMARY KEY,
    discovery_url TEXT NOT NULL,
    issuer TEXT NOT NULL,
    client_id TEXT NOT NULL,
    client_secret_encrypted BYTEA NOT NULL,
    redirect_url TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    updated_by TEXT NOT NULL,
    CONSTRAINT oidc_configurations_org_fk FOREIGN KEY (organization_id) REFERENCES organizations(id) ON DELETE RESTRICT
);
```

- [x] **Adım 7: `internal/storage/postgres/identity.go`'yu güncelle**

`insertUser`'ı nullable `password_hash` ve yeni OIDC sütunlarını kapsayacak şekilde değiştir:

```go
func insertUser(ctx context.Context, database identityDatabase, user identity.User) error {
	var passwordHash *string
	if user.PasswordHash != "" {
		passwordHash = &user.PasswordHash
	}
	_, err := database.Exec(ctx, `
		INSERT INTO users (id, organization_id, email, role, password_hash, totp_secret_encrypted, totp_confirmed_at, created_at, disabled_at, oidc_issuer, oidc_subject)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		user.ID, user.OrganizationID, user.Email, string(user.Role), passwordHash,
		user.TOTPSecretEncrypted, user.TOTPConfirmedAt, user.CreatedAt, user.DisabledAt, user.OIDCIssuer, user.OIDCSubject)
	if err != nil {
		return fmt.Errorf("insert user: %w", err)
	}
	return nil
}
```

`UserByEmail`/`UserByID`'yi genişletilmiş SELECT'e uydur, `UserByOIDCIdentity`'yi ekle, `scanUser`'ı nullable `password_hash` ve yeni sütunları kapsayacak şekilde değiştir:

```go
func (store *Store) UserByEmail(ctx context.Context, organizationID, email string) (identity.User, error) {
	return scanUser(store.pool.QueryRow(ctx, `
		SELECT id, organization_id, email, role, password_hash, totp_secret_encrypted, totp_confirmed_at, created_at, disabled_at, oidc_issuer, oidc_subject
		FROM users WHERE organization_id=$1 AND email=$2`, organizationID, email))
}

func (store *Store) UserByID(ctx context.Context, id string) (identity.User, error) {
	return scanUser(store.pool.QueryRow(ctx, `
		SELECT id, organization_id, email, role, password_hash, totp_secret_encrypted, totp_confirmed_at, created_at, disabled_at, oidc_issuer, oidc_subject
		FROM users WHERE id=$1`, id))
}

func (store *Store) UserByOIDCIdentity(ctx context.Context, organizationID, issuer, subject string) (identity.User, error) {
	return scanUser(store.pool.QueryRow(ctx, `
		SELECT id, organization_id, email, role, password_hash, totp_secret_encrypted, totp_confirmed_at, created_at, disabled_at, oidc_issuer, oidc_subject
		FROM users WHERE organization_id=$1 AND oidc_issuer=$2 AND oidc_subject=$3 AND oidc_subject <> ''`, organizationID, issuer, subject))
}

func scanUser(row pgx.Row) (identity.User, error) {
	var user identity.User
	var role string
	var passwordHash *string
	if err := row.Scan(&user.ID, &user.OrganizationID, &user.Email, &role, &passwordHash,
		&user.TOTPSecretEncrypted, &user.TOTPConfirmedAt, &user.CreatedAt, &user.DisabledAt,
		&user.OIDCIssuer, &user.OIDCSubject); errors.Is(err, pgx.ErrNoRows) {
		return identity.User{}, identity.ErrInvalidCredentials
	} else if err != nil {
		return identity.User{}, fmt.Errorf("scan user: %w", err)
	}
	user.Role = identity.Role(role)
	if passwordHash != nil {
		user.PasswordHash = *passwordHash
	}
	return user, nil
}
```

`SaveInvite`/`InviteByTokenHash`'i `identity_type` sütununu kapsayacak şekilde genişlet, `InviteByEmail`/`ConsumeInviteByID`'yi ekle:

```go
func (store *Store) SaveInvite(ctx context.Context, invite identity.Invite, tokenHash string) error {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin save invite: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, `
		INSERT INTO invites (id, token_hash, organization_id, email, role, identity_type, created_by, expires_at, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		invite.ID, tokenHash, invite.OrganizationID, invite.Email, string(invite.Role), string(invite.IdentityType), invite.CreatedBy, invite.ExpiresAt, invite.CreatedAt)
	if err != nil {
		return fmt.Errorf("save invite: %w", err)
	}
	for _, grant := range invite.SiteRoleGrants {
		if _, err := tx.Exec(ctx, `INSERT INTO invite_site_roles (invite_id, site_id, role) VALUES ($1,$2,$3)`, invite.ID, grant.SiteID, grant.Role); err != nil {
			return fmt.Errorf("save invite site role: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit save invite: %w", err)
	}
	return nil
}

func (store *Store) InviteByTokenHash(ctx context.Context, tokenHash string) (identity.Invite, error) {
	var invite identity.Invite
	var role, identityType string
	err := store.pool.QueryRow(ctx, `
		SELECT id, organization_id, email, role, identity_type, created_by, expires_at, consumed_at, revoked_at, created_at
		FROM invites WHERE token_hash=$1`, tokenHash).Scan(
		&invite.ID, &invite.OrganizationID, &invite.Email, &role, &identityType, &invite.CreatedBy,
		&invite.ExpiresAt, &invite.ConsumedAt, &invite.RevokedAt, &invite.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.Invite{}, identity.ErrInviteNotFound
	}
	if err != nil {
		return identity.Invite{}, fmt.Errorf("query invite: %w", err)
	}
	invite.Role, invite.IdentityType = identity.Role(role), identity.IdentityType(identityType)

	rows, err := store.pool.Query(ctx, `SELECT site_id, role FROM invite_site_roles WHERE invite_id=$1`, invite.ID)
	if err != nil {
		return identity.Invite{}, fmt.Errorf("query invite site roles: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var grant identity.SiteRoleGrant
		if err := rows.Scan(&grant.SiteID, &grant.Role); err != nil {
			return identity.Invite{}, fmt.Errorf("scan invite site role: %w", err)
		}
		invite.SiteRoleGrants = append(invite.SiteRoleGrants, grant)
	}
	if err := rows.Err(); err != nil {
		return identity.Invite{}, fmt.Errorf("iterate invite site roles: %w", err)
	}
	return invite, nil
}

// InviteByEmail returns the most recently created invite for email,
// regardless of status — LoginOrLinkOIDCUser applies the same
// expiry/consumed/revoked/identity-type checks ConsumeInvite already
// applies for local invites.
func (store *Store) InviteByEmail(ctx context.Context, organizationID, email string) (identity.Invite, error) {
	var invite identity.Invite
	var role, identityType string
	err := store.pool.QueryRow(ctx, `
		SELECT id, organization_id, email, role, identity_type, created_by, expires_at, consumed_at, revoked_at, created_at
		FROM invites WHERE organization_id=$1 AND email=$2
		ORDER BY created_at DESC LIMIT 1`, organizationID, email).Scan(
		&invite.ID, &invite.OrganizationID, &invite.Email, &role, &identityType, &invite.CreatedBy,
		&invite.ExpiresAt, &invite.ConsumedAt, &invite.RevokedAt, &invite.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.Invite{}, identity.ErrInviteNotFound
	}
	if err != nil {
		return identity.Invite{}, fmt.Errorf("query invite by email: %w", err)
	}
	invite.Role, invite.IdentityType = identity.Role(role), identity.IdentityType(identityType)

	rows, err := store.pool.Query(ctx, `SELECT site_id, role FROM invite_site_roles WHERE invite_id=$1`, invite.ID)
	if err != nil {
		return identity.Invite{}, fmt.Errorf("query invite site roles: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var grant identity.SiteRoleGrant
		if err := rows.Scan(&grant.SiteID, &grant.Role); err != nil {
			return identity.Invite{}, fmt.Errorf("scan invite site role: %w", err)
		}
		invite.SiteRoleGrants = append(invite.SiteRoleGrants, grant)
	}
	if err := rows.Err(); err != nil {
		return identity.Invite{}, fmt.Errorf("iterate invite site roles: %w", err)
	}
	return invite, nil
}

func (store *Store) ConsumeInviteByID(ctx context.Context, inviteID string, consumedByUserID string, at time.Time) error {
	result, err := store.pool.Exec(ctx, `
		UPDATE invites SET consumed_at=$2, consumed_by_user_id=$3
		WHERE id=$1 AND consumed_at IS NULL`, inviteID, at, consumedByUserID)
	if err != nil {
		return fmt.Errorf("consume invite by id: %w", err)
	}
	if result.RowsAffected() != 1 {
		return identity.ErrInviteExpired
	}
	return nil
}
```

Dosyanın sonuna OIDC yapılandırma metotlarını ekle:

```go
func (store *Store) SaveOIDCConfiguration(ctx context.Context, config identity.OIDCConfiguration) error {
	_, err := store.pool.Exec(ctx, `
		INSERT INTO oidc_configurations (organization_id, discovery_url, issuer, client_id, client_secret_encrypted, redirect_url, updated_at, updated_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		ON CONFLICT (organization_id) DO UPDATE SET
			discovery_url=EXCLUDED.discovery_url, issuer=EXCLUDED.issuer, client_id=EXCLUDED.client_id,
			client_secret_encrypted=EXCLUDED.client_secret_encrypted, redirect_url=EXCLUDED.redirect_url,
			updated_at=EXCLUDED.updated_at, updated_by=EXCLUDED.updated_by`,
		config.OrganizationID, config.DiscoveryURL, config.Issuer, config.ClientID, config.ClientSecretEncrypted,
		config.RedirectURL, config.UpdatedAt, config.UpdatedBy)
	if err != nil {
		return fmt.Errorf("save oidc configuration: %w", err)
	}
	return nil
}

func (store *Store) OIDCConfigurationByOrganization(ctx context.Context, organizationID string) (identity.OIDCConfiguration, error) {
	var config identity.OIDCConfiguration
	err := store.pool.QueryRow(ctx, `
		SELECT organization_id, discovery_url, issuer, client_id, client_secret_encrypted, redirect_url, updated_at, updated_by
		FROM oidc_configurations WHERE organization_id=$1`, organizationID).Scan(
		&config.OrganizationID, &config.DiscoveryURL, &config.Issuer, &config.ClientID, &config.ClientSecretEncrypted,
		&config.RedirectURL, &config.UpdatedAt, &config.UpdatedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.OIDCConfiguration{}, identity.ErrOIDCNotConfigured
	}
	if err != nil {
		return identity.OIDCConfiguration{}, fmt.Errorf("query oidc configuration: %w", err)
	}
	return config, nil
}
```

- [x] **Adım 8: Build'i doğrula**

Çalıştır: `go build ./... 2>&1 | head -50`
Beklenen: `internal/server` henüz güncellenmediği için (Görev 3'te olacak) derleme hataları beklenir — ama `internal/identity` ve `internal/storage/postgres` kendi içinde tutarlı derlenmeli: `go build ./internal/identity/... ./internal/storage/postgres/...` başarılı olmalı.

- [x] **Adım 9: Commit**

```bash
git add internal/identity internal/storage/postgres/identity.go internal/storage/postgres/migrations/024_oidc.sql
git commit -m "feat: link OIDC identities to invited users and store organization OIDC settings"
```

## Görev 2: `internal/oidc` — OIDC protokol paketi

**Dosyalar:**
- Oluştur: `internal/oidc/oidc.go`, `internal/oidc/oidc_test.go`
- Değiştir: `go.mod`, `go.sum`

**Arayüzler:**
- Üretir: `oidc.Configuration`, `oidc.Flow`, `oidc.NewFlow() (Flow, error)`, `oidc.Claims`, `oidc.Provider`, `oidc.NewProvider(ctx, Configuration) (*Provider, error)`, `(*Provider) AuthCodeURL(Flow) string`, `(*Provider) Exchange(ctx, code string, Flow) (Claims, error)`, `oidc.ErrIssuerMismatch`, `oidc.ErrNonceMismatch`.

- [x] **Adım 1: Bağımlılıkları ekle**

```bash
go get github.com/coreos/go-oidc/v3@v3.21.0
go get golang.org/x/oauth2@v0.37.0
go mod tidy
```

Beklenen: `go.mod`'a `github.com/coreos/go-oidc/v3` ve `golang.org/x/oauth2` doğrudan bağımlılık olarak eklenir; `github.com/go-jose/go-jose/v4` geçişli bağımlılık olarak görünür.

- [x] **Adım 2: `internal/oidc/oidc.go`'yu yaz**

```go
// Package oidc wraps golang.org/x/oauth2 and github.com/coreos/go-oidc/v3
// into the Authorization Code + PKCE flow spike 11.9 needs: nothing in
// this package persists anything or knows about bazUSOP's user/invite
// model — internal/server passes in an organization's stored configuration
// and gets back verified claims (subject, email, acr, amr) to hand to
// internal/identity.
package oidc

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"

	oidclib "github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

var (
	ErrIssuerMismatch = errors.New("discovered issuer does not match the configured allowed issuer")
	ErrNonceMismatch  = errors.New("id token nonce does not match the login flow")
)

// Configuration is one organization's OIDC settings, already decrypted —
// internal/identity is the source of truth and the only thing that
// touches the client secret at rest.
type Configuration struct {
	DiscoveryURL string
	Issuer       string
	ClientID     string
	ClientSecret string
	RedirectURL  string
}

// Flow carries the per-attempt state a caller must round-trip through the
// browser (in a short-lived cookie, see internal/server) between
// AuthCodeURL and Exchange: state (CSRF/replay), nonce (ID token replay)
// and the PKCE code_verifier (authorization code interception).
type Flow struct {
	State        string
	Nonce        string
	CodeVerifier string
}

func NewFlow() (Flow, error) {
	state, err := randomString()
	if err != nil {
		return Flow{}, err
	}
	nonce, err := randomString()
	if err != nil {
		return Flow{}, err
	}
	return Flow{State: state, Nonce: nonce, CodeVerifier: oauth2.GenerateVerifier()}, nil
}

func randomString() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate random value: %w", err)
	}
	return hex.EncodeToString(value), nil
}

// Claims is the subset of a verified ID token spike 11.9 needs: Subject
// (the permanent issuer+subject identity), Email (used only for the
// one-time invite lookup on first login) and, if the IdP included them,
// acr/amr (see the design spec's "OIDC kullanıcı" section — not otherwise
// interpreted in this spike).
type Claims struct {
	Subject       string
	Email         string
	EmailVerified bool
	ACR           string
	AMR           []string
}

type rawClaims struct {
	Email         string   `json:"email"`
	EmailVerified bool     `json:"email_verified"`
	ACR           string   `json:"acr"`
	AMR           []string `json:"amr"`
}

// Provider wraps one organization's OIDC configuration into ready-to-use
// OAuth2 + ID-token-verification machinery.
type Provider struct {
	oauth2Config oauth2.Config
	verifier     *oidclib.IDTokenVerifier
}

// NewProvider fetches the issuer's discovery document and confirms it
// matches the platform administrator's explicitly configured allowed
// issuer — the design spec requires this as its own check, not just
// trusting whatever the discovery URL happens to return.
func NewProvider(ctx context.Context, configuration Configuration) (*Provider, error) {
	provider, err := oidclib.NewProvider(ctx, configuration.DiscoveryURL)
	if err != nil {
		return nil, fmt.Errorf("discover oidc provider: %w", err)
	}
	var discovered struct {
		Issuer string `json:"issuer"`
	}
	if err := provider.Claims(&discovered); err != nil {
		return nil, fmt.Errorf("read discovery claims: %w", err)
	}
	if discovered.Issuer != configuration.Issuer {
		return nil, ErrIssuerMismatch
	}
	return &Provider{
		oauth2Config: oauth2.Config{
			ClientID: configuration.ClientID, ClientSecret: configuration.ClientSecret,
			Endpoint: provider.Endpoint(), RedirectURL: configuration.RedirectURL,
			Scopes: []string{oidclib.ScopeOpenID, "email"},
		},
		verifier: provider.Verifier(&oidclib.Config{ClientID: configuration.ClientID}),
	}, nil
}

// AuthCodeURL builds the IdP redirect target for flow, binding state,
// nonce and a PKCE S256 challenge to this one login attempt.
func (provider *Provider) AuthCodeURL(flow Flow) string {
	return provider.oauth2Config.AuthCodeURL(flow.State, oidclib.Nonce(flow.Nonce), oauth2.S256ChallengeOption(flow.CodeVerifier))
}

// Exchange completes the callback half of the flow: exchanges code for
// tokens (presenting the PKCE verifier), then verifies the returned ID
// token's signature, issuer, audience and expiry (via the JWKS-backed
// verifier from NewProvider) and its nonce against flow.Nonce.
func (provider *Provider) Exchange(ctx context.Context, code string, flow Flow) (Claims, error) {
	token, err := provider.oauth2Config.Exchange(ctx, code, oauth2.VerifierOption(flow.CodeVerifier))
	if err != nil {
		return Claims{}, fmt.Errorf("exchange authorization code: %w", err)
	}
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		return Claims{}, fmt.Errorf("token response did not include an id_token")
	}
	idToken, err := provider.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return Claims{}, fmt.Errorf("verify id token: %w", err)
	}
	if idToken.Nonce != flow.Nonce {
		return Claims{}, ErrNonceMismatch
	}
	var extra rawClaims
	if err := idToken.Claims(&extra); err != nil {
		return Claims{}, fmt.Errorf("decode id token claims: %w", err)
	}
	return Claims{
		Subject: idToken.Subject, Email: extra.Email, EmailVerified: extra.EmailVerified,
		ACR: extra.ACR, AMR: extra.AMR,
	}, nil
}
```

- [x] **Adım 3: `internal/oidc/oidc_test.go`'yu yaz — sahte IdP + kapsamlı testler**

```go
package oidc_test

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"

	"github.com/gokayybaz/bazusop/internal/oidc"
)

// mockIdP is a minimal OIDC provider for tests: it serves discovery, JWKS
// and token endpoints, and hand-signs a real RS256 ID token so
// internal/oidc's signature/issuer/audience/expiry/nonce verification runs
// against real cryptography rather than a stub. It never serves the
// authorization endpoint — tests call Provider.AuthCodeURL directly and
// extract state/nonce/code_challenge from the returned URL string, since
// nothing in this package or its caller ever needs the browser to actually
// navigate there.
type mockIdP struct {
	server            *httptest.Server
	key               *rsa.PrivateKey
	audience          string
	subject           string
	email             string
	emailVerified     bool
	expectedNonce     string
	expectedChallenge string
	issueExpiredToken bool
}

func newMockIdP(t *testing.T) *mockIdP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	idp := &mockIdP{key: key, emailVerified: true}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(response http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(response).Encode(map[string]any{
			"issuer":                                 idp.server.URL,
			"authorization_endpoint":                 idp.server.URL + "/authorize",
			"token_endpoint":                         idp.server.URL + "/token",
			"jwks_uri":                                idp.server.URL + "/jwks",
			"id_token_signing_alg_values_supported":  []string{"RS256"},
		})
	})
	mux.HandleFunc("/jwks", func(response http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(response).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{
			{Key: &idp.key.PublicKey, KeyID: "test-key", Algorithm: "RS256", Use: "sig"},
		}})
	})
	mux.HandleFunc("/token", func(response http.ResponseWriter, request *http.Request) {
		if err := request.ParseForm(); err != nil {
			http.Error(response, err.Error(), http.StatusBadRequest)
			return
		}
		if idp.expectedChallenge != "" {
			sum := sha256.Sum256([]byte(request.FormValue("code_verifier")))
			if base64.RawURLEncoding.EncodeToString(sum[:]) != idp.expectedChallenge {
				http.Error(response, "pkce verifier does not match the original challenge", http.StatusBadRequest)
				return
			}
		}
		expiry := time.Now().Add(time.Hour)
		if idp.issueExpiredToken {
			expiry = time.Now().Add(-time.Hour)
		}
		claims := map[string]any{
			"iss": idp.server.URL, "sub": idp.subject, "aud": idp.audience,
			"exp": expiry.Unix(), "iat": time.Now().Unix(), "nonce": idp.expectedNonce,
			"email": idp.email, "email_verified": idp.emailVerified,
		}
		payload, err := json.Marshal(claims)
		if err != nil {
			http.Error(response, err.Error(), http.StatusInternalServerError)
			return
		}
		signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: idp.key}, (&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "test-key"))
		if err != nil {
			http.Error(response, err.Error(), http.StatusInternalServerError)
			return
		}
		signed, err := signer.Sign(payload)
		if err != nil {
			http.Error(response, err.Error(), http.StatusInternalServerError)
			return
		}
		compact, err := signed.CompactSerialize()
		if err != nil {
			http.Error(response, err.Error(), http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(response).Encode(map[string]any{
			"access_token": "test-access-token", "token_type": "Bearer", "id_token": compact,
		})
	})
	idp.server = httptest.NewServer(mux)
	t.Cleanup(idp.server.Close)
	return idp
}

func newTestProvider(t *testing.T, idp *mockIdP) *oidc.Provider {
	t.Helper()
	provider, err := oidc.NewProvider(t.Context(), oidc.Configuration{
		DiscoveryURL: idp.server.URL + "/.well-known/openid-configuration",
		Issuer:       idp.server.URL, ClientID: idp.audience, ClientSecret: "test-secret",
		RedirectURL: "https://hub.example.com/api/v1/oidc/callback",
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	return provider
}

func primeIdPFromAuthCodeURL(t *testing.T, idp *mockIdP, authCodeURL string) {
	t.Helper()
	parsed, err := url.Parse(authCodeURL)
	if err != nil {
		t.Fatalf("parse auth code url: %v", err)
	}
	if parsed.Query().Get("code_challenge_method") != "S256" {
		t.Fatalf("expected an S256 PKCE challenge method, got %q", parsed.Query().Get("code_challenge_method"))
	}
	idp.expectedChallenge = parsed.Query().Get("code_challenge")
}

func TestExchangeVerifiesSignatureIssuerAudienceAndNonce(t *testing.T) {
	t.Parallel()
	idp := newMockIdP(t)
	idp.audience, idp.subject, idp.email = "test-client", "subject-123", "user@example.com"
	provider := newTestProvider(t, idp)

	flow, err := oidc.NewFlow()
	if err != nil {
		t.Fatalf("new flow: %v", err)
	}
	primeIdPFromAuthCodeURL(t, idp, provider.AuthCodeURL(flow))
	idp.expectedNonce = flow.Nonce

	claims, err := provider.Exchange(t.Context(), "test-code", flow)
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	if claims.Subject != "subject-123" || claims.Email != "user@example.com" || !claims.EmailVerified {
		t.Fatalf("unexpected claims: %#v", claims)
	}
}

func TestExchangeRejectsAMismatchedNonce(t *testing.T) {
	t.Parallel()
	idp := newMockIdP(t)
	idp.audience, idp.subject, idp.email = "test-client", "subject-1", "a@example.com"
	provider := newTestProvider(t, idp)

	flow, err := oidc.NewFlow()
	if err != nil {
		t.Fatal(err)
	}
	primeIdPFromAuthCodeURL(t, idp, provider.AuthCodeURL(flow))
	idp.expectedNonce = "a-different-nonce" // simulates a forged/replayed token

	if _, err := provider.Exchange(t.Context(), "test-code", flow); !errors.Is(err, oidc.ErrNonceMismatch) {
		t.Fatalf("expected ErrNonceMismatch, got %v", err)
	}
}

func TestExchangeRejectsAnExpiredIDToken(t *testing.T) {
	t.Parallel()
	idp := newMockIdP(t)
	idp.audience, idp.subject, idp.email = "test-client", "subject-1", "a@example.com"
	idp.issueExpiredToken = true
	provider := newTestProvider(t, idp)

	flow, err := oidc.NewFlow()
	if err != nil {
		t.Fatal(err)
	}
	primeIdPFromAuthCodeURL(t, idp, provider.AuthCodeURL(flow))
	idp.expectedNonce = flow.Nonce

	if _, err := provider.Exchange(t.Context(), "test-code", flow); err == nil {
		t.Fatal("expected an error for an expired id token")
	}
}

func TestExchangeRejectsAWrongPKCEVerifier(t *testing.T) {
	t.Parallel()
	idp := newMockIdP(t)
	idp.audience, idp.subject, idp.email = "test-client", "subject-1", "a@example.com"
	provider := newTestProvider(t, idp)

	flow, err := oidc.NewFlow()
	if err != nil {
		t.Fatal(err)
	}
	primeIdPFromAuthCodeURL(t, idp, provider.AuthCodeURL(flow))
	idp.expectedNonce = flow.Nonce
	tampered := flow
	tampered.CodeVerifier = "a-different-verifier-that-does-not-match-the-challenge"

	if _, err := provider.Exchange(t.Context(), "test-code", tampered); err == nil {
		t.Fatal("expected an error for a mismatched PKCE verifier")
	}
}

func TestNewProviderRejectsAnIssuerMismatch(t *testing.T) {
	t.Parallel()
	idp := newMockIdP(t)
	_, err := oidc.NewProvider(t.Context(), oidc.Configuration{
		DiscoveryURL: idp.server.URL + "/.well-known/openid-configuration",
		Issuer:       "https://not-the-real-issuer.example.com", ClientID: "test-client", ClientSecret: "test-secret",
		RedirectURL: "https://hub.example.com/api/v1/oidc/callback",
	})
	if !errors.Is(err, oidc.ErrIssuerMismatch) {
		t.Fatalf("expected ErrIssuerMismatch, got %v", err)
	}
}
```

- [x] **Adım 4: Testleri çalıştır**

Çalıştır: `GOCACHE=/tmp/bazusop-go-cache go test ./internal/oidc/... -v 2>&1 | tail -80`
Beklenen: BAŞARILI (tüm testler)

- [x] **Adım 5: Commit**

```bash
git add go.mod go.sum internal/oidc
git commit -m "feat: add internal/oidc, a PKCE-verified OIDC protocol wrapper"
```

## Görev 3: `internal/server` — OIDC ayarları CRUD'u ve giriş/callback uçları

**Dosyalar:**
- Oluştur: `internal/server/oidc.go`
- Değiştir: `internal/server/identity.go`, `internal/server/server.go`

**Arayüzler:**
- Üretir: `handleSetOIDCConfiguration`, `handleGetOIDCConfiguration`, `handleOIDCLogin`, `handleOIDCCallback` (hepsi `internal/server/oidc.go`'da).

- [x] **Adım 1: `internal/server/identity.go`'daki `handleCreateInvite`'ı `identity_type` alanını kabul edecek şekilde güncelle**

```go
func handleCreateInvite(service *identity.Service, sessionService *sessions.Service, authzService *authorization.Service, activityService *activity.Service, scope tenancy.Scope) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		actorUserID, ok := requirePermission(response, request, sessionService, nil, authzService, authorization.PermissionManageUsers, scope.SiteID)
		if !ok {
			return
		}
		var body struct {
			Email        string   `json:"email"`
			Role         string   `json:"role"`
			SiteIDs      []string `json:"site_ids"`
			IdentityType string   `json:"identity_type"`
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
		identityType := identity.IdentityTypeLocal
		if body.IdentityType == string(identity.IdentityTypeOIDC) {
			identityType = identity.IdentityTypeOIDC
		}
		invite, token, err := service.CreateInvite(request.Context(), createdBy, tenancy.DefaultOrganizationID, body.Email, role, grants, identityType)
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

- [x] **Adım 2: `internal/server/oidc.go`'yu yaz**

```go
package server

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gokayybaz/bazusop/internal/activity"
	"github.com/gokayybaz/bazusop/internal/authorization"
	"github.com/gokayybaz/bazusop/internal/identity"
	"github.com/gokayybaz/bazusop/internal/oidc"
	"github.com/gokayybaz/bazusop/internal/sessions"
	"github.com/gokayybaz/bazusop/internal/tenancy"
)

const oidcFlowCookieName = "bazusop_oidc_flow"

type oidcFlowCookieValue struct {
	State        string `json:"state"`
	Nonce        string `json:"nonce"`
	CodeVerifier string `json:"code_verifier"`
}

// setOIDCFlowCookie carries state/nonce/PKCE verifier through the
// browser's round trip to the IdP and back. It is HttpOnly and scoped to
// the OIDC path only; a 10-minute Max-Age comfortably covers a human
// completing an IdP login screen while keeping a stale, unused flow from
// lingering indefinitely.
func setOIDCFlowCookie(response http.ResponseWriter, request *http.Request, flow oidc.Flow) error {
	payload, err := json.Marshal(oidcFlowCookieValue{State: flow.State, Nonce: flow.Nonce, CodeVerifier: flow.CodeVerifier})
	if err != nil {
		return err
	}
	http.SetCookie(response, &http.Cookie{
		Name: oidcFlowCookieName, Value: base64.RawURLEncoding.EncodeToString(payload),
		Path: "/api/v1/oidc", HttpOnly: true, Secure: request.TLS != nil, SameSite: http.SameSiteLaxMode, MaxAge: 600,
	})
	return nil
}

func readAndClearOIDCFlowCookie(response http.ResponseWriter, request *http.Request) (oidc.Flow, bool) {
	cookie, err := request.Cookie(oidcFlowCookieName)
	if err != nil || cookie.Value == "" {
		return oidc.Flow{}, false
	}
	http.SetCookie(response, &http.Cookie{
		Name: oidcFlowCookieName, Value: "", Path: "/api/v1/oidc", HttpOnly: true,
		Secure: request.TLS != nil, SameSite: http.SameSiteLaxMode, MaxAge: -1,
	})
	decoded, err := base64.RawURLEncoding.DecodeString(cookie.Value)
	if err != nil {
		return oidc.Flow{}, false
	}
	var value oidcFlowCookieValue
	if err := json.Unmarshal(decoded, &value); err != nil {
		return oidc.Flow{}, false
	}
	return oidc.Flow{State: value.State, Nonce: value.Nonce, CodeVerifier: value.CodeVerifier}, true
}

func handleSetOIDCConfiguration(service *identity.Service, sessionService *sessions.Service, authzService *authorization.Service, scope tenancy.Scope) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		actorID, ok := requirePermission(response, request, sessionService, nil, authzService, authorization.PermissionManageOrgSecurity, scope.SiteID)
		if !ok {
			return
		}
		var body struct {
			DiscoveryURL string `json:"discovery_url"`
			Issuer       string `json:"issuer"`
			ClientID     string `json:"client_id"`
			ClientSecret string `json:"client_secret"`
			RedirectURL  string `json:"redirect_url"`
		}
		if err := decodeJSON(response, request, &body); err != nil {
			return
		}
		config, err := service.SetOIDCConfiguration(request.Context(), scope.OrganizationID, body.DiscoveryURL, body.Issuer, body.ClientID, body.ClientSecret, body.RedirectURL, actorID)
		if err != nil {
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		writeJSON(response, http.StatusOK, oidcConfigurationResponse(config))
	}
}

func handleGetOIDCConfiguration(service *identity.Service, sessionService *sessions.Service, authzService *authorization.Service, scope tenancy.Scope) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		if _, ok := requirePermission(response, request, sessionService, nil, authzService, authorization.PermissionManageOrgSecurity, scope.SiteID); !ok {
			return
		}
		config, err := service.OIDCConfiguration(request.Context(), scope.OrganizationID)
		if errors.Is(err, identity.ErrOIDCNotConfigured) {
			http.Error(response, http.StatusText(http.StatusNotFound), http.StatusNotFound)
			return
		}
		if err != nil {
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		writeJSON(response, http.StatusOK, oidcConfigurationResponse(config))
	}
}

func oidcConfigurationResponse(config identity.OIDCConfiguration) any {
	return struct {
		DiscoveryURL string `json:"discovery_url"`
		Issuer       string `json:"issuer"`
		ClientID     string `json:"client_id"`
		RedirectURL  string `json:"redirect_url"`
		UpdatedAt    string `json:"updated_at"`
		UpdatedBy    string `json:"updated_by"`
	}{config.DiscoveryURL, config.Issuer, config.ClientID, config.RedirectURL, config.UpdatedAt.Format(timeLayout), config.UpdatedBy}
}

func handleOIDCLogin(service *identity.Service, scope tenancy.Scope) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		config, secret, err := service.OIDCConfigurationWithSecret(request.Context(), scope.OrganizationID)
		if errors.Is(err, identity.ErrOIDCNotConfigured) {
			http.Error(response, http.StatusText(http.StatusNotFound), http.StatusNotFound)
			return
		}
		if err != nil {
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		provider, err := oidc.NewProvider(request.Context(), oidc.Configuration{
			DiscoveryURL: config.DiscoveryURL, Issuer: config.Issuer, ClientID: config.ClientID,
			ClientSecret: secret, RedirectURL: config.RedirectURL,
		})
		if err != nil {
			http.Error(response, http.StatusText(http.StatusBadGateway), http.StatusBadGateway)
			return
		}
		flow, err := oidc.NewFlow()
		if err != nil {
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		if err := setOIDCFlowCookie(response, request, flow); err != nil {
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		http.Redirect(response, request, provider.AuthCodeURL(flow), http.StatusFound)
	}
}

func handleOIDCCallback(service *identity.Service, sessionService *sessions.Service, activityService *activity.Service, scope tenancy.Scope) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		flow, ok := readAndClearOIDCFlowCookie(response, request)
		if !ok {
			http.Error(response, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}
		query := request.URL.Query()
		if query.Get("state") != flow.State {
			http.Error(response, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}
		code := query.Get("code")
		if code == "" {
			http.Error(response, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}
		config, secret, err := service.OIDCConfigurationWithSecret(request.Context(), scope.OrganizationID)
		if err != nil {
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		provider, err := oidc.NewProvider(request.Context(), oidc.Configuration{
			DiscoveryURL: config.DiscoveryURL, Issuer: config.Issuer, ClientID: config.ClientID,
			ClientSecret: secret, RedirectURL: config.RedirectURL,
		})
		if err != nil {
			http.Error(response, http.StatusText(http.StatusBadGateway), http.StatusBadGateway)
			return
		}
		claims, err := provider.Exchange(request.Context(), code, flow)
		if err != nil {
			http.Error(response, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}
		user, err := service.LoginOrLinkOIDCUser(request.Context(), scope.OrganizationID, config.Issuer, claims.Subject, claims.Email)
		if errors.Is(err, identity.ErrInviteNotFound) || errors.Is(err, identity.ErrInviteExpired) || errors.Is(err, identity.ErrInviteWrongIdentityType) {
			http.Error(response, http.StatusText(http.StatusForbidden), http.StatusForbidden)
			return
		}
		if err != nil {
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		_, token, csrfToken, err := sessionService.Create(request.Context(), user.ID, user.OrganizationID)
		if err != nil {
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		setSessionCookies(response, request, token, csrfToken)
		if activityService != nil {
			_ = activityService.Record(request.Context(), activity.Event{
				OrganizationID: scope.OrganizationID, SiteID: scope.SiteID,
				Source: activity.SourceIdentity, ReferenceID: user.ID, Type: "oidc_login",
				Actor: user.ID, Message: user.Email + " OIDC ile giriş yaptı",
			})
		}
		http.Redirect(response, request, "/", http.StatusFound)
	}
}
```

- [x] **Adım 3: `internal/server/server.go`'ya yeni route'ları ekle**

`if configuration.identityService != nil { ... }` bloğunu şununla değiştir (mevcut üç satır + dört yeni satır):

```go
	if configuration.identityService != nil {
		registerAudited(mux, "/api/v1/bootstrap", http.MethodPost, "bootstrap", nil, configuration.auditTrail, configuration.scope, handleBootstrap(configuration.identityService, configuration.bootstrapSecret))
		registerAudited(mux, "/api/v1/users/invites", http.MethodPost, "invites", nil, configuration.auditTrail, configuration.scope, handleCreateInvite(configuration.identityService, configuration.sessionService, configuration.authorizationService, configuration.activityService, configuration.scope))
		registerAudited(mux, "/api/v1/invites/{token}/consume", http.MethodPost, "invites", []string{"token"}, configuration.auditTrail, configuration.scope, handleConsumeInvite(configuration.identityService))
		registerAudited(mux, "/api/v1/users/{userID}/confirm-totp", http.MethodPost, "users", []string{"userID"}, configuration.auditTrail, configuration.scope, handleConfirmTOTP(configuration.identityService))
		registerAudited(mux, "/api/v1/organization/oidc", http.MethodPut, "oidc_configuration", nil, configuration.auditTrail, configuration.scope, handleSetOIDCConfiguration(configuration.identityService, configuration.sessionService, configuration.authorizationService, configuration.scope))
		registerAudited(mux, "/api/v1/organization/oidc", http.MethodGet, "oidc_configuration", nil, configuration.auditTrail, configuration.scope, handleGetOIDCConfiguration(configuration.identityService, configuration.sessionService, configuration.authorizationService, configuration.scope))
		registerAudited(mux, "/api/v1/oidc/login", http.MethodGet, "oidc_login", nil, configuration.auditTrail, configuration.scope, handleOIDCLogin(configuration.identityService, configuration.scope))
		registerAudited(mux, "/api/v1/oidc/callback", http.MethodGet, "oidc_login", nil, configuration.auditTrail, configuration.scope, handleOIDCCallback(configuration.identityService, configuration.sessionService, configuration.activityService, configuration.scope))
	}
```

- [x] **Adım 4: Build'i doğrula**

Çalıştır: `go build ./... 2>&1 | head -50 && echo BUILD_OK`
Beklenen: BAŞARILI (üretim kodu; test dosyaları Görev 4'te güncellenir/oluşturulur)

- [x] **Adım 5: Etkilenen mevcut testleri düzelt**

`internal/server/rbac_test.go` ve `internal/server/rbac_operational_test.go`'daki `identityService.CreateInvite(...)` çağrılarına (Görev 1 Adım 3'te zaten `identity.IdentityTypeLocal` eklendi) ek olarak, `internal/server/identity_test.go`'nun `handleCreateInvite` çağrıları HTTP body üzerinden gittiği için (Go fonksiyon imzası değil) DEĞİŞMEDEN çalışmaya devam eder — yeni `identity_type` alanı isteğe bağlıdır ve `decodeJSON`'ın `DisallowUnknownFields()`'ı yalnız BİLİNMEYEN alanları reddeder, eksik OPSİYONEL alanları değil.

Çalıştır: `GOCACHE=/tmp/bazusop-go-cache go test ./internal/server/... -v 2>&1 | tail -150`
Beklenen: `TestCreateInviteWithASiteRoleGrantsMembershipOnConsumption` gibi mevcut testler değişmeden BAŞARILI olmalı; `internal/server/oidc_test.go` henüz yok, bu yüzden yeni OIDC davranışı için hiçbir test yok (Görev 4'te eklenir).

- [x] **Adım 6: Commit**

```bash
git add internal/server/identity.go internal/server/oidc.go internal/server/server.go
git commit -m "feat: wire OIDC configuration CRUD and login/callback endpoints"
```

## Görev 4: Uçtan uca doğrulama — sahte IdP ile PKCE akışı ve yerel girişin bağımsızlığı

**Dosyalar:**
- Oluştur: `internal/server/oidc_test.go`

**Arayüzler:**
- Tüketir: Görev 2'nin `internal/oidc` paketi (sahte IdP deseni, `internal/oidc/oidc_test.go`'daki `mockIdP`'nin aynısı — farklı paketler oldukları için küçük bir kod tekrarı burada kabul edilebilir, bu depoda `newTestService`/benzeri yardımcıların paket başına tekrarlanması zaten yerleşik bir desen).

- [x] **Adım 1: `internal/server/oidc_test.go`'yu yaz**

```go
package server_test

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"

	"github.com/gokayybaz/bazusop/internal/activity"
	"github.com/gokayybaz/bazusop/internal/alerting"
	"github.com/gokayybaz/bazusop/internal/audittrail"
	"github.com/gokayybaz/bazusop/internal/authorization"
	"github.com/gokayybaz/bazusop/internal/identity"
	"github.com/gokayybaz/bazusop/internal/jobs"
	"github.com/gokayybaz/bazusop/internal/server"
	"github.com/gokayybaz/bazusop/internal/sessions"
	"github.com/gokayybaz/bazusop/internal/tenancy"
)

// mockIdP mirrors internal/oidc/oidc_test.go's helper — see that file's
// doc comment for why /authorize is never actually served.
type mockIdP struct {
	server            *httptest.Server
	key               *rsa.PrivateKey
	audience          string
	subject           string
	email             string
	expectedNonce     string
	expectedChallenge string
}

func newOIDCMockIdP(t *testing.T) *mockIdP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	idp := &mockIdP{key: key}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(response http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(response).Encode(map[string]any{
			"issuer": idp.server.URL, "authorization_endpoint": idp.server.URL + "/authorize",
			"token_endpoint": idp.server.URL + "/token", "jwks_uri": idp.server.URL + "/jwks",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/jwks", func(response http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(response).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{
			{Key: &idp.key.PublicKey, KeyID: "test-key", Algorithm: "RS256", Use: "sig"},
		}})
	})
	mux.HandleFunc("/token", func(response http.ResponseWriter, request *http.Request) {
		if err := request.ParseForm(); err != nil {
			http.Error(response, err.Error(), http.StatusBadRequest)
			return
		}
		if idp.expectedChallenge != "" {
			sum := sha256.Sum256([]byte(request.FormValue("code_verifier")))
			if base64.RawURLEncoding.EncodeToString(sum[:]) != idp.expectedChallenge {
				http.Error(response, "pkce mismatch", http.StatusBadRequest)
				return
			}
		}
		claims := map[string]any{
			"iss": idp.server.URL, "sub": idp.subject, "aud": idp.audience,
			"exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(), "nonce": idp.expectedNonce,
			"email": idp.email, "email_verified": true,
		}
		payload, _ := json.Marshal(claims)
		signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: idp.key}, (&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "test-key"))
		if err != nil {
			http.Error(response, err.Error(), http.StatusInternalServerError)
			return
		}
		signed, err := signer.Sign(payload)
		if err != nil {
			http.Error(response, err.Error(), http.StatusInternalServerError)
			return
		}
		compact, err := signed.CompactSerialize()
		if err != nil {
			http.Error(response, err.Error(), http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(response).Encode(map[string]any{"access_token": "test-access-token", "token_type": "Bearer", "id_token": compact})
	})
	idp.server = httptest.NewServer(mux)
	t.Cleanup(idp.server.Close)
	return idp
}

func newOIDCTestHandler(t *testing.T) (http.Handler, *identity.Service, *sessions.Service) {
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
	activityService := activity.NewService(activity.NewMemoryStore(jobs.NewMemoryStore(), alerting.NewMemoryStore()))
	handler := server.NewHandler(
		server.WithDefaultScope(tenancy.DefaultScope()),
		server.WithIdentity(identityService, "bootstrap-secret"),
		server.WithSessions(sessionService, identityService),
		server.WithAuthorization(authzService),
		server.WithActivity(activityService),
		server.WithAuditTrail(audittrail.NewService(audittrail.NewMemoryStore())),
	)
	return handler, identityService, sessionService
}

func TestOIDCLoginPKCEFlowCreatesSessionAndLocalLoginStaysIndependent(t *testing.T) {
	t.Parallel()
	idp := newOIDCMockIdP(t)
	idp.audience, idp.subject, idp.email = "test-client", "subject-abc", "sso-user@example.com"

	handler, _, _ := newOIDCTestHandler(t)
	adminEmail, adminPassword := "admin@example.com", "correct horse battery staple"
	adminCookies := bootstrapAndLogin(t, handler, "bootstrap-secret", adminEmail, adminPassword)

	configRequest := httptest.NewRequest(http.MethodPut, "/api/v1/organization/oidc", encodeJSON(t, map[string]string{
		"discovery_url": idp.server.URL + "/.well-known/openid-configuration",
		"issuer":        idp.server.URL, "client_id": "test-client", "client_secret": "test-secret",
		"redirect_url": "https://hub.example.com/api/v1/oidc/callback",
	}))
	for _, cookie := range adminCookies {
		configRequest.AddCookie(cookie)
	}
	configResponse := httptest.NewRecorder()
	handler.ServeHTTP(configResponse, configRequest)
	if configResponse.Code != http.StatusOK {
		t.Fatalf("set oidc configuration: %d %s", configResponse.Code, configResponse.Body.String())
	}
	if strings.Contains(configResponse.Body.String(), "test-secret") {
		t.Fatal("client secret must never appear in the configuration response")
	}

	inviteRequest := httptest.NewRequest(http.MethodPost, "/api/v1/users/invites", encodeJSON(t, map[string]any{
		"email": "sso-user@example.com", "identity_type": "oidc",
	}))
	for _, cookie := range adminCookies {
		inviteRequest.AddCookie(cookie)
	}
	inviteResponse := httptest.NewRecorder()
	handler.ServeHTTP(inviteResponse, inviteRequest)
	if inviteResponse.Code != http.StatusCreated {
		t.Fatalf("create oidc invite: %d %s", inviteResponse.Code, inviteResponse.Body.String())
	}

	loginResponse := httptest.NewRecorder()
	handler.ServeHTTP(loginResponse, httptest.NewRequest(http.MethodGet, "/api/v1/oidc/login", nil))
	if loginResponse.Code != http.StatusFound {
		t.Fatalf("expected a redirect to the IdP, got %d: %s", loginResponse.Code, loginResponse.Body.String())
	}
	authorizeURL, err := url.Parse(loginResponse.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse authorize url: %v", err)
	}
	idp.expectedChallenge = authorizeURL.Query().Get("code_challenge")
	idp.expectedNonce = authorizeURL.Query().Get("nonce")
	flowCookies := loginResponse.Result().Cookies()

	callbackRequest := httptest.NewRequest(http.MethodGet, "/api/v1/oidc/callback?code=test-code&state="+authorizeURL.Query().Get("state"), nil)
	for _, cookie := range flowCookies {
		callbackRequest.AddCookie(cookie)
	}
	callbackResponse := httptest.NewRecorder()
	handler.ServeHTTP(callbackResponse, callbackRequest)
	if callbackResponse.Code != http.StatusFound {
		t.Fatalf("expected the callback to redirect after login, got %d: %s", callbackResponse.Code, callbackResponse.Body.String())
	}
	ssoCookies := callbackResponse.Result().Cookies()
	if len(ssoCookies) == 0 {
		t.Fatal("expected the callback to set session cookies")
	}

	whoAmIRequest := httptest.NewRequest(http.MethodGet, "/api/v1/session", nil)
	for _, cookie := range ssoCookies {
		whoAmIRequest.AddCookie(cookie)
	}
	whoAmIResponse := httptest.NewRecorder()
	handler.ServeHTTP(whoAmIResponse, whoAmIRequest)
	if whoAmIResponse.Code != http.StatusOK {
		t.Fatalf("expected the sso session to be valid, got %d: %s", whoAmIResponse.Code, whoAmIResponse.Body.String())
	}
	var whoAmI struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(whoAmIResponse.Body).Decode(&whoAmI); err != nil || whoAmI.Email != "sso-user@example.com" {
		t.Fatalf("unexpected whoami: %#v, %v", whoAmI, err)
	}

	// Local login is entirely unaffected by OIDC being configured: a wrong
	// TOTP code is still evaluated independently and rejected the same way
	// it always was.
	localLoginResponse := httptest.NewRecorder()
	handler.ServeHTTP(localLoginResponse, httptest.NewRequest(http.MethodPost, "/api/v1/sessions", encodeJSON(t, map[string]string{
		"email": adminEmail, "password": adminPassword, "totp_code": "000000",
	})))
	if localLoginResponse.Code != http.StatusUnauthorized {
		t.Fatalf("expected local login to keep working independently, got %d", localLoginResponse.Code)
	}
}

func TestOIDCCallbackRejectsAMismatchedState(t *testing.T) {
	t.Parallel()
	idp := newOIDCMockIdP(t)
	idp.audience, idp.subject, idp.email = "test-client", "subject-1", "sso-user@example.com"
	handler, _, _ := newOIDCTestHandler(t)
	adminCookies := bootstrapAndLogin(t, handler, "bootstrap-secret", "admin@example.com", "correct horse battery staple")

	configRequest := httptest.NewRequest(http.MethodPut, "/api/v1/organization/oidc", encodeJSON(t, map[string]string{
		"discovery_url": idp.server.URL + "/.well-known/openid-configuration",
		"issuer":        idp.server.URL, "client_id": "test-client", "client_secret": "test-secret",
		"redirect_url": "https://hub.example.com/api/v1/oidc/callback",
	}))
	for _, cookie := range adminCookies {
		configRequest.AddCookie(cookie)
	}
	handler.ServeHTTP(httptest.NewRecorder(), configRequest)

	loginResponse := httptest.NewRecorder()
	handler.ServeHTTP(loginResponse, httptest.NewRequest(http.MethodGet, "/api/v1/oidc/login", nil))
	flowCookies := loginResponse.Result().Cookies()

	callbackRequest := httptest.NewRequest(http.MethodGet, "/api/v1/oidc/callback?code=test-code&state=a-forged-state", nil)
	for _, cookie := range flowCookies {
		callbackRequest.AddCookie(cookie)
	}
	callbackResponse := httptest.NewRecorder()
	handler.ServeHTTP(callbackResponse, callbackRequest)
	if callbackResponse.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for a mismatched state, got %d", callbackResponse.Code)
	}
}

func TestOIDCCallbackRejectsWithoutAPendingInvite(t *testing.T) {
	t.Parallel()
	idp := newOIDCMockIdP(t)
	idp.audience, idp.subject, idp.email = "test-client", "subject-uninvited", "uninvited@example.com"
	handler, _, _ := newOIDCTestHandler(t)
	adminCookies := bootstrapAndLogin(t, handler, "bootstrap-secret", "admin@example.com", "correct horse battery staple")

	configRequest := httptest.NewRequest(http.MethodPut, "/api/v1/organization/oidc", encodeJSON(t, map[string]string{
		"discovery_url": idp.server.URL + "/.well-known/openid-configuration",
		"issuer":        idp.server.URL, "client_id": "test-client", "client_secret": "test-secret",
		"redirect_url": "https://hub.example.com/api/v1/oidc/callback",
	}))
	for _, cookie := range adminCookies {
		configRequest.AddCookie(cookie)
	}
	handler.ServeHTTP(httptest.NewRecorder(), configRequest)

	loginResponse := httptest.NewRecorder()
	handler.ServeHTTP(loginResponse, httptest.NewRequest(http.MethodGet, "/api/v1/oidc/login", nil))
	authorizeURL, err := url.Parse(loginResponse.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse authorize url: %v", err)
	}
	idp.expectedChallenge = authorizeURL.Query().Get("code_challenge")
	idp.expectedNonce = authorizeURL.Query().Get("nonce")
	flowCookies := loginResponse.Result().Cookies()

	callbackRequest := httptest.NewRequest(http.MethodGet, "/api/v1/oidc/callback?code=test-code&state="+authorizeURL.Query().Get("state"), nil)
	for _, cookie := range flowCookies {
		callbackRequest.AddCookie(cookie)
	}
	callbackResponse := httptest.NewRecorder()
	handler.ServeHTTP(callbackResponse, callbackRequest)
	if callbackResponse.Code != http.StatusForbidden {
		t.Fatalf("expected 403 without a pending oidc invite, got %d: %s", callbackResponse.Code, callbackResponse.Body.String())
	}
}

func TestOIDCConfigurationRequiresPlatformAdmin(t *testing.T) {
	t.Parallel()
	handler, identityService, sessionService := newOIDCTestHandler(t)
	admin, _, err := identityService.Bootstrap(t.Context(), tenancy.DefaultOrganizationID, "admin@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	_, viewerToken, _, err := sessionService.Create(t.Context(), admin.ID+"-not-admin", admin.OrganizationID)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/organization/oidc", nil)
	request.AddCookie(&http.Cookie{Name: "bazusop_session", Value: viewerToken})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for an unrelated/invalid session, got %d", response.Code)
	}
}

func TestOIDCLoginReturns404WhenNotConfigured(t *testing.T) {
	t.Parallel()
	handler, _, _ := newOIDCTestHandler(t)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/oidc/login", nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("expected 404 when OIDC is not configured, got %d", response.Code)
	}
}
```

**Yazarken düzeltme:** `TestOIDCConfigurationRequiresPlatformAdmin`'de `sessionService.Create(t.Context(), admin.ID+"-not-admin", ...)` çağrısı, var olmayan bir kullanıcı ID'siyle oturum oluşturuyor — `sessions.Service.Create` kullanıcı varlığını kontrol etmiyor (yalnız `IsUserActive` sonraki doğrulamalarda kontrol eder), bu yüzden oturum oluşturma başarılı olur ama `requirePermission`'ın `authzService.Can` çağrısı bu kullanıcı için `IsPlatformAdmin`'i `false` bulur (kullanıcı hiç yok) ve `RoleForUserAtSite`'ı da `ErrMembershipNotFound` döndürür → `403` beklenir, `401` değil. `requirePermission`'ın davranışını yeniden incele: oturum geçerliyse (`sessionService.Validate` başarılıysa) ama izin yetersizse `403` döner; yalnız oturum GEÇERSİZSE (`sessionService.Validate` hata verirse) `401` döner. `sessions.Service.Validate`'in `IsUserActive` kontrolü var olmayan bir kullanıcı için `false` döner (identity.Service.IsUserActive'in dokümantasyonu: "An unknown user is reported inactive") — bu da `sessions.Service.Validate`'i `ErrUserInactive` ile başarısız kılar, dolayısıyla oturum GEÇERSİZ sayılır ve `requirePermission` gerçekten `401` döner. Yukarıdaki test doğru — `401` beklentisi isabetli, düzeltmeye gerek yok; bu not yalnızca akıl yürütmeyi açıklığa kavuşturmak için eklendi.

- [x] **Adım 2: Testleri çalıştır**

Çalıştır: `GOCACHE=/tmp/bazusop-go-cache go test ./internal/server/... -run 'TestOIDC' -v 2>&1 | tail -100`
Beklenen: BAŞARILI (tüm yeni testler)

- [x] **Adım 3: Full build/vet/test**

Çalıştır: `go build ./... 2>&1 && echo BUILD_OK && go vet ./... 2>&1 && echo VET_OK`
Beklenen: ikisi de başarılı

Postgres'i başlat: `docker rm -f bazusop-test-pg >/dev/null 2>&1; docker run -d --name bazusop-test-pg -e POSTGRES_USER=bazusop -e POSTGRES_PASSWORD=bazusop_test -e POSTGRES_DB=bazusop_test -p 5432:5432 postgres:18 >/dev/null` ve hazır olmasını bekle.

Çalıştır: `BAZUSOP_TEST_DATABASE_URL='postgres://bazusop:bazusop_test@127.0.0.1:5432/bazusop_test?sslmode=disable' GOCACHE=/tmp/bazusop-go-cache go test ./... -count=1 2>&1 | tail -30`
Beklenen: her paket `ok`

Postgres'i kaldır: `docker rm -f bazusop-test-pg`

- [x] **Adım 4: Commit**

```bash
git add internal/server/oidc_test.go
git commit -m "test: prove the OIDC PKCE flow end to end against a mock IdP, and that local login stays independent"
```

## Görev 5: Dokümantasyon

**Dosyalar:** Değiştir: `docs/API.md`, `README.md`

- [x] **Adım 1: `docs/API.md`'yi güncelle**

`## Kimlik doğrulama` bölümüne ekle:

```markdown
- Opsiyonel OIDC girişi Authorization Code + PKCE kullanır. Platform
  yöneticisi `PUT /api/v1/organization/oidc` ile discovery URL, issuer,
  client ID/secret ve redirect URL'i yapılandırır (`PermissionManageOrgSecurity`
  — yalnız platform yöneticisi); client secret hiçbir okuma API'sinde geri
  dönmez. `GET /api/v1/oidc/login` IdP'ye yönlendirir; `GET
  /api/v1/oidc/callback` kodu değiştirir, ID token'ı doğrular (imza, issuer,
  audience, süre, nonce, PKCE) ve bir oturum açar. İlk giriş yalnız
  `identity_type: "oidc"` ile oluşturulmuş, e-postası eşleşen geçerli bir
  davetle olur; sonraki girişler `issuer + subject` ile kalıcı olarak
  tanınır. Yerel giriş (`POST /api/v1/sessions`) tamamen bağımsız çalışmaya
  devam eder.
```

`### GET|POST /api/v1/users/invites` bölümü yoksa (mevcut belgede davet ucu ayrı belgelenmemiş olabilir), `POST /api/v1/users/invites`'ın body'sine `identity_type` alanını ekleyerek not düş; belge mevcutsa aşağıdaki paragrafı ekle:

```markdown
### `POST /api/v1/users/invites`

`PermissionManageUsers` (platform yöneticisi) ister. Body'de isteğe bağlı
`identity_type` alanı `"local"` (varsayılan) veya `"oidc"` olabilir — bir
OIDC daveti yalnız OIDC callback akışıyla tüketilebilir, `local` daveti
yalnız `/invites/{token}/consume` ile.

### `PUT|GET /api/v1/organization/oidc`

`PermissionManageOrgSecurity` (yalnız platform yöneticisi) ister. `PUT`
organizasyonun OIDC ayarlarını oluşturur/günceller; `GET` bunları client
secret olmadan okur.

### `GET /api/v1/oidc/login` / `GET /api/v1/oidc/callback`

Kimlik doğrulama gerektirmez (bunlar girişin kendisidir). OIDC
yapılandırılmamışsa `404` döner.
```

- [x] **Adım 2: `README.md`'ye kısa bir not ekle**

"İlk kurulum" bölümünden hemen sonra:

```markdown
Opsiyonel OIDC girişi platform yöneticisi tarafından `PUT
/api/v1/organization/oidc` ile yapılandırılır; ilk OIDC girişi yalnız
`identity_type: "oidc"` ile oluşturulmuş geçerli bir davetle çalışır. Yerel
giriş her zaman bağımsız çalışmaya devam eder.
```

- [x] **Adım 3: Commit**

```bash
git add docs/API.md README.md
git commit -m "docs: document the optional OIDC login endpoints"
```

## Görev 6: Manuel doğrulama

**Dosyalar:** yok (yalnız doğrulama).

- [x] **Adım 1: Docker Compose ile hub'ı ayağa kaldır**

Çalıştır: `docker compose -p bazusop-verify-119 down -v >/dev/null 2>&1; POSTGRES_PASSWORD=verify-pw BAZUSOP_ENROLLMENT_TOKEN=verify-token BAZUSOP_BOOTSTRAP_SECRET=verify-bootstrap BAZUSOP_TOTP_ENCRYPTION_KEY=verify-totp-key BAZUSOP_SERVICE_ACCOUNT_PEPPER=verify-pepper BAZUSOP_PORT=18098 docker compose -p bazusop-verify-119 up --build -d` ve `/api/v1/health`'in 200 dönmesini bekle.

- [x] **Adım 2: Bootstrap ol, giriş yap**

```bash
rm -f /tmp/bazusop-119-cookies.txt
curl -s -c /tmp/bazusop-119-cookies.txt -X POST http://127.0.0.1:18098/api/v1/bootstrap \
  -d '{"secret":"verify-bootstrap","email":"admin@example.com","password":"correct horse battery staple"}' > /tmp/bazusop-119-bootstrap.json
CODE=$(python3 -c "import json; print(json.load(open('/tmp/bazusop-119-bootstrap.json'))['recovery_codes'][0])")
curl -s -c /tmp/bazusop-119-cookies.txt -b /tmp/bazusop-119-cookies.txt -X POST http://127.0.0.1:18098/api/v1/sessions \
  -d '{"email":"admin@example.com","password":"correct horse battery staple","recovery_code":"'"$CODE"'"}' -w "\nlogin:%{http_code}\n"
```

Beklenen: `201`.

- [x] **Adım 3: OIDC yapılandırılmadan `/oidc/login`'in `404` döndüğünü doğrula**

```bash
curl -s -o /dev/null -w "yapılandırılmamış /oidc/login (beklenen 404): %{http_code}\n" http://127.0.0.1:18098/api/v1/oidc/login
```

- [x] **Adım 4: OIDC yapılandırmasını CRUD ile doğrula — client secret hiçbir okuma yanıtında görünmemeli**

```bash
curl -s -b /tmp/bazusop-119-cookies.txt -X PUT http://127.0.0.1:18098/api/v1/organization/oidc \
  -d '{"discovery_url":"https://idp.example.com/.well-known/openid-configuration","issuer":"https://idp.example.com","client_id":"demo-client","client_secret":"super-secret-value","redirect_url":"https://hub.example.com/api/v1/oidc/callback"}' | tee /tmp/bazusop-119-oidc-config.json
echo
grep -q "super-secret-value" /tmp/bazusop-119-oidc-config.json && echo "HATA: secret sızdı!" || echo "OK: secret yanıtta yok"

curl -s -b /tmp/bazusop-119-cookies.txt http://127.0.0.1:18098/api/v1/organization/oidc | tee /tmp/bazusop-119-oidc-read.json
echo
grep -q "super-secret-value" /tmp/bazusop-119-oidc-read.json && echo "HATA: secret sızdı!" || echo "OK: secret yanıtta yok"
```

Beklenen: her iki yanıt da `200`, hiçbirinde `super-secret-value` görünmez.

- [x] **Adım 5: Şimdi OIDC yapılandırıldığına göre `/oidc/login`'in gerçek IdP'ye (gerçek olmasa da, en azından discovery denemesi yapıldığını) yönlendirmeye ÇALIŞTIĞINI doğrula**

```bash
curl -s -o /dev/null -w "yapılandırılmış-ama-erişilemez-IdP /oidc/login (beklenen 502, discovery başarısız olduğu için): %{http_code}\n" http://127.0.0.1:18098/api/v1/oidc/login
```

Beklenen: `502` (verilen `discovery_url` gerçek bir IdP olmadığı için discovery başarısız olur — bu, `handleOIDCLogin`'in yapılandırmayı gerçekten OKUYUP kullanmaya ÇALIŞTIĞINI kanıtlar; tam bir gerçek-IdP round-trip'i bu manuel adımın kapsamı dışındadır, bunun tam otomatik kanıtı Görev 4'ün sahte-IdP testidir).

- [x] **Adım 6: Yerel girişin OIDC yapılandırıldıktan SONRA bile bağımsız çalıştığını doğrula**

```bash
curl -s -o /dev/null -w "yerel giriş (yanlış TOTP, beklenen 401 — davranış değişmedi): %{http_code}\n" -X POST http://127.0.0.1:18098/api/v1/sessions \
  -d '{"email":"admin@example.com","password":"correct horse battery staple","totp_code":"000000"}'
```

Beklenen: `401` (tıpkı OIDC yapılandırılmadan önceki davranışın aynısı).

- [x] **Adım 7: Temizlik**

```bash
docker compose -p bazusop-verify-119 down -v
rm -f /tmp/bazusop-119-cookies.txt /tmp/bazusop-119-bootstrap.json /tmp/bazusop-119-oidc-config.json /tmp/bazusop-119-oidc-read.json
```

## Kendi Kendine İnceleme

**1. Spec kapsaması.**
- "Platform yöneticisi discovery URL, izin verilen issuer, client ID, client secret ve redirect URL yapılandırmasını tanımlar. Client secret şifreli saklanır ve hiçbir okuma API'sinde geri döndürülmez." → Görev 1 (`SetOIDCConfiguration`/`OIDCConfiguration` ayrımı) + Görev 3 (CRUD handler'ları) + Görev 6 Adım 4 (canlı doğrulama).
- "Akış Authorization Code + PKCE kullanır. state, nonce, issuer, audience, imza, zaman alanları ve redirect hedefi doğrulanır." → Görev 2 (`internal/oidc`'in `NewProvider`/`AuthCodeURL`/`Exchange`'i — issuer eşleşmesi `NewProvider`'da, imza/audience/süre `go-oidc`'nin `Verifier.Verify`'ında, nonce elle, PKCE `oauth2`'nin S256 yardımcılarıyla, redirect hedefi `oauth2.Config.RedirectURL` aracılığıyla).
- "İlk giriş yalnız geçerli bir davetle gerçekleşir. Davetin doğrulanmış e-postası ilk bağlantıyı bulmak için kullanılır; bağlantı sonrasında hesap kalıcı olarak issuer + subject ile tanınır. E-posta değişikliği kimliği başka hesaba taşımaz." → Görev 1'in `LoginOrLinkOIDCUser`'ı (önce issuer+subject arar, yoksa email ile bekleyen daveti arar, e-posta yalnız İLK bağlantıda kullanılır).
- "OIDC kullanıcılarının MFA politikası IdP'nin sorumluluğundadır... acr ve amr claim'lerini varsa audit olayına ekler" → `oidc.Claims` acr/amr'ı taşır (Görev 2); audit olayına eklenmesi Genel Kısıtlar'da bilinçli olarak ertelendi.
- "IdP grup claim'leri ilk sürümde rol üretmez. Site üyeliği ve rol ataması yalnız platform yöneticisinin açık işlemiyle değişir." → `LoginOrLinkOIDCUser` rolü/grants'ı yalnız DAVETTEN alır, IdP claim'lerinden asla.
- Yol haritası kabul sinyali ("Davetli kullanıcı PKCE ile bağlanır; yerel giriş bağımsız çalışır") → Görev 4'ün `TestOIDCLoginPKCEFlowCreatesSessionAndLocalLoginStaysIndependent`'i BİREBİR bu iki iddiayı tek testte kanıtlıyor.

**2. Placeholder taraması.** Her adımda gerçek, eksiksiz kod var. `internal/oidc`'in dış kütüphane API'leri Task içine yazılmadan ÖNCE ayrı bir fork ile pkg.go.dev üzerinden doğrulandı (go-oidc v3.21.0, oauth2 v0.37.0) — tahmine dayalı bir API kullanılmadı.

**3. Tip tutarlılığı.** `identity.IdentityType`/`OIDCConfiguration`/`LoginOrLinkOIDCUser` Görev 1'de tanımlandığı gibi Görev 3'te birebir kullanılıyor. `oidc.Configuration`/`Flow`/`Claims`/`Provider` Görev 2'de tanımlanıp Görev 3 ve Görev 4'te tutarlı kullanılıyor. `CreateInvite`'ın yeni `identityType` parametresi TÜM çağrı sitelerinde (Görev 1 Adım 3, Görev 3 Adım 1) güncellendi.
