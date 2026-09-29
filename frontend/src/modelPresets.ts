export const modelPresets = [
  {
    id: "compatible",
    name: "OpenAI 兼容",
    mark: "AI",
    baseURL: "",
    model: "",
    help: "填写服务商提供的接口根地址和模型 ID。",
  },
  {
    id: "deepseek",
    name: "DeepSeek",
    mark: "DS",
    baseURL: "https://api.deepseek.com",
    model: "deepseek-flash",
    help: "使用 DeepSeek 开放平台的 API Key。",
  },
  {
    id: "kimi",
    name: "Kimi 开放平台",
    mark: "K",
    baseURL: "https://api.moonshot.cn/v1",
    model: "kimi-k3",
    help: "使用 Moonshot 开放平台的 API Key；Kimi Code 请选对应预设。",
  },
  {
    id: "kimi-code",
    name: "Kimi Code",
    mark: "K",
    baseURL: "https://api.kimi.com/coding/v1",
    model: "kimi-for-coding",
    help: "使用 Kimi Code 的 API Key，与开放平台的密钥不同。",
  },
  {
    id: "glm",
    name: "GLM · 智谱",
    mark: "GL",
    baseURL: "https://open.bigmodel.cn/api/paas/v4",
    model: "glm-5.3",
    help: "使用智谱开放平台的 API Key；其他接入地址可在下方修改。",
  },
];
export const presetFor = (id: string) =>
  modelPresets.find((p) => p.id === id) ?? modelPresets[0];
