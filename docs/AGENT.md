# bazUSOP agent rehberi

`bazusop-agent`, Linux veya Windows hostundan hub'a yalnız outbound HTTPS
bağlantısı kurar. İlk dikey dilim host/OS/CPU/bellek/IP envanterini toplar;
telemetri, servis, log ve uzak iş çalıştırıcıları ayrı spike'larda bu çalışma
döngüsüne bağlanacaktır.

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

Varsayılan state dizini `%ProgramData%\\bazUSOP\\agent` olur. Bu spike agent
binary'sini ve arşivini üretir; Windows Service ve native agent installer yaşam
döngüsü bir sonraki paketleme dilimidir. O zamana kadar yönetici, state dizini
ACL'ini yalnız agent'ı çalıştıran hesap ve `SYSTEM` okuyabilecek şekilde
sınırlandırmalıdır; POSIX mod bitleri Windows ACL korumasının yerine geçmez.

## Yapılandırma ve teşhis

`BAZUSOP_AGENT_HUB_URL` uzak bağlantıda HTTPS olmalıdır. Kod sertifika
doğrulamasını kapatan bir seçenek sunmaz. Loopback HTTP yalnız protokol
geliştirme kolaylığıdır; envanter ucu mTLS istediğinden tam agent akışı için hub
TLS'i kullanılmalıdır.

Agent başlangıçta hemen, ardından `BAZUSOP_AGENT_REPORT_INTERVAL` periyodunda
envanter göndermeyi dener. Hub geçici olarak erişilemiyorsa hata JSON loga yazılır;
process kapanmadan aynı periyotta kayıt veya raporu tekrar dener.

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
