# Frontend kimlik doğrulama ve RBAC yönetim arayüzü tasarımı

**Tarih:** 2026-09-22
**Durum:** Uygulama planı öncesi onaylı tasarım
**Hedef sürüm ailesi:** `v0.5.x`

## Amaç

11.1–11.10 spike serisi bazUSOP'a tam bir kurumsal kimlik/RBAC/audit backend'i
kazandırdı (yerel kimlik, MFA, opsiyonel OIDC, sunucu taraflı oturumlar, CSRF,
sabit RBAC, servis hesabı token'ları, audit/activity, brute-force koruması).
Ancak `web/` altındaki React arayüzü bu çalışmadan tamamen bağımsız kaldı:
hiçbir login/bootstrap ekranı yok, oturum/CSRF kavramı arayüzde hiç
kullanılmıyor, ve İşler/Alarmlar sayfalarındaki formlar 11.7'de kaldırılan eski
operator/admin bearer köprüsünü hâlâ varsayıyor — yani bu formlar şu an gerçek
backend'e karşı çalışmıyor.

Bu tasarım, orijinal [kurumsal kimlik/RBAC/audit tasarımı](2026-09-13-enterprise-rbac-identity-audit-design.md)'nın
"UI yüzeyleri" bölümünde listelenip 11.x serisinde bilinçli olarak ertelenen
arayüz çalışmasını tamamlar: gerçek bir tarayıcıdan giriş yapılabilen, oturum
ve CSRF farkında, RBAC'a göre kendini gösteren/gizleyen bir web arayüzü.

## Kapsam

Bu tasarım şunları kapsar:

- Oturum durumu için merkezi bir React context (`SessionProvider`) ve
  CSRF-farkında merkezi bir `apiFetch` yardımcı fonksiyonu.
- `react-router-dom` ile gerçek route tabanlı gezinme (mevcut pathname-switch
  deseninin yerini alır).
- Login ekranı (parola + TOTP kodu veya recovery code).
- İlk kurulum (bootstrap) ekranı.
- Davet tüketimi + TOTP kurulum akışı (`/invite/:token`) — bu akışın gerçek
  bir tarayıcı istemcisiyle hiç çalışmadığı bu tasarım sürecinde keşfedildi
  (bkz. "Backend değişiklikleri" bölümü) ve küçük bir backend düzeltmesi bu
  kapsama dahildir.
- İşler ve Alarmlar sayfalarındaki, 11.7'de kaldırılan eski bearer köprüsüne
  dayanan bozuk formların oturum+CSRF tabanlı hale getirilmesi.
- Kullanıcı/davet/site rolü yönetim arayüzü (+ yeni `GET /api/v1/users`
  backend ucu).
- Servis hesabı yönetim arayüzü.
- OIDC yapılandırma ve oturum yönetimi (görüntüleme/iptal) arayüzü.

İlk sürümde kapsam dışı olanlar:

- Yerel `vite dev` sunucusu için ayrı bir API proxy kurulumu — arayüz her
  zaman gömülü olarak hub'dan (aynı origin) servis edilir ve test/geliştirme
  Docker Compose ile yapılır; bu, 11.x serisinin tamamında izlenen kısıttır.
- Kullanıcı tarafından tanımlanan özel roller veya izin matrisi editörü —
  backend zaten sabit rolleri kullanıyor, arayüz onu yansıtır.
- Çoklu organizasyon/tenant seçici arayüzü — backend tek organizasyon
  varsayımını sürdürüyor (bkz. orijinal tasarımın kapsam dışı listesi).
- Mobil-öncelikli veya native uygulama — yalnız responsive web.

## Mimari yaklaşım

Mevcut frontend kasıtlı olarak sade: router yok, state kütüphanesi yok,
merkezi API client yok, her sayfa kendi `fetch`'ini yapıyor. Bu tasarım o
sadeliği korur, yalnız oturum/routing için gereken minimum altyapıyı ekler:

- **`react-router-dom`** yeni bağımlılık olarak eklenir (kullanıcı onayıyla —
  başlangıçta pathname-switch deseni önerilmişti, kullanıcı gerçek router
  istedi). `app.tsx` bir `<BrowserRouter>` içinde route tanımlarına böünür;
  `navigation.ts`'deki `path` alanları artık `<NavLink>` hedefleri olarak
  kullanılır.
- **`SessionProvider`** (`web/src/lib/session.tsx`) — mount olduğunda
  `GET /api/v1/session` çağırır, sonucu context'te tutar (`user | null`,
  `status: "loading" | "authenticated" | "anonymous"`, `csrfToken`). `login()`,
  `logout()` ve `refresh()` metodları dışarı verir.
- **`apiFetch`** (`web/src/lib/api.ts`) — `fetch`'i sarar: GET/HEAD dışı her
  istekte `SessionProvider`'dan aldığı CSRF token'ı `X-CSRF-Token` header'ı
  olarak ekler; her yanıtta `401` görürse `SessionProvider`'ın oturumu
  temizleyip login'e yönlendirmesini tetikleyen bir callback çağırır. Tüm
  mevcut ve yeni mutasyon çağrıları bu yardımcıya taşınır — 11.10'un CSRF
  dersiyle aynı: kontrolü sayfalara dağıtmak yerine tek yerde topla.
- **`ProtectedRoute`** (`web/src/lib/protected-route.tsx`) — `SessionProvider`
  durumu `"anonymous"` ise `/login`'e yönlendiren basit bir route sarmalayıcı.
  Mevcut sayfaların hiçbiri (Genel bakış, Filo, Servisler, Metrikler, Loglar,
  İşler, Alarmlar, Bulut, Aktivite, Denetim izi, Ayarlar) bu sarmalayıcı
  olmadan render edilmez.
- Router dışında yeni bir bağımlılık **eklenmez** — form state için mevcut
  `useState`/`useEffect` deseni, stil için mevcut Tailwind + `class-variance-authority`
  deseni korunur.

Backend tarafında **hiçbir mevcut auth davranışı değişmez** — bu tasarım
yalnız frontend'i var olan uçlara doğru bağlar, artı iki küçük backend
eklentisi (aşağıda).

## Backend değişiklikleri

İki küçük, izole backend değişikliği bu kapsama dahildir; ikisi de yeni
davranış eklemez, yalnız zaten var olan/amaçlanan davranışı tamamlar:

### 1. Davet/TOTP onboarding'in gerçek istemciyle çalışır hale getirilmesi (13.2)

Şu an `internal/identity.Service.ConsumeInvite` hiç TOTP secret'ı
üretmiyor; `ConfirmTOTP` her çağrıda **yeni** bir secret üretip kullanıcının
gönderdiği kodu ona karşı doğruluyor. Kullanıcının bu secret'ı görecek hiçbir
yolu olmadığından, gerçek bir tarayıcı istemcisi bu akışı asla
tamamlayamaz — mevcut testler yalnız Go seviyesinde, secret'ı doğrudan okuyan
bir test callback'iyle geçiyor. Bootstrap akışı ise doğru: secret'ı önceden
üretip `provisioning_uri` olarak döndürüyor.

Düzeltme, `ConsumeInvite`'ı bootstrap'in izlediği desene getirir:

- `ConsumeInvite` artık bir TOTP secret'ı üretir, şifreleyip kullanıcıyla
  birlikte kalıcılaştırır (`TOTPConfirmedAt` boş kalır — onaylanmamış) ve
  dönüş değerine `TOTPEnrollment{ProvisioningURI: ...}` ekler (imza:
  `ConsumeInvite(ctx, token, password) (User, TOTPEnrollment, error)`).
- `ConfirmTOTP` artık secret üretmez; yalnız zaten kalıcılaşmış secret'ı okur
  ve doğrular (mevcut `hasSecret` dalı zaten bunu destekliyor — `else` dalı,
  yani "kullanıcı hiç secret'a sahip değilse yeni üret" dalı kaldırılır, çünkü
  bu noktadan sonra her kullanıcının bir secret'ı olması garanti edilir).
- HTTP katmanında `handleConsumeInvite` yanıtına `provisioning_uri` eklenir;
  `handleConfirmTOTP` yanıtına da (halihazırda üretilen ama hiç dönülmeyen)
  `provisioning_uri` eklenir — istemci QR'ı ister consume adımında ister
  confirm adımında (yeniden) gösterebilir.

### 2. Kullanıcı listeleme ucu (13.4)

Backend'de kullanıcıları listeleyen bir uç hiç yok (yalnız davet oluşturma ve
bilinen ID ile site rolü atama/kaldırma var). Yönetim arayüzü olmadan bu
sorun görünmüyordu; şimdi arayüz "hangi kullanıcılara rol ataması yapılabilir"
sorusuna cevap vermek zorunda. Yeni uç: `GET /api/v1/users` —
`PermissionManageUsers` (organizasyon kapsamlı, yalnız platform yöneticisi)
gerektirir, her kullanıcı için `id, email, role, disabled_at, totp_confirmed_at`
ve o kullanıcının site rolleri listesini döndürür. Salt okunur, mutasyon
yok — mevcut davet/rol atama uçlarını değiştirmez.

## Sayfa ve bileşen envanteri

| Spike | Yeni/değişen sayfa | Yeni/değişen bileşen |
| --- | --- | --- |
| 13.1 | `/login` (LoginPage) | `SessionProvider`, `apiFetch`, `ProtectedRoute`, topbar kullanıcı menüsü (gerçek e-posta/rol + çıkış) |
| 13.2 | `/setup` (BootstrapPage), `/invite/:token` (InvitePage) | Ortak TOTP QR gösterimi bileşeni, ortak recovery code listesi bileşeni (ikisi de hem bootstrap hem davet akışında kullanılır) |
| 13.3 | İşler (JobPanel), Alarmlar (AlarmCenter) | — (mevcut sayfalar içi değişiklik: token-yapıştırma alanları kaldırılır) |
| 13.4 | Ayarlar altına "Kullanıcılar" sekmesi | Kullanıcı listesi tablosu, davet formu, site rolü atama/kaldırma formu |
| 13.5 | Ayarlar altına "Servis hesapları" sekmesi | Servis hesabı listesi, oluşturma formu, tek-seferlik token gösterimi, rotate/revoke/disable aksiyonları |
| 13.6 | Ayarlar altına "Güvenlik" sekmesi | OIDC yapılandırma formu, oturum listesi + iptal aksiyonu |

## Hata ve güvenli kapanma davranışı

- `apiFetch` her `401` yanıtında oturumu temizler ve kullanıcıyı `/login`'e
  yönlendirir — form doldurma sırasında oturum süresi dolarsa kullanıcı sessiz
  bir hata yerine login ekranına düşer (girilen form verisi kaybolabilir, bu
  kabul edilebilir — backend zaten mutasyonu reddetmiş olur).
- CSRF token uyuşmazlığı (`403`) genel bir hata mesajıyla gösterilir
  ("İşlem doğrulanamadı, sayfayı yenileyin") — CSRF'in kendisi kullanıcıya asla
  açıklanmaz (backend zaten aynı ilkeyi izliyor, bkz. 11.10).
- Token, recovery code ve bootstrap secret'ları yalnız bir kez gösterilen
  ekranlarda "kopyala" aksiyonu ve "bu değer tekrar gösterilmeyecek" uyarısı
  bulunur; hiçbiri `localStorage`/`sessionStorage`'a veya herhangi bir
  telemetriye yazılmaz (orijinal tasarımın aynı ilkesi).
- Yetkisiz bir sayfa/aksiyon (RBAC izin yetersizliği) `403` alır; arayüz bunu
  gizlemek yerine (mevcut Denetim izi sayfasındaki desenle tutarlı) açık bir
  "yetkiniz yok" durumuyla gösterir — UI görünürlüğü asla tek güvenlik katmanı
  değildir, her mutasyon backend'de zaten yeniden doğrulanır.
- Bootstrap ekranı, hub zaten kurulmuşsa (`POST /api/v1/bootstrap` → `409`)
  kullanıcıyı "zaten kurulmuş, giriş yapın" mesajıyla `/login`'e yönlendirir —
  "kurulu mu" diye ayrı bir keşif ucu **eklenmez** (YAGNI); mevcut `409`
  sözleşmesi yeterli.

## Teslimat stratejisi

Her spike kendi başına test edilebilir, çalışan bir dikey dilimdir — önce
oturum/erişim temeli, sonra onboarding, sonra bozuk mevcut formların
düzeltilmesi, en son yönetim yüzeyleri (kullanıcı önceliği: önce
giriş+kırık formlar, yönetim arayüzleri sonra).

| Spike | Çıktı | Kabul sinyali |
| --- | --- | --- |
| 13.1 | Router + oturum kabuğu (`SessionProvider`, `apiFetch`, `ProtectedRoute`, login ekranı) | Oturumsuz erişimde her sayfa `/login`'e yönlenir; doğru kimlik bilgisiyle giriş dashboard'u açar; topbar gerçek kullanıcıyı gösterir |
| 13.2 | Backend düzeltmesi + bootstrap/davet/TOTP onboarding ekranları | Sıfırdan bir hub'da tarayıcıdan bootstrap tamamlanır; davetli kullanıcı tarayıcıdan QR'ı okutup TOTP kodunu girerek onboarding'i bitirir |
| 13.3 | İşler/Alarmlar formları oturum+CSRF ile çalışır | Giriş yapmış bir operatör/site-admin tarayıcıdan iş oluşturur, alarm kuralı/bakım penceresi ekler, olay onaylar — eski token alanı yok |
| 13.4 | Kullanıcı/davet/rol yönetimi arayüzü + `GET /api/v1/users` | Platform yöneticisi tarayıcıdan davet oluşturur, kullanıcı listesini görür, site rolü atar/kaldırır |
| 13.5 | Servis hesabı yönetimi arayüzü | Site-admin tarayıcıdan servis hesabı oluşturur (token bir kez görünür), rotate/revoke/disable eder |
| 13.6 | OIDC yapılandırma + oturum yönetimi arayüzü | Platform yöneticisi tarayıcıdan OIDC ayarlarını günceller (secret asla geri dönmez), kendi/başka bir kullanıcının oturumunu iptal eder |

## Test stratejisi

Zorunlu otomatik test sınıfları (Vitest + Testing Library, mevcut
`web/src/app.test.tsx` desenine uygun, mock edilmiş `fetch` ile):

- `SessionProvider`: whoami başarılı/401/ağ hatası durumlarında doğru state.
- `apiFetch`: CSRF header'ının GET'te eklenmediği, POST/PUT/DELETE'te
  eklendiği; `401` üzerine oturumun temizlendiği.
- `ProtectedRoute`: oturumsuzken login'e yönlendirme, oturumluyken içeriği
  gösterme.
- Login formu: başarılı giriş, yanlış kimlik bilgisi, TOTP/recovery code
  seçim arayüzü.
- Bootstrap formu: başarılı kurulum, "zaten kurulmuş" (409) yönlendirmesi.
- Davet/TOTP onboarding: parola belirleme → QR gösterimi → kod doğrulama →
  recovery code gösterimi uçtan uca (mock fetch zinciriyle).
- İşler/Alarmlar formları: eski token alanının kaldırıldığı, yeni
  mutasyonların `apiFetch` üzerinden CSRF header'ıyla gittiği.
- Kullanıcı/rol, servis hesabı, OIDC yönetim formları: `403`/`401`
  durumlarında doğru boş/yetkisiz durumu.
- `npm run build` (tip kontrolü + production build) her spike'ın son adımı.

Backend tarafı değişiklikleri (13.2'nin `ConsumeInvite`/`ConfirmTOTP`
düzeltmesi, 13.4'ün `GET /api/v1/users`'ı) mevcut Go test disiplinine tabidir:
`internal/identity` ve `internal/server` paketlerinde gerçek HTTP istemcisi
simülasyonuyla (artık test callback'i secret'ı "çalmaz", gerçek yanıttan
okur) uçtan uca doğrulanır.

Tarayıcı-seviyesi manuel doğrulama (Docker Compose ile) her spike'ın son
adımı olarak, 11.x serisinde izlenen aynı disiplinle yapılır.

## Başarı ölçütleri

- Tarayıcıdan hiçbir `curl`/API aracı kullanmadan: hub'ı sıfırdan kur, admin
  ile giriş yap, bir kullanıcı davet et, o kullanıcı davetini kabul edip
  TOTP kursun, bir servis hesabı oluştur, bir iş çalıştır, bir alarm kuralı
  ekle — hepsi mümkün olur.
- İşler/Alarmlar formlarında eski "yetkili token" alanı kalmaz.
- Her mutasyon isteği CSRF header'ı taşır; her sayfa oturum yoksa/sona
  ermişse login'e yönlenir.
- `npm run build` ve `npm test` her spike sonunda temiz geçer; Go test
  paketleri (`go test ./...`) etkilenen paketlerde temiz geçer.
