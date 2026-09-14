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

export default {
  fetchModels,
  probeProviders
}
