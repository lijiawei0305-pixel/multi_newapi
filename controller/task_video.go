package controller

import (
	"context"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
)

// UpdateVideoTaskAll is kept for compatibility with older scheduler callers.
// The service polling implementation is the single owner of terminal CAS and
// durable billing adjustment semantics.
func UpdateVideoTaskAll(ctx context.Context, platform constant.TaskPlatform, taskChannelM map[int][]string, taskM map[string]*model.Task) error {
	return service.UpdateVideoTasks(ctx, platform, taskChannelM, taskM)
}
