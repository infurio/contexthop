package main

import (
	"github.com/infurio/contexthop/internal/catalog"
	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/destination"
	"github.com/infurio/contexthop/internal/ui"
)

func entityTags(cfg config.Config, ref catalog.Ref) []ui.Tag {
	metadata, _ := destination.Describe(cfg, destination.Target{Kind: string(ref.Kind), Name: ref.Name})
	tags := []ui.Tag{}
	for _, tag := range metadata.Tags {
		tags = append(tags, ui.Tag{Name: tag.Name, Color: tag.Color})
	}
	return tags
}
