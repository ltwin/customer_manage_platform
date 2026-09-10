package httpapi

import (
	"github.com/gin-gonic/gin"
	"github.com/samson/customer-manage-platform/backend/internal/creativelibrary"
)

func registerCreativeLibrary(r *gin.RouterGroup, h *handlers) {
	w := ServerInterfaceWrapper{Handler: h, ErrorHandler: func(c *gin.Context, _ error, status int) {
		abortError(c, status, CodeValidationFailed, "请求参数不合法")
	}}
	r.POST("/creative/asset-groups", w.CreateCreativeGroup)
	r.POST("/creative/asset-groups/:id/rename", w.RenameCreativeGroup)
	r.POST("/creative/asset-groups/:id/move", w.MoveCreativeGroup)
	r.POST("/creative/asset-groups/:id/delete", w.DeleteCreativeGroup)
	r.POST("/creative/tags", w.CreateCreativeTag)
	r.POST("/creative/tags/:id/edit", w.EditCreativeTag)
	r.POST("/creative/tags/:id/delete", w.DeleteCreativeTag)
	r.POST("/creative/tag-categories", w.CreateCreativeCategory)
	r.POST("/creative/tag-categories/:id/edit", w.EditCreativeCategory)
	r.POST("/creative/tag-categories/:id/delete", w.DeleteCreativeCategory)
	r.POST("/creative/assets/:id/metadata", w.UpdateCreativeAssetMetadata)
	r.POST("/creative/assets/batch-organize", w.OrganizeCreativeAssets)
	r.POST("/creative/library-settings", w.SaveCreativeLibrarySettings)
	r.POST("/creative/assets/:id/trash", w.TrashCreativeAsset)
	r.POST("/creative/assets/batch-trash", w.BatchTrashCreativeAssets)
	r.POST("/creative/assets/:id/restore", w.RestoreCreativeAsset)
	r.POST("/creative/assets/batch-restore", w.BatchRestoreCreativeAssets)
	r.POST("/creative/assets/:id/purge", w.PurgeCreativeAsset)
	r.POST("/creative/assets/batch-purge", w.BatchPurgeCreativeAssets)
	r.GET("/creative/asset-groups", w.ListCreativeGroups)
	r.GET("/creative/tags", w.ListCreativeTags)
	r.GET("/creative/tag-categories", w.ListCreativeCategories)
	r.GET("/creative/library-settings", w.GetCreativeLibrarySettings)
}
func (h *handlers) CreateCreativeGroup(c *gin.Context, _ CreateCreativeGroupParams) {
	h.creativeWrite(c, "", "", creativelibrary.CreateGroup)
}
func (h *handlers) RenameCreativeGroup(c *gin.Context, id string, _ RenameCreativeGroupParams) {
	h.creativeWrite(c, "group_id", id, creativelibrary.RenameGroup)
}
func (h *handlers) MoveCreativeGroup(c *gin.Context, id string, _ MoveCreativeGroupParams) {
	h.creativeWrite(c, "group_id", id, creativelibrary.MoveGroup)
}
func (h *handlers) DeleteCreativeGroup(c *gin.Context, id string, _ DeleteCreativeGroupParams) {
	h.creativeWrite(c, "group_id", id, creativelibrary.DeleteGroup)
}
func (h *handlers) CreateCreativeTag(c *gin.Context, _ CreateCreativeTagParams) {
	h.creativeWrite(c, "", "", creativelibrary.CreateTag)
}
func (h *handlers) EditCreativeTag(c *gin.Context, id string, _ EditCreativeTagParams) {
	h.creativeWrite(c, "tag_id", id, creativelibrary.EditTag)
}
func (h *handlers) DeleteCreativeTag(c *gin.Context, id string, _ DeleteCreativeTagParams) {
	h.creativeWrite(c, "tag_id", id, creativelibrary.DeleteTag)
}
func (h *handlers) CreateCreativeCategory(c *gin.Context, _ CreateCreativeCategoryParams) {
	h.creativeWrite(c, "", "", creativelibrary.CreateCategory)
}
func (h *handlers) EditCreativeCategory(c *gin.Context, id string, _ EditCreativeCategoryParams) {
	h.creativeWrite(c, "category_id", id, creativelibrary.EditCategory)
}
func (h *handlers) DeleteCreativeCategory(c *gin.Context, id string, _ DeleteCreativeCategoryParams) {
	h.creativeWrite(c, "category_id", id, creativelibrary.DeleteCategory)
}
func (h *handlers) UpdateCreativeAssetMetadata(c *gin.Context, id string, _ UpdateCreativeAssetMetadataParams) {
	h.creativeWrite(c, "asset_id", id, creativelibrary.Metadata)
}
func (h *handlers) OrganizeCreativeAssets(c *gin.Context, _ OrganizeCreativeAssetsParams) {
	h.creativeWrite(c, "", "", creativelibrary.Organize)
}
func (h *handlers) SaveCreativeLibrarySettings(c *gin.Context, _ SaveCreativeLibrarySettingsParams) {
	h.creativeWrite(c, "", "", creativelibrary.SaveSettings)
}
func (h *handlers) TrashCreativeAsset(c *gin.Context, id string, _ TrashCreativeAssetParams) {
	h.creativeWrite(c, "asset_id", id, creativelibrary.Trash)
}
func (h *handlers) BatchTrashCreativeAssets(c *gin.Context, _ BatchTrashCreativeAssetsParams) {
	h.creativeWrite(c, "", "", creativelibrary.BatchTrash)
}
func (h *handlers) RestoreCreativeAsset(c *gin.Context, id string, _ RestoreCreativeAssetParams) {
	h.creativeWrite(c, "asset_id", id, creativelibrary.Restore)
}
func (h *handlers) BatchRestoreCreativeAssets(c *gin.Context, _ BatchRestoreCreativeAssetsParams) {
	h.creativeWrite(c, "", "", creativelibrary.BatchRestore)
}
func (h *handlers) PurgeCreativeAsset(c *gin.Context, id string, _ PurgeCreativeAssetParams) {
	h.creativeWrite(c, "asset_id", id, creativelibrary.Purge)
}
func (h *handlers) BatchPurgeCreativeAssets(c *gin.Context, _ BatchPurgeCreativeAssetsParams) {
	h.creativeWrite(c, "", "", creativelibrary.BatchPurge)
}
func (h *handlers) ListCreativeGroups(c *gin.Context) {
	scope, ok := h.creativeScope(c)
	if !ok {
		return
	}
	creativeRead(c, func() (creativelibrary.GroupPage, error) {
		return creativelibrary.ListGroups(c.Request.Context(), scope)
	})
}
func (h *handlers) ListCreativeTags(c *gin.Context) {
	scope, ok := h.creativeScope(c)
	if !ok {
		return
	}
	creativeRead(c, func() (creativelibrary.TagPage, error) { return creativelibrary.ListTags(c.Request.Context(), scope) })
}
func (h *handlers) ListCreativeCategories(c *gin.Context) {
	scope, ok := h.creativeScope(c)
	if !ok {
		return
	}
	creativeRead(c, func() (creativelibrary.CategoryPage, error) {
		return creativelibrary.ListCategories(c.Request.Context(), scope)
	})
}
func (h *handlers) GetCreativeLibrarySettings(c *gin.Context) {
	scope, ok := h.creativeScope(c)
	if !ok {
		return
	}
	creativeRead(c, func() (creativelibrary.Settings, error) {
		return creativelibrary.GetSettings(c.Request.Context(), scope)
	})
}
