package skills

import (
	"context"
	"fmt"
)

type ResolveInput struct {
	Names []string
}

type Resolver struct {
	loader *Loader
}

func NewResolver(loader *Loader) *Resolver {
	return &Resolver{loader: loader}
}

func (r *Resolver) Resolve(ctx context.Context, input ResolveInput) ([]ResolvedSkill, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r == nil || r.loader == nil {
		return nil, fmt.Errorf("skill loader is required")
	}

	names := NormalizeNames(input.Names)
	resolved := make([]ResolvedSkill, 0, len(names))
	for _, name := range names {
		skill, err := r.loader.Get(ctx, name)
		if err != nil {
			return nil, err
		}
		resolved = append(resolved, ResolvedSkill{
			Name:        skill.Name,
			Description: skill.Description,
			SourceRef:   skill.SourceRef,
			RuntimeOnly: true,
		})
	}
	return resolved, nil
}
