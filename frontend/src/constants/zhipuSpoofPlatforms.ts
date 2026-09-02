// GLM Coding Plan 官方支持的工具清单（来源：docs.bigmodel.cn coding plan 接入工具页）。
// 用作 Zhipu GLM 账号"伪装排除平台"多选的选项。
// value 为平台 slug，需与后端 internal/service/zhipu_spoofing.go 的 UA 检测表保持一致；
// 后端未收录 UA 模式的工具（如 pi、hermes-agent）实际无法被识别，选中仅作声明。
export const ZHIPU_SPOOF_PLATFORM_OPTIONS: { value: string; labelKey: string }[] = [
  { value: 'claude-code', labelKey: 'claudeCode' },
  { value: 'claude-ide', labelKey: 'claudeIde' },
  { value: 'codex', labelKey: 'codex' },
  { value: 'opencode', labelKey: 'opencode' },
  { value: 'crush', labelKey: 'crush' },
  { value: 'goose', labelKey: 'goose' },
  { value: 'cursor', labelKey: 'cursor' },
  { value: 'roo', labelKey: 'roo' },
  { value: 'kilo', labelKey: 'kilo' },
  { value: 'cline', labelKey: 'cline' },
  { value: 'droid', labelKey: 'droid' },
  { value: 'openclaw', labelKey: 'openclaw' },
  { value: 'cherry-studio', labelKey: 'cherryStudio' },
  { value: 'trae', labelKey: 'trae' },
  { value: 'qoder', labelKey: 'qoder' },
  { value: 'lingma', labelKey: 'lingma' },
  { value: 'codebuddy', labelKey: 'codebuddy' },
  { value: 'monkeycode', labelKey: 'monkeycode' },
  { value: 'zcode', labelKey: 'zcode' },
  { value: 'pi', labelKey: 'pi' },
  { value: 'hermes-agent', labelKey: 'hermesAgent' }
]
