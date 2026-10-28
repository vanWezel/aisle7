// Shapes of the JSON served by `aisle7 serve` (see server.go).

export type Service = { ID: number; Name: string; short?: string }
export type Config = { country: string; services: Service[] }
export type Genre = { Short: string; Name: string }
export type Country = { Code: string; Name: string }

export type Meta = { config: Config; configured: boolean; genres: Genre[]; countries: Country[] }

export type Stream = { name: string; url: string; mine: boolean }

export type Movie = {
  title: string
  year: number
  overview: string
  imdbId: string
  genres: string[] | null
  jwUrl: string
  poster: string
  imdb: number | null
  votes: number
  rt: number | null
  streaming: Stream[]
  rentBuy: boolean
  mine: boolean
  watched: boolean
  hidden: boolean
  listed: boolean
}

export type Results = { total: number; fetched: number; movies: Movie[] }

export async function send<T>(method: 'POST' | 'PUT', path: string, body: unknown): Promise<T> {
  const r = await fetch(path, { method, headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) })
  const j = await r.json()
  if (!r.ok) throw new Error(j.error ?? r.statusText)
  return j
}

export async function get<T>(path: string, signal?: AbortSignal): Promise<T> {
  const r = await fetch(path, { signal })
  const j = await r.json()
  if (!r.ok) throw new Error(j.error ?? r.statusText)
  return j
}
