// Package builtin blank-imports all built-in types so they register
// themselves in init().
package builtin

import (
	_ "github.com/baba/sandwichcake/pkg/types/binary"
	_ "github.com/baba/sandwichcake/pkg/types/other"
	_ "github.com/baba/sandwichcake/pkg/types/patch"
	_ "github.com/baba/sandwichcake/pkg/types/venv"
	_ "github.com/baba/sandwichcake/pkg/types/weight"
)
