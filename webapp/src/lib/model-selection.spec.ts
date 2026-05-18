import { describe, expect, it } from 'vitest'

import type { ModelCatalogProvider } from '../types/api'
import { findModelProvider, resolveModelSelection } from './model-selection'

const providers: ModelCatalogProvider[] = [
  {
    id: 'openai',
    name: 'OpenAI',
    models: [
      { id: 'gpt-5.4', name: 'GPT 5.4', type: 'chat' },
      { id: 'gpt-5.4-mini', name: 'GPT 5.4 Mini', type: 'chat' },
    ],
  },
  {
    id: 'google',
    name: 'Google',
    models: [{ id: 'gemini-2.5-flash', name: 'Gemini 2.5 Flash', type: 'chat' }],
  },
]

describe('findModelProvider', () => {
  it('returns the provider that matches the requested id', () => {
    expect(findModelProvider(providers, 'google')?.id).toBe('google')
  })

  it('returns null when the provider id is unknown', () => {
    expect(findModelProvider(providers, 'missing')).toBeNull()
  })
})

describe('resolveModelSelection', () => {
  it('keeps the requested model when the provider offers it', () => {
    expect(resolveModelSelection(providers, 'openai', 'gpt-5.4-mini')).toMatchObject({
      providerId: 'openai',
      modelId: 'gpt-5.4-mini',
    })
  })

  it('falls back to the provider default model when the requested model is unavailable', () => {
    expect(resolveModelSelection(providers, 'google', 'missing-model')).toMatchObject({
      providerId: 'google',
      modelId: 'gemini-2.5-flash',
    })
  })

  it('falls back to the first provider when the requested provider is unavailable', () => {
    expect(resolveModelSelection(providers, 'missing-provider', 'gpt-5.4')).toMatchObject({
      providerId: 'openai',
      modelId: 'gpt-5.4',
    })
  })

  it('returns empty ids when the catalog has no providers', () => {
    expect(resolveModelSelection([], 'openai', 'gpt-5.4')).toMatchObject({
      providerId: '',
      modelId: '',
    })
  })

  it('model selection ignores unavailable models', () => {
    expect(resolveModelSelection({
      default_provider_id: 'openai',
      default_model_id: 'disabled-default',
      providers: [
        {
          id: 'empty',
          name: 'Empty',
          models: [],
        },
        {
          id: 'openai',
          name: 'OpenAI',
          models: [
            { id: 'disabled-default', name: 'Disabled Default', type: 'chat', enabled: false },
            { id: 'unavailable', name: 'Unavailable', type: 'chat', available: false },
          ],
        },
        {
          id: 'google',
          name: 'Google',
          models: [
            { id: 'gemini-2.5-flash', name: 'Gemini 2.5 Flash', type: 'chat', enabled: true },
          ],
        },
      ],
    }, 'openai', 'disabled-default')).toMatchObject({
      providerId: 'google',
      modelId: 'gemini-2.5-flash',
    })
  })
})
