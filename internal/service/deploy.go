// Package service orchestrates deploy: normalize request, generate manifests, apply to cluster, update store.
package service

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"kube-deploy/internal/k8s"
	"kube-deploy/internal/model"
	"kube-deploy/internal/store"
)

// DeployService connects HTTP-layer deploy requests to manifest generation and cluster apply.
type DeployService struct {
	store   *store.Store
	applier *k8s.Applier
}

// NewDeployService builds the orchestrator with its dependencies.
func NewDeployService(st *store.Store, applier *k8s.Applier) *DeployService {
	return &DeployService{store: st, applier: applier}
}

// Deploy validates and applies a full application stack, updating status throughout.
func (s *DeployService) Deploy(ctx context.Context, req model.DeployRequest) (*model.DeploymentRecord, error) {
	k8s.Normalize(&req)

	id := uuid.NewString()
	now := time.Now().UTC()

	rec := &model.DeploymentRecord{
		ID:        id,
		Status:    model.StatusApplying,
		Request:   req,
		Namespace: req.Namespace,
		CreatedAt: now,
		UpdatedAt: now,
	}

	bundle, ingressHost, err := k8s.Generate(id, req, now)
	if err != nil {
		return nil, err
	}
	rec.Resources = bundle.ResourceNames()
	rec.IngressHost = ingressHost
	s.store.Save(rec)

	if err := s.applier.Apply(ctx, bundle); err != nil {
		s.fail(id, err.Error())
		return nil, fmt.Errorf("apply manifests: %w", err)
	}

	s.store.Update(id, func(r *model.DeploymentRecord) {
		r.Status = model.StatusDeployed
		r.Error = ""
		r.UpdatedAt = time.Now().UTC()
	})

	rec, _ = s.store.Get(id)
	return rec, nil
}

// Get returns one deployment by id.
func (s *DeployService) Get(id string) (*model.DeploymentRecord, bool) {
	return s.store.Get(id)
}

// List returns all deployments tracked by this API process.
func (s *DeployService) List() []model.DeploymentRecord {
	return s.store.List()
}

// fail marks a deployment as failed and stores the error message for the API response.
func (s *DeployService) fail(id, msg string) {
	s.store.Update(id, func(r *model.DeploymentRecord) {
		r.Status = model.StatusFailed
		r.Error = msg
		r.UpdatedAt = time.Now().UTC()
	})
}
