# Teslimat yol haritası

Her spike aynı teslimat döngüsünü izler:

1. Çalıştırılabilir kabul testini yaz ve kırıldığını gözle.
2. En küçük tutarlı dikey dilimi geliştir.
3. Testleri yeşil tutarak refactor et.
4. Tam testleri ve production build'lerini çalıştır.
5. Tamamlanan spike'ı ayrı commit et ve pushla.

## Spike'lar

| Spike | Çıktı | Kabul sinyali |
| --- | --- | --- |
| 0 — Temel | Go hub, gömülü React shell ve CI komutları | Sağlık API'si/UI testleri geçer; tek binary üretilir |
| 1 — Kayıt | Linux ve Windows agent'ları hub'a güvenli kaydolur | Tek kullanımlık token yenilenebilir agent kimliğine dönüşür |
| 2 — Envanter | Agent'lar normalize host ve OS bilgisi raporlar | Kayıtlı host'lar instance envanterinde görünür |
| 2.1 — Dağıtım temeli | Hub Docker ve Kubernetes'te tutarlı çalışır | Compose smoke ve Helm lint/render kontrolleri geçer |
| 3 — Telemetri | CPU, bellek, disk ve ağ örnekleri TimescaleDB'ye ulaşır | Instance detayı güncel ve geçmiş metrikleri gösterir |
| 3.1 — Türkçe temel | UI ve dokümanlar Türkçe-öncelikli olur | Dil/erişilebilirlik sözleşmesi ve production build geçer |
| 4 — Servisler | systemd ve Windows Service durumu toplanır | Servisler instance bazında filtrelenir ve incelenir |
| 5 — Loglar | journald, dosya ve Windows Event logları aranır | Canlı tail ve sınırlı geçmiş arama çalışır |
| 6 — İşler | Onaylı operasyon aksiyonları imzalı işler olarak çalışır | Restart/reboot çıktısı akar ve audit trail tamamlanır |
| 7 — Alarm | Metrik/erişilebilirlik kuralları yönetilen olay üretir | Alarm yaşam döngüsü ve bakım pencereleri test edilir |
| 8 — Bulut keşfi | AWS, Azure ve GCP envanteri agent'larla uzlaştırılır | Provider instance ile agent kimliği güvenle eşleşir |
| 9.1 — Rol ayrımı | Operasyon ve yönetim mutasyonları ayrı bearer rolleri kullanır | Operator yönetim politikasını değiştiremez; admin iki yetkiyi de taşır |
| 9.2.1 — Ortak canlı akış | PostgreSQL tabanlı ortak log event bus | Farklı hub replikasından ingest edilen log SSE abonesine ulaşır |
| 9.2.2 — Saklama ve yük | Yapılandırılabilir retention ve fan-out regresyon hedefi | 1–3650 günlük policy doğrulanır; maksimum batch 32 aboneye kayıpsız ulaşır |
| 9.3.1 — Release kimliği | Hub sürüm metadata'sı, çapraz platform arşivleri ve checksum manifesti | `--version`/UI aynı build'i gösterir; Linux amd64/arm64 ve Windows amd64 arşivleri tag workflow'uyla yayımlanır |
| 9.3.2 — İmzalı paket ve yükseltme | deb/rpm/MSI, keyless release/container imzası ve kontrollü yükseltme | İmzalar doğrulanır; başarısız Linux yükseltmesi önceki binary'ye döner; MSI downgrade'i engeller |
| 10.1 — Gerçek agent runtime | Linux/Windows agent binary'si, kalıcı mTLS kimliği, yenileme ve host envanter döngüsü | Agent outbound kaydolur; private key diskte korunur; mTLS envanter raporu hub'da görünür; üç hedef arşivi üretilir |
| 10.2 — Ortak enrollment güven kökü | PostgreSQL'de kalıcı agent CA ve atomik tek-kullanımlık token kayıtları | Hub restart'ı agent kimliğini bozmaz; iki replika aynı CA'yı kullanır ve token yalnız bir kez tüketilir |
| 10.3 — Native agent servisi | Linux deb/rpm ve Windows MSI ile yönetilen agent yaşam döngüsü | systemd/Windows Service otomatik başlangıca kurulur; kimlik dizini korunur; üç hedefin paketleri üretilir |
| 10.4 — Gerçek agent telemetrisi | Linux/Windows agent CPU, bellek, kök disk ve ağ sayaçlarını periyodik gönderir | İlk örnek hemen ulaşır; CPU sonraki örneklerde sayaç deltasıdır; envanter hatası telemetriyi durdurmaz |
| 10.5 — Gerçek servis envanteri | Agent systemd ve Windows SCM servislerini ortak modele taşır | Runtime/startup durumu ilk çevrimde mTLS ile görünür; collector hataları diğer raporları durdurmaz |
| 10.6 — Gerçek host logları | Agent journald ve Windows Event kayıtlarını ortak modele taşıyıp mTLS batch'leri gönderir | Önem/kaynak normalize edilir; batch en fazla 1000 kayıttır; cursor yalnız başarılı gönderimden sonra ilerler |
| 10.7 — Güvenli uzak iş çalıştırma | Agent imzalı servis restart ve host reboot işlerini platform API'leriyle çalıştırır | İmza enrollment CA'ya pinlenir; keyfi komut reddedilir; çıktı ve terminal durum sıralı audit olaylarına ulaşır |
| 10.8 — Kesinti güvenli iş kurtarma | Agent yürütme/audit state'ini diskte korur; hub yarım işi korumalı biçimde yeniden sunar | Aktif teslim çift çalışmaz; audit retry idempotenttir; belirsiz crash sonrası komut tekrarlanmadan iş terminale taşınır |
| 10.9 — Kalıcı log checkpoint'i | Agent log cursor'unu diskte korur ve replay'i kararlı kimlikle gönderir | Restart kaldığı yerden sürer; kabul/checkpoint çökme penceresindeki tekrar history veya SSE'de çoğalmaz |
| 11 — Birleşik denetim izi | İş ve alarm olayları `GET /api/v1/audit/events` ile tek zaman çizelgesinde birleşir; `/audit` sayfası gerçek veriyi gösterir | İki farklı kaynaktan gelen olay tek zaman çizelgesinde doğru sırada görünür; site'lar arası sızıntı yok; `limit` çalışır |

## Mimari kısıtlar

- Agent bağlantıyı dışarı doğru başlatır; inbound agent portu gerekmez.
- Tek kullanımlık kayıttan sonra agent kimliği mTLS kullanır.
- Hub modular monolith'tir ve gömülü React uygulamasını sunar.
- İlişkisel durum PostgreSQL'in, zaman serileri TimescaleDB'nin sorumluluğudur.
- Uzak aksiyonlar allowlist ile sınırlı, kimlikli ve denetlenebilir olmalıdır.
- PostgreSQL olmadan enrollment CA ve token-consumption durumu süreçle sınırlıdır;
  üretimde enrollment trafiği kalıcı store kullanan hub'lara yönelmelidir.
