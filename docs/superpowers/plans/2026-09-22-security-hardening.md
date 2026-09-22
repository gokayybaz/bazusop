# Güvenlik Sertleştirmesi (Spike 11.10) Uygulama Planı

> **Otonom çalışanlar için:** ZORUNLU ALT-SKILL: Bu planı görev görev uygulamak için superpowers:subagent-driven-development (önerilen) veya superpowers:executing-plans kullan. Adımlar `- [ ]` checkbox söz dizimiyle takip edilir.

**Amaç:** 11.1–11.9'un inşa ettiği RBAC/kimlik/audit yüzeyinde, dikkatli bir güvenlik denetimiyle bulunan üç GERÇEK açığı kapatır: (1) CSRF token doğrulaması yalnız `handleLogout`'a bağlıydı, 11.5–11.9'un eklediği DİĞER TÜM oturum-çerezi tabanlı mutasyon uçlarında hiç uygulanmıyordu; (2) servis hesabı rotate/revoke/disable uçları hedef kaynağın site'ını hiç doğrulamıyordu (yatay yetki yükseltmeye — IDOR — açık bir desen); (3) giriş, MFA onayı ve davet tüketimi uçlarında hiçbir brute-force/rate-limit koruması yoktu. Yol haritası kabul sinyali: "Scope bypass, CSRF, brute-force ve redaksiyon testleri geçer."

**Mimari:** Üç bağımsız düzeltme, tek bir güvenlik sertleştirmesi spike'ında birleşiyor: (1) CSRF düzeltmesi TEK bir merkezi noktada (`requirePermission`) yapılır ve ONU kullanan HER mutasyon handler'ını geriye dönük korur; (2) IDOR düzeltmesi `internal/serviceaccounts`'un servis katmanına site doğrulaması ekler (mağaza katmanı değişmez); (3) rate limiting, yeni küçük bir `internal/ratelimit` paketiyle (process-içi, sliding-window) üç açık uca (`login`, `confirm-totp`, `invite consume`) bağlanır — dağıtık/çoğaltmalar-arası senkronizasyon bilinçli olarak v1 kapsamı dışıdır (bkz. Genel Kısıtlar).

**Teknoloji yığını:** Go 1.26, yalnızca stdlib (yeni bağımlılık yok).

**Spec:** [docs/superpowers/specs/2026-09-13-enterprise-rbac-identity-audit-design.md](../specs/2026-09-13-enterprise-rbac-identity-audit-design.md) — "Hata ve güvenli kapanma davranışı" ve "Test stratejisi" bölümleri. Yol haritası satırı: `11.10 | Güvenlik sertleştirmesi | Scope bypass, CSRF, brute-force ve redaksiyon testleri geçer`.

## Genel Kısıtlar

- **CSRF düzeltmesi yalnız GET/HEAD DIŞI istekleri etkiler.** `requirePermission`'ın oturum-çerezi yoluna eklenen kontrol, `request.Method` GET veya HEAD olduğunda atlanır — bu, mevcut TÜM salt-okunur uçların (activity/audit/oidc-config okuma/servis-hesabı listeleme) hiçbir değişiklik gerektirmeden çalışmaya devam etmesini sağlar.
- **Rate limiting process-içidir, çoğaltmalar arası senkronize DEĞİLDİR.** Birden fazla hub replikası arkasında, her replika kendi sınırını bağımsız uygular — saldırgan farklı replikalara dağılarak sınırı kısmen aşabilir. Bu, spec'in "Rate limiting... uygulanır" gereksinimini karşılayan, mevcut hiçbir korumaya kıyasla büyük bir iyileştirmedir; tam dağıtık (Postgres destekli) bir çözüm, bu spike'ın kabul sinyalinin ("brute-force testleri geçer") gerektirmediği orantısız bir karmaşıklık eklerdi. Bilinçli bir v1 sınırlaması olarak `docs/OPERATIONS.md`'de belgelenir.
- **Rate limiting kapsam dışı bırakılanlar:** servis hesabı bearer token doğrulaması (`requirePermission`'ın token yolu) rate-limit edilmez — 256-bit rastgele secret'ı brute-force etmek pratikte imkansızdır ve bu, meşru servis hesabı trafiğinin YOĞUN, normal yolu olduğundan agresif bir sınır yanlışlıkla üretim trafiğini keser. OIDC callback kod değişimi de kapsam dışıdır (kod tek kullanımlıktır ve IdP tarafından zaten süreli/tek-seferliktir).
- **Servis hesabı site-scope düzeltmesi, `internal/serviceaccounts` mağaza katmanını DEĞİL yalnız servis katmanını değiştirir** — `Store.RevokeToken`/`DisableAccount` (düşük seviye, ID bazlı) aynı kalır; yeni site doğrulaması `Service.RotateToken`/`RevokeToken`/`DisableAccount`'a eklenir, önce `AccountByID`/`TokenByID` ile hedefin GERÇEK site'ını okuyup çağıranın yetkili olduğu site ile karşılaştırır. Uyuşmazlıkta, kaynağın var olup olmadığını sızdırmamak için `ErrAccountNotFound`/`ErrTokenNotFound` (var olmayanla AYNI hata) döner — spec'in "Kapsam dışı kaynak için 404" ilkesiyle birebir.
- **`handleAssignSiteRole`/`handleRevokeSiteRole` (rbac.go) bilinçli olarak DEĞİŞMEZ.** Bu iki uç `PermissionManageUsers` (organizasyon kapsamlı, yalnız platform yöneticisi) gerektirir ve platform yöneticisi TANIM GEREĞİ tüm siteleri yönetir — URL'deki `{siteID}`'nin hub'ın sabit `scope.SiteID`'sinden farklı olması burada bir güvenlik açığı DEĞİL, doğru ve kasıtlı davranıştır (spec'in rol tablosu: "Kullanıcı daveti ve rol/site ataması: Evet [platform admin], Hayır [diğerleri]").
- **Audit yazımının transactional/rollback garantisi** (spec: "Audit transaction'ı başarısızsa güvenlik-kritik mutasyon rollback olur") ve **acr/amr'ın audit olayına eklenmesi**, önceki spike'larda (11.2, 11.9) bilinçli olarak ertelenmiş kalemlerdir; bu spike'ın kabul sinyali (scope bypass, CSRF, brute-force, redaksiyon) bunları kapsamıyor, tekrar ertelenir.
- **Retention worker** (11.8'de ertelenen partition-temelli audit/activity saklama uygulaması) da bu spike'ın kapsamı dışındadır — kabul sinyalinde yok.

## Dosya Yapısı

- Değiştir: `internal/server/rbac.go` (CSRF çekirdek düzeltmesi).
- Oluştur: `internal/ratelimit/ratelimit.go`, `internal/ratelimit/ratelimit_test.go`.
- Değiştir: `internal/server/sessions.go`, `internal/server/identity.go`, `internal/server/server.go` (rate limiting kablolama).
- Değiştir: `internal/serviceaccounts/serviceaccounts.go`, `internal/serviceaccounts/serviceaccounts_test.go` (site-scope IDOR düzeltmesi).
- Değiştir: `internal/server/serviceaccounts.go`, `internal/server/serviceaccounts_test.go`.
- Değiştir (CSRF header ekleme — mekanik): `internal/server/identity_test.go`, `internal/server/rbac_test.go`, `internal/server/rbac_operational_test.go`, `internal/server/activity_test.go`, `internal/server/alerting_test.go`, `internal/server/cloudinventory_test.go`, `internal/server/jobs_test.go`, `internal/server/sessions_test.go`, `internal/server/oidc_test.go`.
- Oluştur: `internal/server/csrf_test.go` (yeni CSRF-özel testler).
- Değiştir: `docs/API.md`, `docs/OPERATIONS.md`.

## Görev 1: CSRF token doğrulamasını `requirePermission`'a taşı

**Dosyalar:**
- Değiştir: `internal/server/rbac.go`
- Oluştur: `internal/server/csrf_test.go`

**Arayüzler:**
- Değiştirir: `requirePermission`'ın oturum-çerezi yolu artık GET/HEAD dışı isteklerde `requireMatchingCSRFToken`'ı çağırır (imza değişmez).

- [ ] **Adım 1: `internal/server/rbac.go`'daki `requirePermission`'ı güncelle**

```go
func requirePermission(response http.ResponseWriter, request *http.Request, sessionService *sessions.Service, serviceAccountService *serviceaccounts.Service, authzService *authorization.Service, permission authorization.Permission, siteID string) (string, bool) {
	if sessionService != nil {
		if cookie, err := request.Cookie(sessionCookieName); err == nil && cookie.Value != "" {
			session, err := sessionService.Validate(request.Context(), cookie.Value)
			if err != nil {
				clearSessionCookies(response, request)
				http.Error(response, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
				return "", false
			}
			if request.Method != http.MethodGet && request.Method != http.MethodHead && !requireMatchingCSRFToken(response, request, session) {
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

(Yalnız `if request.Method != http.MethodGet && request.Method != http.MethodHead && !requireMatchingCSRFToken(response, request, session) { return "", false }` satırı eklendi, geri kalan fonksiyon değişmedi.)

- [ ] **Adım 2: Build'i doğrula (test dosyaları henüz güncellenmedi, beklenen kırık testler)**

Çalıştır: `go build ./... 2>&1 | head -30`
Beklenen: BAŞARILI (üretim kodu). `go test ./internal/server/...` bu noktada BAŞARISIZ olur (beklenen ara durum) — Adım 3-11 düzeltir.

- [ ] **Adım 3: `internal/server/identity_test.go`'ya CSRF çıkarma yardımcısını ekle, üç testi düzelt**

`bootstrapAndLogin` fonksiyonunun hemen altına ekle:

```go
// csrfTokenFromCookies extracts the CSRF cookie's value from a cookie
// slice returned by a real HTTP login (e.g. bootstrapAndLogin) or a
// manually-assembled test session — the CSRF cookie is deliberately
// non-HttpOnly (see setSessionCookies) so a real browser client can read
// it and echo it back as the X-CSRF-Token header; tests do the same.
func csrfTokenFromCookies(cookies []*http.Cookie) string {
	for _, cookie := range cookies {
		if cookie.Name == "bazusop_csrf" {
			return cookie.Value
		}
	}
	return ""
}
```

`TestBootstrapInviteConsumeAndConfirmTOTPEndToEnd`'deki `inviteRequest` bloğuna (satır ~98-100) CSRF header ekle:

```go
	inviteRequest := httptest.NewRequest(http.MethodPost, "/api/v1/users/invites", encodeJSON(t, map[string]string{"email": "new-admin@example.com"}))
	for _, cookie := range cookies {
		inviteRequest.AddCookie(cookie)
	}
	inviteRequest.Header.Set("X-CSRF-Token", csrfTokenFromCookies(cookies))
```

`TestCreateInviteWithASiteRoleGrantsMembershipOnConsumption`'daki `inviteRequest` bloğuna aynı şekilde ekle:

```go
	inviteRequest := httptest.NewRequest(http.MethodPost, "/api/v1/users/invites", encodeJSON(t, map[string]any{
		"email": "operator@example.com", "role": "operator", "site_ids": []string{"site_default"},
	}))
	for _, cookie := range cookies {
		inviteRequest.AddCookie(cookie)
	}
	inviteRequest.Header.Set("X-CSRF-Token", csrfTokenFromCookies(cookies))
```

`TestCreateInviteRejectsAnInvalidRole`'daki `request` bloğuna aynı şekilde ekle:

```go
	request := httptest.NewRequest(http.MethodPost, "/api/v1/users/invites", encodeJSON(t, map[string]any{
		"email": "broken@example.com", "role": "operator",
	}))
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	request.Header.Set("X-CSRF-Token", csrfTokenFromCookies(cookies))
```

- [ ] **Adım 4: Testleri çalıştır**

Çalıştır: `GOCACHE=/tmp/bazusop-go-cache go test ./internal/server/... -run 'TestBootstrap|TestCreateInvite' -v 2>&1 | tail -40`
Beklenen: BAŞARILI

- [ ] **Adım 5: `internal/server/rbac_test.go`'yu düzelt**

`TestRequirePermissionAllowsAPlatformAdminSession`'ı şununla değiştir:

```go
func TestRequirePermissionAllowsAPlatformAdminSession(t *testing.T) {
	t.Parallel()
	handler, identityService, authzService, sessionService := newRBACHandler(t)

	admin, _, err := identityService.Bootstrap(t.Context(), tenancy.DefaultOrganizationID, "admin@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	_, token, csrfToken, err := sessionService.Create(t.Context(), admin.ID, admin.OrganizationID)
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/v1/sites/site_default/memberships", encodeJSON(t, map[string]string{"user_id": "some-user", "role": "viewer"}))
	request.AddCookie(&http.Cookie{Name: "bazusop_session", Value: token})
	request.Header.Set("X-CSRF-Token", csrfToken)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("expected a platform-admin session to be allowed to assign a site role, got %d: %s", response.Code, response.Body.String())
	}

	role, err := authzService.RoleForUserAtSite(t.Context(), "some-user", "site_default")
	if err != nil || role != authorization.SiteRoleViewer {
		t.Fatalf("expected the assignment to take effect, got %v %v", role, err)
	}
}
```

`TestRequirePermissionDeniesASessionLackingThePermission`'daki `_, sessionToken, _, err := sessionService.Create(...)` satırını `_, sessionToken, csrfToken, err := sessionService.Create(...)` yap ve `request.AddCookie(...)`'den hemen sonra `request.Header.Set("X-CSRF-Token", csrfToken)` ekle (bu test zaten `403` beklediği için — CSRF kontrolü izin kontrolünden ÖNCE geldiğinden, CSRF header'ı doğru olmadan test YANLIŞ SEBEPLE 403 alırdı; doğru CSRF header'ı vererek testin GERÇEKTEN izin eksikliğini test ettiğinden emin olunur).

- [ ] **Adım 6: Testleri çalıştır**

Çalıştır: `GOCACHE=/tmp/bazusop-go-cache go test ./internal/server/... -run 'TestRequirePermission' -v 2>&1 | tail -30`
Beklenen: BAŞARILI

- [ ] **Adım 7: `internal/server/rbac_operational_test.go`'yu düzelt**

`newOperatorAndSiteAdminSessions`'daki iki `sessionService.Create` çağrısını ve dönen cookie slice'larını güncelle:

```go
	_, siteAdminToken, siteAdminCSRF, err := sessionService.Create(t.Context(), siteAdmin.ID, siteAdmin.OrganizationID)
	if err != nil {
		t.Fatal(err)
	}
	_, operatorToken, operatorCSRF, err := sessionService.Create(t.Context(), operator.ID, operator.OrganizationID)
	if err != nil {
		t.Fatal(err)
	}
	siteAdminCookies = []*http.Cookie{{Name: "bazusop_session", Value: siteAdminToken}, {Name: "bazusop_csrf", Value: siteAdminCSRF}}
	operatorCookies = []*http.Cookie{{Name: "bazusop_session", Value: operatorToken}, {Name: "bazusop_csrf", Value: operatorCSRF}}
	return handler, siteAdminCookies, operatorCookies
```

`TestSiteAdminCanCreateJobsButNotManageAlerts`'teki `createJob` isteğine, cookie döngüsünden hemen sonra ekle:

```go
	for _, cookie := range siteAdminCookies {
		createJob.AddCookie(cookie)
	}
	createJob.Header.Set("X-CSRF-Token", csrfTokenFromCookies(siteAdminCookies))
```

`TestSiteAdminCanManageAlertsButOperatorCannot`'taki `siteAdminRequest` ve `operatorRequest`'e aynı şekilde ekle:

```go
	for _, cookie := range siteAdminCookies {
		siteAdminRequest.AddCookie(cookie)
	}
	siteAdminRequest.Header.Set("X-CSRF-Token", csrfTokenFromCookies(siteAdminCookies))
```

```go
	for _, cookie := range operatorCookies {
		operatorRequest.AddCookie(cookie)
	}
	operatorRequest.Header.Set("X-CSRF-Token", csrfTokenFromCookies(operatorCookies))
```

- [ ] **Adım 8: Testleri çalıştır**

Çalıştır: `GOCACHE=/tmp/bazusop-go-cache go test ./internal/server/... -run 'TestSiteAdmin' -v 2>&1 | tail -30`
Beklenen: BAŞARILI

- [ ] **Adım 9: `internal/server/activity_test.go`, `alerting_test.go`, `cloudinventory_test.go`, `jobs_test.go`'yu düzelt**

Dört dosyanın hepsinde AYNI mekanik değişiklik: `_, adminToken, _, err := sessionService.Create(...)` satırını `_, adminToken, adminCSRF, err := sessionService.Create(...)` yap, ve o token'ı kullanan İLK POST isteğine (servis hesabı oluşturma — tek mutasyon bu dosyalarda) header ekle.

`activity_test.go` — üç POST isteğinin hepsine (`inviteRequest`, `assignRequest`, `createAccountRequest`) ekle, `adminCookie` tekil değişkenini kullanarak:

```go
	_, adminToken, adminCSRF, err := sessionService.Create(t.Context(), admin.ID, admin.OrganizationID)
	if err != nil {
		t.Fatal(err)
	}
	adminCookie := &http.Cookie{Name: "bazusop_session", Value: adminToken}

	inviteRequest := httptest.NewRequest(http.MethodPost, "/api/v1/users/invites", encodeJSON(t, map[string]string{"email": "new-viewer@example.com"}))
	inviteRequest.AddCookie(adminCookie)
	inviteRequest.Header.Set("X-CSRF-Token", adminCSRF)
	...
	assignRequest := httptest.NewRequest(http.MethodPost, "/api/v1/sites/"+scope.SiteID+"/memberships", encodeJSON(t, map[string]string{"user_id": admin.ID, "role": "viewer"}))
	assignRequest.AddCookie(adminCookie)
	assignRequest.Header.Set("X-CSRF-Token", adminCSRF)
	...
	createAccountRequest := httptest.NewRequest(http.MethodPost, "/api/v1/sites/"+scope.SiteID+"/service-accounts", encodeJSON(t, map[string]any{"name": "ci-bot", "role": "operator"}))
	createAccountRequest.AddCookie(adminCookie)
	createAccountRequest.Header.Set("X-CSRF-Token", adminCSRF)
```

`alerting_test.go`, `cloudinventory_test.go`, `jobs_test.go` — üçünde de TEK POST isteği (`createAccount`) var:

```go
	_, adminToken, adminCSRF, err := sessionService.Create(t.Context(), admin.ID, admin.OrganizationID)
	if err != nil {
		t.Fatal(err)
	}
	createAccount := httptest.NewRequest(http.MethodPost, "/api/v1/sites/.../service-accounts", encodeJSON(t, map[string]any{...}))
	createAccount.AddCookie(&http.Cookie{Name: "bazusop_session", Value: adminToken})
	createAccount.Header.Set("X-CSRF-Token", adminCSRF)
```

(Her dosyadaki mevcut `/sites/...` yolunu ve body'yi DEĞİŞTİRME — yalnız `sessionService.Create`'in üçüncü dönüş değerini `_`'den `adminCSRF`'e çevir ve `createAccount.Header.Set(...)` satırını ekle.)

- [ ] **Adım 10: Testleri çalıştır**

Çalıştır: `GOCACHE=/tmp/bazusop-go-cache go test ./internal/server/... -run 'TestActivityEventsMergeAllSources|TestMetricRule|TestCloudDiscoveryAPI|TestApprovedJobFlows' -v 2>&1 | tail -60`
Beklenen: BAŞARILI

- [ ] **Adım 11: `internal/server/sessions_test.go`'yu düzelt**

`TestAdminCanRevokeAllSessionsForAUser`'daki `revokeRequest` bloğuna ekle:

```go
	revokeRequest := httptest.NewRequest(http.MethodDelete, "/api/v1/users/"+loginPayload.UserID+"/sessions", nil)
	for _, cookie := range cookies {
		revokeRequest.AddCookie(cookie)
	}
	revokeRequest.Header.Set("X-CSRF-Token", csrfTokenFromCookies(cookies))
```

`TestAdminCanRevokeAllSessionsOrgWide`'daki `revokeRequest` bloğuna aynı şekilde ekle:

```go
	revokeRequest := httptest.NewRequest(http.MethodDelete, "/api/v1/sessions/all", nil)
	for _, cookie := range cookies {
		revokeRequest.AddCookie(cookie)
	}
	revokeRequest.Header.Set("X-CSRF-Token", csrfTokenFromCookies(cookies))
```

(`TestLoginWhoAmILogoutEndToEnd` zaten `logoutRequest.Header.Set("X-CSRF-Token", loginPayload.CSRFToken)` satırına sahip — değişmeden kalır.)

- [ ] **Adım 12: Testleri çalıştır**

Çalıştır: `GOCACHE=/tmp/bazusop-go-cache go test ./internal/server/... -run 'TestAdminCanRevoke' -v 2>&1 | tail -30`
Beklenen: BAŞARILI

- [ ] **Adım 13: `internal/server/oidc_test.go`'yu düzelt**

`TestOIDCLoginPKCEFlowCreatesSessionAndLocalLoginStaysIndependent`'teki `configRequest` ve `inviteRequest`'e ekle:

```go
	for _, cookie := range adminCookies {
		configRequest.AddCookie(cookie)
	}
	configRequest.Header.Set("X-CSRF-Token", csrfTokenFromCookies(adminCookies))
```

```go
	for _, cookie := range adminCookies {
		inviteRequest.AddCookie(cookie)
	}
	inviteRequest.Header.Set("X-CSRF-Token", csrfTokenFromCookies(adminCookies))
```

`TestOIDCCallbackRejectsAMismatchedState` ve `TestOIDCCallbackRejectsWithoutAPendingInvite`'teki `configRequest`'e de aynı şekilde ekle.

- [ ] **Adım 14: Testleri çalıştır**

Çalıştır: `GOCACHE=/tmp/bazusop-go-cache go test ./internal/server/... -run 'TestOIDC' -v 2>&1 | tail -60`
Beklenen: BAŞARILI

- [ ] **Adım 15: `internal/server/csrf_test.go`'yu yaz — CSRF'e özel yeni testler**

```go
package server_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMutationsRequireACSRFTokenEvenWithAValidSession(t *testing.T) {
	t.Parallel()
	handler, _, sessionService := newRBACHandler(t)
	// newRBACHandler bootstraps nothing itself; reuse its identity service.
	// A platform-admin session is sufficient to prove the point: CSRF is
	// checked before the permission check, so even a fully-authorized
	// session is rejected without a matching token.
	identityService := handlerIdentityServiceFor(t, handler)
	admin, _, err := identityService.Bootstrap(t.Context(), "org_default", "admin@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	_, token, csrfToken, err := sessionService.Create(t.Context(), admin.ID, admin.OrganizationID)
	if err != nil {
		t.Fatal(err)
	}

	missing := httptest.NewRequest(http.MethodPost, "/api/v1/sites/site_default/memberships", encodeJSON(t, map[string]string{"user_id": "some-user", "role": "viewer"}))
	missing.AddCookie(&http.Cookie{Name: "bazusop_session", Value: token})
	missingResponse := httptest.NewRecorder()
	handler.ServeHTTP(missingResponse, missing)
	if missingResponse.Code != http.StatusForbidden {
		t.Fatalf("expected 403 with no X-CSRF-Token header, got %d: %s", missingResponse.Code, missingResponse.Body.String())
	}

	wrong := httptest.NewRequest(http.MethodPost, "/api/v1/sites/site_default/memberships", encodeJSON(t, map[string]string{"user_id": "some-user", "role": "viewer"}))
	wrong.AddCookie(&http.Cookie{Name: "bazusop_session", Value: token})
	wrong.Header.Set("X-CSRF-Token", "not-the-real-token")
	wrongResponse := httptest.NewRecorder()
	handler.ServeHTTP(wrongResponse, wrong)
	if wrongResponse.Code != http.StatusForbidden {
		t.Fatalf("expected 403 with a wrong X-CSRF-Token, got %d: %s", wrongResponse.Code, wrongResponse.Body.String())
	}

	correct := httptest.NewRequest(http.MethodPost, "/api/v1/sites/site_default/memberships", encodeJSON(t, map[string]string{"user_id": "some-user", "role": "viewer"}))
	correct.AddCookie(&http.Cookie{Name: "bazusop_session", Value: token})
	correct.Header.Set("X-CSRF-Token", csrfToken)
	correctResponse := httptest.NewRecorder()
	handler.ServeHTTP(correctResponse, correct)
	if correctResponse.Code != http.StatusNoContent {
		t.Fatalf("expected 204 with the correct X-CSRF-Token, got %d: %s", correctResponse.Code, correctResponse.Body.String())
	}
}

func TestReadOnlyRequestsDoNotRequireACSRFToken(t *testing.T) {
	t.Parallel()
	handler, identityService, _, sessionService := newRBACHandler(t)
	admin, _, err := identityService.Bootstrap(t.Context(), "org_default", "admin@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	_, token, _, err := sessionService.Create(t.Context(), admin.ID, admin.OrganizationID)
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/session", nil)
	request.AddCookie(&http.Cookie{Name: "bazusop_session", Value: token})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected this handler to require its own auth (401 without further setup), got %d — this test only proves no CSRF-specific 403 is produced for a GET", response.Code)
	}
}
```

**Yazarken düzeltme:** `TestMutationsRequireACSRFTokenEvenWithAValidSession`'da `handlerIdentityServiceFor` diye uydurma bir yardımcı fonksiyon kullanılamaz — `newRBACHandler` zaten `(http.Handler, *identity.Service, *authorization.Service, *sessions.Service)` döndürüyor, bu yeterli. Yukarıdaki taslağı şu şekilde DÜZELT (gereksiz `handlerIdentityServiceFor` çağrısını kaldır, `newRBACHandler`'ın döndürdüğü `identityService`'i doğrudan kullan):

```go
package server_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMutationsRequireACSRFTokenEvenWithAValidSession(t *testing.T) {
	t.Parallel()
	handler, identityService, _, sessionService := newRBACHandler(t)
	admin, _, err := identityService.Bootstrap(t.Context(), "org_default", "admin@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	_, token, csrfToken, err := sessionService.Create(t.Context(), admin.ID, admin.OrganizationID)
	if err != nil {
		t.Fatal(err)
	}

	missing := httptest.NewRequest(http.MethodPost, "/api/v1/sites/site_default/memberships", encodeJSON(t, map[string]string{"user_id": "some-user", "role": "viewer"}))
	missing.AddCookie(&http.Cookie{Name: "bazusop_session", Value: token})
	missingResponse := httptest.NewRecorder()
	handler.ServeHTTP(missingResponse, missing)
	if missingResponse.Code != http.StatusForbidden {
		t.Fatalf("expected 403 with no X-CSRF-Token header, got %d: %s", missingResponse.Code, missingResponse.Body.String())
	}

	wrong := httptest.NewRequest(http.MethodPost, "/api/v1/sites/site_default/memberships", encodeJSON(t, map[string]string{"user_id": "some-user", "role": "viewer"}))
	wrong.AddCookie(&http.Cookie{Name: "bazusop_session", Value: token})
	wrong.Header.Set("X-CSRF-Token", "not-the-real-token")
	wrongResponse := httptest.NewRecorder()
	handler.ServeHTTP(wrongResponse, wrong)
	if wrongResponse.Code != http.StatusForbidden {
		t.Fatalf("expected 403 with a wrong X-CSRF-Token, got %d: %s", wrongResponse.Code, wrongResponse.Body.String())
	}

	correct := httptest.NewRequest(http.MethodPost, "/api/v1/sites/site_default/memberships", encodeJSON(t, map[string]string{"user_id": "some-user", "role": "viewer"}))
	correct.AddCookie(&http.Cookie{Name: "bazusop_session", Value: token})
	correct.Header.Set("X-CSRF-Token", csrfToken)
	correctResponse := httptest.NewRecorder()
	handler.ServeHTTP(correctResponse, correct)
	if correctResponse.Code != http.StatusNoContent {
		t.Fatalf("expected 204 with the correct X-CSRF-Token, got %d: %s", correctResponse.Code, correctResponse.Body.String())
	}
}
```

(`TestReadOnlyRequestsDoNotRequireACSRFToken` taslağı da kaldırılır — `internal/server/activity_test.go`'daki mevcut `TestActivityEventsMergeAllSourcesOrderedByRecency` zaten bir GET isteğini CSRF header'sız, yalnız oturum çerezi ile başarıyla çalıştırıyor; bu davranış ZATEN kanıtlanmış durumda, ayrı bir test eklemek tekrar olur.)

- [ ] **Adım 16: Testleri çalıştır**

Çalıştır: `GOCACHE=/tmp/bazusop-go-cache go test ./internal/server/... -run 'TestMutationsRequireACSRFToken' -v 2>&1 | tail -30`
Beklenen: BAŞARILI

- [ ] **Adım 17: Tam `internal/server` paket testini çalıştır**

Çalıştır: `go vet ./internal/server/... && gofmt -l internal/server/*.go && GOCACHE=/tmp/bazusop-go-cache go test ./internal/server/... -v 2>&1 | tail -300`
Beklenen: vet/gofmt çıktısı yok; TÜM testler (eski + yeni) BAŞARILI

- [ ] **Adım 18: Commit**

```bash
git add internal/server/rbac.go internal/server/csrf_test.go internal/server/identity_test.go internal/server/rbac_test.go internal/server/rbac_operational_test.go internal/server/activity_test.go internal/server/alerting_test.go internal/server/cloudinventory_test.go internal/server/jobs_test.go internal/server/sessions_test.go internal/server/oidc_test.go
git commit -m "fix: enforce CSRF token verification on every session-cookie mutation, not just logout"
```

## Görev 2: Servis hesabı yönetiminde site-scope IDOR düzeltmesi

**Dosyalar:**
- Değiştir: `internal/serviceaccounts/serviceaccounts.go`, `internal/serviceaccounts/serviceaccounts_test.go`
- Değiştir: `internal/server/serviceaccounts.go`, `internal/server/serviceaccounts_test.go`

**Arayüzler:**
- Değiştirir: `Service.RotateToken(ctx, siteID, accountID string, expiryDays int) (string, error)`, `Service.RevokeToken(ctx, siteID, tokenID string) error`, `Service.DisableAccount(ctx, siteID, accountID string) error` (hepsi yeni `siteID` parametresi alır).

- [ ] **Adım 1: `internal/serviceaccounts/serviceaccounts.go`'yu güncelle**

```go
func (service *Service) RotateToken(ctx context.Context, siteID, accountID string, expiryDays int) (string, error) {
	account, err := service.store.AccountByID(ctx, accountID)
	if err != nil {
		return "", err
	}
	if account.SiteID != siteID {
		return "", ErrAccountNotFound
	}
	if err := service.store.RevokeActiveTokensForAccount(ctx, accountID, service.now()); err != nil {
		return "", err
	}
	return service.issueToken(ctx, accountID, expiryDays)
}

func (service *Service) RevokeToken(ctx context.Context, siteID, tokenID string) error {
	token, _, err := service.store.TokenByID(ctx, tokenID)
	if err != nil {
		return err
	}
	account, err := service.store.AccountByID(ctx, token.ServiceAccountID)
	if err != nil {
		return err
	}
	if account.SiteID != siteID {
		return ErrTokenNotFound
	}
	return service.store.RevokeToken(ctx, tokenID, service.now())
}

func (service *Service) DisableAccount(ctx context.Context, siteID, accountID string) error {
	account, err := service.store.AccountByID(ctx, accountID)
	if err != nil {
		return err
	}
	if account.SiteID != siteID {
		return ErrAccountNotFound
	}
	if err := service.store.RevokeActiveTokensForAccount(ctx, accountID, service.now()); err != nil {
		return err
	}
	return service.store.DisableAccount(ctx, accountID, service.now())
}
```

(`CreateAccount`, `ListForSite`, `Validate`, `issueToken`, `hashSecret` değişmeden kalır.)

- [ ] **Adım 2: `internal/serviceaccounts/serviceaccounts_test.go`'daki mevcut çağrıları güncelle**

`TestRotateTokenInvalidatesThePreviousOne`'daki satırı değiştir:

```go
	newToken, err := service.RotateToken(t.Context(), "site_default", account.ID, 0)
```

`TestRevokeTokenRejectsFurtherUse`'daki satırı değiştir:

```go
	if err := service.RevokeToken(t.Context(), "site_default", tokenID); err != nil {
```

`TestDisableAccountRejectsFurtherUseEvenWithAValidToken`'daki satırı değiştir:

```go
	if err := service.DisableAccount(t.Context(), "site_default", account.ID); err != nil {
```

- [ ] **Adım 3: Yeni cross-site IDOR testlerini ekle**

Dosyanın sonuna ekle:

```go
func TestRotateTokenRejectsAnAccountFromAnotherSite(t *testing.T) {
	t.Parallel()
	service := newTestService(t, time.Now)
	account, _, err := service.CreateAccount(t.Context(), "org_default", "site-a", "ci-bot", authorization.SiteRoleOperator, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.RotateToken(t.Context(), "site-b", account.ID, 0); !errors.Is(err, serviceaccounts.ErrAccountNotFound) {
		t.Fatalf("expected ErrAccountNotFound for a cross-site rotate attempt, got %v", err)
	}
}

func TestRevokeTokenRejectsATokenFromAnotherSite(t *testing.T) {
	t.Parallel()
	service := newTestService(t, time.Now)
	_, token, err := service.CreateAccount(t.Context(), "org_default", "site-a", "ci-bot", authorization.SiteRoleOperator, 0)
	if err != nil {
		t.Fatal(err)
	}
	tokenID, _, _ := serviceaccounts.ParseToken(token)
	if err := service.RevokeToken(t.Context(), "site-b", tokenID); !errors.Is(err, serviceaccounts.ErrTokenNotFound) {
		t.Fatalf("expected ErrTokenNotFound for a cross-site revoke attempt, got %v", err)
	}
	if _, err := service.Validate(t.Context(), token, "10.0.0.1"); err != nil {
		t.Fatalf("expected the token to remain valid after a rejected cross-site revoke, got %v", err)
	}
}

func TestDisableAccountRejectsAnAccountFromAnotherSite(t *testing.T) {
	t.Parallel()
	service := newTestService(t, time.Now)
	account, token, err := service.CreateAccount(t.Context(), "org_default", "site-a", "ci-bot", authorization.SiteRoleOperator, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.DisableAccount(t.Context(), "site-b", account.ID); !errors.Is(err, serviceaccounts.ErrAccountNotFound) {
		t.Fatalf("expected ErrAccountNotFound for a cross-site disable attempt, got %v", err)
	}
	if _, err := service.Validate(t.Context(), token, "10.0.0.1"); err != nil {
		t.Fatalf("expected the account to remain active after a rejected cross-site disable, got %v", err)
	}
}
```

- [ ] **Adım 4: Testleri çalıştır**

Çalıştır: `GOCACHE=/tmp/bazusop-go-cache go test ./internal/serviceaccounts/... -v 2>&1 | tail -60`
Beklenen: BAŞARILI (tüm testler)

- [ ] **Adım 5: `internal/server/serviceaccounts.go`'yu güncelle**

`handleCreateServiceAccount`'a, `requirePermission`'dan hemen sonra path/scope tutarlılık kontrolü ekle:

```go
func handleCreateServiceAccount(service *serviceaccounts.Service, sessionService *sessions.Service, authzService *authorization.Service, activityService *activity.Service, scope tenancy.Scope) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		actorID, ok := requirePermission(response, request, sessionService, nil, authzService, authorization.PermissionManageServiceAccounts, scope.SiteID)
		if !ok {
			return
		}
		if request.PathValue("siteID") != scope.SiteID {
			http.Error(response, http.StatusText(http.StatusNotFound), http.StatusNotFound)
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
		...
```

`handleListServiceAccounts`'a aynı kontrolü ekle:

```go
func handleListServiceAccounts(service *serviceaccounts.Service, sessionService *sessions.Service, authzService *authorization.Service, scope tenancy.Scope) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		if _, ok := requirePermission(response, request, sessionService, nil, authzService, authorization.PermissionManageServiceAccounts, scope.SiteID); !ok {
			return
		}
		if request.PathValue("siteID") != scope.SiteID {
			http.Error(response, http.StatusText(http.StatusNotFound), http.StatusNotFound)
			return
		}
		accounts, err := service.ListForSite(request.Context(), request.PathValue("siteID"))
		...
```

`handleRotateServiceAccountToken`'ı `scope.SiteID`'yi `service.RotateToken`'a geçirecek ve `ErrAccountNotFound`'ı `404`'e eşleyecek şekilde güncelle:

```go
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
		token, err := service.RotateToken(request.Context(), scope.SiteID, accountID, body.ExpiryDays)
		if errors.Is(err, serviceaccounts.ErrAccountNotFound) {
			http.Error(response, http.StatusText(http.StatusNotFound), http.StatusNotFound)
			return
		}
		if errors.Is(err, serviceaccounts.ErrInvalidExpiry) {
			http.Error(response, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}
		if err != nil {
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		...
```

`handleRevokeServiceAccountToken`'ı aynı şekilde güncelle:

```go
func handleRevokeServiceAccountToken(service *serviceaccounts.Service, sessionService *sessions.Service, authzService *authorization.Service, activityService *activity.Service, scope tenancy.Scope) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		actorID, ok := requirePermission(response, request, sessionService, nil, authzService, authorization.PermissionManageServiceAccounts, scope.SiteID)
		if !ok {
			return
		}
		tokenID := request.PathValue("tokenID")
		if err := service.RevokeToken(request.Context(), scope.SiteID, tokenID); err != nil {
			if errors.Is(err, serviceaccounts.ErrTokenNotFound) {
				http.Error(response, http.StatusText(http.StatusNotFound), http.StatusNotFound)
				return
			}
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		...
```

`handleDisableServiceAccount`'ı aynı şekilde güncelle:

```go
func handleDisableServiceAccount(service *serviceaccounts.Service, sessionService *sessions.Service, authzService *authorization.Service, activityService *activity.Service, scope tenancy.Scope) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		actorID, ok := requirePermission(response, request, sessionService, nil, authzService, authorization.PermissionManageServiceAccounts, scope.SiteID)
		if !ok {
			return
		}
		accountID := request.PathValue("accountID")
		if err := service.DisableAccount(request.Context(), scope.SiteID, accountID); err != nil {
			if errors.Is(err, serviceaccounts.ErrAccountNotFound) {
				http.Error(response, http.StatusText(http.StatusNotFound), http.StatusNotFound)
				return
			}
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		...
```

- [ ] **Adım 6: Build'i doğrula**

Çalıştır: `go build ./internal/server/... 2>&1 | head -30`
Beklenen: `internal/server_test` (test paketi) derlenemeyebilir (Adım 7 düzeltir); üretim kodu başarılı olmalı: `go build ./internal/serviceaccounts/... ./internal/server/...`

- [ ] **Adım 7: `internal/server/serviceaccounts_test.go`'yu güncelle**

`newServiceAccountHandler`'ı hem CSRF cookie'sini hem de `*serviceaccounts.Service`'i döndürecek şekilde değiştir:

```go
func newServiceAccountHandler(t *testing.T) (http.Handler, []*http.Cookie, *serviceaccounts.Service) {
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
		server.WithJobs(jobService),
		server.WithAuditTrail(audittrail.NewService(audittrail.NewMemoryStore())),
	)

	admin, _, err := identityService.Bootstrap(t.Context(), tenancy.DefaultOrganizationID, "admin@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	_, sessionToken, csrfToken, err := sessionService.Create(t.Context(), admin.ID, admin.OrganizationID)
	if err != nil {
		t.Fatal(err)
	}
	return handler, []*http.Cookie{{Name: "bazusop_session", Value: sessionToken}, {Name: "bazusop_csrf", Value: csrfToken}}, serviceAccountService
}
```

Üç mevcut çağrı sitesini güncelle (`handler, adminCookies := newServiceAccountHandler(t)` → `handler, adminCookies, _ := newServiceAccountHandler(t)`), ve `TestServiceAccountTokenAuthenticatesAndCreatesAJob`/`TestRevokedServiceAccountTokenIsRejected`/`TestServiceAccountTokenCannotManageOtherServiceAccounts`'taki `createRequest`/`revokeRequest`'e CSRF header ekleyin (cookie döngüsünden hemen sonra):

```go
	createRequest.Header.Set("X-CSRF-Token", csrfTokenFromCookies(adminCookies))
```

(`TestRevokedServiceAccountTokenIsRejected`'daki `revokeRequest`'e de aynı satırı ekle.)

- [ ] **Adım 8: Yeni scope-bypass testlerini ekle**

Dosyanın sonuna ekle:

```go
func TestServiceAccountCreateAndListRejectAMismatchedSiteIDInThePath(t *testing.T) {
	t.Parallel()
	handler, adminCookies, _ := newServiceAccountHandler(t)
	csrfToken := csrfTokenFromCookies(adminCookies)

	createRequest := httptest.NewRequest(http.MethodPost, "/api/v1/sites/a-different-site/service-accounts", encodeJSON(t, map[string]any{
		"name": "ci-bot", "role": "operator",
	}))
	for _, cookie := range adminCookies {
		createRequest.AddCookie(cookie)
	}
	createRequest.Header.Set("X-CSRF-Token", csrfToken)
	createResponse := httptest.NewRecorder()
	handler.ServeHTTP(createResponse, createRequest)
	if createResponse.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for a site ID that does not match the hub's configured scope, got %d: %s", createResponse.Code, createResponse.Body.String())
	}

	listRequest := httptest.NewRequest(http.MethodGet, "/api/v1/sites/a-different-site/service-accounts", nil)
	for _, cookie := range adminCookies {
		listRequest.AddCookie(cookie)
	}
	listResponse := httptest.NewRecorder()
	handler.ServeHTTP(listResponse, listRequest)
	if listResponse.Code != http.StatusNotFound {
		t.Fatalf("expected 404 listing service accounts for a mismatched site ID, got %d: %s", listResponse.Code, listResponse.Body.String())
	}
}

func TestServiceAccountRotateRevokeDisableRejectAnAccountFromAnotherSite(t *testing.T) {
	t.Parallel()
	handler, adminCookies, serviceAccountService := newServiceAccountHandler(t)
	csrfToken := csrfTokenFromCookies(adminCookies)

	// Seed an account under a DIFFERENT site directly through the same
	// service instance the handler uses — the HTTP layer no longer allows
	// creating one this way (see the test above), which is exactly why
	// this shortcut is needed to set up the scenario.
	account, token, err := serviceAccountService.CreateAccount(t.Context(), tenancy.DefaultOrganizationID, "a-different-site", "other-site-bot", authorization.SiteRoleOperator, 0)
	if err != nil {
		t.Fatalf("seed cross-site account: %v", err)
	}
	tokenID, _, ok := serviceaccounts.ParseToken(token)
	if !ok {
		t.Fatal("expected a parseable token")
	}

	rotateRequest := httptest.NewRequest(http.MethodPost, "/api/v1/service-accounts/"+account.ID+"/rotate", encodeJSON(t, map[string]int{"expiry_days": 0}))
	for _, cookie := range adminCookies {
		rotateRequest.AddCookie(cookie)
	}
	rotateRequest.Header.Set("X-CSRF-Token", csrfToken)
	rotateResponse := httptest.NewRecorder()
	handler.ServeHTTP(rotateResponse, rotateRequest)
	if rotateResponse.Code != http.StatusNotFound {
		t.Fatalf("expected 404 rotating another site's service account, got %d: %s", rotateResponse.Code, rotateResponse.Body.String())
	}

	revokeRequest := httptest.NewRequest(http.MethodDelete, "/api/v1/service-accounts/tokens/"+tokenID, nil)
	for _, cookie := range adminCookies {
		revokeRequest.AddCookie(cookie)
	}
	revokeRequest.Header.Set("X-CSRF-Token", csrfToken)
	revokeResponse := httptest.NewRecorder()
	handler.ServeHTTP(revokeResponse, revokeRequest)
	if revokeResponse.Code != http.StatusNotFound {
		t.Fatalf("expected 404 revoking another site's service account token, got %d: %s", revokeResponse.Code, revokeResponse.Body.String())
	}

	disableRequest := httptest.NewRequest(http.MethodDelete, "/api/v1/service-accounts/"+account.ID, nil)
	for _, cookie := range adminCookies {
		disableRequest.AddCookie(cookie)
	}
	disableRequest.Header.Set("X-CSRF-Token", csrfToken)
	disableResponse := httptest.NewRecorder()
	handler.ServeHTTP(disableResponse, disableRequest)
	if disableResponse.Code != http.StatusNotFound {
		t.Fatalf("expected 404 disabling another site's service account, got %d: %s", disableResponse.Code, disableResponse.Body.String())
	}

	if _, err := serviceAccountService.Validate(t.Context(), token, "10.0.0.1"); err != nil {
		t.Fatalf("expected the other-site account's token to remain valid after all rejected attempts, got %v", err)
	}
}
```

- [ ] **Adım 9: Testleri çalıştır**

Çalıştır: `GOCACHE=/tmp/bazusop-go-cache go test ./internal/server/... -run 'TestServiceAccount' -v 2>&1 | tail -100`
Beklenen: BAŞARILI (tüm testler)

- [ ] **Adım 10: Full build/vet/gofmt**

Çalıştır: `go build ./... 2>&1 && echo BUILD_OK && go vet ./... 2>&1 && echo VET_OK && gofmt -l internal/serviceaccounts/*.go internal/server/*.go`
Beklenen: ikisi de başarılı, gofmt çıktısı yok

- [ ] **Adım 11: Commit**

```bash
git add internal/serviceaccounts/serviceaccounts.go internal/serviceaccounts/serviceaccounts_test.go internal/server/serviceaccounts.go internal/server/serviceaccounts_test.go
git commit -m "fix: verify service account rotate/revoke/disable target the caller's own site"
```

## Görev 3: Brute-force rate limiting

**Dosyalar:**
- Oluştur: `internal/ratelimit/ratelimit.go`, `internal/ratelimit/ratelimit_test.go`
- Değiştir: `internal/server/sessions.go`, `internal/server/identity.go`, `internal/server/server.go`

**Arayüzler:**
- Üretir: `ratelimit.Limiter`, `ratelimit.New(max int, window time.Duration, options ...Option) *Limiter`, `(*Limiter) Allow(key string) bool`, `ratelimit.WithClock(func() time.Time) Option`.

- [ ] **Adım 1: `internal/ratelimit/ratelimit.go`'yu yaz**

```go
// Package ratelimit provides a minimal, process-local sliding-window rate
// limiter for brute-force protection on unauthenticated, high-value
// endpoints (login, MFA confirmation, invite consumption — see the design
// spec's "Rate limiting" requirement). It is deliberately NOT synchronized
// across hub replicas: see docs/OPERATIONS.md for why this is an accepted
// v1 trade-off rather than a full distributed limiter.
package ratelimit

import (
	"sync"
	"time"
)

type Limiter struct {
	mu       sync.Mutex
	attempts map[string][]time.Time
	max      int
	window   time.Duration
	now      func() time.Time
}

type Option func(*Limiter)

func WithClock(clock func() time.Time) Option { return func(limiter *Limiter) { limiter.now = clock } }

func New(max int, window time.Duration, options ...Option) *Limiter {
	limiter := &Limiter{attempts: make(map[string][]time.Time), max: max, window: window, now: func() time.Time { return time.Now().UTC() }}
	for _, option := range options {
		option(limiter)
	}
	return limiter
}

// Allow reports whether key may proceed. Expired attempts for key are
// always pruned first; a new attempt is recorded only when this call
// returns true, so a caller spamming a blocked key does not keep resetting
// its own window.
func (limiter *Limiter) Allow(key string) bool {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	now := limiter.now()
	cutoff := now.Add(-limiter.window)
	kept := limiter.attempts[key][:0]
	for _, at := range limiter.attempts[key] {
		if at.After(cutoff) {
			kept = append(kept, at)
		}
	}
	if len(kept) >= limiter.max {
		limiter.attempts[key] = kept
		return false
	}
	limiter.attempts[key] = append(kept, now)
	return true
}
```

- [ ] **Adım 2: `internal/ratelimit/ratelimit_test.go`'yu yaz**

```go
package ratelimit_test

import (
	"testing"
	"time"

	"github.com/gokayybaz/bazusop/internal/ratelimit"
)

func TestAllowPermitsUpToMaxThenBlocks(t *testing.T) {
	t.Parallel()
	limiter := ratelimit.New(3, time.Minute)
	for i := 0; i < 3; i++ {
		if !limiter.Allow("key-a") {
			t.Fatalf("expected attempt %d to be allowed", i+1)
		}
	}
	if limiter.Allow("key-a") {
		t.Fatal("expected the 4th attempt within the window to be blocked")
	}
}

func TestAllowTracksKeysIndependently(t *testing.T) {
	t.Parallel()
	limiter := ratelimit.New(1, time.Minute)
	if !limiter.Allow("key-a") {
		t.Fatal("expected the first attempt for key-a to be allowed")
	}
	if !limiter.Allow("key-b") {
		t.Fatal("expected key-b's own limit to be independent of key-a's")
	}
	if limiter.Allow("key-a") {
		t.Fatal("expected key-a to still be blocked")
	}
}

func TestAllowResetsAfterTheWindowElapses(t *testing.T) {
	t.Parallel()
	current := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := func() time.Time { return current }
	limiter := ratelimit.New(1, time.Minute, ratelimit.WithClock(clock))
	if !limiter.Allow("key-a") {
		t.Fatal("expected the first attempt to be allowed")
	}
	if limiter.Allow("key-a") {
		t.Fatal("expected the second attempt to be blocked within the window")
	}
	current = current.Add(2 * time.Minute)
	if !limiter.Allow("key-a") {
		t.Fatal("expected a new attempt to be allowed once the window has elapsed")
	}
}
```

- [ ] **Adım 3: Testleri çalıştır**

Çalıştır: `GOCACHE=/tmp/bazusop-go-cache go test ./internal/ratelimit/... -v 2>&1 | tail -30`
Beklenen: BAŞARILI

- [ ] **Adım 4: `internal/server/sessions.go`'daki `handleLogin`'i rate-limit edecek şekilde güncelle**

Import listesine `"github.com/gokayybaz/bazusop/internal/ratelimit"` ekle.

```go
func handleLogin(identityService *identity.Service, sessionService *sessions.Service, limiter *ratelimit.Limiter) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		if !limiter.Allow(sourceIP(request)) {
			http.Error(response, http.StatusText(http.StatusTooManyRequests), http.StatusTooManyRequests)
			return
		}
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
		...
```

(Fonksiyonun geri kalanı değişmeden kalır.)

- [ ] **Adım 5: `internal/server/identity.go`'daki `handleConfirmTOTP` ve `handleConsumeInvite`'ı rate-limit edecek şekilde güncelle**

Import listesine `"github.com/gokayybaz/bazusop/internal/ratelimit"` ekle.

```go
func handleConsumeInvite(service *identity.Service, limiter *ratelimit.Limiter) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		if !limiter.Allow(sourceIP(request)) {
			http.Error(response, http.StatusText(http.StatusTooManyRequests), http.StatusTooManyRequests)
			return
		}
		var body struct {
			Password string `json:"password"`
		}
		if err := decodeJSON(response, request, &body); err != nil {
			return
		}
		user, err := service.ConsumeInvite(request.Context(), request.PathValue("token"), body.Password)
		if errors.Is(err, identity.ErrInviteNotFound) {
			http.Error(response, http.StatusText(http.StatusNotFound), http.StatusNotFound)
			return
		}
		if errors.Is(err, identity.ErrInviteExpired) {
			http.Error(response, http.StatusText(http.StatusConflict), http.StatusConflict)
			return
		}
		if errors.Is(err, identity.ErrInviteWrongIdentityType) {
			http.Error(response, http.StatusText(http.StatusForbidden), http.StatusForbidden)
			return
		}
		if err != nil {
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		writeJSON(response, http.StatusCreated, struct {
			ID string `json:"id"`
		}{user.ID})
	}
}

func handleConfirmTOTP(service *identity.Service, limiter *ratelimit.Limiter) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		if !limiter.Allow(sourceIP(request)) {
			http.Error(response, http.StatusText(http.StatusTooManyRequests), http.StatusTooManyRequests)
			return
		}
		var body struct {
			Code string `json:"code"`
		}
		if err := decodeJSON(response, request, &body); err != nil {
			return
		}
		enrollment, err := service.ConfirmTOTP(request.Context(), request.PathValue("userID"), func([]byte) string { return body.Code }, time.Now().UTC())
		...
```

(`handleConfirmTOTP`'un geri kalanı değişmeden kalır; `handleBootstrap`, `handleCreateInvite` değişmez — davet OLUŞTURMA zaten `PermissionManageUsers` ile korunuyor, brute-force yüzeyi değil.)

**Yazarken düzeltme:** `handleConsumeInvite`'a eklenen `identity.ErrInviteWrongIdentityType` kontrolü, 11.9'da eklenen ama hiç ele alınmayan bir hata dalını düzeltiyor — önceden bu durum genel `500`'e düşüyordu, şimdi doğru şekilde `403` döner.

- [ ] **Adım 6: `internal/server/server.go`'ya üç limiter'ı oluştur ve kabloya**

Import listesine `"time"` ve `"github.com/gokayybaz/bazusop/internal/ratelimit"` ekle.

`NewHandler`'ın başına, `mux := http.NewServeMux()` satırından hemen önce ekle:

```go
	loginLimiter := ratelimit.New(10, 5*time.Minute)
	confirmTOTPLimiter := ratelimit.New(10, 5*time.Minute)
	consumeInviteLimiter := ratelimit.New(10, 5*time.Minute)
```

`handleLogin` çağrısını güncelle:

```go
		registerAudited(mux, "/api/v1/sessions", http.MethodPost, "sessions", nil, configuration.auditTrail, configuration.scope, handleLogin(configuration.sessionIdentityService, configuration.sessionService, loginLimiter))
```

`handleConsumeInvite`/`handleConfirmTOTP` çağrılarını güncelle:

```go
		registerAudited(mux, "/api/v1/invites/{token}/consume", http.MethodPost, "invites", []string{"token"}, configuration.auditTrail, configuration.scope, handleConsumeInvite(configuration.identityService, consumeInviteLimiter))
		registerAudited(mux, "/api/v1/users/{userID}/confirm-totp", http.MethodPost, "users", []string{"userID"}, configuration.auditTrail, configuration.scope, handleConfirmTOTP(configuration.identityService, confirmTOTPLimiter))
```

- [ ] **Adım 7: Build'i doğrula**

Çalıştır: `go build ./... 2>&1 | head -30`
Beklenen: üretim kodu BAŞARILI; test dosyaları henüz güncellenmediği için `go vet`/`go test` bu handler'ları çağıran yerlerde başarısız OLMAZ (imzaları değiştirmedik, yalnız server.go'daki dahili çağrı sitelerini değiştirdik — hiçbir test dosyası `handleLogin`/`handleConsumeInvite`/`handleConfirmTOTP`'ı doğrudan çağırmıyor, hepsi `server.NewHandler(...)` üzerinden HTTP ile test ediliyor). `go build ./...` zaten başarılıysa bu adım tamamdır.

- [ ] **Adım 8: `internal/server/sessions_test.go`/`identity_test.go`'daki mevcut testlerin YİNE de geçtiğini doğrula (limiter varsayılan eşiği aşmadıkları için)**

Çalıştır: `GOCACHE=/tmp/bazusop-go-cache go test ./internal/server/... -run 'TestLogin|TestBootstrap|TestCreateInvite|TestAdminCanRevoke' -v 2>&1 | tail -100`
Beklenen: BAŞARILI (her test en fazla 1-2 kez login/confirm-totp/consume-invite çağırıyor, eşik 10 — hiçbiri tetiklenmez)

- [ ] **Adım 9: Rate limiting'in gerçekten tetiklendiğini kanıtlayan yeni testler ekle**

`internal/server/sessions_test.go`'nun sonuna ekle:

```go
func TestLoginIsRateLimitedPerSourceIP(t *testing.T) {
	t.Parallel()
	handler, _ := newSessionHandler(t)
	email, password := "admin@example.com", "correct horse battery staple"
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/v1/bootstrap", encodeJSON(t, map[string]string{
		"secret": "bootstrap-secret", "email": email, "password": password,
	})))

	for i := 0; i < 10; i++ {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/sessions", encodeJSON(t, map[string]string{
			"email": email, "password": "wrong password", "totp_code": "000000",
		})))
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: expected 401 for a wrong password within the rate limit, got %d", i+1, response.Code)
		}
	}

	limited := httptest.NewRecorder()
	handler.ServeHTTP(limited, httptest.NewRequest(http.MethodPost, "/api/v1/sessions", encodeJSON(t, map[string]string{
		"email": email, "password": "wrong password", "totp_code": "000000",
	})))
	if limited.Code != http.StatusTooManyRequests {
		t.Fatalf("expected the 11th attempt within 5 minutes to be rate limited, got %d", limited.Code)
	}
}
```

`internal/server/identity_test.go`'nun sonuna ekle:

```go
func TestConfirmTOTPIsRateLimitedPerSourceIP(t *testing.T) {
	t.Parallel()
	handler := newIdentityHandler(t, "correct-secret")
	cookies := bootstrapAndLogin(t, handler, "correct-secret", "admin@example.com", "correct horse battery staple")

	inviteRequest := httptest.NewRequest(http.MethodPost, "/api/v1/users/invites", encodeJSON(t, map[string]string{"email": "new-user@example.com"}))
	for _, cookie := range cookies {
		inviteRequest.AddCookie(cookie)
	}
	inviteRequest.Header.Set("X-CSRF-Token", csrfTokenFromCookies(cookies))
	inviteResponse := httptest.NewRecorder()
	handler.ServeHTTP(inviteResponse, inviteRequest)
	var invitePayload struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(inviteResponse.Body).Decode(&invitePayload); err != nil || invitePayload.Token == "" {
		t.Fatalf("expected a non-empty invite token, got %#v, %v", invitePayload, err)
	}

	consumeResponse := httptest.NewRecorder()
	handler.ServeHTTP(consumeResponse, httptest.NewRequest(http.MethodPost, "/api/v1/invites/"+invitePayload.Token+"/consume", encodeJSON(t, map[string]string{"password": "a brand new password"})))
	var consumedUser struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(consumeResponse.Body).Decode(&consumedUser); err != nil || consumedUser.ID == "" {
		t.Fatalf("expected a user id, got %#v, %v", consumedUser, err)
	}

	for i := 0; i < 10; i++ {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/users/"+consumedUser.ID+"/confirm-totp", encodeJSON(t, map[string]string{"code": "000000"})))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("attempt %d: expected 400 for a wrong TOTP code within the rate limit, got %d", i+1, response.Code)
		}
	}

	limited := httptest.NewRecorder()
	handler.ServeHTTP(limited, httptest.NewRequest(http.MethodPost, "/api/v1/users/"+consumedUser.ID+"/confirm-totp", encodeJSON(t, map[string]string{"code": "000000"})))
	if limited.Code != http.StatusTooManyRequests {
		t.Fatalf("expected the 11th attempt within 5 minutes to be rate limited, got %d", limited.Code)
	}
}

func TestConsumeInviteIsRateLimitedPerSourceIP(t *testing.T) {
	t.Parallel()
	handler := newIdentityHandler(t, "correct-secret")
	for i := 0; i < 10; i++ {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/invites/not-a-real-token/consume", encodeJSON(t, map[string]string{"password": "whatever"})))
		if response.Code != http.StatusNotFound {
			t.Fatalf("attempt %d: expected 404 for an unknown invite token within the rate limit, got %d", i+1, response.Code)
		}
	}

	limited := httptest.NewRecorder()
	handler.ServeHTTP(limited, httptest.NewRequest(http.MethodPost, "/api/v1/invites/not-a-real-token/consume", encodeJSON(t, map[string]string{"password": "whatever"})))
	if limited.Code != http.StatusTooManyRequests {
		t.Fatalf("expected the 11th attempt within 5 minutes to be rate limited, got %d", limited.Code)
	}
}
```

- [ ] **Adım 10: Testleri çalıştır**

Çalıştır: `GOCACHE=/tmp/bazusop-go-cache go test ./internal/server/... -run 'RateLimited' -v 2>&1 | tail -60`
Beklenen: BAŞARILI (üç yeni test)

- [ ] **Adım 11: Tam `internal/server` ve `internal/ratelimit` testlerini çalıştır**

Çalıştır: `go vet ./... && gofmt -l internal/ratelimit/*.go internal/server/*.go && GOCACHE=/tmp/bazusop-go-cache go test ./internal/ratelimit/... ./internal/server/... -v 2>&1 | tail -300`
Beklenen: vet/gofmt çıktısı yok; TÜM testler BAŞARILI

- [ ] **Adım 12: Commit**

```bash
git add internal/ratelimit internal/server/sessions.go internal/server/identity.go internal/server/server.go internal/server/identity_test.go
git commit -m "feat: rate limit login, TOTP confirmation and invite consumption per source IP"
```

## Görev 4: Redaksiyon doğrulama testleri

**Dosyalar:** Değiştir: `internal/server/identity_test.go`, `internal/server/serviceaccounts_test.go`

- [ ] **Adım 1: `internal/server/identity_test.go`'ya whoami'nin parola/TOTP secret'ı hiç döndürmediğini kanıtlayan bir test ekle**

```go
func TestWhoAmIResponseNeverIncludesPasswordOrTOTPSecret(t *testing.T) {
	t.Parallel()
	handler := newIdentityHandler(t, "correct-secret")
	cookies := bootstrapAndLogin(t, handler, "correct-secret", "admin@example.com", "correct horse battery staple")

	request := httptest.NewRequest(http.MethodGet, "/api/v1/session", nil)
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	for _, forbidden := range []string{"correct horse battery staple", "password_hash", "totp_secret", "PasswordHash", "TOTPSecretEncrypted"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("whoami response leaked a secret field/value (%q): %s", forbidden, body)
		}
	}
}
```

(Dosyanın import listesine `"strings"` eklenmesi gerekiyorsa ekle.)

- [ ] **Adım 2: `internal/server/serviceaccounts_test.go`'ya listeleme yanıtının token içermediğini kanıtlayan bir test ekle**

```go
func TestListServiceAccountsResponseNeverIncludesTheToken(t *testing.T) {
	t.Parallel()
	handler, adminCookies, _ := newServiceAccountHandler(t)
	csrfToken := csrfTokenFromCookies(adminCookies)

	createRequest := httptest.NewRequest(http.MethodPost, "/api/v1/sites/site_default/service-accounts", encodeJSON(t, map[string]any{
		"name": "ci-bot", "role": "operator",
	}))
	for _, cookie := range adminCookies {
		createRequest.AddCookie(cookie)
	}
	createRequest.Header.Set("X-CSRF-Token", csrfToken)
	createResponse := httptest.NewRecorder()
	handler.ServeHTTP(createResponse, createRequest)
	var created struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(createResponse.Body).Decode(&created); err != nil || created.Token == "" {
		t.Fatalf("expected a one-time token from creation, got %v", err)
	}

	listRequest := httptest.NewRequest(http.MethodGet, "/api/v1/sites/site_default/service-accounts", nil)
	for _, cookie := range adminCookies {
		listRequest.AddCookie(cookie)
	}
	listResponse := httptest.NewRecorder()
	handler.ServeHTTP(listResponse, listRequest)
	if strings.Contains(listResponse.Body.String(), created.Token) {
		t.Fatalf("expected the list response to never include the raw token, got %s", listResponse.Body.String())
	}
}
```

(Dosyanın import listesine `"strings"` eklenmesi gerekiyorsa ekle.)

- [ ] **Adım 2: Testleri çalıştır**

Çalıştır: `GOCACHE=/tmp/bazusop-go-cache go test ./internal/server/... -run 'NeverIncludes' -v 2>&1 | tail -30`
Beklenen: BAŞARILI

- [ ] **Adım 3: Commit**

```bash
git add internal/server/identity_test.go internal/server/serviceaccounts_test.go
git commit -m "test: prove whoami and service-account listing never leak secret fields"
```

## Görev 5: Dokümantasyon

**Dosyalar:** Değiştir: `docs/API.md`, `docs/OPERATIONS.md`

- [ ] **Adım 1: `docs/API.md`'yi güncelle**

`## Kimlik doğrulama` bölümüne ekle:

```markdown
- Oturum çerezi tabanlı GET/HEAD DIŞI her istek `X-CSRF-Token` header'ının
  oturumun CSRF token'ıyla eşleşmesini ister (double-submit-cookie deseni —
  token hem oturum açılışında JSON yanıtında hem de `HttpOnly` OLMAYAN
  `bazusop_csrf` çerezinde döner). Eksik veya yanlış token `403` döner.
  Servis hesabı bearer token'ları bu kontrole tabi değildir (CSRF yalnız
  tarayıcı çerezlerini otomatik gönderen istekleri hedefler).
- Giriş (`POST /api/v1/sessions`), MFA onayı
  (`POST /api/v1/users/{userID}/confirm-totp`) ve davet tüketimi
  (`POST /api/v1/invites/{token}/consume`) kaynak IP başına dakikada
  brute-force sınırlıdır (varsayılan: 5 dakikada 10 deneme); aşıldığında
  `429` döner.
```

- [ ] **Adım 2: `docs/OPERATIONS.md`'ye rate limiting'in process-içi olduğunu belgeleyen bir not ekle**

Uygun bir yere (örn. "Sağlık ve sorun giderme" bölümünden önce) ekle:

```markdown
## Rate limiting

Giriş, MFA onayı ve davet tüketimi uçları kaynak IP başına process-içi bir
sliding-window ile sınırlıdır (varsayılan: 5 dakikada 10 deneme, aşıldığında
`429`). Bu sınır HUB REPLİKALARI ARASINDA SENKRONİZE DEĞİLDİR — birden fazla
replika arkasında her replika kendi sınırını bağımsız uygular, bu yüzden
etkili sınır replika sayısıyla orantılı büyür. Bu, mevcut hiçbir korumaya
kıyasla önemli bir iyileştirmedir; tam dağıtık bir çözüm gelecekte ayrı bir
sertleştirme çalışmasıdır.
```

- [ ] **Adım 3: Commit**

```bash
git add docs/API.md docs/OPERATIONS.md
git commit -m "docs: document CSRF enforcement and rate limiting behavior"
```

## Görev 6: Manuel doğrulama

**Dosyalar:** yok (yalnız doğrulama).

- [ ] **Adım 1: Docker Compose ile hub'ı ayağa kaldır**

Çalıştır: `docker compose -p bazusop-verify-1110 down -v >/dev/null 2>&1; POSTGRES_PASSWORD=verify-pw BAZUSOP_ENROLLMENT_TOKEN=verify-token BAZUSOP_BOOTSTRAP_SECRET=verify-bootstrap BAZUSOP_TOTP_ENCRYPTION_KEY=verify-totp-key BAZUSOP_SERVICE_ACCOUNT_PEPPER=verify-pepper BAZUSOP_PORT=18099 docker compose -p bazusop-verify-1110 up --build -d` ve `/api/v1/health`'in 200 dönmesini bekle.

- [ ] **Adım 2: Bootstrap ol, giriş yap**

```bash
rm -f /tmp/bazusop-1110-cookies.txt
curl -s -c /tmp/bazusop-1110-cookies.txt -X POST http://127.0.0.1:18099/api/v1/bootstrap \
  -d '{"secret":"verify-bootstrap","email":"admin@example.com","password":"correct horse battery staple"}' > /tmp/bazusop-1110-bootstrap.json
CODE=$(python3 -c "import json; print(json.load(open('/tmp/bazusop-1110-bootstrap.json'))['recovery_codes'][0])")
curl -s -c /tmp/bazusop-1110-cookies.txt -b /tmp/bazusop-1110-cookies.txt -X POST http://127.0.0.1:18099/api/v1/sessions \
  -d '{"email":"admin@example.com","password":"correct horse battery staple","recovery_code":"'"$CODE"'"}' -w "\nlogin:%{http_code}\n" > /tmp/bazusop-1110-login.json
cat /tmp/bazusop-1110-login.json
```

Beklenen: `201`.

- [ ] **Adım 3: CSRF token olmadan bir mutasyonun `403` döndüğünü doğrula**

```bash
CSRF=$(python3 -c "import json; print(json.load(open('/tmp/bazusop-1110-login.json').__enter__() if False else open('/tmp/bazusop-1110-login.json'))['csrf_token'])" 2>/dev/null || python3 -c "
import json
with open('/tmp/bazusop-1110-login.json') as f:
    lines = f.read().split('\n')
    print(json.loads(lines[0])['csrf_token'])
")
curl -s -o /dev/null -w "CSRF header olmadan davet (beklenen 403): %{http_code}\n" -b /tmp/bazusop-1110-cookies.txt -X POST http://127.0.0.1:18099/api/v1/users/invites \
  -d '{"email":"viewer@example.com"}'
curl -s -o /dev/null -w "doğru CSRF header ile davet (beklenen 201): %{http_code}\n" -b /tmp/bazusop-1110-cookies.txt -X POST http://127.0.0.1:18099/api/v1/users/invites \
  -H "X-CSRF-Token: $CSRF" -d '{"email":"viewer@example.com"}'
```

Beklenen: sırasıyla `403` ve `201`.

- [ ] **Adım 4: Brute-force rate limiting'in gerçekten tetiklendiğini doğrula**

```bash
for i in $(seq 1 11); do
  code=$(curl -s -o /dev/null -w "%{http_code}" -X POST http://127.0.0.1:18099/api/v1/sessions \
    -d '{"email":"admin@example.com","password":"yanlis-parola","totp_code":"000000"}')
  echo "deneme $i: $code"
done
```

Beklenen: ilk 10 deneme `401`, 11. deneme `429`.

- [ ] **Adım 5: Servis hesabı site-scope IDOR düzeltmesini doğrula**

```bash
curl -s -b /tmp/bazusop-1110-cookies.txt -H "X-CSRF-Token: $CSRF" -X POST http://127.0.0.1:18099/api/v1/sites/baska-bir-site/service-accounts \
  -d '{"name":"evil-bot","role":"operator"}' -w "\nbaşka site (beklenen 404): %{http_code}\n"
```

Beklenen: `404`.

- [ ] **Adım 6: Temizlik**

```bash
docker compose -p bazusop-verify-1110 down -v
rm -f /tmp/bazusop-1110-cookies.txt /tmp/bazusop-1110-bootstrap.json /tmp/bazusop-1110-login.json
```

- [ ] **Adım 7: Full test suite'i son kez çalıştır (Postgres dahil)**

Çalıştır: `docker rm -f bazusop-test-pg >/dev/null 2>&1; docker run -d --name bazusop-test-pg -e POSTGRES_USER=bazusop -e POSTGRES_PASSWORD=bazusop_test -e POSTGRES_DB=bazusop_test -p 5432:5432 postgres:18 >/dev/null` ve hazır olmasını bekle.

Çalıştır: `BAZUSOP_TEST_DATABASE_URL='postgres://bazusop:bazusop_test@127.0.0.1:5432/bazusop_test?sslmode=disable' GOCACHE=/tmp/bazusop-go-cache go test ./... -count=1 2>&1 | tail -30`
Beklenen: her paket `ok`

Çalıştır: `docker rm -f bazusop-test-pg`

## Kendi Kendine İnceleme

**1. Spec kapsaması.**
- "Scope bypass... testleri geçer" → Görev 2 (servis hesabı rotate/revoke/disable site doğrulaması + create/list path-scope tutarlılığı, hem domain hem HTTP katmanında test edilmiş).
- "CSRF... testleri geçer" → Görev 1 (`requirePermission`'ın merkezi düzeltmesi, TÜM mutasyon uçlarını geriye dönük koruyor, `TestMutationsRequireACSRFTokenEvenWithAValidSession` ile doğrudan kanıtlanmış).
- "Brute-force... testleri geçer" → Görev 3 (yeni `internal/ratelimit` paketi, üç açık uca bağlı, üç ayrı testte 429'un gerçekten tetiklendiği kanıtlanmış).
- "Redaksiyon testleri geçer" → Görev 4 (whoami ve servis hesabı listeleme yanıtlarının secret sızdırmadığı doğrudan test edilmiş; genel redaksiyon zaten inşa gereği sağlanıyordu — bu görev bunu KANITLIYOR).
- "Rate limiting giriş, MFA, davet tüketimi... uçlarında" (Hata ve güvenli kapanma davranışı) → Görev 3'ün üç kablolama noktası birebir bu üç örneği kapsıyor.
- Kod incelemesiyle bulunan ek düzeltme (`handleConsumeInvite`'ın `ErrInviteWrongIdentityType`'ı hiç ele almaması, 11.9'dan kalma) → Görev 3 Adım 5'te düzeltildi.

**2. Placeholder taraması.** Her adımda gerçek, eksiksiz kod var. Görev 1 Adım 15'teki "Yazarken düzeltme" notu, ilk taslağın uydurma bir `handlerIdentityServiceFor` yardımcı fonksiyonu kullandığını fark edip `newRBACHandler`'ın zaten döndürdüğü `identityService`'i kullanacak şekilde düzeltiyor — önceki spike'ların "yazarken düzeltme" konvansiyonunu takip ediyor.

**3. Tip tutarlılığı.** `ratelimit.Limiter`/`New`/`Allow`/`WithClock` Görev 3 Adım 1'de tanımlandığı gibi Adım 4-6'da birebir kullanılıyor. `serviceaccounts.Service.RotateToken`/`RevokeToken`/`DisableAccount`'ın yeni `siteID` parametresi Görev 2'nin TÜM çağrı sitelerinde (domain testleri, HTTP handler'ları, HTTP testleri) tutarlı. `csrfTokenFromCookies` Görev 1 Adım 3'te tanımlanıp sonraki tüm adımlarda (Görev 2, Görev 3, Görev 4 dahil) aynı imzayla kullanılıyor.
