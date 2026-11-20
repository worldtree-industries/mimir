package service

import (
	"context"

	"github.com/worldtree-industries/mimir/models"
)

// Configs lists the Configs Mimir knows from Registration.
func (s *Service) Configs(ctx context.Context) ([]models.ConfigInfo, error) {
	return s.store.ConfigInfos(ctx)
}

// Items lists the Items Mimir knows from Registration, those of config only
// unless config is empty.
func (s *Service) Items(ctx context.Context, config string) ([]models.RegisteredItem, error) {
	return s.store.RegisteredItems(ctx, config)
}

// Namespaces lists the Namespaces Mimir knows from Registration, those of
// config only unless config is empty.
func (s *Service) Namespaces(ctx context.Context, config string) ([]models.RegisteredNamespace, error) {
	return s.store.RegisteredNamespaces(ctx, config)
}
