package token

import (
	"github.com/influxdata/influxdb/v2"
	"github.com/influxdata/influxdb/v2/rand"
)

type Service struct {
	store          *Store
	tokenGenerator influxdb.TokenGenerator
	tenantService  influxdb.TenantService
}

func NewService(st *Store, ts influxdb.TenantService) influxdb.AuthorizationService {
	return &Service{
		store:          st,
		tokenGenerator: rand.NewTokenGenerator(64),
		tenantService:  ts,
	}
}
