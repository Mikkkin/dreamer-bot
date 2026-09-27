import { useCallback, useEffect, useRef, useState } from 'react'
import type { ApiClient } from '../api/client'
import { ApiError } from '../api/errors'
import type { ApiImage, ImageOwner } from '../api/types'
import { prepareUpload } from './downscale'

export type PhotoTile =
  | { key: string; kind: 'saved'; image: ApiImage }
  | { key: string; kind: 'new'; blob: Blob; preview: string; progress: number | null; failed: boolean }

export interface PhotoDraft {
  tiles: PhotoTile[]
  /** Files still being decoded and re-encoded. */
  preparing: number
  /** Free slots under the per-entity limit. */
  room: number
  dirty: boolean
  add(files: readonly File[]): Promise<{ skipped: number }>
  remove(key: string): void
  /** Deletes removed photos, then uploads new ones one by one. Returns the upload errors. */
  sync(api: ApiClient, owner: ImageOwner, ownerId: number): Promise<ApiError[]>
}

const savedTile = (image: ApiImage): PhotoTile => ({ key: `img-${image.id}`, kind: 'saved', image })

/** Local state of the photo grid in a form: previews, removals, upload progress. */
export function usePhotoDraft(initial: readonly ApiImage[], max: number): PhotoDraft {
  const [tiles, setTiles] = useState<PhotoTile[]>(() => initial.map(savedTile))
  const [removed, setRemoved] = useState<number[]>([])
  const [preparing, setPreparing] = useState(0)
  const previews = useRef(new Set<string>())
  const seq = useRef(0)
  const alive = useRef(true)

  useEffect(() => {
    alive.current = true
    const urls = previews.current
    return () => {
      alive.current = false
      for (const url of urls) URL.revokeObjectURL(url)
      urls.clear()
    }
  }, [])

  const revoke = (url: string) => {
    URL.revokeObjectURL(url)
    previews.current.delete(url)
  }

  const room = Math.max(0, max - tiles.length - preparing)

  const add = useCallback(
    async (files: readonly File[]) => {
      const accepted = files.slice(0, room)
      setPreparing((n) => n + accepted.length)
      for (const file of accepted) {
        const blob = await prepareUpload(file)
        if (!alive.current) return { skipped: 0 }
        const preview = URL.createObjectURL(blob)
        previews.current.add(preview)
        seq.current += 1
        const tile: PhotoTile = { key: `new-${seq.current}`, kind: 'new', blob, preview, progress: null, failed: false }
        setTiles((list) => [...list, tile])
        setPreparing((n) => n - 1)
      }
      return { skipped: files.length - accepted.length }
    },
    [room],
  )

  const remove = (key: string) => {
    const tile = tiles.find((t) => t.key === key)
    if (!tile) return
    if (tile.kind === 'saved') {
      const id = tile.image.id
      setRemoved((ids) => (ids.includes(id) ? ids : [...ids, id]))
    } else {
      revoke(tile.preview)
    }
    setTiles((list) => list.filter((t) => t.key !== key))
  }

  const patchTile = (key: string, patch: Partial<Extract<PhotoTile, { kind: 'new' }>>) =>
    setTiles((list) => list.map((t) => (t.key === key && t.kind === 'new' ? { ...t, ...patch } : t)))

  const sync = async (api: ApiClient, owner: ImageOwner, ownerId: number): Promise<ApiError[]> => {
    const errors: ApiError[] = []
    const stillRemoved: number[] = []
    // Deletions go first so that replacing photos never trips the per-entity limit.
    for (const id of removed) {
      try {
        await api.deleteImage(owner, ownerId, id)
      } catch (err) {
        if (err instanceof ApiError && err.code === 'not_found') continue
        stillRemoved.push(id)
        errors.push(asApiError(err))
      }
    }
    setRemoved(stillRemoved)

    for (const tile of tiles) {
      if (tile.kind !== 'new') continue
      patchTile(tile.key, { progress: 0, failed: false })
      try {
        const image = await api.uploadImage(owner, ownerId, tile.blob, (p) => patchTile(tile.key, { progress: p }))
        revoke(tile.preview)
        setTiles((list) => list.map((t) => (t.key === tile.key ? savedTile(image) : t)))
      } catch (err) {
        patchTile(tile.key, { progress: null, failed: true })
        errors.push(asApiError(err))
      }
    }
    return errors
  }

  return {
    tiles,
    preparing,
    room,
    dirty: removed.length > 0 || tiles.some((t) => t.kind === 'new'),
    add,
    remove,
    sync,
  }
}

function asApiError(err: unknown): ApiError {
  return err instanceof ApiError ? err : new ApiError(0, 'network', 'Нет соединения')
}
