# Kurumsal kimlik, site kapsamlı RBAC ve denetim tasarımı

**Tarih:** 2026-09-13
**Durum:** Uygulama planı öncesi onaylı tasarım
**Hedef sürüm ailesi:** `v0.4.x`

## Amaç

bazUSOP'u tek organizasyona ait, çok sayıda siteyi yöneten kurumsal bir
operasyon platformuna dönüştürmek. Tasarım; yerel kimlik ve zorunlu MFA,
opsiyonel OIDC, site kapsamlı sabit roller, süreli servis hesabı token'ları,
eksiksiz audit ve kullanılabilir activity akışını birlikte ele alır.

Bu çalışma mevcut agent mTLS güven modelini değiştirmez. İnsan ve otomasyon
kimliklerini agent kimliğinden ayırır; tüm kaynak erişimlerini organizasyon ve
site kapsamına bağlar.

## Kapsam

Bu tasarım şunları kapsar:

- Tek organizasyon, çoklu site veri modeli.
- Yerel kullanıcılar, davetler, parola ve TOTP MFA.
- Opsiyonel OIDC ile davetli kullanıcı bağlama.
- Sunucu taraflı web oturumları ve CSRF koruması.
- Sabit, site kapsamlı RBAC.
- Süreli ve döndürülebilir servis hesabı token'ları.
- Append-only audit ve kullanıcı odaklı activity akışları.
- Mevcut kaynakların varsayılan siteye taşındığı migration.
- Web UI ve API yüzeylerinin yeni güvenlik modeline geçirilmesi.

İlk sürümde kapsam dışı olanlar:

- SAML 2.0.
- IdP grup claim'lerinden otomatik rol atama.
- Kullanıcı tarafından tanımlanan özel roller.
- Birden fazla organizasyon veya tenant.
- Servis hesapları için platform-geneli yetki.
- Ayrı bir kimlik mikroservisi.

## Mimari yaklaşım

Hub mevcut modular monolith yapısını korur. Yeni davranışlar beş dahili alan
modülünde toplanır:

- `tenancy`: organizasyon, site ve kaynak-site ilişkileri.
- `identity`: yerel kullanıcı, davet, parola, TOTP ve recovery code yaşam
  döngüsü.
- `sessions`: web oturumu, süre aşımı, rotasyon ve iptal.
- `authorization`: actor bağlamı, sabit izin kataloğu ve site kapsamı.
- `oidc`: opsiyonel sağlayıcı yapılandırması ve OIDC kimlik bağlama.

Audit ve activity, bu modüllerin ortak altyapısıdır. Güvenlik-kritik bir
mutasyon, kendi durum değişikliği ile audit/activity olayını aynı veritabanı
transaction'ında yazar. Audit yazılamıyorsa mutasyon commit edilmez.

Üretimde kimlik, oturum ve yetki durumu PostgreSQL gerektirir. Süreç içi memory
store yalnız yerel geliştirme ve test içindir; restart sonrasında oturum veya
kimlik sürekliliği vaat etmez.

## Ortak istek bağlamı

Kimliği doğrulanan her istek tek bir ortak bağlama dönüştürülür:

```text
Actor
├── human: user_id + session_id
├── service: service_account_id + token_id
└── agent: agent_id + certificate identity

Scope
├── organization_id
└── site_ids
```

Handler, istemciden gönderilen actor veya site beyanına güvenmez. Actor doğrulanmış
cookie, bearer token ya da mTLS sertifikasından; site ise erişilen kaynağın
kalıcı ilişkisinden çıkarılır.

Her istek girişinde sunucu tarafından benzersiz bir `correlation_id` üretilir.
İstemciden gelen request ID yalnız ayrı ve doğrulanmış bir metadata alanı olarak
tutulabilir; sunucu kimliğinin yerine geçmez. Sunucu kimliği request log, audit,
activity ve iş olaylarını ilişkilendirir.

## Organizasyon ve site modeli

Kurulum başına tam olarak bir organizasyon bulunur. Organizasyonun bir veya daha
fazla sitesi vardır. Aşağıdaki kaynaklar zorunlu `organization_id` ve `site_id`
taşır:

- Agent ve host envanteri.
- Enrollment token'ları.
- Telemetri, servis ve log kayıtları.
- Operasyon işleri ve job event'leri.
- Alarm kuralları, olaylar ve bakım pencereleri.
- Bulut hesapları ve keşfedilen instance'lar.
- Site kapsamlı servis hesapları ve token'ları.
- Activity olayları.

Kullanıcı, davet, OIDC sağlayıcı yapılandırması ve organizasyon güvenlik
politikası organizasyon kapsamındadır. Kullanıcının site erişimi ayrı üyelik
kayıtlarıyla temsil edilir.

Bir agent başka siteye taşındığında yeni veriler yeni siteye yazılır. Geçmiş
telemetri, log, iş, alarm ve audit kayıtlarının site kimliği değiştirilmez. Bu,
geçmiş erişim sınırlarının geriye dönük genişlemesini engeller.

### Migration

Yükseltme sırasında transaction içinde tek organizasyon ve `Varsayılan` adlı bir
site oluşturulur. Mevcut tüm kaynaklar ve geçmiş kayıtlar bu siteye bağlanır.
Migration idempotenttir; yarım kalıp tekrar çalıştığında ikinci organizasyon veya
site üretmez.

Migration tamamlanmadan yeni site kapsamlı handler'lar trafiğe açılmaz. `NULL`
`site_id` bulunan güvenlik-kritik kayıtlar migration başarısızlığı sayılır.

## Kimlik yaşam döngüsü

### Bootstrap

Henüz organizasyon ve platform yöneticisi yoksa hub bootstrap modundadır. İlk
yerel `platform-admin`, güçlü bir ortam secret'ı ile korunan tek kullanımlık
bootstrap endpoint'i üzerinden oluşturulur. Başarılı kurulumdan sonra endpoint
kalıcı olarak kapanır ve aynı secret yeniden kullanılamaz.

Bootstrap isteği uzaktan plaintext HTTP ile kabul edilmez. Başarı ve başarısızlık
audit edilir; secret hiçbir log veya yanıta yansıtılmaz.

### Davet

Bootstrap sonrasında tüm yerel ve OIDC kullanıcılarını yalnız `platform-admin`
davet eder. Davet oluşturulurken kullanıcının organizasyonu, e-postası, kimlik
türü, sitesi veya siteleri ve her site için sabit rolü belirlenir.

Davet token'ı:

- 24 saat geçerlidir.
- Kriptografik rastgelelik kullanır.
- Yalnız oluşturulurken gösterilir.
- Veritabanında sadece hash olarak saklanır.
- Tek kullanımlıktır; başarıyla tüketildiğinde tekrar kullanılamaz.
- İptal edilebilir ve yeni davetle değiştirilebilir.

### Yerel kullanıcı

Yerel kullanıcı daveti tüketirken parola belirler ve TOTP kaydını tamamlar.
Parolalar Argon2id ile, sürümlü parametrelerle hash'lenir. Parametreler zamanla
güçlendirildiğinde başarılı giriş sırasında yeniden hash yapılabilir.

Tüm yerel kullanıcılar için TOTP zorunludur. TOTP secret'ı uygulama seviyesinde
şifrelenerek saklanır. Şifreleme anahtarı veritabanı dışında, zorunlu bir runtime
secret'ı olarak sağlanır ve key ID ile döndürülebilir. Kullanıcıya tek kullanımlık
recovery code'lar yalnız bir
kez gösterilir; veritabanında sadece hash'leri bulunur. Kullanılmış recovery code
atomik olarak tüketilir.

MFA sıfırlama normal API'de yalnız yetkili platform yöneticisi tarafından
başlatılabilir ve hedef kullanıcının mevcut oturumlarını iptal eder. Son
`platform-admin` için acil kurtarma, hub hostu ve veritabanına yönetici erişimi
gerektiren ayrı bir CLI işlemiyle yapılır. Bu işlem yüksek önem seviyesinde audit
edilir.

### OIDC kullanıcı

OIDC ilk sürümde opsiyoneldir. Platform yöneticisi discovery URL, izin verilen
issuer, client ID, client secret ve redirect URL yapılandırmasını tanımlar.
Client secret şifreli saklanır ve hiçbir okuma API'sinde geri döndürülmez.

Akış Authorization Code + PKCE kullanır. `state`, `nonce`, issuer, audience,
imza, zaman alanları ve redirect hedefi doğrulanır. İlk giriş yalnız geçerli bir
davetle gerçekleşir. Davetin doğrulanmış e-postası ilk bağlantıyı bulmak için
kullanılır; bağlantı sonrasında hesap kalıcı olarak `issuer + subject` ile
tanınır. E-posta değişikliği kimliği başka hesaba taşımaz.

OIDC kullanıcılarının MFA politikası IdP'nin sorumluluğundadır. bazUSOP, IdP'nin
sağladığı `acr` ve `amr` claim'lerini varsa audit olayına ekler; ilk sürümde bu
claim'lere dayalı ek bir giriş engeli uygulamaz.

IdP grup claim'leri ilk sürümde rol üretmez. Site üyeliği ve rol ataması yalnız
platform yöneticisinin açık işlemiyle değişir.

## Web oturumları

İnsan kullanıcılar opaque oturum kimliği taşıyan cookie ile çalışır. Yetki ve
site üyeliği istemci token'ına gömülmez; her istek sunucu taraflı güncel oturum ve
üyelik durumunu kullanır.

Cookie özellikleri:

- `HttpOnly`.
- TLS altında `Secure`.
- `SameSite=Lax`.
- Daraltılmış path ve host kapsamı.

Oturum girişte ve yetki seviyesi değiştiğinde rotate edilir. Varsayılan boşta
kalma süresi 30 dakika, mutlak yaşam süresi 8 saattir. Platform yöneticisi bir
kullanıcının veya tüm organizasyonun oturumlarını iptal edebilir. Kullanıcı
devre dışı bırakıldığında, rol/site üyeliği değiştiğinde veya MFA sıfırlandığında
ilgili mevcut oturumlar geçersiz olur.

Durum değiştiren cookie tabanlı istekler CSRF token'ı doğrular. CORS varsayılan
olarak kapalıdır ve güvenilir origin listesi açıkça yapılandırılmadıkça farklı
origin'den credential kabul edilmez.

## Sabit RBAC modeli

İzin değerlendirmesi deny-by-default çalışır. Bir eylemin başarılı olması için
actor türü, sabit rol, gerekli permission, organizasyon ve kaynak sitesi birlikte
uyuşmalıdır.

### Roller

| Yetki alanı | Platform admin | Site admin | Operator | Viewer |
| --- | --- | --- | --- | --- |
| Tüm siteleri görme ve yönetme | Evet | Hayır | Hayır | Hayır |
| Kullanıcı daveti ve rol/site ataması | Evet | Hayır | Hayır | Hayır |
| OIDC ve organizasyon güvenlik ayarları | Evet | Hayır | Hayır | Hayır |
| Atanmış site ve envanteri görme | Evet | Evet | Evet | Evet |
| Agent enrollment, taşıma ve iptal | Evet | Kendi sitesi | Hayır | Hayır |
| Alarm kuralı ve bakım penceresi yönetme | Evet | Kendi sitesi | Hayır | Hayır |
| Bulut hesabı yönetme | Evet | Kendi sitesi | Hayır | Hayır |
| Uzak operasyon işi oluşturma | Evet | Kendi sitesi | Kendi sitesi | Hayır |
| Olay onaylama | Evet | Kendi sitesi | Kendi sitesi | Hayır |
| Site activity akışını görme | Evet | Kendi sitesi | Kendi sitesi | Kendi sitesi |
| Audit olaylarını görme | Tümü | Kendi sitesi | Yalnız kendi eylemleri | Hayır |

`platform-admin` organizasyon kapsamlı insan rolüdür. Diğer roller bir veya daha
fazla site üyeliği üzerinden atanır. Bir kullanıcı farklı sitelerde farklı rol
taşıyabilir.

Kapsam dışında kalan kaynak, varlığını sızdırmamak için `404` döndürür. Kaynak
kapsam içindeyse fakat eylem izni yoksa `403` döndürülür. Her iki karar da audit
edilir.

### Servis hesapları

Servis hesabı tam olarak bir siteye bağlıdır ve yalnız `site-admin`, `operator`
veya `viewer` rolü alabilir. `platform-admin` olamaz, web oturumu açamaz ve başka
siteye erişemez.

Servis hesabı token'ı yüksek entropili bir secret ve hassas olmayan kararlı bir
token kimliğinden oluşur. Secret, veritabanı dışında tutulan bir pepper ile
HMAC-SHA-256 kullanılarak özetlenir ve doğrulama sabit zamanlı karşılaştırma
kullanır.

Token davranışı:

- Yalnız oluşturulurken bir kez gösterilir.
- Veritabanında sürümlü, sabit zamanlı karşılaştırmaya uygun hash ile saklanır.
- Varsayılan 90 gün geçerlidir; üst sınır 365 gündür.
- Son kullanım zamanı ve kaynak IP gibi güvenli metadata'yı günceller.
- Yeni token üretimiyle rotate edilebilir.
- Token veya hesap anında iptal edilebilir.
- Her kullanımda actor, token kimliği, site ve sonuç audit edilir.

Mevcut `BAZUSOP_OPERATOR_TOKEN` ve `BAZUSOP_ADMIN_TOKEN` desteği site kapsamlı
RBAC devreye alınırken kaldırılır. Bu bilinçli bir kimlik doğrulama kırılmasıdır;
release notlarında migration yönergesi verilir.

Agent mTLS kimliği servis hesabına dönüşmez. Agent yalnız kendi agent kimliğine
ve sitesine izin verilen ingest, poll ve audit raporlama uçlarına erişebilir.

## Audit, activity ve operasyon logları

### Audit

Audit akışı güvenlik ve adli inceleme içindir. Aşağıdakiler dahil tüm API
isteklerini ve güvenlik kararlarını başarı/başarısızlık sonucuyla kaydeder:

- Bootstrap, giriş, çıkış, MFA ve recovery işlemleri.
- Oturum oluşturma, rotate etme, süre aşımı ve iptal.
- Davet oluşturma, tüketme, iptal ve replay denemeleri.
- Kullanıcı, site ve rol değişiklikleri.
- OIDC yapılandırması ve callback doğrulama sonuçları.
- Servis hesabı ve token yaşam döngüsü ile token kullanımı.
- Tüm okuma ve mutasyon API istekleri.
- İzin verilen, reddedilen ve kapsam dışı yetki kararları.
- Agent enrollment, sertifika yenileme ve ingest.
- Uzak iş oluşturma, teslim, yürütme ve terminal sonuçlar.

Audit olayı en az şu alanları içerir:

```text
event_id, occurred_at, correlation_id,
actor_type, actor_id, session_id veya token_id,
organization_id, site_id,
action, permission, resource_type, resource_id,
outcome, error_code,
source_ip, user_agent,
redacted_change_summary
```

Audit olayları append-only'dir. Ürün API'si update veya delete sunmaz. Retention
organizasyon güvenlik politikasıdır ve yalnız platform yöneticisi tarafından
uzatılabilir; kısaltma ayrı bir yüksek riskli işlem ve audit olayıdır. Yüksek
riskli olaylar periyodik checkpoint'lerle hash zincirine bağlanır. Harici SIEM
aktarımı için transaction içinde outbox kaydı üretilir.

Varsayılan audit retention 365, activity retention 180 gündür. Audit retention
1-3650 gün, activity retention 30-3650 gün aralığında yapılandırılabilir. Süresi
dolan retention worker, uygulama delete API'si değil, partition temelli bakım
işlemi uygular ve her policy değişikliği ayrıca audit edilir.

### Activity

Activity, kullanıcıların anlayacağı başarılı durum değişikliklerini gösterir.
Örnekler: kullanıcı davet edildi, agent siteye katıldı, alarm onaylandı, bakım
penceresi oluşturuldu, servis restart işi tamamlandı, token rotate edildi.

Başarısız denemeler, sıradan okumalar ve düşük seviyeli protokol ayrıntıları
activity'ye girmez; audit ve operational log'da kalır. Activity organizasyon ve
site filtresi, actor, kaynak türü ve zaman aralığıyla sorgulanabilir.

### Hassas veri redaksiyonu

Parola, TOTP secret'ı, recovery code, cookie, CSRF token, davet token'ı, bearer
token, OIDC client secret, authorization code, private key ve tam hassas istek
gövdeleri hiçbir event veya log alanına yazılmaz. Değişiklik özeti alan bazlı
allowlist ile üretilir; sonradan regex ile temizlemeye güvenilmez.

## UI yüzeyleri

İlk sürüm aşağıdaki yeni veya genişletilmiş sayfaları içerir:

- İlk kurulum ve bootstrap ekranı.
- Yerel giriş, TOTP doğrulama ve recovery code akışı.
- Opsiyonel OIDC giriş seçeneği.
- Kullanıcılar ve davetler.
- Siteler ve site üyelikleri.
- Servis hesapları ve token yaşam döngüsü.
- Organizasyon güvenliği ve OIDC ayarları.
- Site filtreli activity zaman çizelgesi.
- Yetkiye göre filtrelenmiş audit arama ekranı.

UI yalnız görünürlükle güvenlik sağlamaz. Yetkisiz kontrol gizlense bile her API
çağrısı sunucuda aynı permission evaluator ile doğrulanır.

Token, recovery code ve bootstrap secret yalnız bir kez gösterilen arayüzlerde
kopyalama ve açık kayıp uyarısı bulunur. Bu değerler tarayıcı depolamasına veya
telemetriye yazılmaz.

## Hata ve güvenli kapanma davranışı

- Kimlik store'una ulaşılamıyorsa yeni oturum veya mutasyon kabul edilmez.
- Permission evaluator belirsiz sonuçta erişimi reddeder.
- Audit transaction'ı başarısızsa güvenlik-kritik mutasyon rollback olur.
- Activity üretimi aynı transaction'ın parçasıdır; yeniden deneme event ID ile
  idempotenttir.
- OIDC sağlayıcısına ulaşılamıyorsa yerel giriş açık kalır; mevcut OIDC callback
  güvenli hata verir ve secret ayrıntısı göstermez.
- Kullanıcı devre dışı veya davet süresi geçmişse kimlik varlığına dair gereksiz
  ayrıntı sızdırılmaz.
- Rate limiting giriş, MFA, davet tüketimi, recovery ve token doğrulama uçlarında
  actor/IP bağlamında uygulanır.

## Teslimat stratejisi

Önerilen yaklaşım, her biri migration, API, UI, audit ve testleriyle tamamlanan
dikey güvenlik dilimleridir. Tek büyük auth değişikliği ve OIDC-first yaklaşımı;
inceleme yüzeyini büyüttüğü veya dış IdP bağımlılığını erkene aldığı için
seçilmemiştir.

| Spike | Çıktı | Kabul sinyali |
| --- | --- | --- |
| 11.1 | Organizasyon/site modeli ve varsayılan site migration'ı | Her kaynak site taşır; çapraz-site erişim engellenir |
| 11.2 | Append-only audit temeli ve actor/correlation bağlamı | Başarılı/başarısız tüm istekler redakte audit üretir |
| 11.3 | Yerel kullanıcı, bootstrap, davet, Argon2id ve TOTP | İlk admin güvenle kurulur; MFA'sız yerel oturum açılamaz |
| 11.4 | Sunucu taraflı oturumlar ve CSRF | Süre aşımı, rotasyon ve toplu iptal test edilir |
| 11.5 | Sabit RBAC matrisi ve site kapsamı | Tüm API'ler rol-site test matrisinden geçer |
| 11.6 | Süreli servis hesabı token'ları | Tek sefer gösterim, hash, rotasyon, iptal ve expiry doğrulanır |
| 11.7 | Eski operator/admin token'larının kaldırılması | Mutasyonlar yalnız insan oturumu veya servis hesabıyla çalışır |
| 11.8 | Activity akışı ve Audit/Activity UI | Site zaman çizelgesi, filtre ve güvenli dışa aktarım çalışır |
| 11.9 | Opsiyonel OIDC | Davetli kullanıcı PKCE ile bağlanır; yerel giriş bağımsız çalışır |
| 11.10 | Güvenlik sertleştirmesi | Scope bypass, CSRF, brute-force ve redaksiyon testleri geçer |

Her spike TDD döngüsü, migration testi, API yetki matrisi, UI kabul testi, tam CI,
ayrı commit ve review kapısıyla tamamlanır.

## Test stratejisi

Zorunlu otomatik test sınıfları:

- Rol × actor türü × site × permission tablo testleri.
- ID değiştirerek yatay ve dikey yetki yükseltme denemeleri.
- Kapsam dışı kaynak için `404`, kapsam içi izinsiz eylem için `403`.
- Kullanıcı devre dışı bırakma ve rol/site değişikliğinde oturum iptali.
- Session fixation, CSRF, cookie ve origin politikaları.
- Parola hash parametreleri, TOTP replay penceresi ve recovery-code replay'i.
- Davet süresi, iptal, eşzamanlı tüketim ve replay.
- Servis token'ı expiry, iptal, rotasyon ve eşzamanlı kullanım.
- OIDC `state`, `nonce`, PKCE, issuer, audience, imza ve subject bağlama.
- Başarılı/başarısız her güvenlik akışında audit üretimi.
- Audit ve operational loglarda secret redaksiyonu.
- Audit yazımı başarısızken güvenlik-kritik mutasyon rollback'i.
- Agent'ın başka sitenin ingest veya job kaynaklarına erişememesi.
- Migration'ın idempotentliği ve geçmiş site bağının korunması.

PostgreSQL entegrasyon testleri gerçek transaction, constraint ve eşzamanlılık
davranışını kullanır. Memory store testleri geliştirme kolaylığı sağlar ancak
kalıcı store güvenlik kabulünün yerine geçmez.

## Operasyon ve geçiş

- `v0.4.0` release notları eski operator/admin bearer secret'larının kaldırıldığını
  açıkça belirtir.
- Yükseltme öncesi platform yöneticisi ve servis hesapları oluşturma sırası
  operasyon rehberinde belgelenir.
- OIDC devreye alınmadan önce yerel break-glass hesabın girişi ve recovery
  kodları doğrulanır.
- OIDC kapatıldığında bağlı kullanıcı verileri silinmez; yerel kullanıcılar ve
  break-glass erişimi çalışmaya devam eder.
- Site, kimlik, oturum ve audit tabloları için yedek/PITR erişimi secret sınırı
  sayılır.
- SIEM tüketicisi geride kalırsa outbox olayları kaybedilmez; retry ve gözlemleme
  metrikleri sunulur.

## Başarı ölçütleri

Tasarım tamamlanmış sayılır:

- Bir platform yöneticisi site ve davet yaşam döngüsünü yönetebilir.
- Yerel kullanıcı MFA olmadan oturum açamaz.
- OIDC olmadan sistem kurulabilir ve işletilebilir.
- OIDC kullanıcısı yalnız davetle bağlanır; rolü IdP claim'inden türetilmez.
- Her insan ve servis hesabı erişimi güncel site kapsamıyla sınırlandırılır.
- Servis hesabı token'ı süreli, hash'li, rotate ve revoke edilebilirdir.
- Eski operator/admin token yolları kapalıdır.
- Her istek audit edilir; anlamlı başarılı değişiklikler activity'de görünür.
- Secret değerleri log, audit, activity, API yanıtı veya UI telemetry'sine sızmaz.
- Güvenlik ve migration kabul testlerinin tamamı CI'da geçer.
