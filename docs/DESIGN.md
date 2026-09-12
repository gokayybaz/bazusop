# bazUSOP tasarım sistemi

## Ürün karakteri

bazUSOP; teknik güvenilirlik, operasyonel kontrol ve baskı altında sakinlik
hissi verir. Sosyal veya eğlence ürünü değil, altyapı ekiplerinin yoğun kullandığı
bir çalışma yüzeyidir. Kritik durum ilk bakışta anlaşılır; dekorasyon sessiz kalır.

Arayüzün ve kullanıcı dokümantasyonunun ana dili Türkçedir. API yolları, JSON
alanları, log anahtarları ve kod tanımlayıcıları İngilizce kalır. Gelecekteki
İngilizce seçeneği aynı kavram sözlüğünü kullanan ayrı bir locale katmanı olarak
eklenecek; yeni metinler bileşen davranışına gömülü varsayımlar üretmemelidir.

## Renk sistemi

Koyu tema operasyon yüzeyinin varsayılanıdır. Operatör koyu ve açık tema arasında
geçiş yapabilir; seçim cihazda kalıcıdır. Açık tema saf beyaz canvas yerine serin
gri ana yüzey, beyaz kartlar ve koyu metin kullanır.

| Token | Koyu | Açık | Kullanım |
| --- | --- | --- | --- |
| Arka plan | `#121212` | `#F3F6F8` | Ana uygulama yüzeyi |
| Kart | `#1E1E1E` | `#FFFFFF` | Panel ve KPI yüzeyleri |
| Çerçeve | `#2C2C2E` | `#D3DDE4` | Ayırıcı, grid ve tablo satırları |
| Veri vurgusu | `#00E5FF` | `#007F8F` | Birincil grafik, değer ve aksiyon |
| Kritik | `#FF453A` | `#C4312B` | Kritik alarm ve yıkıcı durum |
| Sağlıklı | `#32D74B` | `#167438` | Canlı ve normal durum |
| Birincil metin | `#FFFFFF` | `#101A24` | Başlık ve önemli değer |
| İkincil metin | `#98989D` | `#6A7B8B` | Etiket ve metadata |

Vurgu renkleri semantik ve seyrektir: cyan veri/aktif kontrol, yeşil sağlıklı
durum, amber uyarı, kırmızı kritik durum içindir. Pastel dekorasyon kullanılmaz.

## Tipografi

- Arayüz ve gövde: Inter/system sans, regular, en az 14px.
- Sayfa başlığı: Inter/system sans, semibold, 24–32px.
- KPI ve makine değerleri: JetBrains Mono/Fira Code/system mono, bold, 32px.
- Operasyonel önemi düşük metadata 12–13px olabilir.

## Geometri ve boşluk

- 12 kolonlu responsive grid kullanılır.
- Boşluklar 8px katlarıdır: 8, 16, 24 ve 32px.
- Kartlar 16px radius, 20px iç boşluk ve ince tam çevre çerçevesi kullanır.
- Tablolarda sakin satır ayırıcıları ve `#252525` hover yüzeyi kullanılır.
- Dashboard üç KPI ile başlar; ana alan sekiz kolon operasyon görünümü ve dört
  kolon alarm paneline ayrılır.

## Bilgi mimarisi

Arayüz tek bir uzun dashboard değildir. Ortak sidebar ve üst bar korunurken her
operasyon alanı kendi URL'sinde, yalnızca kendi görevine ait içeriği gösterir:

| Sayfa | URL | Sorumluluk |
| --- | --- | --- |
| Genel bakış | `/` | Filo özeti, kaynak eğilimi ve açık alarm özeti |
| Filo | `/fleet` | Sunucu envanteri ve metrik görünümüne geçiş |
| Servisler | `/services` | Sunucu bazlı servis envanteri ve filtreleme |
| Metrikler | `/metrics` | Sunucu bazlı telemetri ve zaman serisi |
| Loglar | `/logs` | Geçmiş arama ve canlı log akışı |
| İşler | `/jobs` | Uzak aksiyon oluşturma ve denetim olayları |
| Alarmlar | `/alerts` | Kural, bakım penceresi ve olay yaşam döngüsü |
| Bulut hesapları | `/cloud` | Cloud provider bağlantıları ve keşif |
| Denetim izi | `/audit` | Birleşik operasyon zaman çizelgesi |
| Ayarlar | `/settings` | Hub ve arayüz tercihleri |

Tarayıcının geri/ileri hareketleri desteklenir ve her sayfa doğrudan URL ile
açılabilir. Bir sunucu seçimi operasyon sayfaları arasında korunur; servis,
metrik, log ve iş içerikleri aynı anda üst üste gösterilmez.

## Veri görselleştirme

- Değişken kaynak kullanımında area chart tercih edilir.
- Ağır grafik çerçevesi yerine ince `#2C2C2E` grid çizgileri kullanılır.
- Birincil seri cyan, sağlıklı karşılaştırma veya dolgu yeşildir.
- Renk daima metin, şekil veya durum etiketiyle desteklenir; tek sinyal değildir.

## Alarm ve etkileşim kuralları

- Renkli sol kenarlık hiçbir bileşende kullanılmaz. Önem derecesi sakin yüzey
  tonu, ince tam çevre çerçevesi, durum metni ve ikon rengiyle anlatılır.
- Hareket 150–180ms easing kullanır ve `prefers-reduced-motion` tercihine uyar.
- Focus, hover ve seçili durumlar her iki temada da ayırt edilebilir olmalıdır.
- Sunucu, terminal, servis ve aktivite gibi alan ikonları kullanılır; belirsiz
  dekoratif ikonlardan kaçınılır.
