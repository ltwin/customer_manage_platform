package creativecontent

import "github.com/samson/customer-manage-platform/backend/internal/planningmedia"

// generationGate 是生成用途在声明层的权利检查。创作声明命名空间表达
// 不了授权素材的出站标志，所以在该契约扩展存在之前授权素材在这里一律
// 拒绝；摄影师自有素材通过。
func generationGate(in planningmedia.RightsDeclarationInput) error {
	return planningmedia.ValidatePurpose(in, planningmedia.PurposeGenerationReference)
}
