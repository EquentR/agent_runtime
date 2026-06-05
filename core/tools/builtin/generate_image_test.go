package builtin

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/EquentR/agent_runtime/core/attachments"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestOpenAIImageGenProviderEditUsesConfiguredModelAndSingleImageField(t *testing.T) {
	stream := true
	partialImages := 1

	request, result := exerciseOpenAIImageEdit(t, ImageGenProviderConfig{
		Model:               "gpt-image-2",
		Stream:              &stream,
		PartialImages:       &partialImages,
		DefaultSize:         "1024x1024",
		DefaultQuality:      "auto",
		DefaultOutputFormat: "png",
	})

	if request.Fields["model"] != "gpt-image-2" {
		t.Fatalf("model field = %q, want gpt-image-2", request.Fields["model"])
	}
	if request.Fields["prompt"] != "Make it watercolor" {
		t.Fatalf("prompt field = %q, want Make it watercolor", request.Fields["prompt"])
	}
	if request.Fields["stream"] != "true" {
		t.Fatalf("stream field = %q, want true", request.Fields["stream"])
	}
	if request.Fields["partial_images"] != "1" {
		t.Fatalf("partial_images field = %q, want 1", request.Fields["partial_images"])
	}
	if request.Fields["size"] != "1024x1024" {
		t.Fatalf("size field = %q, want 1024x1024", request.Fields["size"])
	}
	if request.Fields["quality"] != "auto" {
		t.Fatalf("quality field = %q, want auto", request.Fields["quality"])
	}
	if request.Fields["output_format"] != "png" {
		t.Fatalf("output_format field = %q, want png", request.Fields["output_format"])
	}
	if len(request.Files) != 1 {
		t.Fatalf("len(files) = %d, want 1", len(request.Files))
	}
	if request.Files[0].FieldName != "image" {
		t.Fatalf("file field = %q, want image", request.Files[0].FieldName)
	}
	if request.Files[0].FileName != "source.png" {
		t.Fatalf("file name = %q, want source.png", request.Files[0].FileName)
	}
	if request.Files[0].MimeType != "image/png" {
		t.Fatalf("file content type = %q, want image/png", request.Files[0].MimeType)
	}
	if request.Files[0].Data != "source-bytes" {
		t.Fatalf("file data = %q, want source-bytes", request.Files[0].Data)
	}
	if result.Model != "gpt-image-2" {
		t.Fatalf("result.Model = %q, want gpt-image-2", result.Model)
	}
}

func TestOpenAIImageGenProviderEditUsesExplicitEditModelOverride(t *testing.T) {
	request, result := exerciseOpenAIImageEdit(t, ImageGenProviderConfig{
		Model:     "gpt-image-2",
		EditModel: "gpt-image-edit",
	})

	if request.Fields["model"] != "gpt-image-edit" {
		t.Fatalf("model field = %q, want gpt-image-edit", request.Fields["model"])
	}
	if result.Model != "gpt-image-edit" {
		t.Fatalf("result.Model = %q, want gpt-image-edit", result.Model)
	}
}

func TestLoadImageAttachmentFallsBackToDetectedMimeTypeForLegacyImageMetadata(t *testing.T) {
	ctx := context.Background()
	storage, err := attachments.NewFilesystemStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFilesystemStore() error = %v", err)
	}
	store := attachments.NewStore(mustOpenImageAttachmentTestDB(t), storage)
	if err := store.AutoMigrate(); err != nil {
		t.Fatalf("store.AutoMigrate() error = %v", err)
	}

	imageData, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR4nGNgYAAAAAMAASsJTYQAAAAASUVORK5CYII=")
	if err != nil {
		t.Fatalf("DecodeString() error = %v", err)
	}
	storedObject, err := storage.PutDraft(ctx, attachments.PutDraftInput{
		FileName: "legacy-image.bin",
		MimeType: "application/octet-stream",
		Data:     imageData,
	})
	if err != nil {
		t.Fatalf("PutDraft() error = %v", err)
	}
	draft, err := store.CreateDraft(ctx, attachments.CreateDraftInput{
		ID:             "att_legacy_image",
		ConversationID: "conv_1",
		CreatedBy:      "tester",
		StorageBackend: storedObject.StorageBackend,
		StorageKey:     storedObject.StorageKey,
		FileName:       storedObject.FileName,
		MimeType:       storedObject.MimeType,
		SizeBytes:      storedObject.SizeBytes,
		Kind:           attachments.KindImage,
	})
	if err != nil {
		t.Fatalf("CreateDraft() error = %v", err)
	}
	sent, err := store.PromoteDraftToSent(ctx, draft.ID, attachments.PromoteInput{ConversationID: "conv_1"})
	if err != nil {
		t.Fatalf("PromoteDraftToSent() error = %v", err)
	}

	env := runtimeEnv{attachmentStore: store, attachmentStorage: storage}
	loaded, err := env.loadImageAttachment(ctx, "tester", sent.ID)
	if err != nil {
		t.Fatalf("loadImageAttachment() error = %v", err)
	}
	if loaded.ID != sent.ID {
		t.Fatalf("loaded.ID = %q, want %q", loaded.ID, sent.ID)
	}
	if loaded.MimeType != "image/png" {
		t.Fatalf("loaded.MimeType = %q, want image/png", loaded.MimeType)
	}
	if string(loaded.Data) != string(imageData) {
		t.Fatalf("loaded.Data = %x, want %x", loaded.Data, imageData)
	}
}

func TestLoadImageAttachmentRejectsSVGSource(t *testing.T) {
	ctx := context.Background()
	storage, err := attachments.NewFilesystemStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFilesystemStore() error = %v", err)
	}
	store := attachments.NewStore(mustOpenImageAttachmentTestDB(t), storage)
	if err := store.AutoMigrate(); err != nil {
		t.Fatalf("store.AutoMigrate() error = %v", err)
	}

	storedObject, err := storage.PutDraft(ctx, attachments.PutDraftInput{
		FileName: "diagram.svg",
		MimeType: "image/svg+xml",
		Data:     []byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`),
	})
	if err != nil {
		t.Fatalf("PutDraft() error = %v", err)
	}
	draft, err := store.CreateDraft(ctx, attachments.CreateDraftInput{
		ID:             "att_svg_source",
		ConversationID: "conv_1",
		CreatedBy:      "tester",
		StorageBackend: storedObject.StorageBackend,
		StorageKey:     storedObject.StorageKey,
		FileName:       storedObject.FileName,
		MimeType:       storedObject.MimeType,
		SizeBytes:      storedObject.SizeBytes,
		Kind:           attachments.KindText,
	})
	if err != nil {
		t.Fatalf("CreateDraft() error = %v", err)
	}
	sent, err := store.PromoteDraftToSent(ctx, draft.ID, attachments.PromoteInput{ConversationID: "conv_1"})
	if err != nil {
		t.Fatalf("PromoteDraftToSent() error = %v", err)
	}

	env := runtimeEnv{attachmentStore: store, attachmentStorage: storage}
	_, err = env.loadImageAttachment(ctx, "tester", sent.ID)
	if err == nil || !strings.Contains(err.Error(), "unsupported mime type") {
		t.Fatalf("loadImageAttachment() error = %v, want unsupported mime type", err)
	}
}

type capturedMultipartFile struct {
	FieldName string
	FileName  string
	MimeType  string
	Data      string
}

type capturedImageEditRequest struct {
	Fields map[string]string
	Files  []capturedMultipartFile
}

func exerciseOpenAIImageEdit(t *testing.T, config ImageGenProviderConfig) (capturedImageEditRequest, imageGenResponse) {
	t.Helper()

	var request capturedImageEditRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/images/edits" {
			t.Fatalf("path = %q, want /images/edits", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("Authorization = %q, want Bearer test-key", got)
		}

		mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil {
			t.Fatalf("ParseMediaType() error = %v", err)
		}
		if !strings.HasPrefix(mediaType, "multipart/") {
			t.Fatalf("content type = %q, want multipart", mediaType)
		}

		reader := multipart.NewReader(r.Body, params["boundary"])
		request.Fields = map[string]string{}
		for {
			part, err := reader.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatalf("NextPart() error = %v", err)
			}

			data, err := io.ReadAll(part)
			if err != nil {
				t.Fatalf("ReadAll(part) error = %v", err)
			}

			if part.FileName() != "" {
				request.Files = append(request.Files, capturedMultipartFile{
					FieldName: part.FormName(),
					FileName:  part.FileName(),
					MimeType:  part.Header.Get("Content-Type"),
					Data:      string(data),
				})
				continue
			}
			request.Fields[part.FormName()] = string(data)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"created": 1,
			"data": []map[string]any{
				{"b64_json": "aGVsbG8="},
			},
		})
	}))
	defer server.Close()

	config.BaseURL = server.URL
	config.APIKey = "test-key"
	provider := openaiImageGenProvider{
		client: server.Client(),
		config: config,
	}

	result, err := provider.Edit(context.Background(), imageEditParams{
		Prompt: "Make it watercolor",
		SourceImages: []imageInputAttachment{{
			ID:       "att_source",
			FileName: "source.png",
			MimeType: "image/png",
			Data:     []byte("source-bytes"),
		}},
		N: 1,
	})
	if err != nil {
		t.Fatalf("Edit() error = %v", err)
	}

	return request, result
}

func mustOpenImageAttachmentTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("gorm.Open() error = %v", err)
	}
	return db
}
