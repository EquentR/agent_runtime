package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"testing"
	"time"

	"github.com/EquentR/agent_runtime/app/logics"
	"github.com/EquentR/agent_runtime/core/attachments"
	"github.com/EquentR/agent_runtime/pkg/rest"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

type attachmentHandlerTestDeps struct {
	authLogic       *logics.AuthLogic
	attachmentStore *attachments.Store
	filesystemStore *attachments.FilesystemStore
}

type attachmentResponse struct {
	ID             string  `json:"id"`
	ConversationID string  `json:"conversation_id"`
	FileName       string  `json:"file_name"`
	MimeType       string  `json:"mime_type"`
	SizeBytes      int64   `json:"size_bytes"`
	Kind           string  `json:"kind"`
	Status         string  `json:"status"`
	PreviewText    string  `json:"preview_text"`
	Width          *int    `json:"width"`
	Height         *int    `json:"height"`
	ExpiresAt      *string `json:"expires_at"`
}

type attachmentDeleteResponse struct {
	Deleted bool `json:"deleted"`
}

func TestAttachmentHandlerUploadCreatesDraftAttachment(t *testing.T) {
	deps, server := newAttachmentHandlerTestServer(t)
	ownerCookie := registerAndLoginAuthUser(t, server.URL, "alice")

	response := uploadAttachment(t, server.URL, ownerCookie, uploadAttachmentRequest{
		FileName:    "notes.txt",
		ContentType: "text/plain",
		Data:        []byte("hello attachment"),
	})
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("upload status = %d, want %d", response.StatusCode, http.StatusOK)
	}
	uploaded := decodeAttachmentResponse(t, response.Body)
	if uploaded.ID == "" {
		t.Fatal("uploaded id = empty, want non-empty")
	}
	if uploaded.FileName != "notes.txt" {
		t.Fatalf("uploaded file_name = %q, want %q", uploaded.FileName, "notes.txt")
	}
	if uploaded.MimeType != "text/plain" {
		t.Fatalf("uploaded mime_type = %q, want %q", uploaded.MimeType, "text/plain")
	}
	if uploaded.Status != string(attachments.StatusDraft) {
		t.Fatalf("uploaded status = %q, want %q", uploaded.Status, attachments.StatusDraft)
	}
	if uploaded.Kind != attachments.KindText {
		t.Fatalf("uploaded kind = %q, want %q", uploaded.Kind, attachments.KindText)
	}
	if uploaded.ExpiresAt == nil || *uploaded.ExpiresAt == "" {
		t.Fatal("uploaded expires_at = nil/empty, want ttl timestamp")
	}

	stored, err := deps.attachmentStore.GetAttachment(context.Background(), uploaded.ID)
	if err != nil {
		t.Fatalf("GetAttachment() error = %v", err)
	}
	if stored.CreatedBy != "alice" {
		t.Fatalf("stored created_by = %q, want %q", stored.CreatedBy, "alice")
	}
}

func TestAttachmentHandlerRejectsUnsupportedMimeType(t *testing.T) {
	_, server := newAttachmentHandlerTestServer(t)
	ownerCookie := registerAndLoginAuthUser(t, server.URL, "alice")

	response := uploadAttachment(t, server.URL, ownerCookie, uploadAttachmentRequest{
		FileName:    "archive.zip",
		ContentType: "application/zip",
		Data:        []byte("PK\x03\x04"),
	})
	defer response.Body.Close()

	envelope := decodeEnvelope(t, response.Body)
	if envelope.OK {
		t.Fatal("unsupported mime upload unexpectedly succeeded")
	}
	if envelope.Code != http.StatusBadRequest {
		t.Fatalf("envelope.Code = %d, want %d", envelope.Code, http.StatusBadRequest)
	}
	if envelope.Message == "" {
		t.Fatal("unsupported mime message = empty, want clear error")
	}
}

func TestAttachmentHandlerRejectsEmptyUpload(t *testing.T) {
	_, server := newAttachmentHandlerTestServer(t)
	ownerCookie := registerAndLoginAuthUser(t, server.URL, "alice")

	response := uploadAttachment(t, server.URL, ownerCookie, uploadAttachmentRequest{
		FileName:    "empty.txt",
		ContentType: "text/plain",
		Data:        nil,
	})
	defer response.Body.Close()

	envelope := decodeEnvelope(t, response.Body)
	if envelope.OK {
		t.Fatal("empty upload unexpectedly succeeded")
	}
	if envelope.Code != http.StatusBadRequest {
		t.Fatalf("envelope.Code = %d, want %d", envelope.Code, http.StatusBadRequest)
	}
	if envelope.Message == "" {
		t.Fatal("empty upload message = empty, want clear error")
	}
}

func TestAttachmentHandlerRejectsOversizedUpload(t *testing.T) {
	_, server := newAttachmentHandlerTestServer(t)
	ownerCookie := registerAndLoginAuthUser(t, server.URL, "alice")

	response := uploadAttachment(t, server.URL, ownerCookie, uploadAttachmentRequest{
		FileName:    "large.txt",
		ContentType: "text/plain",
		Data:        bytes.Repeat([]byte("a"), int(maxAttachmentUploadBytes)+1),
	})
	defer response.Body.Close()

	envelope := decodeEnvelope(t, response.Body)
	if envelope.OK {
		t.Fatal("oversized upload unexpectedly succeeded")
	}
	if envelope.Code != http.StatusBadRequest {
		t.Fatalf("envelope.Code = %d, want %d", envelope.Code, http.StatusBadRequest)
	}
	if envelope.Message == "" {
		t.Fatal("oversized upload message = empty, want clear error")
	}
}

func TestAttachmentHandlerGetContentRequiresOwnership(t *testing.T) {
	deps, server := newAttachmentHandlerTestServer(t)
	ownerCookie := registerAndLoginAuthUser(t, server.URL, "alice")
	otherCookie := registerAndLoginAuthUser(t, server.URL, "bob")

	uploadResponse := uploadAttachment(t, server.URL, ownerCookie, uploadAttachmentRequest{
		FileName:    "notes.txt",
		ContentType: "text/plain",
		Data:        []byte("hello attachment"),
	})
	defer uploadResponse.Body.Close()
	uploaded := decodeAttachmentResponse(t, uploadResponse.Body)

	otherResponse := doAttachmentRequest(t, http.MethodGet, server.URL+"/api/v1/attachments/"+uploaded.ID+"/content", nil, "", otherCookie)
	defer otherResponse.Body.Close()
	otherEnvelope := decodeEnvelope(t, otherResponse.Body)
	if otherEnvelope.OK {
		t.Fatal("other user content unexpectedly succeeded")
	}
	if otherEnvelope.Code != http.StatusUnauthorized {
		t.Fatalf("other envelope.Code = %d, want %d", otherEnvelope.Code, http.StatusUnauthorized)
	}

	ownerResponse := doAttachmentRequest(t, http.MethodGet, server.URL+"/api/v1/attachments/"+uploaded.ID+"/content", nil, "", ownerCookie)
	defer ownerResponse.Body.Close()
	if ownerResponse.StatusCode != http.StatusOK {
		t.Fatalf("owner status = %d, want %d", ownerResponse.StatusCode, http.StatusOK)
	}
	if contentType := ownerResponse.Header.Get("Content-Type"); contentType != "text/plain" {
		t.Fatalf("content type = %q, want %q", contentType, "text/plain")
	}
	body, err := io.ReadAll(ownerResponse.Body)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if string(body) != "hello attachment" {
		t.Fatalf("body = %q, want %q", string(body), "hello attachment")
	}

	stored, err := deps.attachmentStore.GetAttachment(context.Background(), uploaded.ID)
	if err != nil {
		t.Fatalf("GetAttachment() error = %v", err)
	}
	if stored.CreatedBy != "alice" {
		t.Fatalf("stored created_by = %q, want %q", stored.CreatedBy, "alice")
	}
}

func TestAttachmentHandlerDeleteDraftAttachment(t *testing.T) {
	deps, server := newAttachmentHandlerTestServer(t)
	ownerCookie := registerAndLoginAuthUser(t, server.URL, "alice")

	uploadResponse := uploadAttachment(t, server.URL, ownerCookie, uploadAttachmentRequest{
		FileName:    "notes.txt",
		ContentType: "text/plain",
		Data:        []byte("hello attachment"),
	})
	defer uploadResponse.Body.Close()
	uploaded := decodeAttachmentResponse(t, uploadResponse.Body)

	stored, err := deps.attachmentStore.GetAttachment(context.Background(), uploaded.ID)
	if err != nil {
		t.Fatalf("GetAttachment() before delete error = %v", err)
	}

	deleteResponse := doAttachmentRequest(t, http.MethodDelete, server.URL+"/api/v1/attachments/"+uploaded.ID, nil, "", ownerCookie)
	defer deleteResponse.Body.Close()
	if deleteResponse.StatusCode != http.StatusOK {
		t.Fatalf("delete status = %d, want %d", deleteResponse.StatusCode, http.StatusOK)
	}
	deleted := decodeAttachmentDeleteResponse(t, deleteResponse.Body)
	if !deleted.Deleted {
		t.Fatal("deleted.Deleted = false, want true")
	}

	if _, err := deps.attachmentStore.GetAttachment(context.Background(), uploaded.ID); !errors.Is(err, attachments.ErrAttachmentNotFound) {
		t.Fatalf("GetAttachment() after delete error = %v, want ErrAttachmentNotFound", err)
	}
	if _, err := deps.filesystemStore.Stat(context.Background(), stored.StorageKey); !errors.Is(err, attachments.ErrObjectNotFound) {
		t.Fatalf("filesystem Stat() error = %v, want ErrObjectNotFound", err)
	}
}

func TestAttachmentHandlerDeleteDraftAttachmentRejectsNonOwner(t *testing.T) {
	deps, server := newAttachmentHandlerTestServer(t)
	ownerCookie := registerAndLoginAuthUser(t, server.URL, "alice")
	otherCookie := registerAndLoginAuthUser(t, server.URL, "bob")

	uploadResponse := uploadAttachment(t, server.URL, ownerCookie, uploadAttachmentRequest{
		FileName:    "notes.txt",
		ContentType: "text/plain",
		Data:        []byte("hello attachment"),
	})
	defer uploadResponse.Body.Close()
	uploaded := decodeAttachmentResponse(t, uploadResponse.Body)

	deleteResponse := doAttachmentRequest(t, http.MethodDelete, server.URL+"/api/v1/attachments/"+uploaded.ID, nil, "", otherCookie)
	defer deleteResponse.Body.Close()
	envelope := decodeEnvelope(t, deleteResponse.Body)
	if envelope.OK {
		t.Fatal("non-owner delete unexpectedly succeeded")
	}
	if envelope.Code != http.StatusUnauthorized {
		t.Fatalf("envelope.Code = %d, want %d", envelope.Code, http.StatusUnauthorized)
	}

	if _, err := deps.attachmentStore.GetAttachment(context.Background(), uploaded.ID); err != nil {
		t.Fatalf("GetAttachment() after rejected delete error = %v, want nil", err)
	}
}

func newAttachmentHandlerTestServer(t *testing.T) (*attachmentHandlerTestDeps, *httptest.Server) {
	t.Helper()

	dsn := "file:" + t.Name() + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("gorm.Open() error = %v", err)
	}

	authLogic := newAuthLogicForTest(t, db)
	authMiddleware := NewAuthMiddleware(authLogic)
	filesystemStore, err := attachments.NewFilesystemStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFilesystemStore() error = %v", err)
	}
	attachmentStore := attachments.NewStore(db, filesystemStore)
	if err := attachmentStore.AutoMigrate(); err != nil {
		t.Fatalf("attachmentStore.AutoMigrate() error = %v", err)
	}

	engine := rest.Init()
	group := engine.Group("/api/v1")
	NewAuthHandler(authLogic).Register(group)
	NewAttachmentHandler(attachmentStore, filesystemStore, 24*time.Hour, authMiddleware.RequireSession()).Register(group)

	server := httptest.NewServer(engine)
	t.Cleanup(server.Close)
	return &attachmentHandlerTestDeps{
		authLogic:       authLogic,
		attachmentStore: attachmentStore,
		filesystemStore: filesystemStore,
	}, server
}

type uploadAttachmentRequest struct {
	FileName    string
	ContentType string
	Data        []byte
}

func uploadAttachment(t *testing.T, baseURL string, cookie *http.Cookie, request uploadAttachmentRequest) *http.Response {
	t.Helper()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	partHeaders := textproto.MIMEHeader{}
	partHeaders.Set("Content-Disposition", `form-data; name="file"; filename="`+request.FileName+`"`)
	if request.ContentType != "" {
		partHeaders.Set("Content-Type", request.ContentType)
	}
	part, err := writer.CreatePart(partHeaders)
	if err != nil {
		t.Fatalf("CreatePart() error = %v", err)
	}
	if _, err := part.Write(request.Data); err != nil {
		t.Fatalf("part.Write() error = %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("writer.Close() error = %v", err)
	}

	return doAttachmentRequest(t, http.MethodPost, baseURL+"/api/v1/attachments", body, writer.FormDataContentType(), cookie)
}

func doAttachmentRequest(t *testing.T, method, url string, body io.Reader, contentType string, cookie *http.Cookie) *http.Response {
	t.Helper()

	request, err := http.NewRequest(method, url, body)
	if err != nil {
		t.Fatalf("http.NewRequest() error = %v", err)
	}
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	if cookie != nil {
		request.AddCookie(cookie)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("http.DefaultClient.Do() error = %v", err)
	}
	return response
}

func decodeAttachmentResponse(t *testing.T, body io.Reader) attachmentResponse {
	t.Helper()

	envelope := decodeEnvelope(t, body)
	if !envelope.OK {
		t.Fatalf("response ok = false, raw data = %s", string(envelope.Data))
	}
	var attachment attachmentResponse
	if err := json.Unmarshal(envelope.Data, &attachment); err != nil {
		t.Fatalf("json.Unmarshal(attachment) error = %v", err)
	}
	return attachment
}

func decodeAttachmentDeleteResponse(t *testing.T, body io.Reader) attachmentDeleteResponse {
	t.Helper()

	envelope := decodeEnvelope(t, body)
	if !envelope.OK {
		t.Fatalf("response ok = false, raw data = %s", string(envelope.Data))
	}
	var deleted attachmentDeleteResponse
	if err := json.Unmarshal(envelope.Data, &deleted); err != nil {
		t.Fatalf("json.Unmarshal(deleted) error = %v", err)
	}
	return deleted
}
