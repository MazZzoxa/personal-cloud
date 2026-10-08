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
  createdAt: string
  updatedAt: string
}

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
  const [activeView, setActiveView] = useState<'files' | 'chat' | 'devices'>('files')
  const [currentPath, setCurrentPath] = useState('')
  const [items, setItems] = useState<Entry[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [searchQuery, setSearchQuery] = useState('')
  const [searchResults, setSearchResults] = useState<Entry[]>([])
  const [searchLoading, setSearchLoading] = useState(false)
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
    if (!clean) {
      setSearchResults([])
      return
    }
    setSearchLoading(true)
    setError('')
    try {
      const data = await requestJSON(`/api/search?q=${encodeURIComponent(clean)}`) as { items: Entry[] }
      setSearchResults(data.items)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Не удалось выполнить поиск')
    } finally {
      setSearchLoading(false)
    }
  }, [])

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
    const url = `/api/files/download?path=${encodeURIComponent(entry.path)}`
    const link = document.createElement('a')
    link.href = url
    link.download = entry.name
    document.body.appendChild(link)
    link.click()
    link.remove()
  }

  const displayItems = searchQuery.trim() ? searchResults : items
  const isSearching = searchQuery.trim().length > 0
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
      setCurrentPath(entry.path)
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
            <strong>Personal Cloud</strong>
            <span>v0.5.0</span>
          </div>
        </div>
        <nav>
          <button className={`nav-item ${activeView === 'files' ? 'active' : ''}`} onClick={() => { setActiveView('files'); setSearchQuery(''); setMobileNavOpen(false) }}><span>▦</span> Файлы</button>
          <button className={`nav-item ${activeView === 'chat' ? 'active' : ''}`} onClick={() => { setActiveView('chat'); setMobileNavOpen(false) }}><span>⌁</span> Чат</button>
          <button className={`nav-item ${activeView === 'devices' ? 'active' : ''}`} onClick={() => { setActiveView('devices'); setMobileNavOpen(false) }}><span>◈</span> Устройства</button>
          <button className="nav-item" disabled><span>⚙</span> Настройки <small>v0.7</small></button>
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
              value={searchQuery}
              onChange={(event) => setSearchQuery(event.target.value)}
              placeholder="Поиск файлов и папок…"
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
                  if (entry.kind === 'folder') setCurrentPath(entry.path)
                  else download(entry)
                }}
                title={entry.kind === 'folder' ? 'Двойной щелчок — открыть папку' : undefined}
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
                    <button className="name-button" onDoubleClick={() => entry.kind === 'folder' ? setCurrentPath(entry.path) : download(entry)}>{entry.name}</button>
                    {isSearching && <small className="search-location">{entry.path}</small>}
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
                        {entry.kind === 'folder' && <button onClick={() => { setOpenActionPath(null); setCurrentPath(entry.path) }}>Открыть</button>}
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
          {isSearching && <span>{searchResults.length} совпадений</span>}
        </footer>
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
