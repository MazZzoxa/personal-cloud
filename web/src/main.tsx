import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { createRoot } from 'react-dom/client'
import './styles.css'

type Entry = {
  id: number
  name: string
  path: string
  kind: 'file' | 'folder'
  size: number
  mimeType: string
  extension: string
  category: string
  preview: '' | 'image' | 'video' | 'audio' | 'pdf' | 'text'
  createdAt: string
  updatedAt: string
}

type SearchHit = Entry & { match?: 'name' | 'path' | 'content'; snippet?: string; line?: number }
type SearchResponse = { items: SearchHit[]; terms: string[]; total: number; truncated: boolean; partial: boolean }
type TextPreviewData = { name: string; path: string; content: string; size: number; encoding: string; lines: number; truncated: boolean }

type ApiResponse = { path: string; items: Entry[] }
type Breadcrumb = { label: string; path: string }
type UploadState = { name: string; loaded: number; total: number; status: 'uploading' | 'done' | 'error' }

const formatBytes = (value: number) => {
  if (value < 1024) return `${value} B`
  const units = ['KB', 'MB', 'GB', 'TB']
  let size = value / 1024
  for (const unit of units) {
    if (size < 1024 || unit === 'TB') return `${size.toFixed(size >= 10 ? 0 : 1)} ${unit}`
    size /= 1024
  }
  return `${value} B`
}

const formatDate = (value: string) => {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return '—'
  return date.toLocaleString()
}

const formatType = (entry: Entry) => {
  if (entry.kind === 'folder') return 'Папка'
  if (entry.extension) return entry.extension.toUpperCase()
  if (entry.mimeType) return entry.mimeType
  return 'Файл'
}

const breadcrumbs = (path: string): Breadcrumb[] => {
  const result: Breadcrumb[] = [{ label: 'Моё облако', path: '' }]
  if (!path) return result
  let current = ''
  for (const part of path.split('/').filter(Boolean)) {
    current = current ? `${current}/${part}` : part
    result.push({ label: part, path: current })
  }
  return result
}

const requestJSON = async (url: string, options?: RequestInit) => {
  const response = await fetch(url, options)
  const data = await response.json().catch(() => ({}))
  if (!response.ok) throw new Error(data.error ?? 'Произошла ошибка')
  return data
}

// v0.7.0 — search highlighting and file preview helpers ----------------------

// Same folding as the server: lower case, and «ё» is treated as «е».
const foldChar = (char: string) => {
  const lower = char.toLowerCase()
  if (Array.from(lower).length !== 1) return char
  return lower === 'ё' ? 'е' : lower
}

const highlightText = (text: string, terms: string[]): React.ReactNode => {
  if (!terms.length || !text) return text
  const chars = Array.from(text)
  const folded = chars.map(foldChar)
  const marked = new Array<boolean>(chars.length).fill(false)
  for (const term of terms) {
    const needle = Array.from(term)
    if (!needle.length) continue
    for (let start = 0; start + needle.length <= folded.length; start += 1) {
      let matches = true
      for (let offset = 0; offset < needle.length; offset += 1) {
        if (folded[start + offset] !== needle[offset]) { matches = false; break }
      }
      if (matches) for (let offset = 0; offset < needle.length; offset += 1) marked[start + offset] = true
    }
  }
  if (!marked.some(Boolean)) return text
  const parts: React.ReactNode[] = []
  let index = 0
  while (index < chars.length) {
    let end = index
    while (end < chars.length && marked[end] === marked[index]) end += 1
    const piece = chars.slice(index, end).join('')
    parts.push(marked[index] ? <mark key={index}>{piece}</mark> : piece)
    index = end
  }
  return parts
}

const previewUrl = (path: string) => `/api/files/preview?path=${encodeURIComponent(path)}`
const downloadUrl = (path: string) => `/api/files/download?path=${encodeURIComponent(path)}`

const SEARCH_TYPES: Array<[string, string]> = [
  ['all', 'Все типы'], ['folder', 'Папки'], ['image', 'Изображения'], ['video', 'Видео'], ['audio', 'Аудио'],
  ['document', 'Документы'], ['text', 'Текст'], ['code', 'Код'], ['archive', 'Архивы'], ['other', 'Прочее'],
]
const SEARCH_PERIODS: Array<[string, string]> = [
  ['any', 'За всё время'], ['day', 'За сутки'], ['week', 'За неделю'], ['month', 'За месяц'], ['year', 'За год'],
]
const SEARCH_SORTS: Array<[string, string]> = [
  ['relevance', 'По релевантности'], ['name', 'По имени'], ['size', 'По размеру'], ['modified', 'По дате изменения'], ['type', 'По типу'],
]

type PreviewModalProps = {
  entry: Entry
  siblings: Entry[]
  onNavigate: (entry: Entry) => void
  onClose: () => void
}

function PreviewModal({ entry, siblings, onNavigate, onClose }: PreviewModalProps) {
  const [text, setText] = useState<TextPreviewData | null>(null)
  const [textError, setTextError] = useState('')
  const [mediaError, setMediaError] = useState(false)
  const [zoomed, setZoomed] = useState(false)
  const rootRef = useRef<HTMLDivElement | null>(null)

  const index = siblings.findIndex((item) => item.path === entry.path)
  const previous = index > 0 ? siblings[index - 1] : null
  const next = index >= 0 && index < siblings.length - 1 ? siblings[index + 1] : null

  useEffect(() => {
    setText(null)
    setTextError('')
    setMediaError(false)
    setZoomed(false)
    if (entry.preview !== 'text') return
    const controller = new AbortController()
    void (async () => {
      try {
        const response = await fetch(`/api/files/preview/text?path=${encodeURIComponent(entry.path)}`, { signal: controller.signal })
        const data = await response.json().catch(() => ({}))
        if (!response.ok) throw new Error(data.error ?? 'Не удалось открыть файл')
        setText(data as TextPreviewData)
      } catch (err) {
        if (controller.signal.aborted) return
        setTextError(err instanceof Error ? err.message : 'Не удалось открыть файл')
      }
    })()
    return () => controller.abort()
  }, [entry.path, entry.preview])

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      const target = event.target as HTMLElement | null
      if (target?.closest('video, audio')) {
        if (event.key === 'Escape') onClose()
        return
      }
      if (event.key === 'Escape') onClose()
      else if (event.key === 'ArrowLeft' && previous) onNavigate(previous)
      else if (event.key === 'ArrowRight' && next) onNavigate(next)
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose, onNavigate, previous, next])

  useEffect(() => {
    rootRef.current?.focus()
    const previousOverflow = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    return () => { document.body.style.overflow = previousOverflow }
  }, [])

  const url = previewUrl(entry.path)
  const lineNumbers = useMemo(() => {
    if (!text) return ''
    return Array.from({ length: Math.max(text.lines, 1) }, (_, i) => i + 1).join('\n')
  }, [text])

  const unavailable = (message: string) => (
    <div className="preview-fallback">
      <strong>{message}</strong>
      <span>Файл можно скачать и открыть на устройстве.</span>
      <a className="primary" href={downloadUrl(entry.path)} download={entry.name}>Скачать</a>
    </div>
  )

  return (
    <div className="preview-overlay" onClick={onClose} role="dialog" aria-modal="true" aria-label={`Предпросмотр: ${entry.name}`}>
      <div className="preview-window" ref={rootRef} tabIndex={-1} onClick={(event) => event.stopPropagation()}>
        <header className="preview-header">
          <div className="preview-title">
            <strong title={entry.path}>{entry.name}</strong>
            <small>
              {formatBytes(entry.size)} · {formatType(entry)}
              {index >= 0 && siblings.length > 1 ? ` · ${index + 1} из ${siblings.length}` : ''}
            </small>
          </div>
          <div className="preview-actions">
            <button className="icon-button" onClick={() => previous && onNavigate(previous)} disabled={!previous} title="Предыдущий (←)" aria-label="Предыдущий файл">‹</button>
            <button className="icon-button" onClick={() => next && onNavigate(next)} disabled={!next} title="Следующий (→)" aria-label="Следующий файл">›</button>
            {entry.preview !== 'text' && <a className="secondary preview-link" href={url} target="_blank" rel="noreferrer">Открыть в новой вкладке</a>}
            <a className="secondary preview-link" href={downloadUrl(entry.path)} download={entry.name}>Скачать</a>
            <button className="icon-button" onClick={onClose} title="Закрыть (Esc)" aria-label="Закрыть предпросмотр">×</button>
          </div>
        </header>
        <div className={`preview-body preview-${entry.preview}`}>
          {entry.preview === 'image' && (mediaError ? unavailable('Не удалось показать изображение') : (
            <img
              src={url}
              alt={entry.name}
              className={zoomed ? 'zoomed' : ''}
              onClick={() => setZoomed((value) => !value)}
              onError={() => setMediaError(true)}
              title={zoomed ? 'Нажмите, чтобы уменьшить' : 'Нажмите, чтобы увеличить'}
            />
          ))}
          {entry.preview === 'video' && (mediaError ? unavailable('Браузер не смог воспроизвести это видео') : (
            <video src={url} controls preload="metadata" playsInline onError={() => setMediaError(true)} />
          ))}
          {entry.preview === 'audio' && (mediaError ? unavailable('Браузер не смог воспроизвести этот звук') : (
            <div className="audio-card">
              <div className="audio-icon">♪</div>
              <strong>{entry.name}</strong>
              <audio src={url} controls preload="metadata" onError={() => setMediaError(true)} />
            </div>
          ))}
          {entry.preview === 'pdf' && (
            <iframe src={url} title={entry.name} />
          )}
          {entry.preview === 'text' && (textError ? unavailable(textError) : !text ? (
            <div className="preview-status">Загрузка…</div>
          ) : (
            <div className="code-wrap">
              <div className="code-view">
                <pre className="code-gutter" aria-hidden="true">{lineNumbers}</pre>
                <pre className="code-body">{text.content}</pre>
              </div>
              <div className="code-meta">
                <span>{text.lines} строк</span>
                <span>Кодировка: {text.encoding}</span>
                {text.truncated && <span className="code-truncated">Показано начало файла (первые 512 КБ из {formatBytes(text.size)}) — скачайте файл, чтобы прочитать целиком</span>}
              </div>
            </div>
          ))}
        </div>
      </div>
    </div>
  )
}

// v0.4.0 — device identity ---------------------------------------------------

type DeviceInfo = {
  id: number
  name: string
  kind: 'host' | 'paired'
  userAgent: string
  createdAt: string
  lastSeenAt: string
  lastIp: string
  current: boolean
  online: boolean
}

type ConnectionRoute = {
  kind: 'lan' | 'tailscale'
  label: string
  url: string
  address: string
  available: boolean
}

type NetworkInfo = {
  current: 'host' | 'lan' | 'tailscale' | 'unknown'
  routes: ConnectionRoute[]
}

type AuthMe = { authenticated: boolean; isHost?: boolean; device?: DeviceInfo; version?: string }
type PairingInfo = {
  code: string
  expiresAt: string
  ttlSeconds: number
  urls: string[]
  routes?: ConnectionRoute[]
}

const UNAUTHORIZED_EVENT = 'pc-unauthorized'

// Any 401 from the API means this browser is no longer a trusted device:
// tell the app so it can return to the pairing screen.
const nativeFetch = window.fetch.bind(window)
window.fetch = async (input: RequestInfo | URL, init?: RequestInit) => {
  const response = await nativeFetch(input, init)
  if (response.status === 401) {
    const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url
    if (!url.includes('/api/auth/')) window.dispatchEvent(new Event(UNAUTHORIZED_EVENT))
  }
  return response
}

const deviceParts = (ua: string) => {
  const os = /Windows/i.test(ua) ? 'Windows'
    : /Android/i.test(ua) ? 'Android'
    : /iPhone/i.test(ua) ? 'iPhone'
    : /iPad/i.test(ua) ? 'iPad'
    : /Mac OS X|Macintosh/i.test(ua) ? 'macOS'
    : /Linux/i.test(ua) ? 'Linux'
    : ''
  const browser = /Edg\//.test(ua) ? 'Edge'
    : /OPR\/|Opera/.test(ua) ? 'Opera'
    : /Firefox\//.test(ua) ? 'Firefox'
    : /Chrome\//.test(ua) ? 'Chrome'
    : /Safari\//.test(ua) ? 'Safari'
    : ''
  return { os, browser }
}

const describeUserAgent = (ua: string) => {
  const { os, browser } = deviceParts(ua)
  return [browser, os].filter(Boolean).join(' · ') || 'Неизвестное устройство'
}

const guessDeviceName = () => {
  const { os, browser } = deviceParts(navigator.userAgent)
  return [os, browser].filter(Boolean).join(' ') || 'Новое устройство'
}

const formatPairCode = (value: string) => {
  const clean = value.toUpperCase().replace(/[^A-Z0-9]/g, '').slice(0, 8)
  return clean.length > 4 ? `${clean.slice(0, 4)}-${clean.slice(4)}` : clean
}


type ChatAttachment = {
  id: number
  name: string
  mimeType: string
  size: number
  url: string
}

type ChatMessage = {
  id: number
  body: string
  createdAt: string
  attachments: ChatAttachment[]
}

type ChatViewProps = {
  onOpenMenu: () => void
}

const isImageAttachment = (attachment: ChatAttachment) => attachment.mimeType.toLowerCase().startsWith('image/')

const isImageUrl = (url: string) => /\.(avif|gif|jpe?g|png|svg|webp)(?:[?#].*)?$/i.test(url)

const trimUrlPunctuation = (value: string) => {
  const match = value.match(/[.,!?;:'\")\]}]+$/)
  if (!match) return { url: value, trailing: '' }
  const trailing = match[0]
  return { url: value.slice(0, -trailing.length), trailing }
}

const renderMessageBody = (body: string) => {
  const urlPattern = /https?:\/\/[^\s]+/g
  const parts: React.ReactNode[] = []
  let cursor = 0
  let match: RegExpExecArray | null
  let key = 0

  while ((match = urlPattern.exec(body)) !== null) {
    if (match.index > cursor) {
      parts.push(<React.Fragment key={`text-${key++}`}>{body.slice(cursor, match.index)}</React.Fragment>)
    }

    const raw = match[0]
    const { url, trailing } = trimUrlPunctuation(raw)
    parts.push(
      <React.Fragment key={`url-${key++}`}>
        <a href={url} target="_blank" rel="noreferrer noopener">{url}</a>
        {isImageUrl(url) && (
          <img className="chat-link-image" src={url} alt="Изображение по ссылке" loading="lazy" />
        )}
        {trailing}
      </React.Fragment>,
    )
    cursor = match.index + raw.length
  }

  if (cursor < body.length) {
    parts.push(<React.Fragment key={`text-${key++}`}>{body.slice(cursor)}</React.Fragment>)
  }

  if (!parts.length) return body

  return parts
}

function ChatView({ onOpenMenu }: ChatViewProps) {
  const [messages, setMessages] = useState<ChatMessage[]>([])
  const [loading, setLoading] = useState(true)
  const [loadingOlder, setLoadingOlder] = useState(false)
  const [hasMore, setHasMore] = useState(false)
  const [body, setBody] = useState('')
  const [attachments, setAttachments] = useState<File[]>([])
  const [sending, setSending] = useState(false)
  const [selectedIds, setSelectedIds] = useState<Set<number>>(new Set())
  const [editingId, setEditingId] = useState<number | null>(null)
  const [editBody, setEditBody] = useState('')
  const [savingEdit, setSavingEdit] = useState(false)
  const [actionBusy, setActionBusy] = useState(false)
  const [connection, setConnection] = useState<'connecting' | 'connected' | 'reconnecting'>('connecting')
  const [error, setError] = useState('')
  const socketRef = useRef<WebSocket | null>(null)
  const reconnectTimerRef = useRef<number | null>(null)
  const stoppedRef = useRef(false)
  const messagesRef = useRef<HTMLDivElement | null>(null)
  const chatSelectionAnchorRef = useRef<number | null>(null)

  const mergeMessages = useCallback((incoming: ChatMessage[]) => {
    setMessages((previous) => {
      const byId = new Map(previous.map((message) => [message.id, message]))
      incoming.forEach((message) => byId.set(message.id, message))
      return Array.from(byId.values()).sort((a, b) => a.id - b.id)
    })
  }, [])

  const toggleMessageSelection = (id: number, withShift = false) => {
    setSelectedIds((current) => {
      if (withShift && chatSelectionAnchorRef.current !== null) {
        const anchorIndex = messages.findIndex((message) => message.id === chatSelectionAnchorRef.current)
        const targetIndex = messages.findIndex((message) => message.id === id)

        if (anchorIndex !== -1 && targetIndex !== -1) {
          const start = Math.min(anchorIndex, targetIndex)
          const end = Math.max(anchorIndex, targetIndex)
          const next = new Set(current)
          for (let index = start; index <= end; index += 1) {
            next.add(messages[index].id)
          }
          return next
        }
      }

      const next = new Set(current)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })

    if (!withShift || chatSelectionAnchorRef.current === null) {
      chatSelectionAnchorRef.current = id
    }
  }

  const clearMessageSelection = () => {
    setSelectedIds(new Set())
    chatSelectionAnchorRef.current = null
    setEditingId(null)
    setEditBody('')
  }

  const selectedMessages = useMemo(
    () => messages.filter((message) => selectedIds.has(message.id)),
    [messages, selectedIds],
  )

  const copySelectedMessages = async () => {
    if (!selectedMessages.length || actionBusy) return
    const text = selectedMessages.map((message) => {
      const lines = message.body ? [message.body] : []
      if (message.attachments.length) {
        lines.push(`Вложения: ${message.attachments.map((attachment) => attachment.name).join(', ')}`)
      }
      return lines.join('\n')
    }).join('\n\n')
    if (!text) return
    setActionBusy(true)
    setError('')
    try {
      if (navigator.clipboard?.writeText) {
        await navigator.clipboard.writeText(text)
      } else {
        const helper = document.createElement('textarea')
        helper.value = text
        helper.style.position = 'fixed'
        helper.style.opacity = '0'
        document.body.appendChild(helper)
        helper.focus()
        helper.select()
        if (!document.execCommand('copy')) throw new Error('copy failed')
        helper.remove()
      }
    } catch {
      setError('Не удалось скопировать сообщения')
    } finally {
      setActionBusy(false)
    }
  }

  const downloadSelectedMessages = async () => {
    if (!selectedMessages.length || actionBusy) return
    setActionBusy(true)
    setError('')
    try {
      const response = await fetch('/api/chat/download', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ ids: selectedMessages.map((message) => message.id) }),
      })
      if (!response.ok) {
        const data = await response.json().catch(() => ({}))
        throw new Error(data.error ?? 'Не удалось скачать сообщения')
      }
      const blob = await response.blob()
      const url = URL.createObjectURL(blob)
      const anchor = document.createElement('a')
      anchor.href = url
      anchor.download = 'personal-cloud-chat.zip'
      document.body.appendChild(anchor)
      anchor.click()
      anchor.remove()
      URL.revokeObjectURL(url)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Не удалось скачать сообщения')
    } finally {
      setActionBusy(false)
    }
  }

  const beginEditSelected = () => {
    if (selectedMessages.length !== 1) return
    const message = selectedMessages[0]
    if (!message.body) return
    setEditingId(message.id)
    setEditBody(message.body)
    window.requestAnimationFrame(() => {
      document.getElementById('chat-edit-textarea')?.focus()
    })
  }

  const cancelEdit = () => {
    setEditingId(null)
    setEditBody('')
  }

  const saveEdit = async () => {
    if (editingId === null || savingEdit) return
    const cleanBody = editBody.trim()
    if (!cleanBody) return
    setSavingEdit(true)
    setError('')
    try {
      const updated = await requestJSON(`/api/chat/messages/${editingId}`, {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ body: cleanBody }),
      }) as ChatMessage
      mergeMessages([updated])
      cancelEdit()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Не удалось изменить сообщение')
    } finally {
      setSavingEdit(false)
    }
  }

  const deleteSelectedMessages = async () => {
    if (!selectedMessages.length || actionBusy) return
    const label = selectedMessages.length === 1 ? 'это сообщение' : `эти ${selectedMessages.length} сообщения`
    if (!window.confirm(`Удалить ${label}? Отменить действие нельзя.`)) return
    setActionBusy(true)
    setError('')
    try {
      const response = await fetch('/api/chat/delete', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ ids: selectedMessages.map((message) => message.id) }),
      })
      if (!response.ok) {
        const data = await response.json().catch(() => ({}))
        throw new Error(data.error ?? 'Не удалось удалить сообщения')
      }
      setMessages((current) => current.filter((message) => !selectedIds.has(message.id)))
      clearMessageSelection()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Не удалось удалить сообщения')
    } finally {
      setActionBusy(false)
    }
  }

  const scrollToBottom = useCallback((behavior: ScrollBehavior = 'smooth') => {
    window.requestAnimationFrame(() => {
      const container = messagesRef.current
      if (!container) return
      container.scrollTo({ top: container.scrollHeight, behavior })
    })
  }, [])

  const hasSelection = selectedIds.size > 0
  useEffect(() => {
    // Панель действий занимает место над историей: если пользователь был у нижнего края,
    // оставляем нижние сообщения на виду, а не прячем их под сжавшимся списком.
    const container = messagesRef.current
    if (!container) return
    const distance = container.scrollHeight - container.scrollTop - container.clientHeight
    if (distance < 160) container.scrollTo({ top: container.scrollHeight })
  }, [hasSelection])

  const loadMessages = useCallback(async () => {
    setLoading(true)
    setError('')
    try {
      const data = await requestJSON('/api/chat/messages?limit=50') as { items: ChatMessage[]; hasMore: boolean }
      mergeMessages(data.items)
      setHasMore(data.hasMore)
      scrollToBottom('auto')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Не удалось загрузить историю чата')
    } finally {
      setLoading(false)
    }
  }, [scrollToBottom])

  useEffect(() => {
    void loadMessages()
  }, [loadMessages])

  useEffect(() => {
    stoppedRef.current = false

    const connect = () => {
      if (stoppedRef.current) return
      setConnection((current) => current === 'connected' ? 'reconnecting' : 'connecting')
      const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
      const socket = new WebSocket(`${protocol}//${window.location.host}/api/chat/ws`)
      socketRef.current = socket

      socket.onopen = () => {
        setConnection('connected')
      }

      socket.onmessage = (event) => {
        try {
          const data = JSON.parse(event.data) as { type?: string; message?: ChatMessage; messageId?: number; error?: string }
          if (data.type === 'message' && data.message) {
            const container = messagesRef.current
            const nearBottom = !container || container.scrollHeight - container.scrollTop - container.clientHeight < 120
            mergeMessages([data.message])
            if (nearBottom) scrollToBottom()
          }
          if (data.type === 'message_updated' && data.message) {
            mergeMessages([data.message])
          }
          if (data.type === 'message_deleted' && data.messageId) {
            setMessages((current) => current.filter((message) => message.id !== data.messageId))
            setSelectedIds((current) => {
              const next = new Set(current)
              next.delete(data.messageId as number)
              return next
            })
          }
          if (data.type === 'revoked') window.dispatchEvent(new Event(UNAUTHORIZED_EVENT))
          if (data.type === 'error' && data.error) setError(data.error)
        } catch {
          setError('Получено некорректное сообщение от сервера')
        }
      }

      socket.onclose = () => {
        if (stoppedRef.current) return
        setConnection('reconnecting')
        reconnectTimerRef.current = window.setTimeout(connect, 2500)
      }

      socket.onerror = () => {
        socket.close()
      }
    }

    connect()
    return () => {
      stoppedRef.current = true
      if (reconnectTimerRef.current !== null) window.clearTimeout(reconnectTimerRef.current)
      reconnectTimerRef.current = null
      socketRef.current?.close()
      socketRef.current = null
    }
  }, [mergeMessages, scrollToBottom])

  const loadOlder = async () => {
    if (loadingOlder || !hasMore || !messages.length) return
    const before = messages[0].id
    const container = messagesRef.current
    const previousHeight = container?.scrollHeight ?? 0
    const previousTop = container?.scrollTop ?? 0
    setLoadingOlder(true)
    try {
      const data = await requestJSON(`/api/chat/messages?limit=50&before=${before}`) as { items: ChatMessage[]; hasMore: boolean }
      mergeMessages(data.items)
      setHasMore(data.hasMore)
      window.requestAnimationFrame(() => {
        if (!container) return
        container.scrollTop = container.scrollHeight - previousHeight + previousTop
      })
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Не удалось загрузить старые сообщения')
    } finally {
      setLoadingOlder(false)
    }
  }

  const sendMessage = async () => {
    const cleanBody = body.trim()
    if ((!cleanBody && !attachments.length) || sending) return
    setSending(true)
    setError('')
    try {
      const form = new FormData()
      form.append('body', cleanBody)
      attachments.forEach((file) => form.append('files', file))
      const message = await requestJSON('/api/chat/messages', { method: 'POST', body: form }) as ChatMessage
      mergeMessages([message])
      setBody('')
      setAttachments([])
      scrollToBottom()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Не удалось отправить сообщение')
    } finally {
      setSending(false)
    }
  }

  const removeAttachment = (index: number) => {
    setAttachments((current) => current.filter((_, currentIndex) => currentIndex !== index))
  }

  return (
    <div className="chat-view">
      <header className="topbar chat-topbar">
        <div className="topbar-title">
          <button className="mobile-menu-button" onClick={onOpenMenu} aria-label="Открыть меню">☰</button>
          <div className="title-copy">
            <h1>Чат</h1>
            <p>Личная переписка между вашими устройствами</p>
          </div>
        </div>
        <div className={`chat-connection ${connection}`}>
          <span />
          {connection === 'connected' ? 'Подключён' : connection === 'connecting' ? 'Подключение…' : 'Переподключение…'}
        </div>
      </header>

      <section className="chat-card">
        {selectedMessages.length > 0 && (
          <div className="chat-selection-toolbar">
            <div className="chat-selection-count">
              <strong>Выбрано: {selectedMessages.length}</strong>
              <span>{selectedMessages.reduce((sum, message) => sum + message.attachments.length, 0)} вложений</span>
            </div>
            <span className="chat-selection-hint">Shift + клик — выделить диапазон</span>
            <div className="chat-selection-actions">
              <button onClick={() => void copySelectedMessages()} disabled={actionBusy || selectedMessages.every((message) => !message.body)} title="Скопировать текст выделенных сообщений">Копировать</button>
              <button onClick={() => void downloadSelectedMessages()} disabled={actionBusy} title="Скачать сообщения и вложения одним ZIP-архивом">Скачать</button>
              <button onClick={beginEditSelected} disabled={actionBusy || selectedMessages.length !== 1 || !selectedMessages[0]?.body} title="Изменить выбранное сообщение">Изменить</button>
              <button className="danger" onClick={() => void deleteSelectedMessages()} disabled={actionBusy}>Удалить</button>
              <button className="chat-selection-clear" onClick={clearMessageSelection} disabled={actionBusy}>Отменить</button>
            </div>
          </div>
        )}
        {error && <div className="error-banner">{error}</div>}

        <div className="chat-history" ref={messagesRef}>
          {hasMore && (
            <button className="chat-load-more" onClick={() => void loadOlder()} disabled={loadingOlder}>
              {loadingOlder ? 'Загрузка…' : 'Загрузить предыдущие сообщения'}
            </button>
          )}
          {loading ? (
            <div className="empty-state chat-empty-state">Загрузка истории…</div>
          ) : messages.length === 0 ? (
            <div className="empty-state chat-empty-state">
              <div className="empty-icon">⌁</div>
              <strong>История пока пуста</strong>
              <span>Отправьте первое сообщение с любого подключённого устройства.</span>
            </div>
          ) : (
            messages.map((message) => {
              const isSelected = selectedIds.has(message.id)
              const isEditing = editingId === message.id
              return (
                <article className={`chat-message ${isSelected ? 'selected' : ''}`} key={message.id}>
                  <div className="chat-message-head">
                    <button
                      className={`chat-message-select ${isSelected ? 'checked' : ''}`}
                      onClick={(event) => toggleMessageSelection(message.id, event.shiftKey)}
                      aria-label={isSelected ? 'Снять выделение сообщения' : 'Выделить сообщение'}
                      aria-pressed={isSelected}
                    >
                      <span>✓</span>
                    </button>
                    <strong>Вы</strong>
                    <time>{formatDate(message.createdAt)}</time>
                  </div>
                  {isEditing ? (
                    <div className="chat-edit-box">
                      <textarea
                        id="chat-edit-textarea"
                        value={editBody}
                        onChange={(event) => setEditBody(event.target.value)}
                        maxLength={32768}
                        rows={4}
                      />
                      <div className="chat-edit-actions">
                        <button className="secondary" onClick={cancelEdit} disabled={savingEdit}>Отмена</button>
                        <button className="primary" onClick={() => void saveEdit()} disabled={savingEdit || !editBody.trim()}>
                          {savingEdit ? 'Сохранение…' : 'Сохранить'}
                        </button>
                      </div>
                    </div>
                  ) : (
                    <>
                      {message.body && <div className="chat-message-body">{renderMessageBody(message.body)}</div>}
                      {message.attachments.length > 0 && (
                        <div className="chat-attachments">
                          {message.attachments.map((attachment) => (
                            <div className="chat-attachment" key={attachment.id}>
                              {isImageAttachment(attachment) ? (
                                <a href={attachment.url} target="_blank" rel="noreferrer noopener" className="chat-image-link">
                                  <img src={attachment.url} alt={attachment.name} loading="lazy" />
                                </a>
                              ) : (
                                <a className="chat-file-link" href={attachment.url} download={attachment.name}>
                                  <span className="chat-file-icon">□</span>
                                  <span>
                                    <strong>{attachment.name}</strong>
                                    <small>{formatBytes(attachment.size)}</small>
                                  </span>
                                </a>
                              )}
                            </div>
                          ))}
                        </div>
                      )}
                    </>
                  )}
                </article>
              )
            })
          )}
        </div>

        <div className="chat-composer">
          {attachments.length > 0 && (
            <div className="chat-pending-attachments">
              {attachments.map((file, index) => (
                <div className="chat-pending-file" key={`${file.name}-${file.size}-${file.lastModified}-${index}`}>
                  <span>{file.name}</span>
                  <small>{formatBytes(file.size)}</small>
                  <button onClick={() => removeAttachment(index)} aria-label={`Убрать ${file.name}`}>×</button>
                </div>
              ))}
            </div>
          )}
          <div className="chat-input-row">
            <label className="chat-attach-button" title="Прикрепить файлы">
              ＋
              <input
                type="file"
                multiple
                hidden
                onChange={(event) => {
                  if (event.target.files) {
                    const selectedFiles: File[] = Array.from(event.target.files as FileList)
                    setAttachments((current) => [...current, ...selectedFiles].slice(0, 8))
                  }
                  event.currentTarget.value = ''
                }}
              />
            </label>
            <textarea
              value={body}
              onChange={(event) => setBody(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === 'Enter' && !event.shiftKey) {
                  event.preventDefault()
                  void sendMessage()
                }
              }}
              placeholder="Напишите сообщение…"
              rows={2}
              maxLength={32768}
              aria-label="Сообщение"
            />
            <button className="primary chat-send-button" onClick={() => void sendMessage()} disabled={sending || (!body.trim() && !attachments.length)}>
              {sending ? 'Отправка…' : 'Отправить'}
            </button>
          </div>
          <div className="chat-composer-hint">Enter — отправить · Shift+Enter — новая строка · максимум 8 файлов по 100 МБ</div>
        </div>
      </section>
    </div>
  )
}

const connectionTargetUrl = (baseUrl: string) => {
  try {
    const target = new URL(baseUrl)
    target.pathname = window.location.pathname
    target.search = window.location.search
    if (window.location.hash) target.hash = window.location.hash
    return target.toString()
  } catch {
    return baseUrl
  }
}

type ConnectionSwitcherProps = {
  compact?: boolean
}

function ConnectionSwitcher({ compact = false }: ConnectionSwitcherProps) {
  const [network, setNetwork] = useState<NetworkInfo | null>(null)
  const [failed, setFailed] = useState(false)

  const load = useCallback(async () => {
    try {
      const data = await requestJSON('/api/network') as NetworkInfo
      setNetwork(data)
      setFailed(false)
    } catch {
      setFailed(true)
    }
  }, [])

  useEffect(() => {
    void load()
    const timer = window.setInterval(() => void load(), 15000)
    return () => window.clearInterval(timer)
  }, [load])

  const currentLabel =
    network?.current === 'tailscale' ? 'Tailscale'
      : network?.current === 'lan' ? 'Локальная сеть'
      : network?.current === 'host' ? 'Этот компьютер'
      : 'Не определено'

  return (
    <div className={`connection-switcher ${compact ? 'compact' : ''}`}>
      <div className="connection-switcher-head">
        <span>Подключение</span>
        <strong>{currentLabel}</strong>
      </div>

      {failed ? (
        <small className="connection-switcher-note">Не удалось определить доступные маршруты.</small>
      ) : network?.routes.length ? (
        <div className="connection-options">
          {network.routes.map((route) => (
            <a
              key={route.kind}
              className={`connection-option ${network.current === route.kind ? 'current' : ''}`}
              href={connectionTargetUrl(route.url)}
              title={`Открыть через ${route.label}`}
            >
              <span className="connection-option-main">
                <strong>{route.label}</strong>
                <small>{route.address}</small>
              </span>
              {network.current === route.kind && <span className="connection-option-state">Текущий</span>}
            </a>
          ))}
        </div>
      ) : (
        <small className="connection-switcher-note">Tailscale или LAN-адрес пока не обнаружены.</small>
      )}

      {!compact && (
        <small className="connection-switcher-note">
          Tailscale работает между устройствами одного tailnet и не открывает сервер всему публичному Интернету.
        </small>
      )}
    </div>
  )
}

function PairScreen({ onPaired }: { onPaired: () => void }) {
  const [code, setCode] = useState(() => {
    const match = window.location.hash.match(/pair=([A-Za-z0-9-]+)/)
    return match ? formatPairCode(match[1]) : ''
  })
  const [name, setName] = useState(guessDeviceName)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  const submit = async (event: React.FormEvent) => {
    event.preventDefault()
    if (busy) return
    setBusy(true)
    setError('')
    try {
      await requestJSON('/api/auth/pair', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ code, name }),
      })
      window.history.replaceState(null, '', window.location.pathname + window.location.search)
      onPaired()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Не удалось подключить устройство')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="pair-screen">
      <form className="pair-card" onSubmit={(event) => void submit(event)}>
        <div className="brand">
          <div className="brand-mark">PC</div>
          <div>
            <strong>Personal Cloud</strong>
            <span>Подключение устройства</span>
          </div>
        </div>
        <p className="pair-lead">Это устройство ещё не подключено к вашему облаку. Введите одноразовый код подключения.</p>

        <label className="pair-field">
          <span>Код подключения</span>
          <input
            className="pair-code-input"
            value={code}
            onChange={(event) => setCode(formatPairCode(event.target.value))}
            placeholder="XXXX-XXXX"
            autoComplete="off"
            autoCapitalize="characters"
            spellCheck={false}
            autoFocus
          />
        </label>

        <label className="pair-field">
          <span>Название устройства</span>
          <input value={name} onChange={(event) => setName(event.target.value)} maxLength={60} autoComplete="off" />
        </label>

        {error && <div className="pair-error">{error}</div>}

        <button className="primary" type="submit" disabled={busy || code.replace('-', '').length !== 8}>
          {busy ? 'Подключение…' : 'Подключить устройство'}
        </button>

        <small className="pair-help">
          Код создаётся в разделе «Устройства» на компьютере-хосте или на уже подключённом устройстве. Он действует 5 минут и работает один раз. Также код выводится в консоли сервера при запуске.
        </small>

        <ConnectionSwitcher />
      </form>
    </div>
  )
}

type DevicesViewProps = { onOpenMenu: () => void; onSignedOut: () => void }

function DevicesView({ onOpenMenu, onSignedOut }: DevicesViewProps) {
  const [devices, setDevices] = useState<DeviceInfo[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [pairing, setPairing] = useState<PairingInfo | null>(null)
  const [creating, setCreating] = useState(false)
  const [copied, setCopied] = useState(false)
  const [now, setNow] = useState(() => Date.now())
  const knownCountRef = useRef<number | null>(null)
  const pairingRef = useRef<PairingInfo | null>(null)
  pairingRef.current = pairing

  const load = useCallback(async () => {
    try {
      const data = await requestJSON('/api/devices') as { items: DeviceInfo[] }
      // A new device appeared while a code is shown: pairing succeeded.
      if (pairingRef.current && knownCountRef.current !== null && data.items.length > knownCountRef.current) {
        setPairing(null)
      }
      knownCountRef.current = data.items.length
      setDevices(data.items)
      setError('')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Не удалось загрузить устройства')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void load()
    const timer = window.setInterval(() => void load(), 8000)
    return () => window.clearInterval(timer)
  }, [load])

  useEffect(() => {
    if (!pairing) return
    const timer = window.setInterval(() => setNow(Date.now()), 1000)
    return () => window.clearInterval(timer)
  }, [pairing])

  const secondsLeft = pairing ? Math.max(0, Math.round((new Date(pairing.expiresAt).getTime() - now) / 1000)) : 0

  useEffect(() => {
    if (pairing && secondsLeft === 0) setPairing(null)
  }, [pairing, secondsLeft])

  const createCode = async () => {
    setCreating(true)
    setError('')
    setCopied(false)
    try {
      const data = await requestJSON('/api/devices/pairing', { method: 'POST' }) as PairingInfo
      setNow(Date.now())
      setPairing(data)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Не удалось создать код')
    } finally {
      setCreating(false)
    }
  }

  const pairingRoutes = pairing?.routes?.length
    ? pairing.routes
    : (pairing?.urls ?? []).map((url, index) => ({
      kind: index === 0 ? 'lan' as const : 'tailscale' as const,
      label: index === 0 ? 'Доступная сеть' : 'Дополнительный адрес',
      url: url.replace(/#pair=.*$/, ''),
      address: (() => {
        try { return new URL(url).hostname } catch { return '' }
      })(),
      available: true,
    }))

  const pairingLink = pairing
    ? connectionTargetUrl(
      `${(pairingRoutes?.[0]?.url ?? window.location.origin).replace(/\/$/, '')}/#pair=${pairing.code}`,
    )
    : ''

  const copyLink = async () => {
    try {
      await navigator.clipboard.writeText(pairingLink)
      setCopied(true)
      window.setTimeout(() => setCopied(false), 1800)
    } catch {
      setError('Не удалось скопировать ссылку — выделите и скопируйте её вручную')
    }
  }

  const rename = async (device: DeviceInfo) => {
    const name = window.prompt('Новое название устройства', device.name)?.trim()
    if (!name || name === device.name) return
    try {
      await requestJSON(`/api/devices/${device.id}`, {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name }),
      })
      await load()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Не удалось переименовать')
    }
  }

  const revoke = async (device: DeviceInfo) => {
    const message = device.current
      ? 'Отключить это устройство? Чтобы снова войти, понадобится новый код подключения.'
      : `Отозвать доступ у «${device.name}»? Устройство сразу потеряет доступ к облаку.`
    if (!window.confirm(message)) return
    try {
      const response = await fetch(`/api/devices/${device.id}`, { method: 'DELETE' })
      if (!response.ok && response.status !== 401) {
        const data = await response.json().catch(() => ({}))
        throw new Error(data.error ?? 'Не удалось отозвать доступ')
      }
      if (device.current) {
        onSignedOut()
        return
      }
      await load()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Не удалось отозвать доступ')
    }
  }

  return (
    <>
      <header className="topbar">
        <div className="topbar-title">
          <button className="mobile-menu-button" onClick={onOpenMenu} aria-label="Открыть меню">☰</button>
          <div className="title-copy">
            <h1>Устройства</h1>
            <p>Доверенные устройства и доступ к вашему облаку</p>
          </div>
        </div>
        <button className="icon-button" onClick={() => void load()} title="Обновить">↻</button>
      </header>

      <section className="toolbar">
        <button className="primary" onClick={() => void createCode()} disabled={creating}>
          ＋ {pairing ? 'Новый код' : 'Добавить устройство'}
        </button>
        <span className="selection-note">Подключённых устройств: {devices.length}</span>
      </section>

      {pairing && (
        <section className="pairing-card">
          <div className="pairing-head">
            <span>Код подключения</span>
            <strong className="pairing-timer">{Math.floor(secondsLeft / 60)}:{String(secondsLeft % 60).padStart(2, '0')}</strong>
          </div>
          <div className="pairing-code">{pairing.code}</div>
          <p>Откройте Personal Cloud на новом устройстве и введите код — или используйте один из доступных маршрутов:</p>
          <div className="pairing-routes">
            {(pairingRoutes ?? []).map((route) => (
              <a
                key={`${route.kind}-${route.url}`}
                className="pairing-route"
                href={connectionTargetUrl(`${route.url.replace(/\/$/, '')}/#pair=${pairing.code}`)}
              >
                <span>
                  <strong>{route.label}</strong>
                  <small>{route.address}</small>
                </span>
                <span>↗</span>
              </a>
            ))}
          </div>
          <div className="pairing-link">
            <code>{pairingLink}</code>
            <button className="secondary" onClick={() => void copyLink()}>{copied ? 'Скопировано' : 'Копировать'}</button>
          </div>
          <small>Код одноразовый. Новый код отменяет предыдущий.</small>
        </section>
      )}

      <section className="content-card">
        {error && <div className="error-banner">{error}</div>}
        {loading ? (
          <div className="empty-state">Загрузка…</div>
        ) : devices.length === 0 ? (
          <div className="empty-state"><strong>Нет устройств</strong></div>
        ) : (
          devices.map((device) => (
            <div className="device-row" key={device.id}>
              <span className={`device-icon ${device.kind}`}>{device.kind === 'host' ? '▣' : '◈'}</span>
              <div className="device-main">
                <div className="device-title">
                  <strong>{device.name}</strong>
                  {device.kind === 'host' && <span className="badge">Хост</span>}
                  {device.current && <span className="badge accent">Это устройство</span>}
                  {device.online && <span className="badge online">В сети</span>}
                </div>
                <small>
                  {device.kind === 'host' ? 'Компьютер, на котором запущен сервер' : describeUserAgent(device.userAgent)}
                  {device.lastIp ? ` · ${device.lastIp}` : ''}
                </small>
                <small>
                  Подключено: {formatDate(device.createdAt)} · Активность: {formatDate(device.lastSeenAt)}
                </small>
              </div>
              {device.kind === 'paired' && (
                <div className="device-actions">
                  <button className="secondary" onClick={() => void rename(device)}>Переименовать</button>
                  <button className="secondary danger-action" onClick={() => void revoke(device)}>
                    {device.current ? 'Отключить' : 'Отозвать'}
                  </button>
                </div>
              )}
            </div>
          ))
        )}
      </section>
    </>
  )
}

type AppSettings = {
  cloudName: string
  maxUploadMB: number
  logRetentionDays: number
}

type StorageInfo = {
  fileCount: number
  folderCount: number
  storageBytes: number
  chatBytes: number
  databaseBytes: number
  totalCloudBytes: number
  diskTotalBytes: number
  diskFreeBytes: number
  diskAvailable: boolean
}

type AuditLog = {
  id: number
  createdAt: string
  level: 'info' | 'warning' | 'error'
  event: string
  method: string
  path: string
  status: number
  deviceName: string
  ip: string
}

type InfoResponse = { name: string; version: string; files: number; messages: number }

type ManagementViewProps = { onOpenMenu: () => void }

function StorageView({ onOpenMenu }: ManagementViewProps) {
  const [info, setInfo] = useState<StorageInfo | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  const load = useCallback(async () => {
    setLoading(true)
    try {
      setInfo(await requestJSON('/api/storage') as StorageInfo)
      setError('')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Не удалось получить информацию о хранилище')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { void load() }, [load])

  const usedPercent = info && info.diskAvailable && info.diskTotalBytes > 0
    ? Math.min(100, Math.max(0, Math.round(((info.diskTotalBytes - info.diskFreeBytes) / info.diskTotalBytes) * 100)))
    : 0

  return (
    <div className="management-view">
      <header className="topbar">
        <div className="topbar-title">
          <button className="mobile-menu-button" onClick={onOpenMenu} aria-label="Открыть меню">☰</button>
          <div className="title-copy"><h1>Хранилище</h1><p>Занятое пространство и состояние локального диска</p></div>
        </div>
        <button className="icon-button" onClick={() => void load()} title="Обновить" disabled={loading}>↻</button>
      </header>

      {error && <div className="error-banner">{error}</div>}
      {loading && !info ? <div className="empty-state">Сбор информации о хранилище…</div> : info && (
        <>
          <section className="management-grid">
            <article className="metric-card">
              <span className="metric-icon">▣</span><small>Данные облака</small>
              <strong>{formatBytes(info.totalCloudBytes)}</strong>
              <p>Файлы, чат и база данных</p>
            </article>
            <article className="metric-card">
              <span className="metric-icon">□</span><small>Файлы</small>
              <strong>{info.fileCount.toLocaleString()}</strong>
              <p>{formatBytes(info.storageBytes)} занято</p>
            </article>
            <article className="metric-card">
              <span className="metric-icon">▰</span><small>Папки</small>
              <strong>{info.folderCount.toLocaleString()}</strong>
              <p>Внутри хранилища</p>
            </article>
          </section>

          <section className="management-panel">
            <div className="panel-heading">
              <div><h2>Диск компьютера</h2><p>Свободное место на том диске, где расположен Personal Cloud</p></div>
              <span className={`status-pill ${info.diskAvailable ? 'status-good' : 'status-muted'}`}>{info.diskAvailable ? 'Доступен' : 'Нет данных'}</span>
            </div>
            {info.diskAvailable ? (
              <>
                <div className="disk-summary"><strong>{formatBytes(info.diskFreeBytes)} свободно</strong><span>из {formatBytes(info.diskTotalBytes)}</span></div>
                <div className="disk-track"><div className="disk-fill" style={{ width: `${usedPercent}%` }} /></div>
                <div className="disk-foot"><span>Использовано на диске: {formatBytes(Math.max(0, info.diskTotalBytes - info.diskFreeBytes))}</span><strong>{usedPercent}%</strong></div>
              </>
            ) : <p className="muted-copy">Операционная система не предоставила сведения о вместимости диска. Размер данных Personal Cloud всё равно подсчитан ниже.</p>}
            <div className="storage-breakdown">
              <div><span>Файлы и папки</span><strong>{formatBytes(info.storageBytes)}</strong></div>
              <div><span>Вложения чата</span><strong>{formatBytes(info.chatBytes)}</strong></div>
              <div><span>База данных</span><strong>{formatBytes(info.databaseBytes)}</strong></div>
            </div>
          </section>
          <p className="management-footnote">Размеры считаются по локальным данным сервера. Свободное место отображается для текущего пользователя Windows/Linux и может немного меняться во время загрузок.</p>
        </>
      )}
    </div>
  )
}

function SettingsView({ onOpenMenu, onSaved }: ManagementViewProps & { onSaved: (settings: AppSettings) => void }) {
  const [settings, setSettings] = useState<AppSettings | null>(null)
  const [network, setNetwork] = useState<NetworkInfo | null>(null)
  const [version, setVersion] = useState('—')
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const [settingsData, infoData, networkData] = await Promise.all([
        requestJSON('/api/settings') as Promise<AppSettings>,
        requestJSON('/api/info') as Promise<InfoResponse>,
        requestJSON('/api/network') as Promise<NetworkInfo>,
      ])
      setSettings(settingsData)
      setVersion(infoData.version)
      setNetwork(networkData)
      setError('')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Не удалось загрузить настройки')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { void load() }, [load])

  const update = <K extends keyof AppSettings>(key: K, value: AppSettings[K]) => {
    setSettings((current) => current ? { ...current, [key]: value } : current)
    setNotice('')
  }

  const save = async (event: React.FormEvent) => {
    event.preventDefault()
    if (!settings || saving) return
    setSaving(true)
    setError('')
    setNotice('')
    try {
      const updated = await requestJSON('/api/settings', {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(settings),
      }) as AppSettings
      setSettings(updated)
      onSaved(updated)
      setNotice('Настройки сохранены.')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Не удалось сохранить настройки')
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="management-view">
      <header className="topbar">
        <div className="topbar-title">
          <button className="mobile-menu-button" onClick={onOpenMenu} aria-label="Открыть меню">☰</button>
          <div className="title-copy"><h1>Настройки</h1><p>Конфигурация Personal Cloud</p></div>
        </div>
        <button className="icon-button" onClick={() => void load()} title="Обновить" disabled={loading}>↻</button>
      </header>

      {error && <div className="error-banner">{error}</div>}
      {loading && !settings ? <div className="empty-state">Загрузка настроек…</div> : settings && (
        <>
          <form className="management-panel settings-form" onSubmit={(event) => void save(event)}>
            <div className="panel-heading"><div><h2>Основные параметры</h2><p>Изменения применяются сразу и сохраняются в базе данных</p></div></div>
            <label className="setting-field">
              <span>Название облака</span>
              <input value={settings.cloudName} onChange={(event) => update('cloudName', event.target.value)} maxLength={50} required />
              <small>Отображается в боковом меню и заголовке вкладки.</small>
            </label>
            <label className="setting-field">
              <span>Максимальный размер одного файла (МБ)</span>
              <input type="number" value={settings.maxUploadMB} onChange={(event) => update('maxUploadMB', Number(event.target.value))} min={1} max={51200} required />
              <small>От 1 до 51200 МБ. Применяется к загрузкам в файловое хранилище; вложения чата ограничены отдельно — 100 МБ на файл.</small>
            </label>
            <label className="setting-field">
              <span>Хранить журналы (дней)</span>
              <input type="number" value={settings.logRetentionDays} onChange={(event) => update('logRetentionDays', Number(event.target.value))} min={1} max={365} required />
              <small>Старые записи автоматически удаляются при следующем регистрируемом действии.</small>
            </label>
            {notice && <div className="success-message">{notice}</div>}
            <div className="settings-actions"><button className="primary" type="submit" disabled={saving || loading}>{saving ? 'Сохранение…' : 'Сохранить настройки'}</button></div>
          </form>

          <section className="management-panel">
            <div className="panel-heading"><div><h2>Сведения о сервере</h2><p>Текущая конфигурация подключения</p></div><span className="status-pill status-good">v{version}</span></div>
            <div className="route-list">
              {(network?.routes ?? []).map((route) => (
                <div className="route-row" key={`${route.kind}-${route.url}`}>
                  <span className={`route-dot ${route.available ? 'available' : ''}`} />
                  <div><strong>{route.label}</strong><small>{route.address}</small></div>
                  <span className={`status-pill ${route.available ? 'status-good' : 'status-muted'}`}>{route.available ? 'Доступен' : 'Недоступен'}</span>
                </div>
              ))}
              {!network?.routes.length && <p className="muted-copy">LAN- и Tailscale-маршруты пока не обнаружены.</p>}
            </div>
            <p className="management-footnote">Параметры сетевого интерфейса и порт сервера задаются при запуске. Для удалённого доступа используется Tailscale; Personal Cloud не открывает публичный туннель самостоятельно.</p>
          </section>
        </>
      )}
    </div>
  )
}

function LogsView({ onOpenMenu }: ManagementViewProps) {
  const [logs, setLogs] = useState<AuditLog[]>([])
  const [level, setLevel] = useState('all')
  const [searchInput, setSearchInput] = useState('')
  const [query, setQuery] = useState('')
  const [hasMore, setHasMore] = useState(false)
  const [loading, setLoading] = useState(true)
  const [clearing, setClearing] = useState(false)
  const [error, setError] = useState('')

  const load = useCallback(async (reset: boolean, before?: number) => {
    setLoading(true)
    try {
      const params = new URLSearchParams({ limit: '100' })
      if (level !== 'all') params.set('level', level)
      if (query) params.set('q', query)
      if (!reset && before) params.set('before', String(before))
      const data = await requestJSON(`/api/logs?${params.toString()}`) as { items: AuditLog[]; hasMore: boolean }
      setLogs((current) => reset ? data.items : [...current, ...data.items])
      setHasMore(data.hasMore)
      setError('')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Не удалось загрузить журнал')
    } finally {
      setLoading(false)
    }
  }, [level, query])

  useEffect(() => { void load(true) }, [load])

  const clear = async () => {
    if (!window.confirm('Удалить все текущие записи журнала? Это действие нельзя отменить.')) return
    setClearing(true)
    setError('')
    try {
      await requestJSON('/api/logs', { method: 'DELETE' })
      await load(true)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Не удалось очистить журнал')
    } finally {
      setClearing(false)
    }
  }

  const applySearch = (event: React.FormEvent) => {
    event.preventDefault()
    setQuery(searchInput.trim())
  }

  return (
    <div className="management-view">
      <header className="topbar">
        <div className="topbar-title">
          <button className="mobile-menu-button" onClick={onOpenMenu} aria-label="Открыть меню">☰</button>
          <div className="title-copy"><h1>Журнал событий</h1><p>Изменения, действия устройств и ошибки запросов</p></div>
        </div>
        <button className="icon-button" onClick={() => void load(true)} title="Обновить" disabled={loading}>↻</button>
      </header>

      <form className="logs-filters" onSubmit={applySearch}>
        <div className="search-box"><span>⌕</span><input value={searchInput} onChange={(event) => setSearchInput(event.target.value)} placeholder="Поиск по событиям, устройствам, IP…" aria-label="Поиск по журналу" /></div>
        <select value={level} onChange={(event) => setLevel(event.target.value)} aria-label="Уровень события">
          <option value="all">Все события</option><option value="info">Информация</option><option value="warning">Предупреждения</option><option value="error">Ошибки</option>
        </select>
        <button className="secondary" type="submit">Найти</button>
        <button className="secondary danger-action" type="button" onClick={() => void clear()} disabled={clearing}>{clearing ? 'Очистка…' : 'Очистить журнал'}</button>
      </form>

      {error && <div className="error-banner">{error}</div>}
      <section className="management-panel log-list">
        {loading && logs.length === 0 ? <div className="empty-state">Загрузка журнала…</div> : logs.length === 0 ? (
          <div className="empty-state"><strong>Записей пока нет</strong><span>События появятся после действий в облаке.</span></div>
        ) : logs.map((item) => (
          <article className="log-row" key={item.id}>
            <span className={`log-level log-${item.level}`}>{item.level === 'error' ? 'Ошибка' : item.level === 'warning' ? 'Внимание' : 'Инфо'}</span>
            <div className="log-main"><strong>{item.event}</strong><small>{formatDate(item.createdAt)} · {item.deviceName || 'Система'}{item.ip ? ` · ${item.ip}` : ''}</small><code>{item.method} {item.path}</code></div>
            <span className={`log-status ${item.status >= 400 ? 'failed' : ''}`}>{item.status}</span>
          </article>
        ))}
        {loading && logs.length > 0 && <div className="list-status">Обновление…</div>}
        {!loading && hasMore && logs.length > 0 && <div className="load-more-row"><button className="secondary" onClick={() => void load(false, logs[logs.length - 1]?.id)}>Загрузить более старые события</button></div>}
      </section>
      <p className="management-footnote">Записи хранятся локально в SQLite. По умолчанию срок хранения — 30 дней; его можно изменить в настройках. Пароли, коды сопряжения и содержимое файлов в журнал не записываются.</p>
    </div>
  )
}

// v0.8.0 — backup and recovery ------------------------------------------------

type BackupInfo = {
  name: string
  kind: string
  size: number
  modifiedAt: string
  createdAt: string
  version: string
  storageFiles: number
  chatFiles: number
  folderCount: number
  dataBytes: number
  valid: boolean
  error?: string
}

type BackupJob = {
  op: '' | 'backup' | 'restore' | 'verify'
  state: 'idle' | 'running' | 'done' | 'error'
  phase: string
  name: string
  done: number
  total: number
  error?: string
}

type BackupSettings = { dir: string; keep: number; intervalHours: number }

type BackupList = {
  items: BackupInfo[]
  dir: string
  defaultDir: string
  settings: BackupSettings
  job: BackupJob
  dirAvailable: boolean
  diskAvailable: boolean
  diskFreeBytes: number
}

const IDLE_JOB: BackupJob = { op: '', state: 'idle', phase: '', name: '', done: 0, total: 0 }

const BACKUP_KINDS: Record<string, string> = {
  manual: 'Вручную',
  auto: 'Автоматически',
  'pre-restore': 'Перед восстановлением',
  imported: 'Загружена',
}

const BACKUP_JOB_TITLES: Record<string, string> = {
  backup: 'Создание резервной копии',
  restore: 'Восстановление данных',
  verify: 'Проверка резервной копии',
}

const BACKUP_INTERVALS: Array<[number, string]> = [
  [0, 'Выключено'], [6, 'Каждые 6 часов'], [12, 'Каждые 12 часов'], [24, 'Раз в сутки'], [72, 'Раз в 3 дня'], [168, 'Раз в неделю'],
]

function BackupsView({ onOpenMenu }: ManagementViewProps) {
  const [data, setData] = useState<BackupList | null>(null)
  const [job, setJob] = useState<BackupJob>(IDLE_JOB)
  const [form, setForm] = useState<BackupSettings | null>(null)
  const [formDirty, setFormDirty] = useState(false)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [importing, setImporting] = useState(false)
  const [watching, setWatching] = useState(false)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const importInput = useRef<HTMLInputElement>(null)
  const formDirtyRef = useRef(false)
  formDirtyRef.current = formDirty

  const load = useCallback(async () => {
    try {
      const next = await requestJSON('/api/backups') as BackupList
      setData(next)
      setJob(next.job)
      if (next.job.state === 'running') setWatching(true)
      if (!formDirtyRef.current) setForm(next.settings)
      setError('')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Не удалось загрузить список резервных копий')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { void load() }, [load])

  // Poll the background job while something is running.
  useEffect(() => {
    if (job.state !== 'running') return
    const timer = window.setInterval(async () => {
      try {
        const next = await requestJSON('/api/backups/status') as BackupJob
        setJob(next)
        if (next.state !== 'running') await load()
      } catch {
        // The server may be busy swapping folders; try again on the next tick.
      }
    }, 1000)
    return () => window.clearInterval(timer)
  }, [job.state, load])

  // Report the result of an operation that this page started or watched.
  useEffect(() => {
    if (!watching) return
    if (job.state === 'done') {
      if (job.op === 'restore') {
        setNotice('Данные восстановлены из резервной копии. Страница сейчас перезагрузится…')
        window.setTimeout(() => window.location.reload(), 2500)
      } else if (job.op === 'backup') {
        setNotice(`Резервная копия создана: ${job.name}`)
      } else if (job.op === 'verify') {
        setNotice(`Копия проверена, повреждений не найдено: ${job.name}`)
      }
      setWatching(false)
      void load()
    } else if (job.state === 'error') {
      setError(job.error || 'Операция завершилась ошибкой')
      setWatching(false)
      void load()
    }
  }, [job, watching, load])

  const start = async (url: string, body?: unknown) => {
    setError('')
    setNotice('')
    try {
      const next = await requestJSON(url, {
        method: 'POST',
        ...(body === undefined ? {} : { headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) }),
      }) as BackupJob
      setJob(next)
      setWatching(true)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Не удалось запустить операцию')
    }
  }

  const restore = (item: BackupInfo) => {
    const created = formatDate(item.createdAt || item.modifiedAt)
    const message = `Восстановить данные из копии от ${created}?\n\n` +
      'Текущие файлы, чат и база данных будут заменены содержимым копии. ' +
      'Перед этим автоматически создаётся страховочная копия текущего состояния. ' +
      'Список подключённых устройств не меняется. Во время восстановления облако недоступно для записи.'
    if (!window.confirm(message)) return
    void start(`/api/backups/${encodeURIComponent(item.name)}/restore`, { confirm: true })
  }

  const remove = async (item: BackupInfo) => {
    if (!window.confirm(`Удалить резервную копию «${item.name}»? Это действие нельзя отменить.`)) return
    setError('')
    setNotice('')
    try {
      await requestJSON(`/api/backups/${encodeURIComponent(item.name)}`, { method: 'DELETE' })
      await load()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Не удалось удалить копию')
    }
  }

  const importFile = async (event: React.ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0]
    event.target.value = ''
    if (!file) return
    setImporting(true)
    setError('')
    setNotice('')
    try {
      const body = new FormData()
      body.append('file', file)
      await requestJSON('/api/backups/import', { method: 'POST', body })
      setNotice(`Копия «${file.name}» добавлена в список.`)
      await load()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Не удалось загрузить копию')
    } finally {
      setImporting(false)
    }
  }

  const updateForm = <K extends keyof BackupSettings>(key: K, value: BackupSettings[K]) => {
    setForm((current) => current ? { ...current, [key]: value } : current)
    setFormDirty(true)
    setNotice('')
  }

  const saveSettings = async (event: React.FormEvent) => {
    event.preventDefault()
    if (!form || saving) return
    setSaving(true)
    setError('')
    setNotice('')
    try {
      const saved = await requestJSON('/api/backups/settings', {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(form),
      }) as BackupSettings
      setForm(saved)
      setFormDirty(false)
      formDirtyRef.current = false
      setNotice('Параметры резервного копирования сохранены.')
      await load()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Не удалось сохранить параметры')
    } finally {
      setSaving(false)
    }
  }

  const running = job.state === 'running'
  const restoring = running && job.op === 'restore'
  const percent = job.total > 0 ? Math.min(100, Math.round((job.done / job.total) * 100)) : null
  const items = data?.items ?? []

  return (
    <div className="management-view">
      <header className="topbar">
        <div className="topbar-title">
          <button className="mobile-menu-button" onClick={onOpenMenu} aria-label="Открыть меню">☰</button>
          <div className="title-copy"><h1>Резервные копии</h1><p>Сохранение и восстановление файлов, чата и настроек</p></div>
        </div>
        <button className="icon-button" onClick={() => void load()} title="Обновить" disabled={loading || running}>↻</button>
      </header>

      {error && <div className="error-banner">{error}</div>}
      {notice && <div className="success-message backup-notice">{notice}</div>}

      {loading && !data ? <div className="empty-state">Загрузка списка копий…</div> : data && (
        <>
          <section className="management-panel">
            <div className="panel-heading">
              <div><h2>Создать копию</h2><p>Файлы, вложения чата, сообщения и настройки сохраняются в один архив</p></div>
              <span className={`status-pill ${data.dirAvailable ? 'status-good' : 'status-muted'}`}>{data.dirAvailable ? 'Папка доступна' : 'Папка недоступна'}</span>
            </div>
            <div className="backup-toolbar">
              <button className="primary" onClick={() => void start('/api/backups')} disabled={running || importing || !data.dirAvailable}>Создать резервную копию</button>
              <button className="secondary" onClick={() => importInput.current?.click()} disabled={running || importing || !data.dirAvailable}>{importing ? 'Загрузка…' : 'Загрузить копию (.zip)'}</button>
              <input ref={importInput} type="file" accept=".zip,application/zip" hidden onChange={(event) => void importFile(event)} />
            </div>
            {running && !restoring && (
              <div className="backup-progress">
                <div className="backup-progress-head"><strong>{BACKUP_JOB_TITLES[job.op] ?? 'Выполняется операция'}</strong><span>{job.phase}{percent !== null ? ` · ${percent}%` : ''}</span></div>
                <div className="disk-track"><div className={`disk-fill ${percent === null ? 'indeterminate' : ''}`} style={percent === null ? undefined : { width: `${percent}%` }} /></div>
                {job.total > 0 && <small>{formatBytes(job.done)} из {formatBytes(job.total)}</small>}
              </div>
            )}
            <div className="backup-location">
              <span>Папка копий</span>
              <code>{data.dir}</code>
              {data.diskAvailable && <small>Свободно на диске: {formatBytes(data.diskFreeBytes)}</small>}
            </div>
            {!data.dirAvailable && <p className="muted-copy">Папка резервных копий не найдена. Подключите диск или выберите другую папку ниже.</p>}
          </section>

          <section className="management-panel log-list">
            <div className="panel-heading backup-list-heading">
              <div><h2>Сохранённые копии</h2><p>{items.length ? `Всего: ${items.length}` : 'Копий пока нет'}</p></div>
            </div>
            {items.length === 0 ? (
              <div className="empty-state"><strong>Резервных копий пока нет</strong><span>Создайте первую копию — её можно будет скачать и сохранить на другом диске.</span></div>
            ) : items.map((item) => (
              <article className="backup-row" key={item.name}>
                <div className="backup-main">
                  <div className="backup-title">
                    <strong>{formatDate(item.createdAt || item.modifiedAt)}</strong>
                    <span className={`kind-badge kind-${item.kind}`}>{BACKUP_KINDS[item.kind] ?? item.kind}</span>
                    {!item.valid && <span className="kind-badge kind-broken">Повреждена</span>}
                  </div>
                  <small>
                    {item.valid
                      ? `${item.storageFiles.toLocaleString()} файлов, ${item.folderCount.toLocaleString()} папок, ${item.chatFiles.toLocaleString()} вложений чата · данных ${formatBytes(item.dataBytes)} · архив ${formatBytes(item.size)}${item.version ? ` · v${item.version}` : ''}`
                      : item.error || 'Архив не удалось прочитать'}
                  </small>
                  <code>{item.name}</code>
                </div>
                <div className="backup-actions">
                  <a className="secondary" href={`/api/backups/${encodeURIComponent(item.name)}/download`} download>Скачать</a>
                  <button className="secondary" onClick={() => void start(`/api/backups/${encodeURIComponent(item.name)}/verify`)} disabled={running || !item.valid}>Проверить</button>
                  <button className="secondary" onClick={() => restore(item)} disabled={running || !item.valid}>Восстановить</button>
                  <button className="secondary danger-action" onClick={() => void remove(item)} disabled={running}>Удалить</button>
                </div>
              </article>
            ))}
          </section>

          {form && (
            <form className="management-panel settings-form" onSubmit={(event) => void saveSettings(event)}>
              <div className="panel-heading"><div><h2>Расписание и место хранения</h2><p>Автоматические копии создаются, пока запущен сервер</p></div></div>
              <label className="setting-field">
                <span>Автоматическое копирование</span>
                <select value={form.intervalHours} onChange={(event) => updateForm('intervalHours', Number(event.target.value))}>
                  {BACKUP_INTERVALS.map(([hours, label]) => <option key={hours} value={hours}>{label}</option>)}
                  {!BACKUP_INTERVALS.some(([hours]) => hours === form.intervalHours) && <option value={form.intervalHours}>{`Каждые ${form.intervalHours} ч`}</option>}
                </select>
                <small>Новая копия создаётся, если с момента последней прошло больше выбранного времени.</small>
              </label>
              <label className="setting-field">
                <span>Сколько автоматических копий хранить</span>
                <input type="number" value={form.keep} onChange={(event) => updateForm('keep', Number(event.target.value))} min={1} max={100} required />
                <small>Старые автоматические копии удаляются. Копии, созданные вручную или загруженные, не удаляются никогда.</small>
              </label>
              <label className="setting-field">
                <span>Папка для резервных копий</span>
                <input value={form.dir} onChange={(event) => updateForm('dir', event.target.value)} placeholder={data.defaultDir} spellCheck={false} />
                <small>Оставьте пустым, чтобы использовать папку по умолчанию. Копия на том же диске не защищает от его поломки — лучше указать внешний диск, например D:\Backups.</small>
              </label>
              <div className="settings-actions"><button className="primary" type="submit" disabled={saving || !formDirty}>{saving ? 'Сохранение…' : 'Сохранить параметры'}</button></div>
            </form>
          )}

          <p className="management-footnote">В копию входят файлы и папки, вложения и история чата, настройки облака и журнал событий. Список подключённых устройств в копию не попадает и при восстановлении не меняется — доступ остаётся таким, как сейчас. Копия не шифруется: храните архивы в надёжном месте.</p>
        </>
      )}

      {restoring && (
        <div className="backup-overlay" role="alertdialog" aria-live="polite" aria-label="Идёт восстановление">
          <div className="backup-overlay-card">
            <h2>Идёт восстановление</h2>
            <p>{job.phase || 'Подготовка'}{percent !== null ? ` · ${percent}%` : ''}</p>
            <div className="disk-track"><div className={`disk-fill ${percent === null ? 'indeterminate' : ''}`} style={percent === null ? undefined : { width: `${percent}%` }} /></div>
            <small>Не закрывайте сервер и не выключайте компьютер. Облако временно доступно только для чтения статуса.</small>
          </div>
        </div>
      )}
    </div>
  )
}

function Root() {
  const [me, setMe] = useState<AuthMe | null>(null)
  const [failed, setFailed] = useState(false)

  const refresh = useCallback(async () => {
    try {
      const data = await requestJSON('/api/auth/me') as AuthMe
      setMe(data)
      setFailed(false)
    } catch {
      setFailed(true)
    }
  }, [])

  useEffect(() => {
    void refresh()
  }, [refresh])

  useEffect(() => {
    const onUnauthorized = () => setMe({ authenticated: false })
    const recheck = () => { if (document.visibilityState === 'visible') void refresh() }
    window.addEventListener(UNAUTHORIZED_EVENT, onUnauthorized)
    document.addEventListener('visibilitychange', recheck)
    const timer = window.setInterval(() => void refresh(), 60000)
    return () => {
      window.removeEventListener(UNAUTHORIZED_EVENT, onUnauthorized)
      document.removeEventListener('visibilitychange', recheck)
      window.clearInterval(timer)
    }
  }, [refresh])

  if (!me) {
    return (
      <div className="pair-screen">
        <div className="pair-card">
          {failed ? (
            <>
              <strong>Сервер недоступен</strong>
              <p className="pair-lead">Не удалось связаться с Personal Cloud. Проверьте, что сервер запущен.</p>
              <button className="primary" onClick={() => void refresh()}>Повторить</button>
            </>
          ) : 'Загрузка…'}
        </div>
      </div>
    )
  }

  if (!me.authenticated || !me.device) return <PairScreen onPaired={() => void refresh()} />

  return <App me={me as AuthMe & { device: DeviceInfo }} onSignedOut={() => setMe({ authenticated: false })} />
}

type AppProps = { me: AuthMe & { device: DeviceInfo }; onSignedOut: () => void }

function App({ me, onSignedOut }: AppProps) {
  const [activeView, setActiveView] = useState<'files' | 'chat' | 'devices' | 'storage' | 'backups' | 'settings' | 'logs'>('files')
  const [cloudName, setCloudName] = useState('Personal Cloud')
  const [appVersion, setAppVersion] = useState('')
  useEffect(() => {
    void requestJSON('/api/info').then((data: InfoResponse) => { setCloudName(data.name); setAppVersion(data.version) }).catch(() => undefined)
  }, [])
  useEffect(() => { document.title = cloudName }, [cloudName])
  const [currentPath, setCurrentPath] = useState('')
  const [items, setItems] = useState<Entry[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [searchQuery, setSearchQuery] = useState('')
  const [searchResults, setSearchResults] = useState<SearchHit[]>([])
  const [searchLoading, setSearchLoading] = useState(false)
  const [searchTerms, setSearchTerms] = useState<string[]>([])
  const [searchTotal, setSearchTotal] = useState(0)
  const [searchTruncated, setSearchTruncated] = useState(false)
  const [searchPartial, setSearchPartial] = useState(false)
  const [searchType, setSearchType] = useState('all')
  const [searchPeriod, setSearchPeriod] = useState('any')
  const [searchSort, setSearchSort] = useState('relevance')
  const [searchOrder, setSearchOrder] = useState<'default' | 'asc' | 'desc'>('default')
  const [searchContent, setSearchContent] = useState(false)
  const [searchHere, setSearchHere] = useState(false)
  const [previewPath, setPreviewPath] = useState<string | null>(null)
  const searchRequestRef = useRef(0)
  const searchAbortRef = useRef<AbortController | null>(null)
  const searchInputRef = useRef<HTMLInputElement | null>(null)
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [dragOver, setDragOver] = useState(false)
  const [mobileNavOpen, setMobileNavOpen] = useState(false)
  const [upload, setUpload] = useState<UploadState | null>(null)
  const [openActionPath, setOpenActionPath] = useState<string | null>(null)
  const [bulkDownloading, setBulkDownloading] = useState(false)
  const selectionAnchorRef = useRef<string | null>(null)
  const inputRef = useRef<HTMLInputElement | null>(null)

  const load = useCallback(async (path: string) => {
    setLoading(true)
    setError('')
    setSelected(new Set())
    selectionAnchorRef.current = null
    try {
      const data = await requestJSON(`/api/files?path=${encodeURIComponent(path)}`) as ApiResponse
      setItems(data.items)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Не удалось получить список файлов')
    } finally {
      setLoading(false)
    }
  }, [])

  const runSearch = useCallback(async (query: string) => {
    const clean = query.trim()
    searchAbortRef.current?.abort()
    if (!clean) {
      searchRequestRef.current += 1
      setSearchResults([])
      setSearchTerms([])
      setSearchTotal(0)
      setSearchTruncated(false)
      setSearchPartial(false)
      setSearchLoading(false)
      return
    }
    const requestId = searchRequestRef.current + 1
    searchRequestRef.current = requestId
    const controller = new AbortController()
    searchAbortRef.current = controller

    const params = new URLSearchParams({ q: clean })
    if (searchType !== 'all') params.set('type', searchType)
    if (searchPeriod !== 'any') params.set('modified', searchPeriod)
    if (searchSort !== 'relevance') params.set('sort', searchSort)
    if (searchOrder !== 'default') params.set('order', searchOrder)
    if (searchContent) params.set('content', '1')
    if (searchHere && currentPath) params.set('path', currentPath)

    setSearchLoading(true)
    setError('')
    try {
      const data = await requestJSON(`/api/search?${params.toString()}`, { signal: controller.signal }) as SearchResponse
      if (requestId !== searchRequestRef.current) return
      setSearchResults(data.items)
      setSearchTerms(data.terms ?? [])
      setSearchTotal(data.total)
      setSearchTruncated(data.truncated)
      setSearchPartial(data.partial)
    } catch (err) {
      if (controller.signal.aborted || requestId !== searchRequestRef.current) return
      setError(err instanceof Error ? err.message : 'Не удалось выполнить поиск')
    } finally {
      if (requestId === searchRequestRef.current) setSearchLoading(false)
    }
  }, [searchType, searchPeriod, searchSort, searchOrder, searchContent, searchHere, currentPath])

  useEffect(() => {
    if (searchQuery.trim()) return
    void load(currentPath)
  }, [currentPath, load, searchQuery])

  useEffect(() => {
    const closeMenu = (event: MouseEvent) => {
      const target = event.target as HTMLElement | null
      if (!target?.closest('.action-menu')) setOpenActionPath(null)
    }
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setOpenActionPath(null)
    }
    document.addEventListener('click', closeMenu)
    document.addEventListener('keydown', closeOnEscape)
    return () => {
      document.removeEventListener('click', closeMenu)
      document.removeEventListener('keydown', closeOnEscape)
    }
  }, [])

  useEffect(() => {
    const timeout = window.setTimeout(() => void runSearch(searchQuery), 220)
    return () => window.clearTimeout(timeout)
  }, [runSearch, searchQuery])

  useEffect(() => {
    setSelected(new Set())
    selectionAnchorRef.current = null
    setOpenActionPath(null)
  }, [searchQuery])

  useEffect(() => {
    if (activeView !== 'files') return
    const onKey = (event: KeyboardEvent) => {
      if (event.key !== '/' || event.ctrlKey || event.metaKey || event.altKey) return
      const target = event.target as HTMLElement | null
      if (target && (target.tagName === 'INPUT' || target.tagName === 'TEXTAREA' || target.tagName === 'SELECT' || target.isContentEditable)) return
      event.preventDefault()
      searchInputRef.current?.focus()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [activeView])

  const uploadFiles = async (files: FileList | File[]) => {
    const list = Array.from(files)
    if (!list.length) return
    setError('')

    for (const file of list) {
      setUpload({ name: file.name, loaded: 0, total: file.size, status: 'uploading' })
      await new Promise<void>((resolve) => {
        const xhr = new XMLHttpRequest()
        xhr.open('POST', `/api/files?path=${encodeURIComponent(currentPath)}`)
        xhr.upload.onprogress = (event) => {
          if (event.lengthComputable) {
            setUpload({ name: file.name, loaded: event.loaded, total: event.total, status: 'uploading' })
          }
        }
        xhr.onload = () => {
          if (xhr.status >= 200 && xhr.status < 300) {
            setUpload({ name: file.name, loaded: file.size, total: file.size, status: 'done' })
          } else {
            if (xhr.status === 401) window.dispatchEvent(new Event(UNAUTHORIZED_EVENT))
            let message = `Не удалось загрузить ${file.name}`
            try { message = JSON.parse(xhr.responseText).error ?? message } catch { /* ignore invalid response */ }
            setUpload({ name: file.name, loaded: file.size, total: file.size, status: 'error' })
            setError(message)
          }
          resolve()
        }
        xhr.onerror = () => {
          setUpload({ name: file.name, loaded: 0, total: file.size, status: 'error' })
          setError(`Не удалось загрузить ${file.name}`)
          resolve()
        }
        const form = new FormData()
        form.append('file', file)
        xhr.send(form)
      })
    }

    await load(currentPath)
    window.setTimeout(() => setUpload(null), 900)
  }

  const createFolder = async () => {
    const name = window.prompt('Название новой папки')?.trim()
    if (!name) return
    const path = currentPath ? `${currentPath}/${name}` : name
    try {
      await requestJSON('/api/folders', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ path }),
      })
      if (searchQuery) await runSearch(searchQuery)
      else await load(currentPath)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Не удалось создать папку')
    }
  }

  const refreshView = async () => {
    if (searchQuery.trim()) await runSearch(searchQuery)
    else await load(currentPath)
  }

  const remove = async (entry: Entry) => {
    setOpenActionPath(null)
    const message = entry.kind === 'folder'
      ? `Удалить папку «${entry.name}» и всё её содержимое?`
      : `Удалить файл «${entry.name}»?`
    if (!window.confirm(message)) return
    try {
      await requestJSON(`/api/files?path=${encodeURIComponent(entry.path)}`, { method: 'DELETE' })
      await refreshView()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Не удалось удалить')
    }
  }

  const rename = async (entry: Entry) => {
    setOpenActionPath(null)
    const name = window.prompt(`Новое имя для «${entry.name}»`, entry.name)?.trim()
    if (!name || name === entry.name) return
    try {
      await requestJSON('/api/files/rename', {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ path: entry.path, name }),
      })
      await refreshView()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Не удалось переименовать')
    }
  }

  const transfer = async (entry: Entry, mode: 'move' | 'copy') => {
    setOpenActionPath(null)
    const action = mode === 'move' ? 'переместить' : 'скопировать'
    const destination = window.prompt(`В какую папку ${action} «${entry.name}»?\nВведите путь относительно корня. Пусто = корень.`, currentPath)?.trim()
    if (destination === null) return
    try {
      await requestJSON(`/api/files/${mode}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ path: entry.path, destination }),
      })
      await refreshView()
    } catch (err) {
      setError(err instanceof Error ? err.message : `Не удалось ${action}`)
    }
  }

  const download = (entry: Entry) => {
    setOpenActionPath(null)
    const link = document.createElement('a')
    link.href = downloadUrl(entry.path)
    link.download = entry.name
    document.body.appendChild(link)
    link.click()
    link.remove()
  }

  const displayItems: SearchHit[] = searchQuery.trim() ? searchResults : items
  const isSearching = searchQuery.trim().length > 0

  const previewableItems = useMemo(
    () => displayItems.filter((entry) => entry.kind === 'file' && entry.preview !== ''),
    [displayItems],
  )
  const previewEntry = previewPath ? displayItems.find((entry) => entry.path === previewPath && entry.kind === 'file' && entry.preview !== '') ?? null : null
  useEffect(() => {
    if (previewPath && !previewEntry) setPreviewPath(null)
  }, [previewPath, previewEntry])

  const openPreview = (entry: Entry) => {
    setOpenActionPath(null)
    setPreviewPath(entry.path)
  }
  // Double click: folders open, previewable files open in the viewer, the rest download.
  const openEntry = (entry: Entry) => {
    if (entry.kind === 'folder') {
      setCurrentPath(entry.path)
      if (isSearching) setSearchQuery('')
    } else if (entry.preview) openPreview(entry)
    else download(entry)
  }
  const allSelected = displayItems.length > 0 && displayItems.every((entry) => selected.has(entry.path))

  const selectEntry = (entry: Entry, extendRange = false) => {
    const clickedIndex = displayItems.findIndex((item) => item.path === entry.path)
    const anchorIndex = selectionAnchorRef.current
      ? displayItems.findIndex((item) => item.path === selectionAnchorRef.current)
      : -1

    if (extendRange && clickedIndex >= 0 && anchorIndex >= 0) {
      const start = Math.min(anchorIndex, clickedIndex)
      const end = Math.max(anchorIndex, clickedIndex)
      setSelected((previous) => {
        const next = new Set(previous)
        for (let index = start; index <= end; index += 1) {
          next.add(displayItems[index].path)
        }
        return next
      })
    } else {
      setSelected((previous) => {
        const next = new Set(previous)
        if (next.has(entry.path)) next.delete(entry.path)
        else next.add(entry.path)
        return next
      })
    }

    selectionAnchorRef.current = entry.path
  }

  const toggleSelectAll = () => {
    if (allSelected) {
      setSelected(new Set())
      selectionAnchorRef.current = null
      return
    }

    setSelected(new Set(displayItems.map((entry) => entry.path)))
    selectionAnchorRef.current = null
  }

  const handleRowClick = (event: React.MouseEvent<HTMLDivElement>, entry: Entry) => {
    const target = event.target as HTMLElement | null
    if (target?.closest('input, .row-actions, .action-menu')) return
    if (selected.size > 0) selectEntry(entry, event.shiftKey)
  }

  const selectedEntries = useMemo(() => {
    const byPath = new Map(displayItems.map((entry) => [entry.path, entry]))
    return Array.from(selected)
      .map((path) => byPath.get(path))
      .filter((entry): entry is Entry => Boolean(entry))
  }, [displayItems, selected])

  const clearSelection = () => {
    setSelected(new Set())
    selectionAnchorRef.current = null
  }

  const bulkRemove = async () => {
    const entries = selectedEntries
    if (!entries.length) return
    setOpenActionPath(null)
    const hasFolders = entries.some((entry) => entry.kind === 'folder')
    const message = hasFolders
      ? `Удалить выбранные ${entries.length} объекта(ов) вместе с содержимым папок?`
      : `Удалить выбранные ${entries.length} файла(ов)?`
    if (!window.confirm(message)) return

    try {
      for (const entry of entries) {
        await requestJSON(`/api/files?path=${encodeURIComponent(entry.path)}`, { method: 'DELETE' })
      }
      clearSelection()
      await refreshView()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Не удалось удалить выбранные объекты')
    }
  }

  const bulkTransfer = async (mode: 'move' | 'copy') => {
    const entries = selectedEntries
    if (!entries.length) return
    setOpenActionPath(null)
    const action = mode === 'move' ? 'переместить' : 'скопировать'
    const destination = window.prompt(`В какую папку ${action} выбранные ${entries.length} объекта(ов)?\nВведите путь относительно корня. Пусто = корень.`, currentPath)?.trim()
    if (destination === null) return

    try {
      for (const entry of entries) {
        await requestJSON(`/api/files/${mode}`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ path: entry.path, destination }),
        })
      }
      clearSelection()
      await refreshView()
    } catch (err) {
      setError(err instanceof Error ? err.message : `Не удалось ${action} выбранные объекты`)
    }
  }

  const bulkSingleAction = async () => {
    const entry = selectedEntries[0]
    if (!entry || selectedEntries.length !== 1) return
    if (entry.kind === 'folder') {
      clearSelection()
      openEntry(entry)
      return
    }
    download(entry)
  }

  const bulkDownload = async () => {
    if (selectedEntries.length < 2 || bulkDownloading) return
    setOpenActionPath(null)
    setError('')
    setBulkDownloading(true)
    try {
      const response = await fetch('/api/files/download-bulk', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ paths: selectedEntries.map((entry) => entry.path) }),
      })
      if (!response.ok) {
        const data = await response.json().catch(() => ({})) as { error?: string }
        throw new Error(data.error ?? 'Не удалось скачать выбранные объекты')
      }

      const blob = await response.blob()
      const url = URL.createObjectURL(blob)
      const link = document.createElement('a')
      link.href = url
      link.download = 'personal-cloud.zip'
      document.body.appendChild(link)
      link.click()
      link.remove()
      window.setTimeout(() => URL.revokeObjectURL(url), 1000)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Не удалось скачать выбранные объекты')
    } finally {
      setBulkDownloading(false)
    }
  }

  const bulkRename = async () => {
    const entry = selectedEntries[0]
    if (!entry || selectedEntries.length !== 1) return
    await rename(entry)
    clearSelection()
  }
  const selectedCount = selectedEntries.length
  const stats = useMemo(() => {
    const files = displayItems.filter((item) => item.kind === 'file')
    return {
      files: files.length,
      folders: displayItems.filter((item) => item.kind === 'folder').length,
      size: files.reduce((sum, item) => sum + item.size, 0),
    }
  }, [displayItems])
  const progressPercent = upload && upload.total > 0 ? Math.round((upload.loaded / upload.total) * 100) : 0

  return (
    <div className="app-shell">
      {mobileNavOpen && <button className="mobile-backdrop" aria-label="Закрыть меню" onClick={() => setMobileNavOpen(false)} />}

      <aside className={`sidebar ${mobileNavOpen ? 'open' : ''}`}>
        <div className="mobile-menu-close">
          <button className="icon-button" onClick={() => setMobileNavOpen(false)} aria-label="Закрыть меню">×</button>
        </div>
        <div className="brand">
          <div className="brand-mark">PC</div>
          <div>
            <strong>{cloudName}</strong>
            <span>{appVersion ? `v${appVersion}` : ''}</span>
          </div>
        </div>
        <nav>
          <div className="nav-label">Рабочая область</div>
          <button className={`nav-item ${activeView === 'files' ? 'active' : ''}`} onClick={() => { setActiveView('files'); setSearchQuery(''); setMobileNavOpen(false) }}><span>▦</span> Файлы</button>
          <button className={`nav-item ${activeView === 'chat' ? 'active' : ''}`} onClick={() => { setActiveView('chat'); setMobileNavOpen(false) }}><span>⌁</span> Чат</button>
          <div className="nav-label nav-label-spaced">Управление облаком</div>
          <button className={`nav-item ${activeView === 'devices' ? 'active' : ''}`} onClick={() => { setActiveView('devices'); setMobileNavOpen(false) }}><span>◈</span> Устройства</button>
          <button className={`nav-item ${activeView === 'storage' ? 'active' : ''}`} onClick={() => { setActiveView('storage'); setMobileNavOpen(false) }}><span>▤</span> Хранилище</button>
          <button className={`nav-item ${activeView === 'backups' ? 'active' : ''}`} onClick={() => { setActiveView('backups'); setMobileNavOpen(false) }}><span>◧</span> Резервные копии</button>
          <button className={`nav-item ${activeView === 'settings' ? 'active' : ''}`} onClick={() => { setActiveView('settings'); setMobileNavOpen(false) }}><span>⚙</span> Настройки</button>
          <button className={`nav-item ${activeView === 'logs' ? 'active' : ''}`} onClick={() => { setActiveView('logs'); setMobileNavOpen(false) }}><span>≡</span> Журнал событий</button>
        </nav>
        <ConnectionSwitcher compact />
        <div className="sidebar-note">
          <span>{me.isHost ? 'Компьютер-хост' : 'Подключённое устройство'}</span>
          <strong>{me.device.name}</strong>
          <small>Файлы остаются на вашем компьютере.</small>
        </div>
      </aside>

      <main className={`main ${activeView === 'chat' ? 'chat-main' : ''}`}>
        {activeView === 'devices' ? (
          <DevicesView onOpenMenu={() => setMobileNavOpen(true)} onSignedOut={onSignedOut} />
        ) : activeView === 'storage' ? (
          <StorageView onOpenMenu={() => setMobileNavOpen(true)} />
        ) : activeView === 'backups' ? (
          <BackupsView onOpenMenu={() => setMobileNavOpen(true)} />
        ) : activeView === 'settings' ? (
          <SettingsView onOpenMenu={() => setMobileNavOpen(true)} onSaved={(saved) => setCloudName(saved.cloudName)} />
        ) : activeView === 'logs' ? (
          <LogsView onOpenMenu={() => setMobileNavOpen(true)} />
        ) : activeView === 'files' ? (
          <>
        <header className="topbar">
          <div className="topbar-title">
            <button className="mobile-menu-button" onClick={() => setMobileNavOpen(true)} aria-label="Открыть меню">☰</button>
            <div className="title-copy">
              <h1>{isSearching ? 'Поиск' : 'Файлы'}</h1>
              <p>{isSearching ? `Результаты для «${searchQuery.trim()}»` : 'Ваше личное хранилище'}</p>
            </div>
          </div>
          <button className="icon-button" onClick={() => void refreshView()} title="Обновить">↻</button>
        </header>

        <section className="toolbar">
          <div className="search-box">
            <span>⌕</span>
            <input
              ref={searchInputRef}
              value={searchQuery}
              onChange={(event) => setSearchQuery(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === 'Escape') { setSearchQuery(''); event.currentTarget.blur() }
              }}
              placeholder="Поиск файлов и папок… (нажмите /)"
              aria-label="Поиск файлов и папок"
            />
            {searchQuery && <button onClick={() => setSearchQuery('')} aria-label="Очистить поиск">×</button>}
          </div>
          {!isSearching && (
            <>
              <button className="primary" onClick={() => inputRef.current?.click()}>＋ Загрузить</button>
              <button className="secondary" onClick={() => void createFolder()}>＋ Папка</button>
            </>
          )}
          {displayItems.length > 0 && (
            <button className="secondary select-all-button" onClick={toggleSelectAll}>
              {allSelected ? 'Снять всё' : 'Выделить всё'}
            </button>
          )}
          {selectedCount > 0 && (
            <div className="selection-toolbar">
              <span className="selection-count">Выбрано: {selectedCount}</span>
              {selectedCount === 1 && (
                <>
                  {selectedEntries[0]?.kind === 'file' && selectedEntries[0].preview !== '' && (
                    <button className="selection-action" onClick={() => openPreview(selectedEntries[0])}>Просмотр</button>
                  )}
                  <button className="selection-action" onClick={() => void bulkSingleAction()}>
                    {selectedEntries[0]?.kind === 'folder' ? 'Открыть' : 'Скачать'}
                  </button>
                  <button className="selection-action" onClick={() => void bulkRename()}>Переименовать</button>
                </>
              )}
              {selectedCount > 1 && (
                <button
                  className="selection-action"
                  onClick={() => void bulkDownload()}
                  disabled={bulkDownloading}
                >
                  {bulkDownloading ? 'Подготовка…' : 'Скачать'}
                </button>
              )}
              <button className="selection-action" onClick={() => void bulkTransfer('move')}>Переместить</button>
              <button className="selection-action" onClick={() => void bulkTransfer('copy')}>Копировать</button>
              <button className="selection-action danger-action" onClick={() => void bulkRemove()}>Удалить</button>
              <button className="selection-clear" onClick={clearSelection}>Снять выделение</button>
            </div>
          )}
          <input
            ref={inputRef}
            type="file"
            multiple
            hidden
            onChange={(event) => {
              if (event.target.files) void uploadFiles(event.target.files)
              event.currentTarget.value = ''
            }}
          />
        </section>

        {isSearching && (
          <section className="search-filters" aria-label="Фильтры поиска">
            <select value={searchType} onChange={(event) => setSearchType(event.target.value)} aria-label="Тип файлов">
              {SEARCH_TYPES.map(([value, label]) => <option key={value} value={value}>{label}</option>)}
            </select>
            <select value={searchPeriod} onChange={(event) => setSearchPeriod(event.target.value)} aria-label="Дата изменения">
              {SEARCH_PERIODS.map(([value, label]) => <option key={value} value={value}>{label}</option>)}
            </select>
            <select value={searchSort} onChange={(event) => { setSearchSort(event.target.value); setSearchOrder('default') }} aria-label="Сортировка">
              {SEARCH_SORTS.map(([value, label]) => <option key={value} value={value}>{label}</option>)}
            </select>
            <button
              className="secondary order-button"
              onClick={() => {
                const naturalDesc = searchSort === 'relevance' || searchSort === 'size' || searchSort === 'modified'
                const effectiveDesc = searchOrder === 'default' ? naturalDesc : searchOrder === 'desc'
                setSearchOrder(effectiveDesc ? 'asc' : 'desc')
              }}
              title="Изменить направление сортировки"
              aria-label="Изменить направление сортировки"
            >
              {(searchOrder === 'default' ? (searchSort === 'relevance' || searchSort === 'size' || searchSort === 'modified') : searchOrder === 'desc') ? '↓' : '↑'}
            </button>
            <label className="check-option">
              <input type="checkbox" checked={searchContent} onChange={(event) => setSearchContent(event.target.checked)} />
              <span>Искать внутри текстовых файлов</span>
            </label>
            {currentPath && (
              <label className="check-option">
                <input type="checkbox" checked={searchHere} onChange={(event) => setSearchHere(event.target.checked)} />
                <span>Только в папке «{currentPath.split('/').pop()}»</span>
              </label>
            )}
            <small className="search-hint">Несколько слов — ищем все сразу; «в кавычках» — точная фраза; ext:pdf, type:image — быстрые фильтры.</small>
          </section>
        )}

        {!isSearching && (
          <>
            <div
              className={`drop-zone ${dragOver ? 'dragging' : ''}`}
              onDragEnter={(event) => { event.preventDefault(); setDragOver(true) }}
              onDragOver={(event) => event.preventDefault()}
              onDragLeave={(event) => { if (event.currentTarget === event.target) setDragOver(false) }}
              onDrop={(event) => {
                event.preventDefault()
                setDragOver(false)
                void uploadFiles(event.dataTransfer.files)
              }}
            >
              <span className="desktop-drop-text">Перетащите файлы сюда для загрузки</span>
              <span className="mobile-drop-text">Выберите файлы через кнопку «Загрузить»</span>
            </div>

            <div className="breadcrumbs">
              {breadcrumbs(currentPath).map((crumb, index) => (
                <React.Fragment key={crumb.path || 'root'}>
                  {index > 0 && <span>/</span>}
                  <button className={index === breadcrumbs(currentPath).length - 1 ? 'current' : ''} onClick={() => setCurrentPath(crumb.path)}>{crumb.label}</button>
                </React.Fragment>
              ))}
            </div>
          </>
        )}

        {upload && !isSearching && (
          <div className="upload-progress">
            <div className="upload-progress-head">
              <strong>{upload.status === 'done' ? 'Загрузка завершена' : upload.status === 'error' ? 'Ошибка загрузки' : 'Загрузка'}</strong>
              <span>{formatBytes(upload.loaded)} / {formatBytes(upload.total)} · {progressPercent}%</span>
            </div>
            <div className="upload-progress-name">{upload.name}</div>
            <div className="progress-track"><div className={`progress-bar ${upload.status}`} style={{ width: `${progressPercent}%` }} /></div>
          </div>
        )}

        <section className="content-card">
          {error && <div className="error-banner">{error}</div>}
          <div className={`table-head ${isSearching ? 'search-head' : ''}`}>
            <span>Имя</span><span>Размер</span><span>Тип</span><span>Изменён</span><span></span>
          </div>
          {!isSearching && currentPath && (
            <button className="file-row folder-row" onClick={() => {
              const parts = currentPath.split('/')
              parts.pop()
              setCurrentPath(parts.join('/'))
            }}>
              <span><strong className="file-icon">↑</strong> ..</span>
              <span></span><span></span><span></span><span></span>
            </button>
          )}
          {(isSearching ? searchLoading : loading) ? (
            <div className="empty-state">Загрузка…</div>
          ) : displayItems.length === 0 ? (
            <div className="empty-state">
              <div className="empty-icon">□</div>
              <strong>{isSearching ? 'Ничего не найдено' : 'Папка пуста'}</strong>
              <span>{isSearching ? 'Попробуйте другое название или расширение.' : 'Загрузите первый файл или создайте папку.'}</span>
            </div>
          ) : (
            displayItems.map((entry) => (
              <div
                className={`file-row ${selected.has(entry.path) ? 'selected' : ''} ${selected.size > 0 ? 'selection-mode' : ''} ${openActionPath === entry.path ? 'menu-open' : ''}`}
                key={entry.path}
                onClick={(event) => handleRowClick(event, entry)}
                onDoubleClick={(event) => {
                  const target = event.target as HTMLElement | null
                  if (target?.closest('button, input, .action-menu')) return
                  openEntry(entry)
                }}
                title={entry.kind === 'folder' ? 'Двойной щелчок — открыть папку' : entry.preview ? 'Двойной щелчок — просмотр' : undefined}
              >
                <div className="name-cell">
                  <input
                    type="checkbox"
                    checked={selected.has(entry.path)}
                    onChange={() => undefined}
                    onClick={(event) => {
                      event.stopPropagation()
                      selectEntry(entry, event.shiftKey)
                    }}
                    aria-label={`Выбрать ${entry.name}`}
                  />
                  <span className={`file-icon ${entry.kind}`}>{entry.kind === 'folder' ? '▰' : '□'}</span>
                  <div className="name-copy">
                    <button className="name-button" onDoubleClick={() => openEntry(entry)}>{isSearching ? highlightText(entry.name, searchTerms) : entry.name}</button>
                    {isSearching && <small className="search-location">{highlightText(entry.path, searchTerms)}</small>}
                    {isSearching && entry.match === 'content' && entry.snippet && (
                      <small className="search-snippet" title={entry.line ? `Строка ${entry.line}` : undefined}>
                        {entry.line ? <span className="snippet-line">стр. {entry.line}</span> : null}
                        {highlightText(entry.snippet, searchTerms)}
                      </small>
                    )}
                  </div>
                </div>
                <span>{entry.kind === 'folder' ? '—' : formatBytes(entry.size)}</span>
                <span title={entry.mimeType || undefined}>{formatType(entry)}</span>
                <span title={`Создан: ${formatDate(entry.createdAt)}`}>{formatDate(entry.updatedAt)}</span>
                <span className="row-actions">
                  <span className={`action-menu ${openActionPath === entry.path ? 'open' : ''}`}>
                    <button
                      className="action-menu-trigger"
                      aria-label={`Действия для ${entry.name}`}
                      aria-expanded={openActionPath === entry.path}
                      onClick={(event) => {
                        event.stopPropagation()
                        setOpenActionPath((current) => current === entry.path ? null : entry.path)
                      }}
                    >⋯</button>
                    {openActionPath === entry.path && (
                      <div className="action-menu-popover" onClick={(event) => event.stopPropagation()}>
                        {entry.kind === 'folder' && <button onClick={() => { setOpenActionPath(null); openEntry(entry) }}>Открыть</button>}
                        {entry.kind === 'file' && entry.preview !== '' && <button onClick={() => openPreview(entry)}>Просмотр</button>}
                        {entry.kind === 'file' && <button onClick={() => download(entry)}>Скачать</button>}
                        <button onClick={() => void rename(entry)}>Переименовать</button>
                        <button onClick={() => void transfer(entry, 'move')}>Переместить</button>
                        <button onClick={() => void transfer(entry, 'copy')}>Копировать</button>
                        <button className="danger-action" onClick={() => void remove(entry)}>Удалить</button>
                      </div>
                    )}
                  </span>
                </span>
              </div>
            ))
          )}
        </section>

        <footer className="footer-stats">
          <span>{stats.folders} папок</span>
          <span>{stats.files} файлов</span>
          <span>{formatBytes(stats.size)} в результате</span>
          {isSearching && <span>{searchTotal} совпадений</span>}
          {isSearching && searchTruncated && <span className="footer-warning">Показаны первые {searchResults.length} — уточните запрос</span>}
          {isSearching && searchPartial && <span className="footer-warning">Поиск по содержимому охватил не все файлы</span>}
        </footer>

        {previewEntry && (
          <PreviewModal
            entry={previewEntry}
            siblings={previewableItems}
            onNavigate={(next) => setPreviewPath(next.path)}
            onClose={() => setPreviewPath(null)}
          />
        )}
          </>
        ) : (
          <ChatView onOpenMenu={() => setMobileNavOpen(true)} />
        )}
      </main>
    </div>
  )
}

createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <Root />
  </React.StrictMode>,
)
