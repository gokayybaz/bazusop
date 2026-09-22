import { useEffect, useState } from "react"
import QRCode from "qrcode"

export function TotpQrCode({ provisioningUri }: { provisioningUri: string }) {
  const [svg, setSvg] = useState("")
  const [failed, setFailed] = useState(false)

  useEffect(() => {
    let cancelled = false
    setSvg("")
    setFailed(false)
    QRCode.toString(provisioningUri, { type: "svg", margin: 1, width: 200 })
      .then((markup) => {
        if (!cancelled) setSvg(markup)
      })
      .catch(() => {
        if (!cancelled) setFailed(true)
      })
    return () => {
      cancelled = true
    }
  }, [provisioningUri])

  if (failed) {
    return (
      <p className="auth-error">
        QR kod oluşturulamadı. Doğrulayıcı uygulamanıza şu adresi elle ekleyin: {provisioningUri}
      </p>
    )
  }
  if (!svg) {
    return (
      <div className="totp-qr" role="status">
        QR kod oluşturuluyor…
      </div>
    )
  }
  return <div aria-label="Doğrulayıcı uygulaması QR kodu" className="totp-qr" dangerouslySetInnerHTML={{ __html: svg }} role="img" />
}
