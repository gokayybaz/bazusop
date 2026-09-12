# bazUSOP agent rehberi

`bazusop-agent`, Linux veya Windows hostundan hub'a yalnız outbound HTTPS
bağlantısı kurar. Host/OS/CPU/bellek/IP envanteriyle birlikte CPU kullanımı,
bellek kullanımı, kök disk doluluğu, toplam ağ byte sayaçları ve yönetilen servis
durumlarını ve host loglarını toplar; onaylı uzak operasyon işlerini güvenli
allowlist içinde çalıştırır.

## Güvenli kayıt

1. Hub'ı `BAZUSOP_TLS_CERT_FILE`, `BAZUSOP_TLS_KEY_FILE` ve yüksek entropili
   `BAZUSOP_ENROLLMENT_TOKEN` ile başlatın.
2. Özel PKI kullanıyorsanız hub server sertifikasını imzalayan CA'yı hosta PEM
   olarak yerleştirin. Public CA kullanıyorsanız sistem trust store yeterlidir.
3. Agent'a hub URL'sini, ilk kayıt token'ını ve gerekiyorsa server CA yolunu verin.
4. Hub CSR'ı doğrular, 24 saatlik client sertifikası ve kararlı bir SPIFFE agent
   ID üretir. Token yalnız bir kez tüketilebilir.
5. Agent private key'i kendisi üretir; hub'a veya loga göndermez. Kimlik dosyaları
   aynı dizinde geçici dosya + atomik rename ile yazılır; Unix'te private key ve
   metadata `0600` izni taşır.

Agent, sertifika bitimine bir saat kala mevcut mTLS kimliğiyle yeni CSR gönderir.
Sertifika zaten sona ermişse otomatik yenileme yapılamaz; state dizini kontrollü
biçimde kaldırılıp yeni bir bootstrap token ile tekrar kayıt gerekir.

## Linux çalıştırma

```bash
sudo install -d -m 0700 /var/lib/bazusop-agent
sudo env \
  BAZUSOP_AGENT_HUB_URL=https://hub.example.com \
  BAZUSOP_AGENT_ENROLLMENT_TOKEN='tek-kullanimlik-token' \
  BAZUSOP_AGENT_SERVER_CA_FILE=/etc/bazusop/hub-server-ca.crt \
  BAZUSOP_AGENT_REPORT_INTERVAL=30s \
  /usr/local/bin/bazusop-agent
```

İlk başarılı kayıt sonrasında servis ortamından
`BAZUSOP_AGENT_ENROLLMENT_TOKEN` kaldırılabilir. State dizini yedeklenmemeli veya
başka hosta kopyalanmamalıdır; içindeki private key host kimliğidir.

## Windows çalıştırma

Yönetici PowerShell oturumunda örnek doğrulama:

```powershell
$env:BAZUSOP_AGENT_HUB_URL = "https://hub.example.com"
$env:BAZUSOP_AGENT_ENROLLMENT_TOKEN = "tek-kullanimlik-token"
$env:BAZUSOP_AGENT_SERVER_CA_FILE = "C:\\ProgramData\\bazUSOP\\hub-server-ca.crt"
& "C:\\Program Files\\bazUSOP\\bazusop-agent.exe"
```

Varsayılan state dizini `%ProgramData%\\bazUSOP\\agent` olur. Agent MSI'ı
`bazusop-agent` adlı, otomatik başlangıçlı bir Windows Service kaydeder ve state
dizinini kaldırma/yükseltmede korur. Hub URL'si, enrollment token'ı ve özel CA
yolu paket veya MSI komut satırına yazılmamalı; bunları makine kapsamlı ortam
değişkenleriyle tanımladıktan sonra `Start-Service bazusop-agent` çalıştırılmalıdır.
Agent, state dizini ACL'ini çalışan hesap, `SYSTEM` ve yerel yöneticiler dışında
erişime kapatır.

## Native paket kurulumu

Linux deb/rpm paketi `bazusop-agent.service` birimini kurar ve boot için
etkinleştirir. İlk başlatmadan önce `/etc/bazusop/agent.env` dosyasını root
sahipliğinde `0600` izinle oluşturun:

```bash
sudo install -m 0600 /dev/null /etc/bazusop/agent.env
sudoedit /etc/bazusop/agent.env
sudo systemctl start bazusop-agent
```

Dosyada `BAZUSOP_AGENT_HUB_URL`, ilk kayıt için
`BAZUSOP_AGENT_ENROLLMENT_TOKEN` ve gerekiyorsa
`BAZUSOP_AGENT_SERVER_CA_FILE` bulunmalıdır. Başarılı kayıttan sonra token'ı
dosyadan kaldırıp servisi yeniden başlatın. Paket yükseltmesi servisi yeniden
başlatır; uninstall, host kimliği olan `/var/lib/bazusop-agent` dizinini silmez.

## Yapılandırma ve teşhis

`BAZUSOP_AGENT_HUB_URL` uzak bağlantıda HTTPS olmalıdır. Kod sertifika
doğrulamasını kapatan bir seçenek sunmaz. Loopback HTTP yalnız protokol
geliştirme kolaylığıdır; envanter ucu mTLS istediğinden tam agent akışı için hub
TLS'i kullanılmalıdır.

Agent başlangıçta hemen, ardından `BAZUSOP_AGENT_REPORT_INTERVAL` periyodunda
envanter, telemetri, servis snapshot'ı ve host loglarını göndermeyi dener. CPU'nun ilk örneği boot'tan itibaren
ortalama kullanımdır; sonraki örnekler iki sistem sayacı arasındaki deltadan
hesaplanır. Bellek kullanılabilir kapasiteden, disk işletim sisteminin kök
volume'ünden hesaplanır; ağ alanları kümülatif alınan/gönderilen byte sayaçlarıdır.
Bir collector hatası diğer rapor türünü engellemez. Hub geçici olarak
erişilemiyorsa hata JSON loga yazılır; process kapanmadan aynı periyotta tekrar
dener.

Linux collector, `systemctl list-units` ve `list-unit-files` toplu çıktılarını
birleştirir; her servis için ayrı process açmaz. Windows collector Service Control
Manager'dan servis durumunu ve başlangıç tipini okur. Geçici durumlar `unknown`,
başarısız Windows exit code'u veya systemd `failed` durumu `failed` olarak
raporlanır. Snapshot hub'da önceki servis listesini atomik olarak değiştirir.

Linux log collector `journalctl` JSON çıktısını, Windows collector ise System ve
Application Event kanallarını okur. Syslog priority ve Windows event level
değerleri ortak `debug/info/warn/error/critical` önem modeline çevrilir; her batch
en fazla 1000 kayıt taşır. Cursor yalnız hub batch'i kabul ettikten sonra ilerler,
bu nedenle geçici gönderim hatasında aynı batch yeniden denenir. Cursor process
belleğindedir; agent restart'ı ilk rapor aralığını yeniden okuyabileceği için log
teslimi en az bir kez semantiğindedir ve nadir tekrarlar mümkün kabul edilir.

## Uzak operasyon işleri

Agent her rapor çevriminde kendisine atanmış en eski işi mTLS ile atomik teslim
alır. Hub işleri kalıcı enrollment CA anahtarıyla Ed25519 olarak imzalar; agent
gönderilen anahtarı diskteki `agent-ca.crt` public key'iyle eşleştirir ve kanonik
iş imzasını doğrular. Agent ID, `running`/sequence durumu, aksiyon ve hedef ayrıca
yerelde doğrulanmadan hiçbir komut çalıştırılmaz.

Allowlist yalnız `service.restart` ve `host.reboot` aksiyonlarını içerir. Linux'ta
servis restart `systemctl`, Windows'ta doğrudan Service Control Manager ile
çalışır; keyfi shell veya komut gövdesi kabul edilmez. Reboot, terminal audit
olayının hub'a ulaşabilmesi için Linux ve Windows'ta yaklaşık bir dakika sonrasına
programlanır. Native Linux servisi root, Windows servisi LocalSystem çalıştığından
bu işlemler için gereken host yetkilerine sahiptir. Başlangıç, başarı ve hata
sonuçları artan sequence değerleriyle audit izine gönderilir; geçersiz veya güven
zincirine uymayan iş çalıştırılmadan `failed` yapılır. Audit gönderimi geçici
olarak başarısız olursa bekleyen olay sonraki çevrimde yeniden gönderilir; aynı
process içinde komut ikinci kez çalıştırılmaz.

`401` için sırasıyla client sertifika süresini, agent state dosyalarını, hub'ın
agent CA durumunu ve reverse proxy'nin client sertifikasını hub'a kadar koruduğunu
kontrol edin. `x509: certificate signed by unknown authority` hatasında
`BAZUSOP_AGENT_SERVER_CA_FILE` hub'ın client CA'sını değil, hub server
sertifikasını imzalayan CA'yı göstermelidir.

## Hub kalıcılığı

Hub PostgreSQL modunda çalışıyorsa agent CA ve token tüketim durumu ortak store'da
kalır. Restart veya başka hub replikasına yönlenme mevcut sertifikayı bozmaz ve
aynı token eşzamanlı iki kayıtta kullanılamaz. Hub memory modundaysa CA süreçle
birlikte kaybolur; bu mod yalnız geliştirme içindir.

Yeni bir agent eklemek için yüksek entropili yeni bir
`BAZUSOP_ENROLLMENT_TOKEN` değeri dağıtıp hub replikalarını rolling restart edin.
Yeni token'ın yalnız SHA-256 özeti kaydedilir; önceki tüketilmemiş token iptal
edilir ve daha önce tüketilen token aynı değerle yeniden etkinleşmez. CA private key'i veritabanında bulunduğundan database
erişimi, yedekler ve PITR çıktıları secret sınıfında korunmalıdır. Harici KMS/HSM
ile envelope encryption daha ileri sertleştirme adımıdır.
