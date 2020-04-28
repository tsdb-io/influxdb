package inmem

import (
	"context"
	"testing"

	platform "github.com/influxdata/influxdb/v2"
	platformtesting "github.com/influxdata/influxdb/v2/testing"
)

func initDBRPMappingService(f platformtesting.DBRPMappingFields, t *testing.T) (platform.DBRPMappingService, func()) {
	s := NewService()
	ctx := context.TODO()
	if err := f.Populate(ctx, s); err != nil {
		t.Fatal(err)
	}
	return s, func() {}
}

func TestDBRPMappingService_CreateDBRPMapping(t *testing.T) {
	t.Skip("not needed anymore because we are moving to a new implementation of dbrp")
	t.Parallel()
	platformtesting.CreateDBRPMapping(initDBRPMappingService, t)
}

func TestDBRPMappingService_FindDBRPMappingByKey(t *testing.T) {
	t.Skip("not needed anymore because we are moving to a new implementation of dbrp")
	t.Parallel()
	platformtesting.FindDBRPMappingByKey(initDBRPMappingService, t)
}

func TestDBRPMappingService_FindDBRPMappings(t *testing.T) {
	t.Skip("not needed anymore because we are moving to a new implementation of dbrp")
	t.Parallel()
	platformtesting.FindDBRPMappings(initDBRPMappingService, t)
}

func TestDBRPMappingService_DeleteDBRPMapping(t *testing.T) {
	t.Skip("not needed anymore because we are moving to a new implementation of dbrp")
	t.Parallel()
	platformtesting.DeleteDBRPMapping(initDBRPMappingService, t)
}

func TestDBRPMappingService_FindDBRPMapping(t *testing.T) {
	t.Skip("not needed anymore because we are moving to a new implementation of dbrp")
	t.Parallel()
	platformtesting.FindDBRPMapping(initDBRPMappingService, t)
}
