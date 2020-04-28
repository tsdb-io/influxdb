package token

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi"
	"github.com/go-chi/chi/middleware"
	"github.com/influxdata/influxdb/v2"
	kithttp "github.com/influxdata/influxdb/v2/kit/transport/http"
	"go.uber.org/zap"
)

type AuthHandler struct {
	chi.Router
	api           *kithttp.API
	log           *zap.Logger
	authSvc       influxdb.AuthorizationService
	lookupService influxdb.LookupService
	tenantService influxdb.TenantService
}

// NewHTTPAuthHandler constructs a new http server.
func NewHTTPAuthHandler(log *zap.Logger, authService influxdb.AuthorizationService, tenantService influxdb.TenantService, lookupService influxdb.LookupService) *AuthHandler {
	h := &AuthHandler{
		api:           kithttp.NewAPI(kithttp.WithLog(log)),
		log:           log,
		authSvc:       authService,
		tenantService: tenantService,
		lookupService: lookupService,
	}

	r := chi.NewRouter()
	r.Use(
		middleware.Recoverer,
		middleware.RequestID,
		middleware.RealIP,
	)

	r.Route("/", func(r chi.Router) {
		r.Post("/", h.handlePostAuthorization)
		r.Get("/", h.handleGetAuthorizations)

		r.Route("/{id}", func(r chi.Router) {
			r.Get("/", h.handleGetAuthorization)
			r.Patch("/", h.handleUpdateAuthorization)
			r.Delete("/", h.handleDeleteAuthorization)
		})
	})

	h.Router = r
	return h
}

const prefixAuthorization = "/api/v2/authorizations"

func (h *AuthHandler) Prefix() string {
	return prefixAuthorization
}

// handlePostAuthorization is the HTTP handler for the POST /api/v2/authorizations route.
func (h *AuthHandler) handlePostAuthorization(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	a, err := decodePostAuthorizationRequest(ctx, r)
	if err != nil {
		h.api.Err(w, err)
		return
	}
	// We can assume we have a User ID because if the request does not provide one, then the authorizer
	// middleware gets it from the context
	auth := a.toInfluxdb(*a.UserID)

	if err := h.authSvc.CreateAuthorization(ctx, auth); err != nil {
		h.api.Err(w, err)
		return
	}

	perms, err := newPermissionsResponse(ctx, auth.Permissions, h.lookupService)
	if err != nil {
		h.api.Err(w, err)
		return
	}

	h.log.Debug("Auth created ", zap.String("auth", fmt.Sprint(auth)))

	resp, err := h.newAuthResponse(ctx, auth, perms)
	if err != nil {
		h.api.Err(w, influxdb.ErrUnableToCreateToken)
	}

	h.api.Respond(w, http.StatusCreated, resp)
}

type postAuthorizationRequest struct {
	Status      influxdb.Status       `json:"status"`
	OrgID       influxdb.ID           `json:"orgID"`
	UserID      *influxdb.ID          `json:"userID,omitempty"`
	Description string                `json:"description"`
	Permissions []influxdb.Permission `json:"permissions"`
}

type authResponse struct {
	ID          influxdb.ID          `json:"id"`
	Token       string               `json:"token"`
	Status      influxdb.Status      `json:"status"`
	Description string               `json:"description"`
	OrgID       influxdb.ID          `json:"orgID"`
	Org         string               `json:"org"`
	UserID      influxdb.ID          `json:"userID"`
	User        string               `json:"user"`
	Permissions []permissionResponse `json:"permissions"`
	Links       map[string]string    `json:"links"`
	CreatedAt   time.Time            `json:"createdAt"`
	UpdatedAt   time.Time            `json:"updatedAt"`
}

// In the future, we would like only the service layer to look up the user and org to see if they are valid
// but for now we need to look up the User and Org here because the API expects the response
// to have the names of the Org and User
func (h *AuthHandler) newAuthResponse(ctx context.Context, a *influxdb.Authorization, ps []permissionResponse) (*authResponse, error) {
	org, err := h.tenantService.FindOrganizationByID(ctx, a.OrgID)
	if err != nil {
		h.log.Info("Failed to get org", zap.String("handler", "getAuthorizations"), zap.String("orgID", a.OrgID.String()), zap.Error(err))
		return nil, err
	}
	user, err := h.tenantService.FindUserByID(ctx, a.UserID)
	if err != nil {
		h.log.Info("Failed to get user", zap.String("userID", a.UserID.String()), zap.Error(err))
		return nil, err
	}
	res := &authResponse{
		ID:          a.ID,
		Token:       a.Token,
		Status:      a.Status,
		Description: a.Description,
		OrgID:       a.OrgID,
		UserID:      a.UserID,
		User:        user.Name,
		Org:         org.Name,
		Permissions: ps,
		Links: map[string]string{
			"self": fmt.Sprintf("/api/v2/authorizations/%s", a.ID),
			"user": fmt.Sprintf("/api/v2/users/%s", a.UserID),
		},
		CreatedAt: a.CreatedAt,
		UpdatedAt: a.UpdatedAt,
	}
	return res, nil
}

func (p *postAuthorizationRequest) toInfluxdb(userID influxdb.ID) *influxdb.Authorization {
	return &influxdb.Authorization{
		OrgID:       p.OrgID,
		Status:      p.Status,
		Description: p.Description,
		Permissions: p.Permissions,
		UserID:      userID,
	}
}

func (a *authResponse) toInfluxdb() *influxdb.Authorization {
	res := &influxdb.Authorization{
		ID:          a.ID,
		Token:       a.Token,
		Status:      a.Status,
		Description: a.Description,
		OrgID:       a.OrgID,
		UserID:      a.UserID,
		CRUDLog: influxdb.CRUDLog{
			CreatedAt: a.CreatedAt,
			UpdatedAt: a.UpdatedAt,
		},
	}
	for _, p := range a.Permissions {
		res.Permissions = append(res.Permissions, influxdb.Permission{Action: p.Action, Resource: p.Resource.Resource})
	}
	return res
}

type authsResponse struct {
	Links map[string]string `json:"links"`
	Auths []*authResponse   `json:"authorizations"`
}

func newAuthsResponse(as []*authResponse) *authsResponse {
	return &authsResponse{
		// TODO(desa): update links to include paging and filter information
		Links: map[string]string{
			"self": "/api/v2/authorizations",
		},
		Auths: as,
	}
}

func newPostAuthorizationRequest(a *influxdb.Authorization) (*postAuthorizationRequest, error) {
	res := &postAuthorizationRequest{
		OrgID:       a.OrgID,
		Description: a.Description,
		Permissions: a.Permissions,
		Status:      a.Status,
	}

	if a.UserID.Valid() {
		res.UserID = &a.UserID
	}

	res.SetDefaults()

	return res, res.Validate()
}

func (p *postAuthorizationRequest) SetDefaults() {
	if p.Status == "" {
		p.Status = influxdb.Active
	}
}

func (p *postAuthorizationRequest) Validate() error {
	if len(p.Permissions) == 0 {
		return &influxdb.Error{
			Code: influxdb.EInvalid,
			Msg:  "authorization must include permissions",
		}
	}

	for _, perm := range p.Permissions {
		if err := perm.Valid(); err != nil {
			return &influxdb.Error{
				Err: err,
			}
		}
	}

	if !p.OrgID.Valid() {
		return &influxdb.Error{
			Err:  influxdb.ErrInvalidID,
			Code: influxdb.EInvalid,
			Msg:  "org id required",
		}
	}

	if p.Status == "" {
		p.Status = influxdb.Active
	}

	err := p.Status.Valid()
	if err != nil {
		return err
	}

	return nil
}

type permissionResponse struct {
	Action   influxdb.Action  `json:"action"`
	Resource resourceResponse `json:"resource"`
}

type resourceResponse struct {
	influxdb.Resource
	Name         string `json:"name,omitempty"`
	Organization string `json:"org,omitempty"`
}

func newPermissionsResponse(ctx context.Context, ps []influxdb.Permission, svc influxdb.LookupService) ([]permissionResponse, error) {
	res := make([]permissionResponse, len(ps))
	for i, p := range ps {
		res[i] = permissionResponse{
			Action: p.Action,
			Resource: resourceResponse{
				Resource: p.Resource,
			},
		}

		if p.Resource.ID != nil {
			name, err := svc.Name(ctx, p.Resource.Type, *p.Resource.ID)
			if influxdb.ErrorCode(err) == influxdb.ENotFound {
				continue
			}
			if err != nil {
				return nil, err
			}
			res[i].Resource.Name = name
		}

		if p.Resource.OrgID != nil {
			name, err := svc.Name(ctx, influxdb.OrgsResourceType, *p.Resource.OrgID)
			if influxdb.ErrorCode(err) == influxdb.ENotFound {
				continue
			}
			if err != nil {
				return nil, err
			}
			res[i].Resource.Organization = name
		}
	}
	return res, nil
}

func decodePostAuthorizationRequest(ctx context.Context, r *http.Request) (*postAuthorizationRequest, error) {
	a := &postAuthorizationRequest{}
	if err := json.NewDecoder(r.Body).Decode(a); err != nil {
		return nil, &influxdb.Error{
			Code: influxdb.EInvalid,
			Msg:  "invalid json structure",
			Err:  err,
		}
	}

	a.SetDefaults()

	return a, a.Validate()
}

// handleGetAuthorizations is the HTTP handler for the GET /api/v2/authorizations route.
func (h *AuthHandler) handleGetAuthorizations(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	req, err := decodeGetAuthorizationsRequest(ctx, r)
	if err != nil {
		h.log.Info("Failed to decode request", zap.String("handler", "getAuthorizations"), zap.Error(err))
		h.api.Err(w, err)
		return
	}

	opts := influxdb.FindOptions{}
	as, _, err := h.authSvc.FindAuthorizations(ctx, req.filter, opts)

	if err != nil {
		h.api.Err(w, err)
		return
	}

	f := req.filter
	// If the user or org name was provided, look up the ID first
	if f.User != nil {
		u, err := h.tenantService.FindUser(ctx, influxdb.UserFilter{Name: f.User})
		if err != nil {
			h.api.Err(w, err)
			return
		}
		f.UserID = &u.ID
	}

	if f.Org != nil {
		o, err := h.tenantService.FindOrganization(ctx, influxdb.OrganizationFilter{Name: f.Org})
		if err != nil {
			h.api.Err(w, err)
			return
		}
		f.OrgID = &o.ID
	}

	auths := make([]*authResponse, 0, len(as))
	for _, a := range as {
		ps, err := newPermissionsResponse(ctx, a.Permissions, h.lookupService)
		if err != nil {
			h.api.Err(w, err)
			return
		}

		resp, err := h.newAuthResponse(ctx, a, ps)
		if err != nil {
			h.log.Info("Failed to create auth response", zap.String("handler", "getAuthorizations"))
			continue
		}
		auths = append(auths, resp)
	}

	h.log.Debug("Auths retrieved ", zap.String("auths", fmt.Sprint(auths)))

	h.api.Respond(w, http.StatusOK, newAuthsResponse(auths))
}

type getAuthorizationsRequest struct {
	filter influxdb.AuthorizationFilter
}

func decodeGetAuthorizationsRequest(ctx context.Context, r *http.Request) (*getAuthorizationsRequest, error) {
	qp := r.URL.Query()

	req := &getAuthorizationsRequest{}

	userID := qp.Get("userID")
	if userID != "" {
		id, err := influxdb.IDFromString(userID)
		if err != nil {
			return nil, err
		}
		req.filter.UserID = id
	}

	user := qp.Get("user")
	if user != "" {
		req.filter.User = &user
	}

	orgID := qp.Get("orgID")
	if orgID != "" {
		id, err := influxdb.IDFromString(orgID)
		if err != nil {
			return nil, err
		}
		req.filter.OrgID = id
	}

	org := qp.Get("org")
	if org != "" {
		req.filter.Org = &org
	}

	authID := qp.Get("id")
	if authID != "" {
		id, err := influxdb.IDFromString(authID)
		if err != nil {
			return nil, err
		}
		req.filter.ID = id
	}

	return req, nil
}

func (h *AuthHandler) handleGetAuthorization(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	id, err := influxdb.IDFromString(chi.URLParam(r, "id"))
	if err != nil {
		h.log.Info("Failed to decode request", zap.String("handler", "getAuthorization"), zap.Error(err))
		h.api.Err(w, err)
		return
	}

	a, err := h.authSvc.FindAuthorizationByID(ctx, *id)
	if err != nil {
		// Don't log here, it should already be handled by the service
		h.api.Err(w, err)
		return
	}

	ps, err := newPermissionsResponse(ctx, a.Permissions, h.lookupService)
	if err != nil {
		h.api.Err(w, err)
		return
	}

	h.log.Debug("Auth retrieved ", zap.String("auth", fmt.Sprint(a)))

	resp, err := h.newAuthResponse(ctx, a, ps)
	if err != nil {
		h.api.Err(w, err)
		return
	}

	h.api.Respond(w, http.StatusOK, resp)
}

// type getAuthorizationRequest struct {
// 	ID influxdb.ID
// }

// func decodeGetAuthorizationRequest(ctx context.Context, r *http.Request) (*getAuthorizationRequest, error) {
// 	params := httprouter.ParamsFromContext(ctx)
// 	id := params.ByName("id")
// 	if id == "" {
// 		return nil, &influxdb.Error{
// 			Code: influxdb.EInvalid,
// 			Msg:  "url missing id",
// 		}
// 	}

// 	var i influxdb.ID
// 	if err := i.DecodeFromString(id); err != nil {
// 		return nil, err
// 	}

// 	return &getAuthorizationRequest{
// 		ID: i,
// 	}, nil
// }

// handleUpdateAuthorization is the HTTP handler for the PATCH /api/v2/authorizations/:id route that updates the authorization's status and desc.
func (h *AuthHandler) handleUpdateAuthorization(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	req, err := decodeUpdateAuthorizationRequest(ctx, r)
	if err != nil {
		h.log.Info("Failed to decode request", zap.String("handler", "updateAuthorization"), zap.Error(err))
		h.api.Err(w, err)
		return
	}

	a, err := h.authSvc.FindAuthorizationByID(ctx, req.ID)
	if err != nil {
		h.api.Err(w, err)
		return
	}

	a, err = h.authSvc.UpdateAuthorization(ctx, a.ID, req.AuthorizationUpdate)
	if err != nil {
		h.api.Err(w, err)
		return
	}

	ps, err := newPermissionsResponse(ctx, a.Permissions, h.lookupService)
	if err != nil {
		h.api.Err(w, err)
		return
	}
	h.log.Debug("Auth updated", zap.String("auth", fmt.Sprint(a)))

	resp, err := h.newAuthResponse(ctx, a, ps)
	if err != nil {
		h.api.Err(w, err)
		return
	}

	h.api.Respond(w, http.StatusOK, resp)
}

type updateAuthorizationRequest struct {
	ID influxdb.ID
	*influxdb.AuthorizationUpdate
}

func decodeUpdateAuthorizationRequest(ctx context.Context, r *http.Request) (*updateAuthorizationRequest, error) {
	id, err := influxdb.IDFromString(chi.URLParam(r, "id"))
	if err != nil {
		return nil, err
	}

	upd := &influxdb.AuthorizationUpdate{}
	if err := json.NewDecoder(r.Body).Decode(upd); err != nil {
		return nil, err
	}

	return &updateAuthorizationRequest{
		ID:                  *id,
		AuthorizationUpdate: upd,
	}, nil
}

// handleDeleteAuthorization is the HTTP handler for the DELETE /api/v2/authorizations/:id route.
func (h *AuthHandler) handleDeleteAuthorization(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	req, err := decodeDeleteAuthorizationRequest(ctx, r)
	if err != nil {
		h.log.Info("Failed to decode request", zap.String("handler", "deleteAuthorization"), zap.Error(err))
		h.api.Err(w, err)
		return
	}

	if err := h.authSvc.DeleteAuthorization(ctx, req.ID); err != nil {
		// Don't log here, it should already be handled by the service
		h.api.Err(w, err)
		return
	}

	h.log.Debug("Auth deleted", zap.String("authID", fmt.Sprint(req.ID)))

	w.WriteHeader(http.StatusNoContent)
}

type deleteAuthorizationRequest struct {
	ID influxdb.ID
}

// we can clean up and remove these decode functions todo (al)
func decodeDeleteAuthorizationRequest(ctx context.Context, r *http.Request) (*deleteAuthorizationRequest, error) {
	id, err := influxdb.IDFromString(chi.URLParam(r, "id"))
	if err != nil {
		return nil, err
	}

	return &deleteAuthorizationRequest{
		ID: *id,
	}, nil
}
