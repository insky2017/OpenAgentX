package nativebridge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"

	"openagentx/internal/domain"
)

func settingsVersion(s domain.AgentModelSettings) string {
	return fmt.Sprintf("oax-agent-model:%d", s.Version)
}
func nullableEffort(e string) any {
	if e == "" {
		return nil
	}
	return e
}

// These writes change only the durable Agent preference. In particular they
// never reach app-server's user config or change an already frozen Run.
func (b *bridge) updateSettings(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(params, &fields); err != nil {
		return nil, err
	}
	if fields == nil {
		return nil, errors.New("模型设置必须是对象")
	}
	var model, effort *string
	var expected string
	if method == "thread/settings/update" {
		var thread string
		if json.Unmarshal(fields["threadId"], &thread) != nil || thread != b.threadID {
			return nil, errors.New("模型设置只能操作当前 Agent 的会话")
		}
		for key, value := range fields {
			switch key {
			case "threadId", "collaborationMode":
			case "model":
				if string(value) != "null" {
					if err := json.Unmarshal(value, &model); err != nil {
						return nil, err
					}
				}
			case "effort":
				if string(value) != "null" {
					if err := json.Unmarshal(value, &effort); err != nil {
						return nil, err
					}
				}
			default:
				if string(value) != "null" {
					return nil, fmt.Errorf("受管终端仅允许修改 model/effort，不支持 %s", key)
				}
			}
		}
		if value, ok := fields["collaborationMode"]; ok && string(value) != "null" {
			nestedModel, nestedEffort, err := nativeCollaborationModel(value)
			if err != nil {
				return nil, err
			}
			if model != nil && nestedModel != nil && *model != *nestedModel || effort != nil && nestedEffort != nil && *effort != *nestedEffort {
				return nil, errors.New("模型设置中的 collaborationMode 与 model/effort 冲突")
			}
			if nestedModel != nil {
				model = nestedModel
			}
			if nestedEffort != nil {
				effort = nestedEffort
			}
		}

	} else {
		for key, value := range fields {
			switch key {
			case "edits":
			case "filePath":
				if string(value) != "null" {
					return nil, errors.New("受管终端不允许写配置文件；模型偏好仅保存在 OAX")
				}
			case "expectedVersion":
				if string(value) != "null" {
					if err := json.Unmarshal(value, &expected); err != nil {
						return nil, err
					}
				}
			case "reloadUserConfig":
				var reload bool
				if err := json.Unmarshal(value, &reload); err != nil {
					return nil, err
				}
			default:
				return nil, fmt.Errorf("不支持的模型配置字段 %s", key)
			}
		}
		var edits []struct {
			KeyPath       string          `json:"keyPath"`
			Value         json.RawMessage `json:"value"`
			MergeStrategy string          `json:"mergeStrategy"`
		}
		if err := json.Unmarshal(fields["edits"], &edits); err != nil {
			return nil, err
		}
		if len(edits) == 0 {
			return nil, errors.New("模型配置 edits 不能为空")
		}
		seen := map[string]bool{}
		for _, edit := range edits {
			if edit.MergeStrategy != "replace" && edit.MergeStrategy != "upsert" {
				return nil, errors.New("不支持的模型配置 mergeStrategy")
			}
			key := edit.KeyPath
			switch key {
			case "model", "models.new_thread.model":
				key = "model"
			case "model_reasoning_effort", "models.new_thread.model_reasoning_effort":
				key = "effort"
			default:
				return nil, fmt.Errorf("受管终端仅允许模型配置，不支持 %s", key)
			}
			if seen[key] {
				return nil, fmt.Errorf("模型配置重复字段 %s", key)
			}
			seen[key] = true
			var value string
			if string(edit.Value) == "null" && key == "effort" {
				value = ""
			} else if err := json.Unmarshal(edit.Value, &value); err != nil {
				return nil, err
			}
			if key == "model" {
				model = &value
			} else {
				effort = &value
			}
		}
	}
	if model == nil && effort == nil {
		return nil, errors.New("未提供 model 或 effort 设置")
	}
	if model != nil && *model == "" {
		return nil, errors.New("model 不能为空")
	}
	b.settingsMu.Lock()
	defer b.settingsMu.Unlock()
	current, err := b.control.GetAgentModelSettings(ctx, b.agentID, b.backendID)
	if err != nil {
		return nil, err
	}
	if expected != "" && expected != settingsVersion(current) {
		return nil, errors.New("模型设置版本已变化；请刷新后重试")
	}
	next := domain.AgentModelSettingsUpdate{BackendID: b.backendID, Model: current.Model, Effort: current.Effort, ExpectedVersion: current.Version}
	if model != nil {
		next.Model = *model
	}
	if effort != nil {
		next.Effort = *effort
	}
	saved := current
	if next.Model != current.Model || next.Effort != current.Effort {
		saved, err = b.control.SetAgentModelSettings(ctx, b.agentID, next)
		if err != nil {
			return nil, err
		}
	}
	if method == "thread/settings/update" {
		return rawResult(map[string]any{})
	}
	// The protocol requires a path even for this virtual configuration layer.
	// It identifies OAX's durable store, never the user's Codex configuration.
	store := filepath.Clean(filepath.Join(filepath.Dir(b.statePath), "..", "..", "..", "data", "openagentx.db"))
	return rawResult(map[string]any{"status": "ok", "version": settingsVersion(saved), "filePath": store, "overriddenMetadata": nil})
}

func (b *bridge) projectSettings(ctx context.Context, method string, raw json.RawMessage) (json.RawMessage, error) {
	switch method {
	case "thread/resume", "config/read", "model/list", "thread/settings/updated":
	default:
		return raw, nil
	}
	settings, err := b.control.GetAgentModelSettings(ctx, b.agentID, b.backendID)
	if err != nil {
		return nil, err
	}
	var result map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	if result == nil {
		return nil, errors.New("Codex 返回空模型配置")
	}
	switch method {
	case "thread/resume":
		result["model"], result["reasoningEffort"] = settings.Model, nullableEffort(settings.Effort)
		projectCollaboration(result, settings)
	case "thread/settings/updated":
		if s, ok := result["threadSettings"].(map[string]any); ok {
			s["model"], s["effort"] = settings.Model, nullableEffort(settings.Effort)
			projectCollaboration(s, settings)
		}
	case "config/read":
		config, ok := result["config"].(map[string]any)
		if !ok {
			return nil, errors.New("Codex config/read 缺少配置对象")
		}
		config["model"], config["model_reasoning_effort"] = settings.Model, nullableEffort(settings.Effort)
		if models, ok := config["models"].(map[string]any); ok {
			if next, ok := models["new_thread"].(map[string]any); ok {
				next["model"], next["model_reasoning_effort"] = settings.Model, nullableEffort(settings.Effort)
			}
		}
		origins, ok := result["origins"].(map[string]any)
		if !ok {
			origins = map[string]any{}
			result["origins"] = origins
		}
		meta := map[string]any{"name": map[string]any{"type": "sessionFlags"}, "version": settingsVersion(settings)}
		origins["model"], origins["model_reasoning_effort"] = meta, meta
	case "model/list":
		allowed := map[string]bool{}
		for _, model := range settings.Models {
			allowed[model] = true
		}
		filtered := []any{}
		data, ok := result["data"].([]any)
		if !ok {
			return nil, errors.New("Codex model/list 缺少模型目录")
		}
		for _, item := range data {
			model, ok := item.(map[string]any)
			if !ok {
				continue
			}
			name, _ := model["model"].(string)
			if !allowed[name] {
				continue
			}
			model["isDefault"] = name == settings.Model
			if efforts, ok := settings.ModelEfforts[name]; ok {
				allowedEffort := map[string]bool{}
				for _, effort := range efforts {
					allowedEffort[effort] = true
				}
				opts := []any{}
				if source, ok := model["supportedReasoningEfforts"].([]any); ok {
					for _, opt := range source {
						if o, ok := opt.(map[string]any); ok {
							if e, ok := o["reasoningEffort"].(string); ok && allowedEffort[e] {
								opts = append(opts, opt)
							}
						}
					}
				}
				model["supportedReasoningEfforts"] = opts
			}
			if name == settings.Model && settings.Effort != "" {
				model["defaultReasoningEffort"] = settings.Effort
			}
			filtered = append(filtered, model)
		}
		result["data"] = filtered
	}
	return rawResult(result)
}

func projectCollaboration(target map[string]any, settings domain.AgentModelSettings) {
	if mode, ok := target["collaborationMode"].(map[string]any); ok {
		if values, ok := mode["settings"].(map[string]any); ok {
			values["model"], values["reasoning_effort"] = settings.Model, nullableEffort(settings.Effort)
		}
	}
}

// Codex 0.160.1's /model menu carries its selection inside collaborationMode.
// Extract only model/effort; mode changes and developer instructions stay denied.
func nativeCollaborationModel(raw json.RawMessage) (*string, *string, error) {
	var value struct {
		Mode     string `json:"mode"`
		Settings struct {
			Model        *string         `json:"model"`
			Effort       json.RawMessage `json:"reasoning_effort"`
			Instructions json.RawMessage `json:"developer_instructions"`
		} `json:"settings"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return nil, nil, err
	}
	if value.Mode != "default" || len(value.Settings.Instructions) > 0 && string(value.Settings.Instructions) != "null" {
		return nil, nil, errors.New("受管模型菜单不允许改变协作模式或开发者指令")
	}
	var effort *string
	if len(value.Settings.Effort) > 0 {
		s := ""
		if string(value.Settings.Effort) != "null" {
			if err := json.Unmarshal(value.Settings.Effort, &s); err != nil {
				return nil, nil, err
			}
		}
		effort = &s
	}
	return value.Settings.Model, effort, nil
}
