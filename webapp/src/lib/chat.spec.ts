import { describe, expect, it } from 'vitest'

import { formatToolParams } from './chat'

describe('formatToolParams', () => {
  it('formats generate_image params as an emoji summary with full prompt and options', () => {
    const prompt = 'draw a cinematic scene with readable storefront signage, warm sunset reflections, and detailed brush texture'

    const formatted = formatToolParams('generate_image', JSON.stringify({
      prompt,
      quality: 'high',
      size: '1536x1024',
      n: 3,
    }))

    expect(formatted).toContain('🎨')
    expect(formatted).toContain(prompt)
    expect(formatted).toContain('质量: high')
    expect(formatted).toContain('大小: 1536x1024')
    expect(formatted).toContain('数量: 3')
    expect(formatted).not.toContain('"prompt"')
    expect(formatted).not.toContain('{')
  })

  it('formats edit_image params as an emoji summary with full prompt and options', () => {
    const prompt = 'replace the background with a clean product studio backdrop while keeping the subject unchanged'

    const formatted = formatToolParams('edit_image', JSON.stringify({
      prompt,
      source_attachment_ids: ['att_source'],
      quality: 'medium',
      size: '1024x1536',
      n: 2,
    }))

    expect(formatted).toContain('🎨✏️')
    expect(formatted).toContain(prompt)
    expect(formatted).toContain('质量: medium')
    expect(formatted).toContain('大小: 1024x1536')
    expect(formatted).toContain('数量: 2')
    expect(formatted).not.toContain('"prompt"')
    expect(formatted).not.toContain('{')
  })
})
