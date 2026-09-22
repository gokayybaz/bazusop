# Bootstrap ve Davet/TOTP Onboarding (Spike 13.2) Uygulama Planı

> **Otonom çalışanlar için:** ZORUNLU ALT-SKILL: Bu planı görev görev uygulamak için superpowers:subagent-driven-development (önerilen) veya superpowers:executing-plans kullan. Adımlar `- [ ]` checkbox söz dizimiyle takip edilir.

**Amaç:** Davet → TOTP onboarding akışını gerçek bir tarayıcı istemcisiyle uçtan uca çalışır hale getirir (backend'de `ConsumeInvite` hiç TOTP secret'ı üretip döndürmüyordu; `ConfirmTOTP` her denemede yeni bir secret üretiyordu — kullanıcı doğru kodu asla üretemezdi), ve frontend'e tarayıcıdan erişilebilen bir ilk kurulum (`/setup`) ile davet kabul/TOTP kurulum (`/invite/:token`) ekranı ekler.

**Mimari:** Backend tarafında `ConsumeInvite` artık `Bootstrap`'ın izlediği deseni takip eder: TOTP secret'ı hemen üretip kalıcılaştırır ve `provisioning_uri`'yi döndürür; `ConfirmTOTP` artık yeni secret üretmez, yalnız zaten kalıcılaşmış olanı doğrular. Frontend tarafında paylaşılan iki bileşen (`TotpQrCode`, `RecoveryCodes`) hem yeni `BootstrapPage` (`/setup`) hem `InvitePage` (`/invite/:token`) tarafından kullanılır; ikisi de `ProtectedRoute`'un dışında, `/login` ile aynı seviyede public route'lardır. QR render'ı `qrcode` paketinin SVG çıktısıyla yapılır (canvas/DOM bağımlılığı yok, jsdom testlerinde sorunsuz çalışır — bu plan yazılırken doğrulandı).

**Teknoloji yığını:** Go 1.26 (backend düzeltmesi), React 19 + TypeScript + `qrcode` (yeni bağımlılık, SVG QR render).

**Spec:** [docs/superpowers/specs/2026-09-22-frontend-identity-rbac-ui-design.md](../specs/2026-09-22-frontend-identity-rbac-ui-design.md) — bu plan yalnız Spike 13.2'yi kapsar. Spike 13.1 ([2026-09-22-frontend-session-shell.md](2026-09-22-frontend-session-shell.md)) zaten uygulandı ve `main`'e merge edildi; bu plan onun üzerine inşa eder (`SessionProvider`, `apiFetch`, `ProtectedRoute`, `app.tsx`'in router kökü zaten mevcut).

## Genel Kısıtlar

- **`ConfirmTOTP`'un "secret yoksa yeni üret" dalı tamamen kaldırılır**, `hasSecret` yardımcı fonksiyonuyla birlikte — `ConsumeInvite` artık HER ZAMAN bir secret üretip kalıcılaştırdığından (Bootstrap zaten kendi secret'ını üretiyor), `ConfirmTOTP`'a ulaşan her kullanıcının bir secret'ı olması garantidir; bu dal artık ulaşılamaz kod olurdu.
- **`BootstrapPage`/`InvitePage` oturum durumuna göre yönlendirme YAPMAZ** (LoginPage'in aksine) — zaten giriş yapmış birinin `/setup`'a gitmesi backend'de zaten güvenle `409` ile karşılanıyor; ayrı bir client-side kontrol eklemek YAGNI'dir.
- **"Kurulu mu?" diye ayrı bir keşif ucu eklenmez** — `BootstrapPage` her zaman formu gösterir; `POST /api/v1/bootstrap`'ın `409` yanıtı "zaten kurulu" durumunu taşır (spec'in kararı).
- **QR render'ı `qrcode`'un `toString(..., {type: "svg"})` çıktısıyla yapılır**, `toDataURL` (canvas gerektirir, jsdom'da güvenilir değildir) DEĞİL — bu plan yazılırken gerçek bir Node ortamında doğrulandı: SVG çıktısı saf string, canvas/DOM bağımlılığı yok.

## Dosya Yapısı

- Değiştir: `internal/identity/identity.go`, `internal/identity/identity_test.go`.
- Değiştir: `internal/server/identity.go`, `internal/server/identity_test.go`, `internal/server/rbac_test.go`, `internal/server/rbac_operational_test.go`.
- Değiştir: `web/package.json` (ve `npm install`'ın ürettiği `package-lock.json`).
- Oluştur: `web/src/components/totp-qr-code.tsx`, `web/src/components/totp-qr-code.test.tsx`.
- Oluştur: `web/src/components/recovery-codes.tsx`, `web/src/components/recovery-codes.test.tsx`.
- Oluştur: `web/src/pages/bootstrap.tsx`, `web/src/pages/bootstrap.test.tsx`.
- Oluştur: `web/src/pages/invite.tsx`, `web/src/pages/invite.test.tsx`.
- Değiştir: `web/src/app.tsx`, `web/src/pages/login.tsx`, `web/src/styles.css`.

## Görev 1: Backend — `ConsumeInvite`'ın TOTP secret'ı üretip döndürmesi

**Dosyalar:**
- Değiştir: `internal/identity/identity.go`
- Değiştir: `internal/identity/identity_test.go`

**Arayüzler:**
- Değiştirir: `Service.ConsumeInvite(ctx, token, password) (User, TOTPEnrollment, error)` (önceki imza: `(User, error)`). `Service.ConfirmTOTP` imzası değişmez ama artık dönen `TOTPEnrollment.ProvisioningURI` de doludur (önceden yalnız `RecoveryCodes` doluydu).

- [x] **Adım 1: `internal/identity/identity.go`'daki `ConsumeInvite`'ı güncelle**

```go
func (service *Service) ConsumeInvite(ctx context.Context, token, password string) (User, TOTPEnrollment, error) {
	tokenHash := hashToken(token)
	invite, err := service.store.InviteByTokenHash(ctx, tokenHash)
	if err != nil {
		return User{}, TOTPEnrollment{}, err
	}
	if invite.ConsumedAt != nil || invite.RevokedAt != nil || service.now().After(invite.ExpiresAt) {
		return User{}, TOTPEnrollment{}, ErrInviteExpired
	}
	if invite.IdentityType == IdentityTypeOIDC {
		return User{}, TOTPEnrollment{}, ErrInviteWrongIdentityType
	}
	passwordHash, err := HashPassword(password)
	if err != nil {
		return User{}, TOTPEnrollment{}, err
	}
	totpSecret, err := GenerateTOTPSecret()
	if err != nil {
		return User{}, TOTPEnrollment{}, err
	}
	encryptedSecret, err := EncryptTOTPSecret(totpSecret, service.totpEncryptionKey)
	if err != nil {
		return User{}, TOTPEnrollment{}, err
	}
	id, err := newID()
	if err != nil {
		return User{}, TOTPEnrollment{}, err
	}
	user := User{
		ID: id, OrganizationID: invite.OrganizationID, Email: invite.Email, Role: invite.Role,
		PasswordHash: passwordHash, TOTPSecretEncrypted: encryptedSecret, CreatedAt: service.now(),
	}
	if err := service.store.CreateUser(ctx, user); err != nil {
		return User{}, TOTPEnrollment{}, err
	}
	if len(invite.SiteRoleGrants) > 0 && service.siteRoleGrantor != nil {
		if err := service.siteRoleGrantor(ctx, user.ID, invite.SiteRoleGrants); err != nil {
			return User{}, TOTPEnrollment{}, err
		}
	}
	if err := service.store.ConsumeInvite(ctx, tokenHash, user.ID, service.now()); err != nil {
		return User{}, TOTPEnrollment{}, err
	}
	return user, TOTPEnrollment{ProvisioningURI: TOTPProvisioningURI("bazUSOP", invite.Email, totpSecret)}, nil
}
```

- [x] **Adım 2: `ConfirmTOTP`'u, artık secret üretmeyecek ve `ProvisioningURI`'yi de dönecek şekilde güncelle; `hasSecret`'ı sil**

```go
func (service *Service) ConfirmTOTP(ctx context.Context, userID string, codeFromSecret func(secret []byte) string, at time.Time) (TOTPEnrollment, error) {
	user, err := service.store.UserByID(ctx, userID)
	if err != nil {
		return TOTPEnrollment{}, err
	}
	if user.TOTPConfirmedAt != nil {
		return TOTPEnrollment{}, ErrTOTPAlreadyConfirmed
	}
	secret, err := DecryptTOTPSecret(user.TOTPSecretEncrypted, service.totpEncryptionKey)
	if err != nil {
		return TOTPEnrollment{}, err
	}
	submitted := codeFromSecret(secret)
	if !VerifyTOTPCode(secret, submitted, at) {
		return TOTPEnrollment{}, ErrInvalidTOTPCode
	}
	confirmedAt := at
	user.TOTPConfirmedAt = &confirmedAt
	if err := service.store.UpdateUser(ctx, user); err != nil {
		return TOTPEnrollment{}, err
	}
	codes, err := GenerateRecoveryCodes(10)
	if err != nil {
		return TOTPEnrollment{}, err
	}
	hashes := make([]string, len(codes))
	for index, code := range codes {
		hashes[index] = HashRecoveryCode(code)
	}
	if err := service.store.SaveRecoveryCodes(ctx, userID, hashes); err != nil {
		return TOTPEnrollment{}, err
	}
	return TOTPEnrollment{ProvisioningURI: TOTPProvisioningURI("bazUSOP", user.Email, secret), RecoveryCodes: codes}, nil
}
```

(`func hasSecret(user User) bool { return len(user.TOTPSecretEncrypted) > 0 }` fonksiyonunu dosyadan tamamen sil — artık hiçbir çağıranı kalmıyor.)

- [x] **Adım 3: Build'i doğrula (test dosyaları henüz güncellenmedi, beklenen derleme hataları)**

Çalıştır: `cd /Users/gokaybaz/Documents/ChatGPT/bazusop && go build ./internal/identity/... 2>&1 | head -20`
Beklenen: `BUILD_OK` (üretim kodu derlenir; `go vet ./...`/`go test ./...` bu noktada `internal/identity`, `internal/server` test paketlerinde başarısız olur — Adım 4 ve Görev 2 düzeltir)

- [x] **Adım 4: `internal/identity/identity_test.go`'daki dört çağrı sitesini güncelle**

`TestInviteLifecycleCreateConsumeConfirmTOTP`'daki satırı değiştir (51. satır civarı):

```go
	user, consumeEnrollment, err := service.ConsumeInvite(t.Context(), token, "a brand new password")
	if err != nil {
		t.Fatalf("consume invite: %v", err)
	}
	if user.Email != "new-admin@example.com" || user.TOTPConfirmedAt != nil {
		t.Fatalf("expected an unconfirmed new user, got %#v", user)
	}
	if consumeEnrollment.ProvisioningURI == "" {
		t.Fatal("expected ConsumeInvite to return a non-empty provisioning URI")
	}

	if _, _, err := service.ConsumeInvite(t.Context(), token, "trying again"); err == nil {
		t.Fatal("expected a second consumption of the same token to fail")
	}

	now := time.Now().UTC()
	enrollment, err := service.ConfirmTOTP(t.Context(), user.ID, func(secret []byte) string { return identity.GenerateTOTPCode(secret, now) }, now)
	if err != nil {
		t.Fatalf("confirm TOTP: %v", err)
	}
	if len(enrollment.RecoveryCodes) != 10 || enrollment.ProvisioningURI == "" {
		t.Fatalf("expected 10 recovery codes and a non-empty provisioning URI, got %#v", enrollment)
	}
```

(Yalnızca `user, err :=` → `user, consumeEnrollment, err :=`, ardındaki `if _, err := ...` → `if _, _, err := ...`, ve `enrollment.ProvisioningURI == ""` kontrolleri eklendi; geri kalan kod aynı.)

`TestConsumeInviteWithSiteRoleGrantsInvokesTheGrantor`'daki satırı değiştir:

```go
	user, _, err := service.ConsumeInvite(t.Context(), token, "a brand new password")
```

`TestConsumeInviteRejectsAnOIDCTypeInvite`'daki satırı değiştir:

```go
	if _, _, err := service.ConsumeInvite(t.Context(), token, "a password"); !errors.Is(err, identity.ErrInviteWrongIdentityType) {
```

- [x] **Adım 5: Gerçek bir istemcinin yalnız `provisioning_uri`'yi kullanarak onboarding'i tamamlayabildiğini kanıtlayan yeni bir test ekle**

`internal/identity/identity_test.go`'nun sonuna ekle:

```go
func TestConsumeInviteReturnsAProvisioningURIThatAnIndependentClientCanUse(t *testing.T) {
	t.Parallel()
	service := newTestService()
	if _, _, err := service.Bootstrap(t.Context(), "org_default", "admin@example.com", "correct horse battery staple"); err != nil {
		t.Fatal(err)
	}
	_, token, err := service.CreateInvite(t.Context(), "admin", "org_default", "new-user@example.com", identity.RolePlatformAdmin, nil, identity.IdentityTypeLocal)
	if err != nil {
		t.Fatalf("create invite: %v", err)
	}

	user, enrollment, err := service.ConsumeInvite(t.Context(), token, "a brand new password")
	if err != nil {
		t.Fatalf("consume invite: %v", err)
	}

	// Simulate a real authenticator app: parse the secret out of the
	// provisioning URI exactly as a QR scanner would, with no access to
	// any Go-internal state — this is the actual bug being fixed.
	parsed, err := url.Parse(enrollment.ProvisioningURI)
	if err != nil {
		t.Fatalf("parse provisioning URI: %v", err)
	}
	secret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(parsed.Query().Get("secret"))
	if err != nil {
		t.Fatalf("decode provisioning URI secret: %v", err)
	}

	now := time.Now().UTC()
	independentlyComputedCode := identity.GenerateTOTPCode(secret, now)
	if _, err := service.ConfirmTOTP(t.Context(), user.ID, func([]byte) string { return independentlyComputedCode }, now); err != nil {
		t.Fatalf("expected a code computed only from the provisioning URI's secret to be accepted, got %v", err)
	}
}
```

Dosyanın import bloğuna `"encoding/base32"` ve `"net/url"` ekle (mevcut `"context"`, `"errors"`, `"testing"`, `"time"`, `"github.com/gokayybaz/bazusop/internal/identity"` importlarının yanına, alfabetik sırayla).

- [x] **Adım 6: Testleri çalıştır**

Çalıştır: `cd /Users/gokaybaz/Documents/ChatGPT/bazusop && go vet ./internal/identity/... && gofmt -l internal/identity/*.go && GOCACHE=/tmp/bazusop-go-cache go test ./internal/identity/... -v 2>&1 | tail -80`
Beklenen: vet/gofmt çıktısı yok; tüm testler BAŞARILI

- [x] **Adım 7: Commit**

```bash
cd /Users/gokaybaz/Documents/ChatGPT/bazusop
git add internal/identity/identity.go internal/identity/identity_test.go
git commit -m "fix: ConsumeInvite now generates and returns the TOTP provisioning URI"
```

## Görev 2: Backend — HTTP katmanı ve çağrı sitesi güncellemeleri

**Dosyalar:**
- Değiştir: `internal/server/identity.go`
- Değiştir: `internal/server/identity_test.go`
- Değiştir: `internal/server/rbac_test.go`, `internal/server/rbac_operational_test.go`

**Arayüzler:**
- Değiştirir: `POST /api/v1/invites/{token}/consume` yanıtı artık `provisioning_uri` alanı içerir; `POST /api/v1/users/{userID}/confirm-totp` yanıtı da artık `provisioning_uri` içerir (istemci QR'ı ister consume ister confirm adımında gösterebilir).

- [x] **Adım 1: `internal/server/identity.go`'daki `handleConsumeInvite`'ı güncelle**

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
		user, enrollment, err := service.ConsumeInvite(request.Context(), request.PathValue("token"), body.Password)
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
			ID              string `json:"id"`
			ProvisioningURI string `json:"provisioning_uri"`
		}{user.ID, enrollment.ProvisioningURI})
	}
}
```

- [x] **Adım 2: `handleConfirmTOTP`'un yanıtına `provisioning_uri` ekle**

```go
		writeJSON(response, http.StatusOK, struct {
			ProvisioningURI string   `json:"provisioning_uri"`
			RecoveryCodes   []string `json:"recovery_codes"`
		}{enrollment.ProvisioningURI, enrollment.RecoveryCodes})
```

(Yalnız bu `writeJSON` çağrısı değişir; fonksiyonun geri kalanı — rate limit kontrolü, hata dalları — aynı kalır.)

- [x] **Adım 3: Build'i doğrula**

Çalıştır: `cd /Users/gokaybaz/Documents/ChatGPT/bazusop && go build ./internal/server/... 2>&1 | head -20`
Beklenen: BAŞARILI

- [x] **Adım 4: `internal/server/rbac_operational_test.go` ve `rbac_test.go`'daki çağrı sitelerini güncelle**

`rbac_operational_test.go`'daki `inviteConsumeAndAssign`'de:

```go
	user, _, err := identityService.ConsumeInvite(t.Context(), token, "a brand new password")
```

`rbac_test.go`'daki `TestRequirePermissionDeniesASessionLackingThePermission`'da:

```go
	viewer, _, err := identityService.ConsumeInvite(t.Context(), token, "a brand new password")
```

- [x] **Adım 5: Testleri çalıştır**

Çalıştır: `cd /Users/gokaybaz/Documents/ChatGPT/bazusop && GOCACHE=/tmp/bazusop-go-cache go test ./internal/server/... -run 'TestSiteAdmin|TestRequirePermission' -v 2>&1 | tail -30`
Beklenen: BAŞARILI

- [x] **Adım 6: `internal/server/identity_test.go`'ya, gerçek bir istemcinin yalnız `provisioning_uri`'yi kullanarak onboarding'i HTTP seviyesinde tamamlayıp giriş yapabildiğini kanıtlayan bir test ekle**

Dosyanın sonuna, `secretFromProvisioningURI` yardımcı fonksiyonuyla birlikte ekle:

```go
func secretFromProvisioningURI(t *testing.T, provisioningURI string) []byte {
	t.Helper()
	parsed, err := url.Parse(provisioningURI)
	if err != nil {
		t.Fatalf("parse provisioning URI: %v", err)
	}
	secret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(parsed.Query().Get("secret"))
	if err != nil {
		t.Fatalf("decode provisioning URI secret: %v", err)
	}
	return secret
}

func TestInviteConsumeReturnsAProvisioningURIARealClientCanUseToConfirmAndLogIn(t *testing.T) {
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
	if consumeResponse.Code != http.StatusCreated {
		t.Fatalf("expected 201 from invite consumption, got %d: %s", consumeResponse.Code, consumeResponse.Body.String())
	}
	var consumedUser struct {
		ID              string `json:"id"`
		ProvisioningURI string `json:"provisioning_uri"`
	}
	if err := json.NewDecoder(consumeResponse.Body).Decode(&consumedUser); err != nil || consumedUser.ID == "" || consumedUser.ProvisioningURI == "" {
		t.Fatalf("expected a user id and a non-empty provisioning URI, got %#v, %v", consumedUser, err)
	}

	secret := secretFromProvisioningURI(t, consumedUser.ProvisioningURI)
	code := identity.GenerateTOTPCode(secret, time.Now().UTC())

	confirmResponse := httptest.NewRecorder()
	handler.ServeHTTP(confirmResponse, httptest.NewRequest(http.MethodPost, "/api/v1/users/"+consumedUser.ID+"/confirm-totp", encodeJSON(t, map[string]string{"code": code})))
	if confirmResponse.Code != http.StatusOK {
		t.Fatalf("expected 200 confirming TOTP with a code derived only from the provisioning URI, got %d: %s", confirmResponse.Code, confirmResponse.Body.String())
	}
	var confirmPayload struct {
		ProvisioningURI string   `json:"provisioning_uri"`
		RecoveryCodes   []string `json:"recovery_codes"`
	}
	if err := json.NewDecoder(confirmResponse.Body).Decode(&confirmPayload); err != nil || len(confirmPayload.RecoveryCodes) != 10 || confirmPayload.ProvisioningURI == "" {
		t.Fatalf("expected 10 recovery codes and a non-empty provisioning URI, got %#v, %v", confirmPayload, err)
	}

	loginResponse := httptest.NewRecorder()
	handler.ServeHTTP(loginResponse, httptest.NewRequest(http.MethodPost, "/api/v1/sessions", encodeJSON(t, map[string]string{
		"email": "new-user@example.com", "password": "a brand new password", "totp_code": identity.GenerateTOTPCode(secret, time.Now().UTC()),
	})))
	if loginResponse.Code != http.StatusCreated {
		t.Fatalf("expected the newly onboarded user to log in with their own authenticator app, got %d: %s", loginResponse.Code, loginResponse.Body.String())
	}
}
```

Dosyanın import bloğuna `"encoding/base32"`, `"net/url"` ve `"time"` ekle (mevcut `"encoding/json"`, `"net/http"`, `"net/http/httptest"`, `"strings"`, `"testing"` importlarının yanına, alfabetik sırayla).

- [x] **Adım 7: Testleri çalıştır**

Çalıştır: `cd /Users/gokaybaz/Documents/ChatGPT/bazusop && GOCACHE=/tmp/bazusop-go-cache go test ./internal/server/... -run 'TestInviteConsumeReturnsAProvisioningURI' -v 2>&1 | tail -30`
Beklenen: BAŞARILI

- [x] **Adım 8: Tam paket testlerini çalıştır**

Çalıştır: `cd /Users/gokaybaz/Documents/ChatGPT/bazusop && go vet ./internal/server/... && gofmt -l internal/server/*.go && GOCACHE=/tmp/bazusop-go-cache go test ./internal/server/... 2>&1 | tail -10`
Beklenen: vet/gofmt çıktısı yok; `ok`

- [x] **Adım 9: Commit**

```bash
cd /Users/gokaybaz/Documents/ChatGPT/bazusop
git add internal/server/identity.go internal/server/identity_test.go internal/server/rbac_test.go internal/server/rbac_operational_test.go
git commit -m "feat: return provisioning_uri from invite consumption and TOTP confirmation"
```

## Görev 3: Frontend — `qrcode` bağımlılığı ve paylaşılan bileşenler

**Dosyalar:**
- Değiştir: `web/package.json` (ve `npm install`'ın ürettiği `package-lock.json`)
- Oluştur: `web/src/components/totp-qr-code.tsx`, `web/src/components/totp-qr-code.test.tsx`
- Oluştur: `web/src/components/recovery-codes.tsx`, `web/src/components/recovery-codes.test.tsx`

**Arayüzler:**
- Üretir: `TotpQrCode({ provisioningUri: string })`, `RecoveryCodes({ codes: string[] })` — Görev 4 ve 5'te `BootstrapPage`/`InvitePage` tarafından tüketilir.

- [x] **Adım 1: `qrcode` ve tip tanımlarını kur**

Çalıştır: `cd /Users/gokaybaz/Documents/ChatGPT/bazusop/web && npm install qrcode@^1.5.4 && npm install --save-dev @types/qrcode@^1.5.6 2>&1 | tail -20`
Beklenen: `package.json`'a `"qrcode": "^1.5.4"` (dependencies) ve `"@types/qrcode": "^1.5.6"` (devDependencies) eklenir.

- [x] **Adım 2: `web/src/components/totp-qr-code.tsx`'i yaz**

```tsx
import { useEffect, useState } from "react"
import QRCode from "qrcode"

export function TotpQrCode({ provisioningUri }: { provisioningUri: string }) {
  const [svg, setSvg] = useState("")
  const [failed, setFailed] = useState(false)

  useEffect(() => {
    let cancelled = false
    setSvg("")
    setFailed(false)
    QRCode.toString(provisioningUri, { type: "svg", margin: 1, width: 200 })
      .then((markup) => {
        if (!cancelled) setSvg(markup)
      })
      .catch(() => {
        if (!cancelled) setFailed(true)
      })
    return () => {
      cancelled = true
    }
  }, [provisioningUri])

  if (failed) {
    return (
      <p className="auth-error">
        QR kod oluşturulamadı. Doğrulayıcı uygulamanıza şu adresi elle ekleyin: {provisioningUri}
      </p>
    )
  }
  if (!svg) {
    return (
      <div className="totp-qr" role="status">
        QR kod oluşturuluyor…
      </div>
    )
  }
  // eslint-disable-next-line react/no-danger -- qrcode's own SVG string, not user input
  return <div aria-label="Doğrulayıcı uygulaması QR kodu" className="totp-qr" dangerouslySetInnerHTML={{ __html: svg }} role="img" />
}
```

- [x] **Adım 3: `web/src/components/totp-qr-code.test.tsx`'i yaz**

```tsx
import { render, screen, waitFor } from "@testing-library/react"
import { describe, expect, it } from "vitest"

import { TotpQrCode } from "./totp-qr-code"

describe("TotpQrCode", () => {
  it("renders a scannable SVG QR code for the provisioning URI", async () => {
    render(<TotpQrCode provisioningUri="otpauth://totp/bazUSOP:admin@example.com?secret=JBSWY3DPEHPK3PXP&issuer=bazUSOP&digits=6&period=30" />)

    const image = await screen.findByRole("img", { name: "Doğrulayıcı uygulaması QR kodu" })
    expect(image.querySelector("svg")).toBeInTheDocument()
  })
})
```

- [x] **Adım 4: `web/src/components/recovery-codes.tsx`'i yaz**

```tsx
export function RecoveryCodes({ codes }: { codes: string[] }) {
  function copyAll() {
    void navigator.clipboard?.writeText(codes.join("\n"))
  }

  return (
    <div className="recovery-codes">
      <p className="auth-warning">Bu kodlar yalnız bir kez gösterilir. Güvenli bir yere kaydedin.</p>
      <ul aria-label="Kurtarma kodları">
        {codes.map((code) => (
          <li key={code}>{code}</li>
        ))}
      </ul>
      <button onClick={copyAll} type="button">
        Tümünü kopyala
      </button>
    </div>
  )
}
```

- [x] **Adım 5: `web/src/components/recovery-codes.test.tsx`'i yaz**

```tsx
import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"

import { RecoveryCodes } from "./recovery-codes"

describe("RecoveryCodes", () => {
  it("renders every code and a one-time-display warning", () => {
    render(<RecoveryCodes codes={["aaaa1111", "bbbb2222", "cccc3333"]} />)

    const list = screen.getByRole("list", { name: "Kurtarma kodları" })
    expect(list.children).toHaveLength(3)
    expect(screen.getByText("aaaa1111")).toBeInTheDocument()
    expect(screen.getByText("bbbb2222")).toBeInTheDocument()
    expect(screen.getByText("cccc3333")).toBeInTheDocument()
    expect(screen.getByText(/yalnız bir kez gösterilir/)).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Tümünü kopyala" })).toBeInTheDocument()
  })
})
```

- [x] **Adım 6: Testleri çalıştır**

Çalıştır: `cd /Users/gokaybaz/Documents/ChatGPT/bazusop/web && npx vitest run totp-qr-code recovery-codes 2>&1 | tail -40`
Beklenen: 2 test dosyası, hepsi BAŞARILI

- [x] **Adım 7: Tip kontrolü**

Çalıştır: `cd /Users/gokaybaz/Documents/ChatGPT/bazusop/web && npx tsc -b --noEmit 2>&1 | tail -40`
Beklenen: hata yok

- [x] **Adım 8: Commit**

```bash
cd /Users/gokaybaz/Documents/ChatGPT/bazusop
git add web/package.json web/package-lock.json web/src/components/totp-qr-code.tsx web/src/components/totp-qr-code.test.tsx web/src/components/recovery-codes.tsx web/src/components/recovery-codes.test.tsx
git commit -m "feat: add TotpQrCode and RecoveryCodes shared onboarding components"
```

## Görev 4: Frontend — `BootstrapPage` (`/setup`)

**Dosyalar:**
- Oluştur: `web/src/pages/bootstrap.tsx`, `web/src/pages/bootstrap.test.tsx`
- Değiştir: `web/src/app.tsx`, `web/src/pages/login.tsx`, `web/src/styles.css`

**Arayüzler:**
- Tüketir: `TotpQrCode`, `RecoveryCodes` (`../components/...`).
- Üretir: `BootstrapPage` — Görev 4'ün `app.tsx` route'una eklenir.

- [x] **Adım 1: `web/src/pages/bootstrap.tsx`'i yaz**

```tsx
import { type FormEvent, useState } from "react"
import { Link, useNavigate } from "react-router-dom"

import { RecoveryCodes } from "../components/recovery-codes"
import { TotpQrCode } from "../components/totp-qr-code"

type BootstrapResult = { provisioningUri: string; recoveryCodes: string[] }

export function BootstrapPage() {
  const navigate = useNavigate()
  const [secret, setSecret] = useState("")
  const [email, setEmail] = useState("")
  const [password, setPassword] = useState("")
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState("")
  const [alreadyBootstrapped, setAlreadyBootstrapped] = useState(false)
  const [result, setResult] = useState<BootstrapResult | null>(null)

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setSubmitting(true)
    setError("")
    setAlreadyBootstrapped(false)
    fetch("/api/v1/bootstrap", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ secret, email, password }),
    })
      .then(async (response) => {
        if (response.status === 409) {
          setAlreadyBootstrapped(true)
          return
        }
        if (!response.ok) {
          setError("Kurulum secret'ı, e-posta veya parola hatalı.")
          return
        }
        const payload = (await response.json()) as { provisioning_uri: string; recovery_codes: string[] }
        setResult({ provisioningUri: payload.provisioning_uri, recoveryCodes: payload.recovery_codes })
      })
      .catch(() => setError("Hub'a ulaşılamıyor, bağlantınızı kontrol edin."))
      .finally(() => setSubmitting(false))
  }

  if (alreadyBootstrapped) {
    return (
      <div className="auth-shell">
        <div className="auth-card">
          <h1>Hub zaten kurulu</h1>
          <p>Bu hub daha önce kuruldu. Devam etmek için giriş yapın.</p>
          <Link className="auth-toggle" to="/login">
            Girişe git
          </Link>
        </div>
      </div>
    )
  }

  if (result) {
    return (
      <div className="auth-shell">
        <div className="auth-card auth-card-wide">
          <h1>Kurulum tamamlandı</h1>
          <p>Doğrulayıcı uygulamanızla aşağıdaki QR kodu okutun.</p>
          <TotpQrCode provisioningUri={result.provisioningUri} />
          <RecoveryCodes codes={result.recoveryCodes} />
          <button onClick={() => navigate("/login")} type="button">
            Girişe devam et
          </button>
        </div>
      </div>
    )
  }

  return (
    <div className="auth-shell">
      <form aria-label="İlk kurulum" className="auth-card" onSubmit={handleSubmit}>
        <div className="brand">
          <div aria-hidden="true" className="brand-mark">U</div>
          <div>
            <strong>bazUSOP</strong>
            <span>kontrol düzlemi</span>
          </div>
        </div>
        <h1>İlk kurulum</h1>
        <label>
          <span>Kurulum secret'ı</span>
          <input autoComplete="off" onChange={(event) => setSecret(event.target.value)} required type="password" value={secret} />
        </label>
        <label>
          <span>Yönetici e-postası</span>
          <input autoComplete="username" onChange={(event) => setEmail(event.target.value)} required type="email" value={email} />
        </label>
        <label>
          <span>Parola</span>
          <input autoComplete="new-password" onChange={(event) => setPassword(event.target.value)} required type="password" value={password} />
        </label>
        {error ? (
          <p aria-live="polite" className="auth-error">
            {error}
          </p>
        ) : null}
        <button disabled={submitting} type="submit">
          {submitting ? "Kuruluyor…" : "Kurulumu tamamla"}
        </button>
        <Link className="auth-setup-link" to="/login">
          Zaten kuruluysa giriş yap
        </Link>
      </form>
    </div>
  )
}
```

- [x] **Adım 2: `web/src/app.tsx`'e `/setup` route'unu ekle**

```tsx
import { BrowserRouter, Route, Routes } from "react-router-dom"

import { AppShell } from "./app-shell"
import { ProtectedRoute } from "./lib/protected-route"
import { SessionProvider } from "./lib/session"
import { BootstrapPage } from "./pages/bootstrap"
import { LoginPage } from "./pages/login"

export function App() {
  return (
    <BrowserRouter>
      <SessionProvider>
        <Routes>
          <Route element={<LoginPage />} path="/login" />
          <Route element={<BootstrapPage />} path="/setup" />
          <Route
            element={
              <ProtectedRoute>
                <AppShell />
              </ProtectedRoute>
            }
            path="/*"
          />
        </Routes>
      </SessionProvider>
    </BrowserRouter>
  )
}
```

- [x] **Adım 3: `web/src/pages/login.tsx`'e "İlk kurulum" bağlantısını ekle**

`Navigate, useNavigate` import satırını `Link, Navigate, useNavigate` yap. Formun sonuna, `submit` butonundan hemen sonra ekle:

```tsx
        <button disabled={submitting} type="submit">
          {submitting ? "Giriş yapılıyor…" : "Giriş yap"}
        </button>
        <Link className="auth-setup-link" to="/setup">
          İlk kurulum
        </Link>
      </form>
    </div>
  )
}
```

(Yalnız `<Link className="auth-setup-link" to="/setup">İlk kurulum</Link>` satırı eklendi.)

- [x] **Adım 4: `web/src/styles.css`'e yeni stiller ekle**

Dosyanın sonuna ekle:

```css
.auth-card-wide { max-width: 420px; }
.totp-qr { display: grid; place-items: center; padding: 16px; border-radius: 12px; background: #ffffff; }
.totp-qr svg { width: 180px; height: 180px; }
.recovery-codes { display: grid; gap: 10px; }
.recovery-codes ul { margin: 0; padding: 0; list-style: none; display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 8px; }
.recovery-codes li { padding: 8px 10px; border-radius: 8px; background: var(--card); color: var(--strong); font: 600 13px ui-monospace, SFMono-Regular, Menlo, monospace; text-align: center; }
.recovery-codes button { min-height: 38px; border: 1px solid var(--border); border-radius: 8px; background: var(--surface); color: var(--strong); cursor: pointer; }
.auth-warning { margin: 0; padding: 10px 12px; border-radius: 8px; background: var(--warning-soft); color: var(--warning); font-size: 12px; }
.auth-setup-link { display: block; margin-top: 4px; color: var(--subtle); font-size: 11px; text-align: center; text-decoration: none; }
```

- [x] **Adım 5: `web/src/pages/bootstrap.test.tsx`'i yaz**

```tsx
import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { MemoryRouter } from "react-router-dom"

import { BootstrapPage } from "./bootstrap"

describe("BootstrapPage", () => {
  afterEach(() => vi.unstubAllGlobals())

  it("completes setup and shows the QR code and recovery codes", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue({
      ok: true,
      status: 201,
      json: async () => ({
        provisioning_uri: "otpauth://totp/bazUSOP:admin@example.com?secret=JBSWY3DPEHPK3PXP&issuer=bazUSOP&digits=6&period=30",
        recovery_codes: ["aaaa1111", "bbbb2222"],
      }),
    } as Response))

    render(
      <MemoryRouter initialEntries={["/setup"]}>
        <BootstrapPage />
      </MemoryRouter>,
    )

    fireEvent.change(screen.getByLabelText("Kurulum secret'ı"), { target: { value: "verify-bootstrap" } })
    fireEvent.change(screen.getByLabelText("Yönetici e-postası"), { target: { value: "admin@example.com" } })
    fireEvent.change(screen.getByLabelText("Parola"), { target: { value: "correct horse battery staple" } })
    fireEvent.click(screen.getByRole("button", { name: "Kurulumu tamamla" }))

    expect(await screen.findByRole("heading", { name: "Kurulum tamamlandı" })).toBeInTheDocument()
    await waitFor(() => expect(screen.getByRole("img", { name: "Doğrulayıcı uygulaması QR kodu" }).querySelector("svg")).toBeInTheDocument())
    expect(screen.getByText("aaaa1111")).toBeInTheDocument()
  })

  it("shows an already-bootstrapped message with a link to login on 409", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue({ ok: false, status: 409 } as Response))

    render(
      <MemoryRouter initialEntries={["/setup"]}>
        <BootstrapPage />
      </MemoryRouter>,
    )

    fireEvent.change(screen.getByLabelText("Kurulum secret'ı"), { target: { value: "wrong" } })
    fireEvent.change(screen.getByLabelText("Yönetici e-postası"), { target: { value: "admin@example.com" } })
    fireEvent.change(screen.getByLabelText("Parola"), { target: { value: "correct horse battery staple" } })
    fireEvent.click(screen.getByRole("button", { name: "Kurulumu tamamla" }))

    expect(await screen.findByRole("heading", { name: "Hub zaten kurulu" })).toBeInTheDocument()
    expect(screen.getByRole("link", { name: "Girişe git" })).toHaveAttribute("href", "/login")
  })
})
```

- [x] **Adım 6: Testleri çalıştır**

Çalıştır: `cd /Users/gokaybaz/Documents/ChatGPT/bazusop/web && npx vitest run bootstrap.test 2>&1 | tail -50`
Beklenen: 2 test BAŞARILI

- [x] **Adım 7: `app.test.tsx`'in hâlâ geçtiğini doğrula (yeni route eklendi, mevcut davranış değişmemeli)**

Çalıştır: `cd /Users/gokaybaz/Documents/ChatGPT/bazusop/web && npm test 2>&1 | tail -30`
Beklenen: tüm testler BAŞARILI

- [x] **Adım 8: Tip kontrolü ve production build**

Çalıştır: `cd /Users/gokaybaz/Documents/ChatGPT/bazusop/web && npm run build 2>&1 | tail -40`
Beklenen: BAŞARILI

- [x] **Adım 9: Commit**

```bash
cd /Users/gokaybaz/Documents/ChatGPT/bazusop
git add web/src/pages/bootstrap.tsx web/src/pages/bootstrap.test.tsx web/src/app.tsx web/src/pages/login.tsx web/src/styles.css
git commit -m "feat: add the /setup bootstrap page with QR and recovery code display"
```

## Görev 5: Frontend — `InvitePage` (`/invite/:token`)

**Dosyalar:**
- Oluştur: `web/src/pages/invite.tsx`, `web/src/pages/invite.test.tsx`
- Değiştir: `web/src/app.tsx`

**Arayüzler:**
- Tüketir: `TotpQrCode`, `RecoveryCodes` (`../components/...`), `useParams` (`react-router-dom`).

- [x] **Adım 1: `web/src/pages/invite.tsx`'i yaz**

```tsx
import { type FormEvent, useState } from "react"
import { Link, useNavigate, useParams } from "react-router-dom"

import { RecoveryCodes } from "../components/recovery-codes"
import { TotpQrCode } from "../components/totp-qr-code"

type Step =
  | { name: "password" }
  | { name: "totp"; userId: string; provisioningUri: string }
  | { name: "done"; recoveryCodes: string[] }

export function InvitePage() {
  const { token = "" } = useParams<{ token: string }>()
  const navigate = useNavigate()
  const [step, setStep] = useState<Step>({ name: "password" })
  const [password, setPassword] = useState("")
  const [code, setCode] = useState("")
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState("")

  function handleConsume(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setSubmitting(true)
    setError("")
    fetch(`/api/v1/invites/${encodeURIComponent(token)}/consume`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ password }),
    })
      .then(async (response) => {
        if (response.status === 404) {
          setError("Bu davet bulunamadı.")
          return
        }
        if (response.status === 409) {
          setError("Bu davetin süresi dolmuş veya zaten kullanılmış.")
          return
        }
        if (response.status === 403) {
          setError("Bu davet bu giriş yöntemiyle kullanılamaz.")
          return
        }
        if (response.status === 429) {
          setError("Çok fazla deneme yapıldı, birkaç dakika sonra tekrar deneyin.")
          return
        }
        if (!response.ok) {
          setError("Davet kabul edilemedi.")
          return
        }
        const payload = (await response.json()) as { id: string; provisioning_uri: string }
        setStep({ name: "totp", userId: payload.id, provisioningUri: payload.provisioning_uri })
      })
      .catch(() => setError("Hub'a ulaşılamıyor, bağlantınızı kontrol edin."))
      .finally(() => setSubmitting(false))
  }

  function handleConfirm(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (step.name !== "totp") return
    setSubmitting(true)
    setError("")
    fetch(`/api/v1/users/${encodeURIComponent(step.userId)}/confirm-totp`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ code }),
    })
      .then(async (response) => {
        if (response.status === 429) {
          setError("Çok fazla deneme yapıldı, birkaç dakika sonra tekrar deneyin.")
          return
        }
        if (!response.ok) {
          setError("Doğrulayıcı kodu hatalı.")
          return
        }
        const payload = (await response.json()) as { recovery_codes: string[] }
        setStep({ name: "done", recoveryCodes: payload.recovery_codes })
      })
      .catch(() => setError("Hub'a ulaşılamıyor, bağlantınızı kontrol edin."))
      .finally(() => setSubmitting(false))
  }

  if (step.name === "done") {
    return (
      <div className="auth-shell">
        <div className="auth-card auth-card-wide">
          <h1>Hesabınız hazır</h1>
          <RecoveryCodes codes={step.recoveryCodes} />
          <button onClick={() => navigate("/login")} type="button">
            Girişe devam et
          </button>
        </div>
      </div>
    )
  }

  if (step.name === "totp") {
    return (
      <div className="auth-shell">
        <form aria-label="Doğrulayıcı kurulumu" className="auth-card auth-card-wide" onSubmit={handleConfirm}>
          <h1>Doğrulayıcı uygulamasını bağla</h1>
          <p>Aşağıdaki QR kodu doğrulayıcı uygulamanızla okutun, sonra üretilen kodu girin.</p>
          <TotpQrCode provisioningUri={step.provisioningUri} />
          <label>
            <span>Doğrulayıcı kodu</span>
            <input autoComplete="one-time-code" inputMode="numeric" maxLength={6} onChange={(event) => setCode(event.target.value)} required value={code} />
          </label>
          {error ? (
            <p aria-live="polite" className="auth-error">
              {error}
            </p>
          ) : null}
          <button disabled={submitting} type="submit">
            {submitting ? "Doğrulanıyor…" : "Kodu doğrula"}
          </button>
        </form>
      </div>
    )
  }

  return (
    <div className="auth-shell">
      <form aria-label="Daveti kabul et" className="auth-card" onSubmit={handleConsume}>
        <div className="brand">
          <div aria-hidden="true" className="brand-mark">U</div>
          <div>
            <strong>bazUSOP</strong>
            <span>kontrol düzlemi</span>
          </div>
        </div>
        <h1>Hesabınızı oluşturun</h1>
        <label>
          <span>Parola</span>
          <input autoComplete="new-password" onChange={(event) => setPassword(event.target.value)} required type="password" value={password} />
        </label>
        {error ? (
          <p aria-live="polite" className="auth-error">
            {error}
          </p>
        ) : null}
        <button disabled={submitting} type="submit">
          {submitting ? "Gönderiliyor…" : "Devam et"}
        </button>
        <Link className="auth-setup-link" to="/login">
          Zaten hesabınız var mı? Giriş yapın
        </Link>
      </form>
    </div>
  )
}
```

- [x] **Adım 2: `web/src/app.tsx`'e `/invite/:token` route'unu ekle**

```tsx
import { BrowserRouter, Route, Routes } from "react-router-dom"

import { AppShell } from "./app-shell"
import { ProtectedRoute } from "./lib/protected-route"
import { SessionProvider } from "./lib/session"
import { BootstrapPage } from "./pages/bootstrap"
import { InvitePage } from "./pages/invite"
import { LoginPage } from "./pages/login"

export function App() {
  return (
    <BrowserRouter>
      <SessionProvider>
        <Routes>
          <Route element={<LoginPage />} path="/login" />
          <Route element={<BootstrapPage />} path="/setup" />
          <Route element={<InvitePage />} path="/invite/:token" />
          <Route
            element={
              <ProtectedRoute>
                <AppShell />
              </ProtectedRoute>
            }
            path="/*"
          />
        </Routes>
      </SessionProvider>
    </BrowserRouter>
  )
}
```

- [x] **Adım 3: `web/src/pages/invite.test.tsx`'i yaz**

```tsx
import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { MemoryRouter, Route, Routes } from "react-router-dom"

import { InvitePage } from "./invite"

function renderAtToken(token: string) {
  return render(
    <MemoryRouter initialEntries={[`/invite/${token}`]}>
      <Routes>
        <Route element={<InvitePage />} path="/invite/:token" />
      </Routes>
    </MemoryRouter>,
  )
}

describe("InvitePage", () => {
  afterEach(() => vi.unstubAllGlobals())

  it("walks through password, TOTP confirmation, and recovery codes", async () => {
    const fetchMock = vi.fn().mockImplementation((input: RequestInfo | URL) => {
      const url = String(input)
      if (url.includes("/consume")) return Promise.resolve({
        ok: true,
        status: 201,
        json: async () => ({ id: "user-1", provisioning_uri: "otpauth://totp/bazUSOP:new-user@example.com?secret=JBSWY3DPEHPK3PXP&issuer=bazUSOP&digits=6&period=30" }),
      } as Response)
      if (url.includes("/confirm-totp")) return Promise.resolve({
        ok: true,
        status: 200,
        json: async () => ({ recovery_codes: ["aaaa1111", "bbbb2222"] }),
      } as Response)
      return Promise.resolve({ ok: false } as Response)
    })
    vi.stubGlobal("fetch", fetchMock)

    renderAtToken("token-abc")

    expect(screen.getByRole("heading", { name: "Hesabınızı oluşturun" })).toBeInTheDocument()
    fireEvent.change(screen.getByLabelText("Parola"), { target: { value: "a brand new password" } })
    fireEvent.click(screen.getByRole("button", { name: "Devam et" }))

    expect(await screen.findByRole("heading", { name: "Doğrulayıcı uygulamasını bağla" })).toBeInTheDocument()
    await waitFor(() => expect(screen.getByRole("img", { name: "Doğrulayıcı uygulaması QR kodu" }).querySelector("svg")).toBeInTheDocument())

    fireEvent.change(screen.getByLabelText("Doğrulayıcı kodu"), { target: { value: "123456" } })
    fireEvent.click(screen.getByRole("button", { name: "Kodu doğrula" }))

    expect(await screen.findByRole("heading", { name: "Hesabınız hazır" })).toBeInTheDocument()
    expect(screen.getByText("aaaa1111")).toBeInTheDocument()

    const consumeCall = fetchMock.mock.calls.find((call) => String(call[0]).includes("/consume"))
    expect(consumeCall?.[0]).toBe("/api/v1/invites/token-abc/consume")
  })

  it("shows an error when the invite token is unknown", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue({ ok: false, status: 404 } as Response))

    renderAtToken("unknown-token")

    fireEvent.change(screen.getByLabelText("Parola"), { target: { value: "whatever password" } })
    fireEvent.click(screen.getByRole("button", { name: "Devam et" }))

    expect(await screen.findByText("Bu davet bulunamadı.")).toBeInTheDocument()
  })
})
```

- [x] **Adım 4: Testleri çalıştır**

Çalıştır: `cd /Users/gokaybaz/Documents/ChatGPT/bazusop/web && npx vitest run invite.test 2>&1 | tail -50`
Beklenen: 2 test BAŞARILI

- [x] **Adım 5: Tam frontend test süitini ve production build'i çalıştır**

Çalıştır: `cd /Users/gokaybaz/Documents/ChatGPT/bazusop/web && npm test 2>&1 | tail -30 && npm run build 2>&1 | tail -30`
Beklenen: tüm testler BAŞARILI, build BAŞARILI

- [x] **Adım 6: Commit**

```bash
cd /Users/gokaybaz/Documents/ChatGPT/bazusop
git add web/src/pages/invite.tsx web/src/pages/invite.test.tsx web/src/app.tsx
git commit -m "feat: add the /invite/:token page for password + TOTP onboarding"
```

## Görev 6: Docker Compose ile uçtan uca manuel doğrulama

**Dosyalar:** yok (yalnız doğrulama).

- [x] **Adım 1: Docker Compose ile hub'ı yeniden derleyip ayağa kaldır**

Çalıştır: `cd /Users/gokaybaz/Documents/ChatGPT/bazusop && docker compose down -v >/dev/null 2>&1; BAZUSOP_PORT=8090 BAZUSOP_BOOTSTRAP_SECRET=verify-bootstrap BAZUSOP_TOTP_ENCRYPTION_KEY=verify-totp-key BAZUSOP_SERVICE_ACCOUNT_PEPPER=verify-pepper docker compose up --build -d 2>&1 | tail -30`

- [x] **Adım 2: Sağlık kontrolünü bekle**

Çalıştır: `for i in $(seq 1 20); do curl -s -o /dev/null -w "%{http_code}" http://127.0.0.1:8090/api/v1/health | grep -q 200 && echo healthy && break; sleep 1; done`

- [x] **Adım 3: Tek bir Python betiğiyle bootstrap → giriş → davet oluştur → davet kabul et → TOTP onayla → giriş yap uçtan uca (tarayıcı istemcisini simüle eder, Go test süitinin dışında bağımsız kanıt)**

```bash
python3 -c "
import base64, hashlib, hmac, http.cookiejar, json, struct, time, urllib.parse, urllib.request

cookie_jar = http.cookiejar.CookieJar()
opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(cookie_jar))

def call(method, path, body=None, extra_headers=None):
    data = json.dumps(body).encode() if body is not None else None
    headers = {'Content-Type': 'application/json'}
    headers.update(extra_headers or {})
    request = urllib.request.Request('http://127.0.0.1:8090' + path, data=data, headers=headers, method=method)
    with opener.open(request) as response:
        return json.load(response)

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

# 1. bootstrap the platform admin
bootstrap_payload = call('POST', '/api/v1/bootstrap', {'secret': 'verify-bootstrap', 'email': 'admin@example.com', 'password': 'correct horse battery staple'})
admin_secret = secret_from_uri(bootstrap_payload['provisioning_uri'])

# 2. log in as the admin
login_payload = call('POST', '/api/v1/sessions', {'email': 'admin@example.com', 'password': 'correct horse battery staple', 'totp_code': totp_code(admin_secret)})
csrf = login_payload['csrf_token']

# 3. create an invite
invite_payload = call('POST', '/api/v1/users/invites', {'email': 'new-user@example.com'}, {'X-CSRF-Token': csrf})
token = invite_payload['token']

# 4. consume the invite as a brand new, unauthenticated client
consume_payload = call('POST', f'/api/v1/invites/{token}/consume', {'password': 'a brand new password'})
user_id = consume_payload['id']
new_user_secret = secret_from_uri(consume_payload['provisioning_uri'])
print('invite consumed, provisioning_uri present:', bool(consume_payload['provisioning_uri']))

# 5. confirm TOTP using ONLY the secret parsed from the provisioning URI
confirm_payload = call('POST', f'/api/v1/users/{user_id}/confirm-totp', {'code': totp_code(new_user_secret)})
print('confirm-totp accepted, recovery codes:', len(confirm_payload['recovery_codes']))

# 6. log in as the newly onboarded user with their own authenticator app
new_login_payload = call('POST', '/api/v1/sessions', {'email': 'new-user@example.com', 'password': 'a brand new password', 'totp_code': totp_code(new_user_secret)})
print('new user logged in, user_id:', new_login_payload['user_id'])
"
```

Beklenen: script hatasız tamamlanır; "invite consumed, provisioning_uri present: True", "confirm-totp accepted, recovery codes: 10", "new user logged in, user_id: ..." satırlarını basar. Bu, tam davet → TOTP → giriş akışının gerçek bir HTTP istemcisiyle (Go testi değil) uçtan uca çalıştığını kanıtlar.

- [x] **Adım 4: Temizlik**

```bash
cd /Users/gokaybaz/Documents/ChatGPT/bazusop
docker compose down -v
```

- [x] **Adım 5: Go ve frontend testlerini son kez birlikte çalıştır**

Çalıştır: `cd /Users/gokaybaz/Documents/ChatGPT/bazusop && go build ./... && go vet ./... && gofmt -l . && GOCACHE=/tmp/bazusop-go-cache go test ./... 2>&1 | tail -30`
Çalıştır: `cd /Users/gokaybaz/Documents/ChatGPT/bazusop/web && npm test 2>&1 | tail -30 && npm run build 2>&1 | tail -20`
Beklenen: hepsi BAŞARILI

## Kendi Kendine İnceleme

**1. Spec kapsaması.** Spike 13.2'nin kabul sinyali ("Sıfırdan bir hub'da tarayıcıdan bootstrap tamamlanır; davetli kullanıcı tarayıcıdan QR'ı okutup TOTP kodunu girerek onboarding'i bitirir") → Görev 6'nın Adım 5'i bunu gerçek bir HTTP istemcisiyle (Go test süitinin dışında, Python'la) uçtan uca kanıtlıyor; Görev 1/2'nin yeni testleri aynısını Go seviyesinde kanıtlıyor. Spec'in "Backend değişiklikleri" bölümündeki ConsumeInvite/ConfirmTOTP imza değişikliği ve `GET /api/v1/users`'a referans YOK (o 13.4'e ait, bu plan kapsamında değil).

**2. Placeholder taraması.** Görev 6'nın ilk taslağında Adım 3-5 arasında yarım kalan, birbirini geçersiz kılan iki ayrı betik denemesi vardı ("Bu adım karmaşık hâle geldi; bunun yerine Adım 5'i kullan" gibi kendi kendine düzeltme notları) — bu, kendi kendine inceleme sırasında fark edilip TEK, eksiksiz, uçtan uca çalışan bir Adım 3 betiğiyle değiştirildi; sonraki adımlar (Temizlik, son test çalıştırması) buna göre yeniden numaralandırıldı.

**3. Tip tutarlılığı.** `TOTPEnrollment{ProvisioningURI, RecoveryCodes}` Görev 1'de tanımlandığı gibi Görev 2'nin HTTP yanıtlarında (`provisioning_uri` JSON alanı) birebir taşınıyor. Frontend'de `{ id: string; provisioning_uri: string }` (consume) ve `{ recovery_codes: string[] }` (confirm — `provisioning_uri` de var ama `InvitePage`/`BootstrapPage` onu confirm adımında kullanmıyor, yalnız consume adımından gelen URI'yi QR'da gösteriyor; bu kasıtlı — aynı URI iki adımda da aynı secret'ı kodluyor, tekrar göstermeye gerek yok) `TotpQrCode`/`RecoveryCodes`'un `provisioningUri`/`codes` prop'larıyla tutarlı.
