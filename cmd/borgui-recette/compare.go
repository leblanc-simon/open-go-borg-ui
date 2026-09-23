package main

import (
	"flag"
	"fmt"
	"strings"
)

// maxReported borne le nombre d'écarts détaillés : au-delà, la liste n'aide
// plus à comprendre et noie le récapitulatif.
const maxReported = 50

// commandCompare confronte un dossier restauré au manifeste des originaux.
func commandCompare(t translate, args []string) (int, error) {
	var ignored pathList
	flags := flag.NewFlagSet("compare", flag.ContinueOnError)
	flags.Var(&ignored, "ignore", "chemin relatif à écarter de la comparaison, répétable")
	positional, err := parse(flags, args)
	if err != nil || len(positional) != 2 {
		return exitError, usageError{}
	}

	expected, err := readManifest(t, positional[0])
	if err != nil {
		return exitError, err
	}
	actual, err := scan(t, positional[1])
	if err != nil {
		return exitError, err
	}

	expected, actual = ignored.filter(expected), ignored.filter(actual)
	if len(ignored) > 0 {
		fmt.Println(t("recette.ignored", map[string]any{"Paths": strings.Join(ignored, ", ")}))
	}

	report := compare(expected, actual)
	reported := 0
	show := func(key string, e entry) {
		if reported < maxReported {
			fmt.Println(t(key, map[string]any{"Path": e.path, "Kind": kindLabel(t, e)}))
		}
		reported++
	}
	for _, e := range report.missing {
		show("recette.missing", e)
	}
	for _, e := range report.different {
		show("recette.different", e)
	}
	for _, e := range report.extra {
		show("recette.extra", e)
	}
	if reported > maxReported {
		fmt.Println(t("recette.more", map[string]any{"Count": reported - maxReported}))
	}

	fmt.Println(t("recette.summary", map[string]any{
		"Identical": report.identical,
		"Missing":   len(report.missing),
		"Different": len(report.different),
		"Extra":     len(report.extra),
	}))
	if !report.ok() {
		fmt.Println(t("recette.failed"))
		return exitDifference, nil
	}
	fmt.Println(t("recette.passed"))
	return exitSuccess, nil
}

// comparison est le résultat d'une confrontation.
type comparison struct {
	identical int
	// missing sont attendus mais absents ; different sont présents avec un
	// autre type, une autre taille ou une autre empreinte ; extra sont
	// présents sans être attendus.
	missing, different, extra []entry
}

// ok indique une restauration fidèle.
func (c comparison) ok() bool {
	return len(c.missing) == 0 && len(c.different) == 0 && len(c.extra) == 0
}

// compare confronte deux listes d'entrées.
//
// Les chemins sont comparés octet pour octet : un nom restauré sous une autre
// forme Unicode que l'original est un écart, pas une équivalence.
func compare(expected, actual []entry) comparison {
	var result comparison
	found := make(map[string]entry, len(actual))
	for _, e := range actual {
		found[e.path] = e
	}
	for _, want := range expected {
		got, ok := found[want.path]
		if !ok {
			result.missing = append(result.missing, want)
			continue
		}
		delete(found, want.path)
		if got.kind != want.kind || got.digest != want.digest || (want.kind == "f" && got.size != want.size) {
			result.different = append(result.different, want)
			continue
		}
		result.identical++
	}
	for _, e := range actual {
		if _, left := found[e.path]; left {
			result.extra = append(result.extra, e)
		}
	}
	return result
}

// kindLabel décrit la nature d'un élément. La cible d'un lien est montrée :
// c'est ce que la recette demande d'observer pour les jonctions Windows.
func kindLabel(t translate, e entry) string {
	switch e.kind {
	case "d":
		return t("recette.kind_dir")
	case "l":
		return t("recette.kind_link", map[string]any{"Target": e.digest})
	default:
		return t("recette.kind_file")
	}
}

// pathList est une option répétable de chemins relatifs.
type pathList []string

func (p *pathList) String() string { return strings.Join(*p, ",") }

func (p *pathList) Set(value string) error {
	*p = append(*p, strings.Trim(strings.ReplaceAll(value, `\`, "/"), "/"))
	return nil
}

// filter retire les entrées désignées, et tout ce qu'elles contiennent.
func (p pathList) filter(entries []entry) []entry {
	if len(p) == 0 {
		return entries
	}
	kept := entries[:0:0]
	for _, e := range entries {
		skip := false
		for _, ignored := range p {
			if e.path == ignored || strings.HasPrefix(e.path, ignored+"/") {
				skip = true
				break
			}
		}
		if !skip {
			kept = append(kept, e)
		}
	}
	return kept
}
