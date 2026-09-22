export function RecoveryCodes({ codes }: { codes: string[] }) {
  function copyAll() {
    void navigator.clipboard?.writeText(codes.join("\n"))
  }

  return (
    <div className="recovery-codes">
      <p className="auth-warning">Bu kodlar yalnız bir kez gösterilir. Güvenli bir yere kaydedin.</p>
      <ul aria-label="Kurtarma kodları">
        {codes.map((code) => (
          <li key={code}>{code}</li>
        ))}
      </ul>
      <button onClick={copyAll} type="button">
        Tümünü kopyala
      </button>
    </div>
  )
}
