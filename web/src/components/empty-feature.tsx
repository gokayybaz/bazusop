import type { ComponentType } from "react"

import { Card } from "./ui/card"

export function EmptyFeature({ icon: Icon, title, text }: { icon: ComponentType<{ size?: number }>; title: string; text: string }) {
  return <Card className="empty-feature page-card"><span><Icon size={24} /></span><h2>{title}</h2><p>{text}</p></Card>
}
