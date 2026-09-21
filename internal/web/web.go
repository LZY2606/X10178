// Package web embeds the browser UI assets.
package web

import "embed"

//go:embed static/*
var Static embed.FS
