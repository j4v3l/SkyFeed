package app

import (
	"context"
	"log/slog"

	"github.com/j4v3l/SkyFeed/internal/config"
	"github.com/j4v3l/SkyFeed/internal/storage"
)

func bootstrapRoleBindings(ctx context.Context, repository storage.Repository, cfg config.Discord, logger *slog.Logger) {
	if repository == nil || cfg.GuildID == 0 {
		return
	}
	specs := []struct {
		tier   string
		roleID uint64
	}{
		{tier: "admin", roleID: cfg.AdminRoleID},
		{tier: "operator", roleID: cfg.OperatorRoleID},
		{tier: "moderator", roleID: cfg.ModeratorRoleID},
	}
	for _, spec := range specs {
		if spec.roleID == 0 {
			continue
		}
		if err := repository.UpsertRoleBinding(ctx, storage.RoleBinding{GuildID: cfg.GuildID, Tier: spec.tier, RoleID: spec.roleID}); err != nil {
			logger.Warn("role binding bootstrap failed", "component", "app", "event", "role_bootstrap_failure", "tier", spec.tier, "error", err)
		}
	}
}
