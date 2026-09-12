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
| 9.3 — Sürümleme | Paketleme, imzalama ve kontrollü yükseltme | deb/rpm/MSI/container imzalanır ve yükseltme geri dönüşü test edilir |

## Mimari kısıtlar

- Agent bağlantıyı dışarı doğru başlatır; inbound agent portu gerekmez.
- Tek kullanımlık kayıttan sonra agent kimliği mTLS kullanır.
- Hub modular monolith'tir ve gömülü React uygulamasını sunar.
- İlişkisel durum PostgreSQL'in, zaman serileri TimescaleDB'nin sorumluluğudur.
- Uzak aksiyonlar allowlist ile sınırlı, kimlikli ve denetlenebilir olmalıdır.
- Ortak enrollment CA ve token-consumption durumu tamamlanmadan enrollment
  trafiği yatay ölçeklenmez.
