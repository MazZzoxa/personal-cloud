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
  updatedAt: string
}

type ApiResponse = { path: string; items: Entry[] }

type Breadcrumb = { label: string; path: string }

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

function App() {
  const [currentPath, setCurrentPath] = useState('')
  const [items, setItems] = useState<Entry[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [dragOver, setDragOver] = useState(false)
  const [mobileNavOpen, setMobileNavOpen] = useState(false)
  const inputRef = useRef<HTMLInputElement | null>(null)

  const load = useCallback(async (path: string) => {
    setLoading(true)
    setError('')
    setSelected(new Set())
    try {
      const response = await fetch(`/api/files?path=${encodeURIComponent(path)}`)
      const data = await response.json()
      if (!response.ok) throw new Error(data.error ?? 'Не удалось получить список файлов')
      setItems((data as ApiResponse).items)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Произошла ошибка')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void load(currentPath)
  }, [currentPath, load])

  const upload = async (files: FileList | File[]) => {
    const list = Array.from(files)
    if (!list.length) return
    setError('')
    for (const file of list) {
      const form = new FormData()
      form.append('file', file)
      const response = await fetch(`/api/files?path=${encodeURIComponent(currentPath)}`, {
        method: 'POST',
        body: form,
      })
      const data = await response.json()
      if (!response.ok) {
        setError(data.error ?? `Не удалось загрузить ${file.name}`)
        break
      }
    }
    await load(currentPath)
  }

  const createFolder = async () => {
    const name = window.prompt('Название новой папки')?.trim()
    if (!name) return
    const path = currentPath ? `${currentPath}/${name}` : name
    const response = await fetch('/api/folders', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ path }),
    })
    const data = await response.json()
    if (!response.ok) {
      setError(data.error ?? 'Не удалось создать папку')
      return
    }
    await load(currentPath)
  }

  const remove = async (entry: Entry) => {
    const message = entry.kind === 'folder'
      ? `Удалить папку «${entry.name}» и всё её содержимое?`
      : `Удалить файл «${entry.name}»?`
    if (!window.confirm(message)) return
    const response = await fetch(`/api/files?path=${encodeURIComponent(entry.path)}`, { method: 'DELETE' })
    if (!response.ok) {
      const data = await response.json()
      setError(data.error ?? 'Не удалось удалить')
      return
    }
    await load(currentPath)
  }

  const download = (entry: Entry) => {
    const url = `/api/files/download?path=${encodeURIComponent(entry.path)}`
    const link = document.createElement('a')
    link.href = url
    link.download = entry.name
    document.body.appendChild(link)
    link.click()
    link.remove()
  }

  const toggleSelected = (entry: Entry) => {
    setSelected((previous) => {
      const next = new Set(previous)
      if (next.has(entry.path)) next.delete(entry.path)
      else next.add(entry.path)
      return next
    })
  }

  const selectedCount = selected.size
  const stats = useMemo(() => {
    const files = items.filter((item) => item.kind === 'file')
    return {
      files: files.length,
      folders: items.filter((item) => item.kind === 'folder').length,
      size: files.reduce((sum, item) => sum + item.size, 0),
    }
  }, [items])

  return (
    <div className="app-shell">
      {mobileNavOpen && (
        <button
          className="mobile-backdrop"
          aria-label="Закрыть меню"
          onClick={() => setMobileNavOpen(false)}
        />
      )}

      <aside className={`sidebar ${mobileNavOpen ? 'open' : ''}`}>
        <div className="mobile-menu-close">
          <button className="icon-button" onClick={() => setMobileNavOpen(false)} aria-label="Закрыть меню">×</button>
        </div>
        <div className="brand">
          <div className="brand-mark">PC</div>
          <div>
            <strong>Personal Cloud</strong>
            <span>v0.1.0</span>
          </div>
        </div>
        <nav>
          <button className="nav-item active" onClick={() => setMobileNavOpen(false)}><span>▦</span> Файлы</button>
          <button className="nav-item" disabled><span>✉</span> Чат <small>v0.3</small></button>
          <button className="nav-item" disabled><span>⚙</span> Настройки <small>v0.4</small></button>
        </nav>
        <div className="sidebar-note">
          <span>Локальное хранилище</span>
          <strong>Домашний ПК</strong>
          <small>Данные не уходят в стороннее облако.</small>
        </div>
      </aside>

      <main className="main">
        <header className="topbar">
          <div className="topbar-title">
            <button
              className="mobile-menu-button"
              onClick={() => setMobileNavOpen(true)}
              aria-label="Открыть меню"
            >
              ☰
            </button>
            <div className="title-copy">
              <h1>Файлы</h1>
              <p>Ваше личное хранилище</p>
            </div>
          </div>
          <button className="icon-button" onClick={() => void load(currentPath)} title="Обновить">↻</button>
        </header>

        <section className="toolbar">
          <button className="primary" onClick={() => inputRef.current?.click()}>＋ Загрузить</button>
          <button className="secondary" onClick={() => void createFolder()}>＋ Папка</button>
          {selectedCount > 0 && <span className="selection-note">Выбрано: {selectedCount}</span>}
          <input
            ref={inputRef}
            type="file"
            multiple
            hidden
            onChange={(event) => {
              if (event.target.files) void upload(event.target.files)
              event.currentTarget.value = ''
            }}
          />
        </section>

        <div
          className={`drop-zone ${dragOver ? 'dragging' : ''}`}
          onDragEnter={(event) => { event.preventDefault(); setDragOver(true) }}
          onDragOver={(event) => event.preventDefault()}
          onDragLeave={(event) => { if (event.currentTarget === event.target) setDragOver(false) }}
          onDrop={(event) => {
            event.preventDefault()
            setDragOver(false)
            void upload(event.dataTransfer.files)
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

        <section className="content-card">
          {error && <div className="error-banner">{error}</div>}
          <div className="table-head">
            <span>Имя</span><span>Размер</span><span>Изменён</span><span></span>
          </div>
          {currentPath && (
            <button className="file-row folder-row" onDoubleClick={() => {
              const parts = currentPath.split('/')
              parts.pop()
              setCurrentPath(parts.join('/'))
            }}>
              <span><strong className="file-icon">↑</strong> ..</span>
              <span></span><span></span><span></span>
            </button>
          )}
          {loading ? (
            <div className="empty-state">Загрузка…</div>
          ) : items.length === 0 ? (
            <div className="empty-state">
              <div className="empty-icon">□</div>
              <strong>Папка пуста</strong>
              <span>Загрузите первый файл или создайте папку.</span>
            </div>
          ) : (
            items.map((entry) => (
              <div className={`file-row ${selected.has(entry.path) ? 'selected' : ''}`} key={entry.path}>
                <label className="name-cell">
                  <input type="checkbox" checked={selected.has(entry.path)} onChange={() => toggleSelected(entry)} />
                  <span className={`file-icon ${entry.kind}`}>{entry.kind === 'folder' ? '▰' : '□'}</span>
                  <button className="name-button" onDoubleClick={() => entry.kind === 'folder' ? setCurrentPath(entry.path) : download(entry)}>
                    {entry.name}
                  </button>
                </label>
                <span>{entry.kind === 'folder' ? '—' : formatBytes(entry.size)}</span>
                <span>{new Date(entry.updatedAt).toLocaleString()}</span>
                <span className="row-actions">
                  {entry.kind === 'file' && <button onClick={() => download(entry)} title="Скачать">↓</button>}
                  {entry.kind === 'folder' && <button onClick={() => setCurrentPath(entry.path)} title="Открыть">→</button>}
                  <button onClick={() => void remove(entry)} title="Удалить">×</button>
                </span>
              </div>
            ))
          )}
        </section>

        <footer className="footer-stats">
          <span>{stats.folders} папок</span>
          <span>{stats.files} файлов</span>
          <span>{formatBytes(stats.size)} в текущей папке</span>
        </footer>
      </main>
    </div>
  )
}

createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
)
