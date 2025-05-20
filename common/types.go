package common

import (
	"fmt"
)

var _ = fmt.Print

type WindowRegion struct {
	X, Y, Width, Height int
	Label               string
}
