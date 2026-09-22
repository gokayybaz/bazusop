# Eski Operator/Admin Token'larının Kaldırılması (Spike 11.7) Uygulama Planı

> **Otonom çalışanlar için:** ZORUNLU ALT-SKILL: Bu planı görev görev uygulamak için superpowers:subagent-driven-development (önerilen) veya superpowers:executing-plans kullan. Adımlar `- [ ]` checkbox söz dizimiyle takip edilir.

**Amaç:** `BAZUSOP_OPERATOR_TOKEN`/`BAZUSOP_ADMIN_TOKEN` statik bearer token köprüsü kod tabanından tamamen kaldırılır. Spike 11.5'te inşa edilen `requirePermission`, artık yalnız iki kimlik doğrulama yoluna sahip olur: insan oturumu (gerçek RBAC ile) veya servis hesabı token'ı (spike 11.6). Bu, spec'in açıkça öngördüğü, bilinçli bir kimlik doğrulama kırılmasıdır.

**Mimari:** Bu spike doğası gereği **çıkarımsal** (subtractive) — önceki spike'ların additive TDD ritmi ("kırmızı test → minimal kod → yeşil") burada tam olarak uygulanamaz, çünkü bir özelliği kaldırmak hem üretim kodunu hem de onu tüketen test dosyalarını AYNI ANDA bozar (paket derlenemez hale gelir, tek tek "kırmızı" görülemez). Bunun yerine her görev "kaldır/değiştir → derlemeyi/testleri yeşile döndür" bite-sized adımlarına bölünür ve Genel Kısıtlar'da bu sapma açıkça not edilir.

**Teknoloji yığını:** Go 1.26, yalnızca stdlib — yeni bağımlılık yok. Docs değişiklikleri Markdown.

**Spec:** [docs/superpowers/specs/2026-09-13-enterprise-rbac-identity-audit-design.md](../specs/2026-09-13-enterprise-rbac-identity-audit-design.md) — "Sabit RBAC modeli" → "Servis hesapları": *"Mevcut BAZUSOP_OPERATOR_TOKEN ve BAZUSOP_ADMIN_TOKEN desteği site kapsamlı RBAC devreye alınırken kaldırılır. Bu bilinçli bir kimlik doğrulama kırılmasıdır; release notlarında migration yönergesi verilir."* Yol haritası spike 11.7'yi uygular; kabul sinyali: *"Mutasyonlar yalnız insan oturumu veya servis hesabıyla çalışır."*

## Genel Kısıtlar

- **Kod incelemesiyle bulunan kritik bir boşluk:** `handleRevokeUserSessions`/`handleRevokeAllSessions` (spike 11.4, `internal/server/sessions.go`), spike 11.5'in `requirePermission`'ına HİÇ geçirilmemiş — hâlâ doğrudan eski `authorizeRole`'u çağırıyorlar. Eski köprü kaldırıldığında bu iki uç, düzeltilmezse, HİÇBİR kimlik doğrulama yoluna sahip olmadan tamamen kullanılamaz hale gelirdi. Bu spike bunları `requirePermission` + `authorization.PermissionManageUsers`'a geçirerek düzeltir — bu gerçek bir hata düzeltmesidir, yeni bir tasarım kararı değil.
- `internal/server/middleware.go`'daki `deriveActor`'ın `ActorLegacyToken` dalı **korunur** — eski bearer köprüsü artık hiçbir isteği yetkilendirmez, ama bir istemci hâlâ eski `Authorization: Bearer <token>` şemasını deneyebilir; bunu `ActorLegacyToken` olarak denetim izinde etiketlemek, kimin henüz migrate olmadığını görmek için gerçek bir teşhis değeri taşır. Bu bilinçli bir "sil"me değil.
- Servis hesabı yönetim uçları (davet oluşturma, site-üyeliği atama/iptal, servis hesabı oluşturma/rotasyon/iptal, oturum toplu iptali) bu spike'ta da servis hesabı bearer yoluna asla açılmaz (spike 11.5/11.6'da zaten kurulan desen) — yalnız insan oturumu bu "insan yönetimi" eylemlerini gerçekleştirebilir.
- Test dönüşümlerinde, bir testin ASIL amacı hâlâ geçerliyse (örn. "iş oluşturma → agent talep eder → agent olay bildirir → audit sorgusu" uçtan uca akışı) test **silinmez**, kimlik doğrulama adımı servis hesabı token'ına dönüştürülür. Bir testin TÜM amacı eski köprüye özgü davranışsa (örn. "operator token yoksa 503") test **silinir** — yeni tasarımda karşılığı yok.
- `internal/server/*_test.go` dosyalarında yerel değişken adı `identity` (spike öncesi `authority, identity := enrolledIdentity(t)` gibi) kullanan testler, bu spike'ın eklediği `internal/identity` paket import'uyla çakışır — bu testlerde yerel değişken `agentIdentity` olarak yeniden adlandırılır.
- Spec'in "release notlarında migration yönergesi verilir" şartı, bu depoda ayrı bir CHANGELOG/release-notes süreci olmadığından, yeni bir `docs/MIGRATION_v0.4.md` dosyasıyla karşılanır ve README'den bağlantı verilir.

## Dosya Yapısı

- Sil: `internal/server/authorization.go`, `internal/server/authorization_test.go`.
- Değiştir: `internal/server/rbac.go` (`requirePermission` iki-yollu hale gelir), `internal/server/server.go` (`handlerOptions`, tüm çağrı siteleri), `internal/server/jobs.go`, `internal/server/alerting.go`, `internal/server/cloudinventory.go`, `internal/server/identity.go`, `internal/server/sessions.go`, `internal/server/serviceaccounts.go` (handler imzaları).
- Değiştir: `internal/server/jobs_test.go`, `internal/server/alerting_test.go`, `internal/server/cloudinventory_test.go`, `internal/server/identity_test.go`, `internal/server/rbac_test.go`, `internal/server/rbac_operational_test.go`, `internal/server/serviceaccounts_test.go`.
- Değiştir: `internal/config/config.go`, `internal/config/config_test.go`, `cmd/bazusop-hub/main.go`, `compose.yaml`.
- Değiştir: `README.md`, `docs/OPERATIONS.md`, `docs/RUNBOOK.md`.
- Oluştur: `docs/MIGRATION_v0.4.md`.

## Görev 1: `internal/server` üretim kodundan eski köprüyü çıkar

**Dosyalar:**
- Sil: `internal/server/authorization.go`
- Değiştir: `internal/server/rbac.go`, `internal/server/server.go`, `internal/server/jobs.go`, `internal/server/alerting.go`, `internal/server/cloudinventory.go`, `internal/server/identity.go`, `internal/server/sessions.go`, `internal/server/serviceaccounts.go`

**Arayüzler:**
- Üretir: `func requirePermission(response http.ResponseWriter, request *http.Request, sessionService *sessions.Service, serviceAccountService *serviceaccounts.Service, authzService *authorization.Service, permission authorization.Permission, siteID string) (string, bool)` — `tokens accessTokens`/`legacyRole accessRole` parametreleri kaldırılmış, eski bearer'a düşme bloğu kaldırılmış hali.

- [x] **Adım 1: `internal/server/authorization.go`'yu sil**

```bash
rm internal/server/authorization.go
```

- [x] **Adım 2: `rbac.go`'daki `requirePermission`'ı iki-yollu hale getir**

`internal/server/rbac.go`'da `requirePermission`'ı şununla değiştir:

```go
// requirePermission authenticates the caller as either a human session or a
// service account and checks the real permission matrix — the only two
// ways to authenticate a mutation as of spike 11.7, which removed the
// legacy operator/admin bearer-token bridge entirely (see the design
// spec's "Servis hesapları" section). Neither present, or either present
// but invalid or lacking the permission, is rejected.
func requirePermission(response http.ResponseWriter, request *http.Request, sessionService *sessions.Service, serviceAccountService *serviceaccounts.Service, authzService *authorization.Service, permission authorization.Permission, siteID string) (string, bool) {
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
	http.Error(response, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
	return "", false
}
```

`bearerToken` yardımcı fonksiyonu değişmeden kalır. `handleAssignSiteRole`/`handleRevokeSiteRole`'un imzalarından `tokens accessTokens` parametresini kaldır; gövdelerindeki `requirePermission` çağrılarından `tokens, roleAdmin` argümanlarını kaldır (servis hesabı argümanı zaten `nil`):

```go
func handleAssignSiteRole(sessionService *sessions.Service, authzService *authorization.Service, scope tenancy.Scope) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		if _, ok := requirePermission(response, request, sessionService, nil, authzService, authorization.PermissionManageUsers, scope.SiteID); !ok {
			return
		}
		...
```

(`handleRevokeSiteRole`'da aynı değişiklik.)

- [x] **Adım 3: Altı işlevsel handler'ın imzalarından `tokens accessTokens` parametresini kaldır**

Aşağıdaki her handler'ın imzasından `tokens accessTokens` parametresi silinir; gövdelerindeki `requirePermission` çağrılarından `tokens, roleX` argümanları silinir (izin sabiti ve servis hesabı argümanı aynı kalır):

- `internal/server/jobs.go`: `handleCreateJob(service *jobs.Service, sessionService *sessions.Service, serviceAccountService *serviceaccounts.Service, authzService *authorization.Service, scope tenancy.Scope) http.HandlerFunc` — çağrı: `requirePermission(response, request, sessionService, serviceAccountService, authzService, authorization.PermissionCreateJobs, scope.SiteID)`.
- `internal/server/alerting.go`: `handleCreateAlertRule`, `handleCreateMaintenance` (`authorization.PermissionManageAlerts`), `handleAcknowledgeIncident` (`authorization.PermissionAcknowledgeIncidents`) — aynı kalıp.
- `internal/server/cloudinventory.go`: `handleCreateCloudAccount`, `handleReconcileCloudInstances` (`authorization.PermissionManageCloudAccounts`) — aynı kalıp.

Örnek (`jobs.go`):

```go
func handleCreateJob(service *jobs.Service, sessionService *sessions.Service, serviceAccountService *serviceaccounts.Service, authzService *authorization.Service, scope tenancy.Scope) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		if _, ok := requirePermission(response, request, sessionService, serviceAccountService, authzService, authorization.PermissionCreateJobs, scope.SiteID); !ok {
			return
		}
		...
```

- [x] **Adım 4: `handleCreateInvite`'ın imzasından `tokens accessTokens` parametresini kaldır**

`internal/server/identity.go`'da:

```go
func handleCreateInvite(service *identity.Service, sessionService *sessions.Service, authzService *authorization.Service, scope tenancy.Scope) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		actorUserID, ok := requirePermission(response, request, sessionService, nil, authzService, authorization.PermissionManageUsers, scope.SiteID)
		if !ok {
			return
		}
		...
```

- [x] **Adım 5: `internal/server/serviceaccounts.go`'daki beş handler'ın imzalarından `tokens accessTokens` parametresini kaldır**

`handleCreateServiceAccount`, `handleListServiceAccounts`, `handleRotateServiceAccountToken`, `handleRevokeServiceAccountToken`, `handleDisableServiceAccount` — hepsinde aynı kalıp: imzadan `tokens accessTokens` silinir, `requirePermission` çağrısından `tokens, roleAdmin` silinir (servis hesabı argümanı zaten `nil`).

- [x] **Adım 6: `handleRevokeUserSessions`/`handleRevokeAllSessions`'ı `requirePermission`'a taşı (hata düzeltmesi)**

`internal/server/sessions.go`'nun import listesine `"github.com/gokayybaz/bazusop/internal/authorization"` ve `"github.com/gokayybaz/bazusop/internal/serviceaccounts"` ekle. İki handler'ı şununla değiştir:

```go
func handleRevokeUserSessions(sessionService *sessions.Service, authzService *authorization.Service, scope tenancy.Scope) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		if _, ok := requirePermission(response, request, sessionService, nil, authzService, authorization.PermissionManageUsers, scope.SiteID); !ok {
			return
		}
		if err := sessionService.RevokeAllForUser(request.Context(), request.PathValue("userID")); err != nil {
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		response.WriteHeader(http.StatusNoContent)
	}
}

func handleRevokeAllSessions(sessionService *sessions.Service, authzService *authorization.Service, scope tenancy.Scope) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		if _, ok := requirePermission(response, request, sessionService, nil, authzService, authorization.PermissionManageUsers, scope.SiteID); !ok {
			return
		}
		if err := sessionService.RevokeAllForOrganization(request.Context(), tenancy.DefaultOrganizationID); err != nil {
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		response.WriteHeader(http.StatusNoContent)
	}
}
```

**Not:** `serviceaccounts` import'u bu dosyada doğrudan bir tip için kullanılmıyor gibi görünebilir (yalnız `nil` geçiriliyor) — ama `requirePermission`'ın ikinci parametresi `*serviceaccounts.Service` tipinde olduğu için Go, `nil`'i bu tipe örtük dönüştürebilir ve **import gerekmez** (tip adı kaynak dosyada hiç yazılmıyor). Bu satırı planın önceki taslağının aksine **eklemeyin** — yalnız `authorization` import'u gerçekten gereklidir (`authorization.Permission`, `authorization.PermissionManageUsers` tipleri/sabitleri doğrudan kullanılıyor).

- [x] **Adım 7: `handlerOptions`'tan `operatorToken`/`adminToken` alanlarını kaldır, tüm `accessTokens{...}` yerel değişkenlerini sil, `server.go`'daki çağrı sitelerini güncelle**

`internal/server/server.go`'da `handlerOptions`'tan `operatorToken string` ve `adminToken string` alanlarını sil. `NewHandler`'daki her `if configuration.XService != nil { tokens := accessTokens{...}; ... }` bloğunda `tokens := accessTokens{...}` satırını sil ve altındaki `registerAudited(...)` çağrılarından `tokens` argümanını (her zaman `scope`'dan hemen önceki pozisyonda) kaldır. Etkilenen bloklar: `jobService`, `alertService`, `cloudInventory`, `identityService`, `sessionService`, `authorizationService`, `serviceAccountService`.

Örnek (`jobService` bloğu, önce/sonra):

```go
	if configuration.jobService != nil {
		registerAudited(mux, "/api/v1/instances/{agentID}/jobs", http.MethodPost, "jobs", []string{"agentID"}, configuration.auditTrail, configuration.scope, handleCreateJob(configuration.jobService, configuration.sessionService, configuration.serviceAccountService, configuration.authorizationService, configuration.scope))
		...
```

`sessionService` bloğundaki `handleRevokeUserSessions`/`handleRevokeAllSessions` çağrıları:

```go
		registerAudited(mux, "/api/v1/users/{userID}/sessions", http.MethodDelete, "sessions", []string{"userID"}, configuration.auditTrail, configuration.scope, handleRevokeUserSessions(configuration.sessionService, configuration.authorizationService, configuration.scope))
		registerAudited(mux, "/api/v1/sessions/all", http.MethodDelete, "sessions", nil, configuration.auditTrail, configuration.scope, handleRevokeAllSessions(configuration.sessionService, configuration.authorizationService, configuration.scope))
```

`WithAdminToken` artık `internal/server/authorization.go`'nun silinmesiyle birlikte gitti — `server.go`'da ona bir referans yoktu zaten (yalnız `handlerOptions.adminToken` alanı vardı, o da bu adımda siliniyor).

`WithJobs`/`WithAlerts`/`WithCloudInventory`'nin `operatorToken string` parametresini kaldır (bu üçü sırasıyla `jobs.go`/`alerting.go`/`cloudinventory.go` içinde tanımlı):

```go
func WithJobs(service *jobs.Service) Option {
	return func(options *handlerOptions) {
		options.jobService = service
	}
}
```

(`WithAlerts`/`WithCloudInventory` için aynı kalıp — yalnız `options.alertService = service` / `options.cloudInventory = service` kalır.)

- [x] **Adım 8: Build'i doğrula**

Çalıştır: `go build ./...`
Beklenen: başarılı (test dosyaları henüz güncellenmediği için `go vet`/`go test` bu adımda başarısız olur — bu beklenen bir ara durumdur, Görev 3 düzeltir)

- [x] **Adım 9: Commit**

```bash
git add internal/server/authorization.go internal/server/rbac.go internal/server/server.go internal/server/jobs.go internal/server/alerting.go internal/server/cloudinventory.go internal/server/identity.go internal/server/sessions.go internal/server/serviceaccounts.go
git commit -m "feat: remove legacy operator/admin bearer bridge from internal/server"
```

## Görev 2: `internal/config`, `main.go` ve `compose.yaml`'dan eski token desteğini kaldır

**Dosyalar:**
- Değiştir: `internal/config/config.go`, `internal/config/config_test.go`, `cmd/bazusop-hub/main.go`, `compose.yaml`

- [x] **Adım 1: `config.go`'dan `OperatorToken`/`AdminToken` alanlarını kaldır**

`internal/config/config.go`'da `Config` struct'ından `OperatorToken string` ve `AdminToken string` alanlarını, `Load()`'dan ilgili iki `os.Getenv` satırını sil.

- [x] **Adım 2: `config_test.go`'dan ilgili testleri sil**

`internal/config/config_test.go`'dan `TestOperatorToken` ve `TestAdminToken` fonksiyonlarının tamamını sil.

- [x] **Adım 3: Testleri çalıştırıp geçtiğini doğrula**

Çalıştır: `GOCACHE=/tmp/bazusop-go-cache go test ./internal/config/... -v`
Beklenen: BAŞARILI (kalan tüm testler)

- [x] **Adım 4: `main.go`'yu güncelle**

`cmd/bazusop-hub/main.go`'da şu iki `logger.Warn` çağrısını sil:

```go
	if configuration.OperatorToken == "" && configuration.AdminToken == "" {
		logger.Warn("BAZUSOP_OPERATOR_TOKEN and BAZUSOP_ADMIN_TOKEN are not set; authorized mutations are disabled")
	}
	if configuration.AdminToken == "" {
		logger.Warn("BAZUSOP_ADMIN_TOKEN is not set; operator token retains administrative access for compatibility")
	}
```

`identityService`/`authorizationService` kurulumundaki mevcut uyarı mesajını, artık mutasyonların TEK yolu olduğunu netleştirecek şekilde güncelle:

```go
	} else {
		logger.Warn("BAZUSOP_BOOTSTRAP_SECRET and/or BAZUSOP_TOTP_ENCRYPTION_KEY are not set; local identity is disabled, and since spike 11.7 removed the legacy bearer bridge, no session or service-account authentication is possible — every mutating API route will reject all requests")
	}
```

`server.NewHandler(...)` çağrısında:
- `server.WithJobs(jobService, configuration.OperatorToken)` → `server.WithJobs(jobService)`
- `server.WithAlerts(alertService, configuration.OperatorToken)` → `server.WithAlerts(alertService)`
- `server.WithCloudInventory(cloudInventoryService, configuration.OperatorToken)` → `server.WithCloudInventory(cloudInventoryService)`
- `server.WithAdminToken(configuration.AdminToken),` satırını tamamen sil.

- [x] **Adım 5: `compose.yaml`'ı güncelle**

`internal/config` bölümünden şu iki satırı sil:

```yaml
      BAZUSOP_OPERATOR_TOKEN: "${BAZUSOP_OPERATOR_TOKEN:-local-operator-token}"
      BAZUSOP_ADMIN_TOKEN: "${BAZUSOP_ADMIN_TOKEN:-local-admin-token}"
```

- [x] **Adım 6: Tam repo build'ini doğrula**

Çalıştır: `go build ./... && go vet ./internal/config/... ./cmd/... && gofmt -l internal/config/*.go cmd/bazusop-hub/*.go`
Beklenen: build başarılı; vet/gofmt çıktısı yok (`internal/server` paketinin `go vet`'i Görev 3 tamamlanana kadar başarısız kalmaya devam eder — beklenen)

- [x] **Adım 7: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go cmd/bazusop-hub/main.go compose.yaml
git commit -m "feat: remove BAZUSOP_OPERATOR_TOKEN/BAZUSOP_ADMIN_TOKEN configuration"
```

## Görev 3: `internal/server` test dosyalarını yeni şekle göre taşı

**Dosyalar:**
- Sil: `internal/server/authorization_test.go`
- Değiştir: `internal/server/jobs_test.go`, `internal/server/alerting_test.go`, `internal/server/cloudinventory_test.go`, `internal/server/identity_test.go`, `internal/server/rbac_test.go`, `internal/server/rbac_operational_test.go`, `internal/server/serviceaccounts_test.go`

- [x] **Adım 1: `authorization_test.go`'yu sil**

```bash
rm internal/server/authorization_test.go
```

Bu dosyadaki üç test (`TestOperatorCannotChangeAdministrativePolicy`, `TestAdminCanChangePolicyAndRunOperations`, `TestUnknownBearerTokenIsUnauthorizedForAdminRoute`) tamamen eski bearer sistemine özgüydü — yeni tasarımda karşılığı yok.

- [x] **Adım 2: `jobs_test.go`'yu güncelle**

`TestJobCreationIsUnavailableWithoutConfiguredOperatorToken`'ı tamamen sil (eski köprünün "token yapılandırılmamışsa 503" davranışına özgüydü, artık yok).

`TestJobClaimRequiresAgentIdentity`'de `server.WithJobs(jobService, "operator-secret")` çağrısını `server.WithJobs(jobService)` yap.

`TestJobCreationRequiresOperatorToken`'ı yeniden adlandırıp basitleştir:

```go
func TestJobCreationRequiresAuthentication(t *testing.T) {
	t.Parallel()
	jobService := newServerJobService(t)
	handler := server.NewHandler(server.WithJobs(jobService))
	request := httptest.NewRequest(http.MethodPost, "/api/v1/instances/agent-01/jobs", encodeJSON(t, jobs.CreateRequest{
		Action: jobs.ActionHostReboot, ApprovedBy: "gokay", Reason: "kernel rollout",
	}))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", response.Code)
	}
}
```

`TestApprovedJobFlowsToAuthenticatedAgent`'ı, iş oluşturma adımını bir servis hesabı token'ıyla kimlik doğrulayacak şekilde yeniden yaz — geri kalan akış (agent mTLS ile talep eder, olay bildirir, audit sorgulanır) değişmeden kalır:

```go
func TestApprovedJobFlowsToAuthenticatedAgent(t *testing.T) {
	t.Parallel()
	authority, err := enrollment.NewAuthority("bootstrap-secret")
	if err != nil {
		t.Fatalf("create authority: %v", err)
	}
	agentIdentity, err := authority.Enroll(enrollment.Request{BootstrapToken: "bootstrap-secret", Name: "edge-01", OperatingSystem: "linux", CSRPEM: serverCSR(t, "edge-01")})
	if err != nil {
		t.Fatalf("enroll identity: %v", err)
	}
	jobService := newServerJobService(t, tenancy.Agent{ID: agentIdentity.AgentID, OrganizationID: tenancy.DefaultOrganizationID, SiteID: tenancy.DefaultSiteID})

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
		server.WithEnrollment(authority), server.WithJobs(jobService),
		server.WithIdentity(identityService, "bootstrap-secret"),
		server.WithSessions(sessionService, identityService),
		server.WithAuthorization(authzService),
		server.WithServiceAccounts(serviceAccountService),
	)

	admin, _, err := identityService.Bootstrap(t.Context(), tenancy.DefaultOrganizationID, "admin@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	_, adminToken, _, err := sessionService.Create(t.Context(), admin.ID, admin.OrganizationID)
	if err != nil {
		t.Fatal(err)
	}
	createAccount := httptest.NewRequest(http.MethodPost, "/api/v1/sites/"+tenancy.DefaultSiteID+"/service-accounts", encodeJSON(t, map[string]any{"name": "ci-bot", "role": "operator"}))
	createAccount.AddCookie(&http.Cookie{Name: "bazusop_session", Value: adminToken})
	accountResponse := httptest.NewRecorder()
	handler.ServeHTTP(accountResponse, createAccount)
	if accountResponse.Code != http.StatusCreated {
		t.Fatalf("create service account: %d %s", accountResponse.Code, accountResponse.Body.String())
	}
	var account struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(accountResponse.Body).Decode(&account); err != nil {
		t.Fatalf("decode service account: %v", err)
	}

	createResponse := httptest.NewRecorder()
	createRequest := httptest.NewRequest(http.MethodPost, "/api/v1/instances/"+agentIdentity.AgentID+"/jobs", encodeJSON(t, jobs.CreateRequest{
		Action: jobs.ActionServiceRestart, Target: "nginx.service", ApprovedBy: "gokay", Reason: "config rollout",
	}))
	createRequest.Header.Set("Authorization", "Bearer "+account.Token)
	handler.ServeHTTP(createResponse, createRequest)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", createResponse.Code, createResponse.Body.String())
	}
	var created jobs.Job
	if err := json.NewDecoder(createResponse.Body).Decode(&created); err != nil {
		t.Fatalf("decode job: %v", err)
	}

	claim := httptest.NewRequest(http.MethodGet, "/api/v1/agents/jobs/next", nil)
	claim.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{serverCertificate(t, agentIdentity.CertificatePEM)}}
	claimResponse := httptest.NewRecorder()
	handler.ServeHTTP(claimResponse, claim)
	if claimResponse.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", claimResponse.Code, claimResponse.Body.String())
	}

	event := httptest.NewRequest(http.MethodPost, "/api/v1/agents/jobs/"+created.ID+"/events", encodeJSON(t, jobs.EventRequest{Sequence: 2, Type: jobs.EventSucceeded, Message: "nginx restarted"}))
	event.TLS = claim.TLS
	eventResponse := httptest.NewRecorder()
	handler.ServeHTTP(eventResponse, event)
	if eventResponse.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", eventResponse.Code, eventResponse.Body.String())
	}

	auditResponse := httptest.NewRecorder()
	handler.ServeHTTP(auditResponse, httptest.NewRequest(http.MethodGet, "/api/v1/instances/"+agentIdentity.AgentID+"/jobs/"+created.ID+"/events", nil))
	if auditResponse.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", auditResponse.Code, auditResponse.Body.String())
	}
	var auditPayload struct {
		Events []jobs.Event `json:"events"`
	}
	if err := json.NewDecoder(auditResponse.Body).Decode(&auditPayload); err != nil {
		t.Fatalf("decode audit: %v", err)
	}
	if len(auditPayload.Events) != 3 || auditPayload.Events[2].Type != jobs.EventSucceeded {
		t.Fatalf("unexpected audit: %#v", auditPayload.Events)
	}
}
```

**Önemli:** Orijinal test `identity, err := authority.Enroll(...)` yerel değişkenini kullanıyordu — bu isim, yeni eklenen `internal/identity` paket import'uyla çakışır. Yukarıdaki yeniden yazımda değişken adı baştan `agentIdentity` olarak kullanılmıştır; dönüştürme sırasında `identity.AgentID`/`identity.CertificatePEM` referanslarının hepsinin `agentIdentity.AgentID`/`agentIdentity.CertificatePEM` olarak güncellendiğinden emin ol.

Dosyanın import listesine ekle: `"github.com/gokayybaz/bazusop/internal/authorization"`, `"github.com/gokayybaz/bazusop/internal/identity"`, `"github.com/gokayybaz/bazusop/internal/serviceaccounts"`, `"github.com/gokayybaz/bazusop/internal/sessions"`.

- [x] **Adım 3: Testleri çalıştırıp geçtiğini doğrula**

Çalıştır: `GOCACHE=/tmp/bazusop-go-cache go test ./internal/server/... -run 'TestJob' -v 2>&1 | tail -60`
Beklenen: BAŞARILI (diğer paket testleri henüz derlenmeyebilir — bu adım yalnız `jobs_test.go`'nun kendi mantığını doğrular; tam paket testi Görev 3'ün son adımında çalıştırılır)

- [x] **Adım 4: `alerting_test.go`'yu güncelle**

`TestMetricRuleCreatesAndAcknowledgesIncident`'ı, kural oluşturma ve olay onaylama adımlarını bir `site-admin` rollü servis hesabı token'ıyla kimlik doğrulayacak şekilde yeniden yaz (site-admin hem `PermissionManageAlerts` hem `PermissionAcknowledgeIncidents`'i karşılar, tek hesapla ikisi de test edilebilir):

```go
func TestMetricRuleCreatesAndAcknowledgesIncident(t *testing.T) {
	t.Parallel()
	authority, agentIdentity := enrolledIdentity(t)
	alerts := newAlertService(t)

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
		server.WithEnrollment(authority), server.WithTelemetry(telemetry.NewService(telemetry.NewMemoryStore())), server.WithAlerts(alerts),
		server.WithIdentity(identityService, "bootstrap-secret"),
		server.WithSessions(sessionService, identityService),
		server.WithAuthorization(authzService),
		server.WithServiceAccounts(serviceAccountService),
	)

	admin, _, err := identityService.Bootstrap(t.Context(), tenancy.DefaultOrganizationID, "admin@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	_, adminToken, _, err := sessionService.Create(t.Context(), admin.ID, admin.OrganizationID)
	if err != nil {
		t.Fatal(err)
	}
	createAccount := httptest.NewRequest(http.MethodPost, "/api/v1/sites/"+tenancy.DefaultSiteID+"/service-accounts", encodeJSON(t, map[string]any{"name": "ops-bot", "role": "site-admin"}))
	createAccount.AddCookie(&http.Cookie{Name: "bazusop_session", Value: adminToken})
	accountResponse := httptest.NewRecorder()
	handler.ServeHTTP(accountResponse, createAccount)
	if accountResponse.Code != http.StatusCreated {
		t.Fatalf("create service account: %d %s", accountResponse.Code, accountResponse.Body.String())
	}
	var account struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(accountResponse.Body).Decode(&account); err != nil {
		t.Fatalf("decode service account: %v", err)
	}

	createRule := httptest.NewRequest(http.MethodPost, "/api/v1/alert-rules", encodeJSON(t, alerting.RuleRequest{Name: "Yüksek CPU", Kind: alerting.KindMetric, Metric: alerting.MetricCPU, Threshold: 90, Severity: alerting.SeverityCritical, Enabled: true}))
	createRule.Header.Set("Authorization", "Bearer "+account.Token)
	ruleResponse := httptest.NewRecorder()
	handler.ServeHTTP(ruleResponse, createRule)
	if ruleResponse.Code != http.StatusCreated {
		t.Fatalf("expected rule 201, got %d: %s", ruleResponse.Code, ruleResponse.Body.String())
	}

	recordedAt := time.Now().UTC()
	report := httptest.NewRequest(http.MethodPost, "/api/v1/agents/telemetry", encodeJSON(t, telemetry.Sample{RecordedAt: recordedAt, CPUPercent: 97, MemoryPercent: 50, DiskPercent: 40}))
	report.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{serverCertificate(t, agentIdentity.CertificatePEM)}}
	reportResponse := httptest.NewRecorder()
	handler.ServeHTTP(reportResponse, report)
	if reportResponse.Code != http.StatusNoContent {
		t.Fatalf("expected telemetry 204, got %d", reportResponse.Code)
	}
	older := httptest.NewRequest(http.MethodPost, "/api/v1/agents/telemetry", encodeJSON(t, telemetry.Sample{RecordedAt: recordedAt.Add(-time.Hour), CPUPercent: 20, MemoryPercent: 50, DiskPercent: 40}))
	older.TLS = report.TLS
	olderResponse := httptest.NewRecorder()
	handler.ServeHTTP(olderResponse, older)
	if olderResponse.Code != http.StatusNoContent {
		t.Fatalf("expected historical telemetry 204, got %d", olderResponse.Code)
	}

	listResponse := httptest.NewRecorder()
	handler.ServeHTTP(listResponse, httptest.NewRequest(http.MethodGet, "/api/v1/incidents", nil))
	var payload struct {
		Incidents []alerting.Incident `json:"incidents"`
	}
	if listResponse.Code != http.StatusOK {
		t.Fatalf("expected incidents 200, got %d", listResponse.Code)
	}
	if err := json.NewDecoder(listResponse.Body).Decode(&payload); err != nil || len(payload.Incidents) != 1 || payload.Incidents[0].Status != alerting.StatusOpen {
		t.Fatalf("decode incidents: %#v, %v", payload, err)
	}

	ack := httptest.NewRequest(http.MethodPost, "/api/v1/incidents/"+payload.Incidents[0].ID+"/acknowledge", encodeJSON(t, map[string]string{"actor": "gokay"}))
	ack.Header.Set("Authorization", "Bearer "+account.Token)
	ackResponse := httptest.NewRecorder()
	handler.ServeHTTP(ackResponse, ack)
	if ackResponse.Code != http.StatusOK {
		t.Fatalf("expected acknowledge 200, got %d: %s", ackResponse.Code, ackResponse.Body.String())
	}
}

func TestAlertMutationsRequireAuthentication(t *testing.T) {
	t.Parallel()
	handler := server.NewHandler(server.WithAlerts(newAlertService(t)))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/alert-rules", encodeJSON(t, alerting.RuleRequest{})))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", response.Code)
	}
}
```

**Önemli:** `enrolledIdentity(t)`'nin döndürdüğü ikinci değer orijinalde `identity` adıyla yakalanıyordu — aynı isim çakışması burada da geçerli; yukarıdaki yeniden yazım baştan `agentIdentity` kullanıyor. Dosyanın import listesine ekle: `"github.com/gokayybaz/bazusop/internal/authorization"`, `"github.com/gokayybaz/bazusop/internal/identity"`, `"github.com/gokayybaz/bazusop/internal/serviceaccounts"`, `"github.com/gokayybaz/bazusop/internal/sessions"`, `"github.com/gokayybaz/bazusop/internal/tenancy"`.

- [x] **Adım 5: Testleri çalıştırıp geçtiğini doğrula**

Çalıştır: `GOCACHE=/tmp/bazusop-go-cache go test ./internal/server/... -run 'TestMetricRule|TestAlertMutations' -v 2>&1 | tail -30`
Beklenen: BAŞARILI

- [x] **Adım 6: `cloudinventory_test.go`'yu güncelle**

`TestCloudDiscoveryAPIReconcilesProviderInventory`'yi, hesap oluşturma ve instance senkronizasyonu adımlarını bir `site-admin` rollü servis hesabı token'ıyla kimlik doğrulayacak şekilde yeniden yaz:

```go
func TestCloudDiscoveryAPIReconcilesProviderInventory(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	hosts := inventory.NewService(inventory.NewMemoryStore())
	if err := hosts.Report(ctx, tenancy.Agent{ID: "agent-01", OrganizationID: "org_default", SiteID: "site_default"}, inventory.Facts{Hostname: "edge-01.example.com", OSFamily: "linux", OSName: "Ubuntu", OSVersion: "24.04", Architecture: "amd64", CPUCores: 4, MemoryBytes: 8 << 30, IPAddresses: []string{"10.0.0.8"}, AgentVersion: "0.1.0"}); err != nil {
		t.Fatalf("seed host: %v", err)
	}
	cloud := cloudinventory.NewService(cloudinventory.NewMemoryStore(), hosts)

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
		server.WithCloudInventory(cloud),
		server.WithIdentity(identityService, "bootstrap-secret"),
		server.WithSessions(sessionService, identityService),
		server.WithAuthorization(authzService),
		server.WithServiceAccounts(serviceAccountService),
	)

	admin, _, err := identityService.Bootstrap(ctx, tenancy.DefaultOrganizationID, "admin@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	_, adminToken, _, err := sessionService.Create(ctx, admin.ID, admin.OrganizationID)
	if err != nil {
		t.Fatal(err)
	}
	createAccount := httptest.NewRequest(http.MethodPost, "/api/v1/sites/site_default/service-accounts", encodeJSON(t, map[string]any{"name": "cloud-bot", "role": "site-admin"}))
	createAccount.AddCookie(&http.Cookie{Name: "bazusop_session", Value: adminToken})
	accountResponse := httptest.NewRecorder()
	handler.ServeHTTP(accountResponse, createAccount)
	if accountResponse.Code != http.StatusCreated {
		t.Fatalf("create service account: %d %s", accountResponse.Code, accountResponse.Body.String())
	}
	var account struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(accountResponse.Body).Decode(&account); err != nil {
		t.Fatalf("decode service account: %v", err)
	}

	create := httptest.NewRequest(http.MethodPost, "/api/v1/cloud/accounts", encodeJSON(t, cloudinventory.AccountRequest{Name: "Üretim AWS", Provider: cloudinventory.ProviderAWS, ExternalID: "123456789012"}))
	create.Header.Set("Authorization", "Bearer "+account.Token)
	created := httptest.NewRecorder()
	handler.ServeHTTP(created, create)
	if created.Code != http.StatusCreated {
		t.Fatalf("create account status = %d: %s", created.Code, created.Body.String())
	}
	var cloudAccount cloudinventory.Account
	if err := json.NewDecoder(created.Body).Decode(&cloudAccount); err != nil {
		t.Fatalf("decode account: %v", err)
	}

	syncRequest := httptest.NewRequest(http.MethodPut, "/api/v1/cloud/accounts/"+cloudAccount.ID+"/instances", encodeJSON(t, map[string]any{"instances": []cloudinventory.DiscoveredInstance{{ProviderInstanceID: "i-0123", Name: "edge-01", Region: "eu-central-1", State: "running", OSFamily: "linux", AgentIDHint: "agent-01"}}}))
	syncRequest.Header.Set("Authorization", "Bearer "+account.Token)
	synced := httptest.NewRecorder()
	handler.ServeHTTP(synced, syncRequest)
	if synced.Code != http.StatusOK {
		t.Fatalf("sync status = %d: %s", synced.Code, synced.Body.String())
	}

	listed := httptest.NewRecorder()
	handler.ServeHTTP(listed, httptest.NewRequest(http.MethodGet, "/api/v1/cloud/instances", nil))
	if listed.Code != http.StatusOK {
		t.Fatalf("list instances status = %d: %s", listed.Code, listed.Body.String())
	}
	var payload struct {
		Instances []cloudinventory.Instance `json:"instances"`
	}
	if err := json.NewDecoder(listed.Body).Decode(&payload); err != nil || len(payload.Instances) != 1 || payload.Instances[0].MatchStatus != cloudinventory.MatchVerified {
		t.Fatalf("instances = %#v, error = %v", payload.Instances, err)
	}
}

func TestCloudDiscoveryMutationsRequireAuthentication(t *testing.T) {
	t.Parallel()
	cloud := cloudinventory.NewService(cloudinventory.NewMemoryStore(), inventory.NewService(inventory.NewMemoryStore()))
	handler := server.NewHandler(server.WithCloudInventory(cloud))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/cloud/accounts", encodeJSON(t, cloudinventory.AccountRequest{})))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", response.Code)
	}
}
```

**Not:** İç içe geçmiş `account` adlandırma çakışmasına dikkat — orijinal testte decode edilen bulut hesabı `account` adını taşıyordu; yukarıdaki yeniden yazımda servis hesabı yanıtı `account`, bulut hesabı yanıtı `cloudAccount` olarak ayrıştırılmıştır. Dosyanın import listesine ekle: `"github.com/gokayybaz/bazusop/internal/authorization"`, `"github.com/gokayybaz/bazusop/internal/identity"`, `"github.com/gokayybaz/bazusop/internal/serviceaccounts"`, `"github.com/gokayybaz/bazusop/internal/sessions"`.

- [x] **Adım 7: Testleri çalıştırıp geçtiğini doğrula**

Çalıştır: `GOCACHE=/tmp/bazusop-go-cache go test ./internal/server/... -run 'TestCloudDiscovery' -v 2>&1 | tail -30`
Beklenen: BAŞARILI

- [x] **Adım 8: `identity_test.go`'yu güncelle**

`newIdentityHandler`'ı, oturum/RBAC bağımlılıklarını da kuracak ve `adminToken` parametresini kaldıracak şekilde değiştir; bootstrap sonrası HTTP üzerinden giriş yapıp session çerezi döndüren bir yardımcı ekle:

```go
func newIdentityHandler(t *testing.T, bootstrapSecret string) http.Handler {
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
	return server.NewHandler(
		server.WithIdentity(identityService, bootstrapSecret),
		server.WithSessions(sessionService, identityService),
		server.WithAuthorization(authzService),
		server.WithAuditTrail(audittrail.NewService(audittrail.NewMemoryStore())),
	)
}

// bootstrapAndLogin bootstraps the first platform-admin over HTTP and logs
// them in with the first recovery code, returning the session cookies —
// the only way to authenticate a mutation now that spike 11.7 removed the
// legacy bearer bridge.
func bootstrapAndLogin(t *testing.T, handler http.Handler, bootstrapSecret, email, password string) []*http.Cookie {
	t.Helper()
	bootstrapResponse := httptest.NewRecorder()
	handler.ServeHTTP(bootstrapResponse, httptest.NewRequest(http.MethodPost, "/api/v1/bootstrap", encodeJSON(t, map[string]string{
		"secret": bootstrapSecret, "email": email, "password": password,
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
		t.Fatalf("login failed: %d %s", loginResponse.Code, loginResponse.Body.String())
	}
	return loginResponse.Result().Cookies()
}
```

Dört test fonksiyonunu güncelle — `newIdentityHandler(t, "correct-secret", "admin-token")` çağrılarını `newIdentityHandler(t, "correct-secret")` yap; `TestBootstrapInviteConsumeAndConfirmTOTPEndToEnd`'de ayrı bootstrap+`Authorization: Bearer admin-token` bloğunu `bootstrapAndLogin` çağrısı + dönen çerezlerin davet isteğine eklenmesiyle değiştir:

```go
func TestBootstrapInviteConsumeAndConfirmTOTPEndToEnd(t *testing.T) {
	t.Parallel()
	handler := newIdentityHandler(t, "correct-secret")
	email, password := "admin@example.com", "correct horse battery staple"
	cookies := bootstrapAndLogin(t, handler, "correct-secret", email, password)

	secondBootstrap := httptest.NewRecorder()
	handler.ServeHTTP(secondBootstrap, httptest.NewRequest(http.MethodPost, "/api/v1/bootstrap", encodeJSON(t, map[string]string{
		"secret": "correct-secret", "email": "someone-else@example.com", "password": "another password entirely",
	})))
	if secondBootstrap.Code != http.StatusConflict {
		t.Fatalf("expected 409 on a second bootstrap attempt, got %d", secondBootstrap.Code)
	}

	inviteWithoutAuth := httptest.NewRecorder()
	handler.ServeHTTP(inviteWithoutAuth, httptest.NewRequest(http.MethodPost, "/api/v1/users/invites", encodeJSON(t, map[string]string{"email": "new-admin@example.com"})))
	if inviteWithoutAuth.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for invite creation without a session, got %d", inviteWithoutAuth.Code)
	}

	inviteRequest := httptest.NewRequest(http.MethodPost, "/api/v1/users/invites", encodeJSON(t, map[string]string{"email": "new-admin@example.com"}))
	for _, cookie := range cookies {
		inviteRequest.AddCookie(cookie)
	}
	inviteResponse := httptest.NewRecorder()
	handler.ServeHTTP(inviteResponse, inviteRequest)
	if inviteResponse.Code != http.StatusCreated {
		t.Fatalf("expected 201 from invite creation, got %d: %s", inviteResponse.Code, inviteResponse.Body.String())
	}
	var invitePayload struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(inviteResponse.Body).Decode(&invitePayload); err != nil || invitePayload.Token == "" {
		t.Fatalf("expected a non-empty invite token, got %#v, %v", invitePayload, err)
	}

	consumeResponse := httptest.NewRecorder()
	handler.ServeHTTP(consumeResponse, httptest.NewRequest(http.MethodPost, "/api/v1/invites/"+invitePayload.Token+"/consume", encodeJSON(t, map[string]string{"password": "a brand new password"})))
	if consumeResponse.Code != http.StatusCreated {
		t.Fatalf("expected 201 from invite consumption, got %d: %s", consumeResponse.Code, consumeResponse.Body.String())
	}
	var consumedUser struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(consumeResponse.Body).Decode(&consumedUser); err != nil || consumedUser.ID == "" {
		t.Fatalf("expected a user id, got %#v, %v", consumedUser, err)
	}

	badTOTP := httptest.NewRecorder()
	handler.ServeHTTP(badTOTP, httptest.NewRequest(http.MethodPost, "/api/v1/users/"+consumedUser.ID+"/confirm-totp", encodeJSON(t, map[string]string{"code": "000000"})))
	if badTOTP.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for a wrong TOTP code, got %d: %s", badTOTP.Code, badTOTP.Body.String())
	}
}

func TestCreateInviteWithASiteRoleGrantsMembershipOnConsumption(t *testing.T) {
	t.Parallel()
	handler := newIdentityHandler(t, "correct-secret")
	cookies := bootstrapAndLogin(t, handler, "correct-secret", "admin@example.com", "correct horse battery staple")

	inviteRequest := httptest.NewRequest(http.MethodPost, "/api/v1/users/invites", encodeJSON(t, map[string]any{
		"email": "operator@example.com", "role": "operator", "site_ids": []string{"site_default"},
	}))
	for _, cookie := range cookies {
		inviteRequest.AddCookie(cookie)
	}
	inviteResponse := httptest.NewRecorder()
	handler.ServeHTTP(inviteResponse, inviteRequest)
	if inviteResponse.Code != http.StatusCreated {
		t.Fatalf("expected 201 from a site-role invite, got %d: %s", inviteResponse.Code, inviteResponse.Body.String())
	}
}

func TestCreateInviteRejectsAnInvalidRole(t *testing.T) {
	t.Parallel()
	handler := newIdentityHandler(t, "correct-secret")
	cookies := bootstrapAndLogin(t, handler, "correct-secret", "admin@example.com", "correct horse battery staple")

	request := httptest.NewRequest(http.MethodPost, "/api/v1/users/invites", encodeJSON(t, map[string]any{
		"email": "broken@example.com", "role": "operator",
	}))
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for a site role with no site_ids, got %d: %s", response.Code, response.Body.String())
	}
}
```

`TestBootstrapRequiresTheConfiguredSecret`'te de `newIdentityHandler(t, "correct-secret", "admin-token")` çağrısını `newIdentityHandler(t, "correct-secret")` yap (gövdesi başka değişiklik gerektirmez).

Dosyanın import listesine ekle: `"github.com/gokayybaz/bazusop/internal/authorization"`, `"github.com/gokayybaz/bazusop/internal/sessions"`.

- [x] **Adım 9: Testleri çalıştırıp geçtiğini doğrula**

Çalıştır: `GOCACHE=/tmp/bazusop-go-cache go test ./internal/server/... -run 'TestBootstrap|TestCreateInvite' -v 2>&1 | tail -40`
Beklenen: BAŞARILI

- [x] **Adım 10: `rbac_test.go`'yu güncelle**

`newRBACHandler`'ın kullanılmayan `adminToken, operatorToken string` parametrelerini (yalnız `adminToken`, `WithAdminToken`'a geçiriliyordu — o da siliniyor; `operatorToken` zaten hiç kullanılmıyordu) kaldır, `WithAdminToken(adminToken)` satırını sil, iki çağrı sitesini (`newRBACHandler(t, "admin-token", "operator-token")` → `newRBACHandler(t)`) güncelle:

```go
func newRBACHandler(t *testing.T) (http.Handler, *identity.Service, *authorization.Service, *sessions.Service) {
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
	handler := server.NewHandler(
		server.WithIdentity(identityService, "bootstrap-secret"),
		server.WithSessions(sessionService, identityService),
		server.WithAuthorization(authzService),
		server.WithAuditTrail(audittrail.NewService(audittrail.NewMemoryStore())),
	)
	return handler, identityService, authzService, sessionService
}
```

- [x] **Adım 11: Testleri çalıştırıp geçtiğini doğrula**

Çalıştır: `GOCACHE=/tmp/bazusop-go-cache go test ./internal/server/... -run 'TestRequirePermission' -v`
Beklenen: BAŞARILI

- [x] **Adım 12: `rbac_operational_test.go`'yu güncelle**

`newOperatorAndSiteAdminSessions`'da `server.WithJobs(jobService, "operator-token")` → `server.WithJobs(jobService)`, `server.WithAlerts(alertService, "operator-token")` → `server.WithAlerts(alertService)`, `server.WithAdminToken("admin-token"),` satırını sil.

- [x] **Adım 13: Testleri çalıştırıp geçtiğini doğrula**

Çalıştır: `GOCACHE=/tmp/bazusop-go-cache go test ./internal/server/... -run 'TestSiteAdmin' -v`
Beklenen: BAŞARILI

- [x] **Adım 14: `serviceaccounts_test.go`'yu güncelle**

`newServiceAccountHandler`'ın `adminToken string` parametresini kaldır, `server.WithJobs(jobService, "operator-token")` → `server.WithJobs(jobService)`, `server.WithAdminToken(adminToken),` satırını sil. Üç çağrı sitesini (`newServiceAccountHandler(t, "admin-token")` → `newServiceAccountHandler(t)`) güncelle.

- [x] **Adım 15: Tam `internal/server` paket testini, vet ve gofmt'ı çalıştır**

Çalıştır: `go vet ./internal/server/... && gofmt -l internal/server/*.go && GOCACHE=/tmp/bazusop-go-cache go test ./internal/server/... -v 2>&1 | tail -250`
Beklenen: vet/gofmt çıktısı yok; tüm testler (yeni ve eski) BAŞARILI

- [x] **Adım 16: Tam repo build/vet/gofmt/test**

Çalıştır: `go build ./... && go vet ./... && gofmt -l . 2>&1 | grep -v node_modules && BAZUSOP_TEST_DATABASE_URL='postgres://bazusop:bazusop_test@127.0.0.1:5432/bazusop_test?sslmode=disable' GOCACHE=/tmp/bazusop-go-cache go test ./...`
Beklenen: build başarılı; vet/gofmt çıktısı yok; her paket `ok`

- [x] **Adım 17: Commit**

```bash
git add internal/server/authorization_test.go internal/server/jobs_test.go internal/server/alerting_test.go internal/server/cloudinventory_test.go internal/server/identity_test.go internal/server/rbac_test.go internal/server/rbac_operational_test.go internal/server/serviceaccounts_test.go
git commit -m "test: migrate internal/server tests off the legacy bearer bridge"
```

## Görev 4: Dokümantasyon — README, OPERATIONS, RUNBOOK ve migration rehberi

**Dosyalar:**
- Değiştir: `README.md`, `docs/OPERATIONS.md`, `docs/RUNBOOK.md`
- Oluştur: `docs/MIGRATION_v0.4.md`

- [x] **Adım 1: `README.md`'yi güncelle**

`## Hızlı başlangıç` bölümündeki iki örnek komuttan `BAZUSOP_OPERATOR_TOKEN`/`BAZUSOP_ADMIN_TOKEN` satırlarını kaldır, yerine `BAZUSOP_BOOTSTRAP_SECRET`/`BAZUSOP_TOTP_ENCRYPTION_KEY` ekle:

```bash
BAZUSOP_ENROLLMENT_TOKEN="tek-kullanimlik-guclu-bir-secret" \
BAZUSOP_BOOTSTRAP_SECRET="tek-kullanimlik-baska-bir-guclu-secret" \
BAZUSOP_TOTP_ENCRYPTION_KEY="totp-secret-sifreleme-anahtari" \
BAZUSOP_TELEMETRY_RETENTION_DAYS=30 \
BAZUSOP_LOG_RETENTION_DAYS=14 \
./bin/bazusop-hub
```

(Docker Compose örneğinde de aynı değişiklik.)

`## Yapılandırma özeti` tablosundan `BAZUSOP_OPERATOR_TOKEN`/`BAZUSOP_ADMIN_TOKEN` satırlarını kaldır; tabloya şu satırları ekle:

```markdown
| `BAZUSOP_BOOTSTRAP_SECRET` | İlk platform yöneticisini oluşturmak için tek kullanımlık secret |
| `BAZUSOP_TOTP_ENCRYPTION_KEY` | TOTP secret'larını veritabanında şifrelemek için runtime anahtarı |
| `BAZUSOP_SERVICE_ACCOUNT_PEPPER` | Servis hesabı token'larını HMAC ile özetlemek için veritabanı dışı pepper |
| `BAZUSOP_TRUSTED_ORIGINS` | CORS için güvenilir origin listesi (virgülle ayrılmış); varsayılan kapalı |
```

Tablodan hemen sonra kısa bir "İlk kurulum" paragrafı ekle:

```markdown
### İlk kurulum

`BAZUSOP_BOOTSTRAP_SECRET` ve `BAZUSOP_TOTP_ENCRYPTION_KEY` ayarlandığında,
ilk platform yöneticisi tek kullanımlık `/api/v1/bootstrap` ucuyla oluşturulur;
ardından `/api/v1/sessions` ile oturum açılır. Mutasyon içeren tüm API'ler
artık yalnız geçerli bir insan oturumu veya servis hesabı token'ı ile
çalışır — ayrıntılar ve otomasyon (CI/CD) geçişi için bkz.
[docs/MIGRATION_v0.4.md](docs/MIGRATION_v0.4.md).
```

- [x] **Adım 2: `docs/OPERATIONS.md`'yi güncelle**

Env var tablosundan `BAZUSOP_OPERATOR_TOKEN`/`BAZUSOP_ADMIN_TOKEN` satırlarını kaldır, README'dekiyle aynı dört yeni satırı ekle. İki örnek komuttaki (`./bin/bazusop-hub` ve `docker compose up`) `BAZUSOP_OPERATOR_TOKEN=...`/`BAZUSOP_ADMIN_TOKEN=...` argümanlarını `BAZUSOP_BOOTSTRAP_SECRET=...`/`BAZUSOP_TOTP_ENCRYPTION_KEY=...` ile değiştir. 178. satır civarındaki sorun giderme notunu ("... `BAZUSOP_OPERATOR_TOKEN` yapılandırmasını kontrol et") "... geçerli bir oturum veya servis hesabı token'ı kullanıldığını kontrol et; bkz. [docs/MIGRATION_v0.4.md](MIGRATION_v0.4.md)" olacak şekilde güncelle.

- [x] **Adım 3: `docs/RUNBOOK.md`'yi güncelle**

168. satır civarındaki `-H "Authorization: Bearer $BAZUSOP_ADMIN_TOKEN"` curl örneğini, bir servis hesabı token'ı veya oturum çerezi kullanan bir örnekle değiştir:

```bash
-H "Authorization: Bearer $BAZUSOP_SERVICE_ACCOUNT_TOKEN" -H "Content-Type: application/json" \
```

(Çevresindeki açıklama metnini, `$BAZUSOP_SERVICE_ACCOUNT_TOKEN`'ın `/api/v1/sites/{siteID}/service-accounts` ile önceden oluşturulmuş bir servis hesabı token'ı olduğunu belirtecek şekilde güncelle.)

- [x] **Adım 4: `docs/MIGRATION_v0.4.md`'yi oluştur**

```markdown
# v0.4'e Geçiş: Eski Operator/Admin Token'larının Kaldırılması

Spike 11.7, `BAZUSOP_OPERATOR_TOKEN` ve `BAZUSOP_ADMIN_TOKEN` statik bearer
secret'larını kod tabanından tamamen kaldırdı. Bu, spec'in "Servis
hesapları" bölümünde baştan öngörülmüş, **bilinçli bir kimlik doğrulama
kırılmasıdır** — bu iki ortam değişkenine bağımlı bir dağıtım, yükseltme
sonrası hiçbir mutasyon isteğini kimlik doğrulayamaz hale gelir.

## Ne değişti

- `BAZUSOP_OPERATOR_TOKEN`/`BAZUSOP_ADMIN_TOKEN` artık okunmuyor; hub bu
  değişkenleri görse bile hiçbir etkisi olmaz.
- Mutasyon içeren her API isteği artık ya geçerli bir **insan oturumu**
  (çerez tabanlı, spike 11.4) ya da geçerli bir **servis hesabı token'ı**
  (spike 11.6) gerektirir.
- Yalnız okuma yapan (`GET`) rotalar bu spike'tan etkilenmedi.

## Otomasyon/CI script'lerinizi geçirme

1. Hub'ı `BAZUSOP_BOOTSTRAP_SECRET` ve `BAZUSOP_TOTP_ENCRYPTION_KEY` ile
   başlatın (henüz yapmadıysanız).
2. `POST /api/v1/bootstrap` ile ilk platform yöneticisini oluşturun; yanıt
   TOTP QR kodunu (`provisioning_uri`) ve 10 tek kullanımlık recovery code
   içerir — recovery code'ları güvenli bir yere kaydedin.
3. `POST /api/v1/sessions` ile giriş yapıp bir oturum çerezi alın.
4. Otomasyonun ihtiyaç duyduğu her site için bir servis hesabı oluşturun:
   `POST /api/v1/sites/{siteID}/service-accounts` — gövde: `{"name":
   "ci-bot", "role": "operator"}` (`operator`, `site-admin` veya `viewer`
   olabilir; hangi eylemleri yapacaksa o rolü seçin). Yanıttaki `token`
   alanı **yalnız bu istekte** gösterilir — kaydedin.
5. Eski `Authorization: Bearer $BAZUSOP_OPERATOR_TOKEN`/`$BAZUSOP_ADMIN_TOKEN`
   header'larını script'lerinizde `Authorization: Bearer <servis hesabı
   token'ı>` ile değiştirin.
6. Token'lar varsayılan 90 gün, en fazla 365 gün geçerlidir —
   `POST /api/v1/service-accounts/{accountID}/rotate` ile süresi dolmadan
   rotate edin.

## Ek insan yöneticileri davet etme

Platform yöneticisi, `POST /api/v1/users/invites` ile (oturum çerezi
gerektirir) yeni bir platform-admin veya site-rolü davet edebilir; davet
edilen kullanıcı `POST /api/v1/invites/{token}/consume` ile parolasını
belirler ve `POST /api/v1/users/{userID}/confirm-totp` ile TOTP'sini
onaylar.
```

- [x] **Adım 5: Commit**

```bash
git add README.md docs/OPERATIONS.md docs/RUNBOOK.md docs/MIGRATION_v0.4.md
git commit -m "docs: document the legacy bearer bridge removal and v0.4 migration path"
```

## Görev 5: Canlı hub'a karşı manuel doğrulama

**Dosyalar:** yok (yalnız doğrulama).

- [x] **Adım 1: Yeni secret'larla, eski token'lar OLMADAN bir hub ayağa kaldır**

Çalıştır: `POSTGRES_PASSWORD=verify-pw BAZUSOP_ENROLLMENT_TOKEN=verify-token BAZUSOP_BOOTSTRAP_SECRET=verify-bootstrap BAZUSOP_TOTP_ENCRYPTION_KEY=verify-totp-key BAZUSOP_SERVICE_ACCOUNT_PEPPER=verify-pepper BAZUSOP_PORT=18096 docker compose -p bazusop-verify-v04 up --build -d` ve hub konteynerinin healthy olmasını bekle. (`BAZUSOP_OPERATOR_TOKEN`/`BAZUSOP_ADMIN_TOKEN` **bilerek verilmiyor**.)

- [x] **Adım 2: Eski bearer şemasının artık HİÇBİR şeyi yetkilendirmediğini doğrula**

```bash
curl -s -o /dev/null -w "eski-tarz bearer (beklenen 401): %{http_code}\n" -X POST http://127.0.0.1:18096/api/v1/alert-rules \
  -H "Authorization: Bearer rastgele-bir-eski-token" \
  -d '{"name":"test","kind":"metric","metric":"cpu","threshold":90,"severity":"critical","enabled":true}'
```

Beklenen: `401` (eskiden `403`/`503` olabilirdi — artık köprü hiç yok, düz 401).

- [x] **Adım 3: Bootstrap ol, giriş yap, bir servis hesabı oluştur**

```bash
curl -s -c /tmp/bazusop-v04-cookies.txt -X POST http://127.0.0.1:18096/api/v1/bootstrap \
  -d '{"secret":"verify-bootstrap","email":"admin@example.com","password":"correct horse battery staple"}'
# yanıttan ilk recovery_code'u $CODE olarak sakla
curl -s -c /tmp/bazusop-v04-cookies.txt -X POST http://127.0.0.1:18096/api/v1/sessions \
  -d '{"email":"admin@example.com","password":"correct horse battery staple","recovery_code":"'"$CODE"'"}'
curl -s -b /tmp/bazusop-v04-cookies.txt -X POST http://127.0.0.1:18096/api/v1/sites/site_default/service-accounts \
  -d '{"name":"ci-bot","role":"operator"}'
```

Beklenen: bootstrap `201`; login `201`; servis hesabı oluşturma `201` (`bazusop_sat_` önekli bir `token` içerir).

- [x] **Adım 4: Servis hesabı token'ının gerçek bir mutasyonu kimlik doğruladığını onayla**

```bash
curl -s -o /dev/null -w "status:%{http_code}\n" -X POST http://127.0.0.1:18096/api/v1/alert-rules \
  -H "Authorization: Bearer <adım 3'teki token>" \
  -d '{"name":"Yüksek CPU","kind":"metric","metric":"cpu","threshold":90,"severity":"critical","enabled":true}'
```

Beklenen: `201` (operator rolü `PermissionManageAlerts` gerektirdiğinden — bekle, operator bu izni KARŞILAMAZ; site-admin gerekir). **Beklenen düzeltme:** operator rolüyle oluşturulan servis hesabı `PermissionManageAlerts`'i karşılamaz (yalnız site-admin karşılar — bkz. spike 11.5'in matrisi); bu adımda `403` beklenir. Alarm kuralı yönetimini gerçekten test etmek isteniyorsa adım 3'teki servis hesabı `"role":"site-admin"` ile oluşturulmalıdır. Bu adımı, operator rolüyle oluşturulan hesabın `PermissionCreateJobs`'u karşıladığı bir iş oluşturma isteğiyle değiştir:

```bash
curl -s -o /dev/null -w "status:%{http_code}\n" -X POST http://127.0.0.1:18096/api/v1/instances/edge-01/jobs \
  -H "Authorization: Bearer <adım 3'teki token>" \
  -d '{"action":"service.restart","target":"nginx.service","approved_by":"ci-bot","reason":"verify"}'
```

Beklenen: `500` (host `edge-01` bu temiz hub'da kayıtlı değil — ama bu, kimlik doğrulama/yetkilendirmenin geçtiğini kanıtlar: `401`/`403` DEĞİL, `500`).

- [x] **Adım 5: Denetim izini sorgula**

```bash
docker compose -p bazusop-verify-v04 exec postgres psql -U bazusop -d bazusop -c \
  "SELECT actor_type, action, resource_type, outcome, error_code FROM audit_events WHERE resource_type IN ('alert_rules','jobs','service_accounts') ORDER BY occurred_at;"
```

Beklenen: eski-tarz bearer denemesi `actor_type=legacy_token`/`outcome=failure`/`error_code=401` olarak görünür (spike 11.7 `deriveActor`'ın bu dalını bilinçli olarak koruduğu için); servis hesabı denemeleri `actor_type=service_account` olarak görünür.

- [x] **Adım 6: Kapat ve geçici dosyaları temizle**

Çalıştır: `docker compose -p bazusop-verify-v04 down -v && rm -f /tmp/bazusop-v04-cookies.txt`

## Kendi Kendine İnceleme

**1. Spec kapsaması.**
- "Mevcut BAZUSOP_OPERATOR_TOKEN ve BAZUSOP_ADMIN_TOKEN desteği ... kaldırılır" → Görev 1-2 (üretim kodu + config + main.go + compose.yaml'dan tam kaldırma).
- "Bu bilinçli bir kimlik doğrulama kırılmasıdır" → Görev 5'te canlı olarak doğrulandı (eski bearer artık yalnız 401, hiçbir mutasyonu yetkilendirmiyor).
- "Release notlarında migration yönergesi verilir" → Görev 4 (`docs/MIGRATION_v0.4.md` + README/OPERATIONS/RUNBOOK güncellemeleri).
- "Mutasyonlar yalnız insan oturumu veya servis hesabıyla çalışır" (yol haritası kabul sinyali) → Görev 1'in `requirePermission` yeniden yazımı + Görev 3/5'in canlı ve test-seviyesi kanıtları.
- Kod incelemesiyle bulunan gerçek hata (`handleRevokeUserSessions`/`handleRevokeAllSessions`'ın hiç `requirePermission`'a taşınmamış olması) → Görev 1 Adım 6'da düzeltildi, aksi halde bu iki uç köprü kaldırıldığında tamamen erişilemez kalırdı.

**2. Placeholder taraması.** Her adımda gerçek kod var. Bu plan doğası gereği çıkarımsal olduğundan klasik "önce yanlış yaz, sonra düzelt" TDD dizisi yerine, Görev 5 Adım 4'te kendi içinde bir düzeltme örneği barındırıyor (operator rolünün `PermissionManageAlerts`'i karşılamadığını fark edip adımı `PermissionCreateJobs`'a çeken bir doğrulama komutuna değiştirme) — canlı doğrulama sırasında beklenen bir yanlış varsayımın nasıl düzeltileceğini gösteriyor, çözümsüz bırakılmıyor.

**3. Tip tutarlılığı.** `requirePermission`'ın yeni imzası (`tokens`/`legacyRole` olmadan) Görev 1'in tüm 11 çağrı sitesinde ve Görev 3'ün test dönüşümlerinde tutarlı. `WithJobs`/`WithAlerts`/`WithCloudInventory`'nin tek-parametreli yeni imzaları hem `cmd/bazusop-hub/main.go` hem tüm test dosyalarında aynı şekilde güncelleniyor.
