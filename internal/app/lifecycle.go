package app

import (
	"context"

	"golang.org/x/sync/errgroup"
)

type service func(context.Context) error

func runServices(ctx context.Context, services ...service) error {
	group, groupContext := errgroup.WithContext(ctx)
	group.SetLimit(len(services))
	for _, run := range services {
		run := run
		group.Go(func() error {
			return run(groupContext)
		})
	}
	return group.Wait()
}
