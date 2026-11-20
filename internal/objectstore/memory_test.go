package objectstore_test

import (
	"testing"

	"github.com/worldtree-industries/mimir/internal/objectstore"
	"github.com/worldtree-industries/mimir/internal/objectstore/objectstoretest"
)

func TestMemoryMeetsContract(t *testing.T) {
	objectstoretest.Run(t, objectstore.NewMemory(), "mimir-dev")
}
