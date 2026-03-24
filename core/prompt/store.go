package prompt

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"
)

var ErrPromptDocumentNotFound = errors.New("prompt document not found")
var ErrPromptBindingNotFound = errors.New("prompt binding not found")

const statusActive = "active"

type Store struct {
	db *gorm.DB
}

type CreateDocumentInput struct {
	ID          string
	Name        string
	Description string
	Content     string
	Scope       string
	Status      string
	CreatedBy   string
	UpdatedBy   string
}

type UpdateDocumentInput struct {
	ID          string
	Name        *string
	Description *string
	Content     *string
	Scope       *string
	Status      *string
	UpdatedBy   *string
}

type ListDocumentsFilter struct {
	Status string
	Scope  string
}

type CreateBindingInput struct {
	PromptID   string
	Scene      string
	Phase      string
	IsDefault  bool
	Priority   int
	ProviderID string
	ModelID    string
	Status     string
	CreatedBy  string
	UpdatedBy  string
}

type UpdateBindingInput struct {
	ID         uint64
	PromptID   *string
	Scene      *string
	Phase      *string
	IsDefault  *bool
	Priority   *int
	ProviderID *string
	ModelID    *string
	Status     *string
	UpdatedBy  *string
}

type ListBindingsFilter struct {
	Scene      string
	Phase      string
	Status     string
	PromptID   string
	ProviderID string
	ModelID    string
}

type DefaultBindingFilter struct {
	Scene      string
	Phase      string
	ProviderID string
	ModelID    string
}

func NewStore(db *gorm.DB) *Store {
	return &Store{db: db}
}

func (s *Store) AutoMigrate() error {
	if s == nil || s.db == nil {
		return fmt.Errorf("store db cannot be nil")
	}
	return s.db.AutoMigrate(&PromptDocument{}, &PromptBinding{})
}

func (s *Store) CreateDocument(ctx context.Context, input CreateDocumentInput) (*PromptDocument, error) {
	if err := s.requireDB(); err != nil {
		return nil, err
	}

	document, err := newPromptDocument(input)
	if err != nil {
		return nil, err
	}
	if err := s.db.WithContext(ctx).Create(&document).Error; err != nil {
		return nil, err
	}
	return &document, nil
}

func (s *Store) GetDocument(ctx context.Context, id string) (*PromptDocument, error) {
	if err := s.requireDB(); err != nil {
		return nil, err
	}

	trimmedID := strings.TrimSpace(id)
	if trimmedID == "" {
		return nil, fmt.Errorf("document id cannot be empty")
	}

	var document PromptDocument
	err := s.db.WithContext(ctx).First(&document, "id = ?", trimmedID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("%w: %s", ErrPromptDocumentNotFound, trimmedID)
	}
	if err != nil {
		return nil, err
	}
	return &document, nil
}

func (s *Store) UpdateDocument(ctx context.Context, input UpdateDocumentInput) (*PromptDocument, error) {
	if err := s.requireDB(); err != nil {
		return nil, err
	}

	document, err := s.GetDocument(ctx, input.ID)
	if err != nil {
		return nil, err
	}

	if input.Name != nil {
		name := strings.TrimSpace(*input.Name)
		if name == "" {
			return nil, fmt.Errorf("document name cannot be empty")
		}
		document.Name = name
	}
	if input.Description != nil {
		document.Description = *input.Description
	}
	if input.Content != nil {
		if *input.Content == "" {
			return nil, fmt.Errorf("document content cannot be empty")
		}
		document.Content = *input.Content
	}
	if input.Scope != nil {
		scope := strings.TrimSpace(*input.Scope)
		if scope == "" {
			return nil, fmt.Errorf("document scope cannot be empty")
		}
		document.Scope = scope
	}
	if input.Status != nil {
		document.Status = normalizeStatus(*input.Status)
	}
	if input.UpdatedBy != nil {
		document.UpdatedBy = strings.TrimSpace(*input.UpdatedBy)
	}
	if err := validateDocumentRecord(*document); err != nil {
		return nil, err
	}

	if err := s.db.WithContext(ctx).Save(document).Error; err != nil {
		return nil, err
	}
	return document, nil
}

func (s *Store) ListDocuments(ctx context.Context, filter ListDocumentsFilter) ([]PromptDocument, error) {
	if err := s.requireDB(); err != nil {
		return nil, err
	}

	query := s.db.WithContext(ctx).Model(&PromptDocument{})
	if status := strings.TrimSpace(filter.Status); status != "" {
		query = query.Where("status = ?", status)
	}
	if scope := strings.TrimSpace(filter.Scope); scope != "" {
		query = query.Where("scope = ?", scope)
	}

	var documents []PromptDocument
	err := query.Order("created_at asc").Order("id asc").Find(&documents).Error
	if err != nil {
		return nil, err
	}
	return documents, nil
}

func (s *Store) DeleteDocument(ctx context.Context, id string) error {
	if err := s.requireDB(); err != nil {
		return err
	}

	trimmedID := strings.TrimSpace(id)
	if trimmedID == "" {
		return fmt.Errorf("document id cannot be empty")
	}

	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Delete(&PromptBinding{}, "prompt_id = ?", trimmedID).Error; err != nil {
			return err
		}

		result := tx.Delete(&PromptDocument{}, "id = ?", trimmedID)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return fmt.Errorf("%w: %s", ErrPromptDocumentNotFound, trimmedID)
		}
		return nil
	})
}

func (s *Store) CreateBinding(ctx context.Context, input CreateBindingInput) (*PromptBinding, error) {
	if err := s.requireDB(); err != nil {
		return nil, err
	}

	binding, err := newPromptBinding(input)
	if err != nil {
		return nil, err
	}
	if err := s.ensureDocumentExists(ctx, binding.PromptID); err != nil {
		return nil, err
	}
	if err := s.db.WithContext(ctx).Create(&binding).Error; err != nil {
		return nil, err
	}
	return &binding, nil
}

func (s *Store) GetBinding(ctx context.Context, id uint64) (*PromptBinding, error) {
	if err := s.requireDB(); err != nil {
		return nil, err
	}

	var binding PromptBinding
	err := s.db.WithContext(ctx).First(&binding, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("%w: %d", ErrPromptBindingNotFound, id)
	}
	if err != nil {
		return nil, err
	}
	return &binding, nil
}

func (s *Store) UpdateBinding(ctx context.Context, input UpdateBindingInput) (*PromptBinding, error) {
	if err := s.requireDB(); err != nil {
		return nil, err
	}

	binding, err := s.GetBinding(ctx, input.ID)
	if err != nil {
		return nil, err
	}

	if input.PromptID != nil {
		promptID := strings.TrimSpace(*input.PromptID)
		if promptID == "" {
			return nil, fmt.Errorf("binding prompt id cannot be empty")
		}
		if err := s.ensureDocumentExists(ctx, promptID); err != nil {
			return nil, err
		}
		binding.PromptID = promptID
	}
	if input.Scene != nil {
		scene := strings.TrimSpace(*input.Scene)
		if scene == "" {
			return nil, fmt.Errorf("binding scene cannot be empty")
		}
		binding.Scene = scene
	}
	if input.Phase != nil {
		phase := strings.TrimSpace(*input.Phase)
		if phase == "" {
			return nil, fmt.Errorf("binding phase cannot be empty")
		}
		binding.Phase = phase
	}
	if input.IsDefault != nil {
		binding.IsDefault = *input.IsDefault
	}
	if input.Priority != nil {
		binding.Priority = *input.Priority
	}
	if input.ProviderID != nil {
		binding.ProviderID = strings.TrimSpace(*input.ProviderID)
	}
	if input.ModelID != nil {
		binding.ModelID = strings.TrimSpace(*input.ModelID)
	}
	if input.Status != nil {
		binding.Status = normalizeStatus(*input.Status)
	}
	if input.UpdatedBy != nil {
		binding.UpdatedBy = strings.TrimSpace(*input.UpdatedBy)
	}
	if err := validateBindingRecord(*binding); err != nil {
		return nil, err
	}

	if err := s.db.WithContext(ctx).Save(binding).Error; err != nil {
		return nil, err
	}
	return binding, nil
}

func (s *Store) DeleteBinding(ctx context.Context, id uint64) error {
	if err := s.requireDB(); err != nil {
		return err
	}

	result := s.db.WithContext(ctx).Delete(&PromptBinding{}, "id = ?", id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("%w: %d", ErrPromptBindingNotFound, id)
	}
	return nil
}

func (s *Store) ListBindings(ctx context.Context, filter ListBindingsFilter) ([]PromptBinding, error) {
	if err := s.requireDB(); err != nil {
		return nil, err
	}

	query := s.db.WithContext(ctx).Model(&PromptBinding{})
	if scene := strings.TrimSpace(filter.Scene); scene != "" {
		query = query.Where("scene = ?", scene)
	}
	if phase := strings.TrimSpace(filter.Phase); phase != "" {
		query = query.Where("phase = ?", phase)
	}
	if status := strings.TrimSpace(filter.Status); status != "" {
		query = query.Where("status = ?", status)
	}
	if promptID := strings.TrimSpace(filter.PromptID); promptID != "" {
		query = query.Where("prompt_id = ?", promptID)
	}
	if providerID := strings.TrimSpace(filter.ProviderID); providerID != "" {
		query = query.Where("provider_id = ?", providerID)
	}
	if modelID := strings.TrimSpace(filter.ModelID); modelID != "" {
		query = query.Where("model_id = ?", modelID)
	}

	var bindings []PromptBinding
	err := query.Order("created_at asc").Order("id asc").Find(&bindings).Error
	if err != nil {
		return nil, err
	}
	return bindings, nil
}

func (s *Store) ListDefaultBindings(ctx context.Context, filter DefaultBindingFilter) ([]PromptBinding, error) {
	if err := s.requireDB(); err != nil {
		return nil, err
	}

	scene := strings.TrimSpace(filter.Scene)
	if scene == "" {
		return nil, fmt.Errorf("binding scene cannot be empty")
	}
	phase := strings.TrimSpace(filter.Phase)
	if phase == "" {
		return nil, fmt.Errorf("binding phase cannot be empty")
	}

	bindingTable := (PromptBinding{}).TableName()
	documentTable := (PromptDocument{}).TableName()
	query := s.db.WithContext(ctx).
		Model(&PromptBinding{}).
		Preload("Prompt").
		Joins("JOIN "+documentTable+" ON "+documentTable+".id = "+bindingTable+".prompt_id").
		Where(bindingTable+".scene = ?", scene).
		Where(bindingTable+".phase = ?", phase).
		Where(bindingTable+".is_default = ?", true).
		Where(bindingTable+".status = ?", statusActive).
		Where(documentTable+".status = ?", statusActive)

	if providerID := strings.TrimSpace(filter.ProviderID); providerID != "" {
		query = query.Where("("+bindingTable+".provider_id = '' OR "+bindingTable+".provider_id = ?)", providerID)
	}
	if modelID := strings.TrimSpace(filter.ModelID); modelID != "" {
		query = query.Where("("+bindingTable+".model_id = '' OR "+bindingTable+".model_id = ?)", modelID)
	}

	var bindings []PromptBinding
	err := query.Order(bindingTable + ".priority asc").Order(bindingTable + ".created_at asc").Order(bindingTable + ".id asc").Find(&bindings).Error
	if err != nil {
		return nil, err
	}
	return bindings, nil
}

func (s *Store) requireDB() error {
	if s == nil || s.db == nil {
		return fmt.Errorf("store db cannot be nil")
	}
	return nil
}

func (s *Store) ensureDocumentExists(ctx context.Context, id string) error {
	trimmedID := strings.TrimSpace(id)
	if trimmedID == "" {
		return fmt.Errorf("document id cannot be empty")
	}

	var count int64
	err := s.db.WithContext(ctx).Model(&PromptDocument{}).Where("id = ?", trimmedID).Count(&count).Error
	if err != nil {
		return err
	}
	if count == 0 {
		return fmt.Errorf("%w: %s", ErrPromptDocumentNotFound, trimmedID)
	}
	return nil
}

func newPromptDocument(input CreateDocumentInput) (PromptDocument, error) {
	document := PromptDocument{
		ID:          strings.TrimSpace(input.ID),
		Name:        strings.TrimSpace(input.Name),
		Description: input.Description,
		Content:     input.Content,
		Scope:       strings.TrimSpace(input.Scope),
		Status:      normalizeStatus(input.Status),
		CreatedBy:   strings.TrimSpace(input.CreatedBy),
		UpdatedBy:   strings.TrimSpace(input.UpdatedBy),
	}
	if err := validateDocumentRecord(document); err != nil {
		return PromptDocument{}, err
	}
	return document, nil
}

func newPromptBinding(input CreateBindingInput) (PromptBinding, error) {
	binding := PromptBinding{
		PromptID:   strings.TrimSpace(input.PromptID),
		Scene:      strings.TrimSpace(input.Scene),
		Phase:      strings.TrimSpace(input.Phase),
		IsDefault:  input.IsDefault,
		Priority:   input.Priority,
		ProviderID: strings.TrimSpace(input.ProviderID),
		ModelID:    strings.TrimSpace(input.ModelID),
		Status:     normalizeStatus(input.Status),
		CreatedBy:  strings.TrimSpace(input.CreatedBy),
		UpdatedBy:  strings.TrimSpace(input.UpdatedBy),
	}
	if err := validateBindingRecord(binding); err != nil {
		return PromptBinding{}, err
	}
	return binding, nil
}

func normalizeStatus(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return statusActive
	}
	return trimmed
}

func validateDocumentRecord(document PromptDocument) error {
	if strings.TrimSpace(document.ID) == "" {
		return fmt.Errorf("document id cannot be empty")
	}
	if strings.TrimSpace(document.Name) == "" {
		return fmt.Errorf("document name cannot be empty")
	}
	if document.Content == "" {
		return fmt.Errorf("document content cannot be empty")
	}
	if strings.TrimSpace(document.Scope) == "" {
		return fmt.Errorf("document scope cannot be empty")
	}
	if strings.TrimSpace(document.Status) == "" {
		return fmt.Errorf("document status cannot be empty")
	}
	return nil
}

func validateBindingRecord(binding PromptBinding) error {
	if strings.TrimSpace(binding.PromptID) == "" {
		return fmt.Errorf("binding prompt id cannot be empty")
	}
	if strings.TrimSpace(binding.Scene) == "" {
		return fmt.Errorf("binding scene cannot be empty")
	}
	if strings.TrimSpace(binding.Phase) == "" {
		return fmt.Errorf("binding phase cannot be empty")
	}
	if strings.TrimSpace(binding.Status) == "" {
		return fmt.Errorf("binding status cannot be empty")
	}
	return nil
}
