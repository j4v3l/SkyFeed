package discord

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"

	disgocord "github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/rest"
	"github.com/disgoorg/snowflake/v2"
)

type RegistrationStats struct {
	Created int
	Updated int
	Deleted int
	Kept    int
	Ignored int
}

func SyncGuildCommands(ctx context.Context, api rest.Applications, applicationID, guildID snowflake.ID) (RegistrationStats, error) {
	return syncCommands(commandAPI{
		scope: "guild",
		list: func() ([]disgocord.ApplicationCommand, error) {
			return api.GetGuildCommands(applicationID, guildID, false, rest.WithCtx(ctx))
		},
		create: func(command disgocord.ApplicationCommandCreate) (disgocord.ApplicationCommand, error) {
			return api.CreateGuildCommand(applicationID, guildID, command, rest.WithCtx(ctx))
		},
		update: func(id snowflake.ID, command disgocord.ApplicationCommandUpdate) (disgocord.ApplicationCommand, error) {
			return api.UpdateGuildCommand(applicationID, guildID, id, command, rest.WithCtx(ctx))
		},
		delete: func(id snowflake.ID) error {
			return api.DeleteGuildCommand(applicationID, guildID, id, rest.WithCtx(ctx))
		},
	})
}

func SyncGlobalCommands(ctx context.Context, api rest.Applications, applicationID snowflake.ID) (RegistrationStats, error) {
	return syncCommands(commandAPI{
		scope: "global",
		list: func() ([]disgocord.ApplicationCommand, error) {
			return api.GetGlobalCommands(applicationID, false, rest.WithCtx(ctx))
		},
		create: func(command disgocord.ApplicationCommandCreate) (disgocord.ApplicationCommand, error) {
			return api.CreateGlobalCommand(applicationID, command, rest.WithCtx(ctx))
		},
		update: func(id snowflake.ID, command disgocord.ApplicationCommandUpdate) (disgocord.ApplicationCommand, error) {
			return api.UpdateGlobalCommand(applicationID, id, command, rest.WithCtx(ctx))
		},
		delete: func(id snowflake.ID) error {
			return api.DeleteGlobalCommand(applicationID, id, rest.WithCtx(ctx))
		},
	})
}

type commandAPI struct {
	scope  string
	list   func() ([]disgocord.ApplicationCommand, error)
	create func(disgocord.ApplicationCommandCreate) (disgocord.ApplicationCommand, error)
	update func(snowflake.ID, disgocord.ApplicationCommandUpdate) (disgocord.ApplicationCommand, error)
	delete func(snowflake.ID) error
}

func syncCommands(api commandAPI) (RegistrationStats, error) {
	desired := DesiredCommands()
	if err := validateDesiredCommands(desired); err != nil {
		return RegistrationStats{}, err
	}
	existing, err := api.list()
	if err != nil {
		return RegistrationStats{}, fmt.Errorf("list %s commands: %w", api.scope, err)
	}
	byName := make(map[string]disgocord.SlashCommandCreate, len(desired))
	for _, command := range desired {
		byName[command.CommandName()] = command.(disgocord.SlashCommandCreate)
	}

	stats := RegistrationStats{}
	seen := make(map[string]struct{}, len(existing))
	for _, remote := range existing {
		wanted, owned := byName[remote.Name()]
		if !owned {
			if OwnedCommand(remote.Name()) {
				if err := api.delete(remote.ID()); err != nil {
					return stats, fmt.Errorf("delete owned command %q: %w", remote.Name(), err)
				}
				stats.Deleted++
			} else {
				stats.Ignored++
			}
			continue
		}
		seen[remote.Name()] = struct{}{}
		if commandEquivalent(remote, wanted) {
			stats.Kept++
			continue
		}
		update := slashUpdate(wanted)
		if _, err := api.update(remote.ID(), update); err != nil {
			return stats, fmt.Errorf("update command %q: %w", remote.Name(), err)
		}
		stats.Updated++
	}
	for _, command := range desired {
		if _, exists := seen[command.CommandName()]; exists {
			continue
		}
		if _, err := api.create(command); err != nil {
			return stats, fmt.Errorf("create command %q: %w", command.CommandName(), err)
		}
		stats.Created++
	}
	return stats, nil
}

func commandEquivalent(remote disgocord.ApplicationCommand, desired disgocord.SlashCommandCreate) bool {
	slash, ok := remote.(disgocord.SlashCommand)
	if !ok {
		return false
	}
	permissions := disgocord.Permissions(0)
	permissionsSet := desired.DefaultMemberPermissions.OK && desired.DefaultMemberPermissions.Value != nil
	if permissionsSet {
		permissions = *desired.DefaultMemberPermissions.Value
	}
	return slash.Name() == desired.Name &&
		slash.Description == desired.Description &&
		reflect.DeepEqual(slash.Options, desired.Options) &&
		(!permissionsSet || slash.DefaultMemberPermissions() == permissions)
}

func slashUpdate(command disgocord.SlashCommandCreate) disgocord.SlashCommandUpdate {
	name, description, options := command.Name, command.Description, command.Options
	return disgocord.SlashCommandUpdate{
		Name:                     &name,
		Description:              &description,
		Options:                  &options,
		DefaultMemberPermissions: command.DefaultMemberPermissions,
		IntegrationTypes:         slicePointer(command.IntegrationTypes),
		Contexts:                 slicePointer(command.Contexts),
		NSFW:                     command.NSFW,
	}
}

func slicePointer[T any](values []T) *[]T {
	if values == nil {
		return nil
	}
	return &values
}

// MarshalCommandSchema is used by diagnostics and tests to make schema drift
// visible without issuing Discord requests.
func MarshalCommandSchema() ([]byte, error) {
	return json.MarshalIndent(DesiredCommands(), "", "  ")
}
