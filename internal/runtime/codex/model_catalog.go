package codex

import (
	"context"
	"fmt"
	"openagentx/internal/domain"
	"slices"
)

func cloneModelEfforts(input map[string][]string) map[string][]string {
	result := make(map[string][]string, len(input))
	for model, efforts := range input {
		result[model] = slices.Clone(efforts)
	}
	return result
}

// Discover once per Worker lifetime before registration. This is ordinary RPC,
// not a model turn. The same catalog governs the menu, planner and adapter.
func (a *Adapter) loadModelCatalog(ctx context.Context, client *RPCClient) error {
	a.mu.Lock()
	loaded := a.catalogLoaded
	a.mu.Unlock()
	if loaded {
		return nil
	}
	efforts := map[string][]string{}
	var models []string
	var cursor *string
	for page := 0; page < 20; page++ {
		var response struct {
			Data []struct {
				Model     string `json:"model"`
				Supported []struct {
					Effort string `json:"reasoningEffort"`
				} `json:"supportedReasoningEfforts"`
			} `json:"data"`
			NextCursor *string `json:"nextCursor"`
		}
		if err := client.Call(ctx, "model/list", map[string]any{"limit": 100, "cursor": cursor}, &response); err != nil {
			return fmt.Errorf("Codex model catalog: %w", err)
		}
		for _, item := range response.Data {
			if err := domain.ValidateIdentifier("model", item.Model); err != nil {
				return err
			}
			if !slices.Contains(models, item.Model) {
				models = append(models, item.Model)
			}
			for _, e := range item.Supported {
				if err := domain.ValidateIdentifier("reasoning effort", e.Effort); err != nil {
					return err
				}
				if !slices.Contains(efforts[item.Model], e.Effort) {
					efforts[item.Model] = append(efforts[item.Model], e.Effort)
				}
			}
		}
		if response.NextCursor == nil || *response.NextCursor == "" {
			if len(models) == 0 {
				return fmt.Errorf("Codex returned an empty model catalog")
			}
			a.mu.Lock()
			defer a.mu.Unlock()
			// Keep the configured first model as the existing default. Explicit
			// local models may be absent from a provider's public catalog.
			for _, model := range models {
				if !slices.Contains(a.config.Models, model) {
					a.config.Models = append(a.config.Models, model)
				}
			}
			a.modelEfforts = efforts
			a.catalogLoaded = true
			return nil
		}
		if cursor != nil && *cursor == *response.NextCursor {
			return fmt.Errorf("Codex model catalog cursor did not advance")
		}
		cursor = response.NextCursor
	}
	return fmt.Errorf("Codex model catalog exceeds page limit")
}
