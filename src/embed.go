package ritchie

import "embed"

// The RLR package sources and the sources of the monolith target, which
// every generated build tree carries.
//
//go:embed rlr/runtime.go rlr/numeric.go rlr/sequences.go rlr/arrays.go rlr/strings.go rlr/option.go rlr/box.go rlr/dicts.go rlr/errors.go rlr/display.go rlr/decimal.go rlr/decimalorder.go rlr/decimalconvert.go rlr/keyed.go rlr/temporal.go rlr/temporaltext.go rlr/temporalorder.go rlr/effects.go rlr/units.go rlr/schedule.go rlr/ahead.go rlr/json.go rlr/externaltext.go rlr/mesh/mesh.go rlr/mesh/classify.go rlr/mesh/exposure.go monolith/monolith.go
var RLRSources embed.FS
