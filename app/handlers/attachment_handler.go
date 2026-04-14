package handlers

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/EquentR/agent_runtime/core/attachments"
	resp "github.com/EquentR/agent_runtime/pkg/rest"
	"github.com/gin-gonic/gin"
)

const defaultAttachmentDraftTTL = 24 * time.Hour
const maxAttachmentUploadBytes int64 = 10 << 20

var errAttachmentAccessDenied = errors.New("无权访问该附件")

type AttachmentHandler struct {
	store        *attachments.Store
	storage      attachments.Storage
	draftTTL     time.Duration
	middlewares  []gin.HandlerFunc
	authRequired bool
}

type AttachmentResponse struct {
	ID             string     `json:"id"`
	ConversationID string     `json:"conversation_id,omitempty"`
	FileName       string     `json:"file_name"`
	MimeType       string     `json:"mime_type"`
	SizeBytes      int64      `json:"size_bytes"`
	Kind           string     `json:"kind"`
	Status         string     `json:"status"`
	PreviewText    string     `json:"preview_text,omitempty"`
	Width          *int       `json:"width,omitempty"`
	Height         *int       `json:"height,omitempty"`
	ExpiresAt      *time.Time `json:"expires_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

type attachmentDeleteResult struct {
	Deleted bool `json:"deleted"`
}

func NewAttachmentHandler(store *attachments.Store, storage attachments.Storage, draftTTL time.Duration, middlewares ...gin.HandlerFunc) *AttachmentHandler {
	return &AttachmentHandler{
		store:        store,
		storage:      storage,
		draftTTL:     draftTTL,
		middlewares:  middlewares,
		authRequired: len(middlewares) > 0,
	}
}

func (h *AttachmentHandler) Register(rg *gin.RouterGroup) {
	if h == nil || h.store == nil || h.storage == nil {
		return
	}
	options := []resp.WrapperOption{}
	if len(h.middlewares) > 0 {
		options = append(options, resp.WithMiddlewares(h.middlewares...))
	}
	resp.HandlerWrapper(rg, "attachments", []*resp.Handler{
		resp.NewJsonOptionsHandler(h.handleUploadAttachment),
		resp.NewJsonOptionsHandler(h.handleGetAttachment),
		resp.NewHandler(http.MethodGet, "/:id/content", h.handleGetAttachmentContent()),
		resp.NewJsonOptionsHandler(h.handleDeleteAttachment),
	}, options...)
}

// @Summary 上传附件
// @Description 上传一个 multipart 文件，创建 draft 附件并返回展示元数据。
// @Tags attachments
// @Accept multipart/form-data
// @Produce json
// @Param file formData file true "附件文件"
// @Param conversation_id formData string false "会话 ID"
// @Success 200 {object} AttachmentSwaggerResponse
// @Failure 400 {object} ErrorSwaggerResponse
// @Failure 401 {object} ErrorSwaggerResponse
// @Router /attachments [post]
func (h *AttachmentHandler) handleUploadAttachment() (method, relativePath string, wrapper resp.JsonOptionsResultWrapper, opts []resp.WrapperOption) {
	return http.MethodPost, "", func(c *gin.Context) (any, []resp.ResOpt, error) {
		user := currentAuthUser(c)
		if user == nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusUnauthorized)}, errAttachmentAccessDenied
		}

		fileHeader, err := c.FormFile("file")
		if err != nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusBadRequest)}, fmt.Errorf("file is required")
		}
		if fileHeader.Size > maxAttachmentUploadBytes {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusBadRequest)}, fmt.Errorf("uploaded file exceeds %d bytes limit", maxAttachmentUploadBytes)
		}
		reader, err := fileHeader.Open()
		if err != nil {
			return nil, nil, err
		}
		defer reader.Close()

		data, err := io.ReadAll(io.LimitReader(reader, maxAttachmentUploadBytes+1))
		if err != nil {
			return nil, nil, err
		}
		if int64(len(data)) > maxAttachmentUploadBytes {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusBadRequest)}, fmt.Errorf("uploaded file exceeds %d bytes limit", maxAttachmentUploadBytes)
		}
		if len(data) == 0 {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusBadRequest)}, fmt.Errorf("uploaded file cannot be empty")
		}

		mimeType := normalizeAttachmentMimeType(fileHeader.Header.Get("Content-Type"), data)
		if !isSupportedAttachmentMimeType(mimeType) {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusBadRequest)}, fmt.Errorf("mime type %q is not supported", mimeType)
		}

		storedObject, err := h.storage.PutDraft(c.Request.Context(), attachments.PutDraftInput{
			FileName: fileHeader.Filename,
			MimeType: mimeType,
			Data:     data,
		})
		if err != nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusBadRequest)}, err
		}

		expiresAt := time.Now().UTC().Add(h.resolvedDraftTTL())
		previewText, contextText := buildAttachmentTextMetadata(fileHeader.Filename, storedObject.Kind, data)
		attachment, err := h.store.CreateDraft(c.Request.Context(), attachments.CreateDraftInput{
			ConversationID: strings.TrimSpace(c.PostForm("conversation_id")),
			CreatedBy:      user.Username,
			StorageBackend: storedObject.StorageBackend,
			StorageKey:     storedObject.StorageKey,
			FileName:       storedObject.FileName,
			MimeType:       storedObject.MimeType,
			SizeBytes:      storedObject.SizeBytes,
			Kind:           storedObject.Kind,
			PreviewText:    previewText,
			ContextText:    contextText,
			ExpiresAt:      &expiresAt,
		})
		if err != nil {
			_ = h.storage.Delete(c.Request.Context(), storedObject.StorageKey)
			return nil, nil, err
		}
		return buildAttachmentResponse(attachment), nil, nil
	}, nil
}

// @Summary 获取附件元数据
// @Description 返回指定附件的元数据与当前状态。
// @Tags attachments
// @Produce json
// @Param id path string true "附件 ID"
// @Success 200 {object} AttachmentSwaggerResponse
// @Failure 401 {object} ErrorSwaggerResponse
// @Failure 404 {object} ErrorSwaggerResponse
// @Router /attachments/{id} [get]
func (h *AttachmentHandler) handleGetAttachment() (method, relativePath string, wrapper resp.JsonOptionsResultWrapper, opts []resp.WrapperOption) {
	return http.MethodGet, "/:id", func(c *gin.Context) (any, []resp.ResOpt, error) {
		attachment, opts, err := h.loadAccessibleAttachment(c, c.Param("id"))
		if err != nil {
			return nil, opts, err
		}
		return buildAttachmentResponse(attachment), nil, nil
	}, nil
}

// @Summary 获取附件原始内容
// @Description 鉴权后返回附件原始内容流。
// @Tags attachments
// @Produce application/octet-stream
// @Param id path string true "附件 ID"
// @Success 200 {file} string "attachment content"
// @Failure 401 {object} ErrorSwaggerResponse
// @Failure 404 {object} ErrorSwaggerResponse
// @Router /attachments/{id}/content [get]
func (h *AttachmentHandler) handleGetAttachmentContent() gin.HandlerFunc {
	return func(c *gin.Context) {
		attachment, opts, err := h.loadAccessibleAttachment(c, c.Param("id"))
		if err != nil {
			resp.BadJson(c, nil, err, opts...)
			return
		}

		reader, meta, err := h.storage.Open(c.Request.Context(), attachment.StorageKey)
		if err != nil {
			status := []resp.ResOpt{}
			if errors.Is(err, attachments.ErrObjectNotFound) {
				status = append(status, resp.WithCode(http.StatusNotFound))
			}
			resp.BadJson(c, nil, err, status...)
			return
		}
		defer reader.Close()

		fileName := firstNonEmpty(meta.FileName, attachment.FileName)
		headers := map[string]string{}
		if fileName != "" {
			headers["Content-Disposition"] = fmt.Sprintf(`inline; filename="%s"`, fileName)
		}
		c.DataFromReader(http.StatusOK, meta.SizeBytes, firstNonEmpty(meta.MimeType, attachment.MimeType), reader, headers)
	}
}

// @Summary 删除草稿附件
// @Description 删除指定 draft 附件及其底层存储对象。
// @Tags attachments
// @Produce json
// @Param id path string true "附件 ID"
// @Success 200 {object} AttachmentDeleteSwaggerResponse
// @Failure 400 {object} ErrorSwaggerResponse
// @Failure 401 {object} ErrorSwaggerResponse
// @Failure 404 {object} ErrorSwaggerResponse
// @Router /attachments/{id} [delete]
func (h *AttachmentHandler) handleDeleteAttachment() (method, relativePath string, wrapper resp.JsonOptionsResultWrapper, opts []resp.WrapperOption) {
	return http.MethodDelete, "/:id", func(c *gin.Context) (any, []resp.ResOpt, error) {
		attachment, opts, err := h.loadAccessibleAttachment(c, c.Param("id"))
		if err != nil {
			return nil, opts, err
		}
		if attachment.Status != attachments.StatusDraft {
			return attachmentDeleteResult{Deleted: false}, []resp.ResOpt{resp.WithCode(http.StatusBadRequest)}, fmt.Errorf("only draft attachments can be deleted")
		}

		if err := h.storage.Delete(c.Request.Context(), attachment.StorageKey); err != nil && !errors.Is(err, attachments.ErrObjectNotFound) {
			return nil, nil, err
		}
		if err := h.store.DeleteAttachment(c.Request.Context(), attachment.ID); err != nil {
			if errors.Is(err, attachments.ErrAttachmentNotFound) {
				return attachmentDeleteResult{Deleted: false}, []resp.ResOpt{resp.WithCode(http.StatusNotFound)}, err
			}
			return nil, nil, err
		}
		return attachmentDeleteResult{Deleted: true}, nil, nil
	}, nil
}

func (h *AttachmentHandler) loadAccessibleAttachment(c *gin.Context, attachmentID string) (*attachments.Attachment, []resp.ResOpt, error) {
	if h == nil || h.store == nil {
		return nil, nil, fmt.Errorf("attachment store is not configured")
	}
	attachment, err := h.store.GetAttachment(c.Request.Context(), attachmentID)
	if err != nil {
		if errors.Is(err, attachments.ErrAttachmentNotFound) {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusNotFound)}, err
		}
		return nil, nil, err
	}
	if err := h.ensureAttachmentAccess(c, attachment); err != nil {
		return nil, []resp.ResOpt{resp.WithCode(http.StatusUnauthorized)}, err
	}
	return attachment, nil, nil
}

func (h *AttachmentHandler) ensureAttachmentAccess(c *gin.Context, attachment *attachments.Attachment) error {
	if !h.authRequired || attachment == nil {
		return nil
	}
	user := currentAuthUser(c)
	if user != nil && user.Username == attachment.CreatedBy {
		return nil
	}
	return errAttachmentAccessDenied
}

func (h *AttachmentHandler) resolvedDraftTTL() time.Duration {
	if h == nil || h.draftTTL <= 0 {
		return defaultAttachmentDraftTTL
	}
	return h.draftTTL
}

func normalizeAttachmentMimeType(headerValue string, data []byte) string {
	if mimeType := strings.TrimSpace(headerValue); mimeType != "" {
		return mimeType
	}
	return strings.TrimSpace(http.DetectContentType(data))
}

func isSupportedAttachmentMimeType(mimeType string) bool {
	normalized := strings.ToLower(strings.TrimSpace(mimeType))
	switch {
	case strings.HasPrefix(normalized, "text/"):
		return true
	case strings.HasPrefix(normalized, "image/"):
		return true
	case normalized == "application/json":
		return true
	default:
		return false
	}
}

func buildAttachmentTextMetadata(fileName string, kind string, data []byte) (string, string) {
	switch kind {
	case attachments.KindImage:
		context := fmt.Sprintf("[image attachment: %s]", firstNonEmpty(strings.TrimSpace(fileName), "image"))
		return context, context
	case attachments.KindText:
		text := string(data)
		if !utf8.Valid(data) {
			text = ""
		}
		preview := truncateAttachmentText(compactWhitespace(text), 160)
		context := truncateAttachmentText(text, 4000)
		if preview == "" {
			preview = firstNonEmpty(strings.TrimSpace(fileName), "text attachment")
		}
		return preview, context
	default:
		preview := firstNonEmpty(strings.TrimSpace(fileName), "attachment")
		return preview, preview
	}
}

func truncateAttachmentText(value string, limit int) string {
	if limit <= 0 || value == "" {
		return value
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "..."
}

func compactWhitespace(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func buildAttachmentResponse(attachment *attachments.Attachment) AttachmentResponse {
	if attachment == nil {
		return AttachmentResponse{}
	}
	return AttachmentResponse{
		ID:             attachment.ID,
		ConversationID: attachment.ConversationID,
		FileName:       attachment.FileName,
		MimeType:       attachment.MimeType,
		SizeBytes:      attachment.SizeBytes,
		Kind:           attachment.Kind,
		Status:         string(attachment.Status),
		PreviewText:    attachment.PreviewText,
		Width:          attachment.Width,
		Height:         attachment.Height,
		ExpiresAt:      attachment.ExpiresAt,
		CreatedAt:      attachment.CreatedAt,
		UpdatedAt:      attachment.UpdatedAt,
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
