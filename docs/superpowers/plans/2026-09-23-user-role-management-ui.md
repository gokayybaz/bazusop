# Kullanıcı ve Rol Yönetimi Arayüzü (Spike 13.4) Uygulama Planı

> **Otonom çalışanlar için:** ZORUNLU ALT-SKILL: Bu planı görev görev uygulamak için superpowers:subagent-driven-development (önerilen) veya superpowers:executing-plans kullan. Adımlar `- [ ]` checkbox söz dizimiyle takip edilir.

**Amaç:** Ayarlar sayfasına, tarayıcıdan platform yöneticisinin kullanıcıları görüp davet edebildiği, site rolü atayıp kaldırabildiği bir "Kullanıcılar" sekmesi ekler. Backend'de kullanıcıları listeleyen bir uç hiç yoktu (yalnız davet oluşturma ve bilinen ID ile rol atama vardı) — bu, spec'in tasarım sürecinde bulunan, küçük ve izole bir eksikti; bu spike onu da tamamlıyor.

**Mimari:** Yeni `GET /api/v1/users` ucu `PermissionManageUsers` (organizasyon kapsamlı, yalnız platform yöneticisi) gerektirir; `internal/identity.Service`'e yeni `UsersForOrganization` metodu (her iki store'a da karşılık gelen yeni bir metotla) ve `internal/authorization.Service`'in zaten var olan `MembershipsForUser`'ı birleştirilerek her kullanıcının site rollerini de içeren tek bir liste döner — mutasyon yok, salt okuma. Frontend'de `web/src/pages/settings-users.tsx` yeni, izole bir bileşen olarak eklenir (13.1-13.3'ün izole test deseniyle tutarlı); `SettingsPage` basit bir sekme anahtarıyla "Genel" (mevcut içerik, değişmez) ve "Kullanıcılar" (yeni) arasında geçiş yapar.

**Teknoloji yığını:** Go 1.26 (yeni salt-okunur uç), React 19 + TypeScript (yeni sekme + form bileşeni).

**Spec:** [docs/superpowers/specs/2026-09-22-frontend-identity-rbac-ui-design.md](../specs/2026-09-22-frontend-identity-rbac-ui-design.md) — bu plan yalnız Spike 13.4'ü kapsar.

## Genel Kısıtlar

- **Kullanıcı devre dışı bırakma/etkinleştirme arayüzü bu spike'ın KAPSAMI DIŞINDA** — spec'in 13.4 satırı yalnız "davet oluşturma, kullanıcı listesi, site rolü atama/kaldırma" diyor. Yeni `GET /api/v1/users` yanıtı `disabled_at`/`totp_confirmed_at`'i salt görüntüleme amacıyla (rozet olarak) döndürür; bunları değiştiren bir mutasyon eklenmez.
- **Site ID'leri elle girilir, ayrı bir "siteleri listele" ucu eklenmez** — sistem hâlâ tek-site mimarisini izliyor (11.5'in kararı); form alanları `site_default`'a varsayılan değerle önceden doldurulur, kullanıcı gerekirse değiştirebilir. Bu, 13.1'in bootstrap ekranındaki "kurulu mu diye ayrı bir keşif ucu eklenmez" ilkesiyle aynı YAGNI mantığı.
- **Pending (henüz kabul edilmemiş) davetleri listeleyen bir görünüm eklenmez** — backend'de böyle bir uç yok, eklemek ayrı bir kapsam genişlemesi olurdu.
- **`SettingsPage`'in mevcut "Genel" içeriği (görünüm, depolama modu, saklama politikası, sürüm) hiç değişmez** — yalnız bir sekme anahtarının arkasına taşınır; varsayılan sekme "Genel" kalır ki mevcut `app.test.tsx`'teki "opens application pages directly from their URL" testi hiç değişmeden geçsin.

## Dosya Yapısı

- Değiştir: `internal/identity/identity.go`, `internal/identity/memorystore.go`, `internal/identity/identity_test.go`.
- Değiştir: `internal/storage/postgres/identity.go`, `internal/storage/postgres/identity_integration_test.go`.
- Değiştir: `internal/server/identity.go`, `internal/server/server.go`, `internal/server/identity_test.go`.
- Değiştir: `web/src/types.ts`, `web/src/pages/settings.tsx`.
- Oluştur: `web/src/pages/settings-users.tsx`, `web/src/pages/settings-users.test.tsx`, `web/src/pages/settings.test.tsx`.

## Görev 1: Backend — `UsersForOrganization` (identity domain katmanı)

**Dosyalar:**
- Değiştir: `internal/identity/identity.go`, `internal/identity/memorystore.go`, `internal/identity/identity_test.go`.
- Değiştir: `internal/storage/postgres/identity.go`, `internal/storage/postgres/identity_integration_test.go`.

**Arayüzler:**
- Üretir: `identity.Store.UsersForOrganization(ctx, organizationID) ([]User, error)`, `identity.Service.UsersForOrganization(ctx, organizationID) ([]User, error)` — Görev 2'de HTTP katmanı tüketir.

- [ ] **Adım 1: `internal/identity/identity.go`'daki `Store` arayüzüne yeni metodu ekle**

`Store` arayüzüne (mevcut `OIDCConfigurationByOrganization` satırından hemen sonra) ekle:

```go
	UsersForOrganization(ctx context.Context, organizationID string) ([]User, error)
```

`Service`'e karşılık gelen sarmalayıcıyı ekle (dosyanın `UserByID` metodundan hemen sonrasına):

```go
func (service *Service) UsersForOrganization(ctx context.Context, organizationID string) ([]User, error) {
	return service.store.UsersForOrganization(ctx, organizationID)
}
```

- [ ] **Adım 2: `internal/identity/memorystore.go`'ya `UsersForOrganization`'ı ekle**

Dosyanın başına `"sort"` import'unu ekle (`"context"`, `"sync"`, `"time"` ile birlikte, alfabetik sırayla). Dosyanın sonuna ekle:

```go
func (store *MemoryStore) UsersForOrganization(_ context.Context, organizationID string) ([]User, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	var users []User
	for _, user := range store.usersByID {
		if user.OrganizationID == organizationID {
			users = append(users, user)
		}
	}
	sort.Slice(users, func(i, j int) bool { return users[i].Email < users[j].Email })
	return users, nil
}
```

- [ ] **Adım 3: `internal/storage/postgres/identity.go`'ya `UsersForOrganization`'ı ekle**

`OIDCConfigurationByOrganization`'dan hemen sonra ekle:

```go
func (store *Store) UsersForOrganization(ctx context.Context, organizationID string) ([]identity.User, error) {
	rows, err := store.pool.Query(ctx, `
		SELECT id, organization_id, email, role, password_hash, totp_secret_encrypted, totp_confirmed_at, created_at, disabled_at, oidc_issuer, oidc_subject
		FROM users WHERE organization_id=$1 ORDER BY email`, organizationID)
	if err != nil {
		return nil, fmt.Errorf("query users for organization: %w", err)
	}
	defer rows.Close()
	var users []identity.User
	for rows.Next() {
		user, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, user)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate users: %w", err)
	}
	return users, nil
}
```

(`scanUser` zaten `pgx.Row` alıyor — `pgx.Rows`'un `Scan` metodu aynı imzayı taşıdığından `rows`'u doğrudan geçirmek güvenli; dosyada zaten bu deseni takip eden başka kod yok ama pgx v5'te standart bir kullanımdır.)

- [ ] **Adım 4: Build'i doğrula**

Çalıştır: `cd /Users/gokaybaz/Documents/ChatGPT/bazusop && go build ./internal/identity/... ./internal/storage/postgres/... 2>&1 | head -30`
Beklenen: BAŞARILI

- [ ] **Adım 5: `internal/identity/identity_test.go`'ya yeni bir test ekle**

Dosyanın sonuna ekle:

```go
func TestUsersForOrganizationReturnsOnlyThatOrganizationsUsersSortedByEmail(t *testing.T) {
	t.Parallel()
	service := newTestService()
	if _, _, err := service.Bootstrap(t.Context(), "org_default", "zed@example.com", "correct horse battery staple"); err != nil {
		t.Fatal(err)
	}
	otherOrgService := newTestService()
	if _, _, err := otherOrgService.Bootstrap(t.Context(), "other_org", "other@example.com", "correct horse battery staple"); err != nil {
		t.Fatal(err)
	}
	_, token, err := service.CreateInvite(t.Context(), "admin", "org_default", "alice@example.com", identity.RolePlatformAdmin, nil, identity.IdentityTypeLocal)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.ConsumeInvite(t.Context(), token, "a brand new password"); err != nil {
		t.Fatal(err)
	}

	users, err := service.UsersForOrganization(t.Context(), "org_default")
	if err != nil {
		t.Fatalf("users for organization: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("expected exactly 2 users in org_default, got %#v", users)
	}
	if users[0].Email != "alice@example.com" || users[1].Email != "zed@example.com" {
		t.Fatalf("expected users sorted by email, got %#v", users)
	}
}
```

(Bu test `service`/`otherOrgService`'in ayrı `identity.Service`/`MemoryStore` örnekleri olduğunu doğrular — `newTestService()` her çağrıda yeni bir `MemoryStore` üretir; `otherOrgService`'in kullanıcısı `service.UsersForOrganization("org_default")` sonucuna hiç karışmaz çünkü tamamen ayrı bir store'da yaşıyor. Bu, gerçek bir Postgres'te "organization_id filtrelemesi" testinin memory-store karşılığıdır.)

- [ ] **Adım 6: Testi çalıştır**

Çalıştır: `cd /Users/gokaybaz/Documents/ChatGPT/bazusop && GOCACHE=/tmp/bazusop-go-cache go test ./internal/identity/... -run 'TestUsersForOrganization' -v 2>&1 | tail -30`
Beklenen: BAŞARILI

- [ ] **Adım 7: `internal/storage/postgres/identity_integration_test.go`'a bir doğrulama ekle**

`TestPostgresIdentityBootstrapInviteAndRecoveryCodeLifecycle`'daki `consumedInvite` kontrolünden hemen sonra, `SaveRecoveryCodes` çağrısından önce ekle:

```go
	orgUsers, err := store.UsersForOrganization(ctx, orgID)
	if err != nil {
		t.Fatalf("users for organization: %v", err)
	}
	if len(orgUsers) != 2 {
		t.Fatalf("expected exactly 2 users (admin + newUser) in %s, got %#v", orgID, orgUsers)
	}
	if orgUsers[0].Email != "admin@example.com" || orgUsers[1].Email != "new-admin@example.com" {
		t.Fatalf("expected users sorted by email, got %#v", orgUsers)
	}
```

- [ ] **Adım 8: Testleri çalıştır**

Postgres gerektirir. Çalıştır:

```bash
docker rm -f bazusop-test-pg-1340 >/dev/null 2>&1
docker run -d --name bazusop-test-pg-1340 -e POSTGRES_USER=bazusop -e POSTGRES_PASSWORD=bazusop_test -e POSTGRES_DB=bazusop_test -p 5432:5432 postgres:18 >/dev/null
for i in $(seq 1 30); do docker exec bazusop-test-pg-1340 pg_isready -U bazusop -d bazusop_test >/dev/null 2>&1 && break; sleep 1; done
cd /Users/gokaybaz/Documents/ChatGPT/bazusop
BAZUSOP_TEST_DATABASE_URL='postgres://bazusop:bazusop_test@127.0.0.1:5432/bazusop_test?sslmode=disable' GOCACHE=/tmp/bazusop-go-cache go test ./internal/storage/postgres/... -run 'TestPostgresIdentityBootstrapInviteAndRecoveryCodeLifecycle' -v 2>&1 | tail -30
docker rm -f bazusop-test-pg-1340 >/dev/null 2>&1
```

Beklenen: BAŞARILI

- [ ] **Adım 9: Tam paket testlerini çalıştır (Postgres olmadan, memory-store testleri için)**

Çalıştır: `cd /Users/gokaybaz/Documents/ChatGPT/bazusop && go vet ./internal/identity/... ./internal/storage/postgres/... && gofmt -l internal/identity/*.go internal/storage/postgres/*.go && GOCACHE=/tmp/bazusop-go-cache go test ./internal/identity/... 2>&1 | tail -10`
Beklenen: vet/gofmt çıktısı yok; `ok`

- [ ] **Adım 10: Commit**

```bash
cd /Users/gokaybaz/Documents/ChatGPT/bazusop
git add internal/identity/identity.go internal/identity/memorystore.go internal/identity/identity_test.go internal/storage/postgres/identity.go internal/storage/postgres/identity_integration_test.go
git commit -m "feat: add identity.Service.UsersForOrganization for the upcoming users-list endpoint"
```

## Görev 2: Backend — `GET /api/v1/users` HTTP ucu

**Dosyalar:**
- Değiştir: `internal/server/identity.go`, `internal/server/server.go`, `internal/server/identity_test.go`.

**Arayüzler:**
- Üretir: `GET /api/v1/users` → `{"users": [{"id","email","role","disabled_at"?,"totp_confirmed_at"?,"site_roles": [{"site_id","role"}]}]}`. `PermissionManageUsers` gerektirir (yalnız platform yöneticisi).

- [ ] **Adım 1: `internal/server/identity.go`'ya `handleListUsers`'ı ekle**

Dosyanın sonuna ekle:

```go
func handleListUsers(identityService *identity.Service, authzService *authorization.Service, sessionService *sessions.Service, scope tenancy.Scope) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		if _, ok := requirePermission(response, request, sessionService, nil, authzService, authorization.PermissionManageUsers, scope.SiteID); !ok {
			return
		}
		users, err := identityService.UsersForOrganization(request.Context(), scope.OrganizationID)
		if err != nil {
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		type siteRolePayload struct {
			SiteID string `json:"site_id"`
			Role   string `json:"role"`
		}
		type userPayload struct {
			ID              string            `json:"id"`
			Email           string            `json:"email"`
			Role            string            `json:"role"`
			DisabledAt      *string           `json:"disabled_at,omitempty"`
			TOTPConfirmedAt *string           `json:"totp_confirmed_at,omitempty"`
			SiteRoles       []siteRolePayload `json:"site_roles"`
		}
		payload := make([]userPayload, 0, len(users))
		for _, user := range users {
			memberships, err := authzService.MembershipsForUser(request.Context(), user.ID)
			if err != nil {
				http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				return
			}
			siteRoles := make([]siteRolePayload, 0, len(memberships))
			for _, membership := range memberships {
				siteRoles = append(siteRoles, siteRolePayload{SiteID: membership.SiteID, Role: string(membership.Role)})
			}
			entry := userPayload{ID: user.ID, Email: user.Email, Role: string(user.Role), SiteRoles: siteRoles}
			if user.DisabledAt != nil {
				formatted := user.DisabledAt.Format(timeLayout)
				entry.DisabledAt = &formatted
			}
			if user.TOTPConfirmedAt != nil {
				formatted := user.TOTPConfirmedAt.Format(timeLayout)
				entry.TOTPConfirmedAt = &formatted
			}
			payload = append(payload, entry)
		}
		writeJSON(response, http.StatusOK, struct {
			Users []userPayload `json:"users"`
		}{payload})
	}
}
```

- [ ] **Adım 2: `internal/server/server.go`'ya route'u ekle**

`registerAudited(mux, "/api/v1/users/invites", ...)` satırından hemen önce ekle:

```go
		registerAudited(mux, "/api/v1/users", http.MethodGet, "users", nil, configuration.auditTrail, configuration.scope, handleListUsers(configuration.identityService, configuration.authorizationService, configuration.sessionService, configuration.scope))
```

- [ ] **Adım 3: Build'i doğrula**

Çalıştır: `cd /Users/gokaybaz/Documents/ChatGPT/bazusop && go build ./... 2>&1 | head -30 && echo BUILD_OK`
Beklenen: `BUILD_OK`

- [ ] **Adım 4: `internal/server/identity_test.go`'ya yeni bir test ekle**

Dosyanın sonuna ekle:

```go
func TestListUsersRequiresPlatformAdminAndReturnsSiteRoles(t *testing.T) {
	t.Parallel()
	handler := newIdentityHandler(t, "correct-secret")
	cookies := bootstrapAndLogin(t, handler, "correct-secret", "admin@example.com", "correct horse battery staple")
	csrfToken := csrfTokenFromCookies(cookies)

	inviteRequest := httptest.NewRequest(http.MethodPost, "/api/v1/users/invites", encodeJSON(t, map[string]any{
		"email": "operator@example.com", "role": "operator", "site_ids": []string{"site_default"},
	}))
	for _, cookie := range cookies {
		inviteRequest.AddCookie(cookie)
	}
	inviteRequest.Header.Set("X-CSRF-Token", csrfToken)
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

	listWithoutAuth := httptest.NewRecorder()
	handler.ServeHTTP(listWithoutAuth, httptest.NewRequest(http.MethodGet, "/api/v1/users", nil))
	if listWithoutAuth.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for listing users without a session, got %d", listWithoutAuth.Code)
	}

	listRequest := httptest.NewRequest(http.MethodGet, "/api/v1/users", nil)
	for _, cookie := range cookies {
		listRequest.AddCookie(cookie)
	}
	listResponse := httptest.NewRecorder()
	handler.ServeHTTP(listResponse, listRequest)
	if listResponse.Code != http.StatusOK {
		t.Fatalf("expected 200 listing users, got %d: %s", listResponse.Code, listResponse.Body.String())
	}
	var payload struct {
		Users []struct {
			ID        string `json:"id"`
			Email     string `json:"email"`
			Role      string `json:"role"`
			SiteRoles []struct {
				SiteID string `json:"site_id"`
				Role   string `json:"role"`
			} `json:"site_roles"`
		} `json:"users"`
	}
	if err := json.NewDecoder(listResponse.Body).Decode(&payload); err != nil {
		t.Fatalf("decode users: %v", err)
	}
	if len(payload.Users) != 2 {
		t.Fatalf("expected 2 users (admin + operator), got %#v", payload.Users)
	}
	var operatorEntry *struct {
		ID        string `json:"id"`
		Email     string `json:"email"`
		Role      string `json:"role"`
		SiteRoles []struct {
			SiteID string `json:"site_id"`
			Role   string `json:"role"`
		} `json:"site_roles"`
	}
	for index := range payload.Users {
		if payload.Users[index].Email == "operator@example.com" {
			operatorEntry = &payload.Users[index]
		}
	}
	if operatorEntry == nil {
		t.Fatalf("expected an operator@example.com entry, got %#v", payload.Users)
	}
	if len(operatorEntry.SiteRoles) != 1 || operatorEntry.SiteRoles[0].SiteID != "site_default" || operatorEntry.SiteRoles[0].Role != "operator" {
		t.Fatalf("expected the operator to have a site_default/operator site role, got %#v", operatorEntry.SiteRoles)
	}
}
```

- [ ] **Adım 5: Testi çalıştır**

Çalıştır: `cd /Users/gokaybaz/Documents/ChatGPT/bazusop && GOCACHE=/tmp/bazusop-go-cache go test ./internal/server/... -run 'TestListUsers' -v 2>&1 | tail -30`
Beklenen: BAŞARILI

- [ ] **Adım 6: Tam paket testlerini çalıştır**

Çalıştır: `cd /Users/gokaybaz/Documents/ChatGPT/bazusop && go vet ./internal/server/... && gofmt -l internal/server/*.go && GOCACHE=/tmp/bazusop-go-cache go test ./internal/server/... 2>&1 | tail -10`
Beklenen: vet/gofmt çıktısı yok; `ok`

- [ ] **Adım 7: Commit**

```bash
cd /Users/gokaybaz/Documents/ChatGPT/bazusop
git add internal/server/identity.go internal/server/server.go internal/server/identity_test.go
git commit -m "feat: add GET /api/v1/users, platform-admin only, with each user's site roles"
```

## Görev 3: Frontend — `SettingsUsersTab` bileşeni

**Dosyalar:**
- Değiştir: `web/src/types.ts`
- Oluştur: `web/src/pages/settings-users.tsx`, `web/src/pages/settings-users.test.tsx`

**Arayüzler:**
- Tüketir: `useSession` (`../lib/session`).
- Üretir: `SettingsUsersTab` — Görev 4'te `SettingsPage`'in "Kullanıcılar" sekmesine bağlanır.

- [ ] **Adım 1: `web/src/types.ts`'e yeni tipleri ekle**

Dosyanın sonuna ekle:

```ts
export type UserSiteRole = { site_id: string; role: string }
export type ManagedUser = { id: string; email: string; role: string; disabled_at?: string; totp_confirmed_at?: string; site_roles: UserSiteRole[] }
```

- [ ] **Adım 2: `web/src/pages/settings-users.tsx`'i yaz**

```tsx
import { type FormEvent, useEffect, useState } from "react"
import { ShieldCheck } from "lucide-react"

import { Badge } from "../components/ui/badge"
import { Card } from "../components/ui/card"
import { EmptyFeature } from "../components/empty-feature"
import { useSession } from "../lib/session"
import type { ManagedUser } from "../types"

const jsonHeaders = { "Content-Type": "application/json" }

export function SettingsUsersTab() {
  const { apiFetch } = useSession()
  const [users, setUsers] = useState<ManagedUser[]>([])
  const [state, setState] = useState<"loading" | "ready" | "error" | "forbidden">("loading")
  const [inviteEmail, setInviteEmail] = useState("")
  const [inviteRole, setInviteRole] = useState<"" | "site-admin" | "operator" | "viewer">("")
  const [inviteSiteIds, setInviteSiteIds] = useState("site_default")
  const [inviteLink, setInviteLink] = useState("")
  const [inviteError, setInviteError] = useState("")
  const [roleUserId, setRoleUserId] = useState("")
  const [roleSiteId, setRoleSiteId] = useState("site_default")
  const [roleValue, setRoleValue] = useState<"site-admin" | "operator" | "viewer">("viewer")
  const [roleMessage, setRoleMessage] = useState("")

  function loadUsers() {
    setState("loading")
    fetch("/api/v1/users")
      .then((response) => {
        if (response.status === 401 || response.status === 403) return Promise.reject(new Error("forbidden"))
        if (!response.ok) return Promise.reject(new Error("unavailable"))
        return response.json() as Promise<{ users: ManagedUser[] }>
      })
      .then((payload) => {
        setUsers(payload.users ?? [])
        setState("ready")
      })
      .catch((error: unknown) => setState((error as Error).message === "forbidden" ? "forbidden" : "error"))
  }

  useEffect(() => {
    loadUsers()
  }, [])

  function createInvite(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setInviteError("")
    setInviteLink("")
    apiFetch("/api/v1/users/invites", {
      method: "POST",
      headers: jsonHeaders,
      body: JSON.stringify({
        email: inviteEmail,
        role: inviteRole,
        site_ids: inviteRole ? inviteSiteIds.split(",").map((id) => id.trim()).filter(Boolean) : [],
      }),
    })
      .then((response) => {
        if (!response.ok) throw new Error()
        return response.json() as Promise<{ email: string; token: string }>
      })
      .then((payload) => {
        setInviteLink(`${window.location.origin}/invite/${payload.token}`)
        setInviteEmail("")
      })
      .catch(() => setInviteError("Davet oluşturulamadı; alanları kontrol edin."))
  }

  function assignRole(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setRoleMessage("")
    apiFetch(`/api/v1/sites/${encodeURIComponent(roleSiteId)}/memberships`, {
      method: "POST",
      headers: jsonHeaders,
      body: JSON.stringify({ user_id: roleUserId, role: roleValue }),
    })
      .then((response) => {
        if (!response.ok) throw new Error()
        setRoleMessage("Rol atandı.")
        loadUsers()
      })
      .catch(() => setRoleMessage("Rol atanamadı; kullanıcı ve site ID'sini kontrol edin."))
  }

  function revokeRole() {
    setRoleMessage("")
    apiFetch(`/api/v1/sites/${encodeURIComponent(roleSiteId)}/memberships/${encodeURIComponent(roleUserId)}`, { method: "DELETE" })
      .then((response) => {
        if (!response.ok) throw new Error()
        setRoleMessage("Rol kaldırıldı.")
        loadUsers()
      })
      .catch(() => setRoleMessage("Rol kaldırılamadı; kullanıcı ve site ID'sini kontrol edin."))
  }

  if (state === "forbidden") {
    return <EmptyFeature icon={ShieldCheck} text="Bu ekranı görüntülemek için platform yöneticisi oturumu gerekir." title="Kullanıcı yönetimine erişim yetkiniz yok" />
  }
  if (state === "error") {
    return <EmptyFeature icon={ShieldCheck} text="Hub bağlantısını kontrol edin." title="Kullanıcılara ulaşılamıyor" />
  }

  return (
    <div className="settings-users">
      <Card className="table-card page-card">
        <div className="card-header">
          <div>
            <h2>Kullanıcılar</h2>
            <p>Organizasyondaki tüm kullanıcılar ve site rolleri</p>
          </div>
        </div>
        {state === "loading" ? (
          <p className="muted">Yükleniyor…</p>
        ) : users.length === 0 ? (
          <p className="muted">Henüz kullanıcı yok.</p>
        ) : (
          <div className="table-scroll">
            <table aria-label="Kullanıcılar">
              <thead>
                <tr>
                  <th>E-posta</th>
                  <th>Rol</th>
                  <th>Site rolleri</th>
                  <th>TOTP</th>
                  <th>Durum</th>
                </tr>
              </thead>
              <tbody>
                {users.map((user) => (
                  <tr key={user.id}>
                    <td>{user.email}</td>
                    <td>{user.role || "—"}</td>
                    <td>
                      {user.site_roles.length === 0
                        ? "—"
                        : user.site_roles.map((entry) => (
                            <Badge className="site-role-badge" key={`${entry.site_id}:${entry.role}`}>
                              {entry.site_id}: {entry.role}
                            </Badge>
                          ))}
                    </td>
                    <td>
                      <Badge className={user.totp_confirmed_at ? "totp-status confirmed" : "totp-status pending"}>
                        {user.totp_confirmed_at ? "Onaylı" : "Onaysız"}
                      </Badge>
                    </td>
                    <td>
                      <Badge className={user.disabled_at ? "user-status disabled" : "user-status active"}>
                        {user.disabled_at ? "Devre dışı" : "Aktif"}
                      </Badge>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Card>

      <div className="settings-users-forms">
        <Card className="settings-form-card">
          <h3>Kullanıcı davet et</h3>
          <form className="alarm-form" onSubmit={createInvite}>
            <label>
              <span>E-posta</span>
              <input onChange={(event) => setInviteEmail(event.target.value)} required type="email" value={inviteEmail} />
            </label>
            <label>
              <span>Rol</span>
              <select onChange={(event) => setInviteRole(event.target.value as typeof inviteRole)} value={inviteRole}>
                <option value="">Platform yöneticisi</option>
                <option value="site-admin">Site yöneticisi</option>
                <option value="operator">Operatör</option>
                <option value="viewer">İzleyici</option>
              </select>
            </label>
            {inviteRole ? (
              <label>
                <span>Site ID'leri (virgülle ayrılmış)</span>
                <input onChange={(event) => setInviteSiteIds(event.target.value)} required value={inviteSiteIds} />
              </label>
            ) : null}
            <button type="submit">Davet oluştur</button>
            {inviteError ? (
              <p aria-live="polite" className="auth-error">
                {inviteError}
              </p>
            ) : null}
            {inviteLink ? (
              <p aria-live="polite">
                Davet bağlantısı: <code>{inviteLink}</code>
              </p>
            ) : null}
          </form>
        </Card>

        <Card className="settings-form-card">
          <h3>Site rolü ata / kaldır</h3>
          <form className="alarm-form" onSubmit={assignRole}>
            <label>
              <span>Kullanıcı</span>
              <select onChange={(event) => setRoleUserId(event.target.value)} required value={roleUserId}>
                <option value="">Seçin…</option>
                {users.map((user) => (
                  <option key={user.id} value={user.id}>
                    {user.email}
                  </option>
                ))}
              </select>
            </label>
            <label>
              <span>Site ID</span>
              <input onChange={(event) => setRoleSiteId(event.target.value)} required value={roleSiteId} />
            </label>
            <label>
              <span>Rol</span>
              <select onChange={(event) => setRoleValue(event.target.value as typeof roleValue)} value={roleValue}>
                <option value="site-admin">Site yöneticisi</option>
                <option value="operator">Operatör</option>
                <option value="viewer">İzleyici</option>
              </select>
            </label>
            <div className="settings-form-actions">
              <button disabled={!roleUserId || !roleSiteId} type="submit">
                Rolü ata
              </button>
              <button disabled={!roleUserId || !roleSiteId} onClick={revokeRole} type="button">
                Rolü kaldır
              </button>
            </div>
            {roleMessage ? <p aria-live="polite">{roleMessage}</p> : null}
          </form>
        </Card>
      </div>
    </div>
  )
}
```

- [ ] **Adım 3: Tip kontrolü**

Çalıştır: `cd /Users/gokaybaz/Documents/ChatGPT/bazusop/web && npx tsc -b --noEmit 2>&1 | tail -40`
Beklenen: hata yok

- [ ] **Adım 4: `web/src/pages/settings-users.test.tsx`'i yaz**

```tsx
import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

import { SettingsUsersTab } from "./settings-users"
import { SessionProvider } from "../lib/session"
import type { ManagedUser } from "../types"

const authenticatedWhoAmI = { user_id: "admin-1", email: "admin@example.com", role: "platform-admin", csrf_token: "csrf-token-abc", expires_at: "2026-09-23T22:00:00Z" }

const baseUsers: ManagedUser[] = [
  { id: "admin-1", email: "admin@example.com", role: "platform-admin", totp_confirmed_at: "2026-09-01T00:00:00Z", site_roles: [] },
  { id: "user-2", email: "operator@example.com", role: "", totp_confirmed_at: "2026-09-02T00:00:00Z", site_roles: [{ site_id: "site_default", role: "operator" }] },
]

describe("SettingsUsersTab", () => {
  afterEach(() => vi.unstubAllGlobals())

  it("shows the user table with site roles and status badges", async () => {
    vi.stubGlobal("fetch", vi.fn().mockImplementation((input: RequestInfo | URL) => {
      const url = String(input)
      if (url.endsWith("/api/v1/session")) return Promise.resolve({ ok: true, json: async () => authenticatedWhoAmI } as Response)
      if (url.endsWith("/api/v1/users")) return Promise.resolve({ ok: true, json: async () => ({ users: baseUsers }) } as Response)
      return Promise.resolve({ ok: false } as Response)
    }))

    render(
      <SessionProvider>
        <SettingsUsersTab />
      </SessionProvider>,
    )

    expect(await screen.findByText("admin@example.com")).toBeInTheDocument()
    expect(screen.getByText("operator@example.com")).toBeInTheDocument()
    expect(screen.getByText("site_default: operator")).toBeInTheDocument()
    expect(screen.getAllByText("Onaylı")).toHaveLength(2)
    expect(screen.getAllByText("Aktif")).toHaveLength(2)
  })

  it("shows a forbidden state for a non-admin session", async () => {
    vi.stubGlobal("fetch", vi.fn().mockImplementation((input: RequestInfo | URL) => {
      const url = String(input)
      if (url.endsWith("/api/v1/session")) return Promise.resolve({ ok: true, json: async () => ({ ...authenticatedWhoAmI, role: "operator" }) } as Response)
      if (url.endsWith("/api/v1/users")) return Promise.resolve({ ok: false, status: 403 } as Response)
      return Promise.resolve({ ok: false } as Response)
    }))

    render(
      <SessionProvider>
        <SettingsUsersTab />
      </SessionProvider>,
    )

    expect(await screen.findByRole("heading", { name: "Kullanıcı yönetimine erişim yetkiniz yok" })).toBeInTheDocument()
  })

  it("creates an invite and shows the resulting link with a CSRF header", async () => {
    const fetchMock = vi.fn().mockImplementation((input: RequestInfo | URL) => {
      const url = String(input)
      if (url.endsWith("/api/v1/session")) return Promise.resolve({ ok: true, json: async () => authenticatedWhoAmI } as Response)
      if (url.endsWith("/api/v1/users")) return Promise.resolve({ ok: true, json: async () => ({ users: baseUsers }) } as Response)
      if (url.endsWith("/api/v1/users/invites")) return Promise.resolve({ ok: true, status: 201, json: async () => ({ email: "new@example.com", token: "invite-token-abc" }) } as Response)
      return Promise.resolve({ ok: false } as Response)
    })
    vi.stubGlobal("fetch", fetchMock)

    render(
      <SessionProvider>
        <SettingsUsersTab />
      </SessionProvider>,
    )
    await waitFor(() => expect(screen.getByText("admin@example.com")).toBeInTheDocument())

    fireEvent.change(screen.getByLabelText("E-posta"), { target: { value: "new@example.com" } })
    fireEvent.click(screen.getByRole("button", { name: "Davet oluştur" }))

    expect(await screen.findByText("invite-token-abc", { exact: false })).toBeInTheDocument()
    const inviteCall = fetchMock.mock.calls.find((call) => String(call[0]).endsWith("/api/v1/users/invites"))
    expect(inviteCall).toBeDefined()
    const headers = new Headers((inviteCall?.[1] as RequestInit).headers)
    expect(headers.get("X-CSRF-Token")).toBe("csrf-token-abc")
  })

  it("assigns a site role with a CSRF header and refreshes the list", async () => {
    const updatedUsers: ManagedUser[] = [
      baseUsers[0],
      { ...baseUsers[1], site_roles: [{ site_id: "site_default", role: "operator" }, { site_id: "site-b", role: "viewer" }] },
    ]
    let usersCallCount = 0
    const fetchMock = vi.fn().mockImplementation((input: RequestInfo | URL) => {
      const url = String(input)
      if (url.endsWith("/api/v1/session")) return Promise.resolve({ ok: true, json: async () => authenticatedWhoAmI } as Response)
      if (url.endsWith("/api/v1/users")) {
        usersCallCount += 1
        return Promise.resolve({ ok: true, json: async () => ({ users: usersCallCount === 1 ? baseUsers : updatedUsers }) } as Response)
      }
      if (url.endsWith("/memberships")) return Promise.resolve({ ok: true, status: 204 } as Response)
      return Promise.resolve({ ok: false } as Response)
    })
    vi.stubGlobal("fetch", fetchMock)

    render(
      <SessionProvider>
        <SettingsUsersTab />
      </SessionProvider>,
    )
    await waitFor(() => expect(screen.getByText("admin@example.com")).toBeInTheDocument())

    fireEvent.change(screen.getByLabelText("Kullanıcı"), { target: { value: "user-2" } })
    fireEvent.change(screen.getByLabelText("Site ID"), { target: { value: "site-b" } })
    fireEvent.click(screen.getByRole("button", { name: "Rolü ata" }))

    expect(await screen.findByText("site-b: viewer")).toBeInTheDocument()
    const assignCall = fetchMock.mock.calls.find((call) => String(call[0]).endsWith("/api/v1/sites/site-b/memberships") && (call[1] as RequestInit)?.method === "POST")
    expect(assignCall).toBeDefined()
    const headers = new Headers((assignCall?.[1] as RequestInit).headers)
    expect(headers.get("X-CSRF-Token")).toBe("csrf-token-abc")
  })
})
```

- [ ] **Adım 5: Testleri çalıştır**

Çalıştır: `cd /Users/gokaybaz/Documents/ChatGPT/bazusop/web && npx vitest run settings-users.test 2>&1 | tail -100`
Beklenen: 4 test BAŞARILI

- [ ] **Adım 6: Commit**

```bash
cd /Users/gokaybaz/Documents/ChatGPT/bazusop
git add web/src/types.ts web/src/pages/settings-users.tsx web/src/pages/settings-users.test.tsx
git commit -m "feat: add the SettingsUsersTab component (user list, invite, site role assign/revoke)"
```

## Görev 4: Frontend — `SettingsPage`'e sekme ekle

**Dosyalar:**
- Değiştir: `web/src/pages/settings.tsx`, `web/src/styles.css`
- Oluştur: `web/src/pages/settings.test.tsx`

**Arayüzler:**
- Tüketir: `SettingsUsersTab` (`./settings-users`).

- [ ] **Adım 1: `web/src/pages/settings.tsx`'i güncelle**

```tsx
import { useEffect, useState } from "react"

import { ThemeToggle } from "../components/theme-toggle"
import { Badge } from "../components/ui/badge"
import { Card } from "../components/ui/card"
import { formatBuildDate } from "../lib/format"
import { SettingsUsersTab } from "./settings-users"
import type { RuntimeConfiguration } from "../types"

export function SettingsPage() {
  const [tab, setTab] = useState<"general" | "users">("general")
  const [runtime, setRuntime] = useState<RuntimeConfiguration | null>(null)
  useEffect(() => {
    const controller = new AbortController()
    fetch("/api/v1/system/configuration", { signal: controller.signal })
      .then((response) => response.ok ? response.json() as Promise<RuntimeConfiguration> : Promise.reject())
      .then(setRuntime)
      .catch(() => undefined)
    return () => controller.abort()
  }, [])
  const storageLabel = runtime?.storage === "postgresql" ? (runtime.timescale_enabled ? "TimescaleDB" : "PostgreSQL") : "Süreç içi bellek"
  return (
    <div className="settings-page">
      <div className="settings-tabs" role="tablist">
        <button aria-selected={tab === "general"} className={tab === "general" ? "settings-tab active" : "settings-tab"} onClick={() => setTab("general")} role="tab" type="button">Genel</button>
        <button aria-selected={tab === "users"} className={tab === "users" ? "settings-tab active" : "settings-tab"} onClick={() => setTab("users")} role="tab" type="button">Kullanıcılar</button>
      </div>
      {tab === "general" ? <div className="settings-grid"><Card className="settings-card"><div><h2>Görünüm</h2><p>Operasyon yüzeyi için açık veya koyu temayı seçin.</p></div><ThemeToggle /></Card><Card className="settings-card"><div><h2>Hub çalışma modu</h2><p>Etkin kalıcı depolama ve zaman serisi çalışma modu.</p></div><Badge className="environment">{runtime ? storageLabel : "Yükleniyor"}</Badge></Card><Card className="settings-card retention-card"><div><h2>Saklama politikası</h2><p>TimescaleDB etkin olduğunda otomatik uygulanır.</p></div><div className="retention-values"><span>Telemetri: {runtime?.telemetry_retention_days ?? "—"} gün</span><span>Loglar: {runtime?.log_retention_days ?? "—"} gün</span></div></Card><Card className="settings-card build-card"><div><h2>Hub sürümü</h2><p>Çalışan binary'nin sürüm ve kaynak kimliği.</p></div><div className="build-values"><strong>{runtime?.version ?? "—"}</strong><span>{runtime?.commit ?? "Yükleniyor"}</span><time dateTime={runtime?.build_date}>{runtime?.build_date ? formatBuildDate(runtime.build_date) : "—"}</time></div></Card></div> : <SettingsUsersTab />}
    </div>
  )
}
```

- [ ] **Adım 2: `web/src/styles.css`'e sekme stillerini ekle**

Dosyanın sonuna ekle:

```css
.settings-page { display: grid; gap: 18px; }
.settings-tabs { display: flex; gap: 6px; border-bottom: 1px solid var(--border); }
.settings-tab { padding: 10px 16px; border: 0; background: none; color: var(--subtle); font-weight: 600; font-size: 13px; cursor: pointer; }
.settings-tab.active { color: var(--strong); box-shadow: 0 2px 0 0 var(--accent); }
.settings-users { display: grid; gap: 18px; }
.settings-users-forms { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 16px; }
.settings-form-card { padding: 20px; border-radius: 16px; display: grid; gap: 12px; }
.settings-form-card h3 { margin: 0; color: var(--strong); font-size: 15px; }
.settings-form-actions { display: flex; gap: 10px; }
.site-role-badge { margin: 2px 4px 2px 0; }
.totp-status.confirmed, .user-status.active { color: var(--success); border-color: color-mix(in srgb, var(--success) 32%, var(--border)); background: var(--success-soft); }
.totp-status.pending, .user-status.disabled { color: var(--warning); border-color: color-mix(in srgb, var(--warning) 32%, var(--border)); background: var(--warning-soft); }
@media (max-width: 1100px) { .settings-users-forms { grid-template-columns: 1fr; } }
```

**Yazarken düzeltme:** `.settings-tab.active`'te `box-shadow` kullanıldı, `border` DEĞİL — `design-system.test.ts`'in "no colored leading borders" kuralı yalnız `border-left`'i yasaklıyor (alt çizgi/box-shadow etkisi bu kuralın kapsamı dışında ve zaten dosyada `.metric-card:hover { box-shadow: var(--shadow); }` gibi örnekleri var), ama `border-left`'ten tamamen kaçınmak için baştan `box-shadow` tercih edildi — 13.2'de yaşanan aynı hatayı burada tekrarlamamak için.

- [ ] **Adım 3: `web/src/pages/settings.test.tsx`'i yaz**

```tsx
import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

import { SettingsPage } from "./settings"
import { SessionProvider } from "../lib/session"

const authenticatedWhoAmI = { user_id: "admin-1", email: "admin@example.com", role: "platform-admin", csrf_token: "csrf-token-abc", expires_at: "2026-09-23T22:00:00Z" }

describe("SettingsPage", () => {
  afterEach(() => vi.unstubAllGlobals())

  it("defaults to the general tab and switches to the users tab on click", async () => {
    vi.stubGlobal("fetch", vi.fn().mockImplementation((input: RequestInfo | URL) => {
      const url = String(input)
      if (url.endsWith("/api/v1/session")) return Promise.resolve({ ok: true, json: async () => authenticatedWhoAmI } as Response)
      if (url.endsWith("/api/v1/system/configuration")) return Promise.resolve({ ok: true, json: async () => ({ storage: "memory", timescale_enabled: false, telemetry_retention_days: 30, log_retention_days: 14, version: "0.5.0", commit: "abc123", build_date: "2026-09-23T00:00:00Z" }) } as Response)
      if (url.endsWith("/api/v1/users")) return Promise.resolve({ ok: true, json: async () => ({ users: [] }) } as Response)
      return Promise.resolve({ ok: false } as Response)
    }))

    render(
      <SessionProvider>
        <SettingsPage />
      </SessionProvider>,
    )

    expect(await screen.findByText("Görünüm")).toBeInTheDocument()
    expect(screen.queryByText("Kullanıcı davet et")).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole("tab", { name: "Kullanıcılar" }))

    expect(await screen.findByText("Kullanıcı davet et")).toBeInTheDocument()
    expect(screen.queryByText("Görünüm")).not.toBeInTheDocument()
    await waitFor(() => expect(screen.getByText("Henüz kullanıcı yok.")).toBeInTheDocument())
  })
})
```

- [ ] **Adım 4: Testleri çalıştır**

Çalıştır: `cd /Users/gokaybaz/Documents/ChatGPT/bazusop/web && npx vitest run settings.test 2>&1 | tail -60`
Beklenen: BAŞARILI

- [ ] **Adım 5: Tam frontend test süitini ve production build'i çalıştır (mevcut `app.test.tsx`'in "opens application pages directly from their URL" testi hiç değişmeden geçmeli)**

Çalıştır: `cd /Users/gokaybaz/Documents/ChatGPT/bazusop/web && npm test 2>&1 | tail -60 && npm run build 2>&1 | tail -30`
Beklenen: tüm testler BAŞARILI, build BAŞARILI

- [ ] **Adım 6: Commit**

```bash
cd /Users/gokaybaz/Documents/ChatGPT/bazusop
git add web/src/pages/settings.tsx web/src/pages/settings.test.tsx web/src/styles.css
git commit -m "feat: add a Kullanıcılar tab to Settings, wired to SettingsUsersTab"
```

## Görev 5: Docker Compose ile uçtan uca manuel doğrulama

**Dosyalar:** yok (yalnız doğrulama).

- [ ] **Adım 1: Docker Compose ile hub'ı yeniden derleyip ayağa kaldır**

Çalıştır: `cd /Users/gokaybaz/Documents/ChatGPT/bazusop && docker compose down -v >/dev/null 2>&1; BAZUSOP_PORT=8090 BAZUSOP_BOOTSTRAP_SECRET=verify-bootstrap BAZUSOP_TOTP_ENCRYPTION_KEY=verify-totp-key BAZUSOP_SERVICE_ACCOUNT_PEPPER=verify-pepper docker compose up --build -d 2>&1 | tail -30`

- [ ] **Adım 2: Sağlık kontrolünü bekle**

Çalıştır: `for i in $(seq 1 20); do curl -s -o /dev/null -w "%{http_code}" http://127.0.0.1:8090/api/v1/health | grep -q 200 && echo healthy && break; sleep 1; done`

- [ ] **Adım 3: Tek bir Python betiğiyle bootstrap → giriş → kullanıcı listesini çek → davet oluştur/kabul et → tekrar listele → site rolü ata/kaldır → tekrar listele — hepsi gerçek bir HTTP istemcisiyle**

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
login_payload = call('POST', '/api/v1/sessions', {'email': 'admin@example.com', 'password': 'correct horse battery staple', 'totp_code': totp_code(admin_secret)})
csrf = login_payload['csrf_token']

users_before = call('GET', '/api/v1/users')
print('users before invite:', len(users_before['users']))

invite_payload = call('POST', '/api/v1/users/invites', {'email': 'operator@example.com', 'role': 'operator', 'site_ids': ['site_default']}, {'X-CSRF-Token': csrf})
token = invite_payload['token']
consume_payload = call('POST', f'/api/v1/invites/{token}/consume', {'password': 'a brand new password'})
operator_id = consume_payload['id']

users_after_invite = call('GET', '/api/v1/users')
print('users after invite:', len(users_after_invite['users']))
operator_entry = next(u for u in users_after_invite['users'] if u['email'] == 'operator@example.com')
print('operator site roles after consume:', operator_entry['site_roles'])

call('POST', '/api/v1/sites/site-b/memberships', {'user_id': operator_id, 'role': 'viewer'}, {'X-CSRF-Token': csrf})
users_after_assign = call('GET', '/api/v1/users')
operator_entry = next(u for u in users_after_assign['users'] if u['email'] == 'operator@example.com')
print('operator site roles after assigning site-b:', operator_entry['site_roles'])

call('DELETE', '/api/v1/sites/site-b/memberships/' + operator_id, None, {'X-CSRF-Token': csrf})
users_after_revoke = call('GET', '/api/v1/users')
operator_entry = next(u for u in users_after_revoke['users'] if u['email'] == 'operator@example.com')
print('operator site roles after revoking site-b:', operator_entry['site_roles'])
"
```

Beklenen çıktı sırasıyla: "users before invite: 1", "users after invite: 2", "operator site roles after consume: [{'site_id': 'site_default', 'role': 'operator'}]", "...after assigning site-b: [{'site_id': 'site_default', ...}, {'site_id': 'site-b', 'role': 'viewer'}]", "...after revoking site-b: [{'site_id': 'site_default', 'role': 'operator'}]".

- [ ] **Adım 4: Temizlik**

```bash
cd /Users/gokaybaz/Documents/ChatGPT/bazusop
docker compose down -v
```

- [ ] **Adım 5: Go ve frontend testlerini son kez birlikte çalıştır (gerçek Postgres ile)**

```bash
docker rm -f bazusop-test-pg-1340b >/dev/null 2>&1
docker run -d --name bazusop-test-pg-1340b -e POSTGRES_USER=bazusop -e POSTGRES_PASSWORD=bazusop_test -e POSTGRES_DB=bazusop_test -p 5432:5432 postgres:18 >/dev/null
for i in $(seq 1 30); do docker exec bazusop-test-pg-1340b pg_isready -U bazusop -d bazusop_test >/dev/null 2>&1 && break; sleep 1; done
cd /Users/gokaybaz/Documents/ChatGPT/bazusop
go build ./... && go vet ./... && gofmt -l .
BAZUSOP_TEST_DATABASE_URL='postgres://bazusop:bazusop_test@127.0.0.1:5432/bazusop_test?sslmode=disable' GOCACHE=/tmp/bazusop-go-cache go test ./... -count=1 2>&1 | tail -30
docker rm -f bazusop-test-pg-1340b >/dev/null 2>&1
cd web && npm test 2>&1 | tail -50 && npm run build 2>&1 | tail -20
```

Beklenen: hepsi BAŞARILI

## Kendi Kendine İnceleme

**1. Spec kapsaması.** Spike 13.4'ün kabul sinyali ("Platform yöneticisi tarayıcıdan davet oluşturur, kullanıcı listesini görür, site rolü atar/kaldırır") → Görev 3-4'ün bileşen testleri ve Görev 5'in Adım 3'ü (gerçek HTTP istemcisiyle, her adımdan sonra listeyi tekrar çekip değişikliği doğrulayarak) bunu kanıtlıyor. Spec'in "Backend değişiklikleri #2" bölümündeki `GET /api/v1/users` tasarımı (yalnız platform yöneticisi, `id/email/role/disabled_at/totp_confirmed_at` + site rolleri, mutasyon yok) Görev 1-2'de birebir uygulandı.

**2. Placeholder taraması.** Her adımda gerçek, eksiksiz kod var. Görev 4 Adım 2'deki "Yazarken düzeltme" notu, ilk yaklaşımın (aktif sekme göstergesi için `border` kullanma dürtüsü) `design-system.test.ts`'in kuralına çarpabileceğini önceden fark edip `box-shadow`'a yönlendiriyor — 13.2'de gerçekten yaşanan bir hatayı burada önceden engelliyor. Kendi kendine inceleme sırasında ayrıca iki küçük düzeltme yapıldı: Görev 2 Adım 1'deki `userPayload` struct alan hizalaması gofmt'a uygun hale getirildi, ve Görev 5 Adım 3'teki doğrulama betiğinin `call()` yardımcı fonksiyonu boş yanıt gövdelerini (`DELETE` → `204`) `response.length` (bazı durumlarda `None` olup script'i çökertebilir) yerine `response.read()`'in gerçek uzunluğuna bakarak güvenilir şekilde ele alacak biçimde düzeltildi.

**3. Tip tutarlılığı.** `ManagedUser`/`UserSiteRole` (`web/src/types.ts`, Görev 3 Adım 1) `SettingsUsersTab`'ın state'inde ve `GET /api/v1/users`'ın Görev 2'de tanımlanan JSON şeklinde (`id, email, role, disabled_at?, totp_confirmed_at?, site_roles: [{site_id, role}]`) birebir eşleşiyor. `identity.Service.UsersForOrganization` (Görev 1) hem `MemoryStore` hem Postgres `Store` implementasyonlarında aynı imzayla (`ctx, organizationID) ([]User, error)`) tanımlanıyor.
