package app

import (
	"context"
	"testing"

	"github.com/j4v3l/SkyFeed/internal/config"
	"github.com/j4v3l/SkyFeed/internal/storage/sqlite"
)

func TestBootstrapRoleBindingsFromConfig(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(ctx, t.TempDir()+"/skyfeed.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.EnsureGuild(ctx, 42); err != nil {
		t.Fatal(err)
	}
	bootstrapRoleBindings(ctx, store, config.Discord{
		GuildID:         42,
		AdminRoleID:     100,
		OperatorRoleID:  200,
		ModeratorRoleID: 300,
	}, nil)
	bindings, err := store.RoleBindings(ctx, 42)
	if err != nil {
		t.Fatal(err)
	}
	if len(bindings) != 3 {
		t.Fatalf("bindings=%+v", bindings)
	}
}
