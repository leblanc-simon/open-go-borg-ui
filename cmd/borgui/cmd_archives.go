package main

import (
	"context"
	"fmt"

	"leblanc.io/open-go-borg-ui/internal/borg"
)

// commandArchives énumère les sauvegardes disponibles.
func (a *app) commandArchives(ctx context.Context, _ []string) (int, error) {
	profile, err := a.profile()
	if err != nil {
		return exitError, err
	}
	if err := a.ensurePassphrase(profile); err != nil {
		return exitError, err
	}

	runner, err := a.runner()
	if err != nil {
		return a.reportMissingEngine(err)
	}
	env, err := a.environment(profile)
	if err != nil {
		return exitError, err
	}

	list, _, err := borg.List(ctx, runner, env)
	if err != nil {
		return exitError, err
	}
	if len(list.Archives) == 0 {
		fmt.Println(a.T("archives.empty"))
		return exitSuccess, nil
	}

	for _, archive := range list.Archives {
		fmt.Println(a.T("archives.entry", map[string]any{
			"Date": archive.Start.Local().Format("2006-01-02 15:04"),
			"Name": archive.Name,
		}))
	}
	fmt.Println(a.T("archives.count", map[string]any{"Count": len(list.Archives)}))
	return exitSuccess, nil
}
