# Servis Hesabı Yönetimi Arayüzü (Spike 13.5) Uygulama Planı

> **Otonom çalışanlar için:** ZORUNLU ALT-SKILL: Bu planı görev görev uygulamak için superpowers:subagent-driven-development (önerilen) veya superpowers:executing-plans kullan. Adımlar `- [ ]` checkbox söz dizimiyle takip edilir.

**Amaç:** Ayarlar sayfasına, tarayıcıdan bir site yöneticisinin servis hesabı oluşturup (token bir kez görünür), mevcut hesapların token'ını yenileyebildiği (rotate), iptal edebildiği (revoke) ve hesabı devre dışı bırakabildiği bir "Servis hesapları" sekmesi ekler. Backend'de oluşturma/rotate/revoke/disable/listeleme uçları 12.x'te zaten eklenmişti — ama listeleme ucu yalnız hesabın kendisini döner, hiçbir *aktif token* bilgisi taşımaz. Bu, arayüzün "revoke" aksiyonu için gereken token ID'sini hiç bilememesi anlamına geliyor (create/rotate yanıtları token'ı yalnız bir kerelik gösterir, saklamaz). Bu spike bu eksikliği de dolduruyor.

**Mimari:** `internal/serviceaccounts.Service.ListForSite` şu an yalnız `[]ServiceAccount` dönüyor; dönüş tipi `[]AccountSummary` olacak şekilde genişletilir (`AccountSummary` `ServiceAccount`'ı gömer, artı `ActiveToken *Token` alanı taşır — mevcut `accounts[0].Name` gibi kod, gömme sayesinde değişmeden çalışmaya devam eder). Yeni `Store.ActiveTokensForSite(ctx, siteID) (map[string]Token, error)` metodu, site'daki her hesap için (varsa) en güncel iptal edilmemiş token'ı tek bir sorguda getirir — bu, `GET /api/v1/users`'ın 13.4'te iki store'u (identity + authorization) birleştirmesiyle aynı desen, burada tek domain içinde iki tabloyu (`service_accounts` + `service_account_tokens`) birleştiriyor. `internal/server/serviceaccounts.go`'daki `handleListServiceAccounts` bu genişletilmiş veriyi `id/name/role/created_at/disabled_at/active_token{id,expires_at,last_used_at}` şeklinde, düzgün `json` etiketleriyle kendi yanıt DTO'suna eşler — mevcut ham domain tipini (hiç `json` etiketi yok) doğrudan pazarlamak yerine, tıpkı create/rotate uçlarının ve 13.4'ün `handleListUsers`'ının zaten yaptığı gibi. `POST /api/v1/sites/{siteID}/service-accounts`, `POST /api/v1/service-accounts/{accountID}/rotate`, `DELETE /api/v1/service-accounts/tokens/{tokenID}`, `DELETE /api/v1/service-accounts/{accountID}` uçlarının hiçbiri değişmiyor — route'lar zaten var, yalnız listeleme yanıtı zenginleşiyor. Frontend'de `web/src/pages/settings-service-accounts.tsx` yeni, izole bir bileşen olarak eklenir (13.1-13.4'ün izole test deseniyle tutarlı); `SettingsPage` üçüncü bir sekme ("Servis hesapları") kazanır.

**Teknoloji yığını:** Go 1.26 (mevcut uçların genişletilmesi, yeni route yok), React 19 + TypeScript (yeni sekme + form bileşeni).

**Spec:** [docs/superpowers/specs/2026-09-22-frontend-identity-rbac-ui-design.md](../specs/2026-09-22-frontend-identity-rbac-ui-design.md) — bu plan yalnız Spike 13.5'i kapsar.

## Genel Kısıtlar

- **Bu spike'ta `internal/server/server.go`'ya HİÇBİR yeni route eklenmez.** Create/list/rotate/revoke/disable uçları 12.x'te (`1b245f6`, `bc7cf46`, `eef13d4`) zaten eklendi ve zaten `registerAudited` ile audit trail'e bağlı. Yalnız `handleListServiceAccounts`'ın yanıt gövdesi zenginleşiyor.
- **Servis hesabını kalıcı silme (hard delete) bu spike'ın KAPSAMI DIŞINDA** — spec'in 13.5 satırı yalnız "oluşturur, rotate/revoke/disable eder" diyor; backend'de zaten böyle bir uç yok, eklemek kapsam genişlemesi olurdu.
- **Devre dışı bir hesabı yeniden etkinleştiren bir "enable" aksiyonu eklenmez** — backend'de `DisableAccount` var ama tersi (`EnableAccount`) yok; bu da spec'in kapsamı dışında.
- **Site ID sabit `site_default` kullanılır, ayrı bir site seçici eklenmez** — sistem hâlâ tek-site mimarisini izliyor (11.5'in kararı, 13.4'ün Genel Kısıtlar'ında da aynı ilke). `settings-users.tsx`'in aksine burada kullanıcıdan site ID bile istenmiyor — create/list uçları zaten site ID'yi URL path'inde taşıyor, form alanı gerekmiyor.
- **Token geçmişi / iptal edilmiş token'ların listesi eklenmez** — yalnız hesabın *o anki aktif* token'ının (varsa) `id/expires_at/last_used_at`'i gösterilir; bu, revoke aksiyonunun hedefleyeceği token ID'sini bilmesi için gereken minimum bilgi. `last_used_ip` gibi ek alanlar da gösterilmez (backend zaten tutuyor ama UI'a taşınmıyor — YAGNI).
- **`SettingsPage`'in mevcut "Genel" ve "Kullanıcılar" sekmeleri hiç değişmez** — yalnız üçüncü bir sekme eklenir; varsayılan sekme "Genel" kalır ki mevcut `app.test.tsx` testleri değişmeden geçsin.

## Dosya Yapısı

- Değiştir: `internal/serviceaccounts/serviceaccounts.go`, `internal/serviceaccounts/memorystore.go`, `internal/serviceaccounts/serviceaccounts_test.go`.
- Değiştir: `internal/storage/postgres/serviceaccounts.go`, `internal/storage/postgres/serviceaccounts_integration_test.go`.
- Değiştir: `internal/server/serviceaccounts.go`, `internal/server/serviceaccounts_test.go`.
- Değiştir: `web/src/types.ts`, `web/src/pages/settings.tsx`, `web/src/pages/settings.test.tsx`, `web/src/styles.css`.
- Oluştur: `web/src/pages/settings-service-accounts.tsx`, `web/src/pages/settings-service-accounts.test.tsx`.

## Görev 1: Backend — `AccountSummary` ve `ActiveTokensForSite` (serviceaccounts domain katmanı)

**Dosyalar:**
- Değiştir: `internal/serviceaccounts/serviceaccounts.go`, `internal/serviceaccounts/memorystore.go`, `internal/serviceaccounts/serviceaccounts_test.go`.
- Değiştir: `internal/storage/postgres/serviceaccounts.go`, `internal/storage/postgres/serviceaccounts_integration_test.go`.

**Arayüzler:**
- Üretir: `serviceaccounts.AccountSummary` (gömülü `ServiceAccount` + `ActiveToken *Token`), `Store.ActiveTokensForSite(ctx, siteID) (map[string]Token, error)`, `Service.ListForSite(ctx, siteID) ([]AccountSummary, error)` (imza değişikliği — Görev 2'de HTTP katmanı tüketir).

- [x] **Adım 1: `internal/serviceaccounts/serviceaccounts.go`'a `AccountSummary` tipini ve `Store` arayüzüne yeni metodu ekle**

`Token` tipinden hemen sonra ekle:

```go
// AccountSummary is what Service.ListForSite returns — the account plus
// its current active token (nil if none, e.g. right after a Revoke with
// no follow-up Rotate). ServiceAccount is embedded so existing callers
// that access fields like summary.Name keep working unchanged.
type AccountSummary struct {
	ServiceAccount
	ActiveToken *Token
}
```

`Store` arayüzüne (`AccountsForSite` satırından hemen sonra) ekle:

```go
	ActiveTokensForSite(ctx context.Context, siteID string) (map[string]Token, error)
```

`Service.ListForSite`'ı şu şekilde değiştir (dönüş tipi `[]ServiceAccount`'tan `[]AccountSummary`'e değişiyor):

```go
func (service *Service) ListForSite(ctx context.Context, siteID string) ([]AccountSummary, error) {
	accounts, err := service.store.AccountsForSite(ctx, siteID)
	if err != nil {
		return nil, err
	}
	activeTokens, err := service.store.ActiveTokensForSite(ctx, siteID)
	if err != nil {
		return nil, err
	}
	summaries := make([]AccountSummary, 0, len(accounts))
	for _, account := range accounts {
		summary := AccountSummary{ServiceAccount: account}
		if token, ok := activeTokens[account.ID]; ok {
			tokenCopy := token
			summary.ActiveToken = &tokenCopy
		}
		summaries = append(summaries, summary)
	}
	return summaries, nil
}
```

- [x] **Adım 2: `internal/serviceaccounts/memorystore.go`'ya `ActiveTokensForSite`'ı ekle**

`AccountsForSite` metodundan hemen sonra ekle:

```go
func (store *MemoryStore) ActiveTokensForSite(_ context.Context, siteID string) (map[string]Token, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	result := make(map[string]Token)
	for _, record := range store.tokens {
		if record.token.RevokedAt != nil {
			continue
		}
		account, ok := store.accounts[record.token.ServiceAccountID]
		if !ok || account.SiteID != siteID {
			continue
		}
		existing, found := result[record.token.ServiceAccountID]
		if !found || record.token.CreatedAt.After(existing.CreatedAt) {
			result[record.token.ServiceAccountID] = record.token
		}
	}
	return result, nil
}
```

(Yalnız iptal edilmemiş token'lar arasından, hesap başına en son oluşturulanı seçiyor — normal akışta bir hesabın aynı anda en fazla bir aktif token'ı olur [`RotateToken`/`DisableAccount` yeni bir tane oluşturmadan önce eskilerini iptal ediyor], `CreatedAt.After` karşılaştırması yalnız bir güvenlik payı.)

- [x] **Adım 3: `internal/storage/postgres/serviceaccounts.go`'a `ActiveTokensForSite`'ı ekle**

`AccountsForSite`'tan hemen sonra ekle:

```go
func (store *Store) ActiveTokensForSite(ctx context.Context, siteID string) (map[string]serviceaccounts.Token, error) {
	rows, err := store.pool.Query(ctx, `
		SELECT DISTINCT ON (t.service_account_id)
			t.id, t.service_account_id, t.created_at, t.expires_at, t.last_used_at, t.last_used_ip, t.revoked_at
		FROM service_account_tokens t
		JOIN service_accounts a ON a.id = t.service_account_id
		WHERE a.site_id = $1 AND t.revoked_at IS NULL
		ORDER BY t.service_account_id, t.created_at DESC`, siteID)
	if err != nil {
		return nil, fmt.Errorf("query active service account tokens: %w", err)
	}
	defer rows.Close()
	result := make(map[string]serviceaccounts.Token)
	for rows.Next() {
		var token serviceaccounts.Token
		var lastUsedIP *string
		if err := rows.Scan(&token.ID, &token.ServiceAccountID, &token.CreatedAt, &token.ExpiresAt, &token.LastUsedAt, &lastUsedIP, &token.RevokedAt); err != nil {
			return nil, fmt.Errorf("scan active service account token: %w", err)
		}
		if lastUsedIP != nil {
			token.LastUsedIP = *lastUsedIP
		}
		result[token.ServiceAccountID] = token
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate active service account tokens: %w", err)
	}
	return result, nil
}
```

(`DISTINCT ON (t.service_account_id)` + `ORDER BY t.service_account_id, t.created_at DESC`, PostgreSQL'e özgü — her hesap için en son oluşturulan iptal edilmemiş token'ı tek satıra indirger; memory store'daki elle-yazılmış eşdeğeri budur.)

- [x] **Adım 4: Build'i doğrula**

Çalıştır: `cd /Users/gokaybaz/Documents/ChatGPT/bazusop && go build ./internal/serviceaccounts/... ./internal/storage/postgres/... 2>&1 | head -30`
Beklenen: BAŞARILI

- [x] **Adım 5: `internal/serviceaccounts/serviceaccounts_test.go`'daki mevcut `TestListForSiteReturnsOnlyThatSitesAccounts` testinin hâlâ geçtiğini doğrula, sonra yeni bir test ekle**

`AccountSummary`'nin `ServiceAccount`'ı gömmesi sayesinde `accounts[0].Name` erişimi hiç değişmeden çalışır — bu testte kod değişikliği gerekmiyor.

Dosyanın sonuna ekle:

```go
func TestListForSiteIncludesTheActiveTokenAndOmitsARevokedOne(t *testing.T) {
	t.Parallel()
	service := newTestService(t, time.Now)
	account, firstToken, err := service.CreateAccount(t.Context(), "org_default", "site_default", "ci-bot", authorization.SiteRoleOperator, 0)
	if err != nil {
		t.Fatal(err)
	}
	firstTokenID, _, _ := serviceaccounts.ParseToken(firstToken)

	afterCreate, err := service.ListForSite(t.Context(), "site_default")
	if err != nil || len(afterCreate) != 1 || afterCreate[0].ActiveToken == nil || afterCreate[0].ActiveToken.ID != firstTokenID {
		t.Fatalf("expected the fresh token to be reported as active, got %#v %v", afterCreate, err)
	}

	secondToken, err := service.RotateToken(t.Context(), "site_default", account.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	secondTokenID, _, _ := serviceaccounts.ParseToken(secondToken)

	afterRotate, err := service.ListForSite(t.Context(), "site_default")
	if err != nil || len(afterRotate) != 1 || afterRotate[0].ActiveToken == nil || afterRotate[0].ActiveToken.ID != secondTokenID {
		t.Fatalf("expected the rotated token to replace the active one, got %#v %v", afterRotate, err)
	}

	if err := service.RevokeToken(t.Context(), "site_default", secondTokenID); err != nil {
		t.Fatal(err)
	}
	afterRevoke, err := service.ListForSite(t.Context(), "site_default")
	if err != nil || len(afterRevoke) != 1 || afterRevoke[0].ActiveToken != nil {
		t.Fatalf("expected no active token after revoking the only one, got %#v %v", afterRevoke, err)
	}
}
```

- [x] **Adım 6: Testi çalıştır**

Çalıştır: `cd /Users/gokaybaz/Documents/ChatGPT/bazusop && GOCACHE=/tmp/bazusop-go-cache go test ./internal/serviceaccounts/... -run 'TestListForSite' -v 2>&1 | tail -40`
Beklenen: BAŞARILI

- [x] **Adım 7: `internal/storage/postgres/serviceaccounts_integration_test.go`'a bir doğrulama ekle**

`secondToken` oluşturulduktan hemen sonra (`if err := store.CreateToken(ctx, secondToken, "hash-2"); err != nil { t.Fatalf(...) }` bloğundan sonra), `RevokeActiveTokensForAccount` çağrısından önce ekle:

```go
	activeBeforeRevoke, err := store.ActiveTokensForSite(ctx, siteID)
	if err != nil || activeBeforeRevoke[account.ID].ID != secondToken.ID {
		t.Fatalf("expected the active tokens map to report the second token as active, got %#v %v", activeBeforeRevoke, err)
	}
```

`secondRevoked` kontrolünden hemen sonra (ikinci token'ın iptal edildiği doğrulandıktan sonra), `DisableAccount` çağrısından önce ekle:

```go
	activeAfterRevoke, err := store.ActiveTokensForSite(ctx, siteID)
	if err != nil {
		t.Fatalf("active tokens after revoke: %v", err)
	}
	if _, stillActive := activeAfterRevoke[account.ID]; stillActive {
		t.Fatalf("expected no active token for the account after revoking, got %#v", activeAfterRevoke)
	}
```

- [x] **Adım 8: Testleri çalıştır (Postgres gerektirir)**

```bash
docker rm -f bazusop-test-pg-1350 >/dev/null 2>&1
docker run -d --name bazusop-test-pg-1350 -e POSTGRES_USER=bazusop -e POSTGRES_PASSWORD=bazusop_test -e POSTGRES_DB=bazusop_test -p 5432:5432 postgres:18 >/dev/null
for i in $(seq 1 30); do docker exec bazusop-test-pg-1350 pg_isready -U bazusop -d bazusop_test >/dev/null 2>&1 && break; sleep 1; done
cd /Users/gokaybaz/Documents/ChatGPT/bazusop
BAZUSOP_TEST_DATABASE_URL='postgres://bazusop:bazusop_test@127.0.0.1:5432/bazusop_test?sslmode=disable' GOCACHE=/tmp/bazusop-go-cache go test ./internal/storage/postgres/... -run 'TestPostgresServiceAccountLifecycle' -v 2>&1 | tail -30
docker rm -f bazusop-test-pg-1350 >/dev/null 2>&1
```

Beklenen: BAŞARILI

- [x] **Adım 9: Tam paket testlerini çalıştır (Postgres olmadan, memory-store testleri için)**

Çalıştır: `cd /Users/gokaybaz/Documents/ChatGPT/bazusop && go vet ./internal/serviceaccounts/... ./internal/storage/postgres/... && gofmt -l internal/serviceaccounts/*.go internal/storage/postgres/*.go && GOCACHE=/tmp/bazusop-go-cache go test ./internal/serviceaccounts/... 2>&1 | tail -10`
Beklenen: vet/gofmt çıktısı yok; `ok`

- [x] **Adım 10: Commit**

```bash
cd /Users/gokaybaz/Documents/ChatGPT/bazusop
git add internal/serviceaccounts/serviceaccounts.go internal/serviceaccounts/memorystore.go internal/serviceaccounts/serviceaccounts_test.go internal/storage/postgres/serviceaccounts.go internal/storage/postgres/serviceaccounts_integration_test.go
git commit -m "feat: add serviceaccounts.Service.ListForSite active-token summaries for the upcoming management UI"
```

## Görev 2: Backend — `GET /api/v1/sites/{siteID}/service-accounts` yanıtını zenginleştir

**Dosyalar:**
- Değiştir: `internal/server/serviceaccounts.go`, `internal/server/serviceaccounts_test.go`.

**Arayüzler:**
- Üretir: `GET /api/v1/sites/{siteID}/service-accounts` → `{"service_accounts": [{"id","name","role","created_at","disabled_at"?,"active_token": {"id","expires_at","last_used_at"?} | null}]}`. Route zaten var (`internal/server/server.go`, değişmiyor); yalnız `handleListServiceAccounts`'ın gövdesi değişiyor.

- [x] **Adım 1: `internal/server/serviceaccounts.go`'daki `handleListServiceAccounts`'ı değiştir**

Mevcut fonksiyonun tamamını (`func handleListServiceAccounts(...) http.HandlerFunc { ... }`) şununla değiştir:

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
		summaries, err := service.ListForSite(request.Context(), request.PathValue("siteID"))
		if err != nil {
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		type activeTokenPayload struct {
			ID         string  `json:"id"`
			ExpiresAt  string  `json:"expires_at"`
			LastUsedAt *string `json:"last_used_at,omitempty"`
		}
		type accountPayload struct {
			ID          string              `json:"id"`
			Name        string              `json:"name"`
			Role        string              `json:"role"`
			CreatedAt   string              `json:"created_at"`
			DisabledAt  *string             `json:"disabled_at,omitempty"`
			ActiveToken *activeTokenPayload `json:"active_token"`
		}
		payload := make([]accountPayload, 0, len(summaries))
		for _, summary := range summaries {
			entry := accountPayload{
				ID:        summary.ID,
				Name:      summary.Name,
				Role:      string(summary.Role),
				CreatedAt: summary.CreatedAt.Format(timeLayout),
			}
			if summary.DisabledAt != nil {
				formatted := summary.DisabledAt.Format(timeLayout)
				entry.DisabledAt = &formatted
			}
			if summary.ActiveToken != nil {
				tokenPayload := &activeTokenPayload{ID: summary.ActiveToken.ID, ExpiresAt: summary.ActiveToken.ExpiresAt.Format(timeLayout)}
				if summary.ActiveToken.LastUsedAt != nil {
					formatted := summary.ActiveToken.LastUsedAt.Format(timeLayout)
					tokenPayload.LastUsedAt = &formatted
				}
				entry.ActiveToken = tokenPayload
			}
			payload = append(payload, entry)
		}
		writeJSON(response, http.StatusOK, struct {
			ServiceAccounts []accountPayload `json:"service_accounts"`
		}{payload})
	}
}
```

(`summary.ID`/`summary.Name`/`summary.Role`/`summary.CreatedAt`/`summary.DisabledAt`, `AccountSummary`'nin gömdüğü `ServiceAccount`'tan gelir — Görev 1'deki gömme sayesinde doğrudan erişilebilir. `timeLayout`, `internal/server/sessions.go`'da zaten tanımlı, aynı pakette olduğundan ekstra import gerekmiyor.)

- [x] **Adım 2: Build'i doğrula**

Çalıştır: `cd /Users/gokaybaz/Documents/ChatGPT/bazusop && go build ./... 2>&1 | head -30 && echo BUILD_OK`
Beklenen: `BUILD_OK`

- [x] **Adım 3: `internal/server/serviceaccounts_test.go`'a yeni bir test ekle**

`TestListServiceAccountsResponseNeverIncludesTheToken` testinden hemen sonra ekle:

```go
func TestListServiceAccountsIncludesTheActiveTokenSummary(t *testing.T) {
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
		ID    string `json:"id"`
		Token string `json:"token"`
	}
	if err := json.NewDecoder(createResponse.Body).Decode(&created); err != nil || created.Token == "" {
		t.Fatalf("expected a one-time token from creation, got %v", err)
	}
	tokenID, _, ok := serviceaccounts.ParseToken(created.Token)
	if !ok {
		t.Fatal("expected a parseable token")
	}

	type accountPayload struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Role        string `json:"role"`
		CreatedAt   string `json:"created_at"`
		ActiveToken *struct {
			ID        string `json:"id"`
			ExpiresAt string `json:"expires_at"`
		} `json:"active_token"`
	}

	listBeforeRequest := httptest.NewRequest(http.MethodGet, "/api/v1/sites/site_default/service-accounts", nil)
	for _, cookie := range adminCookies {
		listBeforeRequest.AddCookie(cookie)
	}
	listBefore := httptest.NewRecorder()
	handler.ServeHTTP(listBefore, listBeforeRequest)
	var beforePayload struct {
		ServiceAccounts []accountPayload `json:"service_accounts"`
	}
	if err := json.NewDecoder(listBefore.Body).Decode(&beforePayload); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(beforePayload.ServiceAccounts) != 1 || beforePayload.ServiceAccounts[0].ActiveToken == nil || beforePayload.ServiceAccounts[0].ActiveToken.ID != tokenID {
		t.Fatalf("expected the created account's active token to be reported, got %#v", beforePayload.ServiceAccounts)
	}
	if beforePayload.ServiceAccounts[0].Name != "ci-bot" || beforePayload.ServiceAccounts[0].Role != "operator" || beforePayload.ServiceAccounts[0].CreatedAt == "" {
		t.Fatalf("expected name/role/created_at to be populated, got %#v", beforePayload.ServiceAccounts[0])
	}

	revokeRequest := httptest.NewRequest(http.MethodDelete, "/api/v1/service-accounts/tokens/"+tokenID, nil)
	for _, cookie := range adminCookies {
		revokeRequest.AddCookie(cookie)
	}
	revokeRequest.Header.Set("X-CSRF-Token", csrfToken)
	revokeResponse := httptest.NewRecorder()
	handler.ServeHTTP(revokeResponse, revokeRequest)
	if revokeResponse.Code != http.StatusNoContent {
		t.Fatalf("expected 204 revoking the token, got %d: %s", revokeResponse.Code, revokeResponse.Body.String())
	}

	listAfterRequest := httptest.NewRequest(http.MethodGet, "/api/v1/sites/site_default/service-accounts", nil)
	for _, cookie := range adminCookies {
		listAfterRequest.AddCookie(cookie)
	}
	listAfter := httptest.NewRecorder()
	handler.ServeHTTP(listAfter, listAfterRequest)
	var afterPayload struct {
		ServiceAccounts []accountPayload `json:"service_accounts"`
	}
	if err := json.NewDecoder(listAfter.Body).Decode(&afterPayload); err != nil {
		t.Fatalf("decode list after revoke: %v", err)
	}
	if len(afterPayload.ServiceAccounts) != 1 || afterPayload.ServiceAccounts[0].ActiveToken != nil {
		t.Fatalf("expected no active token after revoke, got %#v", afterPayload.ServiceAccounts)
	}
}
```

- [x] **Adım 4: Testi çalıştır**

Çalıştır: `cd /Users/gokaybaz/Documents/ChatGPT/bazusop && GOCACHE=/tmp/bazusop-go-cache go test ./internal/server/... -run 'TestListServiceAccounts' -v 2>&1 | tail -40`
Beklenen: BAŞARILI

- [x] **Adım 5: Tam paket testlerini çalıştır**

Çalıştır: `cd /Users/gokaybaz/Documents/ChatGPT/bazusop && go vet ./internal/server/... && gofmt -l internal/server/*.go && GOCACHE=/tmp/bazusop-go-cache go test ./internal/server/... 2>&1 | tail -10`
Beklenen: vet/gofmt çıktısı yok; `ok`

- [x] **Adım 6: Commit**

```bash
cd /Users/gokaybaz/Documents/ChatGPT/bazusop
git add internal/server/serviceaccounts.go internal/server/serviceaccounts_test.go
git commit -m "feat: report each service account's active token summary from the list endpoint"
```

## Görev 3: Frontend — `SettingsServiceAccountsTab` bileşeni

**Dosyalar:**
- Değiştir: `web/src/types.ts`, `web/src/styles.css`.
- Oluştur: `web/src/pages/settings-service-accounts.tsx`, `web/src/pages/settings-service-accounts.test.tsx`.

**Arayüzler:**
- Tüketir: `useSession()` → `{ apiFetch }` (`web/src/lib/session.tsx`), `Badge`/`Card` (`web/src/components/ui/`), `EmptyFeature` (`web/src/components/empty-feature.tsx`).
- Üretir: `SettingsServiceAccountsTab` bileşeni — Görev 4'te `SettingsPage` tüketir.

- [x] **Adım 1: `web/src/types.ts`'e yeni tipleri ekle**

Dosyanın sonuna (`ManagedUser`'dan hemen sonra) ekle:

```ts
export type ServiceAccountToken = { id: string; expires_at: string; last_used_at?: string }
export type ServiceAccount = { id: string; name: string; role: string; created_at: string; disabled_at?: string; active_token: ServiceAccountToken | null }
```

- [x] **Adım 2: `web/src/pages/settings-service-accounts.tsx`'i oluştur**

```tsx
import { type FormEvent, useEffect, useState } from "react"
import { KeyRound } from "lucide-react"

import { Badge } from "../components/ui/badge"
import { Card } from "../components/ui/card"
import { EmptyFeature } from "../components/empty-feature"
import { useSession } from "../lib/session"
import type { ServiceAccount } from "../types"

const jsonHeaders = { "Content-Type": "application/json" }
const siteID = "site_default"

export function SettingsServiceAccountsTab() {
  const { apiFetch } = useSession()
  const [accounts, setAccounts] = useState<ServiceAccount[]>([])
  const [state, setState] = useState<"loading" | "ready" | "error" | "forbidden">("loading")
  const [name, setName] = useState("")
  const [role, setRole] = useState<"site-admin" | "operator" | "viewer">("operator")
  const [expiryDays, setExpiryDays] = useState("")
  const [createError, setCreateError] = useState("")
  const [revealedToken, setRevealedToken] = useState<{ name: string; token: string } | null>(null)
  const [actionMessage, setActionMessage] = useState("")

  function loadAccounts() {
    setState("loading")
    fetch(`/api/v1/sites/${siteID}/service-accounts`)
      .then((response) => {
        if (response.status === 401 || response.status === 403) return Promise.reject(new Error("forbidden"))
        if (!response.ok) return Promise.reject(new Error("unavailable"))
        return response.json() as Promise<{ service_accounts: ServiceAccount[] }>
      })
      .then((payload) => {
        setAccounts(payload.service_accounts ?? [])
        setState("ready")
      })
      .catch((error: unknown) => setState((error as Error).message === "forbidden" ? "forbidden" : "error"))
  }

  useEffect(() => {
    loadAccounts()
  }, [])

  function createAccount(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setCreateError("")
    setRevealedToken(null)
    apiFetch(`/api/v1/sites/${siteID}/service-accounts`, {
      method: "POST",
      headers: jsonHeaders,
      body: JSON.stringify({ name, role, expiry_days: expiryDays ? Number(expiryDays) : 0 }),
    })
      .then((response) => {
        if (!response.ok) throw new Error()
        return response.json() as Promise<{ id: string; name: string; role: string; token: string }>
      })
      .then((payload) => {
        setRevealedToken({ name: payload.name, token: payload.token })
        setName("")
        setExpiryDays("")
        loadAccounts()
      })
      .catch(() => setCreateError("Servis hesabı oluşturulamadı; alanları kontrol edin."))
  }

  function rotateToken(account: ServiceAccount) {
    setActionMessage("")
    setRevealedToken(null)
    apiFetch(`/api/v1/service-accounts/${encodeURIComponent(account.id)}/rotate`, {
      method: "POST",
      headers: jsonHeaders,
      body: JSON.stringify({ expiry_days: 0 }),
    })
      .then((response) => {
        if (!response.ok) throw new Error()
        return response.json() as Promise<{ token: string }>
      })
      .then((payload) => {
        setRevealedToken({ name: account.name, token: payload.token })
        loadAccounts()
      })
      .catch(() => setActionMessage(`${account.name} için token yenilenemedi.`))
  }

  function revokeToken(account: ServiceAccount) {
    if (!account.active_token) return
    setActionMessage("")
    apiFetch(`/api/v1/service-accounts/tokens/${encodeURIComponent(account.active_token.id)}`, { method: "DELETE" })
      .then((response) => {
        if (!response.ok) throw new Error()
        setActionMessage(`${account.name} token'ı iptal edildi.`)
        loadAccounts()
      })
      .catch(() => setActionMessage(`${account.name} token'ı iptal edilemedi.`))
  }

  function disableAccount(account: ServiceAccount) {
    setActionMessage("")
    apiFetch(`/api/v1/service-accounts/${encodeURIComponent(account.id)}`, { method: "DELETE" })
      .then((response) => {
        if (!response.ok) throw new Error()
        setActionMessage(`${account.name} devre dışı bırakıldı.`)
        loadAccounts()
      })
      .catch(() => setActionMessage(`${account.name} devre dışı bırakılamadı.`))
  }

  if (state === "forbidden") {
    return <EmptyFeature icon={KeyRound} text="Bu ekranı görüntülemek için site yöneticisi oturumu gerekir." title="Servis hesabı yönetimine erişim yetkiniz yok" />
  }
  if (state === "error") {
    return <EmptyFeature icon={KeyRound} text="Hub bağlantısını kontrol edin." title="Servis hesaplarına ulaşılamıyor" />
  }

  return (
    <div className="settings-users">
      {revealedToken ? (
        <Card className="settings-form-card token-reveal-card">
          <h3>{revealedToken.name} için token</h3>
          <p className="auth-warning">Bu token yalnız bir kez gösterilir. Güvenli bir yere kaydedin.</p>
          <code>{revealedToken.token}</code>
          <button onClick={() => void navigator.clipboard?.writeText(revealedToken.token)} type="button">
            Kopyala
          </button>
        </Card>
      ) : null}

      <Card className="table-card page-card">
        <div className="card-header">
          <div>
            <h2>Servis hesapları</h2>
            <p>Otomasyon ve entegrasyonlar için kullanılan servis hesapları</p>
          </div>
        </div>
        {state === "loading" ? (
          <p className="muted">Yükleniyor…</p>
        ) : accounts.length === 0 ? (
          <p className="muted">Henüz servis hesabı yok.</p>
        ) : (
          <div className="table-scroll">
            <table aria-label="Servis hesapları">
              <thead>
                <tr>
                  <th>Ad</th>
                  <th>Rol</th>
                  <th>Oluşturulma</th>
                  <th>Token durumu</th>
                  <th>Durum</th>
                  <th>Aksiyonlar</th>
                </tr>
              </thead>
              <tbody>
                {accounts.map((account) => (
                  <tr key={account.id}>
                    <td>{account.name}</td>
                    <td>{account.role}</td>
                    <td>{account.created_at}</td>
                    <td>
                      <Badge className={account.active_token ? "totp-status confirmed" : "totp-status pending"}>
                        {account.active_token ? "Aktif token var" : "Token yok"}
                      </Badge>
                    </td>
                    <td>
                      <Badge className={account.disabled_at ? "user-status disabled" : "user-status active"}>
                        {account.disabled_at ? "Devre dışı" : "Aktif"}
                      </Badge>
                    </td>
                    <td>
                      <div className="service-account-actions">
                        <button disabled={Boolean(account.disabled_at)} onClick={() => rotateToken(account)} type="button">
                          Token yenile
                        </button>
                        <button disabled={!account.active_token} onClick={() => revokeToken(account)} type="button">
                          Token iptal et
                        </button>
                        <button disabled={Boolean(account.disabled_at)} onClick={() => disableAccount(account)} type="button">
                          Devre dışı bırak
                        </button>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
        {actionMessage ? <p aria-live="polite">{actionMessage}</p> : null}
      </Card>

      <div className="settings-users-forms">
        <Card className="settings-form-card">
          <h3>Servis hesabı oluştur</h3>
          <form className="alarm-form" onSubmit={createAccount}>
            <label>
              <span>Ad</span>
              <input onChange={(event) => setName(event.target.value)} required value={name} />
            </label>
            <label>
              <span>Rol</span>
              <select onChange={(event) => setRole(event.target.value as typeof role)} value={role}>
                <option value="site-admin">Site yöneticisi</option>
                <option value="operator">Operatör</option>
                <option value="viewer">İzleyici</option>
              </select>
            </label>
            <label>
              <span>Geçerlilik (gün, opsiyonel)</span>
              <input max={365} min={1} onChange={(event) => setExpiryDays(event.target.value)} placeholder="90" type="number" value={expiryDays} />
            </label>
            <button type="submit">Servis hesabı oluştur</button>
            {createError ? (
              <p aria-live="polite" className="auth-error">
                {createError}
              </p>
            ) : null}
          </form>
        </Card>
      </div>
    </div>
  )
}
```

- [x] **Adım 3: `web/src/styles.css`'e yeni sınıfları ekle**

`.settings-users-forms { ... }` kuralından hemen önce (`.settings-form-actions` kuralından sonra) ekle:

```css
.token-reveal-card { display: grid; gap: 10px; }
.token-reveal-card h3 { margin: 0; color: var(--strong); font-size: 15px; }
.token-reveal-card code { display: block; padding: 10px 12px; border-radius: 8px; background: var(--card); color: var(--strong); font: 600 13px ui-monospace, SFMono-Regular, Menlo, monospace; word-break: break-all; }
.token-reveal-card button { min-height: 38px; width: fit-content; border: 1px solid var(--border); border-radius: 8px; background: var(--surface); color: var(--strong); cursor: pointer; }
.service-account-actions { display: flex; flex-wrap: wrap; gap: 8px; }
```

`.incident-row button, .alarm-form button { ... }` ve `.incident-row button:disabled, .alarm-form button:disabled { ... }` kurallarını, `.service-account-actions button` / `.service-account-actions button:disabled` seçicilerini de kapsayacak şekilde genişlet:

```css
.incident-row button, .alarm-form button, .service-account-actions button { min-height: 36px; padding: 0 11px; border: 1px solid color-mix(in srgb, var(--accent) 34%, var(--border)); border-radius: 8px; color: var(--accent); background: var(--accent-soft); cursor: pointer; }
.incident-row button:disabled, .alarm-form button:disabled, .service-account-actions button:disabled { cursor: not-allowed; opacity: .45; }
```

(Mevcut iki satırın yerine geçiyor — üçüncü seçiciyi ekleyerek. Ne `border-left` ne `box-shadow: inset` kullanılıyor, `design-system.test.ts`'in kuralına uygun.)

- [x] **Adım 4: `web/src/pages/settings-service-accounts.test.tsx`'i oluştur**

```tsx
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

import { SettingsServiceAccountsTab } from "./settings-service-accounts"
import { SessionProvider } from "../lib/session"
import type { ServiceAccount } from "../types"

const authenticatedWhoAmI = { user_id: "admin-1", email: "admin@example.com", role: "platform-admin", csrf_token: "csrf-token-abc", expires_at: "2026-09-23T22:00:00Z" }

const baseAccounts: ServiceAccount[] = [
  { id: "acct-1", name: "ci-bot", role: "operator", created_at: "2026-09-20T10:00:00Z", active_token: { id: "token-1", expires_at: "2026-12-19T10:00:00Z" } },
  { id: "acct-2", name: "backup-bot", role: "viewer", created_at: "2026-09-21T10:00:00Z", disabled_at: "2026-09-22T10:00:00Z", active_token: null },
]

const listURL = "/api/v1/sites/site_default/service-accounts"

describe("SettingsServiceAccountsTab", () => {
  afterEach(() => vi.unstubAllGlobals())

  it("shows the service account table with token and status badges", async () => {
    vi.stubGlobal("fetch", vi.fn().mockImplementation((input: RequestInfo | URL) => {
      const url = String(input)
      if (url.endsWith("/api/v1/session")) return Promise.resolve({ ok: true, json: async () => authenticatedWhoAmI } as Response)
      if (url.endsWith(listURL)) return Promise.resolve({ ok: true, json: async () => ({ service_accounts: baseAccounts }) } as Response)
      return Promise.resolve({ ok: false } as Response)
    }))

    render(
      <SessionProvider>
        <SettingsServiceAccountsTab />
      </SessionProvider>,
    )

    const table = await screen.findByRole("table", { name: "Servis hesapları" })
    expect(within(table).getByText("ci-bot")).toBeInTheDocument()
    expect(within(table).getByText("backup-bot")).toBeInTheDocument()
    expect(within(table).getByText("Aktif token var")).toBeInTheDocument()
    expect(within(table).getByText("Token yok")).toBeInTheDocument()
    expect(within(table).getByText("Aktif")).toBeInTheDocument()
    expect(within(table).getByText("Devre dışı")).toBeInTheDocument()
  })

  it("shows a forbidden state for a non-site-admin session", async () => {
    vi.stubGlobal("fetch", vi.fn().mockImplementation((input: RequestInfo | URL) => {
      const url = String(input)
      if (url.endsWith("/api/v1/session")) return Promise.resolve({ ok: true, json: async () => ({ ...authenticatedWhoAmI, role: "operator" }) } as Response)
      if (url.endsWith(listURL)) return Promise.resolve({ ok: false, status: 403 } as Response)
      return Promise.resolve({ ok: false } as Response)
    }))

    render(
      <SessionProvider>
        <SettingsServiceAccountsTab />
      </SessionProvider>,
    )

    expect(await screen.findByRole("heading", { name: "Servis hesabı yönetimine erişim yetkiniz yok" })).toBeInTheDocument()
  })

  it("creates a service account and reveals the one-time token with a CSRF header", async () => {
    const fetchMock = vi.fn().mockImplementation((input: RequestInfo | URL) => {
      const url = String(input)
      if (url.endsWith("/api/v1/session")) return Promise.resolve({ ok: true, json: async () => authenticatedWhoAmI } as Response)
      if (url.endsWith(listURL)) return Promise.resolve({ ok: true, json: async () => ({ service_accounts: [] }) } as Response)
      return Promise.resolve({ ok: false } as Response)
    })
    vi.stubGlobal("fetch", fetchMock)

    render(
      <SessionProvider>
        <SettingsServiceAccountsTab />
      </SessionProvider>,
    )
    await screen.findByText("Henüz servis hesabı yok.")

    fetchMock.mockImplementation((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.endsWith("/api/v1/session")) return Promise.resolve({ ok: true, json: async () => authenticatedWhoAmI } as Response)
      if (url.endsWith(listURL) && (!init || init.method === undefined)) return Promise.resolve({ ok: true, json: async () => ({ service_accounts: [] }) } as Response)
      if (url.endsWith(listURL) && init?.method === "POST") return Promise.resolve({ ok: true, status: 201, json: async () => ({ id: "acct-3", name: "ci-bot", role: "operator", token: "bazusop_sat_newid_newsecret" }) } as Response)
      return Promise.resolve({ ok: false } as Response)
    })

    fireEvent.change(screen.getByLabelText("Ad"), { target: { value: "ci-bot" } })
    fireEvent.click(screen.getByRole("button", { name: "Servis hesabı oluştur" }))

    expect(await screen.findByText("bazusop_sat_newid_newsecret")).toBeInTheDocument()
    const createCall = fetchMock.mock.calls.find((call) => String(call[0]).endsWith(listURL) && (call[1] as RequestInit)?.method === "POST")
    expect(createCall).toBeDefined()
    const headers = new Headers((createCall?.[1] as RequestInit).headers)
    expect(headers.get("X-CSRF-Token")).toBe("csrf-token-abc")
  })

  it("rotates a token and reveals the new one-time token with a CSRF header", async () => {
    const fetchMock = vi.fn().mockImplementation((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.endsWith("/api/v1/session")) return Promise.resolve({ ok: true, json: async () => authenticatedWhoAmI } as Response)
      if (url.endsWith(listURL)) return Promise.resolve({ ok: true, json: async () => ({ service_accounts: [baseAccounts[0]] }) } as Response)
      if (url.endsWith("/acct-1/rotate") && init?.method === "POST") return Promise.resolve({ ok: true, status: 201, json: async () => ({ token: "bazusop_sat_rotatedid_rotatedsecret" }) } as Response)
      return Promise.resolve({ ok: false } as Response)
    })
    vi.stubGlobal("fetch", fetchMock)

    render(
      <SessionProvider>
        <SettingsServiceAccountsTab />
      </SessionProvider>,
    )
    await screen.findByRole("table", { name: "Servis hesapları" })

    fireEvent.click(screen.getByRole("button", { name: "Token yenile" }))

    expect(await screen.findByText("bazusop_sat_rotatedid_rotatedsecret")).toBeInTheDocument()
    const rotateCall = fetchMock.mock.calls.find((call) => String(call[0]).endsWith("/acct-1/rotate"))
    expect(rotateCall).toBeDefined()
    const headers = new Headers((rotateCall?.[1] as RequestInit).headers)
    expect(headers.get("X-CSRF-Token")).toBe("csrf-token-abc")
  })

  it("revokes the active token with a CSRF header and disables the revoke button once none remains", async () => {
    let listCallCount = 0
    const fetchMock = vi.fn().mockImplementation((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.endsWith("/api/v1/session")) return Promise.resolve({ ok: true, json: async () => authenticatedWhoAmI } as Response)
      if (url.endsWith(listURL)) {
        listCallCount += 1
        const account = listCallCount === 1 ? baseAccounts[0] : { ...baseAccounts[0], active_token: null }
        return Promise.resolve({ ok: true, json: async () => ({ service_accounts: [account] }) } as Response)
      }
      if (url.endsWith("/service-accounts/tokens/token-1") && init?.method === "DELETE") return Promise.resolve({ ok: true, status: 204 } as Response)
      return Promise.resolve({ ok: false } as Response)
    })
    vi.stubGlobal("fetch", fetchMock)

    render(
      <SessionProvider>
        <SettingsServiceAccountsTab />
      </SessionProvider>,
    )
    await screen.findByRole("table", { name: "Servis hesapları" })

    fireEvent.click(screen.getByRole("button", { name: "Token iptal et" }))

    expect(await screen.findByText("ci-bot token'ı iptal edildi.")).toBeInTheDocument()
    await waitFor(() => expect(screen.getByRole("button", { name: "Token iptal et" })).toBeDisabled())
    const revokeCall = fetchMock.mock.calls.find((call) => String(call[0]).endsWith("/service-accounts/tokens/token-1"))
    expect(revokeCall).toBeDefined()
    const headers = new Headers((revokeCall?.[1] as RequestInit).headers)
    expect(headers.get("X-CSRF-Token")).toBe("csrf-token-abc")
  })

  it("disables a service account with a CSRF header and shows the disabled status", async () => {
    let listCallCount = 0
    const fetchMock = vi.fn().mockImplementation((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.endsWith("/api/v1/session")) return Promise.resolve({ ok: true, json: async () => authenticatedWhoAmI } as Response)
      if (url.endsWith(listURL)) {
        listCallCount += 1
        const account = listCallCount === 1 ? baseAccounts[0] : { ...baseAccounts[0], disabled_at: "2026-09-23T12:00:00Z", active_token: null }
        return Promise.resolve({ ok: true, json: async () => ({ service_accounts: [account] }) } as Response)
      }
      if (url.endsWith("/service-accounts/acct-1") && init?.method === "DELETE") return Promise.resolve({ ok: true, status: 204 } as Response)
      return Promise.resolve({ ok: false } as Response)
    })
    vi.stubGlobal("fetch", fetchMock)

    render(
      <SessionProvider>
        <SettingsServiceAccountsTab />
      </SessionProvider>,
    )
    const table = await screen.findByRole("table", { name: "Servis hesapları" })
    expect(within(table).getByText("Aktif")).toBeInTheDocument()

    fireEvent.click(screen.getByRole("button", { name: "Devre dışı bırak" }))

    expect(await screen.findByText("ci-bot devre dışı bırakıldı.")).toBeInTheDocument()
    await waitFor(() => expect(within(table).getByText("Devre dışı")).toBeInTheDocument())
    const disableCall = fetchMock.mock.calls.find((call) => String(call[0]).endsWith("/service-accounts/acct-1") && (call[1] as RequestInit)?.method === "DELETE")
    expect(disableCall).toBeDefined()
    const headers = new Headers((disableCall?.[1] as RequestInit).headers)
    expect(headers.get("X-CSRF-Token")).toBe("csrf-token-abc")
  })
})
```

(4. testte `fetchMock.mockImplementation` render'dan sonra iki kez değiştiriliyor: ilk implementasyon yalnız boş listeyi döner [ilk `useEffect` çağrısı için], ikincisi hem GET'i hem `POST`'u ayırt eder — bu, `settings-users.test.tsx`'teki `usersCallCount` sayaç desenine benzer bir alternatif; burada GET ve POST aynı URL'e gittiği için `init?.method` üzerinden ayırt ediliyor.)

- [x] **Adım 5: Testleri çalıştır**

```bash
cd /Users/gokaybaz/Documents/ChatGPT/bazusop/web
npm test -- settings-service-accounts 2>&1 | tail -60
```

Beklenen: BAŞARILI (6 test)

- [x] **Adım 6: Tip kontrolü ve tam frontend test paketini çalıştır**

```bash
cd /Users/gokaybaz/Documents/ChatGPT/bazusop/web
npm test 2>&1 | tail -50
```

Beklenen: BAŞARILI

- [x] **Adım 7: Commit**

```bash
cd /Users/gokaybaz/Documents/ChatGPT/bazusop
git add web/src/types.ts web/src/styles.css web/src/pages/settings-service-accounts.tsx web/src/pages/settings-service-accounts.test.tsx
git commit -m "feat: add the SettingsServiceAccountsTab component (list, create, rotate, revoke, disable)"
```

## Görev 4: Frontend — `SettingsPage`'e üçüncü sekmeyi ekle

**Dosyalar:**
- Değiştir: `web/src/pages/settings.tsx`, `web/src/pages/settings.test.tsx`.

- [x] **Adım 1: `web/src/pages/settings.tsx`'i değiştir**

Tüm dosyanın içeriğini şununla değiştir:

```tsx
import { useEffect, useState } from "react"

import { ThemeToggle } from "../components/theme-toggle"
import { Badge } from "../components/ui/badge"
import { Card } from "../components/ui/card"
import { formatBuildDate } from "../lib/format"
import { SettingsServiceAccountsTab } from "./settings-service-accounts"
import { SettingsUsersTab } from "./settings-users"
import type { RuntimeConfiguration } from "../types"

export function SettingsPage() {
  const [tab, setTab] = useState<"general" | "users" | "service-accounts">("general")
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
        <button aria-selected={tab === "service-accounts"} className={tab === "service-accounts" ? "settings-tab active" : "settings-tab"} onClick={() => setTab("service-accounts")} role="tab" type="button">Servis hesapları</button>
      </div>
      {tab === "general" ? <div className="settings-grid"><Card className="settings-card"><div><h2>Görünüm</h2><p>Operasyon yüzeyi için açık veya koyu temayı seçin.</p></div><ThemeToggle /></Card><Card className="settings-card"><div><h2>Hub çalışma modu</h2><p>Etkin kalıcı depolama ve zaman serisi çalışma modu.</p></div><Badge className="environment">{runtime ? storageLabel : "Yükleniyor"}</Badge></Card><Card className="settings-card retention-card"><div><h2>Saklama politikası</h2><p>TimescaleDB etkin olduğunda otomatik uygulanır.</p></div><div className="retention-values"><span>Telemetri: {runtime?.telemetry_retention_days ?? "—"} gün</span><span>Loglar: {runtime?.log_retention_days ?? "—"} gün</span></div></Card><Card className="settings-card build-card"><div><h2>Hub sürümü</h2><p>Çalışan binary'nin sürüm ve kaynak kimliği.</p></div><div className="build-values"><strong>{runtime?.version ?? "—"}</strong><span>{runtime?.commit ?? "Yükleniyor"}</span><time dateTime={runtime?.build_date}>{runtime?.build_date ? formatBuildDate(runtime.build_date) : "—"}</time></div></Card></div> : tab === "users" ? <SettingsUsersTab /> : <SettingsServiceAccountsTab />}
    </div>
  )
}
```

- [x] **Adım 2: `web/src/pages/settings.test.tsx`'e yeni bir test ekle**

Mevcut tek `it` bloğundan sonra, `describe` bloğunun içine ekle:

```tsx
  it("switches to the service-accounts tab on click", async () => {
    vi.stubGlobal("fetch", vi.fn().mockImplementation((input: RequestInfo | URL) => {
      const url = String(input)
      if (url.endsWith("/api/v1/session")) return Promise.resolve({ ok: true, json: async () => authenticatedWhoAmI } as Response)
      if (url.endsWith("/api/v1/system/configuration")) return Promise.resolve({ ok: true, json: async () => ({ storage: "memory", timescale_enabled: false, telemetry_retention_days: 30, log_retention_days: 14, version: "0.5.0", commit: "abc123", build_date: "2026-09-23T00:00:00Z" }) } as Response)
      if (url.endsWith("/api/v1/sites/site_default/service-accounts")) return Promise.resolve({ ok: true, json: async () => ({ service_accounts: [] }) } as Response)
      return Promise.resolve({ ok: false } as Response)
    }))

    render(
      <SessionProvider>
        <SettingsPage />
      </SessionProvider>,
    )

    expect(await screen.findByText("Görünüm")).toBeInTheDocument()

    fireEvent.click(screen.getByRole("tab", { name: "Servis hesapları" }))

    expect(await screen.findByText("Servis hesabı oluştur")).toBeInTheDocument()
    expect(screen.queryByText("Görünüm")).not.toBeInTheDocument()
    await waitFor(() => expect(screen.getByText("Henüz servis hesabı yok.")).toBeInTheDocument())
  })
```

- [x] **Adım 3: Testleri çalıştır**

```bash
cd /Users/gokaybaz/Documents/ChatGPT/bazusop/web
npm test -- settings.test 2>&1 | tail -40
```

Beklenen: BAŞARILI (2 test)

- [x] **Adım 4: Tip kontrolü + production build**

```bash
cd /Users/gokaybaz/Documents/ChatGPT/bazusop/web
npm run build 2>&1 | tail -30
```

Beklenen: BAŞARILI

- [x] **Adım 5: Commit**

```bash
cd /Users/gokaybaz/Documents/ChatGPT/bazusop
git add web/src/pages/settings.tsx web/src/pages/settings.test.tsx
git commit -m "feat: add a Servis hesapları tab to Settings, wired to SettingsServiceAccountsTab"
```

## Görev 5: Docker Compose ile uçtan uca manuel doğrulama

**Dosyalar:** yok (yalnız doğrulama).

- [x] **Adım 1: Docker Compose ile hub'ı yeniden derleyip ayağa kaldır**

Çalıştır: `cd /Users/gokaybaz/Documents/ChatGPT/bazusop && docker compose down -v >/dev/null 2>&1; BAZUSOP_PORT=8090 BAZUSOP_BOOTSTRAP_SECRET=verify-bootstrap BAZUSOP_TOTP_ENCRYPTION_KEY=verify-totp-key BAZUSOP_SERVICE_ACCOUNT_PEPPER=verify-pepper docker compose up --build -d 2>&1 | tail -30`

- [x] **Adım 2: Sağlık kontrolünü bekle**

Çalıştır: `for i in $(seq 1 20); do curl -s -o /dev/null -w "%{http_code}" http://127.0.0.1:8090/api/v1/health | grep -q 200 && echo healthy && break; sleep 1; done`

- [x] **Adım 3: Tek bir Python betiğiyle bootstrap → giriş → servis hesabı oluştur → listele → rotate → listele → revoke → listele → disable → listele — hepsi gerçek bir HTTP istemcisiyle**

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

accounts_before = call('GET', '/api/v1/sites/site_default/service-accounts')
print('accounts before create:', len(accounts_before['service_accounts']))

created = call('POST', '/api/v1/sites/site_default/service-accounts', {'name': 'ci-bot', 'role': 'operator', 'expiry_days': 30}, {'X-CSRF-Token': csrf})
account_id = created['id']
first_token = created['token']
print('created account:', created['name'], created['role'])

accounts_after_create = call('GET', '/api/v1/sites/site_default/service-accounts')
entry = next(a for a in accounts_after_create['service_accounts'] if a['id'] == account_id)
print('active token after create:', entry['active_token'] is not None)

rotated = call('POST', f'/api/v1/service-accounts/{account_id}/rotate', {'expiry_days': 30}, {'X-CSRF-Token': csrf})
new_token = rotated['token']
print('rotated token differs from first:', new_token != first_token)

accounts_after_rotate = call('GET', '/api/v1/sites/site_default/service-accounts')
entry = next(a for a in accounts_after_rotate['service_accounts'] if a['id'] == account_id)
active_token_id = entry['active_token']['id']
print('active token id present after rotate:', active_token_id is not None)

call('DELETE', f'/api/v1/service-accounts/tokens/{active_token_id}', None, {'X-CSRF-Token': csrf})
accounts_after_revoke = call('GET', '/api/v1/sites/site_default/service-accounts')
entry = next(a for a in accounts_after_revoke['service_accounts'] if a['id'] == account_id)
print('active token after revoke:', entry['active_token'])

call('DELETE', f'/api/v1/service-accounts/{account_id}', None, {'X-CSRF-Token': csrf})
accounts_after_disable = call('GET', '/api/v1/sites/site_default/service-accounts')
entry = next(a for a in accounts_after_disable['service_accounts'] if a['id'] == account_id)
print('disabled_at set after disable:', entry['disabled_at'] is not None)
"
```

Beklenen çıktı sırasıyla: "accounts before create: 0", "created account: ci-bot operator", "active token after create: True", "rotated token differs from first: True", "active token id present after rotate: True", "active token after revoke: None", "disabled_at set after disable: True".

- [x] **Adım 4: Temizlik**

```bash
cd /Users/gokaybaz/Documents/ChatGPT/bazusop
docker compose down -v
```

- [x] **Adım 5: Go ve frontend testlerini son kez birlikte çalıştır (gerçek Postgres ile)**

```bash
docker rm -f bazusop-test-pg-1350b >/dev/null 2>&1
docker run -d --name bazusop-test-pg-1350b -e POSTGRES_USER=bazusop -e POSTGRES_PASSWORD=bazusop_test -e POSTGRES_DB=bazusop_test -p 5432:5432 postgres:18 >/dev/null
for i in $(seq 1 30); do docker exec bazusop-test-pg-1350b pg_isready -U bazusop -d bazusop_test >/dev/null 2>&1 && break; sleep 1; done
cd /Users/gokaybaz/Documents/ChatGPT/bazusop
go build ./... && go vet ./... && gofmt -l .
BAZUSOP_TEST_DATABASE_URL='postgres://bazusop:bazusop_test@127.0.0.1:5432/bazusop_test?sslmode=disable' GOCACHE=/tmp/bazusop-go-cache go test ./... -count=1 2>&1 | tail -30
docker rm -f bazusop-test-pg-1350b >/dev/null 2>&1
cd web && npm test 2>&1 | tail -50 && npm run build 2>&1 | tail -20
```

Beklenen: hepsi BAŞARILI

- [x] **Adım 6: `docker-compose.yml`'daki spike tamamlama notunu güncelle (varsa) ve son bir commit at**

Önceki spike'ların (13.2, 13.3, 13.4) her biri, frontend bundle'ını embed edip planı "tamamlandı" işaretleyen tek bir "build:" commit'iyle kapanmıştı (`fc2779f`, `60926b0`, `31a1192`). Aynı deseni izle:

```bash
cd /Users/gokaybaz/Documents/ChatGPT/bazusop
find . -name "*.md" -newer docs/superpowers/plans/2026-09-23-service-account-management-ui.md 2>/dev/null
```

(Bu adımın gerçek gövdesi, yürütme sırasında 13.4'ün son "build:" commit'inin tam olarak neyi embed ettiğine bakılarak — muhtemelen `internal/server/assets` veya benzeri bir gömülü bundle dizini — belirlenir; yürütücü önce `git show 31a1192 --stat` çalıştırıp aynı adımları burada tekrarlamalı.)

```bash
cd /Users/gokaybaz/Documents/ChatGPT/bazusop
git add -A
git commit -m "build: embed the service-account-management frontend bundle; mark spike 13.5 plan complete"
```

## Kendi Kendine İnceleme

**1. Spec kapsaması.** Spike 13.5'in kabul sinyali ("Site-admin tarayıcıdan servis hesabı oluşturur (token bir kez görünür), rotate/revoke/disable eder") → Görev 3'ün altı bileşen testi (liste+rozetler, yetkisiz durum, oluştur+reveal, rotate+reveal, revoke+buton devre dışı kalması, disable+durum rozeti) ve Görev 5'in Adım 3'ü (gerçek HTTP istemcisiyle, her adımdan sonra listeyi tekrar çekip `active_token`/`disabled_at` değişikliğini doğrulayarak) bunu kanıtlıyor. Spec'in "Sayfa ve bileşen envanteri" tablosundaki 13.5 satırı ("Servis hesabı listesi, oluşturma formu, tek-seferlik token gösterimi, rotate/revoke/disable aksiyonları") Görev 3'te birebir uygulandı.

**2. Placeholder taraması.** Her adımda gerçek, eksiksiz kod var — tek istisna Görev 5 Adım 6'daki commit adımı, çünkü 13.4'ün tam olarak hangi dosyaları embed ettiği yürütme zamanında `git show` ile doğrulanması gereken bir detay (önceki üç spike'ın da aynı belirsizlikle kapandığı, kod tabanının kendi build sürecine bağlı bir adım — bu YAGNI/placeholder değil, gerçek bir "yürütme zamanı keşfi" notu).

**3. Tip tutarlılığı.** `AccountSummary`'nin `ServiceAccount`'ı gömmesi kritik bir tasarım kararı: Görev 1'in yeni testi `afterCreate[0].ActiveToken`, mevcut `TestListForSiteReturnsOnlyThatSitesAccounts` testi ise `accounts[0].Name` kullanıyor — ikisi de aynı gömülü yapı sayesinde derlenir, ayrı bir dönüştürme katmanı gerekmez. Görev 2'nin HTTP handler'ı `summary.ID`/`summary.Name`/`summary.Role`/`summary.CreatedAt`/`summary.DisabledAt` alanlarına doğrudan erişiyor (gömme sayesinde), `summary.ActiveToken`'a da aynı şekilde — Görev 1'de tanımlanan alan adlarıyla birebir eşleşiyor. Frontend'deki `ServiceAccount`/`ServiceAccountToken` tipleri (Görev 3 Adım 1), Görev 2'nin JSON çıktısındaki `id/name/role/created_at/disabled_at/active_token{id,expires_at,last_used_at}` alan adlarıyla birebir örtüşüyor.
