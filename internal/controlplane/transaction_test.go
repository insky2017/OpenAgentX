package controlplane_test

import (
	"agentbus/internal/controlplane"
	openagentsqlite "agentbus/internal/persistence/sqlite"
)

var _ controlplane.TransactionalState = (*openagentsqlite.Repository)(nil)
