import {
  Activity,
  ArrowRight,
  Boxes,
  Check,
  Cloud,
  Database,
  Gauge,
  Github,
  LockKeyhole,
  Network,
  Server,
  ShieldCheck,
  TerminalSquare,
} from "lucide-react"

const repositoryUrl = "https://github.com/gokayybaz/bazusop"

const platformFacts = [
  { label: "İşletim sistemi", value: "Linux + Windows" },
  { label: "Güven sınırı", value: "Agent tabanlı kimlik" },
  { label: "Uzak operasyon", value: "İmzalı işler" },
  { label: "Bulut keşfi", value: "AWS · Azure · GCP" },
]

const productCapabilities = [
  {
    icon: Activity,
    title: "Gözlem",
    description: "Metrik, servis, log ve alarm sinyallerini sunucunun bağlamından koparmadan aynı akışta izleyin.",
    detail: "Telemetri · servisler · loglar",
  },
  {
    icon: TerminalSquare,
    title: "Müdahale",
    description: "Servis yeniden başlatma ve host reboot işlerini onay, imza ve sıralı olay geçmişiyle yürütün.",
    detail: "Allowlist · onay · audit",
  },
  {
    icon: Cloud,
    title: "Uzlaştırma",
    description: "Bulut envanterini kalıcı agent kimliğiyle eşleştirerek on-prem ve provider görünümünü birleştirin.",
    detail: "AWS · Azure · GCP",
  },
]

const securityPoints = [
  "Ed25519 tabanlı kalıcı agent kimliği",
  "TLS 1.3 ve karşılıklı sertifika doğrulaması",
  "Tek kullanımlık kayıt belirteci",
  "İmzalı, allowlist ile sınırlandırılmış işler",
]

const fleetRows = [
  { name: "api-edge-07", os: "Ubuntu 24.04", load: "42%", status: "Sağlıklı", tone: "healthy" },
  { name: "db-prod-02", os: "Windows Server", load: "68%", status: "İzleniyor", tone: "warning" },
  { name: "worker-eu-04", os: "Debian 12", load: "31%", status: "Sağlıklı", tone: "healthy" },
]

function Brand() {
  return (
    <a aria-label="bazUSOP ana sayfa" className="landing-brand" href="./">
      <span className="landing-brand-mark" aria-hidden="true"><span /></span>
      <span className="landing-brand-copy"><strong>bazUSOP</strong><small>server operations</small></span>
    </a>
  )
}

export function LandingPage() {
  return (
    <div className="landing-shell">
      <header className="landing-header">
        <div className="landing-container landing-header__inner">
          <Brand />
          <nav aria-label="Landing page navigasyonu" className="landing-nav">
            <a href="#platform">Platform</a>
            <a href="#architecture">Mimari</a>
            <a href="#security">Güvenlik</a>
          </nav>
          <a className="landing-github" href={repositoryUrl}>
            <Github aria-hidden="true" size={16} />
            <span>GitHub'da incele</span>
          </a>
        </div>
      </header>

      <main>
        <section className="landing-hero">
          <div className="landing-container landing-hero__inner">
            <div className="hero-copy">
              <p className="hero-context"><span aria-hidden="true" /> Açık kaynak operasyon platformu</p>
              <h1>Sunucu operasyonları için tek çalışma yüzeyi.</h1>
              <p className="hero-summary">Linux ve Windows filonuzu gözlemleyin, güvenli biçimde yönetin ve bulut envanteriyle uzlaştırın. Gürültü değil, karar vermek için gereken bağlam.</p>
              <div className="hero-actions">
                <a className="landing-primary" href={repositoryUrl}>GitHub'da incele <ArrowRight aria-hidden="true" size={17} /></a>
                <a className="landing-secondary" href="#architecture">Mimariyi görün</a>
              </div>
              <p className="hero-note"><Check aria-hidden="true" size={15} /> Kendi altyapınızda çalışan tek binary dağıtım</p>
            </div>

            <div className="operation-surface" role="region" aria-label="Canlı operasyon görünümü">
              <div className="surface-topbar">
                <div className="surface-title"><span className="surface-logo">U</span><strong>Operasyon özeti</strong><span>/ Üretim filosu</span></div>
                <div className="surface-state"><i /> Demo görünümü</div>
              </div>
              <div className="surface-body">
                <aside className="surface-rail" aria-hidden="true">
                  <span className="is-active"><Gauge size={17} /></span>
                  <span><Server size={17} /></span>
                  <span><Boxes size={17} /></span>
                  <span><TerminalSquare size={17} /></span>
                  <span><ShieldCheck size={17} /></span>
                </aside>
                <div className="surface-content">
                  <div className="surface-heading">
                    <div><span>Filo sağlığı</span><strong>Tüm sistemler izleniyor</strong></div>
                    <button type="button">Son 24 saat</button>
                  </div>

                  <div className="surface-stats" aria-label="Örnek filo durumu">
                    <div><span>Bağlı agent</span><strong>24</strong><small className="positive"><i /> Çevrimiçi</small></div>
                    <div><span>Açık alarm</span><strong>03</strong><small>2 uyarı · 1 kritik</small></div>
                    <div><span>Bekleyen iş</span><strong>01</strong><small>Onay bekliyor</small></div>
                  </div>

                  <div className="surface-lower">
                    <div className="fleet-table">
                      <div className="fleet-table__head"><span>Sunucu</span><span>Yük</span><span>Durum</span></div>
                      {fleetRows.map((row) => (
                        <div className="fleet-row" key={row.name}>
                          <span className="fleet-machine"><i className={row.tone} /><span><strong>{row.name}</strong><small>{row.os}</small></span></span>
                          <span className="fleet-load"><span style={{ width: row.load }} /><small>{row.load}</small></span>
                          <span className={`fleet-status ${row.tone}`}>{row.status}</span>
                        </div>
                      ))}
                    </div>
                    <div className="signal-chart">
                      <div className="chart-head"><span>Kaynak kullanımı</span><small><i /> CPU</small></div>
                      <svg role="img" aria-label="Örnek filo kaynak kullanım grafiği" viewBox="0 0 360 150">
                        <defs><linearGradient id="landing-area" x1="0" x2="0" y1="0" y2="1"><stop offset="0" stopColor="#4fd1c5" stopOpacity=".2" /><stop offset="1" stopColor="#4fd1c5" stopOpacity="0" /></linearGradient></defs>
                        {[28, 65, 102, 139].map((y) => <line key={y} x1="0" x2="360" y1={y} y2={y} />)}
                        <path className="chart-area" d="M0 120 C34 112 48 91 76 99 S121 74 151 88 S198 52 229 68 S279 45 306 58 S339 32 360 39 L360 150 L0 150Z" />
                        <path className="chart-line-primary" d="M0 120 C34 112 48 91 76 99 S121 74 151 88 S198 52 229 68 S279 45 306 58 S339 32 360 39" />
                      </svg>
                    </div>
                  </div>
                </div>
              </div>
            </div>
          </div>
        </section>

        <section className="landing-facts" aria-label="Ürün kapsamı">
          <ul className="landing-container landing-facts__list" aria-label="Platform kabiliyetleri">
            {platformFacts.map((fact) => <li key={fact.label}><span>{fact.label}</span><strong>{fact.value}</strong></li>)}
          </ul>
        </section>

        <section className="landing-section capabilities" id="platform">
          <div className="section-heading">
            <p className="section-marker">Platform</p>
            <h2>Sinyalden müdahaleye, bağlam kaybetmeden.</h2>
            <p>Gözlem, karar ve aksiyon aynı operasyon modeli içinde kalır. Ekipler araçlar arasında değil, sorun üzerinde çalışır.</p>
          </div>
          <div className="capability-list">
            {productCapabilities.map(({ icon: Icon, title, description, detail }) => (
              <article key={title}>
                <span className="capability-icon" aria-hidden="true"><Icon size={20} /></span>
                <h3>{title}</h3>
                <p>{description}</p>
                <small>{detail}</small>
              </article>
            ))}
          </div>
        </section>

        <section className="architecture" id="architecture">
          <div className="landing-container architecture__inner">
            <div className="architecture-copy">
              <p className="section-marker">Mimari</p>
              <h2>Az bileşen.<br />Açık güven sınırları.</h2>
              <p>Agent’lar bağlantıyı yalnız dışarı doğru başlatır. Hub; kimlik, telemetri, envanter ve operasyon işlerini tek denetlenebilir akışta toplar.</p>
              <ul>
                <li><Check aria-hidden="true" size={16} /> React arayüzü ve Go hub tek çalıştırılabilir dosyada</li>
                <li><Check aria-hidden="true" size={16} /> PostgreSQL veya TimescaleDB veri katmanı</li>
                <li><Check aria-hidden="true" size={16} /> Docker Compose ve Kubernetes dağıtımı</li>
              </ul>
            </div>
            <div className="architecture-map" aria-label="bazUSOP mimari akışı">
              <div className="architecture-lane">
                <span className="lane-label">Yönetilen filo</span>
                <div className="map-cluster">
                  <span><Server size={17} /> Linux</span>
                  <span><Server size={17} /> Windows</span>
                </div>
              </div>
              <div className="architecture-link"><span>mTLS · outbound</span><i aria-hidden="true" /></div>
              <div className="map-hub"><span><Network size={20} /></span><div><strong>bazUSOP Hub</strong><small>Kimlik · API · İş motoru</small></div></div>
              <div className="architecture-branches" aria-hidden="true"><i /><i /></div>
              <div className="map-destinations">
                <div><Database size={18} /><span><strong>Veri katmanı</strong><small>PostgreSQL / TimescaleDB</small></span></div>
                <div><Cloud size={18} /><span><strong>Bulut envanteri</strong><small>AWS / Azure / GCP</small></span></div>
              </div>
            </div>
          </div>
        </section>

        <section className="landing-section security" id="security">
          <div className="security-intro">
            <div className="security-symbol" aria-hidden="true"><LockKeyhole size={26} /></div>
            <div>
              <p className="section-marker">Güvenlik</p>
              <h2>Kimlikten başlayan güvenlik.</h2>
            </div>
          </div>
          <div className="security-content">
            <p>Her agent kendi kriptografik kimliğiyle konuşur. Her uzak aksiyon onaylanır, imzalanır ve sonradan denetlenebilir.</p>
            <div className="security-points">
              {securityPoints.map((point) => <div key={point}><ShieldCheck aria-hidden="true" size={17} /><span>{point}</span></div>)}
            </div>
          </div>
        </section>

        <section className="landing-cta">
          <div className="landing-container landing-cta__inner">
            <p className="section-marker">Açık kaynak · kendi altyapınızda</p>
            <h2>Operasyon gerçeğinizi tek yerde tutun.</h2>
            <p>Dağınık sinyalleri ortak bağlama, kritik müdahaleleri doğrulanabilir iş akışına dönüştürün.</p>
            <a className="landing-primary" href={repositoryUrl}>GitHub'da incele <ArrowRight aria-hidden="true" size={17} /></a>
          </div>
        </section>
      </main>

      <footer className="landing-footer">
        <div className="landing-container landing-footer__inner">
          <Brand />
          <p>Açık kaynak birleşik sunucu operasyon platformu</p>
          <span>© 2026 bazUSOP</span>
        </div>
      </footer>
    </div>
  )
}
