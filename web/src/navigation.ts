import { Activity, Bell, Boxes, ChartNoAxesCombined, Cloud, Gauge, ListChecks, Server, Settings, ShieldCheck, TerminalSquare } from "lucide-react"

import type { PageID } from "./types"

export const navigation = [
  { id: "overview" as const, icon: Gauge, label: "Genel bakış", path: "/" },
  { id: "fleet" as const, icon: Server, label: "Filo", path: "/fleet" },
  { id: "services" as const, icon: Boxes, label: "Servisler", path: "/services" },
  { id: "metrics" as const, icon: ChartNoAxesCombined, label: "Metrikler", path: "/metrics" },
  { id: "logs" as const, icon: TerminalSquare, label: "Loglar", path: "/logs" },
  { id: "jobs" as const, icon: ListChecks, label: "İşler", path: "/jobs" },
  { id: "alerts" as const, icon: Bell, label: "Alarmlar", path: "/alerts" },
]

export const secondaryNavigation = [
  { id: "cloud" as const, icon: Cloud, label: "Bulut hesapları", path: "/cloud" },
  { id: "activity" as const, icon: Activity, label: "Aktivite", path: "/activity" },
  { id: "audit" as const, icon: ShieldCheck, label: "Denetim izi", path: "/audit" },
  { id: "settings" as const, icon: Settings, label: "Ayarlar", path: "/settings" },
]

export const pageMeta: Record<PageID, { eyebrow: string; title: string; description: string }> = {
  overview: { eyebrow: "FİLO / ÜRETİM", title: "Operasyon özeti", description: "Filo sağlığı ve ilgilenilmesi gereken sinyaller" },
  fleet: { eyebrow: "ENVANTER", title: "Sunucu filosu", description: "Kayıtlı Linux ve Windows agent'ları" },
  services: { eyebrow: "SUNUCU DURUMU", title: "Servisler", description: "systemd ve Windows Service envanteri" },
  metrics: { eyebrow: "GÖZLEMLENEBİLİRLİK", title: "Metrikler", description: "CPU, bellek, disk ve ağ telemetrisi" },
  logs: { eyebrow: "GÖZLEMLENEBİLİRLİK", title: "Loglar", description: "Geçmiş arama ve canlı log akışı" },
  jobs: { eyebrow: "OPERASYON", title: "İşler", description: "İmzalı ve denetlenebilir uzak aksiyonlar" },
  alerts: { eyebrow: "OLAY YÖNETİMİ", title: "Alarmlar", description: "Kurallar, olaylar ve bakım pencereleri" },
  cloud: { eyebrow: "KEŞİF", title: "Bulut hesapları", description: "AWS, Azure ve GCP envanter bağlantıları" },
  activity: { eyebrow: "YÖNETİŞİM", title: "Aktivite", description: "İş, alarm, kimlik ve servis hesabı olaylarının zaman çizelgesi" },
  audit: { eyebrow: "YÖNETİŞİM", title: "Denetim izi", description: "İzin bazlı, her isteği kapsayan güvenlik denetim kaydı" },
  settings: { eyebrow: "SİSTEM", title: "Ayarlar", description: "Hub ve arayüz tercihleri" },
}

export function pageFromPath(pathname: string): PageID {
  return [...navigation, ...secondaryNavigation].find((item) => item.path === pathname)?.id ?? "overview"
}
