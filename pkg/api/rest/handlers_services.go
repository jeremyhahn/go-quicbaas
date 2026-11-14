// Copyright (c) 2026 Jeremy Hahn
// SPDX-License-Identifier: AGPL-3.0-only

package rest

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// The service plane's REST surface (#1687). Deploy and redeploy carry the
// signed manifest, whose verified payload names the service; the other verbs
// address one of the caller tenant's services by name.
//
//	GET    /api/v1/services                 list           services:read
//	POST   /api/v1/services                 deploy         services:deploy
//	PUT    /api/v1/services                 redeploy       services:deploy
//	GET    /api/v1/services/{name}          get            services:read
//	DELETE /api/v1/services/{name}          undeploy       services:deploy
//	POST   /api/v1/services/{name}/start    start          services:control
//	POST   /api/v1/services/{name}/stop     stop           services:control
//	POST   /api/v1/services/{name}/resume   resume         services:control
//	POST   /api/v1/services/{name}/restart  restart        services:control

// HandleServicesList lists the caller tenant's services.
func (s *Server) HandleServicesList(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.servicesService(w, r)
	if !ok {
		return
	}
	resp, err := svc.List(r.Context())
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}
	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleServicesDeploy deploys a signed manifest.
func (s *Server) HandleServicesDeploy(w http.ResponseWriter, r *http.Request) {
	s.handleServicesManifest(w, r, http.StatusCreated, func(svc service.ServicesService) servicesManifestVerb { return svc.Deploy })
}

// HandleServicesRedeploy redeploys a settled service from a newer signed
// manifest.
func (s *Server) HandleServicesRedeploy(w http.ResponseWriter, r *http.Request) {
	s.handleServicesManifest(w, r, http.StatusOK, func(svc service.ServicesService) servicesManifestVerb { return svc.Redeploy })
}

type servicesManifestVerb func(context.Context, *service.DeployServiceRequest) (*service.ServiceRecord, error)

func (s *Server) handleServicesManifest(w http.ResponseWriter, r *http.Request, code int, pick func(service.ServicesService) servicesManifestVerb) {
	svc, ok := s.servicesService(w, r)
	if !ok {
		return
	}
	var req service.DeployServiceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}
	rec, err := pick(svc)(r.Context(), &req)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}
	RespondJSON(w, r, code, rec)
}

// HandleServicesGet reads one of the caller tenant's services.
func (s *Server) HandleServicesGet(w http.ResponseWriter, r *http.Request) {
	s.handleServicesName(w, r, func(svc service.ServicesService) servicesNameVerb { return svc.Get })
}

// HandleServicesUndeploy undeploys a settled service.
func (s *Server) HandleServicesUndeploy(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.servicesService(w, r)
	if !ok {
		return
	}
	if err := svc.Undeploy(r.Context(), &service.ServiceNameRequest{Name: chi.URLParam(r, "name")}); err != nil {
		s.handleServiceError(w, r, err)
		return
	}
	RespondNoContent(w, r)
}

// HandleServicesStart starts a stopped service.
func (s *Server) HandleServicesStart(w http.ResponseWriter, r *http.Request) {
	s.handleServicesName(w, r, func(svc service.ServicesService) servicesNameVerb { return svc.Start })
}

// HandleServicesStop stops a running service.
func (s *Server) HandleServicesStop(w http.ResponseWriter, r *http.Request) {
	s.handleServicesName(w, r, func(svc service.ServicesService) servicesNameVerb { return svc.Stop })
}

// HandleServicesResume resumes a halted service.
func (s *Server) HandleServicesResume(w http.ResponseWriter, r *http.Request) {
	s.handleServicesName(w, r, func(svc service.ServicesService) servicesNameVerb { return svc.Resume })
}

// HandleServicesRestart restarts a running or halted service.
func (s *Server) HandleServicesRestart(w http.ResponseWriter, r *http.Request) {
	s.handleServicesName(w, r, func(svc service.ServicesService) servicesNameVerb { return svc.Restart })
}

type servicesNameVerb func(context.Context, *service.ServiceNameRequest) (*service.ServiceRecord, error)

func (s *Server) handleServicesName(w http.ResponseWriter, r *http.Request, pick func(service.ServicesService) servicesNameVerb) {
	svc, ok := s.servicesService(w, r)
	if !ok {
		return
	}
	rec, err := pick(svc)(r.Context(), &service.ServiceNameRequest{Name: chi.URLParam(r, "name")})
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}
	RespondJSON(w, r, http.StatusOK, rec)
}

func (s *Server) servicesService(w http.ResponseWriter, r *http.Request) (service.ServicesService, bool) {
	if s.service == nil || s.service.Services() == nil {
		RespondError(w, r, http.StatusServiceUnavailable, ErrServicesServiceUnavailable)
		return nil, false
	}
	return s.service.Services(), true
}
