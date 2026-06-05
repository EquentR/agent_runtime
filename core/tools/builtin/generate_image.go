package builtin

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/EquentR/agent_runtime/core/attachments"
	corelog "github.com/EquentR/agent_runtime/core/log"
	coretasks "github.com/EquentR/agent_runtime/core/tasks"
	coretools "github.com/EquentR/agent_runtime/core/tools"
	"github.com/EquentR/agent_runtime/core/types"
	"github.com/google/uuid"
)

const (
	defaultImageGenModel     = "gpt-image-2"
	defaultImageSentLifetime = 30 * 24 * time.Hour
	maxImageDownloadSize     = 50 * 1024 * 1024 // 50MB
	maxImageAPIErrorBodySize = 4096
	maxImageEditSources      = 16
)

type imageGenProvider interface {
	Generate(ctx context.Context, params imageGenParams) (imageGenResponse, error)
	Edit(ctx context.Context, params imageEditParams) (imageGenResponse, error)
}

type imageGenParams struct {
	Prompt       string
	Size         string
	Quality      string
	OutputFormat string
	N            int
	EmitPartial  func(index int, partialImageIndex int, mimeType string, b64JSON string) error
}

type imageEditParams struct {
	Prompt        string
	SourceImages  []imageInputAttachment
	MaskImage     *imageInputAttachment
	Size          string
	Quality       string
	Background    string
	OutputFormat  string
	InputFidelity string
	N             int
	EmitPartial   func(index int, partialImageIndex int, mimeType string, b64JSON string) error
}

type imageInputAttachment struct {
	ID       string
	FileName string
	MimeType string
	Data     []byte
}

type imageGenResponse struct {
	Model      string          `json:"model"`
	Size       string          `json:"-"`
	MimeType   string          `json:"-"`
	OutputType string          `json:"-"`
	Images     []imageGenImage `json:"data"`
}

type imageGenImage struct {
	B64JSON       string `json:"b64_json"`
	URL           string `json:"url"`
	RevisedPrompt string `json:"revised_prompt"`
}

type imageGenResultImage struct {
	AttachmentID  string `json:"attachment_id"`
	MetadataURL   string `json:"metadata_url"`
	ContentURL    string `json:"content_url"`
	FileName      string `json:"file_name"`
	MimeType      string `json:"mime_type"`
	SizeBytes     int64  `json:"size_bytes"`
	Width         *int   `json:"width"`
	Height        *int   `json:"height"`
	RevisedPrompt string `json:"revised_prompt"`
}

func newGenerateImageTool(env runtimeEnv) coretools.Tool {
	return coretools.Tool{
		Name:        "generate_image",
		Description: "Generate images using a configured image generation provider (OpenAI-compatible API). Describe the desired visual style in the prompt. Returns attachment references for generated images.",
		Source:      "builtin",
		Parameters: objectSchema([]string{"prompt"}, map[string]types.SchemaProperty{
			"prompt":  {Type: "string", Description: "Image generation prompt describing what to create"},
			"size":    {Type: "string", Description: "Image size, e.g. 1024x1024, 1536x1024, 2048x1152, 3840x2160"},
			"quality": {Type: "string", Description: "Image quality: low, medium, high, or auto"},
			"n":       {Type: "integer", Description: "Number of images to generate, default 1"},
		}),
		Handler: func(ctx context.Context, arguments map[string]interface{}) (string, error) {
			prompt, err := requiredStringArg(arguments, "prompt")
			if err != nil {
				return "", err
			}
			size, _, err := optionalStringArg(arguments, "size")
			if err != nil {
				return "", err
			}
			quality, _, err := optionalStringArg(arguments, "quality")
			if err != nil {
				return "", err
			}
			n, err := intArg(arguments, "n", 1)
			if err != nil {
				return "", err
			}
			if n < 1 {
				n = 1
			}
			if n > 4 {
				n = 4
			}

			startedAt := time.Now()
			logToolStart(ctx, "generate_image", corelog.Int("prompt_length", len(prompt)), corelog.String("size", size), corelog.Int("n", n))

			conversationID, createdBy, err := imageRuntimeOwner(ctx)
			if err != nil {
				logToolFailure(ctx, "generate_image", err, corelog.Int("prompt_length", len(prompt)))
				return "", err
			}

			providerName, provider, err := env.resolveImageGenProvider("")
			if err != nil {
				logToolFailure(ctx, "generate_image", err, corelog.Int("prompt_length", len(prompt)))
				return "", err
			}

			response, err := provider.Generate(ctx, imageGenParams{
				Prompt:       prompt,
				Size:         size,
				Quality:      quality,
				OutputFormat: "",
				N:            n,
				EmitPartial: func(index int, partialImageIndex int, mimeType string, b64JSON string) error {
					return emitImagePartial(ctx, "generate_image", "generate", index, partialImageIndex, mimeType, b64JSON)
				},
			})
			if err != nil {
				logToolFailure(ctx, "generate_image", err, corelog.String("provider", providerName), corelog.Int("prompt_length", len(prompt)), corelog.Duration("duration", time.Since(startedAt)))
				return "", err
			}

			if len(response.Images) == 0 {
				err := fmt.Errorf("API returned no images")
				logToolFailure(ctx, "generate_image", err, corelog.String("provider", providerName))
				return "", err
			}

			images := make([]imageGenResultImage, 0, len(response.Images))
			failedImages := make([]map[string]interface{}, 0)
			for _, img := range response.Images {
				if strings.TrimSpace(img.B64JSON) == "" {
					failedImages = append(failedImages, map[string]interface{}{
						"error":          "empty image b64_json in API response",
						"revised_prompt": img.RevisedPrompt,
					})
					continue
				}

				imageBytes, decodeErr := base64.StdEncoding.DecodeString(img.B64JSON)
				if decodeErr != nil {
					failedImages = append(failedImages, map[string]interface{}{
						"error":          fmt.Sprintf("decode b64_json: %v", decodeErr),
						"revised_prompt": img.RevisedPrompt,
					})
					continue
				}
				attachment, storeErr := env.storeGeneratedImage(ctx, imageBytes, response.MimeType, response.Size, conversationID, createdBy, img.RevisedPrompt)
				if storeErr != nil {
					failedImages = append(failedImages, map[string]interface{}{
						"error":          storeErr.Error(),
						"revised_prompt": img.RevisedPrompt,
					})
					continue
				}
				entry := imageGenResultImage{
					AttachmentID:  attachment.ID,
					MetadataURL:   "/api/v1/attachments/" + attachment.ID,
					ContentURL:    "/api/v1/attachments/" + attachment.ID + "/content",
					FileName:      attachment.FileName,
					MimeType:      attachment.MimeType,
					SizeBytes:     attachment.SizeBytes,
					Width:         attachment.Width,
					Height:        attachment.Height,
					RevisedPrompt: img.RevisedPrompt,
				}
				images = append(images, entry)
			}

			logToolFinish(ctx, "generate_image", corelog.String("provider", providerName), corelog.Int("image_count", len(images)), corelog.Duration("duration", time.Since(startedAt)))
			return jsonResult(map[string]interface{}{
				"tool":                  "generate_image",
				"operation":             "generate",
				"provider":              providerName,
				"model":                 response.Model,
				"count":                 len(images),
				"images":                images,
				"source_attachment_ids": []string{},
				"failed_images":         failedImages,
			})
		},
	}
}

func newEditImageTool(env runtimeEnv) coretools.Tool {
	return coretools.Tool{
		Name:        "edit_image",
		Description: "Edit images using source attachment IDs with a configured OpenAI-compatible image provider. Returns attachment references for edited images.",
		Source:      "builtin",
		Parameters: objectSchema([]string{"prompt", "source_attachment_ids"}, map[string]types.SchemaProperty{
			"prompt":                {Type: "string", Description: "Image edit prompt describing how to transform the source images"},
			"source_attachment_ids": stringArrayProperty("Sent image attachment IDs to use as edit sources"),
			"mask_attachment_id":    {Type: "string", Description: "Optional image attachment ID to use as an edit mask"},
			"size":                  {Type: "string", Description: "Image size, e.g. 1024x1024, 1536x1024, 1024x1536"},
			"quality":               {Type: "string", Description: "Image quality"},
			"background":            {Type: "string", Description: "Background mode"},
			"output_format":         {Type: "string", Description: "Output format, e.g. png, jpeg, webp"},
			"input_fidelity":        {Type: "string", Description: "Input fidelity for image edits"},
			"n":                     {Type: "integer", Description: "Number of images to generate, default 1"},
		}),
		Handler: func(ctx context.Context, arguments map[string]interface{}) (string, error) {
			prompt, err := requiredStringArg(arguments, "prompt")
			if err != nil {
				return "", err
			}
			sourceIDs, err := sourceAttachmentIDsArg(arguments, "source_attachment_ids")
			if err != nil {
				return "", err
			}
			size, _, err := optionalStringArg(arguments, "size")
			if err != nil {
				return "", err
			}
			quality, _, err := optionalStringArg(arguments, "quality")
			if err != nil {
				return "", err
			}
			background, _, err := optionalStringArg(arguments, "background")
			if err != nil {
				return "", err
			}
			outputFormat, _, err := optionalStringArg(arguments, "output_format")
			if err != nil {
				return "", err
			}
			inputFidelity, _, err := optionalStringArg(arguments, "input_fidelity")
			if err != nil {
				return "", err
			}
			maskID, hasMask, err := optionalStringArg(arguments, "mask_attachment_id")
			if err != nil {
				return "", err
			}
			n, err := intArg(arguments, "n", 1)
			if err != nil {
				return "", err
			}
			if n < 1 {
				n = 1
			}
			if n > 4 {
				n = 4
			}

			startedAt := time.Now()
			logToolStart(ctx, "edit_image", corelog.Int("prompt_length", len(prompt)), corelog.Int("source_count", len(sourceIDs)), corelog.Int("n", n))

			conversationID, createdBy, err := imageRuntimeOwner(ctx)
			if err != nil {
				logToolFailure(ctx, "edit_image", err, corelog.Int("prompt_length", len(prompt)))
				return "", err
			}

			sourceImages := make([]imageInputAttachment, 0, len(sourceIDs))
			for _, id := range sourceIDs {
				image, err := env.loadImageAttachment(ctx, createdBy, id)
				if err != nil {
					logToolFailure(ctx, "edit_image", err, corelog.String("attachment_id", id))
					return "", err
				}
				sourceImages = append(sourceImages, image)
			}

			var maskImage *imageInputAttachment
			if hasMask && maskID != "" {
				image, err := env.loadImageAttachment(ctx, createdBy, maskID)
				if err != nil {
					logToolFailure(ctx, "edit_image", err, corelog.String("attachment_id", maskID))
					return "", err
				}
				maskImage = &image
			}

			providerName, provider, err := env.resolveImageGenProvider("")
			if err != nil {
				logToolFailure(ctx, "edit_image", err, corelog.Int("prompt_length", len(prompt)))
				return "", err
			}

			response, err := provider.Edit(ctx, imageEditParams{
				Prompt:        prompt,
				SourceImages:  sourceImages,
				MaskImage:     maskImage,
				Size:          size,
				Quality:       quality,
				Background:    background,
				OutputFormat:  outputFormat,
				InputFidelity: inputFidelity,
				N:             n,
				EmitPartial: func(index int, partialImageIndex int, mimeType string, b64JSON string) error {
					return emitImagePartial(ctx, "edit_image", "edit", index, partialImageIndex, mimeType, b64JSON)
				},
			})
			if err != nil {
				logToolFailure(ctx, "edit_image", err, corelog.String("provider", providerName), corelog.Int("prompt_length", len(prompt)), corelog.Duration("duration", time.Since(startedAt)))
				return "", err
			}

			if len(response.Images) == 0 {
				err := fmt.Errorf("API returned no images")
				logToolFailure(ctx, "edit_image", err, corelog.String("provider", providerName))
				return "", err
			}

			images := make([]imageGenResultImage, 0, len(response.Images))
			failedImages := make([]map[string]interface{}, 0)
			for _, img := range response.Images {
				if strings.TrimSpace(img.B64JSON) == "" {
					failedImages = append(failedImages, map[string]interface{}{
						"error":          "empty image b64_json in API response",
						"revised_prompt": img.RevisedPrompt,
					})
					continue
				}

				imageBytes, decodeErr := base64.StdEncoding.DecodeString(img.B64JSON)
				if decodeErr != nil {
					failedImages = append(failedImages, map[string]interface{}{
						"error":          fmt.Sprintf("decode b64_json: %v", decodeErr),
						"revised_prompt": img.RevisedPrompt,
					})
					continue
				}
				attachment, storeErr := env.storeGeneratedImage(ctx, imageBytes, response.MimeType, response.Size, conversationID, createdBy, img.RevisedPrompt)
				if storeErr != nil {
					failedImages = append(failedImages, map[string]interface{}{
						"error":          storeErr.Error(),
						"revised_prompt": img.RevisedPrompt,
					})
					continue
				}
				images = append(images, imageGenResultImage{
					AttachmentID:  attachment.ID,
					MetadataURL:   "/api/v1/attachments/" + attachment.ID,
					ContentURL:    "/api/v1/attachments/" + attachment.ID + "/content",
					FileName:      attachment.FileName,
					MimeType:      attachment.MimeType,
					SizeBytes:     attachment.SizeBytes,
					Width:         attachment.Width,
					Height:        attachment.Height,
					RevisedPrompt: img.RevisedPrompt,
				})
			}

			logToolFinish(ctx, "edit_image", corelog.String("provider", providerName), corelog.Int("image_count", len(images)), corelog.Duration("duration", time.Since(startedAt)))
			return jsonResult(map[string]interface{}{
				"tool":                  "edit_image",
				"operation":             "edit",
				"provider":              providerName,
				"model":                 response.Model,
				"count":                 len(images),
				"images":                images,
				"source_attachment_ids": sourceIDs,
				"failed_images":         failedImages,
			})
		},
	}
}

func (e runtimeEnv) resolveImageGenProvider(name string) (string, imageGenProvider, error) {
	resolved := strings.ToLower(strings.TrimSpace(name))
	if resolved == "" {
		resolved = strings.ToLower(strings.TrimSpace(e.imageGen.DefaultProvider))
	}
	if resolved == "" {
		if e.imageGen.Openai != nil && strings.TrimSpace(e.imageGen.Openai.APIKey) != "" {
			resolved = "openai"
		}
	}
	if resolved == "" {
		return "", nil, fmt.Errorf("image generation provider is not configured")
	}

	switch resolved {
	case "openai":
		if e.imageGen.Openai == nil || strings.TrimSpace(e.imageGen.Openai.APIKey) == "" {
			return "", nil, fmt.Errorf("image generation provider %q is not configured", resolved)
		}
		return resolved, openaiImageGenProvider{client: e.httpClient, config: *e.imageGen.Openai}, nil
	default:
		return "", nil, fmt.Errorf("unsupported image generation provider: %s", resolved)
	}
}

type openaiImageGenProvider struct {
	client *http.Client
	config ImageGenProviderConfig
}

func (p openaiImageGenProvider) Generate(ctx context.Context, params imageGenParams) (imageGenResponse, error) {
	baseURL := strings.TrimRight(defaultIfEmpty(p.config.BaseURL, "https://api.openai.com/v1"), "/")
	endpoint := baseURL + "/images/generations"
	model := defaultIfEmpty(strings.TrimSpace(p.config.Model), defaultImageGenModel)
	stream := true
	if p.config.Stream != nil {
		stream = *p.config.Stream
	}
	size := firstNonEmpty(params.Size, p.config.DefaultSize)
	quality := firstNonEmpty(params.Quality, p.config.DefaultQuality)
	outputFormat := firstNonEmpty(params.OutputFormat, p.config.DefaultOutputFormat)

	body := map[string]interface{}{
		"model":  model,
		"prompt": params.Prompt,
		"n":      params.N,
		"stream": stream,
	}
	if stream {
		if partialImages, ok := normalizePartialImages(p.config); ok {
			body["partial_images"] = partialImages
		}
	}
	if size != "" {
		body["size"] = size
	}
	if quality != "" {
		body["quality"] = quality
	}
	if outputFormat != "" {
		body["output_format"] = outputFormat
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return imageGenResponse{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return imageGenResponse{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.config.APIKey)

	resp, err := p.client.Do(req)
	if err != nil {
		return imageGenResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxImageAPIErrorBodySize+1024))
		if err != nil {
			return imageGenResponse{}, err
		}
		return imageGenResponse{}, fmt.Errorf("image generation API returned status %d: %s", resp.StatusCode, boundedBody(respBody))
	}

	if strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream") {
		result, err := parseImageGenSSE(ctx, resp.Body, params)
		if err != nil {
			return imageGenResponse{}, err
		}
		result.Model = model
		result.Size = size
		result.OutputType = outputFormat
		result.MimeType = imageMimeType(outputFormat)
		return result, nil
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return imageGenResponse{}, err
	}
	var result imageGenResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return imageGenResponse{}, fmt.Errorf("parse image generation response: %w", err)
	}
	result.Model = model
	result.Size = size
	result.OutputType = outputFormat
	result.MimeType = imageMimeType(outputFormat)
	return result, nil
}

func (p openaiImageGenProvider) Edit(ctx context.Context, params imageEditParams) (imageGenResponse, error) {
	baseURL := strings.TrimRight(defaultIfEmpty(p.config.BaseURL, "https://api.openai.com/v1"), "/")
	endpoint := baseURL + "/images/edits"
	model := resolveOpenAIImageEditModel(p.config)
	stream := true
	if p.config.Stream != nil {
		stream = *p.config.Stream
	}
	size := firstNonEmpty(params.Size, p.config.DefaultSize)
	quality := firstNonEmpty(params.Quality, p.config.DefaultQuality)
	outputFormat := firstNonEmpty(params.OutputFormat, p.config.DefaultOutputFormat)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	fields := map[string]string{
		"model":  model,
		"prompt": params.Prompt,
		"n":      strconv.Itoa(params.N),
		"stream": strconv.FormatBool(stream),
	}
	if stream {
		if partialImages, ok := normalizePartialImages(p.config); ok {
			fields["partial_images"] = strconv.Itoa(partialImages)
		}
	}
	if size != "" {
		fields["size"] = size
	}
	if quality != "" {
		fields["quality"] = quality
	}
	if params.Background != "" {
		fields["background"] = params.Background
	}
	if outputFormat != "" {
		fields["output_format"] = outputFormat
	}
	if params.InputFidelity != "" {
		fields["input_fidelity"] = params.InputFidelity
	}
	for key, value := range fields {
		if err := writer.WriteField(key, value); err != nil {
			_ = writer.Close()
			return imageGenResponse{}, err
		}
	}
	imageFieldName := "image"
	if len(params.SourceImages) > 1 {
		imageFieldName = "image[]"
	}
	for _, image := range params.SourceImages {
		if err := writeImageMultipartFile(writer, imageFieldName, image); err != nil {
			_ = writer.Close()
			return imageGenResponse{}, err
		}
	}
	if params.MaskImage != nil {
		if err := writeImageMultipartFile(writer, "mask", *params.MaskImage); err != nil {
			_ = writer.Close()
			return imageGenResponse{}, err
		}
	}
	if err := writer.Close(); err != nil {
		return imageGenResponse{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, &body)
	if err != nil {
		return imageGenResponse{}, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+p.config.APIKey)

	resp, err := p.client.Do(req)
	if err != nil {
		return imageGenResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxImageAPIErrorBodySize+1024))
		if err != nil {
			return imageGenResponse{}, err
		}
		return imageGenResponse{}, fmt.Errorf("image edit API returned status %d: %s", resp.StatusCode, boundedBody(respBody))
	}

	genParams := imageGenParams{EmitPartial: params.EmitPartial}
	if strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream") {
		result, err := parseImageGenSSE(ctx, resp.Body, genParams)
		if err != nil {
			return imageGenResponse{}, err
		}
		result.Model = model
		result.Size = size
		result.OutputType = outputFormat
		result.MimeType = imageMimeType(outputFormat)
		return result, nil
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return imageGenResponse{}, err
	}
	var result imageGenResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return imageGenResponse{}, fmt.Errorf("parse image edit response: %w", err)
	}
	result.Model = model
	result.Size = size
	result.OutputType = outputFormat
	result.MimeType = imageMimeType(outputFormat)
	return result, nil
}

func writeImageMultipartFile(writer *multipart.Writer, fieldName string, image imageInputAttachment) error {
	mimeType := strings.TrimSpace(image.MimeType)
	if mimeType == "" && len(image.Data) > 0 {
		mimeType = http.DetectContentType(image.Data)
	}
	mimeType = defaultIfEmpty(mimeType, "application/octet-stream")

	fileName := defaultIfEmpty(image.FileName, image.ID+fileExtensionFromMimeType(mimeType))
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", multipart.FileContentDisposition(fieldName, fileName))
	header.Set("Content-Type", mimeType)
	part, err := writer.CreatePart(header)
	if err != nil {
		return err
	}
	_, err = part.Write(image.Data)
	return err
}

func resolveOpenAIImageEditModel(config ImageGenProviderConfig) string {
	if model := strings.TrimSpace(config.EditModel); model != "" {
		return model
	}
	return defaultIfEmpty(strings.TrimSpace(config.Model), defaultImageGenModel)
}

type imageGenSSEPayload struct {
	Type              string          `json:"type"`
	B64JSON           string          `json:"b64_json"`
	RevisedPrompt     string          `json:"revised_prompt"`
	PartialImageIndex *int            `json:"partial_image_index"`
	Index             *int            `json:"index"`
	Data              []imageGenImage `json:"data"`
}

func parseImageGenSSE(ctx context.Context, reader io.Reader, params imageGenParams) (imageGenResponse, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), maxImageDownloadSize)

	var result imageGenResponse
	var eventName string
	var dataLines []string
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return imageGenResponse{}, err
		}
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			done, err := consumeImageGenSSEData(eventName, strings.Join(dataLines, "\n"), params, &result)
			if err != nil {
				return imageGenResponse{}, err
			}
			eventName = ""
			dataLines = nil
			if done {
				return result, nil
			}
			continue
		}
		if event, ok := strings.CutPrefix(line, "event:"); ok {
			eventName = strings.TrimSpace(event)
			continue
		}
		if data, ok := strings.CutPrefix(line, "data:"); ok {
			dataLines = append(dataLines, strings.TrimSpace(data))
		}
	}
	if err := scanner.Err(); err != nil {
		return imageGenResponse{}, fmt.Errorf("read image generation stream: %w", err)
	}
	if len(dataLines) > 0 {
		if _, err := consumeImageGenSSEData(eventName, strings.Join(dataLines, "\n"), params, &result); err != nil {
			return imageGenResponse{}, err
		}
	}
	return result, nil
}

func consumeImageGenSSEData(eventName string, raw string, params imageGenParams, result *imageGenResponse) (bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false, nil
	}
	if raw == "[DONE]" {
		return true, nil
	}

	var payload imageGenSSEPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return false, fmt.Errorf("parse image generation stream event: %w", err)
	}
	eventType := strings.TrimSpace(payload.Type)
	if eventType == "" {
		eventType = strings.TrimSpace(eventName)
	}
	switch {
	case strings.HasSuffix(eventType, ".partial_image"):
		if params.EmitPartial != nil && strings.TrimSpace(payload.B64JSON) != "" {
			index := 0
			if payload.Index != nil {
				index = *payload.Index
			}
			partialIndex := 0
			if payload.PartialImageIndex != nil {
				partialIndex = *payload.PartialImageIndex
			}
			if err := params.EmitPartial(index, partialIndex, "image/png", payload.B64JSON); err != nil {
				return false, err
			}
		}
	case strings.HasSuffix(eventType, ".completed"):
		if len(payload.Data) > 0 {
			result.Images = append(result.Images, payload.Data...)
			return false, nil
		}
		if strings.TrimSpace(payload.B64JSON) != "" {
			result.Images = append(result.Images, imageGenImage{
				B64JSON:       payload.B64JSON,
				RevisedPrompt: payload.RevisedPrompt,
			})
		}
	default:
		if len(payload.Data) > 0 {
			result.Images = append(result.Images, payload.Data...)
		}
	}
	return false, nil
}

func emitImagePartial(ctx context.Context, tool string, operation string, index int, partialImageIndex int, mimeType string, b64JSON string) error {
	runtime, ok := coretools.RuntimeFromContext(ctx)
	if !ok || runtime == nil || runtime.Emit == nil {
		return nil
	}
	mimeType = defaultIfEmpty(mimeType, "image/png")
	persistentPayload := map[string]any{
		"Kind":              "image_partial",
		"Tool":              tool,
		"Operation":         operation,
		"Index":             index,
		"PartialImageIndex": partialImageIndex,
		"MimeType":          mimeType,
		"HasPreview":        true,
	}
	livePayload := map[string]any{
		"Kind":              "image_partial",
		"Tool":              tool,
		"Operation":         operation,
		"Index":             index,
		"PartialImageIndex": partialImageIndex,
		"MimeType":          mimeType,
		"HasPreview":        true,
		"B64JSON":           b64JSON,
	}
	return runtime.Emit(ctx, coretasks.EventLogMessage, "info", coretasks.NewRuntimeEventPayload(persistentPayload, livePayload))
}

func imageRuntimeOwner(ctx context.Context) (string, string, error) {
	runtime, ok := coretools.RuntimeFromContext(ctx)
	if !ok || runtime == nil {
		return "", "", fmt.Errorf("generate_image requires runtime metadata: conversation_id and created_by")
	}
	conversationID := strings.TrimSpace(runtime.Metadata["conversation_id"])
	createdBy := strings.TrimSpace(runtime.Metadata["created_by"])
	if conversationID == "" || createdBy == "" {
		return "", "", fmt.Errorf("generate_image requires runtime metadata: conversation_id and created_by")
	}
	return conversationID, createdBy, nil
}

func sourceAttachmentIDsArg(arguments map[string]any, key string) ([]string, error) {
	rawIDs, err := stringSliceArg(arguments, key)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{}, len(rawIDs))
	ids := make([]string, 0, len(rawIDs))
	for _, rawID := range rawIDs {
		id := strings.TrimSpace(rawID)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("%s is required", key)
	}
	if len(ids) > maxImageEditSources {
		return nil, fmt.Errorf("%s supports at most %d images", key, maxImageEditSources)
	}
	return ids, nil
}

func (e runtimeEnv) loadImageAttachment(ctx context.Context, createdBy string, attachmentID string) (imageInputAttachment, error) {
	if e.attachmentStore == nil {
		return imageInputAttachment{}, fmt.Errorf("attachment store is not configured")
	}
	if e.attachmentStorage == nil {
		return imageInputAttachment{}, fmt.Errorf("attachment storage is not configured")
	}
	attachment, err := e.attachmentStore.GetAttachment(ctx, attachmentID)
	if err != nil {
		return imageInputAttachment{}, err
	}
	if attachment.CreatedBy != createdBy {
		return imageInputAttachment{}, fmt.Errorf("attachment %s owner mismatch: created_by %q", attachment.ID, attachment.CreatedBy)
	}
	if attachment.Status == attachments.StatusExpired {
		return imageInputAttachment{}, fmt.Errorf("attachment %s is expired", attachment.ID)
	}
	if attachment.Status != attachments.StatusSent {
		return imageInputAttachment{}, fmt.Errorf("attachment %s must be sent before image edit, got status %q", attachment.ID, attachment.Status)
	}
	if attachment.ExpiresAt != nil && !attachment.ExpiresAt.After(time.Now().UTC()) {
		return imageInputAttachment{}, fmt.Errorf("attachment %s is expired", attachment.ID)
	}
	reader, _, err := e.attachmentStorage.Open(ctx, attachment.StorageKey)
	if err != nil {
		return imageInputAttachment{}, err
	}
	defer reader.Close()

	data, err := io.ReadAll(io.LimitReader(reader, maxImageDownloadSize+1))
	if err != nil {
		return imageInputAttachment{}, fmt.Errorf("read attachment %s: %w", attachment.ID, err)
	}
	if len(data) > maxImageDownloadSize {
		return imageInputAttachment{}, fmt.Errorf("attachment %s exceeds max image size", attachment.ID)
	}
	if len(data) == 0 {
		return imageInputAttachment{}, fmt.Errorf("attachment %s is empty", attachment.ID)
	}
	mimeType, ok := resolveImageAttachmentMimeType(attachment.MimeType, "", data)
	if !ok {
		mimeType = strings.TrimSpace(attachment.MimeType)
		if mimeType == "" {
			mimeType = "application/octet-stream"
		}
		return imageInputAttachment{}, fmt.Errorf("attachment %s has unsupported mime type %q", attachment.ID, mimeType)
	}
	return imageInputAttachment{
		ID:       attachment.ID,
		FileName: attachment.FileName,
		MimeType: mimeType,
		Data:     data,
	}, nil
}

func resolveImageAttachmentMimeType(recordMimeType string, metaMimeType string, data []byte) (string, bool) {
	mimeType := firstNonEmpty(strings.TrimSpace(recordMimeType), strings.TrimSpace(metaMimeType))
	if strings.HasPrefix(strings.ToLower(mimeType), "image/") {
		return mimeType, true
	}
	if len(data) == 0 {
		return "", false
	}
	detected := strings.TrimSpace(http.DetectContentType(data))
	if strings.HasPrefix(strings.ToLower(detected), "image/") {
		return detected, true
	}
	return "", false
}

func (e runtimeEnv) storeGeneratedImage(ctx context.Context, data []byte, mimeType string, size string, conversationID string, createdBy string, revisedPrompt string) (*attachments.Attachment, error) {
	if e.attachmentStorage == nil {
		return nil, fmt.Errorf("attachment storage is not configured")
	}
	if e.attachmentStore == nil {
		return nil, fmt.Errorf("attachment store is not configured")
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("generated image is empty")
	}
	mimeType = defaultIfEmpty(strings.TrimSpace(mimeType), "image/png")
	ext := fileExtensionFromMimeType(mimeType)
	id := uuid.NewString()
	storageKey := filepath.ToSlash(filepath.Join("images", id+ext))
	fileName := "generated_" + id + ext
	obj, err := e.attachmentStorage.PutDraft(ctx, attachments.PutDraftInput{
		StorageKey: storageKey,
		FileName:   fileName,
		MimeType:   mimeType,
		Data:       data,
	})
	if err != nil {
		return nil, fmt.Errorf("store image: %w", err)
	}

	var width, height *int
	if w, h := parseDimensions(size); w > 0 {
		width = &w
		if h > 0 {
			height = &h
		}
	}
	draft, err := e.attachmentStore.CreateDraft(ctx, attachments.CreateDraftInput{
		ConversationID: conversationID,
		CreatedBy:      createdBy,
		StorageBackend: obj.StorageBackend,
		StorageKey:     obj.StorageKey,
		FileName:       obj.FileName,
		MimeType:       obj.MimeType,
		SizeBytes:      obj.SizeBytes,
		Kind:           attachments.KindImage,
		ContextText:    revisedPrompt,
		Width:          width,
		Height:         height,
	})
	if err != nil {
		_ = e.attachmentStorage.Delete(ctx, obj.StorageKey)
		return nil, fmt.Errorf("create attachment record: %w", err)
	}

	retention := e.imageGen.SentRetention
	if retention <= 0 {
		retention = defaultImageSentLifetime
	}
	retainUntil := time.Now().UTC().Add(retention)
	attachment, err := e.attachmentStore.PromoteDraftToSent(ctx, draft.ID, attachments.PromoteInput{
		ConversationID: conversationID,
		RetainUntil:    &retainUntil,
	})
	if err != nil {
		_ = e.attachmentStore.DeleteAttachment(ctx, draft.ID)
		_ = e.attachmentStorage.Delete(ctx, obj.StorageKey)
		return nil, fmt.Errorf("promote image attachment: %w", err)
	}
	return attachment, nil
}

func imageMimeType(outputFormat string) string {
	switch strings.ToLower(strings.TrimSpace(outputFormat)) {
	case "jpeg", "jpg":
		return "image/jpeg"
	case "webp":
		return "image/webp"
	}
	return "image/png"
}

func boundedBody(body []byte) string {
	trimmed := strings.TrimSpace(string(body))
	if len(trimmed) <= maxImageAPIErrorBodySize {
		return trimmed
	}
	return trimmed[:maxImageAPIErrorBodySize] + "...(truncated)"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func normalizePartialImages(config ImageGenProviderConfig) (int, bool) {
	if config.PartialImages == nil {
		return 1, true
	}
	partialImages := *config.PartialImages
	if partialImages <= 0 {
		return 0, false
	}
	if partialImages > 3 {
		return 3, true
	}
	return partialImages, true
}

// parseDimensions extracts width and height from a size string like "1024x1024".
func parseDimensions(size string) (int, int) {
	parts := strings.Split(strings.TrimSpace(size), "x")
	if len(parts) != 2 {
		return 0, 0
	}
	w, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return 0, 0
	}
	h, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil {
		return 0, 0
	}
	return w, h
}

// fileExtensionFromMimeType returns a file extension (with leading dot) for a
// given MIME type, defaulting to ".png".
func fileExtensionFromMimeType(mimeType string) string {
	switch strings.TrimSpace(strings.ToLower(mimeType)) {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	case "image/bmp":
		return ".bmp"
	case "image/tiff":
		return ".tiff"
	default:
		return ".png"
	}
}
