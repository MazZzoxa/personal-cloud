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

function App() {
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
            <span>v0.2.0</span>
          </div>
        </div>
        <nav>
          <button className="nav-item active" onClick={() => { setSearchQuery(''); setMobileNavOpen(false) }}><span>▦</span> Файлы</button>
          <button className="nav-item" disabled><span>⌁</span> Чат <small>v0.3</small></button>
          <button className="nav-item" disabled><span>⚙</span> Настройки <small>v0.4</small></button>
        </nav>
        <div className="sidebar-note">
          <span>Локальное хранилище</span>
          <strong>Домашний ПК</strong>
          <small>Файлы остаются на вашем устройстве.</small>
        </div>
      </aside>

      <main className="main">
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
      </main>
    </div>
  )
}

createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
)
