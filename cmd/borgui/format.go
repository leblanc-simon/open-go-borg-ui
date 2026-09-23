package main

import (
	"time"

	"leblanc.io/open-go-borg-ui/internal/format"
)

// formatSize rend une taille lisible.
func formatSize(bytes int64) string { return format.Size(bytes) }

// formatDuration rend une durée dans la langue courante.
func (a *app) formatDuration(d time.Duration) string { return format.Duration(a.T, d) }

// formatRate rend un débit moyen, et false si la durée est trop courte.
func formatRate(bytes int64, d time.Duration) (string, bool) { return format.Rate(bytes, d) }
