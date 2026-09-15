<template>
  <section class="rounded-lg border border-gray-200 dark:border-dark-600">
    <div class="flex flex-wrap items-start justify-between gap-3 p-3">
      <div class="min-w-0">
        <p class="text-sm font-medium text-gray-900 dark:text-white">
          {{ t('admin.accounts.cnProviders.clineModels.title') }}
        </p>
        <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
          {{ t('admin.accounts.cnProviders.clineModels.hint') }}
        </p>
      </div>
      <button
        type="button"
        class="btn btn-secondary btn-sm"
        data-testid="cline-models-fetch"
        :disabled="!canFetch || loading"
        @click="fetchModels"
      >
        <svg v-if="loading" class="mr-1.5 h-3.5 w-3.5 animate-spin" fill="none" viewBox="0 0 24 24">
          <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4" />
          <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z" />
        </svg>
        {{ loading ? t('admin.accounts.cnProviders.clineModels.fetching') : t('admin.accounts.cnProviders.clineModels.fetch') }}
      </button>
    </div>

    <p v-if="!canFetch" class="px-3 pb-3 text-xs text-gray-500 dark:text-gray-400">
      {{ t('admin.accounts.cnProviders.clineModels.needKey') }}
    </p>
    <p v-else-if="error" class="px-3 pb-3 text-xs text-red-600 dark:text-red-400" role="alert">
      {{ error }}
    </p>
    <p v-else-if="!fetchedAt && configuredRows.length === 0" class="px-3 pb-3 text-xs text-gray-500 dark:text-gray-400">
      {{ t('admin.accounts.cnProviders.clineModels.notFetched') }}
    </p>

    <div
      v-if="canFetch && (fetchedAt || configuredRows.length > 0)"
      class="border-t border-gray-200 dark:border-dark-600"
    >
      <div class="flex flex-wrap items-center gap-2 p-3">
        <input
          v-model="search"
          type="text"
          class="input flex-1"
          data-testid="cline-models-search"
          :placeholder="t('admin.accounts.cnProviders.clineModels.searchPlaceholder')"
        />
        <button
          type="button"
          class="btn btn-secondary btn-sm"
          data-testid="cline-models-probe-all"
          :disabled="probeTargetCount === 0 || probingAll"
          @click="probeAll"
        >
          <svg v-if="probingAll" class="mr-1.5 h-3.5 w-3.5 animate-spin" fill="none" viewBox="0 0 24 24">
            <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4" />
            <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z" />
          </svg>
          {{ t('admin.accounts.cnProviders.clineModels.probeAll', { count: probeTargetCount }) }}
        </button>
        <span v-if="fetchedAt" class="text-xs text-gray-500 dark:text-gray-400">
          {{ t('admin.accounts.cnProviders.clineModels.fetchedAt') }} {{ formatDateTimeToMinute(fetchedAt) }}
        </span>
      </div>

      <p v-if="showWhitelistHint" class="px-3 pb-3 text-xs text-gray-500 dark:text-gray-400">
        {{ t('admin.accounts.cnProviders.clineModels.whitelistEmptyHint') }}
      </p>

      <p v-if="totalRows === 0" class="px-3 pb-3 text-xs text-gray-500 dark:text-gray-400">
        {{ t('admin.accounts.cnProviders.clineModels.empty') }}
      </p>

      <div
        v-for="section in visibleSections"
        :key="section.key"
        class="border-t border-gray-100 dark:border-dark-700"
      >
        <div class="flex items-center justify-between gap-2 px-3 py-2">
          <div class="flex items-center gap-2">
            <span class="text-xs font-medium uppercase tracking-wide text-gray-500 dark:text-gray-400">
              {{ section.label }}
            </span>
            <span class="badge badge-gray">{{ section.rows.length }}</span>
          </div>
          <button
            v-if="section.key === CATALOG_SECTION_KEY"
            type="button"
            class="text-xs text-primary-600 hover:underline dark:text-primary-400"
            @click="catalogOpen = !catalogOpen"
          >
            {{ catalogOpen ? t('admin.accounts.cnProviders.clineModels.catalogHide') : t('admin.accounts.cnProviders.clineModels.catalogShow') }}
          </button>
        </div>

        <p v-if="section.truncated > 0" class="px-3 pb-2 text-xs text-gray-500 dark:text-gray-400">
          {{ t('admin.accounts.cnProviders.clineModels.catalogTruncated', { count: CATALOG_RENDER_LIMIT, total: section.rows.length + section.truncated }) }}
        </p>

        <div class="space-y-2 px-3 pb-3">
          <div
            v-for="row in section.rows"
            :key="row.id"
            class="rounded-lg border border-gray-200 bg-gray-50/40 p-2.5 dark:border-dark-600 dark:bg-dark-800/40"
            :data-testid="`cline-model-row-${row.id}`"
          >
            <div class="flex flex-wrap items-start justify-between gap-2">
              <div class="min-w-0">
                <div class="flex items-center gap-1.5">
                  <PlatformIcon platform="cline" size="xs" />
                  <span class="break-all font-mono text-xs text-gray-900 dark:text-white">{{ row.id }}</span>
                </div>
                <p v-if="row.description" class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                  {{ row.description }}
                </p>
                <div class="mt-1 flex flex-wrap items-center gap-1.5">
                  <span class="badge badge-gray">
                    {{ t(`admin.accounts.cnProviders.clineModels.pipeline.${preferenceOf(row.id).pipeline}`) }}
                  </span>
                  <span v-if="inWhitelist(row.id)" class="badge badge-primary">
                    {{ t('admin.accounts.cnProviders.clineModels.whitelistBadge') }}
                  </span>
                  <span v-if="probedAt[row.id]" class="text-xs text-gray-400 dark:text-gray-500">
                    {{ t('admin.accounts.cnProviders.clineModels.probedAt') }} {{ formatDateTimeToMinute(probedAt[row.id]) }}
                  </span>
                </div>
              </div>
              <div class="flex shrink-0 flex-wrap items-center gap-1.5">
                <button
                  type="button"
                  class="btn btn-secondary btn-sm"
                  :data-testid="`cline-model-probe-${row.id}`"
                  :disabled="!!probing[row.id] || !canFetch"
                  @click="probe([row.id])"
                >
                  <svg v-if="probing[row.id]" class="mr-1.5 h-3.5 w-3.5 animate-spin" fill="none" viewBox="0 0 24 24">
                    <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4" />
                    <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z" />
                  </svg>
                  {{ probing[row.id] ? t('admin.accounts.cnProviders.clineModels.probing') : t('admin.accounts.cnProviders.clineModels.probe') }}
                </button>
                <button
                  v-if="canEditWhitelist"
                  type="button"
                  :class="[
                    'rounded-full border px-2 py-0.5 text-xs transition-colors',
                    inWhitelist(row.id)
                      ? 'border-primary-300 bg-primary-50 text-primary-700 hover:bg-primary-100 dark:border-primary-700 dark:bg-primary-900/30 dark:text-primary-300'
                      : 'border-gray-300 text-gray-600 hover:bg-gray-100 dark:border-dark-500 dark:text-gray-300 dark:hover:bg-dark-600'
                  ]"
                  :data-testid="`cline-model-whitelist-${row.id}`"
                  @click="toggleWhitelist(row.id)"
                >
                  {{ inWhitelist(row.id) ? t('admin.accounts.cnProviders.clineModels.whitelistRemove') : t('admin.accounts.cnProviders.clineModels.whitelistAdd') }}
                </button>
              </div>
            </div>

            <p v-if="rowError[row.id]" class="mt-2 text-xs text-red-600 dark:text-red-400" role="alert">
              {{ rowError[row.id] }}
            </p>

            <div class="mt-2 flex flex-wrap items-center gap-2">
              <span class="text-xs text-gray-500 dark:text-gray-400">
                {{ t('admin.accounts.cnProviders.clineModels.modeLabel') }}
              </span>
              <Select
                class="w-40"
                :model-value="preferenceOf(row.id).mode"
                :options="modeOptions"
                :disabled="!hasProviders(row.id)"
                :searchable="false"
                :aria-label="t('admin.accounts.cnProviders.clineModels.modeLabel')"
                :data-testid="`cline-model-mode-${row.id}`"
                @update:model-value="setMode(row.id, $event)"
              />
              <span v-if="!hasProviders(row.id)" class="text-xs text-gray-500 dark:text-gray-400">
                {{ hasProbeResult(row.id) ? t('admin.accounts.cnProviders.clineModels.noChoice') : t('admin.accounts.cnProviders.clineModels.notProbed') }}
              </span>
              <button
                v-else-if="preferenceOf(row.id).mode !== 'auto'"
                type="button"
                class="text-xs text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200"
                @click="clearProviders(row.id)"
              >
                {{ t('admin.accounts.cnProviders.clineModels.clearProviders') }}
              </button>
            </div>

            <div
              v-if="hasProviders(row.id) && preferenceOf(row.id).mode !== 'auto'"
              class="mt-2 flex flex-wrap gap-1.5"
            >
              <button
                v-for="provider in providersOf(row.id)"
                :key="provider"
                type="button"
                :class="[
                  'rounded-full px-2 py-0.5 text-xs transition-colors',
                  isProviderSelected(row.id, provider)
                    ? 'bg-primary-100 text-primary-700 dark:bg-primary-900/30 dark:text-primary-300'
                    : 'bg-gray-100 text-gray-600 hover:bg-gray-200 dark:bg-dark-700 dark:text-gray-300 dark:hover:bg-dark-600'
                ]"
                @click="toggleProvider(row.id, provider)"
              >
                {{ providerLabel(row.id, provider) }}
              </button>
            </div>
          </div>
        </div>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import Select from '@/components/common/Select.vue'
import { formatDateTimeToMinute } from '@/utils/format'
import {
  fetchModels as fetchClineModels,
  probeProviders as probeClineProviders,
  type ClineCatalogEntry,
  type ClineModelGroup,
  type ClineModelPipeline,
  type ClineModelProviderMode,
  type ClineModelProviders
} from '@/api/admin/cline'

// 面板只把「有实际注入意义」的条目写回 credentials：auto（不注入）与空供应商列表不落盘，
// 避免把探测过程中的临时状态持久化。
const CATALOG_SECTION_KEY = 'catalog'
/** 未拉取模型清单时的默认分区：账号白名单与已存上游偏好里的模型。 */
const CONFIGURED_SECTION_KEY = 'configured'
/** 目录模型可达数百条，默认折叠且仅渲染前 N 条（配合筛选框使用）。 */
const CATALOG_RENDER_LIMIT = 50

interface PanelRow {
  id: string
  description?: string
}

interface PanelSection {
  key: string
  label: string
  rows: PanelRow[]
  truncated: number
}

const props = defineProps<{
  /** 新建流程：直接使用输入框中的 Key */
  apiKey?: string
  /** 编辑流程：使用后端已存储的 Key */
  accountId?: number
  accountMode?: 'coding' | 'payg'
  modelValue: ClineModelProviders
  /** 账号模型白名单（credentials.model_mapping 中 from===to 的条目）。 */
  modelWhitelist?: string[]
}>()

const emit = defineEmits<{
  (e: 'update:modelValue', value: ClineModelProviders): void
  (e: 'update:modelWhitelist', value: string[]): void
}>()

const { t } = useI18n()

interface RowPreference {
  pipeline: ClineModelPipeline
  mode: ClineModelProviderMode
  providers: string[]
}

const loading = ref(false)
const probingAll = ref(false)
const error = ref('')
const fetchedAt = ref('')
const groups = ref<ClineModelGroup[]>([])
const catalog = ref<ClineCatalogEntry[]>([])
const catalogOpen = ref(false)
const search = ref('')
const probing = ref<Record<string, boolean>>({})
const rowError = ref<Record<string, string>>({})
const probedAt = ref<Record<string, string>>({})
const availableProviders = ref<Record<string, string[]>>({})
const prefs = ref<Record<string, RowPreference>>({})
const lastEmitted = ref('')

const canFetch = computed(
  () => !!props.apiKey?.trim() || (props.accountId != null && props.accountId > 0)
)

const modeOptions = computed(() => [
  { value: 'auto', label: t('admin.accounts.cnProviders.clineModels.modeAuto') },
  { value: 'only', label: t('admin.accounts.cnProviders.clineModels.modeOnly') },
  { value: 'order', label: t('admin.accounts.cnProviders.clineModels.modeOrder') }
])

const VALID_PIPELINES: ClineModelPipeline[] = ['planner', 'direct', 'private', 'unknown']
const VALID_MODES: ClineModelProviderMode[] = ['auto', 'only', 'order']

function preferenceOf(modelId: string): RowPreference {
  return prefs.value[modelId] ?? { pipeline: 'unknown', mode: 'auto', providers: [] }
}

function providersOf(modelId: string): string[] {
  const probed = availableProviders.value[modelId] ?? []
  const known = new Set(probed)
  for (const provider of preferenceOf(modelId).providers) known.add(provider)
  return Array.from(known)
}

function hasProviders(modelId: string): boolean {
  return providersOf(modelId).length > 0
}

function isProviderSelected(modelId: string, provider: string): boolean {
  return preferenceOf(modelId).providers.includes(provider)
}

function providerLabel(modelId: string, provider: string): string {
  const preference = preferenceOf(modelId)
  if (preference.mode !== 'order') return provider
  const index = preference.providers.indexOf(provider)
  return index < 0 ? provider : `${index + 1}. ${provider}`
}

/** 是否已拿到过该模型的上游信息：本次会话探测过，或上次保存的偏好里带了管道。 */
function hasProbeResult(modelId: string): boolean {
  return (
    availableProviders.value[modelId] !== undefined ||
    preferenceOf(modelId).pipeline !== 'unknown'
  )
}

/** 白名单由父组件同步，未传入时不渲染快捷加入按钮。 */
const canEditWhitelist = computed(() => Array.isArray(props.modelWhitelist))

const showWhitelistHint = computed(
  () => canEditWhitelist.value && (props.modelWhitelist ?? []).length === 0
)

function inWhitelist(modelId: string): boolean {
  return (props.modelWhitelist ?? []).includes(modelId)
}

/** 一键加入/移出账号模型白名单：省去复制模型名再回到白名单选择器里查找。 */
function toggleWhitelist(modelId: string) {
  const current = props.modelWhitelist ?? []
  const next = current.includes(modelId)
    ? current.filter((item) => item !== modelId)
    : [...current, modelId]
  emit('update:modelWhitelist', next)
}

/** 默认渲染源：白名单顺序优先，再补上已存上游偏好里的模型。 */
const configuredModelIds = computed(() => {
  const ids: string[] = []
  const seen = new Set<string>()
  const push = (raw: string) => {
    const id = raw.trim()
    if (!id || seen.has(id)) return
    seen.add(id)
    ids.push(id)
  }
  for (const id of props.modelWhitelist ?? []) push(String(id))
  for (const id of Object.keys(prefs.value)) push(id)
  return ids
})

/** 已拉取清单里出现过的模型不重复渲染在默认分区。 */
const configuredRows = computed<PanelRow[]>(() => {
  const fetched = new Set<string>()
  for (const group of groups.value) {
    for (const model of group.models) fetched.add(model.id)
  }
  for (const model of catalog.value) fetched.add(model.id)
  return configuredModelIds.value.filter((id) => !fetched.has(id)).map((id) => ({ id }))
})

function normalizeProviders(value: unknown): ClineModelProviders {
  if (!value || typeof value !== 'object') return {}
  const result: ClineModelProviders = {}
  for (const [modelId, raw] of Object.entries(value as Record<string, unknown>)) {
    if (!modelId || !raw || typeof raw !== 'object') continue
    const entry = raw as Record<string, unknown>
    const pipeline = VALID_PIPELINES.includes(entry.pipeline as ClineModelPipeline)
      ? (entry.pipeline as ClineModelPipeline)
      : 'unknown'
    const mode = VALID_MODES.includes(entry.mode as ClineModelProviderMode)
      ? (entry.mode as ClineModelProviderMode)
      : 'auto'
    const providers = Array.isArray(entry.providers)
      ? entry.providers.filter((item): item is string => typeof item === 'string' && item.length > 0)
      : []
    result[modelId] = { pipeline, mode, providers }
  }
  return result
}

function buildPersistedValue(): ClineModelProviders {
  const result: ClineModelProviders = {}
  for (const [modelId, preference] of Object.entries(prefs.value)) {
    if (preference.mode === 'auto' || preference.providers.length === 0) continue
    result[modelId] = {
      pipeline: preference.pipeline,
      mode: preference.mode,
      providers: [...preference.providers]
    }
  }
  return result
}

function emitValue() {
  const value = buildPersistedValue()
  lastEmitted.value = JSON.stringify(value)
  emit('update:modelValue', value)
}

/** 单选/取消供应商：order 模式下先选的排在前面，selection 顺序即优先顺序。 */
function toggleProvider(modelId: string, provider: string) {
  const preference = preferenceOf(modelId)
  const selected = preference.providers.includes(provider)
  const providers = selected
    ? preference.providers.filter((item) => item !== provider)
    : [...preference.providers, provider]
  prefs.value = {
    ...prefs.value,
    [modelId]: { ...preference, providers }
  }
}

function clearProviders(modelId: string) {
  const preference = preferenceOf(modelId)
  prefs.value = {
    ...prefs.value,
    [modelId]: { ...preference, providers: [] }
  }
}

function setMode(modelId: string, value: string | number | boolean | null) {
  const mode = VALID_MODES.includes(value as ClineModelProviderMode)
    ? (value as ClineModelProviderMode)
    : 'auto'
  const preference = preferenceOf(modelId)
  prefs.value = {
    ...prefs.value,
    [modelId]: { ...preference, mode }
  }
}

const sections = computed<PanelSection[]>(() => {
  const keyword = search.value.trim().toLowerCase()
  const matches = (row: PanelRow) =>
    !keyword ||
    row.id.toLowerCase().includes(keyword) ||
    (row.description ?? '').toLowerCase().includes(keyword)

  const result: PanelSection[] = []

  const configured = configuredRows.value.filter(matches)
  if (configured.length > 0) {
    result.push({
      key: CONFIGURED_SECTION_KEY,
      label: t('admin.accounts.cnProviders.clineModels.configured'),
      rows: configured,
      truncated: 0
    })
  }

  for (const group of groups.value) {
    const rows = group.models
      .map((model) => ({ id: model.id, description: model.description }))
      .filter(matches)
    if (rows.length > 0) result.push({ key: group.key, label: group.label, rows, truncated: 0 })
  }

  const catalogRows = catalog.value
    .map((model) => ({ id: model.id }))
    .filter(matches)
  if (catalogRows.length > 0) {
    result.push({
      key: CATALOG_SECTION_KEY,
      label: t('admin.accounts.cnProviders.clineModels.catalog'),
      rows: catalogRows.slice(0, CATALOG_RENDER_LIMIT),
      truncated: Math.max(0, catalogRows.length - CATALOG_RENDER_LIMIT)
    })
  }
  return result
})

const visibleSections = computed(() =>
  sections.value.filter(
    (section) => section.key !== CATALOG_SECTION_KEY || catalogOpen.value
  )
)

const totalRows = computed(() =>
  sections.value.reduce((sum, section) => sum + section.rows.length, 0)
)

const probeTargetCount = computed(() =>
  visibleSections.value.reduce((sum, section) => sum + section.rows.length, 0)
)

async function fetchModels() {
  if (!canFetch.value || loading.value) return
  loading.value = true
  error.value = ''
  try {
    const result = await fetchClineModels({
      api_key: props.apiKey?.trim() || undefined,
      account_id: props.apiKey?.trim() ? undefined : props.accountId,
      account_mode: props.accountMode
    })
    groups.value = result.groups ?? []
    catalog.value = result.catalog ?? []
    fetchedAt.value = result.fetched_at
    probing.value = {}
    rowError.value = {}
  } catch (err) {
    error.value = (err as { message?: string })?.message ||
      t('admin.accounts.cnProviders.clineModels.fetchFailed')
  } finally {
    loading.value = false
  }
}

async function probe(modelIds: string[]) {
  const targets = modelIds.filter((id) => id && !probing.value[id])
  if (targets.length === 0 || !canFetch.value) return
  const all = targets.length === probeTargetCount.value && targets.length > 1
  if (all) probingAll.value = true
  const next = { ...probing.value }
  for (const id of targets) next[id] = true
  probing.value = next
  const nextRowError = { ...rowError.value }
  for (const id of targets) delete nextRowError[id]
  rowError.value = nextRowError
  try {
    const result = await probeClineProviders({
      api_key: props.apiKey?.trim() || undefined,
      account_id: props.apiKey?.trim() ? undefined : props.accountId,
      model_ids: targets
    })
    const models = result.models ?? {}
    const nextPrefs = { ...prefs.value }
    const nextProbedAt = { ...probedAt.value }
    const nextAvailable = { ...availableProviders.value }
    for (const id of targets) {
      const probed = models[id]
      if (!probed) continue
      const providers = Array.isArray(probed.providers)
        ? probed.providers.filter((item) => typeof item === 'string' && item.length > 0)
        : []
      nextAvailable[id] = providers
      nextProbedAt[id] = probed.probed_at
      const previous = nextPrefs[id] ?? { pipeline: 'unknown', mode: 'auto', providers: [] }
      const keep = previous.providers.filter((item) => providers.includes(item))
      nextPrefs[id] = {
        pipeline: validPipeline(probed.pipeline),
        mode: previous.mode,
        providers: keep
      }
    }
    prefs.value = nextPrefs
    probedAt.value = nextProbedAt
    availableProviders.value = nextAvailable
  } catch (err) {
    const message = (err as { message?: string })?.message ||
      t('admin.accounts.cnProviders.clineModels.probeFailed')
    const nextErrors = { ...rowError.value }
    for (const id of targets) nextErrors[id] = message
    rowError.value = nextErrors
  } finally {
    const cleared = { ...probing.value }
    for (const id of targets) delete cleared[id]
    probing.value = cleared
    if (all) probingAll.value = false
  }
}

function validPipeline(value: unknown): ClineModelPipeline {
  return VALID_PIPELINES.includes(value as ClineModelPipeline)
    ? (value as ClineModelPipeline)
    : 'unknown'
}

function probeAll() {
  const ids = visibleSections.value.flatMap((section) => section.rows.map((row) => row.id))
  void probe(ids)
}

watch(prefs, emitValue, { deep: true })

watch(
  () => props.modelValue,
  (value) => {
    const next = normalizeProviders(value)
    if (JSON.stringify(next) === lastEmitted.value) return
    prefs.value = next
  },
  { immediate: true, deep: true }
)
</script>
