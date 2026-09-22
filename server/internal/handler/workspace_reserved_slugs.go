package handler

import (
	_ "embed"
	"encoding/json"
)

//go:embed reserved_slugs.json
var reservedSlugsJSON []byte

var reservedSlugs = loadReservedSlugs()

type reservedSlugsFile struct {
	Groups []struct {
		Slugs []string `json:"slugs"`
	} `json:"groups"`
}

func loadReservedSlugs() map[string]bool {
	var data reservedSlugsFile
	if err := json.Unmarshal(reservedSlugsJSON, &data); err != nil {

		panic("handler: parse reserved_slugs.json: " + err.Error())
	}
	out := make(map[string]bool)
	for _, g := range data.Groups {
		for _, slug := range g.Slugs {
			out[slug] = true
		}
	}
	return out
}

func isReservedSlug(slug string) bool {
	return reservedSlugs[slug]
}
