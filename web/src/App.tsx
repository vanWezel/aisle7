import { useEffect, useMemo, useState, type ReactNode } from 'react'
import Settings from './Settings'
import { btn, field, select } from './ui'
import { get, send, type Meta, type Movie, type Results } from './types'

const RATINGS = [0, 6, 7, 8]
const STOCKS = [
  ['any', 'Any'],
  ['mine', 'In stock (my services)'],
  ['streaming', 'Streaming'],
  ['rental', 'Rental only'],
  ['both', 'Streaming + rental'],
  ['out', 'Out of stock'],
]
const SORTS = [
  ['popular', 'Popularity'],
  ['imdb', 'IMDb score'],
  ['rt', 'Rotten Tomatoes'],
]
const MIN_VOTES = 10000 // same cut-off as the terminal app for score sorts

type List = 'watched' | 'hidden' | 'watchlist'

// Mirrors rankMovies in tui.go.
function rank(movies: Movie[], sort: string) {
  if (sort === 'popular') return movies
  const key = sort === 'rt' ? 'rt' : 'imdb'
  return movies.filter((m) => m.votes >= MIN_VOTES).sort((a, b) => (b[key] ?? -1) - (a[key] ?? -1))
}

// The filters live in the address bar, so a bookmark or a shared link restores them.
// Defaults are left out to keep the URL short; unknown values fall back to the default.
const URL_PARAMS = new URLSearchParams(location.search)
const fromUrl = (key: string, allowed: string[], fallback: string) => {
  const v = URL_PARAMS.get(key)
  return v !== null && allowed.includes(v) ? v : fallback
}

const Field = ({ label, children }: { label: string; children: ReactNode }) => (
  <label className="flex flex-col gap-0.5 text-dim">
    {label}
    {children}
  </label>
)

const Check = ({ label, checked, onChange }: { label: string; checked: boolean; onChange: (v: boolean) => void }) => (
  <label className="flex items-center gap-2">
    <input type="checkbox" checked={checked} onChange={(e) => onChange(e.target.checked)} /> {label}
  </label>
)

const Pairs = ({ list }: { list: string[][] }) =>
  list.map(([v, l]) => (
    <option key={v} value={v}>
      {l}
    </option>
  ))

export default function App() {
  const [meta, setMeta] = useState<Meta>()
  const [settings, setSettings] = useState(false)
  const [f, setF] = useState({
    title: URL_PARAMS.get('title') ?? '',
    year: URL_PARAMS.get('year') ?? '',
    genre: URL_PARAMS.get('genre') ?? '',
    rating: Number(fromUrl('rating', RATINGS.map(String), '0')),
    stock: fromUrl(
      'stock',
      STOCKS.map(([v]) => v),
      'both',
    ),
    list: URL_PARAMS.get('list') === '1',
  })
  const [sort, setSort] = useState(
    fromUrl(
      'sort',
      SORTS.map(([v]) => v),
      'popular',
    ),
  )
  const [unseen, setUnseen] = useState(URL_PARAMS.get('unseen') !== '0')
  const [showHidden, setShowHidden] = useState(URL_PARAMS.get('hidden') === '1')
  const [res, setRes] = useState<Results>()
  const [loading, setLoading] = useState(false)
  const [err, setErr] = useState('')

  useEffect(() => {
    get<Meta>('/api/meta')
      .then((m) => {
        setMeta(m)
        setSettings(!m.configured)
      })
      .catch((e) => setErr(e.message))
  }, [])

  useEffect(() => {
    const params: Record<string, string> = {
      title: f.title,
      year: f.year,
      genre: f.genre,
      rating: f.rating ? String(f.rating) : '',
      stock: f.stock === 'both' ? '' : f.stock,
      list: f.list ? '1' : '',
      sort: sort === 'popular' ? '' : sort,
      unseen: unseen ? '' : '0',
      hidden: showHidden ? '1' : '',
    }
    const qs = new URLSearchParams(Object.entries(params).filter(([, v]) => v)).toString()
    history.replaceState(null, '', qs ? '?' + qs : location.pathname)
  }, [f, sort, unseen, showHidden])

  // Server-side filters: search again (debounced) whenever one changes.
  useEffect(() => {
    if (!meta) return
    const ac = new AbortController()
    const t = setTimeout(async () => {
      setLoading(true)
      setErr('')
      try {
        const p = new URLSearchParams({ ...f, rating: String(f.rating), list: f.list ? '1' : '' })
        setRes(await get<Results>('/api/search?' + p, ac.signal))
      } catch (e) {
        if (!ac.signal.aborted) setErr((e as Error).message)
      } finally {
        if (!ac.signal.aborted) setLoading(false)
      }
    }, 400)
    return () => {
      clearTimeout(t)
      ac.abort()
    }
  }, [meta, f])

  // Client-side filters: instant, no new search.
  const shown = useMemo(
    () =>
      rank(
        (res?.movies ?? []).filter((m) => (!f.list || m.listed) && !(unseen && m.watched) && (showHidden || !m.hidden)),
        sort,
      ),
    [res, f.list, unseen, showHidden, sort],
  )

  async function mark(m: Movie, list: List) {
    try {
      const { on } = await send<{ on: boolean }>('POST', '/api/mark', { list, title: m.title, year: m.year, imdbId: m.imdbId })
      const key = list === 'watchlist' ? 'listed' : list
      setRes((r) => r && { ...r, movies: r.movies.map((x) => (x === m ? { ...x, [key]: on } : x)) })
    } catch (e) {
      setErr((e as Error).message)
    }
  }

  function closeSettings() {
    setSettings(false)
    // Remember that first-run setup was seen, as the terminal app does.
    if (meta && !meta.configured) {
      send('PUT', '/api/config', meta.config).then(() => setMeta({ ...meta, configured: true }))
    }
  }

  const set = <K extends keyof typeof f>(k: K, v: (typeof f)[K]) => setF((p) => ({ ...p, [k]: v }))
  const localStock = f.stock === 'rental' || f.stock === 'out'
  const status = loading
    ? 'Searching…'
    : !res
      ? ''
      : localStock
        ? `${shown.length} shown of top ${res.fetched}`
        : `${res.total} matches`

  return (
    <div className="flex min-h-dvh flex-col bg-chrome">
      <header className="sticky top-0 z-10 flex items-center gap-4 bg-title px-2 py-2 sm:px-3">
        <h1 className="flex min-w-0 items-center gap-2">
          <img src="/logo.svg" alt="" className="size-8 shrink-0 [image-rendering:pixelated]" />
          <span className="font-display text-[.65rem] leading-snug tracking-widest whitespace-nowrap text-gold [text-shadow:2px_2px_0_var(--color-sh)]">
            AISLE 7
            <small className="mt-1.5 block font-body text-base tracking-[.35em] text-ink [text-shadow:none]">VIDEO GUIDE</small>
          </span>
        </h1>
        <span role="status" className="ml-auto hidden whitespace-nowrap sm:inline text-ink">
          {meta?.config.country} │ {status}
        </span>
        <button className={btn + ' py-0 max-sm:ml-auto'} onClick={() => setSettings(true)}>
          Settings
        </button>
      </header>

      <main className="flex w-full flex-1 flex-col gap-4 px-2 pt-4 pb-6 sm:gap-6 sm:px-3 sm:pt-6 sm:pb-8">
        <section className="flex flex-col gap-3">
          <h2 className="font-display text-xs leading-relaxed text-gold">TONIGHT'S GUIDE</h2>
          <p className="max-w-prose text-lg leading-relaxed text-ink">
            Where to watch a movie, with IMDb and Rotten Tomatoes scores. Choose your streaming services under Settings and the
            shelves show what is in stock for you.
          </p>
          <ul className="flex flex-wrap gap-x-6 gap-y-2 text-dim">
            {Object.values(SHELF).map(([label, color]) => (
              <li key={label} className="flex items-center gap-2">
                <span className={color}>●</span>
                {label}
              </li>
            ))}
          </ul>
        </section>

        <form
          className="bevel-raised grid grid-cols-2 gap-x-4 gap-y-3 bg-chrome p-2 sm:p-4 md:grid-cols-3 lg:grid-cols-[1.3fr_1fr_1fr_.7fr_1.4fr_1.2fr]"
          onSubmit={(e) => e.preventDefault()}
        >
          <Field label="Title">
            <input className={field} value={f.title} onChange={(e) => set('title', e.target.value)} placeholder="any title" />
          </Field>
          <Field label="Year">
            <input className={field} value={f.year} onChange={(e) => set('year', e.target.value)} placeholder="80s, 2010-2015" />
          </Field>
          <Field label="Genre">
            <select className={select} value={f.genre} onChange={(e) => set('genre', e.target.value)}>
              <option value="">Any</option>
              <Pairs list={(meta?.genres ?? []).map((g) => [g.Short, g.Name])} />
            </select>
          </Field>
          <Field label="IMDb rating">
            <select className={select} value={f.rating} onChange={(e) => set('rating', Number(e.target.value))}>
              <Pairs list={RATINGS.map((r) => [String(r), r ? `≥ ${r}` : 'Any'])} />
            </select>
          </Field>
          <Field label="Availability">
            <select className={select} value={f.stock} onChange={(e) => set('stock', e.target.value)}>
              <Pairs list={STOCKS} />
            </select>
          </Field>
          <Field label="Sort">
            <select className={select} value={sort} onChange={(e) => setSort(e.target.value)}>
              <Pairs list={SORTS} />
            </select>
          </Field>
          <div className="col-span-full flex flex-wrap gap-x-8 gap-y-2 pt-1">
            <Check label="Unseen only" checked={unseen} onChange={setUnseen} />
            <Check label="Show hidden" checked={showHidden} onChange={setShowHidden} />
            <Check label="Watchlist only" checked={f.list} onChange={(v) => set('list', v)} />
          </div>
        </form>

        <section className="flex flex-col gap-3">
          <h2 className="font-display text-xs leading-relaxed text-gold">ON THE SHELVES</h2>
          {err && (
            <p role="alert" className="text-bad">
              {err}
            </p>
          )}
          {!loading && res && shown.length === 0 && !err && <p className="text-dim">No results.</p>}
          <div className="bevel-sunken grid grid-cols-[repeat(auto-fill,minmax(min(100%,27rem),1fr))] content-start gap-2 bg-pane p-2 sm:gap-4 sm:p-4">
            {shown.map((m) => (
              <Card key={m.imdbId || m.title + m.year} m={m} onMark={mark} />
            ))}
          </div>
        </section>
      </main>

      {settings && meta && (
        <Settings
          meta={meta}
          onClose={closeSettings}
          onSaved={(config) => {
            setMeta({ ...meta, config, configured: true })
            setSettings(false)
          }}
        />
      )}
    </div>
  )
}

const SHELF = {
  mine: ['In stock', 'text-ok'],
  other: ['Other shelves', 'text-warn'],
  rent: ['Rental', 'text-rent'],
  out: ['Out of stock', 'text-bad'],
} as const

function Card({ m, onMark }: { m: Movie; onMark: (m: Movie, l: List) => void }) {
  const play = m.streaming.find((s) => s.mine && s.url) ?? m.streaming.find((s) => s.url)
  const [label, color] = SHELF[m.mine ? 'mine' : m.streaming.length ? 'other' : m.rentBuy ? 'rent' : 'out']
  const badge = 'px-1.5 text-pane'
  const bar = btn + ' flex-1 text-center whitespace-nowrap'
  const cover = m.poster ? (
    <img src={m.poster} alt="" loading="lazy" className="bevel-sunken aspect-[2/3] w-28 bg-chrome object-cover" />
  ) : (
    <div className="bevel-sunken aspect-[2/3] w-28 bg-chrome" />
  )
  return (
    <article className={'flex flex-col border border-hi bg-chrome/50' + (m.hidden ? ' opacity-50' : '')}>
      <h2 className="bg-title px-2 py-1 sm:px-4 text-xl font-normal text-gold">
        {m.title}
        {m.year > 0 && <span className="text-ink"> ({m.year})</span>}
      </h2>
      <div className="flex flex-1 gap-3 p-2 sm:gap-5 sm:p-4">
        {m.jwUrl ? (
          <a
            href={m.jwUrl}
            target="_blank"
            rel="noreferrer"
            aria-label={`${m.title} on JustWatch`}
            className="shrink-0 self-start hover:opacity-80"
          >
            {cover}
          </a>
        ) : (
          cover
        )}
        <div className="flex min-w-0 flex-1 flex-col gap-3">
          <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
            {m.imdbId ? (
              <a
                className={badge + ' bg-gold hover:underline'}
                href={`https://www.imdb.com/title/${m.imdbId}/`}
                target="_blank"
                rel="noreferrer"
              >
                IMDb {m.imdb ?? 'n/a'}
              </a>
            ) : (
              <span className={badge + ' bg-gold'}>IMDb {m.imdb ?? 'n/a'}</span>
            )}
            <span className={badge + ' bg-bad'}>RT {m.rt != null ? m.rt + '%' : 'n/a'}</span>
            <span className={color + " before:content-['●_']"}>{label}</span>
            {m.watched && <span className="text-dim">✓ Watched</span>}
            {m.listed && <span className="text-dim">+ Watchlist</span>}
          </div>
          {m.streaming.length > 0 && (
            <p className="flex flex-wrap gap-x-4 gap-y-1 text-dim">
              {m.streaming.map((s) => (
                <a key={s.name} href={s.url || undefined} target="_blank" rel="noreferrer" className={s.mine ? 'text-ok' : ''}>
                  {s.name}
                </a>
              ))}
            </p>
          )}
          <p className="line-clamp-4 leading-relaxed text-dim">{m.overview}</p>
        </div>
      </div>
      {/* The button bar spans the whole card; each row's buttons share its width. */}
      <div className="flex flex-col gap-3 border-t border-hi bg-chrome px-2 py-2 sm:px-4">
        {play && (
          <a className={bar + ' text-gold'} href={play.url} target="_blank" rel="noreferrer">
            ▶ Play on {play.name}
          </a>
        )}
        <div className="flex gap-3">
          <button className={bar} onClick={() => onMark(m, 'watched')}>
            {m.watched ? 'Unwatch' : 'Watched'}
          </button>
          <button className={bar} onClick={() => onMark(m, 'watchlist')}>
            {m.listed ? 'Unlist' : '+ Watchlist'}
          </button>
          <button className={bar} onClick={() => onMark(m, 'hidden')}>
            {m.hidden ? 'Unhide' : 'Hide'}
          </button>
        </div>
      </div>
    </article>
  )
}
