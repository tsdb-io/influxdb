package dbrp_test

import (
	"context"
	"encoding/json"
	"io"
	"io/ioutil"
	"net/http/httptest"
	"strings"
	"testing"

	influxdb "github.com/influxdata/influxdb/v2"
	"github.com/influxdata/influxdb/v2/dbrp"
	"github.com/influxdata/influxdb/v2/inmem"
	"github.com/influxdata/influxdb/v2/mock"
	platformtesting "github.com/influxdata/influxdb/v2/testing"
	"go.uber.org/zap/zaptest"
)

func initBucketHttpService(t *testing.T) (influxdb.DBRPMappingServiceV2, *httptest.Server, func()) {
	t.Helper()
	ctx := context.Background()
	bucketSvc := mock.NewBucketService()

	s := inmem.NewKVStore()
	svc, err := dbrp.NewService(ctx, bucketSvc, s)
	if err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(dbrp.NewHTTPDBRPHandler(zaptest.NewLogger(t), svc))
	return svc, server, func() {
		server.Close()
	}
}

func Test_handlePostDBRP(t *testing.T) {
	table := []struct {
		Name         string
		ExpectedErr  *influxdb.Error
		ExpectedDBRP *influxdb.DBRPMapping
		Input        io.Reader
	}{
		{
			Name: "Create valid dbrp",
			Input: strings.NewReader(`{
	"bucket_id": "5555f7ed2a035555",
	"organization_id": "059af7ed2a034000",
	"database": "mydb",
	"retention_policy": "autogen",
	"default": false
}`),
			ExpectedDBRP: &influxdb.DBRPMapping{
				OrganizationID: platformtesting.MustIDBase16("059af7ed2a034000"),
			},
		},
		{
			Name: "Create with invalid orgID",
			Input: strings.NewReader(`{
	"bucket_id": "5555f7ed2a035555",
	"organization_id": "invalid",
	"database": "mydb",
	"retention_policy": "autogen",
	"default": false
}`),
			ExpectedErr: &influxdb.Error{
				Code: influxdb.EInvalid,
				Msg:  "invalid json structure",
				Err:  influxdb.ErrInvalidID.Err,
			},
		},
	}

	for _, s := range table {
		t.Run(s.Name, func(t *testing.T) {
			if s.ExpectedErr != nil && s.ExpectedDBRP != nil {
				t.Error("one of those has to be set")
			}
			_, server, shutdown := initBucketHttpService(t)
			defer shutdown()
			client := server.Client()

			resp, err := client.Post(server.URL+"/", "application/json", s.Input)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()

			if s.ExpectedErr != nil {
				b, err := ioutil.ReadAll(resp.Body)
				if err != nil {
					t.Fatal(err)
				}

				if !strings.Contains(string(b), s.ExpectedErr.Error()) {
					t.Fatal(string(b))
				}
				return
			}
			dbrp := &influxdb.DBRPMapping{}
			if err := json.NewDecoder(resp.Body).Decode(&dbrp); err != nil {
				t.Fatal(err)
			}

			if !dbrp.ID.Valid() {
				t.Fatalf("expected invalid id, got an invalid one %s", dbrp.ID.String())
			}

			if dbrp.OrganizationID != s.ExpectedDBRP.OrganizationID {
				t.Fatalf("expected orgid %s got %s", s.ExpectedDBRP.OrganizationID, dbrp.OrganizationID)
			}

		})
	}
}
