package httpapi

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/samson/customer-manage-platform/backend/internal/creativecanvas"
	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/creativelibrary"
)

func (h *handlers) CreateCreativeCharacter(c *gin.Context, _ CreateCreativeCharacterParams) {
	h.creativeWrite(c, "", "", creativelibrary.CreateCharacter)
}

func (h *handlers) SaveCreativeCharacterRevision(c *gin.Context, id string, _ SaveCreativeCharacterRevisionParams) {
	h.creativeWrite(c, "asset_id", id, creativelibrary.SaveCharacterRevision)
}

func (h *handlers) SuggestCreativeCharacterIPs(c *gin.Context, params SuggestCreativeCharacterIPsParams) {
	scope, ok := h.creativeScope(c)
	if !ok {
		return
	}
	query, limit := "", 20
	if params.Query != nil {
		query = *params.Query
	}
	if params.Limit != nil {
		limit = *params.Limit
	}
	creativeRead(c, func() (creativelibrary.CharacterIPSuggestions, error) {
		return creativelibrary.SuggestCharacterIPs(c.Request.Context(), scope, query, limit)
	})
}

func creativeCharacterError(c *gin.Context, err error) bool {
	var invalid *creativecontent.CharacterFieldError
	var unavailable *creativecontent.CharacterImageError
	var ambiguous *creativelibrary.AmbiguousCharacterWorkError
	var generation *creativecanvas.CharacterGenerationError
	if errors.As(err, &generation) {
		var payload CreativeCharacterGenerationDetails
		for _, card := range generation.Characters {
			payload.Characters = append(payload.Characters, struct {
				ContentRevisionId string `json:"content_revision_id"`
				Name              string `json:"name"`
				ViewCount         int    `json:"view_count"`
			}{ContentRevisionId: card.ContentRevisionID, Name: card.Name, ViewCount: card.ViewCount})
		}
		if generation.ImageIndex > 0 {
			payload.ImageIndex, payload.ContentRevisionId = &generation.ImageIndex, &generation.RevisionID
		} else {
			payload.Limit, payload.Total = &generation.Limit, &generation.Total
		}
		var details ErrorDetails
		if err := details.FromCreativeCharacterGenerationDetails(payload); err != nil {
			abortError(c, http.StatusInternalServerError, CodeInternal, "内部错误")
		} else {
			abortErrorWithTypedDetails(c, http.StatusUnprocessableEntity, "creative_generation_parameters", generation.Error(), details)
		}
		return true
	}
	var details ErrorDetails
	var encodeErr error
	code := CodeValidationFailed
	var message string
	switch {
	case errors.As(err, &invalid):
		message = "角色卡字段不合法"
		encodeErr = details.FromCreativeCharacterFieldDetails(CreativeCharacterFieldDetails{Fields: invalid.Fields})
	case errors.As(err, &unavailable):
		code = "creative_image_unavailable"
		message = "角色卡中有图片不可用，请更换后重试"
		encodeErr = details.FromCreativeCharacterImageDetails(CreativeCharacterImageDetails{ContentRevisionIds: unavailable.RevisionIDs})
	case errors.As(err, &ambiguous):
		message = "作品简称匹配多部作品，请同时选择所属 IP"
		var workDetails CreativeCharacterWorkDetails
		for _, choice := range ambiguous.Candidates {
			workDetails.Candidates = append(workDetails.Candidates, struct {
				Ip   string `json:"ip"`
				Work string `json:"work"`
			}{Ip: choice.IP, Work: choice.Work})
		}
		encodeErr = details.FromCreativeCharacterWorkDetails(workDetails)
	default:
		return false
	}
	if encodeErr != nil {
		abortError(c, http.StatusInternalServerError, CodeInternal, "内部错误")
	} else {
		abortErrorWithTypedDetails(c, http.StatusBadRequest, code, message, details)
	}
	return true
}
