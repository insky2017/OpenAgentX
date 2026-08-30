package controlplane_test

import (
	"openagentx/internal/controlplane"
	openagentsqlite "openagentx/internal/persistence/sqlite"
)

var _ controlplane.TransactionalState = (*openagentsqlite.Repository)(nil)
