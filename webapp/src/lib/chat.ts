export const APP_TITLE = 'Agent Runtime'

export function formatConversationTitle(title: string, fallback: string) {
  const trimmed = title.trim()
  return trimmed || fallback
}

export function formatDocumentTitle(title?: string) {
  const trimmed = title?.trim() ?? ''
  return trimmed ? `${trimmed} - ${APP_TITLE}` : APP_TITLE
}

/**
 * Try to parse a string as JSON. Returns the parsed value or null if not valid JSON.
 */
function tryParseJSON(content: string): unknown {
  const trimmed = content.trim()
  if (!trimmed.startsWith('{') && !trimmed.startsWith('[')) {
    return null
  }
  try {
    return JSON.parse(trimmed)
  } catch {
    return null
  }
}

/**
 * Try to pretty-print a JSON string. Returns formatted JSON or null if not valid JSON.
 */
function tryPrettyJSON(content: string): string | null {
  const parsed = tryParseJSON(content)
  if (parsed === null) {
    return null
  }
  return JSON.stringify(parsed, null, 2)
}

/**
 * Built-in tool display formatters.
 * Each formatter receives the raw result content (usually JSON) and returns
 * a human-friendly text representation, or null to fall back to default.
 */
const toolResultFormatters: Record<string, (content: string) => string | null> = {
  list_files(content: string) {
    const json = tryParseJSON(content)
    if (!json) return null
    if (Array.isArray(json)) {
      if (json.length === 0) return '(empty directory)'
      return json.map((item: unknown) => {
        if (typeof item === 'string') return item
        if (item && typeof item === 'object' && 'name' in item) {
          const entry = item as Record<string, unknown>
          const name = String(entry.name ?? '')
          const isDir = entry.is_dir || entry.type === 'dir'
          return isDir ? `📁 ${name}/` : `📄 ${name}`
        }
        return String(item)
      }).join('\n')
    }
    return null
  },

  read_file(content: string) {
    // read_file returns the file content directly; just show as-is
    return null
  },

  write_file(content: string) {
    const json = tryParseJSON(content)
    if (json && typeof json === 'object' && 'path' in json) {
      const obj = json as Record<string, unknown>
      const path = obj.path || obj.file_path || ''
      const bytes = obj.bytes_written || obj.size || ''
      return bytes ? `✅ 已写入: ${path} (${bytes} bytes)` : `✅ 已写入: ${path}`
    }
    if (typeof content === 'string' && content.trim()) {
      return `✅ ${content.trim()}`
    }
    return null
  },

  delete_file(content: string) {
    const json = tryParseJSON(content)
    if (json && typeof json === 'object' && 'path' in json) {
      return `🗑️ 已删除: ${(json as Record<string, unknown>).path}`
    }
    if (typeof content === 'string' && content.trim()) {
      return `🗑️ ${content.trim()}`
    }
    return null
  },

  move_file(content: string) {
    const json = tryParseJSON(content)
    if (json && typeof json === 'object') {
      const obj = json as Record<string, unknown>
      const from = obj.source || obj.from || ''
      const to = obj.destination || obj.to || ''
      if (from && to) return `📦 已移动: ${from} → ${to}`
    }
    if (typeof content === 'string' && content.trim()) {
      return `📦 ${content.trim()}`
    }
    return null
  },

  copy_file(content: string) {
    const json = tryParseJSON(content)
    if (json && typeof json === 'object') {
      const obj = json as Record<string, unknown>
      const from = obj.source || obj.from || ''
      const to = obj.destination || obj.to || ''
      if (from && to) return `📋 已复制: ${from} → ${to}`
    }
    if (typeof content === 'string' && content.trim()) {
      return `📋 ${content.trim()}`
    }
    return null
  },

  exec_command(content: string) {
    // exec_command output is usually raw text (stdout/stderr), show as-is
    return null
  },

  check_command(content: string) {
    const json = tryParseJSON(content)
    if (json && typeof json === 'object') {
      const obj = json as Record<string, unknown>
      const found = obj.found ?? obj.exists ?? obj.available
      const path = obj.path || obj.location || ''
      if (found !== undefined) {
        return found ? `✅ 命令可用${path ? `: ${path}` : ''}` : `❌ 命令不可用`
      }
    }
    return null
  },

  search_file(content: string) {
    const json = tryParseJSON(content)
    if (json && typeof json === 'object') {
      const obj = json as Record<string, unknown>
      const matches = obj.matches || obj.results
      if (Array.isArray(matches)) {
        if (matches.length === 0) return '🔍 未找到匹配结果'
        const lines = matches.slice(0, 20).map((m: unknown) => {
          if (typeof m === 'string') return m
          if (m && typeof m === 'object') {
            const match = m as Record<string, unknown>
            const line = match.line_number || match.line || ''
            const text = match.text || match.content || match.match || ''
            return line ? `L${line}: ${text}` : String(text)
          }
          return String(m)
        })
        const header = `🔍 找到 ${matches.length} 个匹配`
        return matches.length > 20
          ? `${header}\n${lines.join('\n')}\n... (${matches.length - 20} more)`
          : `${header}\n${lines.join('\n')}`
      }
    }
    return null
  },

  grep_file: (content: string) => toolResultFormatters.search_file(content),

  get_system_info(content: string) {
    const json = tryParseJSON(content)
    if (json && typeof json === 'object') {
      const obj = json as Record<string, unknown>
      const lines: string[] = []
      if (obj.os) lines.push(`🖥️ OS: ${obj.os}`)
      if (obj.arch) lines.push(`⚙️ Arch: ${obj.arch}`)
      if (obj.hostname) lines.push(`📍 Host: ${obj.hostname}`)
      if (obj.cpu_count || obj.cpus) lines.push(`🧮 CPUs: ${obj.cpu_count || obj.cpus}`)
      if (obj.memory || obj.total_memory) lines.push(`💾 Memory: ${obj.memory || obj.total_memory}`)
      if (obj.working_directory || obj.cwd) lines.push(`📂 CWD: ${obj.working_directory || obj.cwd}`)
      if (lines.length > 0) return lines.join('\n')
    }
    return null
  },

  list_processes(content: string) {
    const json = tryParseJSON(content)
    if (json && Array.isArray(json)) {
      if (json.length === 0) return '(no processes)'
      const header = `进程列表 (${json.length} 个)`
      const lines = json.slice(0, 30).map((p: unknown) => {
        if (typeof p === 'string') return p
        if (p && typeof p === 'object') {
          const proc = p as Record<string, unknown>
          return `PID ${proc.pid || proc.PID}: ${proc.name || proc.Name || proc.command || proc.Command || 'unknown'}`
        }
        return String(p)
      })
      return json.length > 30
        ? `${header}\n${lines.join('\n')}\n... (${json.length - 30} more)`
        : `${header}\n${lines.join('\n')}`
    }
    return null
  },

  kill_process(content: string) {
    const json = tryParseJSON(content)
    if (json && typeof json === 'object') {
      const obj = json as Record<string, unknown>
      const pid = obj.pid || obj.PID || ''
      const success = obj.success ?? obj.killed ?? obj.ok
      if (success !== undefined) {
        return success ? `✅ 已终止进程 ${pid}` : `❌ 终止进程 ${pid} 失败`
      }
    }
    if (typeof content === 'string' && content.trim()) {
      return content.trim()
    }
    return null
  },

  http_request(content: string) {
    const json = tryParseJSON(content)
    if (json && typeof json === 'object') {
      const obj = json as Record<string, unknown>
      const status = obj.status_code || obj.status || ''
      const url = obj.url || ''
      const bodyPreview = typeof obj.body === 'string' ? obj.body.slice(0, 500) : ''
      const lines: string[] = []
      if (status) lines.push(`📡 HTTP ${status}${url ? ` - ${url}` : ''}`)
      if (obj.headers && typeof obj.headers === 'object') {
        const ct = (obj.headers as Record<string, unknown>)['content-type'] ||
                   (obj.headers as Record<string, unknown>)['Content-Type'] || ''
        if (ct) lines.push(`Content-Type: ${ct}`)
      }
      if (bodyPreview) {
        lines.push('---')
        lines.push(bodyPreview.length >= 500 ? bodyPreview + '...' : bodyPreview)
      }
      if (lines.length > 0) return lines.join('\n')
    }
    return null
  },

  web_search(content: string) {
    const json = tryParseJSON(content)
    if (json && typeof json === 'object') {
      const obj = json as Record<string, unknown>
      const results = obj.results || obj.items || obj.organic
      if (Array.isArray(results)) {
        if (results.length === 0) return '🔍 无搜索结果'
        const lines = results.slice(0, 10).map((r: unknown, i: number) => {
          if (typeof r === 'string') return `${i + 1}. ${r}`
          if (r && typeof r === 'object') {
            const item = r as Record<string, unknown>
            const title = item.title || item.name || ''
            const url = item.url || item.link || ''
            const snippet = item.snippet || item.description || ''
            const parts = [`${i + 1}. ${title}`]
            if (url) parts.push(`   ${url}`)
            if (snippet) parts.push(`   ${String(snippet).slice(0, 120)}`)
            return parts.join('\n')
          }
          return `${i + 1}. ${String(r)}`
        })
        return `🌐 搜索结果 (${results.length} 条)\n${lines.join('\n')}`
      }
      // Some web_search results have a "summary" or "answer" field
      if (obj.summary || obj.answer) {
        return `🌐 ${obj.summary || obj.answer}`
      }
    }
    return null
  },
}

/**
 * Format message content for display. Handles:
 * - Empty content
 * - JSON pretty-printing
 * - Plain text
 */
export function formatMessageContent(content: string) {
  const trimmed = content.trim()
  if (!trimmed) {
    return '(empty message)'
  }
  // Try to pretty-print JSON
  const pretty = tryPrettyJSON(trimmed)
  if (pretty) {
    return pretty
  }
  return trimmed
}

/**
 * Format tool result content with tool-specific display optimization.
 * Returns a human-friendly representation for known built-in tools,
 * or falls back to formatMessageContent for unknown tools.
 */
export function formatToolResult(toolName: string, content: string): string {
  const trimmed = content.trim()
  if (!trimmed) {
    return '(no output)'
  }

  // Try tool-specific formatter first
  const formatter = toolResultFormatters[toolName]
  if (formatter) {
    const formatted = formatter(trimmed)
    if (formatted) return formatted
  }

  // Fallback: pretty-print JSON or return as plain text
  return formatMessageContent(trimmed)
}
