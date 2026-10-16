package llmgateway

// GenerationBillingCapabilities 是与Adapter版本绑定的受信能力声明。
// 旧Adapter缺省继续从观察结果等待用量；不提供用量的版本须显式返回false。
type GenerationBillingCapabilities interface {
	ProvidesUsageEvidence() bool
}

func (r *GenerationRuntime) externalGenerationBilling(task generationTask) bool {
	registration, ok := r.adapters[task.deploymentID+"/"+task.adapterVersion]
	if !ok {
		return false
	}
	capability, ok := registration.Adapter.(GenerationBillingCapabilities)
	return ok && !capability.ProvidesUsageEvidence()
}

func (*RunningHubImages) ProvidesUsageEvidence() bool { return false }
