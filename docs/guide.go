// Package docs embeds the agent transcription guide.
package docs

import _ "embed"

// Transcription is the tool contract plus the art directions, in that order.
//
//go:embed TRANSCRIPTION.md
var transcription string

//go:embed AGENT_DIRECTIONS.md
var artDirections string

//go:embed QUICKSTART.md
var Quickstart string

// Transcription is what GET /v1/guide returns.
var Transcription = transcription + "\n\n" + artDirections
