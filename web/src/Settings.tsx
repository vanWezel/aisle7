import { useEffect, useState } from 'react'
import { btn, field, select } from './ui'
import { get, send, type Config, type Meta, type Service } from './types'

type Props = { meta: Meta; onClose: () => void; onSaved: (c: Config) => void }

export default function Settings({ meta, onClose, onSaved }: Props) {
  const [country, setCountry] = useState(meta.config.country)
  const [services, setServices] = useState<Service[]>([])
  const [picked, setPicked] = useState(() => new Map(meta.config.services.map((s) => [s.ID, s])))
  const [filter, setFilter] = useState('')
  const [err, setErr] = useState('')
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    const ac = new AbortController()
    setLoading(true)
    setErr('')
    get<Service[]>('/api/services?country=' + country, ac.signal)
      .then((l) => setServices(l ?? []))
      .catch((e) => !ac.signal.aborted && setErr(e.message))
      .finally(() => !ac.signal.aborted && setLoading(false))
    return () => ac.abort()
  }, [country])

  const visible = services.filter((s) => s.Name.toLowerCase().includes(filter.trim().toLowerCase()))

  function toggle(s: Service) {
    const next = new Map(picked)
    if (!next.delete(s.ID)) next.set(s.ID, s)
    setPicked(next)
  }

  async function save() {
    try {
      const chosen = services.filter((s) => picked.has(s.ID))
      onSaved(await send<Config>('PUT', '/api/config', { country, services: chosen }))
    } catch (e) {
      setErr((e as Error).message)
    }
  }

  const label = 'flex flex-col gap-0.5 px-5 text-dim'
  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-label="Settings"
      className="fixed inset-0 z-20 grid place-items-center bg-black/70 p-2 sm:p-4"
    >
      <div className="bevel-raised flex max-h-full w-full max-w-md flex-col gap-5 overflow-auto bg-chrome pb-5">
        <h2 className="bg-title px-2 py-1.5 font-display text-[.6rem] leading-relaxed font-normal text-gold">YOUR VIDEO STORE</h2>
        {!meta.configured && <p className="px-5">Welcome! Pick your country and the services you subscribe to.</p>}
        <label className={label}>
          Country
          <select
            className={select}
            value={country}
            onChange={(e) => {
              setCountry(e.target.value)
              setPicked(new Map()) // service ids differ per country
            }}
          >
            {meta.countries.map((c) => (
              <option key={c.Code} value={c.Code}>
                {c.Name} ({c.Code})
              </option>
            ))}
          </select>
        </label>
        <label className={label}>
          Streaming services you subscribe to ({picked.size} selected)
          <input className={field} value={filter} onChange={(e) => setFilter(e.target.value)} placeholder="type to filter" />
        </label>
        <ul className="bevel-sunken mx-5 max-h-[40vh] min-h-30 overflow-auto bg-pane p-2">
          {loading && <li>Loading services…</li>}
          {!loading && visible.length === 0 && <li>No services match.</li>}
          {visible.map((s) => (
            <li key={s.ID}>
              <label className="flex items-center gap-2 py-px">
                <input type="checkbox" checked={picked.has(s.ID)} onChange={() => toggle(s)} /> {s.Name}
              </label>
            </li>
          ))}
        </ul>
        {err && (
          <p role="alert" className="px-5 text-bad">
            {err}
          </p>
        )}
        <div className="flex gap-3 px-5">
          <button className={btn + ' text-gold'} onClick={save} disabled={loading}>
            OK
          </button>
          <button className={btn} onClick={onClose}>
            Cancel
          </button>
        </div>
      </div>
    </div>
  )
}
