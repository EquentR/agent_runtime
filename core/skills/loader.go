package skills

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	skillsDirectoryName   = "skills"
	skillDocumentName     = "SKILL.md"
	skillDocumentMaxBytes = 32 * 1024
)

type Loader struct {
	workspaceRoot string
}

func NewLoader(workspaceRoot string) *Loader {
	return &Loader{workspaceRoot: strings.TrimSpace(workspaceRoot)}
}

func (l *Loader) List(ctx context.Context) ([]SkillListItem, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	skillsRoot, err := l.skillsRoot()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(skillsRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return []SkillListItem{}, nil
		}
		return nil, fmt.Errorf("list workspace skills: %w", err)
	}

	items := make([]SkillListItem, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		skill, err := l.Get(ctx, entry.Name())
		if err != nil {
			if errors.Is(err, ErrSkillNotFound) || errors.Is(err, ErrInvalidSkillName) {
				continue
			}
			return nil, err
		}
		items = append(items, SkillListItem{
			Name:        skill.Name,
			Description: skill.Description,
			Tags:        skill.Tags,
			Tools:       skill.Tools,
			Version:     skill.Version,
			Hidden:      skill.Hidden,
			SourceRef:   skill.SourceRef,
		})
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].Name < items[j].Name
	})
	return items, nil
}

func (l *Loader) Get(ctx context.Context, name string) (*Skill, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	name, err := normalizeSkillName(name)
	if err != nil {
		return nil, err
	}

	skillsRoot, err := l.skillsRoot()
	if err != nil {
		return nil, err
	}
	entryName, directory, err := l.resolveSkillDirectory(skillsRoot, name)
	if err != nil {
		return nil, err
	}

	documentPath := filepath.Join(directory, skillDocumentName)
	docInfo, err := os.Lstat(documentPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("resolve skill %q: %w", name, ErrSkillNotFound)
		}
		return nil, fmt.Errorf("stat skill %q document: %w", name, err)
	}
	if docInfo.Mode()&os.ModeSymlink != 0 || !docInfo.Mode().IsRegular() {
		return nil, fmt.Errorf("resolve skill %q: %w", name, ErrSkillNotFound)
	}

	file, err := os.Open(documentPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("resolve skill %q: %w", name, ErrSkillNotFound)
		}
		return nil, fmt.Errorf("open skill %q document: %w", name, err)
	}
	defer file.Close()

	content, err := io.ReadAll(io.LimitReader(file, skillDocumentMaxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read skill %q document: %w", name, err)
	}
	if len(content) > skillDocumentMaxBytes {
		return nil, fmt.Errorf("resolve skill %q: %w", name, ErrInvalidSkillDocument)
	}

	sourceRef := filepath.ToSlash(filepath.Join(skillsDirectoryName, entryName, skillDocumentName))
	skill, err := parseSkillDocument(entryName, sourceRef, string(content))
	if err != nil {
		return nil, err
	}
	skill.Directory = directory
	return skill, nil
}

func (l *Loader) resolveSkillDirectory(skillsRoot string, name string) (entryName string, directory string, err error) {
	entries, readErr := os.ReadDir(skillsRoot)
	if readErr != nil {
		if os.IsNotExist(readErr) {
			return "", "", fmt.Errorf("resolve skill %q: %w", name, ErrSkillNotFound)
		}
		return "", "", fmt.Errorf("list workspace skills: %w", readErr)
	}
	for _, entry := range entries {
		if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		if entry.Name() != name {
			continue
		}
		entryName = entry.Name()
		directory = filepath.Join(skillsRoot, entryName)
		info, err := os.Lstat(directory)
		if err != nil {
			if os.IsNotExist(err) {
				return "", "", fmt.Errorf("resolve skill %q: %w", name, ErrSkillNotFound)
			}
			return "", "", fmt.Errorf("stat skill %q: %w", name, err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return "", "", fmt.Errorf("resolve skill %q: %w", name, ErrSkillNotFound)
		}
		return entryName, directory, nil
	}
	return "", "", fmt.Errorf("resolve skill %q: %w", name, ErrSkillNotFound)
}

func (l *Loader) skillsRoot() (string, error) {
	workspaceRoot := strings.TrimSpace(l.workspaceRoot)
	if workspaceRoot == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		workspaceRoot = cwd
	}
	return filepath.Join(workspaceRoot, skillsDirectoryName), nil
}

func normalizeSkillName(name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" || trimmed == "." || trimmed == ".." || strings.ContainsAny(trimmed, `/\\`) {
		return "", fmt.Errorf("%w: %q", ErrInvalidSkillName, name)
	}
	return trimmed, nil
}
