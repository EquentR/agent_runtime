package builtin

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"time"

	coreskills "github.com/EquentR/agent_runtime/core/skills"
	coretools "github.com/EquentR/agent_runtime/core/tools"
	"github.com/EquentR/agent_runtime/core/types"
)

func newUsingSkillsTool(env runtimeEnv) coretools.Tool {
	return coretools.Tool{
		Name:        "using_skills",
		Description: "Load the full content and visible resources for a selected workspace skill",
		Source:      "builtin",
		Parameters: objectSchema([]string{"name"}, map[string]types.SchemaProperty{
			"name": {Type: "string", Description: "Exact selected skill name"},
		}),
		ResultHandler: func(ctx context.Context, arguments map[string]any) (coretools.Result, error) {
			name, err := requiredStringArg(arguments, "name")
			if err != nil {
				return coretools.Result{}, err
			}
			startedAt := time.Now()
			logToolStart(ctx, "using_skills")
			loader := coreskills.NewLoader(env.workspaceRoot)
			skill, err := loader.Get(ctx, name)
			if err != nil {
				logToolFailure(ctx, "using_skills", err)
				return coretools.Result{}, err
			}
			resourceRefs, err := listVisibleSkillResourceRefs(skill.Directory)
			if err != nil {
				logToolFailure(ctx, "using_skills", err)
				return coretools.Result{}, err
			}
			content, err := jsonResult(struct {
				Name         string   `json:"name"`
				Description  string   `json:"description"`
				SourceRef    string   `json:"source_ref"`
				Directory    string   `json:"directory"`
				ResourceRefs []string `json:"resource_refs"`
				Content      string   `json:"content"`
			}{
				Name:         skill.Name,
				Description:  skill.Description,
				SourceRef:    skill.SourceRef,
				Directory:    skill.Directory,
				ResourceRefs: resourceRefs,
				Content:      skill.Content,
			})
			if err != nil {
				logToolFailure(ctx, "using_skills", err)
				return coretools.Result{}, err
			}
			logToolFinish(ctx, "using_skills")
			_ = startedAt
			return coretools.Result{Content: content, Ephemeral: true}, nil
		},
	}
}

func listVisibleSkillResourceRefs(skillDir string) ([]string, error) {
	refs := make([]string, 0, 4)
	err := filepath.WalkDir(skillDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == skillDir {
			return nil
		}
		name := d.Name()
		if strings.HasPrefix(name, ".") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		rel, err := filepath.Rel(skillDir, path)
		if err != nil {
			return fmt.Errorf("resolve skill resource ref: %w", err)
		}
		refs = append(refs, filepath.ToSlash(filepath.Join("skills", filepath.Base(skillDir), rel)))
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(refs)
	return refs, nil
}
