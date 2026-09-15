import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import ClineModelProviderPanel from '../ClineModelProviderPanel.vue'
import type { ClineModelProviders } from '@/api/admin/cline'

const { fetchModels, probeProviders } = vi.hoisted(() => ({
  fetchModels: vi.fn(),
  probeProviders: vi.fn()
}))

vi.mock('@/api/admin/cline', () => ({ fetchModels, probeProviders }))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key })
  }
})

vi.mock('@/components/common/PlatformIcon.vue', () => ({
  default: { name: 'PlatformIcon', template: '<i />' }
}))

// Select 是自定义下拉，测试里换成原生 select 以便直接触发 value 变更。
const SelectStub = {
  name: 'Select',
  props: {
    modelValue: { type: String, default: '' },
    options: { type: Array, default: () => [] },
    disabled: { type: Boolean, default: false }
  },
  emits: ['update:modelValue'],
  template:
    '<select :disabled="disabled" :value="modelValue" @change="$emit(\'update:modelValue\', $event.target.value)">' +
    '<option v-for="o in options" :key="o.value" :value="o.value">{{ o.label }}</option></select>'
}

const MODEL_ID = 'cline-pass/deepseek-v4.1-flash'

function buildWrapper(props: Record<string, unknown> = {}) {
  return mount(ClineModelProviderPanel, {
    props: { modelValue: {}, apiKey: 'sk_test_key', accountMode: 'coding', ...props },
    global: { stubs: { Select: SelectStub } }
  })
}

async function fetchCatalog(wrapper: ReturnType<typeof buildWrapper>) {
  await wrapper.get('[data-testid="cline-models-fetch"]').trigger('click')
  await flushPromises()
}

describe('ClineModelProviderPanel', () => {
  beforeEach(() => {
    fetchModels.mockReset()
    probeProviders.mockReset()
    fetchModels.mockResolvedValue({
      mode: 'coding',
      groups: [
        {
          key: 'coding',
          label: 'cline-pass',
          models: [{ id: MODEL_ID, name: 'DeepSeek V4.1 Flash' }]
        }
      ],
      catalog: [{ id: 'openai/gpt-5.5', owned_by: 'openai' }],
      fetched_at: '2026-09-15T00:00:00Z'
    })
    probeProviders.mockResolvedValue({
      models: { [MODEL_ID]: { pipeline: 'planner', providers: ['togetherai', 'baseten'], probed_at: '2026-09-15T00:00:00Z' } }
    })
  })

  it('未填 Key 时不允许拉取模型', async () => {
    const wrapper = buildWrapper({ apiKey: '' })
    expect(wrapper.get('[data-testid="cline-models-fetch"]').attributes('disabled')).toBeDefined()
    expect(wrapper.text()).toContain('admin.accounts.cnProviders.clineModels.needKey')
    expect(fetchModels).not.toHaveBeenCalled()
  })

  it('按当前模式实时拉取模型并渲染分组', async () => {
    const wrapper = buildWrapper()
    await fetchCatalog(wrapper)

    expect(fetchModels).toHaveBeenCalledWith({
      api_key: 'sk_test_key',
      account_id: undefined,
      account_mode: 'coding'
    })
    expect(wrapper.get(`[data-testid="cline-model-row-${MODEL_ID}"]`).text()).toContain(MODEL_ID)
  })

  it('探测供应商后按 only 模式选择，并只把有效偏好写回 credentials', async () => {
    const wrapper = buildWrapper()
    await fetchCatalog(wrapper)

    // 未探测前没有可选供应商，提示的是「未探测」而不是「上游固定管道」
    expect(wrapper.text()).toContain('admin.accounts.cnProviders.clineModels.notProbed')

    await wrapper.get(`[data-testid="cline-model-probe-${MODEL_ID}"]`).trigger('click')
    await flushPromises()
    expect(probeProviders).toHaveBeenCalledWith({
      api_key: 'sk_test_key',
      account_id: undefined,
      model_ids: [MODEL_ID]
    })

    // mode 切到 only 后才会展开供应商选择
    const modeSelect = wrapper.get(`[data-testid="cline-model-mode-${MODEL_ID}"]`)
    await modeSelect.setValue('only')
    await flushPromises()

    const providerButton = wrapper.findAll('button').find((b) => b.text() === 'togetherai')
    expect(providerButton).toBeTruthy()
    await providerButton!.trigger('click')
    await flushPromises()

    const emitted = wrapper.emitted('update:modelValue')!.at(-1)![0] as ClineModelProviders
    expect(emitted).toEqual({
      [MODEL_ID]: { pipeline: 'planner', mode: 'only', providers: ['togetherai'] }
    })
  })

  it('order 模式下选择顺序即优先顺序', async () => {
    const wrapper = buildWrapper()
    await fetchCatalog(wrapper)
    await wrapper.get(`[data-testid="cline-model-probe-${MODEL_ID}"]`).trigger('click')
    await flushPromises()

    await wrapper.get(`[data-testid="cline-model-mode-${MODEL_ID}"]`).setValue('order')
    await flushPromises()

    for (const name of ['togetherai', 'baseten']) {
      const button = wrapper.findAll('button').find((b) => b.text().endsWith(name))
      await button!.trigger('click')
      await flushPromises()
    }

    const emitted = wrapper.emitted('update:modelValue')!.at(-1)![0] as ClineModelProviders
    expect(emitted[MODEL_ID]).toEqual({
      pipeline: 'planner',
      mode: 'order',
      providers: ['togetherai', 'baseten']
    })
  })

  it('回退到 auto 模式后不再持久化该模型', async () => {
    const wrapper = buildWrapper({
      modelValue: { [MODEL_ID]: { pipeline: 'planner', mode: 'only', providers: ['togetherai'] } }
    })
    await fetchCatalog(wrapper)

    await wrapper.get(`[data-testid="cline-model-mode-${MODEL_ID}"]`).setValue('auto')
    await flushPromises()

    const emitted = wrapper.emitted('update:modelValue')!.at(-1)![0] as ClineModelProviders
    expect(emitted).toEqual({})
  })

  it('编辑流程用 account_id 走已存储的 Key', async () => {
    const wrapper = buildWrapper({ apiKey: '', accountId: 42 })
    await fetchCatalog(wrapper)

    expect(fetchModels).toHaveBeenCalledWith({
      api_key: undefined,
      account_id: 42,
      account_mode: 'coding'
    })
  })

  it('未拉取清单时按白名单与已存偏好预渲染，可直接探测上游', async () => {
    const wrapper = buildWrapper({
      modelWhitelist: [MODEL_ID],
      modelValue: {
        [MODEL_ID]: { pipeline: 'planner', mode: 'only', providers: ['deepseek'] }
      }
    })

    expect(fetchModels).not.toHaveBeenCalled()
    const row = wrapper.get(`[data-testid="cline-model-row-${MODEL_ID}"]`)
    expect(row.text()).toContain(MODEL_ID)
    expect(row.text()).toContain('admin.accounts.cnProviders.clineModels.pipeline.planner')
    expect(row.text()).toContain('admin.accounts.cnProviders.clineModels.whitelistBadge')

    // 上次选中的上游直接可见，无需先探测
    const providerChip = row.findAll('button').find((item) => item.text() === 'deepseek')
    expect(providerChip).toBeTruthy()

    await row.get(`[data-testid="cline-model-probe-${MODEL_ID}"]`).trigger('click')
    await flushPromises()
    expect(probeProviders).toHaveBeenCalledWith({
      api_key: 'sk_test_key',
      account_id: undefined,
      model_ids: [MODEL_ID]
    })
  })

  it('用快捷按钮把模型加入/移出白名单', async () => {
    const wrapper = buildWrapper({ modelWhitelist: [], modelValue: {} })
    await fetchCatalog(wrapper)

    expect(wrapper.text()).toContain('admin.accounts.cnProviders.clineModels.whitelistEmptyHint')

    const button = wrapper.get(`[data-testid="cline-model-whitelist-${MODEL_ID}"]`)
    expect(button.text()).toBe('admin.accounts.cnProviders.clineModels.whitelistAdd')
    await button.trigger('click')
    expect(wrapper.emitted('update:modelWhitelist')!.at(-1)![0]).toEqual([MODEL_ID])

    const inList = buildWrapper({ modelWhitelist: [MODEL_ID], modelValue: {} })
    await fetchCatalog(inList)
    const removeButton = inList.get(`[data-testid="cline-model-whitelist-${MODEL_ID}"]`)
    expect(removeButton.text()).toBe('admin.accounts.cnProviders.clineModels.whitelistRemove')
    await removeButton.trigger('click')
    expect(inList.emitted('update:modelWhitelist')!.at(-1)![0]).toEqual([])
  })
})
