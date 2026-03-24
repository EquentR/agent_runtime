package prompt

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestStoreAutoMigrateCreatesPromptTables(t *testing.T) {
	db := newTestDB(t)
	store := NewStore(db)

	if err := store.AutoMigrate(); err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
	}
	if !db.Migrator().HasTable(&PromptDocument{}) {
		t.Fatal("PromptDocument table missing after auto-migrate")
	}
	if !db.Migrator().HasTable(&PromptBinding{}) {
		t.Fatal("PromptBinding table missing after auto-migrate")
	}
}

func TestStoreCreateAndGetDocument(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	created, err := store.CreateDocument(ctx, CreateDocumentInput{
		ID:          " doc-welcome ",
		Name:        "Welcome",
		Description: "Initial prompt",
		Content:     "You are helpful.",
		Scope:       " admin ",
		Status:      " active ",
		CreatedBy:   " admin-1 ",
		UpdatedBy:   " admin-1 ",
	})
	if err != nil {
		t.Fatalf("CreateDocument() error = %v", err)
	}
	if created.ID != "doc-welcome" {
		t.Fatalf("created id = %q, want %q", created.ID, "doc-welcome")
	}
	if created.Scope != "admin" {
		t.Fatalf("created scope = %q, want %q", created.Scope, "admin")
	}
	if created.Status != "active" {
		t.Fatalf("created status = %q, want %q", created.Status, "active")
	}
	if created.CreatedBy != "admin-1" {
		t.Fatalf("created_by = %q, want %q", created.CreatedBy, "admin-1")
	}

	got, err := store.GetDocument(ctx, " doc-welcome ")
	if err != nil {
		t.Fatalf("GetDocument() error = %v", err)
	}
	if got.ID != "doc-welcome" {
		t.Fatalf("got id = %q, want %q", got.ID, "doc-welcome")
	}
	if got.Content != "You are helpful." {
		t.Fatalf("got content = %q, want %q", got.Content, "You are helpful.")
	}
}

func TestStoreUpdateDocument(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	mustCreateDocument(t, store, CreateDocumentInput{
		ID:          "doc-update",
		Name:        "Original",
		Description: "Original description",
		Content:     "Original content",
		Scope:       "admin",
		Status:      "active",
		CreatedBy:   "admin-1",
		UpdatedBy:   "admin-1",
	})

	updated, err := store.UpdateDocument(ctx, UpdateDocumentInput{
		ID:          " doc-update ",
		Name:        stringPtr("Updated"),
		Description: stringPtr("Updated description"),
		Content:     stringPtr("Updated content"),
		Scope:       stringPtr(" workspace "),
		Status:      stringPtr(" disabled "),
		UpdatedBy:   stringPtr(" admin-2 "),
	})
	if err != nil {
		t.Fatalf("UpdateDocument() error = %v", err)
	}
	if updated.Name != "Updated" {
		t.Fatalf("updated name = %q, want %q", updated.Name, "Updated")
	}
	if updated.Scope != "workspace" {
		t.Fatalf("updated scope = %q, want %q", updated.Scope, "workspace")
	}
	if updated.Status != "disabled" {
		t.Fatalf("updated status = %q, want %q", updated.Status, "disabled")
	}
	if updated.UpdatedBy != "admin-2" {
		t.Fatalf("updated_by = %q, want %q", updated.UpdatedBy, "admin-2")
	}

	got, err := store.GetDocument(ctx, "doc-update")
	if err != nil {
		t.Fatalf("GetDocument() error = %v", err)
	}
	if got.Description != "Updated description" {
		t.Fatalf("got description = %q, want %q", got.Description, "Updated description")
	}
	if got.Content != "Updated content" {
		t.Fatalf("got content = %q, want %q", got.Content, "Updated content")
	}
	if got.CreatedBy != "admin-1" {
		t.Fatalf("created_by = %q, want preserved %q", got.CreatedBy, "admin-1")
	}
}

func TestStoreUpdateDocumentPatchPreservesOmittedFields(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	mustCreateDocument(t, store, CreateDocumentInput{
		ID:          "doc-patch",
		Name:        "Original",
		Description: "Original description",
		Content:     "Original content",
		Scope:       "admin",
		Status:      "disabled",
		CreatedBy:   "admin-1",
		UpdatedBy:   "admin-1",
	})

	updated, err := store.UpdateDocument(ctx, UpdateDocumentInput{
		ID:        " doc-patch ",
		Name:      stringPtr("Renamed"),
		UpdatedBy: stringPtr(" admin-2 "),
	})
	if err != nil {
		t.Fatalf("UpdateDocument() patch error = %v", err)
	}
	if updated.Name != "Renamed" {
		t.Fatalf("updated name = %q, want %q", updated.Name, "Renamed")
	}
	if updated.Description != "Original description" {
		t.Fatalf("updated description = %q, want preserved %q", updated.Description, "Original description")
	}
	if updated.Content != "Original content" {
		t.Fatalf("updated content = %q, want preserved %q", updated.Content, "Original content")
	}
	if updated.Scope != "admin" {
		t.Fatalf("updated scope = %q, want preserved %q", updated.Scope, "admin")
	}
	if updated.Status != "disabled" {
		t.Fatalf("updated status = %q, want preserved %q", updated.Status, "disabled")
	}
	if updated.UpdatedBy != "admin-2" {
		t.Fatalf("updated_by = %q, want %q", updated.UpdatedBy, "admin-2")
	}
}

func TestStoreListDocumentsFiltersAndStableOrder(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	fixed := time.Date(2026, time.March, 23, 9, 0, 0, 0, time.UTC)

	for _, input := range []CreateDocumentInput{
		{ID: "doc-b", Name: "B", Content: "B", Scope: "admin", Status: "active"},
		{ID: "doc-a", Name: "A", Content: "A", Scope: "admin", Status: "active"},
		{ID: "doc-c", Name: "C", Content: "C", Scope: "admin", Status: "disabled"},
		{ID: "doc-d", Name: "D", Content: "D", Scope: "workspace", Status: "active"},
	} {
		mustCreateDocument(t, store, input)
	}
	for _, id := range []string{"doc-a", "doc-b", "doc-c", "doc-d"} {
		setDocumentCreatedAt(t, store, id, fixed)
	}

	activeDocs, err := store.ListDocuments(ctx, ListDocumentsFilter{Status: " active "})
	if err != nil {
		t.Fatalf("ListDocuments(status) error = %v", err)
	}
	if got := documentIDs(activeDocs); !reflect.DeepEqual(got, []string{"doc-a", "doc-b", "doc-d"}) {
		t.Fatalf("active document ids = %#v, want %#v", got, []string{"doc-a", "doc-b", "doc-d"})
	}

	adminDocs, err := store.ListDocuments(ctx, ListDocumentsFilter{Scope: " admin "})
	if err != nil {
		t.Fatalf("ListDocuments(scope) error = %v", err)
	}
	if got := documentIDs(adminDocs); !reflect.DeepEqual(got, []string{"doc-a", "doc-b", "doc-c"}) {
		t.Fatalf("admin document ids = %#v, want %#v", got, []string{"doc-a", "doc-b", "doc-c"})
	}

	activeAdminDocs, err := store.ListDocuments(ctx, ListDocumentsFilter{Status: " active ", Scope: " admin "})
	if err != nil {
		t.Fatalf("ListDocuments(status+scope) error = %v", err)
	}
	if got := documentIDs(activeAdminDocs); !reflect.DeepEqual(got, []string{"doc-a", "doc-b"}) {
		t.Fatalf("active admin document ids = %#v, want %#v", got, []string{"doc-a", "doc-b"})
	}
}

func TestStoreDeleteDocumentAlsoRemovesBindings(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	mustCreateDocument(t, store, CreateDocumentInput{ID: "doc-delete", Name: "Delete me", Content: "Delete me", Scope: "admin", Status: "active"})
	created := mustCreateBinding(t, store, CreateBindingInput{PromptID: "doc-delete", Scene: "agent.run.default", Phase: "session", IsDefault: true, Status: "active"})

	if err := store.DeleteDocument(ctx, " doc-delete "); err != nil {
		t.Fatalf("DeleteDocument() error = %v", err)
	}

	if _, err := store.GetDocument(ctx, "doc-delete"); !errors.Is(err, ErrPromptDocumentNotFound) {
		t.Fatalf("GetDocument(deleted) error = %v, want ErrPromptDocumentNotFound", err)
	}
	if _, err := store.GetBinding(ctx, created.ID); !errors.Is(err, ErrPromptBindingNotFound) {
		t.Fatalf("GetBinding(cascaded) error = %v, want ErrPromptBindingNotFound", err)
	}

	bindings, err := store.ListBindings(ctx, ListBindingsFilter{PromptID: "doc-delete"})
	if err != nil {
		t.Fatalf("ListBindings(after delete) error = %v", err)
	}
	if len(bindings) != 0 {
		t.Fatalf("len(bindings after delete) = %d, want 0", len(bindings))
	}
}

func TestStoreBindingCRUD(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	mustCreateDocument(t, store, CreateDocumentInput{
		ID:      "doc-binding",
		Name:    "Binding doc",
		Content: "Hello",
		Scope:   "admin",
		Status:  "active",
	})
	mustCreateDocument(t, store, CreateDocumentInput{
		ID:      "doc-binding-2",
		Name:    "Binding doc 2",
		Content: "Hello again",
		Scope:   "admin",
		Status:  "active",
	})

	created, err := store.CreateBinding(ctx, CreateBindingInput{
		PromptID:   " doc-binding ",
		Scene:      " agent.run ",
		Phase:      " session ",
		IsDefault:  false,
		Priority:   10,
		ProviderID: " openai ",
		ModelID:    " gpt-4o ",
		Status:     " active ",
		CreatedBy:  " admin-1 ",
		UpdatedBy:  " admin-1 ",
	})
	if err != nil {
		t.Fatalf("CreateBinding() error = %v", err)
	}
	if created.ID == 0 {
		t.Fatal("created binding id = 0, want non-zero")
	}
	if created.PromptID != "doc-binding" {
		t.Fatalf("created prompt_id = %q, want %q", created.PromptID, "doc-binding")
	}
	if created.Scene != "agent.run" {
		t.Fatalf("created scene = %q, want %q", created.Scene, "agent.run")
	}
	if created.Phase != "session" {
		t.Fatalf("created phase = %q, want %q", created.Phase, "session")
	}
	if created.Status != "active" {
		t.Fatalf("created status = %q, want %q", created.Status, "active")
	}

	got, err := store.GetBinding(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetBinding() error = %v", err)
	}
	if got.ProviderID != "openai" {
		t.Fatalf("provider_id = %q, want %q", got.ProviderID, "openai")
	}
	if got.ModelID != "gpt-4o" {
		t.Fatalf("model_id = %q, want %q", got.ModelID, "gpt-4o")
	}

	updated, err := store.UpdateBinding(ctx, UpdateBindingInput{
		ID:         created.ID,
		PromptID:   stringPtr(" doc-binding-2 "),
		Scene:      stringPtr(" agent.run "),
		Phase:      stringPtr(" tool_result "),
		IsDefault:  boolPtr(true),
		Priority:   intPtr(1),
		ProviderID: stringPtr(" anthropic "),
		ModelID:    stringPtr(" claude-3-7-sonnet "),
		Status:     stringPtr(" disabled "),
		UpdatedBy:  stringPtr(" admin-2 "),
	})
	if err != nil {
		t.Fatalf("UpdateBinding() error = %v", err)
	}
	if updated.PromptID != "doc-binding-2" {
		t.Fatalf("updated prompt_id = %q, want %q", updated.PromptID, "doc-binding-2")
	}
	if updated.Phase != "tool_result" {
		t.Fatalf("updated phase = %q, want %q", updated.Phase, "tool_result")
	}
	if !updated.IsDefault {
		t.Fatal("updated is_default = false, want true")
	}
	if updated.Status != "disabled" {
		t.Fatalf("updated status = %q, want %q", updated.Status, "disabled")
	}
	if updated.UpdatedBy != "admin-2" {
		t.Fatalf("updated_by = %q, want %q", updated.UpdatedBy, "admin-2")
	}

	if err := store.DeleteBinding(ctx, created.ID); err != nil {
		t.Fatalf("DeleteBinding() error = %v", err)
	}
	if _, err := store.GetBinding(ctx, created.ID); !errors.Is(err, ErrPromptBindingNotFound) {
		t.Fatalf("GetBinding() after delete error = %v, want ErrPromptBindingNotFound", err)
	}
}

func TestStoreUpdateBindingPatchPreservesOmittedFields(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	mustCreateDocument(t, store, CreateDocumentInput{ID: "doc-binding", Name: "Binding doc", Content: "Hello", Scope: "admin", Status: "active"})

	created := mustCreateBinding(t, store, CreateBindingInput{
		PromptID:   "doc-binding",
		Scene:      "agent.run",
		Phase:      "session",
		IsDefault:  true,
		Priority:   7,
		ProviderID: "openai",
		ModelID:    "gpt-4o",
		Status:     "disabled",
		CreatedBy:  "admin-1",
		UpdatedBy:  "admin-1",
	})

	updated, err := store.UpdateBinding(ctx, UpdateBindingInput{
		ID:        created.ID,
		Phase:     stringPtr("tool_result"),
		UpdatedBy: stringPtr("admin-2"),
	})
	if err != nil {
		t.Fatalf("UpdateBinding() patch error = %v", err)
	}
	if updated.PromptID != "doc-binding" {
		t.Fatalf("updated prompt_id = %q, want preserved %q", updated.PromptID, "doc-binding")
	}
	if updated.Scene != "agent.run" {
		t.Fatalf("updated scene = %q, want preserved %q", updated.Scene, "agent.run")
	}
	if updated.Phase != "tool_result" {
		t.Fatalf("updated phase = %q, want %q", updated.Phase, "tool_result")
	}
	if !updated.IsDefault {
		t.Fatal("updated is_default = false, want preserved true")
	}
	if updated.Priority != 7 {
		t.Fatalf("updated priority = %d, want preserved %d", updated.Priority, 7)
	}
	if updated.ProviderID != "openai" {
		t.Fatalf("updated provider_id = %q, want preserved %q", updated.ProviderID, "openai")
	}
	if updated.ModelID != "gpt-4o" {
		t.Fatalf("updated model_id = %q, want preserved %q", updated.ModelID, "gpt-4o")
	}
	if updated.Status != "disabled" {
		t.Fatalf("updated status = %q, want preserved %q", updated.Status, "disabled")
	}
	if updated.UpdatedBy != "admin-2" {
		t.Fatalf("updated_by = %q, want %q", updated.UpdatedBy, "admin-2")
	}
}

func TestStoreCreateBindingRejectsUnknownPromptDocument(t *testing.T) {
	store := newTestStore(t)

	_, err := store.CreateBinding(context.Background(), CreateBindingInput{
		PromptID:  "missing-doc",
		Scene:     "agent.run",
		Phase:     "session",
		IsDefault: true,
		Status:    "active",
	})
	if !errors.Is(err, ErrPromptDocumentNotFound) {
		t.Fatalf("CreateBinding() error = %v, want ErrPromptDocumentNotFound", err)
	}
}

func TestStoreUpdateBindingRejectsUnknownPromptDocument(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	mustCreateDocument(t, store, CreateDocumentInput{ID: "doc-existing", Name: "Existing", Content: "Hello", Scope: "admin", Status: "active"})
	created := mustCreateBinding(t, store, CreateBindingInput{
		PromptID:  "doc-existing",
		Scene:     "agent.run",
		Phase:     "session",
		IsDefault: true,
		Status:    "active",
	})

	_, err := store.UpdateBinding(ctx, UpdateBindingInput{
		ID:       created.ID,
		PromptID: stringPtr("missing-doc"),
	})
	if !errors.Is(err, ErrPromptDocumentNotFound) {
		t.Fatalf("UpdateBinding() error = %v, want ErrPromptDocumentNotFound", err)
	}
}

func TestStoreListBindingsFiltersAndStableOrder(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	fixed := time.Date(2026, time.March, 23, 10, 0, 0, 0, time.UTC)

	mustCreateDocument(t, store, CreateDocumentInput{ID: "doc-a", Name: "A", Content: "A", Scope: "admin", Status: "active"})
	mustCreateDocument(t, store, CreateDocumentInput{ID: "doc-b", Name: "B", Content: "B", Scope: "admin", Status: "active"})

	binding1 := mustCreateBinding(t, store, CreateBindingInput{PromptID: "doc-b", Scene: "agent.run", Phase: "session", Status: "active"})
	binding2 := mustCreateBinding(t, store, CreateBindingInput{PromptID: "doc-a", Scene: "agent.run", Phase: "session", Status: "active"})
	binding3 := mustCreateBinding(t, store, CreateBindingInput{PromptID: "doc-a", Scene: "agent.run", Phase: "session", Status: "disabled"})
	binding4 := mustCreateBinding(t, store, CreateBindingInput{PromptID: "doc-a", Scene: "agent.run", Phase: "tool_result", Status: "active"})
	for _, id := range []uint64{binding1.ID, binding2.ID, binding3.ID, binding4.ID} {
		setBindingCreatedAt(t, store, id, fixed)
	}

	matching, err := store.ListBindings(ctx, ListBindingsFilter{
		Scene:    " agent.run ",
		Phase:    " session ",
		Status:   " active ",
		PromptID: " ",
	})
	if err != nil {
		t.Fatalf("ListBindings(scene+phase+status) error = %v", err)
	}
	if got := bindingIDs(matching); !reflect.DeepEqual(got, []uint64{binding1.ID, binding2.ID}) {
		t.Fatalf("active binding ids = %#v, want %#v", got, []uint64{binding1.ID, binding2.ID})
	}

	forDocument, err := store.ListBindings(ctx, ListBindingsFilter{PromptID: " doc-a "})
	if err != nil {
		t.Fatalf("ListBindings(prompt_id) error = %v", err)
	}
	if got := bindingIDs(forDocument); !reflect.DeepEqual(got, []uint64{binding2.ID, binding3.ID, binding4.ID}) {
		t.Fatalf("document binding ids = %#v, want %#v", got, []uint64{binding2.ID, binding3.ID, binding4.ID})
	}
}

func TestStoreListDefaultBindingsReturnsActiveDefaultsInOrder(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	base := time.Date(2026, time.March, 23, 11, 0, 0, 0, time.UTC)

	for _, id := range []string{"doc-1", "doc-2", "doc-3", "doc-4"} {
		mustCreateDocument(t, store, CreateDocumentInput{ID: id, Name: id, Content: id, Scope: "admin", Status: "active"})
	}

	binding1 := mustCreateBinding(t, store, CreateBindingInput{PromptID: "doc-1", Scene: "agent.run", Phase: "session", IsDefault: true, Priority: 5, Status: "active"})
	binding2 := mustCreateBinding(t, store, CreateBindingInput{PromptID: "doc-2", Scene: "agent.run", Phase: "session", IsDefault: true, Priority: 5, Status: "active"})
	binding3 := mustCreateBinding(t, store, CreateBindingInput{PromptID: "doc-3", Scene: "agent.run", Phase: "session", IsDefault: true, Priority: 5, Status: "active"})
	binding4 := mustCreateBinding(t, store, CreateBindingInput{PromptID: "doc-4", Scene: "agent.run", Phase: "session", IsDefault: true, Priority: 10, Status: "active"})

	setBindingCreatedAt(t, store, binding1.ID, base.Add(2*time.Minute))
	setBindingCreatedAt(t, store, binding2.ID, base)
	setBindingCreatedAt(t, store, binding3.ID, base)
	setBindingCreatedAt(t, store, binding4.ID, base.Add(-2*time.Minute))

	bindings, err := store.ListDefaultBindings(ctx, DefaultBindingFilter{Scene: " agent.run ", Phase: " session "})
	if err != nil {
		t.Fatalf("ListDefaultBindings() error = %v", err)
	}
	if got := bindingIDs(bindings); !reflect.DeepEqual(got, []uint64{binding2.ID, binding3.ID, binding1.ID, binding4.ID}) {
		t.Fatalf("default binding ids = %#v, want %#v", got, []uint64{binding2.ID, binding3.ID, binding1.ID, binding4.ID})
	}
	if len(bindings) == 0 || bindings[0].Prompt == nil || bindings[0].Prompt.ID != "doc-2" {
		t.Fatalf("first binding prompt = %#v, want preloaded prompt doc-2", bindings[0].Prompt)
	}
}

func TestStoreListDefaultBindingsExcludesDisabledDocuments(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	mustCreateDocument(t, store, CreateDocumentInput{ID: "doc-active", Name: "active", Content: "active", Scope: "admin", Status: "active"})
	mustCreateDocument(t, store, CreateDocumentInput{ID: "doc-disabled", Name: "disabled", Content: "disabled", Scope: "admin", Status: "disabled"})

	active := mustCreateBinding(t, store, CreateBindingInput{PromptID: "doc-active", Scene: "agent.run", Phase: "session", IsDefault: true, Status: "active"})
	mustCreateBinding(t, store, CreateBindingInput{PromptID: "doc-disabled", Scene: "agent.run", Phase: "session", IsDefault: true, Status: "active"})

	bindings, err := store.ListDefaultBindings(ctx, DefaultBindingFilter{Scene: "agent.run", Phase: "session"})
	if err != nil {
		t.Fatalf("ListDefaultBindings() error = %v", err)
	}
	if got := bindingIDs(bindings); !reflect.DeepEqual(got, []uint64{active.ID}) {
		t.Fatalf("default binding ids = %#v, want %#v", got, []uint64{active.ID})
	}
}

func TestStoreListDefaultBindingsExcludesDisabledBindings(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	mustCreateDocument(t, store, CreateDocumentInput{ID: "doc-active", Name: "active", Content: "active", Scope: "admin", Status: "active"})

	active := mustCreateBinding(t, store, CreateBindingInput{PromptID: "doc-active", Scene: "agent.run", Phase: "session", IsDefault: true, Status: "active"})
	mustCreateBinding(t, store, CreateBindingInput{PromptID: "doc-active", Scene: "agent.run", Phase: "session", IsDefault: true, Status: "disabled"})

	bindings, err := store.ListDefaultBindings(ctx, DefaultBindingFilter{Scene: "agent.run", Phase: "session"})
	if err != nil {
		t.Fatalf("ListDefaultBindings() error = %v", err)
	}
	if got := bindingIDs(bindings); !reflect.DeepEqual(got, []uint64{active.ID}) {
		t.Fatalf("default binding ids = %#v, want %#v", got, []uint64{active.ID})
	}
}

func TestStoreListDefaultBindingsOptionalProviderAndModelFilters(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	for _, id := range []string{"doc-generic", "doc-provider", "doc-model", "doc-other-provider", "doc-other-model"} {
		mustCreateDocument(t, store, CreateDocumentInput{ID: id, Name: id, Content: id, Scope: "admin", Status: "active"})
	}

	generic := mustCreateBinding(t, store, CreateBindingInput{PromptID: "doc-generic", Scene: "agent.run", Phase: "session", IsDefault: true, Status: "active"})
	providerOnly := mustCreateBinding(t, store, CreateBindingInput{PromptID: "doc-provider", Scene: "agent.run", Phase: "session", IsDefault: true, ProviderID: "openai", Status: "active"})
	modelSpecific := mustCreateBinding(t, store, CreateBindingInput{PromptID: "doc-model", Scene: "agent.run", Phase: "session", IsDefault: true, ProviderID: "openai", ModelID: "gpt-4o", Status: "active"})
	otherProvider := mustCreateBinding(t, store, CreateBindingInput{PromptID: "doc-other-provider", Scene: "agent.run", Phase: "session", IsDefault: true, ProviderID: "anthropic", Status: "active"})
	otherModel := mustCreateBinding(t, store, CreateBindingInput{PromptID: "doc-other-model", Scene: "agent.run", Phase: "session", IsDefault: true, ProviderID: "openai", ModelID: "gpt-4.1", Status: "active"})

	allBindings, err := store.ListDefaultBindings(ctx, DefaultBindingFilter{Scene: "agent.run", Phase: "session", ProviderID: " ", ModelID: " "})
	if err != nil {
		t.Fatalf("ListDefaultBindings(empty provider/model) error = %v", err)
	}
	if got := bindingIDs(allBindings); !reflect.DeepEqual(got, []uint64{generic.ID, providerOnly.ID, modelSpecific.ID, otherProvider.ID, otherModel.ID}) {
		t.Fatalf("all default binding ids = %#v, want %#v", got, []uint64{generic.ID, providerOnly.ID, modelSpecific.ID, otherProvider.ID, otherModel.ID})
	}

	providerBindings, err := store.ListDefaultBindings(ctx, DefaultBindingFilter{Scene: "agent.run", Phase: "session", ProviderID: " openai "})
	if err != nil {
		t.Fatalf("ListDefaultBindings(provider) error = %v", err)
	}
	if got := bindingIDs(providerBindings); !reflect.DeepEqual(got, []uint64{generic.ID, providerOnly.ID, modelSpecific.ID, otherModel.ID}) {
		t.Fatalf("openai binding ids = %#v, want %#v", got, []uint64{generic.ID, providerOnly.ID, modelSpecific.ID, otherModel.ID})
	}

	modelBindings, err := store.ListDefaultBindings(ctx, DefaultBindingFilter{Scene: "agent.run", Phase: "session", ProviderID: " openai ", ModelID: " gpt-4o "})
	if err != nil {
		t.Fatalf("ListDefaultBindings(provider+model) error = %v", err)
	}
	if got := bindingIDs(modelBindings); !reflect.DeepEqual(got, []uint64{generic.ID, providerOnly.ID, modelSpecific.ID}) {
		t.Fatalf("openai model binding ids = %#v, want %#v", got, []uint64{generic.ID, providerOnly.ID, modelSpecific.ID})
	}
}

func TestStoreListDefaultBindingsModelFilterIncludesGenericBindings(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	for _, id := range []string{"doc-generic", "doc-model-match", "doc-model-other"} {
		mustCreateDocument(t, store, CreateDocumentInput{ID: id, Name: id, Content: id, Scope: "admin", Status: "active"})
	}

	generic := mustCreateBinding(t, store, CreateBindingInput{PromptID: "doc-generic", Scene: "agent.run", Phase: "session", IsDefault: true, Status: "active"})
	match := mustCreateBinding(t, store, CreateBindingInput{PromptID: "doc-model-match", Scene: "agent.run", Phase: "session", IsDefault: true, ModelID: "gpt-4o", Status: "active"})
	mismatch := mustCreateBinding(t, store, CreateBindingInput{PromptID: "doc-model-other", Scene: "agent.run", Phase: "session", IsDefault: true, ModelID: "gpt-4.1", Status: "active"})

	bindings, err := store.ListDefaultBindings(ctx, DefaultBindingFilter{Scene: "agent.run", Phase: "session", ModelID: " gpt-4o "})
	if err != nil {
		t.Fatalf("ListDefaultBindings(model only) error = %v", err)
	}
	if got := bindingIDs(bindings); !reflect.DeepEqual(got, []uint64{generic.ID, match.ID}) {
		t.Fatalf("model-only binding ids = %#v, want %#v (excluding %d)", got, []uint64{generic.ID, match.ID}, mismatch.ID)
	}
}

func mustCreateDocument(t *testing.T, store *Store, input CreateDocumentInput) *PromptDocument {
	t.Helper()

	document, err := store.CreateDocument(context.Background(), input)
	if err != nil {
		t.Fatalf("CreateDocument(%+v) error = %v", input, err)
	}
	return document
}

func mustCreateBinding(t *testing.T, store *Store, input CreateBindingInput) *PromptBinding {
	t.Helper()

	binding, err := store.CreateBinding(context.Background(), input)
	if err != nil {
		t.Fatalf("CreateBinding(%+v) error = %v", input, err)
	}
	return binding
}

func setDocumentCreatedAt(t *testing.T, store *Store, id string, createdAt time.Time) {
	t.Helper()

	if err := store.db.Model(&PromptDocument{}).Where("id = ?", id).UpdateColumn("created_at", createdAt).Error; err != nil {
		t.Fatalf("set document created_at for %q error = %v", id, err)
	}
}

func setBindingCreatedAt(t *testing.T, store *Store, id uint64, createdAt time.Time) {
	t.Helper()

	if err := store.db.Model(&PromptBinding{}).Where("id = ?", id).UpdateColumn("created_at", createdAt).Error; err != nil {
		t.Fatalf("set binding created_at for %d error = %v", id, err)
	}
}

func documentIDs(documents []PromptDocument) []string {
	ids := make([]string, 0, len(documents))
	for _, document := range documents {
		ids = append(ids, document.ID)
	}
	return ids
}

func bindingIDs(bindings []PromptBinding) []uint64 {
	ids := make([]uint64, 0, len(bindings))
	for _, binding := range bindings {
		ids = append(ids, binding.ID)
	}
	return ids
}

func newTestStore(t *testing.T) *Store {
	t.Helper()

	store := NewStore(newTestDB(t))
	if err := store.AutoMigrate(); err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
	}
	return store
}

func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("gorm.Open() error = %v", err)
	}
	return db
}

func stringPtr(value string) *string {
	return &value
}

func boolPtr(value bool) *bool {
	return &value
}

func intPtr(value int) *int {
	return &value
}
