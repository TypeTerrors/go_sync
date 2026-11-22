// Package deploy holds the original CLI service for lossless desktop adoption.
package deploy

import _ "embed"

//go:embed gosync.service
var DefaultService string
