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
 * Built-in tool params formatters.
 * Each formatter receives the parsed JSON arguments object and returns
 * a human-friendly one-line summary, or null to fall back to pretty JSON.
 */
const toolParamsFormatters: Record<string, (args: Record<string, unknown>) => string | null> = {
  list_files(args) {
    const path = args.path || '.'
    const parts: string[] = []
    if (args.recursive) parts.push('递归')
    if (args.max_depth) parts.push(`深度=${args.max_depth}`)
    return parts.length > 0 ? `📂 ${path} (${parts.join(', ')})` : `📂 ${path}`
  },

  read_file(args) {
    const path = args.path || ''
    if (!path) return null
    const startLine = Number(args.start_line || 1)
    const lineCount = Number(args.line_count || 0)
    if (lineCount > 0) {
      return `📄 ${path} L${startLine}-${startLine + lineCount - 1}`
    }
    if (startLine > 1) {
      return `📄 ${path} L${startLine}+`
    }
    return `📄 ${path}`
  },

  write_file(args) {
    const path = args.path || ''
    if (!path) return null
    const content = typeof args.content === 'string' ? args.content : ''
    if (content.length > 0) {
      return `📝 ${path} (${content.length} bytes)`
    }
    return `📝 ${path}`
  },

  exec_command(args) {
    const command = String(args.command || '')
    if (!command) return null
    const cmdArgs = Array.isArray(args.args) ? args.args.map(String) : []
    const full = [command, ...cmdArgs].join(' ')
    const cwd = args.working_directory ? ` [${args.working_directory}]` : ''
    return `$ ${full}${cwd}`
  },

  search_file(args) {
    const pattern = args.pattern || ''
    const path = args.path || '.'
    return `🔍 "${pattern}" in ${path}/`
  },

  grep_file(args) {
    const pattern = args.pattern || ''
    const path = args.path || ''
    return `🔍 "${pattern}" in ${path}`
  },

  delete_file(args) {
    return `🗑️ ${args.path || ''}`
  },

  move_file(args) {
    const from = args.source || args.from || args.path || ''
    const to = args.destination || args.to || args.new_path || ''
    return `📦 ${from} → ${to}`
  },

  copy_file(args) {
    const from = args.source || args.from || args.path || ''
    const to = args.destination || args.to || args.new_path || ''
    return `📋 ${from} → ${to}`
  },

  http_request(args) {
    const method = String(args.method || 'GET').toUpperCase()
    const url = args.url || ''
    return `📡 ${method} ${url}`
  },

  kill_process(args) {
    const pid = args.pid || ''
    const signal = args.signal || ''
    return signal ? `⚠️ kill ${pid} (${signal})` : `⚠️ kill ${pid}`
  },

  check_command(args) {
    return `❓ which ${args.command || ''}`
  },

  web_search(args) {
    return `🌐 "${args.query || ''}"`
  },

  generate_image(args) {
    return formatImageToolParams(args, 'generate')
  },

  edit_image(args) {
    return formatImageToolParams(args, 'edit')
  },
}

function formatImageToolParams(args: Record<string, unknown>, mode: 'generate' | 'edit') {
  const prompt = String(args.prompt || '').trim()
  const details = [
    stringDetail('质量', args.quality),
    stringDetail('大小', args.size),
    stringDetail('数量', args.n ?? args.count),
  ].filter(Boolean)
  const prefix = mode === 'edit' ? '🎨✏️' : '🎨'
  const lines = [`${prefix} ${prompt || '(no prompt)'}`]
  if (details.length > 0) {
    lines.push(details.join(' · '))
  }
  return lines.join('\n')
}

function stringDetail(label: string, value: unknown) {
  if (value === null || value === undefined) {
    return ''
  }
  const text = String(value).trim()
  return text ? `${label}: ${text}` : ''
}

/**
 * Built-in tool display formatters.
 * Each formatter receives the raw result content (usually JSON) and returns
 * a human-friendly text representation, or null to fall back to default.
 */
const toolResultFormatters: Record<string, (content: string) => string | null> = {
  generate_image(content: string) {
    const json = tryParseJSON(content)
    if (!json || typeof json !== 'object' || Array.isArray(json)) return null
    const obj = json as Record<string, unknown>
    if (Array.isArray(obj.images) && obj.images.length > 0) return '已生成'
    if (typeof obj.error === 'string' && obj.error.trim()) return obj.error.trim()
    if (typeof obj.message === 'string' && obj.message.trim()) return obj.message.trim()
    if (Array.isArray(obj.failed_images) && obj.failed_images.length > 0) return '生成失败'
    return null
  },

  edit_image(content: string) {
    return toolResultFormatters.generate_image(content)
  },

  list_files(content: string) {
    const json = tryParseJSON(content)
    if (!json || typeof json !== 'object') return null
    const obj = json as Record<string, unknown>

    // Backend returns { entries: [{path, type},...], returned_entries, remaining_count, truncated }
    const entries = obj.entries
    if (Array.isArray(entries)) {
      if (entries.length === 0) return '(empty directory)'
      const lines = entries.map((item: unknown) => {
        if (typeof item === 'string') return item
        if (item && typeof item === 'object') {
          const entry = item as Record<string, unknown>
          const path = String(entry.path ?? entry.name ?? '')
          const isDir = entry.type === 'dir' || entry.is_dir
          return isDir ? `📁 ${path}/` : `📄 ${path}`
        }
        return String(item)
      })
      const truncated = obj.truncated
      const remaining = Number(obj.remaining_count || 0)
      if (truncated && remaining > 0) {
        lines.push(`... (还有 ${remaining} 项)`)
      }
      return lines.join('\n')
    }

    // Fallback: top-level array (legacy)
    if (Array.isArray(json)) {
      if ((json as unknown[]).length === 0) return '(empty directory)'
      return (json as unknown[]).map((item: unknown) => {
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
    const json = tryParseJSON(content)
    if (!json || typeof json !== 'object') return null
    const obj = json as Record<string, unknown>

    // Backend returns { path, start_line, end_line, total_lines, content, has_more, next_start_line, ... }
    if ('content' in obj && typeof obj.content === 'string') {
      const path = obj.path || ''
      const startLine = Number(obj.start_line || 1)
      const endLine = Number(obj.end_line || 0)
      const totalLines = Number(obj.total_lines || 0)
      const lines: string[] = []
      if (path) {
        const rangeStr = endLine > 0 ? ` L${startLine}-${endLine}/${totalLines}` : ''
        lines.push(`── ${path}${rangeStr} ──`)
      }
      lines.push(obj.content as string)
      if (obj.has_more && totalLines > endLine) {
        lines.push(`\n... (还有 ${totalLines - endLine} 行)`)
      }
      return lines.join('\n')
    }
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
    const json = tryParseJSON(content)
    if (!json || typeof json !== 'object') return null
    const obj = json as Record<string, unknown>

    // Backend returns { success, exit_code, stdout, stderr, timed_out, cwd, ... }
    if ('exit_code' in obj || 'success' in obj) {
      const success = Boolean(obj.success)
      const exitCode = Number(obj.exit_code ?? 0)
      const stdout = typeof obj.stdout === 'string' ? obj.stdout : ''
      const stderr = typeof obj.stderr === 'string' ? obj.stderr : ''
      const timedOut = Boolean(obj.timed_out)
      const lines: string[] = []

      if (timedOut) {
        lines.push(`⏱️ 执行超时 (exit ${exitCode})`)
      } else if (success) {
        lines.push(`✅ exit ${exitCode}`)
      } else {
        lines.push(`❌ exit ${exitCode}`)
      }

      if (stdout.trim()) {
        lines.push(stdout.trim())
      }
      if (stderr.trim() && !success) {
        if (stdout.trim()) lines.push('---')
        lines.push(stderr.trim())
      }
      return lines.join('\n')
    }
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
        const totalMatches = Number(obj.total_matches || matches.length)
        if (matches.length === 0) return '🔍 未找到匹配结果'

        // Group matches by file path for better display
        const grouped = new Map<string, Array<{ line: string; text: string }>>()
        for (const m of matches.slice(0, 30)) {
          if (m && typeof m === 'object') {
            const match = m as Record<string, unknown>
            const path = String(match.path || '')
            const line = String(match.line_number || match.line || '')
            const text = String(match.text || match.content || match.match || '')
            if (!grouped.has(path)) grouped.set(path, [])
            grouped.get(path)!.push({ line, text })
          }
        }

        if (grouped.size > 0) {
          const lines: string[] = [`🔍 找到 ${totalMatches} 个匹配`]
          for (const [path, items] of grouped) {
            if (path) lines.push(`  ${path}`)
            for (const item of items) {
              const prefix = path ? '    ' : '  '
              lines.push(item.line ? `${prefix}L${item.line}: ${item.text}` : `${prefix}${item.text}`)
            }
          }
          if (obj.truncated) {
            const remaining = totalMatches - matches.length
            if (remaining > 0) lines.push(`  ... (还有 ${remaining} 个匹配)`)
          }
          return lines.join('\n')
        }

        // Flat fallback
        const flatLines = matches.slice(0, 20).map((m: unknown) => {
          if (typeof m === 'string') return m
          if (m && typeof m === 'object') {
            const match = m as Record<string, unknown>
            const line = match.line_number || match.line || ''
            const text = match.text || match.content || match.match || ''
            return line ? `L${line}: ${text}` : String(text)
          }
          return String(m)
        })
        const header = `🔍 找到 ${totalMatches} 个匹配`
        return totalMatches > 20
          ? `${header}\n${flatLines.join('\n')}\n... (还有 ${totalMatches - 20} 个匹配)`
          : `${header}\n${flatLines.join('\n')}`
      }
    }
    return null
  },

  grep_file(content: string) {
    return toolResultFormatters.search_file(content)
  },

  get_system_info(content: string) {
    const json = tryParseJSON(content)
    if (json && typeof json === 'object') {
      const obj = json as Record<string, unknown>
      const lines: string[] = []
      if (obj.os) lines.push(`🖥️ OS: ${obj.os}`)
      if (obj.arch) lines.push(`⚙️ Arch: ${obj.arch}`)
      if (obj.hostname) lines.push(`📍 Host: ${obj.hostname}`)
      if (obj.num_cpu || obj.cpu_count || obj.cpus) lines.push(`🧮 CPUs: ${obj.num_cpu || obj.cpu_count || obj.cpus}`)
      if (obj.go_version) lines.push(`🔧 Go: ${obj.go_version}`)
      if (obj.memory || obj.total_memory) lines.push(`💾 Memory: ${obj.memory || obj.total_memory}`)
      if (obj.working_directory || obj.cwd) lines.push(`📂 CWD: ${obj.working_directory || obj.cwd}`)
      if (lines.length > 0) return lines.join('\n')
    }
    return null
  },

  list_processes(content: string) {
    const json = tryParseJSON(content)
    if (!json || typeof json !== 'object') return null
    const obj = json as Record<string, unknown>

    // Backend returns { processes: [...] }
    const processes = Array.isArray(obj.processes) ? obj.processes : Array.isArray(json) ? (json as unknown[]) : null
    if (!processes) return null
    if (processes.length === 0) return '(no processes)'

    const header = `进程列表 (${processes.length} 个)`
    const lines = processes.slice(0, 30).map((p: unknown) => {
      if (typeof p === 'string') return p
      if (p && typeof p === 'object') {
        const proc = p as Record<string, unknown>
        return `PID ${proc.pid || proc.PID}: ${proc.name || proc.Name || proc.command || proc.Command || 'unknown'}`
      }
      return String(p)
    })
    return processes.length > 30
      ? `${header}\n${lines.join('\n')}\n... (还有 ${processes.length - 30} 个)`
      : `${header}\n${lines.join('\n')}`
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
      const contentType = obj.content_type || ''
      const bodyPreview = typeof obj.body === 'string' ? obj.body.slice(0, 500) : ''
      const lines: string[] = []
      if (status) lines.push(`📡 HTTP ${status}${url ? ` - ${url}` : ''}`)
      // Check content_type field from backend
      if (contentType) {
        lines.push(`Content-Type: ${contentType}`)
      } else if (obj.headers && typeof obj.headers === 'object') {
        const ct = (obj.headers as Record<string, unknown>)['content-type'] ||
                   (obj.headers as Record<string, unknown>)['Content-Type'] || ''
        if (ct) lines.push(`Content-Type: ${ct}`)
      }
      if (obj.truncated) {
        lines.push(`(截断: 原始 ${obj.original_size || '?'} bytes, 返回 ${obj.returned_size || '?'} bytes)`)
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
 * Format tool input parameters with tool-specific display optimization.
 * Returns a human-friendly one-line summary for known built-in tools,
 * or falls back to formatMessageContent (pretty JSON) for unknown tools.
 */
export function formatToolParams(toolName: string, content: string): string {
  const trimmed = content.trim()
  if (!trimmed) {
    return '(no params)'
  }

  const parsed = tryParseJSON(trimmed)
  if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
    const formatter = toolParamsFormatters[toolName]
    if (formatter) {
      const formatted = formatter(parsed as Record<string, unknown>)
      if (formatted) return formatted
    }
  }

  // Fallback: pretty-print JSON or return as plain text
  return formatMessageContent(trimmed)
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
