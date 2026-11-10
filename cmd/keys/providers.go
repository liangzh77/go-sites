package main

import "sort"

var providerPresets = []ProviderPreset{
	{
		ID: "openai", Name: "OpenAI", Category: "国际官方",
		Protocol: protocolOpenAIChat, BaseURL: "https://api.openai.com/v1", AuthMode: authBearer,
		DiscoverModels: true, Documentation: "https://platform.openai.com/docs/api-reference",
	},
	{
		ID: "anthropic", Name: "Anthropic", Category: "国际官方",
		Protocol: protocolAnthropic, BaseURL: "https://api.anthropic.com/v1", AuthMode: authAnthropic,
		DiscoverModels: true, Documentation: "https://docs.anthropic.com/en/api/messages",
	},
	{
		ID: "gemini", Name: "Google Gemini", Category: "国际官方",
		Protocol: protocolGemini, BaseURL: "https://generativelanguage.googleapis.com/v1beta",
		AuthMode: authGemini, DiscoverModels: true,
		Documentation: "https://ai.google.dev/api/generate-content",
	},
	{
		ID: "xai", Name: "xAI", Category: "国际官方",
		Protocol: protocolOpenAIChat, BaseURL: "https://api.x.ai/v1", AuthMode: authBearer,
		DiscoverModels: true, Documentation: "https://docs.x.ai/developers/rest-api-reference",
	},
	{
		ID: "mistral", Name: "Mistral", Category: "国际官方",
		Protocol: protocolOpenAIChat, BaseURL: "https://api.mistral.ai/v1", AuthMode: authBearer,
		DiscoverModels: true, Documentation: "https://docs.mistral.ai/api/",
	},
	{
		ID: "deepseek", Name: "DeepSeek", Category: "国内官方",
		Protocol: protocolOpenAIChat, BaseURL: "https://api.deepseek.com", AuthMode: authBearer,
		DiscoverModels: true, Documentation: "https://api-docs.deepseek.com/api/deepseek-api/",
	},
	{
		ID: "alibaba", Name: "阿里云百炼", Category: "国内官方",
		Protocol: protocolOpenAIChat, BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1",
		AuthMode: authBearer, DiscoverModels: false,
		Documentation: "https://help.aliyun.com/zh/model-studio/compatibility-of-openai-with-dashscope",
	},
	{
		ID: "volcengine", Name: "火山方舟", Category: "国内官方",
		Protocol: protocolOpenAIChat, BaseURL: "https://ark.cn-beijing.volces.com/api/v3",
		AuthMode: authBearer, DiscoverModels: false,
		Documentation: "https://www.volcengine.com/docs/82379/1399008",
	},
	{
		ID: "moonshot", Name: "Moonshot / Kimi", Category: "国内官方",
		Protocol: protocolOpenAIChat, BaseURL: "https://api.moonshot.cn/v1", AuthMode: authBearer,
		DiscoverModels: true, Documentation: "https://platform.moonshot.cn/docs",
	},
	{
		ID: "zhipu", Name: "智谱 GLM", Category: "国内官方",
		Protocol: protocolOpenAIChat, BaseURL: "https://open.bigmodel.cn/api/paas/v4",
		AuthMode: authBearer, DiscoverModels: true,
		Documentation: "https://open.bigmodel.cn/dev/api",
	},
	{
		ID: "minimax", Name: "MiniMax", Category: "国内官方",
		Protocol: protocolMiniMax, BaseURL: "https://api.minimax.chat/v1/text", AuthMode: authBearer,
		DiscoverModels: false, Documentation: "https://platform.minimaxi.com/document/ChatCompletion",
	},
	{
		ID: "qianfan", Name: "百度千帆", Category: "国内官方",
		Protocol: protocolOpenAIChat, BaseURL: "https://qianfan.baidubce.com/v2", AuthMode: authBearer,
		DiscoverModels: false, Documentation: "https://cloud.baidu.com/doc/WENXINWORKSHOP/index.html",
	},
	{
		ID: "tencent", Name: "腾讯混元", Category: "国内官方",
		Protocol: protocolOpenAIChat, BaseURL: "https://api.hunyuan.cloud.tencent.com/v1",
		AuthMode: authBearer, DiscoverModels: false,
		Documentation: "https://cloud.tencent.com/document/product/1729/111007",
	},
	{
		ID: "openrouter", Name: "OpenRouter", Category: "聚合 / 推理平台",
		Protocol: protocolOpenAIChat, BaseURL: "https://openrouter.ai/api/v1", AuthMode: authBearer,
		DiscoverModels: true, Documentation: "https://openrouter.ai/docs/api-reference/overview",
	},
	{
		ID: "siliconflow", Name: "硅基流动", Category: "聚合 / 推理平台",
		Protocol: protocolOpenAIChat, BaseURL: "https://api.siliconflow.cn/v1", AuthMode: authBearer,
		DiscoverModels: true, Documentation: "https://docs.siliconflow.cn/",
	},
	{
		ID: "groq", Name: "Groq", Category: "聚合 / 推理平台",
		Protocol: protocolOpenAIChat, BaseURL: "https://api.groq.com/openai/v1", AuthMode: authBearer,
		DiscoverModels: true, Documentation: "https://console.groq.com/docs/api-reference",
	},
	{
		ID: "together", Name: "Together AI", Category: "聚合 / 推理平台",
		Protocol: protocolOpenAIChat, BaseURL: "https://api.together.xyz/v1", AuthMode: authBearer,
		DiscoverModels: true, Documentation: "https://docs.together.ai/reference/chat-completions",
	},
	{
		ID: "fireworks", Name: "Fireworks", Category: "聚合 / 推理平台",
		Protocol: protocolOpenAIChat, BaseURL: "https://api.fireworks.ai/inference/v1",
		AuthMode: authBearer, DiscoverModels: false, Documentation: "https://docs.fireworks.ai/",
	},
	{
		ID: "custom", Name: "自定义 Provider", Category: "自定义",
		Protocol: protocolOpenAIChat, BaseURL: "", AuthMode: authBearer,
		DiscoverModels: true, Documentation: "", AddressRequired: true,
	},
}

var providerPresetIndex = func() map[string]ProviderPreset {
	index := make(map[string]ProviderPreset, len(providerPresets))
	for _, preset := range providerPresets {
		index[preset.ID] = preset
	}
	return index
}()

func presetByID(id string) (ProviderPreset, bool) {
	preset, ok := providerPresetIndex[id]
	return preset, ok
}

func allPresets() []ProviderPreset {
	out := append([]ProviderPreset(nil), providerPresets...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Category == out[j].Category {
			return out[i].Name < out[j].Name
		}
		return out[i].Category < out[j].Category
	})
	return out
}

func protocolOptions() []ProtocolOption {
	return []ProtocolOption{
		{ID: protocolOpenAIChat, Label: protocolLabels[protocolOpenAIChat]},
		{ID: protocolOpenAIResponses, Label: protocolLabels[protocolOpenAIResponses]},
		{ID: protocolAnthropic, Label: protocolLabels[protocolAnthropic]},
		{ID: protocolGemini, Label: protocolLabels[protocolGemini]},
		{ID: protocolMiniMax, Label: protocolLabels[protocolMiniMax]},
	}
}

func authOptions() []ProtocolOption {
	return []ProtocolOption{
		{ID: authBearer, Label: authLabels[authBearer]},
		{ID: authAnthropic, Label: authLabels[authAnthropic]},
		{ID: authGemini, Label: authLabels[authGemini]},
	}
}

func optionsResponse() OptionsResponse {
	return OptionsResponse{
		Options: AppOptions{
			Protocols: protocolOptions(),
			AuthModes: authOptions(),
			Presets:   allPresets(),
		},
		Status: map[string]string{
			statusUntested:    "未检测",
			statusSuccess:     "调用成功",
			statusAuthFailed:  "认证失败",
			statusQuota:       "余额 / 额度不足",
			statusRateLimited: "限流",
			statusModelDenied: "模型不存在 / 无权限",
			statusParameters:  "参数不兼容",
			statusNetwork:     "网络 / 地址异常",
			statusService:     "服务异常",
			statusUnknown:     "不确定",
		},
	}
}
