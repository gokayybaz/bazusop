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
- Docker Compose ve Helm chart'ı artık `BAZUSOP_BOOTSTRAP_SECRET`,
  `BAZUSOP_TOTP_ENCRYPTION_KEY` ve `BAZUSOP_SERVICE_ACCOUNT_PEPPER`
  değişkenlerini/secret anahtarlarını tanır; Helm chart'ında bunlar sırasıyla
  `bootstrap-secret`, `totp-encryption-key`, `service-account-pepper` Secret
  anahtarlarıdır (eski `operator-token`/`admin-token` anahtarlarının yerini
  aldı).

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

## Docker Compose / Helm yapılandırma değişikliği

`compose.yaml`'da `BAZUSOP_OPERATOR_TOKEN`/`BAZUSOP_ADMIN_TOKEN` satırları
kaldırıldı; yerine `BAZUSOP_BOOTSTRAP_SECRET`, `BAZUSOP_TOTP_ENCRYPTION_KEY`
ve `BAZUSOP_SERVICE_ACCOUNT_PEPPER` eklendi (üçü de varsayılan olarak boş —
tanımlamazsanız hub açılır ama hiçbir mutasyon kimlik doğrulanamaz).

Helm chart'ının beklediği `bazusop-secrets` Secret'ındaki `operator-token` ve
`admin-token` anahtarları artık kullanılmıyor; yerine isteğe bağlı
`bootstrap-secret`, `totp-encryption-key`, `service-account-pepper`
anahtarları eklendi (bkz.
[deploy/helm/bazusop/README.md](../deploy/helm/bazusop/README.md)).
