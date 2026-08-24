package discord

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"

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

func PurgeOwnedGuildCommands(ctx context.Context, api rest.Applications, applicationID, guildID snowflake.ID) (RegistrationStats, error) {
	existing, err := api.GetGuildCommands(applicationID, guildID, false, rest.WithCtx(ctx))
	if err != nil {
		return RegistrationStats{}, fmt.Errorf("list guild commands: %w", err)
	}
	stats := RegistrationStats{}
	for _, remote := range existing {
		if !OwnedCommand(remote.Name()) {
			stats.Ignored++
			continue
		}
		if err := api.DeleteGuildCommand(applicationID, guildID, remote.ID(), rest.WithCtx(ctx)); err != nil {
			return stats, fmt.Errorf("delete guild command %q: %w", remote.Name(), err)
		}
		stats.Deleted++
	}
	return stats, nil
}

type commandAPI struct {
	scope  string
	list   func() ([]disgocord.ApplicationCommand, error)
	create func(disgocord.ApplicationCommandCreate) (disgocord.ApplicationCommand, error)
	update func(snowflake.ID, disgocord.ApplicationCommandUpdate) (disgocord.ApplicationCommand, error)
	delete func(snowflake.ID) error
}

func syncCommands(api commandAPI) (RegistrationStats, error) {
	return syncCommandSet(api, DesiredCommands())
}

func syncCommandSet(api commandAPI, desired []disgocord.ApplicationCommandCreate) (RegistrationStats, error) {
	if err := validateDesiredCommands(desired); err != nil {
		return RegistrationStats{}, err
	}
	existing, err := api.list()
	if err != nil {
		return RegistrationStats{}, fmt.Errorf("list %s commands: %w", api.scope, err)
	}
	byName := make(map[string]disgocord.ApplicationCommandCreate, len(desired))
	for _, command := range desired {
		byName[command.CommandName()] = command
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
		if remote.Type() != wanted.Type() {
			if err := api.delete(remote.ID()); err != nil {
				return stats, fmt.Errorf("replace command %q: %w", remote.Name(), err)
			}
			if _, err := api.create(wanted); err != nil {
				return stats, fmt.Errorf("replace command %q: %w", remote.Name(), err)
			}
			stats.Updated++
			continue
		}
		if commandEquivalent(remote, wanted) {
			stats.Kept++
			continue
		}
		update, err := commandUpdate(wanted)
		if err != nil {
			return stats, err
		}
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

func commandEquivalent(remote disgocord.ApplicationCommand, desired disgocord.ApplicationCommandCreate) bool {
	if remote.Type() != desired.Type() {
		return false
	}
	switch typed := desired.(type) {
	case disgocord.SlashCommandCreate:
		slash, ok := remote.(disgocord.SlashCommand)
		if !ok {
			return false
		}
		permissions := disgocord.Permissions(0)
		permissionsSet := typed.DefaultMemberPermissions.OK && typed.DefaultMemberPermissions.Value != nil
		if permissionsSet {
			permissions = *typed.DefaultMemberPermissions.Value
		}
		nsfw := typed.NSFW != nil && *typed.NSFW
		return slash.Name() == typed.Name &&
			slash.Description == typed.Description &&
			reflect.DeepEqual(slash.Options, typed.Options) &&
			slash.DefaultMemberPermissions() == permissions &&
			slices.Equal(slash.IntegrationTypes(), typed.IntegrationTypes) &&
			slices.Equal(slash.Contexts(), typed.Contexts) &&
			slash.NSFW() == nsfw
	case disgocord.MessageCommandCreate:
		command, ok := remote.(disgocord.MessageCommand)
		if !ok {
			return false
		}
		permissions := disgocord.Permissions(0)
		permissionsSet := typed.DefaultMemberPermissions.OK && typed.DefaultMemberPermissions.Value != nil
		if permissionsSet {
			permissions = *typed.DefaultMemberPermissions.Value
		}
		nsfw := typed.NSFW != nil && *typed.NSFW
		return command.Name() == typed.Name &&
			command.DefaultMemberPermissions() == permissions &&
			slices.Equal(command.IntegrationTypes(), typed.IntegrationTypes) &&
			slices.Equal(command.Contexts(), typed.Contexts) &&
			command.NSFW() == nsfw
	default:
		return false
	}
}

func commandUpdate(command disgocord.ApplicationCommandCreate) (disgocord.ApplicationCommandUpdate, error) {
	switch typed := command.(type) {
	case disgocord.SlashCommandCreate:
		return slashUpdate(typed), nil
	case disgocord.MessageCommandCreate:
		return messageCommandUpdate(typed), nil
	default:
		return nil, fmt.Errorf("unsupported command type %T", command)
	}
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

func messageCommandUpdate(command disgocord.MessageCommandCreate) disgocord.MessageCommandUpdate {
	name := command.Name
	return disgocord.MessageCommandUpdate{
		Name:                     &name,
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
