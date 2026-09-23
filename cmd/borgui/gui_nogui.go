//go:build nogui

package main

import (
	"fmt"
	"os"
)

// runGUI signale une compilation sans interface graphique, faite pour
// produire la ligne de commande seule sans CGO — pour Windows depuis Linux,
// notamment.
func (a *app) runGUI() int {
	fmt.Fprintln(os.Stderr, a.T("cli.no_gui"))
	fmt.Fprintln(os.Stderr, a.T("cli.usage"))
	return exitError
}
