package pipeline

import (
	"context"

	"github.com/bigmoon-dev/aegis/internal/model"
)

// Stage processes a request and returns a verdict.
type Stage interface {
	Name() string
	Process(ctx context.Context, req *model.PipelineRequest) (*model.StageResult, error)
}
