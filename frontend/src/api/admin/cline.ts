/**
 * Admin Cline API endpoints.
 * 按账号实时获取可用模型（订阅分组 + 目录）并探测每个模型的上游供应商。
 */

import { apiClient } from '../client'

/** 模型上游管道：planner = Vercel AI Gateway；direct = OpenRouter；private = 固定上游。 */
export type ClineModelPipeline = 'planner' | 'direct' | 'private' | 'unknown'

/** auto = 不注入；only = 白名单钉住；order = 优先顺序（其余回退）。 */
export type ClineModelProviderMode = 'auto' | 'only' | 'order'

export interface ClineModelProviderPreference {
  pipeline: ClineModelPipeline
  mode: ClineModelProviderMode
  providers: string[]
}

/** 账号 credentials.model_providers 的存储形态。 */
export type ClineModelProviders = Record<string, ClineModelProviderPreference>

export interface ClineModelEntry {
  id: string
  name: string
  description?: string
  tags?: string[]
}

export interface ClineModelGroup {
  key: string
  label: string
  models: ClineModelEntry[]
}

export interface ClineCatalogEntry {
  id: string
  owned_by?: string
}

export interface ClineModelsResult {
  mode: 'coding' | 'payg'
  groups: ClineModelGroup[]
  /** 按量付费目录；订阅模式下可能为空。 */
  catalog?: ClineCatalogEntry[]
  /** RFC3339 */
  fetched_at: string
}

export interface ClineProviderProbeEntry {
  pipeline: ClineModelPipeline
  providers: string[]
  /** RFC3339 */
  probed_at: string
}

export interface ClineProviderProbeResult {
  models: Record<string, ClineProviderProbeEntry>
}

export interface ClineModelsRequest {
  /** 新建流程直接传 Key；编辑已有账号改传 account_id 用后端存储的 Key。 */
  api_key?: string
  account_id?: number
  account_mode?: 'coding' | 'payg'
}

export interface ClineProviderProbeRequest {
  api_key?: string
  account_id?: number
  model_ids: string[]
}

export async function fetchModels(body: ClineModelsRequest): Promise<ClineModelsResult> {
  const { data } = await apiClient.post<ClineModelsResult>('/admin/cline/models', body)
  return data
}

export async function probeProviders(
  body: ClineProviderProbeRequest
): Promise<ClineProviderProbeResult> {
  const { data } = await apiClient.post<ClineProviderProbeResult>('/admin/cline/provider-probe', body)
  return data
}

/**
 * 空响应同账号重试次数上限，与后端 maxClineEmptyStreamRetryCount 保持一致。
 * 上限取 60 是因为上游额度被抢时几乎瞬时回错，需要高频长重试才有机会挤进空出的名额。
 */
export const CLINE_EMPTY_STREAM_RETRY_MAX_COUNT = 60
/**
 * 空响应同账号重试次数默认值，与后端 defaultClineEmptyStreamRetryCount 保持一致。
 * 默认即上限：少量重试对这种额度竞争等同于直接失败。
 */
export const CLINE_EMPTY_STREAM_RETRY_DEFAULT_COUNT = 60

/** 归一化凭据里的 cline 空响应重试次数：非法值回退默认值，并夹到 [0, 上限]。 */
export function normalizeClineEmptyStreamRetryCount(value: unknown): number {
  const parsed = typeof value === 'number' ? value : Number.parseInt(String(value ?? ''), 10)
  if (!Number.isFinite(parsed)) return CLINE_EMPTY_STREAM_RETRY_DEFAULT_COUNT
  return Math.min(Math.max(Math.trunc(parsed), 0), CLINE_EMPTY_STREAM_RETRY_MAX_COUNT)
}

/** 空响应同账号重试间隔的下限（毫秒），与后端 minClineEmptyStreamRetryIntervalMs 保持一致。 */
export const CLINE_EMPTY_STREAM_RETRY_INTERVAL_MIN_MS = 100
/** 空响应同账号重试间隔的上限（毫秒），与后端 maxClineEmptyStreamRetryIntervalMs 保持一致。 */
export const CLINE_EMPTY_STREAM_RETRY_INTERVAL_MAX_MS = 5000
/** 空响应同账号重试间隔的默认值（毫秒），与后端 defaultClineEmptyStreamRetryIntervalMs 保持一致。 */
export const CLINE_EMPTY_STREAM_RETRY_INTERVAL_DEFAULT_MS = 2000

/**
 * 归一化凭据里的 cline 空响应重试间隔：非法值回退默认值，并夹到 [下限, 上限]。
 * 该间隔是固定值，不做退避——退避会让重试恰好睡在名额空出的那一刻。
 */
export function normalizeClineEmptyStreamRetryInterval(value: unknown): number {
  const parsed = typeof value === 'number' ? value : Number.parseInt(String(value ?? ''), 10)
  if (!Number.isFinite(parsed)) return CLINE_EMPTY_STREAM_RETRY_INTERVAL_DEFAULT_MS
  return Math.min(
    Math.max(Math.trunc(parsed), CLINE_EMPTY_STREAM_RETRY_INTERVAL_MIN_MS),
    CLINE_EMPTY_STREAM_RETRY_INTERVAL_MAX_MS
  )
}

export default {
  fetchModels,
  probeProviders
}
