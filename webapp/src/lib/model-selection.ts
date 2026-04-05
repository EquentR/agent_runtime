import type { ModelCatalog, ModelCatalogProvider } from '../types/api'

type ModelSelection = {
  providerId: string
  modelId: string
}

type ModelSelectionCatalog = Pick<ModelCatalog, 'providers' | 'default_provider_id' | 'default_model_id'>

type ModelSelectionSource = ModelSelectionCatalog | ModelCatalogProvider[] | null | undefined

export function findModelProvider(providers: ModelCatalogProvider[], providerId: string) {
  return providers.find((provider) => provider.id === providerId) ?? null
}

function resolveProviders(source: ModelSelectionSource) {
  return Array.isArray(source) ? source : source?.providers ?? []
}

function resolveConfiguredDefaultProvider(source: ModelSelectionSource, providers: ModelCatalogProvider[]) {
  if (!source || Array.isArray(source)) {
    return null
  }
  return findModelProvider(providers, source.default_provider_id)
}

function resolveConfiguredDefaultModelId(source: ModelSelectionSource, provider: ModelCatalogProvider | null) {
  if (!source || Array.isArray(source) || !provider) {
    return ''
  }
  if (source.default_provider_id !== provider.id) {
    return ''
  }
  return provider.models.some((model) => model.id === source.default_model_id) ? source.default_model_id : ''
}

export function resolveModelSelection(
  source: ModelSelectionSource,
  providerId: string,
  modelId: string,
): ModelSelection {
  const providers = resolveProviders(source)
  const fallbackProvider = resolveConfiguredDefaultProvider(source, providers) ?? providers[0] ?? null
  const provider = findModelProvider(providers, providerId) ?? fallbackProvider

  if (!provider) {
    return { providerId: '', modelId: '' }
  }

  const configuredDefaultModelId = resolveConfiguredDefaultModelId(source, provider)
  const resolvedModelId = provider.models.some((model) => model.id === modelId)
    ? modelId
    : configuredDefaultModelId || provider.models[0]?.id || ''

  return {
    providerId: provider.id,
    modelId: resolvedModelId,
  }
}
